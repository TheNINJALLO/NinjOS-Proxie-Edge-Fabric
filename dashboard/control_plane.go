// Copyright (C) 2026 Ninj-OS contributors.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"archive/zip"
	"bytes"
	"crypto/hmac"
	cryptorand "crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type healthActionState struct {
	Disabled      map[string]bool  `json:"disabled"`
	UnhealthyFrom map[string]int64 `json:"unhealthyFrom"`
	HealthyFrom   map[string]int64 `json:"healthyFrom"`
}

var transferTicketPattern = regexp.MustCompile(`ticket=([a-fA-F0-9]{16,64})`)

func (d *dashboard) initializeControlPlane() error {
	store, err := openLedger(filepath.Join(d.runtimeDir, "edge-fabric.db"))
	if err != nil {
		return err
	}
	d.ledger = store

	files := map[string]string{
		"health-actions.properties": `# Ninj-OS Edge Fabric automated health actions
enabled=true
require_companion=false
companion_stale_seconds=45
degraded_after_seconds=30
recovery_seconds=60
minimum_tps=12
maximum_mspt=80
`,
		"protection-profiles.properties": `# profile.key=value
default.max_datagram_size=2048
default.max_packets_per_second_per_ip=6000
default.max_handshakes_per_minute=30
default.max_sessions_per_ip=4
default.allow_new_sessions_during_incident=false

kingdom.max_packets_per_second_per_ip=7000
kingdom.max_handshakes_per_minute=35
kingdom.max_sessions_per_ip=5

zoo.max_packets_per_second_per_ip=4000
zoo.max_handshakes_per_minute=20
zoo.max_sessions_per_ip=3
`,
		"health-action-state.json": `{"disabled":{},"unhealthyFrom":{},"healthyFrom":{}}`,
	}
	for name, content := range files {
		path := filepath.Join(d.runtimeDir, name)
		stat, err := os.Stat(path)
		if os.IsNotExist(err) || (err == nil && stat.Size() == 0) {
			_ = os.WriteFile(path, []byte(content), 0644)
		}
	}
	return nil
}

func (d *dashboard) startControlPlaneLoops() {
	go d.metricHistoryLoop()
	go d.healthActionLoop()
	go d.transferTransactionLoop()
	go d.sessionCleanupLoop()
}

func randomSessionToken() (string, error) {
	value := make([]byte, 32)
	if _, err := cryptorand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func normalizeTOTPSecret(secret string) string {
	secret = strings.ToUpper(strings.TrimSpace(secret))
	secret = strings.ReplaceAll(secret, " ", "")
	secret = strings.ReplaceAll(secret, "-", "")
	return secret
}

func validTOTP(secret, code string, now time.Time) bool {
	secret = normalizeTOTPSecret(secret)
	code = strings.TrimSpace(code)
	if secret == "" {
		return true
	}
	if len(code) != 6 {
		return false
	}
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		return false
	}
	counter := now.Unix() / 30
	for offset := int64(-1); offset <= 1; offset++ {
		block := make([]byte, 8)
		value := uint64(counter + offset)
		for index := 7; index >= 0; index-- {
			block[index] = byte(value)
			value >>= 8
		}
		mac := hmac.New(sha1.New, key)
		_, _ = mac.Write(block)
		sum := mac.Sum(nil)
		truncate := int(sum[len(sum)-1] & 0x0f)
		number := (int(sum[truncate]&0x7f) << 24) |
			(int(sum[truncate+1]) << 16) |
			(int(sum[truncate+2]) << 8) |
			int(sum[truncate+3])
		expected := fmt.Sprintf("%06d", number%1000000)
		if hmac.Equal([]byte(expected), []byte(code)) {
			return true
		}
	}
	return false
}

func (d *dashboard) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]string{"error": "Method not allowed"})
		return
	}
	if d.setupRequired() {
		writeJSON(w, 409, map[string]any{"error": "Owner setup required", "setupRequired": true})
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Token    string `json:"token"` // legacy v7.0.x clients
		TOTP     string `json:"totp"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&body) != nil {
		writeJSON(w, 400, map[string]string{"error": "Invalid JSON"})
		return
	}
	username := strings.ToLower(strings.TrimSpace(body.Username))
	provided := body.Password
	if provided == "" {
		provided = body.Token
	}

	// Emergency recovery never becomes a stored dashboard credential. It only
	// creates a short-lived owner session that can replace the owner account.
	if d.recoveryToken != "" && username == "recovery" &&
		hmac.Equal([]byte(tokenHash(provided)), []byte(tokenHash(d.recoveryToken))) {
		token, expires, err := d.createBrowserSession("recovery", "owner", false)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "Unable to create recovery session"})
			return
		}
		d.appendAudit(r, principal{Username: "recovery", Role: "owner"}, "session.recovery_login", "success", map[string]any{"expiresAt": expires})
		writeJSON(w, 200, map[string]any{
			"token": token, "expiresAt": expires,
			"principal": principal{Username: "recovery", Role: "owner"},
			"recovery":  true,
		})
		return
	}

	for _, user := range d.users() {
		if !user.Enabled || strings.ToLower(user.Username) != username {
			continue
		}
		if !verifyUserCredential(user, provided) {
			break
		}
		if !validTOTP(user.TOTPSecret, body.TOTP, time.Now()) {
			d.appendAudit(r, principal{Username: username, Role: user.Role}, "session.login", "totp_failed", nil)
			writeJSON(w, 401, map[string]string{"error": "Invalid login or two-factor code"})
			return
		}
		token, expires, err := d.createBrowserSession(user.Username, user.Role, user.TOTPSecret != "")
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "Unable to create session"})
			return
		}
		d.appendAudit(r, principal{Username: user.Username, Role: user.Role}, "session.login", "success", map[string]any{"expiresAt": expires})
		writeJSON(w, 200, map[string]any{
			"token": token, "expiresAt": expires,
			"principal": principal{Username: user.Username, Role: user.Role},
			"totp":      user.TOTPSecret != "",
		})
		return
	}
	time.Sleep(250 * time.Millisecond)
	writeJSON(w, 401, map[string]string{"error": "Invalid login or two-factor code"})
}

func (d *dashboard) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]string{"error": "Method not allowed"})
		return
	}
	provided := bearerToken(r)
	if provided != "" && d.ledger != nil {
		d.ledger.deleteSession(tokenHash(provided))
	}
	writeJSON(w, 200, map[string]bool{"loggedOut": true})
}

func bearerToken(r *http.Request) string {
	provided := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if provided == "" {
		provided = strings.TrimSpace(r.URL.Query().Get("token"))
	}
	return provided
}

func (d *dashboard) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	required := []string{"metrics", "presence", "transfer"}
	type fleetCapability struct {
		ServerID     string   `json:"serverId"`
		Version      string   `json:"version"`
		Capabilities []string `json:"capabilities"`
		Missing      []string `json:"missing"`
		Compatible   bool     `json:"compatible"`
	}
	result := make([]fleetCapability, 0)
	for _, state := range d.companionStates() {
		serverID := textField(state, "serverId")
		metrics, _ := state["metrics"].(map[string]any)
		version := textField(metrics, "companionVersion")
		capabilities := stringSlice(metrics["capabilities"])
		available := map[string]bool{}
		for _, capability := range capabilities {
			available[capability] = true
		}
		missing := make([]string, 0)
		for _, capability := range required {
			if !available[capability] {
				missing = append(missing, capability)
			}
		}
		result = append(result, fleetCapability{
			ServerID: serverID, Version: version,
			Capabilities: capabilities, Missing: missing,
			Compatible: len(missing) == 0,
		})
	}
	writeJSON(w, 200, map[string]any{"required": required, "fleet": result})
}

func stringSlice(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		if typed, ok := value.([]string); ok {
			return typed
		}
		return []string{}
	}
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
			result = append(result, text)
		}
	}
	return result
}

func (d *dashboard) handleProfiles(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	writeJSON(w, 200, map[string]any{"profiles": d.ledger.profiles(limit)})
}

func (d *dashboard) handleProfileAccess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]string{"error": "Method not allowed"})
		return
	}
	var body struct {
		XUID   string          `json:"xuid"`
		Role   string          `json:"role"`
		Banned bool            `json:"banned"`
		Access map[string]bool `json:"access"`
		Notes  string          `json:"notes"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 128*1024)).Decode(&body) != nil || strings.TrimSpace(body.XUID) == "" {
		writeJSON(w, 400, map[string]string{"error": "xuid is required"})
		return
	}
	if body.Role == "" {
		body.Role = "member"
	}
	if body.Access == nil {
		body.Access = map[string]bool{}
	}
	if err := d.ledger.updateProfileAccess(body.XUID, body.Role, body.Banned, body.Access, body.Notes); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	actor := principalFromRequest(r)
	d.appendAudit(r, actor, "profile.access.update", "success", body)
	writeJSON(w, 200, map[string]bool{"updated": true})
}

func validateProtectionProfiles(content string) error {
	allowed := map[string]string{
		"max_datagram_size":                  "uint",
		"max_packets_per_second_per_ip":      "uint",
		"max_handshakes_per_minute":          "uint",
		"max_sessions_per_ip":                "uint",
		"allow_new_sessions_during_incident": "bool",
	}
	for number, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(strings.SplitN(raw, "#", 2)[0])
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("line %d must use profile.key=value", number+1)
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		dot := strings.LastIndex(key, ".")
		if dot <= 0 || dot == len(key)-1 {
			return fmt.Errorf("line %d must use profile.key=value", number+1)
		}
		field := key[dot+1:]
		kind, ok := allowed[field]
		if !ok {
			return fmt.Errorf("unsupported protection profile field: %s", field)
		}
		switch kind {
		case "uint":
			valueNumber, err := strconv.ParseUint(value, 10, 64)
			if err != nil || valueNumber == 0 {
				return fmt.Errorf("%s must be a positive integer", key)
			}
		case "bool":
			if value != "true" && value != "false" && value != "1" && value != "0" {
				return fmt.Errorf("%s must be true or false", key)
			}
		}
	}
	return nil
}

func (d *dashboard) handleProtectionProfiles(w http.ResponseWriter, r *http.Request) {
	path := filepath.Join(d.runtimeDir, "protection-profiles.properties")
	if r.Method == http.MethodGet {
		content, _ := os.ReadFile(path)
		writeJSON(w, 200, map[string]any{"content": string(content), "path": path})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]string{"error": "Method not allowed"})
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 256*1024)).Decode(&body) != nil {
		writeJSON(w, 400, map[string]string{"error": "Invalid JSON"})
		return
	}
	if err := validateProtectionProfiles(body.Content); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	d.fileMu.Lock()
	current, _ := os.ReadFile(path)
	if len(current) > 0 {
		_ = os.WriteFile(path+".bak", current, 0644)
	}
	temporary := path + ".tmp"
	err := os.WriteFile(temporary, []byte(body.Content), 0644)
	if err == nil {
		err = os.Rename(temporary, path)
	}
	if err == nil {
		err = d.queueCommand("reload", "", "")
	}
	d.fileMu.Unlock()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	actor := principalFromRequest(r)
	d.appendAudit(r, actor, "protection_profiles.apply", "queued", map[string]any{"path": path})
	writeJSON(w, 202, map[string]bool{"queued": true})
}

func (d *dashboard) handleMetricHistory(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		writeJSON(w, 400, map[string]string{"error": "Metric name is required"})
		return
	}
	server := strings.TrimSpace(r.URL.Query().Get("server"))
	minutes, _ := strconv.Atoi(r.URL.Query().Get("minutes"))
	if minutes <= 0 {
		minutes = 60
	}
	if minutes > 60*24*30 {
		minutes = 60 * 24 * 30
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 5000
	}
	since := time.Now().Add(-time.Duration(minutes) * time.Minute).UnixMilli()
	writeJSON(w, 200, map[string]any{
		"name": name, "server": server, "minutes": minutes,
		"samples": d.ledger.metricHistory(name, server, since, limit),
	})
}

func (d *dashboard) handleAlerts(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	writeJSON(w, 200, map[string]any{"alerts": d.ledger.alerts(limit)})
}

func (d *dashboard) handleTransferTransactions(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	writeJSON(w, 200, map[string]any{"transactions": d.ledger.recentTransfers(limit)})
}

func redactJSON(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		output := map[string]any{}
		for key, item := range typed {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "token") || strings.Contains(lower, "secret") ||
				strings.Contains(lower, "password") || strings.Contains(lower, "webhook") {
				output[key] = "[REDACTED]"
				continue
			}
			if lower == "ip" || lower == "client" || lower == "address" || lower == "sourceip" {
				if item == nil || fmt.Sprint(item) == "" {
					output[key] = ""
				} else {
					hash := sha256.Sum256([]byte(fmt.Sprint(item)))
					output[key] = "sha256:" + hex.EncodeToString(hash[:6])
				}
				continue
			}
			output[key] = redactJSON(item)
		}
		return output
	case []any:
		output := make([]any, 0, len(typed))
		for _, item := range typed {
			output = append(output, redactJSON(item))
		}
		return output
	default:
		return value
	}
}

func (d *dashboard) handleSupportBundle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]string{"error": "Method not allowed"})
		return
	}
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	addJSON := func(name string, value any) {
		writer, _ := archive.Create(name)
		bytes, _ := json.MarshalIndent(redactJSON(value), "", "  ")
		_, _ = writer.Write(bytes)
	}
	addText := func(name, text string) {
		writer, _ := archive.Create(name)
		_, _ = writer.Write([]byte(text))
	}

	addJSON("gateway-state.json", safeMap(filepath.Join(d.runtimeDir, "gateway-state.json")))
	addJSON("sessions.json", safeSlice(filepath.Join(d.runtimeDir, "sessions.json")))
	addJSON("presence.json", d.presenceRecords())
	addJSON("companions.json", d.companionStates())
	addJSON("transfers.json", d.ledger.recentTransfers(500))
	addJSON("alerts.json", d.ledger.alerts(500))
	addJSON("profiles.json", d.ledger.profiles(1000))
	if document, err := loadINI(d.configPath); err == nil {
		addText("edge-fabric.ini", renderUnifiedConfig(redactUnifiedDocument(document)))
	}
	addJSON("config-summary.json", safeMap(filepath.Join(d.runtimeDir, "config-summary.json")))
	addText("live-config.properties", redactedConfigFile(d.liveConfigPath))
	addText("protection-profiles.properties", redactedConfigFile(filepath.Join(d.runtimeDir, "protection-profiles.properties")))
	addText("health-actions.properties", redactedConfigFile(filepath.Join(d.runtimeDir, "health-actions.properties")))
	addJSON("recent-events.json", readRotated(filepath.Join(d.runtimeDir, "events.jsonl"), 500))
	addJSON("recent-audit.json", readRotated(filepath.Join(d.runtimeDir, "audit.jsonl"), 500))
	addJSON("system.json", map[string]any{
		"version":       d.version,
		"goVersion":     runtimeVersion(),
		"timestamp":     time.Now().UnixMilli(),
		"uptimeSeconds": time.Since(d.started).Seconds(),
	})
	_ = archive.Close()

	actor := principalFromRequest(r)
	d.appendAudit(r, actor, "support_bundle.generate", "success", map[string]any{"bytes": buffer.Len()})
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="NinjOS-Edge-Fabric-v`+productVersion+`-Support.zip"`)
	w.WriteHeader(200)
	_, _ = w.Write(buffer.Bytes())
}

func runtimeVersion() string {
	return runtime.Version()
}

func redactedConfigFile(path string) string {
	bytes, _ := os.ReadFile(path)
	var builder strings.Builder
	for _, line := range strings.Split(string(bytes), "\n") {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "token") || strings.Contains(lower, "secret") ||
			strings.Contains(lower, "password") || strings.Contains(lower, "webhook") {
			if parts := strings.SplitN(line, "=", 2); len(parts) == 2 {
				builder.WriteString(parts[0] + "=[REDACTED]\n")
				continue
			}
		}
		builder.WriteString(line + "\n")
	}
	return builder.String()
}

func (d *dashboard) recordPresenceProfiles(serverID string, updates []map[string]any) {
	if d.ledger == nil {
		return
	}
	now := time.Now().UnixMilli()
	for _, record := range updates {
		xuid := textField(record, "xuid")
		name := firstNonempty(textField(record, "playerName"), textField(record, "player"))
		if xuid == "" {
			continue
		}
		d.ledger.upsertProfile(xuid, name, serverID, now)
		d.ledger.confirmArrival(xuid, serverID, now)
	}
}

func (d *dashboard) recordPolicyVersion(actor, content, result string, active bool) {
	if d.ledger == nil {
		return
	}
	hash := sha256.Sum256([]byte(content))
	d.ledger.recordPolicy(
		time.Now().UnixMilli(), actor, content,
		hex.EncodeToString(hash[:]), result, active,
	)
}

func numberValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		number, err := typed.Float64()
		return number, err == nil
	case string:
		number, err := strconv.ParseFloat(typed, 64)
		return number, err == nil
	default:
		return 0, false
	}
}

func (d *dashboard) metricHistoryLoop() {
	sampleSeconds := max64(1, envInt64("METRIC_SAMPLE_SECONDS", 15))
	retentionDays := max64(1, envInt64("METRIC_RETENTION_DAYS", 30))
	ticker := time.NewTicker(time.Duration(sampleSeconds) * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if d.ledger == nil {
			continue
		}
		now := time.Now().UnixMilli()
		gateway := safeMap(filepath.Join(d.runtimeDir, "gateway-state.json"))
		for name, key := range map[string]string{
			"gateway.active_sessions": "activeSessions",
			"gateway.tracked_ips":     "trackedIps",
		} {
			if value, ok := numberValue(gateway[key]); ok {
				d.ledger.recordMetric(now, "gateway", "", name, value)
			}
		}
		counters, _ := gateway["counters"].(map[string]any)
		for name, key := range map[string]string{
			"gateway.dropped_packets": "droppedPackets",
			"gateway.rate_limited":    "rateLimited",
			"gateway.temporary_bans":  "temporaryBans",
		} {
			if value, ok := numberValue(counters[key]); ok {
				d.ledger.recordMetric(now, "gateway", "", name, value)
			}
		}
		incident, _ := gateway["incident"].(map[string]any)
		if value, ok := numberValue(incident["packetsPerSecond"]); ok {
			d.ledger.recordMetric(now, "gateway", "", "gateway.packets_per_second", value)
		}
		if value, ok := numberValue(incident["dropRatio"]); ok {
			d.ledger.recordMetric(now, "gateway", "", "gateway.drop_ratio", value)
		}
		if firewall, ok := gateway["firewall"].(map[string]any); ok {
			if raw, ok := firewall["topRisk"].([]any); ok {
				rows := make([]map[string]any, 0, len(raw))
				for _, item := range raw {
					if row, ok := item.(map[string]any); ok {
						rows = append(rows, row)
					}
				}
				d.ledger.firewallSnapshot(now, rows)
			}
		}
		for _, companion := range d.companionStates() {
			server := textField(companion, "serverId")
			metrics, _ := companion["metrics"].(map[string]any)
			for name, key := range map[string]string{
				"bds.tps":                   "currentTps",
				"bds.mspt":                  "currentMspt",
				"bds.online_players":        "onlinePlayers",
				"bds.cpu_percent":           "processCpuPercent",
				"bds.memory_bytes":          "processMemoryBytes",
				"companion.queue_depth":     "queueDepth",
				"companion.upload_failures": "uploadFailures",
			} {
				if value, ok := numberValue(metrics[key]); ok {
					d.ledger.recordMetric(now, "companion", server, name, value)
				}
			}
		}
		d.ledger.pruneMetrics(now - int64(time.Duration(retentionDays)*24*time.Hour/time.Millisecond))
	}
}

func parseSimpleProperties(path string) map[string]string {
	values := map[string]string{}
	bytes, _ := os.ReadFile(path)
	for _, raw := range strings.Split(string(bytes), "\n") {
		line := strings.TrimSpace(strings.SplitN(raw, "#", 2)[0])
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			values[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return values
}

func propertyBool(values map[string]string, key string, fallback bool) bool {
	value, exists := values[key]
	if !exists {
		return fallback
	}
	return value == "true" || value == "1" || value == "yes" || value == "on"
}

func propertyInt(values map[string]string, key string, fallback int64) int64 {
	value, err := strconv.ParseInt(values[key], 10, 64)
	if err != nil {
		return fallback
	}
	return value
}

func loadHealthActionState(path string) healthActionState {
	state := healthActionState{
		Disabled: map[string]bool{}, UnhealthyFrom: map[string]int64{}, HealthyFrom: map[string]int64{},
	}
	bytes, _ := os.ReadFile(path)
	_ = json.Unmarshal(bytes, &state)
	if state.Disabled == nil {
		state.Disabled = map[string]bool{}
	}
	if state.UnhealthyFrom == nil {
		state.UnhealthyFrom = map[string]int64{}
	}
	if state.HealthyFrom == nil {
		state.HealthyFrom = map[string]int64{}
	}
	return state
}

func saveHealthActionState(path string, state healthActionState) {
	bytes, _ := json.MarshalIndent(state, "", "  ")
	_ = os.WriteFile(path, bytes, 0644)
}

func (d *dashboard) healthActionLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	statePath := filepath.Join(d.runtimeDir, "health-action-state.json")
	for range ticker.C {
		values := parseSimpleProperties(filepath.Join(d.runtimeDir, "health-actions.properties"))
		if !propertyBool(values, "enabled", true) {
			continue
		}
		requireCompanion := propertyBool(values, "require_companion", false)
		staleAfter := propertyInt(values, "companion_stale_seconds", 45) * 1000
		degradedAfter := propertyInt(values, "degraded_after_seconds", 30) * 1000
		recoveryAfter := propertyInt(values, "recovery_seconds", 60) * 1000
		minimumTPS := float64(propertyInt(values, "minimum_tps", 12))
		maximumMSPT := float64(propertyInt(values, "maximum_mspt", 80))

		now := time.Now().UnixMilli()
		gateway := safeMap(filepath.Join(d.runtimeDir, "gateway-state.json"))
		rawBackends, _ := gateway["backends"].([]any)
		companionByServer := map[string]map[string]any{}
		for _, companion := range d.companionStates() {
			companionByServer[textField(companion, "serverId")] = companion
		}
		state := loadHealthActionState(statePath)

		for _, raw := range rawBackends {
			backend, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			server := textField(backend, "name")
			gatewayHealthy, _ := backend["healthy"].(bool)
			healthy := gatewayHealthy
			reason := ""
			companion := companionByServer[server]
			if companion == nil && requireCompanion {
				healthy = false
				reason = "companion missing"
			} else if companion != nil {
				timestamp, _ := numberValue(companion["timestamp"])
				if now-int64(timestamp) > staleAfter {
					healthy = false
					reason = "companion stale"
				}
				metrics, _ := companion["metrics"].(map[string]any)
				if tps, ok := numberValue(metrics["currentTps"]); ok && tps < minimumTPS {
					healthy = false
					reason = fmt.Sprintf("TPS %.2f below %.2f", tps, minimumTPS)
				}
				if mspt, ok := numberValue(metrics["currentMspt"]); ok && mspt > maximumMSPT {
					healthy = false
					reason = fmt.Sprintf("MSPT %.2f above %.2f", mspt, maximumMSPT)
				}
			}
			if !gatewayHealthy {
				reason = "gateway backend health failed"
			}

			if !healthy {
				state.HealthyFrom[server] = 0
				if state.UnhealthyFrom[server] == 0 {
					state.UnhealthyFrom[server] = now
				}
				if !state.Disabled[server] && now-state.UnhealthyFrom[server] >= degradedAfter {
					_ = d.queueCommand("backend", server, "false")
					state.Disabled[server] = true
					message := "Automatically disabled new transfers to " + server + ": " + reason
					d.ledger.recordAlert("backend-auto-disable", "warning", server, message)
					d.appendJSONL(filepath.Join(d.runtimeDir, "events.jsonl"), map[string]any{
						"timestamp": now, "type": "health.backend_disabled", "severity": "warning",
						"serverId": server, "message": message,
					}, 20*1024*1024)
				}
				continue
			}

			state.UnhealthyFrom[server] = 0
			if state.Disabled[server] {
				if state.HealthyFrom[server] == 0 {
					state.HealthyFrom[server] = now
				}
				if now-state.HealthyFrom[server] >= recoveryAfter {
					_ = d.queueCommand("backend", server, "true")
					state.Disabled[server] = false
					state.HealthyFrom[server] = 0
					message := "Automatically restored transfers to " + server + " after stable health"
					d.ledger.recordAlert("backend-auto-recovery", "info", server, message)
					d.appendJSONL(filepath.Join(d.runtimeDir, "events.jsonl"), map[string]any{
						"timestamp": now, "type": "health.backend_recovered", "severity": "info",
						"serverId": server, "message": message,
					}, 20*1024*1024)
				}
			}
		}
		saveHealthActionState(statePath, state)
	}
}

func (d *dashboard) transferTransactionLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if d.ledger == nil {
			continue
		}
		for _, record := range readRotated(filepath.Join(d.runtimeDir, "events.jsonl"), 1000) {
			if textField(record, "type") != "transfer.ticket_consumed" {
				continue
			}
			message := textField(record, "message")
			if match := transferTicketPattern.FindStringSubmatch(message); len(match) == 2 {
				timestamp, _ := numberValue(record["timestamp"])
				d.ledger.updateTransfer(match[1], "proxy_connected", "", int64(timestamp))
			}
		}
		d.ledger.expireTransfers(time.Now().UnixMilli())
	}
}

func (d *dashboard) sessionCleanupLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		if d.ledger != nil {
			d.ledger.cleanupSessions()
		}
	}
}
