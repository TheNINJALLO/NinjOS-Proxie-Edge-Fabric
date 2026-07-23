// Copyright (C) 2026 Ninj-OS contributors.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bufio"
	"context"
	"crypto/hmac"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

//go:embed public/* packet-names.json
var assets embed.FS

type principal struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

type dashboardUser struct {
	Username           string `json:"username"`
	Role               string `json:"role"`
	PasswordHash       string `json:"passwordHash,omitempty"`
	PasswordSalt       string `json:"passwordSalt,omitempty"`
	PasswordIterations int    `json:"passwordIterations,omitempty"`
	TokenHash          string `json:"tokenHash,omitempty"`
	TOTPSecret         string `json:"totpSecret,omitempty"`
	Enabled            bool   `json:"enabled"`
}

type authContextKey string

const principalContextKey authContextKey = "ninjos-principal"

func roleRank(role string) int {
	switch strings.ToLower(role) {
	case "owner":
		return 50
	case "admin":
		return 40
	case "operator":
		return 30
	case "moderator":
		return 20
	case "viewer":
		return 10
	default:
		return 0
	}
}

type presenceEntry struct {
	Key        string `json:"key"`
	XUID       string `json:"xuid,omitempty"`
	PlayerName string `json:"playerName,omitempty"`
	ServerID   string `json:"serverId"`
	Address    string `json:"address,omitempty"`
	SubClient  string `json:"subClientId,omitempty"`
	LastSeen   int64  `json:"lastSeen"`
	FirstSeen  int64  `json:"firstSeen"`
	LastType   string `json:"lastType,omitempty"`
}

type dashboard struct {
	runtimeDir            string
	port                  string
	token                 string
	secret                string
	title                 string
	maxBody               int64
	gameplayLogBytes      int64
	packetNames           map[string]string
	packetCatalog         map[string]any
	discordWebhook        string
	discordBotToken       string
	discordChannel        string
	discordSummary        time.Duration
	transferHost          string
	transferPortStart     int
	transferPortEnd       int
	transferTTL           time.Duration
	transferRequireIP     bool
	transferLogBytes      int64
	usersPath             string
	setupCodePath         string
	setupCode             string
	recoveryToken         string
	companionSecretsPath  string
	companionSourcePath   string
	companionArtifactPath string
	liveConfigPath        string
	configPath            string
	gatewayConfigPath     string
	managedPublicPorts    []int
	auditLogBytes         int64
	presenceTTL           time.Duration
	metricsToken          string
	sessionCoreToken      string
	sessionTTL            time.Duration
	ledger                *ledger
	version               string
	started               time.Time
	fileMu                sync.Mutex
	authMu                sync.Mutex
	replayMu              sync.Mutex
	replaySeen            map[string]int64
	identityMu            sync.Mutex
	identityGrants        map[string]identityGrant
	cpuMu                 sync.Mutex
	lastCPUUsage          uint64
	lastCPUAt             time.Time
	cpuPercent            float64
	eventOffset           int64
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envInt64(name string, fallback int64) int64 {
	value, err := strconv.ParseInt(env(name, strconv.FormatInt(fallback, 10)), 10, 64)
	if err != nil {
		return fallback
	}
	return value
}

func main() {
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "--prepare-config":
			if len(os.Args) != 5 {
				fmt.Fprintln(os.Stderr, "Usage: NinjOSDashboard --prepare-config <edge-fabric.ini> <runtime-dir> <gateway.conf>")
				os.Exit(2)
			}
			if err := prepareUnifiedConfig(os.Args[2], os.Args[3], os.Args[4]); err != nil {
				fmt.Fprintln(os.Stderr, "Configuration error:", err)
				os.Exit(1)
			}
			return
		case "--migrate-legacy":
			if len(os.Args) != 4 {
				fmt.Fprintln(os.Stderr, "Usage: NinjOSDashboard --migrate-legacy <routes.conf> <edge-fabric.ini>")
				os.Exit(2)
			}
			if err := migrateLegacyRoutes(os.Args[2], os.Args[3]); err != nil {
				fmt.Fprintln(os.Stderr, "Migration error:", err)
				os.Exit(1)
			}
			return
		}
	}

	runtimeDir, _ := filepath.Abs(env("RUNTIME_DIR", "runtime"))
	configPath, _ := filepath.Abs(env("EDGE_CONFIG_FILE", filepath.Join("config", "edge-fabric.ini")))
	gatewayConfigPath, _ := filepath.Abs(env("GATEWAY_CONFIG_FILE", "gateway.conf"))
	companionSourcePath, _ := filepath.Abs(env("COMPANION_SOURCE_ARCHIVE", "NinjOS-Endstone-Companion-v3.6.1-GitHub-Clean.zip"))
	d := &dashboard{
		runtimeDir:            runtimeDir,
		port:                  env("DASHBOARD_PORT", "25571"),
		token:                 strings.TrimSpace(os.Getenv("DASHBOARD_TOKEN")),
		secret:                env("COMPANION_SHARED_SECRET", "CHANGE_ME_NOW"),
		title:                 env("DASHBOARD_TITLE", "Ninj-OS Proxie"),
		maxBody:               max64(65536, envInt64("DASHBOARD_MAX_BODY_BYTES", 4*1024*1024)),
		gameplayLogBytes:      max64(1024*1024, envInt64("GAMEPLAY_LOG_MAX_BYTES", 100*1024*1024)),
		discordWebhook:        os.Getenv("DISCORD_WEBHOOK_URL"),
		discordBotToken:       os.Getenv("DISCORD_BOT_TOKEN"),
		discordChannel:        os.Getenv("DISCORD_CHANNEL_ID"),
		discordSummary:        time.Duration(max64(1, envInt64("DISCORD_SUMMARY_MINUTES", 15))) * time.Minute,
		transferHost:          env("TRANSFER_PUBLIC_HOST", "185.83.152.144"),
		transferPortStart:     int(envInt64("TRANSFER_PORT_START", 25600)),
		transferPortEnd:       int(envInt64("TRANSFER_PORT_END", 25619)),
		transferTTL:           time.Duration(max64(5, envInt64("TRANSFER_TICKET_TTL_SECONDS", 20))) * time.Second,
		transferRequireIP:     env("TRANSFER_REQUIRE_SOURCE_IP", "1") != "0",
		transferLogBytes:      max64(1024*1024, envInt64("TRANSFER_LOG_MAX_BYTES", 20*1024*1024)),
		usersPath:             filepath.Join(runtimeDir, "dashboard-users.json"),
		setupCodePath:         filepath.Join(runtimeDir, "FIRST_RUN_SETUP.txt"),
		recoveryToken:         strings.TrimSpace(os.Getenv("DASHBOARD_RECOVERY_TOKEN")),
		companionSecretsPath:  filepath.Join(runtimeDir, "companion-secrets.properties"),
		companionSourcePath:   companionSourcePath,
		companionArtifactPath: filepath.Join(runtimeDir, "companion-artifacts", "ninjos_proxie_companion.so"),
		liveConfigPath:        filepath.Join(runtimeDir, "live-config.properties"),
		configPath:            configPath,
		gatewayConfigPath:     gatewayConfigPath,
		managedPublicPorts:    parsePortList(env("MANAGED_PUBLIC_UDP_PORTS", "25566,25571,25572-25581")),
		auditLogBytes:         max64(1024*1024, envInt64("AUDIT_LOG_MAX_BYTES", 25*1024*1024)),
		presenceTTL:           time.Duration(max64(30, envInt64("PRESENCE_TTL_SECONDS", 120))) * time.Second,
		metricsToken:          strings.TrimSpace(env("METRICS_TOKEN", os.Getenv("DASHBOARD_TOKEN"))),
		sessionCoreToken:      strings.TrimSpace(os.Getenv("SESSION_CORE_TOKEN")),
		sessionTTL:            time.Duration(max64(15, envInt64("DASHBOARD_SESSION_MINUTES", 480))) * time.Minute,
		version:               productVersion,
		started:               time.Now(), lastCPUAt: time.Now(),
		replaySeen:     map[string]int64{},
		identityGrants: map[string]identityGrant{},
	}
	if bytes, err := assets.ReadFile("packet-names.json"); err == nil {
		var catalog struct {
			Source            string            `json:"source"`
			SourceURL         string            `json:"sourceUrl"`
			SourceCommit      string            `json:"sourceCommit"`
			MinecraftVersions []string          `json:"minecraftVersions"`
			ProtocolVersions  []int             `json:"protocolVersions"`
			PacketCount       int               `json:"packetCount"`
			Packets           map[string]string `json:"packets"`
		}
		if json.Unmarshal(bytes, &catalog) == nil {
			d.packetNames = catalog.Packets
			d.packetCatalog = map[string]any{
				"source": catalog.Source, "sourceUrl": catalog.SourceURL,
				"sourceCommit": catalog.SourceCommit, "minecraftVersions": catalog.MinecraftVersions,
				"protocolVersions": catalog.ProtocolVersions, "packetCount": catalog.PacketCount,
			}
		}
	}
	if d.packetNames == nil {
		d.packetNames = map[string]string{}
	}
	if d.packetCatalog == nil {
		d.packetCatalog = map[string]any{"source": "unavailable", "packetCount": 0}
	}
	if d.transferPortEnd < d.transferPortStart {
		d.transferPortStart, d.transferPortEnd = d.transferPortEnd, d.transferPortStart
	}
	if err := os.MkdirAll(d.runtimeDir, 0755); err != nil {
		log.Fatal(err)
	}
	for _, name := range []string{"commands.log", "events.jsonl", "transport-packets.jsonl", "gameplay-packets.jsonl", "transfer-tickets.tsv", "transfer-history.jsonl", "audit.jsonl"} {
		file, _ := os.OpenFile(filepath.Join(d.runtimeDir, name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if file != nil {
			_ = file.Close()
		}
	}
	d.ensureSecurityFiles()
	d.ensureFirstRunSetup()
	if err := ensureUnifiedConfig(d.configPath); err != nil {
		log.Fatal("[Ninj-OS Edge Fabric] Unified configuration initialization failed: ", err)
	}
	if _, err := d.loadTopology(); err != nil {
		log.Fatal("[Ninj-OS Edge Fabric] Backend registry initialization failed: ", err)
	}
	if err := d.initializeControlPlane(); err != nil {
		log.Fatal("[Ninj-OS Edge Fabric] SQLite ledger initialization failed: ", err)
	}
	d.lastCPUUsage = readCPUUsage()

	mux := http.NewServeMux()
	mux.HandleFunc("/health", d.handleHealth)
	mux.HandleFunc("/health/live", d.handleHealth)
	mux.HandleFunc("/health/ready", d.handleReady)
	mux.HandleFunc("/health/backends", d.handleBackendHealth)
	mux.HandleFunc("/version", d.handleVersion)
	mux.HandleFunc("/metrics", d.handleMetrics)
	mux.HandleFunc("/api/setup/status", d.handleSetupStatus)
	mux.HandleFunc("/api/setup", d.handleOwnerSetup)
	mux.HandleFunc("/api/login", d.handleLogin)
	mux.HandleFunc("/api/logout", d.handleLogout)
	mux.HandleFunc("/api/account", d.requireRole("owner", d.handleOwnerAccount))
	mux.HandleFunc("/api/users", d.requireRole("owner", d.handleDashboardUsers))
	mux.HandleFunc("/ingest", d.handleIngest)
	mux.HandleFunc("/transfer", d.handleTransferRequest)
	mux.HandleFunc("/api/session-core/v1/grants", d.handleIdentityGrant)
	mux.HandleFunc("/api/bridge/v1/join/consume", d.handleIdentityConsume)
	mux.HandleFunc("/api/bridge/v1/permissions", d.handlePermissionSnapshot)
	mux.HandleFunc("/api/state", d.requireRole("viewer", d.handleState))
	mux.HandleFunc("/api/whoami", d.requireRole("viewer", d.handleWhoAmI))
	mux.HandleFunc("/api/packets", d.requireRole("viewer", d.handlePackets))
	mux.HandleFunc("/api/events", d.requireRole("viewer", d.handleEvents))
	mux.HandleFunc("/api/presence", d.requireRole("viewer", d.handlePresence))
	mux.HandleFunc("/api/capabilities", d.requireRole("viewer", d.handleCapabilities))
	mux.HandleFunc("/api/profiles", d.requireRole("viewer", d.handleProfiles))
	mux.HandleFunc("/api/profiles/access", d.requireRole("admin", d.handleProfileAccess))
	mux.HandleFunc("/api/history", d.requireRole("viewer", d.handleMetricHistory))
	mux.HandleFunc("/api/alerts", d.requireRole("viewer", d.handleAlerts))
	mux.HandleFunc("/api/transfer-transactions", d.requireRole("viewer", d.handleTransferTransactions))
	mux.HandleFunc("/api/support-bundle", d.requireRole("admin", d.handleSupportBundle))
	mux.HandleFunc("/api/audit", d.requireRole("admin", d.handleAudit))
	mux.HandleFunc("/api/config", d.requireRole("admin", d.handleConfig))
	mux.HandleFunc("/api/unified-config", d.requireRole("admin", d.handleUnifiedConfig))
	mux.HandleFunc("/api/security-sources", d.requireRole("admin", d.handleSecuritySources))
	mux.HandleFunc("/api/settings", d.requireRole("admin", d.handleManagedSettings))
	mux.HandleFunc("/api/secrets", d.requireRole("admin", d.handleManagedSecrets))
	mux.HandleFunc("/api/companion-manager", d.requireRole("admin", d.handleCompanionManager))
	mux.HandleFunc("/api/companion-download", d.requireRole("admin", d.handleCompanionDownload))
	mux.HandleFunc("/api/backend-registry", d.requireRole("admin", d.handleBackendRegistry))
	mux.HandleFunc("/api/backend-registry/settings", d.requireRole("admin", d.handleTopologySettings))
	mux.HandleFunc("/api/backend-registry/test", d.requireRole("admin", d.handleBackendTest))
	mux.HandleFunc("/api/protection-profiles", d.requireRole("admin", d.handleProtectionProfiles))
	mux.HandleFunc("/api/config/rollback", d.requireRole("admin", d.handleConfigRollback))
	mux.HandleFunc("/api/control", d.requireRole("operator", d.handleControl))
	mux.HandleFunc("/api/transfers", d.requireRole("viewer", d.handleTransfersAPI))
	public, _ := fs.Sub(assets, "public")
	mux.Handle("/", http.FileServer(http.FS(public)))

	go d.cpuSampler()
	go d.discordWatcher()
	go d.discordSummaryLoop()
	d.startControlPlaneLoops()

	log.Printf("[%s v%s] Dashboard listening on 0.0.0.0:%s/TCP", productName, productVersion, d.port)
	if d.secret == "CHANGE_ME_NOW" {
		log.Printf("[Ninj-OS Dashboard] WARNING: change COMPANION_SHARED_SECRET")
	}
	server := &http.Server{Addr: ":" + d.port, Handler: securityHeaders(mux), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second}
	log.Fatal(server.ListenAndServe())
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'")
		next.ServeHTTP(w, r)
	})
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func (d *dashboard) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"ok": true, "version": d.version, "timestamp": time.Now().UnixMilli()})
}
func (d *dashboard) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"name":           productName,
		"version":        d.version,
		"engine":         productEngine,
		"implementation": productImplementation,
		"reference":      productReference,
		"reference_url":  productReferenceURL,
	})
}
func (d *dashboard) handleReady(w http.ResponseWriter, r *http.Request) {
	gateway := safeMap(filepath.Join(d.runtimeDir, "gateway-state.json"))
	ready := len(gateway) > 0
	status := 200
	if !ready {
		status = 503
	}
	writeJSON(w, status, map[string]any{"ready": ready, "gateway": gateway["timestamp"], "timestamp": time.Now().UnixMilli()})
}
func (d *dashboard) handleBackendHealth(w http.ResponseWriter, r *http.Request) {
	gateway := d.combinedRuntimeState()
	writeJSON(w, 200, map[string]any{"backends": gateway["backends"], "timestamp": time.Now().UnixMilli()})
}
func (d *dashboard) handleWhoAmI(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, principalFromRequest(r))
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (d *dashboard) authenticate(r *http.Request) (principal, bool) {
	provided := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if provided == "" {
		provided = r.URL.Query().Get("token")
	}
	if provided == "" {
		return principal{}, false
	}
	hash := tokenHash(provided)
	if d.ledger != nil {
		if actor, ok := d.ledger.session(hash); ok {
			return actor, true
		}
	}
	for _, user := range d.users() {
		if user.Enabled && hmac.Equal([]byte(hash), []byte(strings.ToLower(user.TokenHash))) {
			return principal{Username: user.Username, Role: user.Role}, true
		}
	}
	return principal{}, false
}

func (d *dashboard) requireRole(required string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := d.authenticate(r)
		if !ok {
			writeJSON(w, 401, map[string]string{"error": "Unauthorized"})
			return
		}
		if roleRank(actor.Role) < roleRank(required) {
			d.appendAudit(r, actor, "access.denied", "denied", map[string]any{"path": r.URL.Path, "requiredRole": required})
			writeJSON(w, 403, map[string]string{"error": "Insufficient role"})
			return
		}
		ctx := context.WithValue(r.Context(), principalContextKey, actor)
		next(w, r.WithContext(ctx))
	}
}

func principalFromRequest(r *http.Request) principal {
	if value, ok := r.Context().Value(principalContextKey).(principal); ok {
		return value
	}
	return principal{Username: "unknown", Role: "unknown"}
}

func clientHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func (d *dashboard) appendAudit(r *http.Request, actor principal, action, result string, details any) {
	timestamp := time.Now().UnixMilli()
	remoteIP := clientHost(r)
	d.appendJSONL(filepath.Join(d.runtimeDir, "audit.jsonl"), map[string]any{
		"timestamp": timestamp, "actor": actor.Username, "role": actor.Role,
		"action": action, "result": result, "remoteIp": remoteIP, "details": details,
	}, d.auditLogBytes)
	if d.ledger != nil {
		d.ledger.recordAudit(timestamp, actor.Username, actor.Role, action, result, remoteIP, details)
	}
}

func safeJSON(path string, fallback any) any {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return fallback
	}
	var value any
	if json.Unmarshal(bytes, &value) != nil {
		return fallback
	}
	return value
}
func safeMap(path string) map[string]any {
	value := safeJSON(path, map[string]any{})
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}
func safeSlice(path string) []any {
	value := safeJSON(path, []any{})
	if s, ok := value.([]any); ok {
		return s
	}
	return []any{}
}

// combinedRuntimeState presents transparent gateway and Full Proxy Session Core
// listeners through the same backend-health model. Session Core owns only
// full_proxy routes, so a backend name can safely replace an older gateway row.
func (d *dashboard) combinedRuntimeState() map[string]any {
	gateway := safeMap(filepath.Join(d.runtimeDir, "gateway-state.json"))
	combined := cloneMap(gateway)
	byID := map[string]map[string]any{}
	order := []string{}
	merge := func(state map[string]any) {
		rawBackends, _ := state["backends"].([]any)
		for _, raw := range rawBackends {
			backend, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			id := normalizeCompanionServerID(textField(backend, "name"))
			if id == "" {
				continue
			}
			if _, exists := byID[id]; !exists {
				order = append(order, id)
			}
			byID[id] = backend
		}
	}
	merge(gateway)
	sessionCore := safeMap(filepath.Join(d.runtimeDir, "session-core-state.json"))
	merge(sessionCore)
	if packs, ok := sessionCore["protocolPacks"].([]any); ok {
		combined["protocolPacks"] = packs
	}
	backends := make([]any, 0, len(order))
	for _, id := range order {
		backends = append(backends, byID[id])
	}
	combined["backends"] = backends
	if timestamp, ok := number(sessionCore["timestamp"]); ok {
		if current, exists := number(combined["timestamp"]); !exists || timestamp > current {
			combined["timestamp"] = timestamp
		}
	}
	return combined
}

func safeFileID(value string) string {
	var builder strings.Builder
	for _, char := range strings.ToLower(value) {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-' || char == '_' {
			builder.WriteRune(char)
		}
	}
	if builder.Len() == 0 {
		return "unknown"
	}
	return builder.String()
}

func (d *dashboard) companionStates() []map[string]any {
	paths, _ := filepath.Glob(filepath.Join(d.runtimeDir, "companion-state-*.json"))
	states := make([]map[string]any, 0, len(paths))
	now := time.Now().UnixMilli()
	for _, path := range paths {
		state := safeMap(path)
		timestamp, _ := number(state["timestamp"])
		connected := timestamp > 0 && now-int64(timestamp) <= 15000
		state["connected"] = connected
		status := "offline"
		if connected {
			status = "healthy"
			metrics, _ := state["metrics"].(map[string]any)
			// uploadFailures is a lifetime counter. A fresh signed report proves
			// transport is currently working, so old failures must not keep a
			// recovered companion permanently degraded.
			if queue, ok := number(metrics["queueDepth"]); ok && queue > 10000 {
				status = "degraded"
			}
		}
		state["health"] = status
		states = append(states, state)
	}
	sort.Slice(states, func(i, j int) bool { return textField(states[i], "serverId") < textField(states[j], "serverId") })
	return states
}

func cloneMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source)+12)
	for key, value := range source {
		result[key] = value
	}
	return result
}

func gatewayBackendMap(gateway map[string]any) map[string]map[string]any {
	result := map[string]map[string]any{}
	rawBackends, _ := gateway["backends"].([]any)
	for _, raw := range rawBackends {
		backend, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id := normalizeCompanionServerID(textField(backend, "name"))
		if id != "" {
			result[id] = backend
		}
	}
	return result
}

// companionFleet merges configured backends with the latest report from each
// Endstone companion. Configured servers remain visible even before their first
// report, which lets operators see the whole network rather than only the first
// companion that happened to connect.
func (d *dashboard) companionFleet(gateway map[string]any, states []map[string]any) []map[string]any {
	now := time.Now().UnixMilli()
	stateByID := map[string]map[string]any{}
	for _, state := range states {
		id := normalizeCompanionServerID(textField(state, "serverId"))
		if id != "" {
			stateByID[id] = state
		}
	}

	gatewayByID := gatewayBackendMap(gateway)
	fleet := []map[string]any{}
	seen := map[string]bool{}
	topology, err := d.loadTopology()
	if err == nil {
		for _, backend := range topology.Backends {
			id := normalizeCompanionServerID(backend.ID)
			item := map[string]any{
				"serverId":         id,
				"displayName":      valueOr(map[string]string{"name": backend.DisplayName}, "name", backend.ID),
				"configured":       true,
				"backendEnabled":   backend.Enabled,
				"backendHost":      backend.Host,
				"backendPort":      backend.BackendPort,
				"publicPort":       backend.PublicPort,
				"connected":        false,
				"health":           "offline",
				"reportStatus":     "never",
				"ageSeconds":       nil,
				"secretConfigured": false,
				"metrics":          map[string]any{},
			}
			if backend.DisplayName == "" {
				item["displayName"] = backend.ID
			}
			if state, ok := stateByID[id]; ok {
				for key, value := range cloneMap(state) {
					item[key] = value
				}
			}
			if gatewayBackend, ok := gatewayByID[id]; ok {
				item["gatewayHealthy"] = gatewayBackend["healthy"]
				item["gatewayEnabled"] = gatewayBackend["enabled"]
				item["gatewayLatencyMs"] = gatewayBackend["latencyMs"]
				item["activeSessions"] = gatewayBackend["activeSessions"]
			}
			secret := d.companionSecret(id)
			item["secretConfigured"] = secret != "" && secret != "CHANGE_ME_NOW"
			if timestamp, ok := number(item["timestamp"]); ok && timestamp > 0 {
				age := max(int64(0), now-int64(timestamp)) / 1000
				item["ageSeconds"] = age
				if connected, _ := item["connected"].(bool); connected {
					item["reportStatus"] = "live"
				} else {
					item["reportStatus"] = "stale"
				}
			}
			fleet = append(fleet, item)
			seen[id] = true
		}
	}

	for _, state := range states {
		id := normalizeCompanionServerID(textField(state, "serverId"))
		if id == "" || seen[id] {
			continue
		}
		item := cloneMap(state)
		item["serverId"] = id
		item["displayName"] = id
		item["configured"] = false
		item["reportStatus"] = "orphaned"
		if timestamp, ok := number(item["timestamp"]); ok && timestamp > 0 {
			item["ageSeconds"] = max(int64(0), now-int64(timestamp)) / 1000
		}
		fleet = append(fleet, item)
	}

	sort.Slice(fleet, func(i, j int) bool {
		return textField(fleet[i], "displayName") < textField(fleet[j], "displayName")
	})
	return fleet
}

func (d *dashboard) presenceRecords() []presenceEntry {
	bytes, err := os.ReadFile(filepath.Join(d.runtimeDir, "presence.json"))
	if err != nil {
		return []presenceEntry{}
	}
	var values map[string]presenceEntry
	if json.Unmarshal(bytes, &values) != nil {
		return []presenceEntry{}
	}
	result := make([]presenceEntry, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].LastSeen > result[j].LastSeen })
	return result
}

func (d *dashboard) presenceSummary() map[string]any {
	now := time.Now().UnixMilli()
	online := 0
	byServer := map[string]int{}
	for _, item := range d.presenceRecords() {
		if now-item.LastSeen <= d.presenceTTL.Milliseconds() {
			online++
			byServer[item.ServerID]++
		}
	}
	return map[string]any{"online": online, "tracked": len(d.presenceRecords()), "byServer": byServer, "ttlSeconds": int(d.presenceTTL.Seconds())}
}

func (d *dashboard) handleState(w http.ResponseWriter, r *http.Request) {
	gateway := d.combinedRuntimeState()
	reportedCompanions := d.companionStates()
	companions := d.companionFleet(gateway, reportedCompanions)
	companion := map[string]any{}
	for _, item := range companions {
		if textField(item, "serverId") == "kingdom" {
			companion = item
			break
		}
	}
	if len(companion) == 0 && len(companions) > 0 {
		companion = companions[0]
	}
	activeTickets, _ := d.loadTransferTickets()
	writeJSON(w, 200, map[string]any{
		"title": d.title, "version": d.version, "gateway": gateway, "companion": companion, "companions": companions,
		"presence": d.presenceSummary(), "principal": principalFromRequest(r),
		"transferBroker": func() map[string]any {
			start, end := d.transferPoolRange()
			return map[string]any{"host": d.transferHost, "portStart": start, "portEnd": end, "availablePorts": d.availableTransferPorts(), "ttlSeconds": int(d.transferTTL.Seconds()), "requireSourceIp": d.transferRequireIP, "activeTickets": len(activeTickets)}
		}(),
		"sessions": safeSlice(filepath.Join(d.runtimeDir, "sessions.json")),
		"system":   d.systemMetrics(),
		"discord":  map[string]any{"enabled": d.discordWebhook != "" || (d.discordBotToken != "" && d.discordChannel != ""), "mode": d.discordMode()},
		"security": map[string]any{"passwordLogin": true, "setupRequired": d.setupRequired(), "companionSecretDefault": d.secret == "CHANGE_ME_NOW", "rbac": true, "perServerSecrets": true, "sessions": true, "totp": true},
		"management": map[string]any{
			"sqliteLedger":           d.ledger != nil,
			"transferTransactions":   true,
			"historicalMetrics":      true,
			"healthActions":          true,
			"supportBundles":         true,
			"protectionProfiles":     true,
			"structuredSettings":     true,
			"secretVault":            true,
			"companionDownloads":     true,
			"companionArtifactStore": true,
		},
	})
}
func number(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case int64:
		return float64(v), true
	case json.Number:
		n, e := v.Float64()
		return n, e == nil
	}
	return 0, false
}

func tailJSONL(path string, limit int, maxRead int64) []map[string]any {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return nil
	}
	start := stat.Size() - maxRead
	if start < 0 {
		start = 0
	}
	_, _ = file.Seek(start, io.SeekStart)
	reader := bufio.NewReader(file)
	if start > 0 {
		_, _ = reader.ReadString('\n')
	}
	lines := make([]map[string]any, 0, limit*2)
	scanner := bufio.NewScanner(reader)
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, 8*1024*1024)
	for scanner.Scan() {
		var record map[string]any
		if json.Unmarshal(scanner.Bytes(), &record) == nil {
			lines = append(lines, record)
			if len(lines) > limit*3 {
				lines = lines[len(lines)-limit*2:]
			}
		}
	}
	result := make([]map[string]any, 0, min(limit, len(lines)))
	for i := len(lines) - 1; i >= 0 && len(result) < limit; i-- {
		result = append(result, lines[i])
	}
	return result
}
func readRotated(path string, limit int) []map[string]any {
	a := tailJSONL(path, limit, 12*1024*1024)
	if len(a) >= limit {
		return a
	}
	return append(a, tailJSONL(path+".1", limit-len(a), 12*1024*1024)...)
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func textField(record map[string]any, key string) string {
	if v, ok := record[key]; ok {
		return fmt.Sprint(v)
	}
	return ""
}

func (d *dashboard) enrichPacketName(record map[string]any) {
	id := textField(record, "packetId")
	name := d.packetNames[id]
	if id == "" || name == "" {
		return
	}
	existing := strings.TrimSpace(textField(record, "packetName"))
	if existing != "" && !strings.HasPrefix(existing, "Unknown packet #") && existing != "Packet #"+id && existing != name {
		record["decodedPacketName"] = existing
	}
	record["packetName"] = name
	record["packetNameSource"] = "Mojang/bedrock-protocol-docs"
}

func containsFold(value, needle string) bool {
	return strings.Contains(strings.ToLower(value), strings.ToLower(needle))
}

func (d *dashboard) handlePackets(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 {
		limit = 500
	}
	if limit > 5000 {
		limit = 5000
	}
	layer := r.URL.Query().Get("layer")
	records := make([]map[string]any, 0, limit*2)
	if layer == "" || layer == "all" || layer == "transport" {
		records = append(records, readRotated(filepath.Join(d.runtimeDir, "transport-packets.jsonl"), limit)...)
	}
	if layer == "" || layer == "all" || layer == "gameplay" {
		records = append(records, readRotated(filepath.Join(d.runtimeDir, "gameplay-packets.jsonl"), limit)...)
	}
	if layer == "" || layer == "all" || layer == "protocol" {
		paths, _ := filepath.Glob(filepath.Join(d.runtimeDir, "protocol-observations", "*", "*.jsonl"))
		for _, path := range paths {
			records = append(records, readRotated(path, limit)...)
		}
	}
	sort.SliceStable(records, func(i, j int) bool {
		a, _ := number(records[i]["timestamp"])
		b, _ := number(records[j]["timestamp"])
		return a > b
	})
	direction := r.URL.Query().Get("direction")
	player := r.URL.Query().Get("player")
	packetID := r.URL.Query().Get("packetId")
	query := r.URL.Query().Get("q")
	tier := r.URL.Query().Get("tier")
	recordID := r.URL.Query().Get("recordId")
	includeDetails := r.URL.Query().Get("details") == "1"
	filtered := make([]map[string]any, 0, limit)
	tierCounts := map[string]int{}
	for _, record := range records {
		d.enrichPacketName(record)
		if direction != "" && textField(record, "direction") != direction {
			continue
		}
		if recordID != "" && textField(record, "recordId") != recordID {
			continue
		}
		if packetID != "" && textField(record, "packetId") != packetID {
			continue
		}
		if tier != "" {
			matched := false
			if tiers, ok := record["captureTiers"].([]any); ok {
				for _, value := range tiers {
					if fmt.Sprint(value) == tier {
						matched = true
						break
					}
				}
			}
			if !matched {
				continue
			}
		}
		if player != "" {
			hay := textField(record, "playerName") + " " + textField(record, "xuid") + " " + textField(record, "client") + " " + textField(record, "ip")
			if !containsFold(hay, player) {
				continue
			}
		}
		if query != "" {
			hay := textField(record, "packetName") + " " + textField(record, "raknetName") + " " + textField(record, "action") + " " + textField(record, "backend")
			if !containsFold(hay, query) {
				continue
			}
		}
		view := record
		if !includeDetails && textField(record, "layer") == "protocol" {
			view = make(map[string]any, len(record))
			for key, value := range record {
				view[key] = value
			}
			for _, key := range []string{"decoded", "translatedDecoded", "decodeError"} {
				if _, exists := view[key]; exists {
					view["hasDetails"] = true
					delete(view, key)
				}
			}
			if rawWire, exists := view["wire"].(map[string]any); exists {
				wire := make(map[string]any, len(rawWire))
				for key, value := range rawWire {
					if key != "data" {
						wire[key] = value
					}
				}
				view["wire"] = wire
				view["hasDetails"] = true
			}
		}
		filtered = append(filtered, view)
		if tiers, ok := record["captureTiers"].([]any); ok {
			for _, value := range tiers {
				tierCounts[fmt.Sprint(value)]++
			}
		}
		if len(filtered) >= limit {
			break
		}
	}
	writeJSON(w, 200, map[string]any{"records": filtered, "count": len(filtered), "tiers": tierCounts, "catalog": d.packetCatalog})
}
func (d *dashboard) handleEvents(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 {
		limit = 300
	}
	if limit > 2000 {
		limit = 2000
	}
	writeJSON(w, 200, map[string]any{"records": readRotated(filepath.Join(d.runtimeDir, "events.jsonl"), limit)})
}

type transferTicket struct {
	ID           string `json:"ticketId"`
	Port         int    `json:"port"`
	Destination  string `json:"destination"`
	SourceIP     string `json:"sourceIp"`
	XUID         string `json:"xuid"`
	PlayerName   string `json:"playerName"`
	SourceServer string `json:"sourceServer"`
	ExpiresAt    int64  `json:"expiresAt"`
	CreatedAt    int64  `json:"createdAt"`
}

type transferRequest struct {
	Destination  string `json:"destination"`
	SourceIP     string `json:"sourceIp"`
	SourcePort   int    `json:"sourcePort"`
	XUID         string `json:"xuid"`
	PlayerName   string `json:"playerName"`
	SourceServer string `json:"sourceServer"`
}

func sanitizeTicketField(value string) string {
	value = strings.ReplaceAll(value, "\t", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	return strings.TrimSpace(value)
}

func (d *dashboard) transferTicketPath() string {
	return filepath.Join(d.runtimeDir, "transfer-tickets.tsv")
}

func (d *dashboard) loadTransferTickets() ([]transferTicket, error) {
	bytes, err := os.ReadFile(d.transferTicketPath())
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	now := time.Now().UnixMilli()
	tickets := make([]transferTicket, 0)
	for _, line := range strings.Split(string(bytes), "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 9 {
			continue
		}
		port, portErr := strconv.Atoi(fields[1])
		expiresAt, expiresErr := strconv.ParseInt(fields[7], 10, 64)
		createdAt, createdErr := strconv.ParseInt(fields[8], 10, 64)
		if portErr != nil || expiresErr != nil || createdErr != nil || expiresAt <= now {
			continue
		}
		tickets = append(tickets, transferTicket{
			ID: fields[0], Port: port, Destination: fields[2], SourceIP: fields[3],
			XUID: fields[4], PlayerName: fields[5], SourceServer: fields[6],
			ExpiresAt: expiresAt, CreatedAt: createdAt,
		})
	}
	return tickets, nil
}

func (d *dashboard) writeTransferTickets(tickets []transferTicket) error {
	var builder strings.Builder
	builder.WriteString("# ticketId\\tport\\tdestination\\tsourceIp\\txuid\\tplayer\\tsourceServer\\texpiresAt\\tcreatedAt\n")
	for _, ticket := range tickets {
		fmt.Fprintf(&builder, "%s\t%d\t%s\t%s\t%s\t%s\t%s\t%d\t%d\n",
			sanitizeTicketField(ticket.ID), ticket.Port,
			sanitizeTicketField(ticket.Destination), sanitizeTicketField(ticket.SourceIP),
			sanitizeTicketField(ticket.XUID), sanitizeTicketField(ticket.PlayerName),
			sanitizeTicketField(ticket.SourceServer), ticket.ExpiresAt, ticket.CreatedAt)
	}
	temporary := d.transferTicketPath() + ".tmp"
	if err := os.WriteFile(temporary, []byte(builder.String()), 0644); err != nil {
		return err
	}
	return os.Rename(temporary, d.transferTicketPath())
}

func randomTicketID() (string, error) {
	value := make([]byte, 16)
	if _, err := cryptorand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func (d *dashboard) backendAvailable(destination string) (bool, string) {
	state := d.combinedRuntimeState()
	backends, _ := state["backends"].([]any)
	for _, raw := range backends {
		backend, ok := raw.(map[string]any)
		if !ok || textField(backend, "name") != destination {
			continue
		}
		enabled, _ := backend["enabled"].(bool)
		healthy, _ := backend["healthy"].(bool)
		if !enabled {
			return false, "Destination backend is disabled"
		}
		if !healthy {
			return false, "Destination backend is unhealthy"
		}
		return true, ""
	}
	return false, "Unknown destination backend"
}

func (d *dashboard) resolveGatewaySourceIP(sourceServer string, sourcePort int) string {
	if sourcePort <= 0 {
		return ""
	}
	sessions := safeSlice(filepath.Join(d.runtimeDir, "sessions.json"))
	for _, raw := range sessions {
		session, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		portValue, ok := number(session["upstreamLocalPort"])
		if !ok || int(portValue) != sourcePort {
			continue
		}
		if sourceServer != "" && textField(session, "backend") != sourceServer {
			continue
		}
		return textField(session, "ip")
	}
	return ""
}

func (d *dashboard) createTransferTicket(request transferRequest) (transferTicket, error) {
	request.Destination = sanitizeTicketField(request.Destination)
	request.SourceIP = sanitizeTicketField(request.SourceIP)
	request.XUID = sanitizeTicketField(request.XUID)
	request.PlayerName = sanitizeTicketField(request.PlayerName)
	request.SourceServer = sanitizeTicketField(request.SourceServer)
	if resolved := d.resolveGatewaySourceIP(request.SourceServer, request.SourcePort); resolved != "" {
		request.SourceIP = resolved
	}
	if request.Destination == "" {
		return transferTicket{}, errors.New("Destination is required")
	}
	if d.transferRequireIP && request.SourceIP == "" {
		return transferTicket{}, errors.New("Source IP is required")
	}
	if ok, reason := d.backendAvailable(request.Destination); !ok {
		return transferTicket{}, errors.New(reason)
	}
	if d.ledger != nil {
		if ok, reason := d.ledger.profileAccess(request.XUID, request.Destination); !ok {
			return transferTicket{}, errors.New(reason)
		}
	}

	d.fileMu.Lock()
	defer d.fileMu.Unlock()

	tickets, err := d.loadTransferTickets()
	if err != nil {
		return transferTicket{}, err
	}
	used := make(map[int]bool, len(tickets))
	for _, ticket := range tickets {
		used[ticket.Port] = true
	}
	assigned := map[int]string{}
	if topology, topologyErr := d.loadTopology(); topologyErr == nil {
		assigned = staticPublicPorts(topology)
	}
	transferStart, transferEnd := d.transferPoolRange()
	selectedPort := 0
	for port := transferStart; port <= transferEnd; port++ {
		if used[port] {
			continue
		}
		if _, staticRoute := assigned[port]; staticRoute {
			continue
		}
		selectedPort = port
		break
	}
	if selectedPort == 0 {
		return transferTicket{}, errors.New("No transfer ports are currently available")
	}
	id, err := randomTicketID()
	if err != nil {
		return transferTicket{}, err
	}
	now := time.Now().UnixMilli()
	ticket := transferTicket{
		ID: id, Port: selectedPort, Destination: request.Destination,
		SourceIP: request.SourceIP, XUID: request.XUID, PlayerName: request.PlayerName,
		SourceServer: request.SourceServer, CreatedAt: now,
		ExpiresAt: now + d.transferTTL.Milliseconds(),
	}
	tickets = append(tickets, ticket)
	if err := d.writeTransferTickets(tickets); err != nil {
		return transferTicket{}, err
	}
	history := map[string]any{
		"timestamp": now, "type": "transfer.ticket_created", "severity": "info",
		"ticketId": ticket.ID, "port": ticket.Port, "destination": ticket.Destination,
		"sourceIp": ticket.SourceIP, "xuid": ticket.XUID, "player": ticket.PlayerName,
		"sourceServer": ticket.SourceServer, "expiresAt": ticket.ExpiresAt,
		"message": fmt.Sprintf("Transfer ticket created for %s: %s -> %s via %s:%d", ticket.PlayerName, ticket.SourceServer, ticket.Destination, d.transferHost, ticket.Port),
	}
	bytes, _ := json.Marshal(history)
	bytes = append(bytes, '\n')
	_ = appendFile(filepath.Join(d.runtimeDir, "transfer-history.jsonl"), bytes)
	_ = appendFile(filepath.Join(d.runtimeDir, "events.jsonl"), bytes)
	if d.ledger != nil {
		d.ledger.createTransfer(ticket)
	}
	return ticket, nil
}

func normalizeCompanionServerID(serverID string) string {
	return strings.ToLower(strings.TrimSpace(serverID))
}

func (d *dashboard) companionSecret(serverID string) string {
	serverID = normalizeCompanionServerID(serverID)
	values := map[string]string{}
	bytes, err := os.ReadFile(d.companionSecretsPath)
	if err == nil {
		for _, line := range strings.Split(string(bytes), "\n") {
			line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				values[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			}
		}
	}
	if secret := values[serverID]; secret != "" {
		return secret
	}
	if secret := values["default"]; secret != "" {
		return secret
	}
	return d.secret
}

func (d *dashboard) verifySignedBody(r *http.Request, body []byte) error {
	timestamp := r.Header.Get("X-NinjOS-Timestamp")
	signature := r.Header.Get("X-NinjOS-Signature")
	serverID := normalizeCompanionServerID(r.Header.Get("X-NinjOS-Server"))
	millis, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || abs64(time.Now().UnixMilli()-millis) > 60000 || serverID == "" {
		return errors.New("Unauthorized")
	}
	mac := hmac.New(sha256.New, []byte(d.companionSecret(serverID)))
	_, _ = mac.Write([]byte(timestamp + "\n"))
	_, _ = mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(signature), []byte(expected)) {
		return errors.New("Unauthorized")
	}
	if !d.acceptReplayKey(signature, time.Now().UnixMilli()) {
		return errors.New("Replay rejected")
	}
	return nil
}

func (d *dashboard) handleTransferRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]string{"error": "Method not allowed"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 65536))
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "Invalid request"})
		return
	}
	if err := d.verifySignedBody(r, body); err != nil {
		status := 401
		if err.Error() == "Replay rejected" {
			status = 409
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	var request transferRequest
	if json.Unmarshal(body, &request) != nil {
		writeJSON(w, 400, map[string]string{"error": "Invalid JSON"})
		return
	}
	headerServer := normalizeCompanionServerID(r.Header.Get("X-NinjOS-Server"))
	request.SourceServer = normalizeCompanionServerID(request.SourceServer)
	if request.SourceServer == "" {
		request.SourceServer = headerServer
	}
	if request.SourceServer != headerServer {
		writeJSON(w, 401, map[string]string{"error": "Source server does not match signed companion identity"})
		return
	}
	ticket, err := d.createTransferTicket(request)
	if err != nil {
		writeJSON(w, 409, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{
		"ticketId": ticket.ID, "host": d.transferHost, "port": ticket.Port,
		"destination": ticket.Destination, "expiresAt": ticket.ExpiresAt,
		"expiresInSeconds": int(d.transferTTL.Seconds()),
	})
}

func (d *dashboard) handleTransfersAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		actor := principalFromRequest(r)
		if roleRank(actor.Role) < roleRank("operator") {
			writeJSON(w, 403, map[string]string{"error": "Operator role required"})
			return
		}
		var request transferRequest
		if json.NewDecoder(io.LimitReader(r.Body, 65536)).Decode(&request) != nil {
			writeJSON(w, 400, map[string]string{"error": "Invalid JSON"})
			return
		}
		ticket, err := d.createTransferTicket(request)
		if err != nil {
			writeJSON(w, 409, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 201, map[string]any{"ticket": ticket, "host": d.transferHost})
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, 405, map[string]string{"error": "Method not allowed"})
		return
	}
	d.fileMu.Lock()
	tickets, err := d.loadTransferTickets()
	if err == nil {
		_ = d.writeTransferTickets(tickets)
	}
	d.fileMu.Unlock()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	history := readRotated(filepath.Join(d.runtimeDir, "transfer-history.jsonl"), 500)
	transferStart, transferEnd := d.transferPoolRange()
	writeJSON(w, 200, map[string]any{
		"host": d.transferHost, "portStart": transferStart,
		"portEnd": transferEnd, "availablePorts": d.availableTransferPorts(),
		"ttlSeconds": int(d.transferTTL.Seconds()),
		"active":     tickets, "history": history,
	})
}

func sanitizeCommand(value string) string {
	value = strings.ReplaceAll(value, "|", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	return strings.TrimSpace(value)
}
func (d *dashboard) handleControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]string{"error": "Method not allowed"})
		return
	}
	var body struct{ Command, Argument, Argument2 string }
	if json.NewDecoder(io.LimitReader(r.Body, 65536)).Decode(&body) != nil {
		writeJSON(w, 400, map[string]string{"error": "Invalid JSON"})
		return
	}
	allowed := map[string]bool{"maintenance": true, "drain": true, "ban": true, "unban": true, "backend": true, "routing": true, "reload": true, "topology_restart": true, "service_restart": true, "risk_reset": true}
	if !allowed[body.Command] {
		writeJSON(w, 400, map[string]string{"error": "Unsupported command"})
		return
	}
	actor := principalFromRequest(r)
	d.fileMu.Lock()
	err := d.queueCommand(body.Command, body.Argument, body.Argument2)
	d.fileMu.Unlock()
	if err != nil {
		d.appendAudit(r, actor, "control."+body.Command, "failed", map[string]any{"error": err.Error()})
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	d.appendJSONL(filepath.Join(d.runtimeDir, "events.jsonl"), map[string]any{"timestamp": time.Now().UnixMilli(), "type": "dashboard.command", "severity": "info", "message": strings.TrimSpace(body.Command + " " + body.Argument + " " + body.Argument2), "actor": actor.Username}, 20*1024*1024)
	d.appendAudit(r, actor, "control."+body.Command, "queued", map[string]any{"argument": body.Argument, "argument2": body.Argument2})
	writeJSON(w, 202, map[string]bool{"queued": true})
}

func appendFile(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(data)
	return err
}
func (d *dashboard) appendJSONL(path string, value any, maxBytes int64) {
	d.fileMu.Lock()
	defer d.fileMu.Unlock()
	if stat, err := os.Stat(path); err == nil && stat.Size() >= maxBytes {
		_ = os.Remove(path + ".1")
		_ = os.Rename(path, path+".1")
	}
	bytes, _ := json.Marshal(value)
	bytes = append(bytes, '\n')
	_ = appendFile(path, bytes)
}

func (d *dashboard) acceptReplayKey(signature string, nowMillis int64) bool {
	d.replayMu.Lock()
	defer d.replayMu.Unlock()
	for key, seenAt := range d.replaySeen {
		if nowMillis-seenAt > 120000 {
			delete(d.replaySeen, key)
		}
	}
	if _, exists := d.replaySeen[signature]; exists {
		return false
	}
	d.replaySeen[signature] = nowMillis
	return true
}

func (d *dashboard) handleIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]string{"error": "Method not allowed"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, d.maxBody+1))
	if err != nil || int64(len(body)) > d.maxBody {
		writeJSON(w, 413, map[string]string{"error": "Request body is too large"})
		return
	}
	if err := d.verifySignedBody(r, body); err != nil {
		status := 401
		if err.Error() == "Replay rejected" {
			status = 409
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	var payload struct {
		ServerID string           `json:"serverId"`
		Records  []map[string]any `json:"records"`
	}
	if json.Unmarshal(body, &payload) != nil {
		writeJSON(w, 400, map[string]string{"error": "Invalid JSON"})
		return
	}
	headerServer := normalizeCompanionServerID(r.Header.Get("X-NinjOS-Server"))
	payload.ServerID = normalizeCompanionServerID(payload.ServerID)
	if payload.ServerID == "" {
		payload.ServerID = headerServer
	}
	if payload.ServerID == "" || payload.ServerID != headerServer {
		writeJSON(w, 401, map[string]string{"error": "Server identity mismatch"})
		return
	}
	accepted := 0
	var metrics map[string]any
	presenceUpdates := make([]map[string]any, 0)
	for _, record := range payload.Records {
		record["serverId"] = payload.ServerID
		record["receivedAt"] = time.Now().UnixMilli()
		switch textField(record, "type") {
		case "packet":
			record["layer"] = "gameplay"
			if textField(record, "xuid") != "" || textField(record, "playerName") != "" {
				presenceUpdates = append(presenceUpdates, record)
			}
			d.enrichPacketName(record)
			if textField(record, "packetName") == "" {
				record["packetName"] = "Packet #" + textField(record, "packetId")
			}
			d.appendJSONL(filepath.Join(d.runtimeDir, "gameplay-packets.jsonl"), record, d.gameplayLogBytes)
			accepted++
		case "metrics":
			metrics = record
			if players, ok := record["players"].([]any); ok {
				for _, raw := range players {
					if player, ok := raw.(map[string]any); ok {
						presenceUpdates = append(presenceUpdates, player)
					}
				}
			}
			accepted++
		case "event", "alert":
			d.appendJSONL(filepath.Join(d.runtimeDir, "events.jsonl"), map[string]any{"timestamp": record["timestamp"], "type": firstNonempty(textField(record, "eventType"), "companion."+textField(record, "type")), "severity": firstNonempty(textField(record, "severity"), "info"), "message": textField(record, "message"), "serverId": payload.ServerID, "player": textField(record, "player"), "xuid": textField(record, "xuid")}, 20*1024*1024)
			accepted++
		}
	}
	if len(presenceUpdates) > 0 {
		d.updatePresence(payload.ServerID, presenceUpdates)
		d.recordPresenceProfiles(payload.ServerID, presenceUpdates)
	}
	if metrics != nil {
		state := map[string]any{"timestamp": time.Now().UnixMilli(), "serverId": payload.ServerID, "metrics": metrics, "connected": true, "recordsAccepted": accepted}
		bytes, _ := json.MarshalIndent(state, "", "  ")
		d.fileMu.Lock()
		_ = os.WriteFile(filepath.Join(d.runtimeDir, "companion-state-"+safeFileID(payload.ServerID)+".json"), bytes, 0644)
		if payload.ServerID == "kingdom" {
			_ = os.WriteFile(filepath.Join(d.runtimeDir, "companion-state.json"), bytes, 0644)
		}
		d.fileMu.Unlock()
	}
	writeJSON(w, 200, map[string]any{
		"accepted":          accepted,
		"serverId":          payload.ServerID,
		"receivedAt":        time.Now().UnixMilli(),
		"secretFingerprint": secretFingerprint(d.companionSecret(payload.ServerID)),
	})
}
func (d *dashboard) updatePresence(serverID string, records []map[string]any) {
	d.fileMu.Lock()
	defer d.fileMu.Unlock()
	path := filepath.Join(d.runtimeDir, "presence.json")
	values := map[string]presenceEntry{}
	if bytes, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(bytes, &values)
	}
	now := time.Now().UnixMilli()
	for _, record := range records {
		xuid := firstNonempty(textField(record, "xuid"), textField(record, "XUID"))
		name := firstNonempty(textField(record, "playerName"), textField(record, "player"), textField(record, "name"))
		if xuid == "" && name == "" {
			continue
		}
		key := xuid
		if key == "" {
			key = serverID + ":" + strings.ToLower(name)
		}
		entry := values[key]
		if entry.FirstSeen == 0 {
			entry.FirstSeen = now
		}
		entry.Key = key
		entry.XUID = xuid
		entry.PlayerName = name
		entry.ServerID = serverID
		entry.Address = firstNonempty(textField(record, "address"), textField(record, "client"))
		entry.SubClient = firstNonempty(textField(record, "subClientId"), textField(record, "subClient"))
		entry.LastSeen = now
		entry.LastType = textField(record, "type")
		values[key] = entry
	}
	for key, entry := range values {
		if now-entry.LastSeen > 7*24*60*60*1000 {
			delete(values, key)
		}
	}
	bytes, _ := json.MarshalIndent(values, "", "  ")
	_ = os.WriteFile(path, bytes, 0644)
}

func (d *dashboard) handlePresence(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UnixMilli()
	values := d.presenceRecords()
	records := make([]map[string]any, 0, len(values))
	for _, item := range values {
		online := now-item.LastSeen <= d.presenceTTL.Milliseconds()
		records = append(records, map[string]any{"key": item.Key, "xuid": item.XUID, "playerName": item.PlayerName, "serverId": item.ServerID, "address": item.Address, "subClientId": item.SubClient, "firstSeen": item.FirstSeen, "lastSeen": item.LastSeen, "online": online})
	}
	writeJSON(w, 200, map[string]any{"records": records, "summary": d.presenceSummary()})
}

func (d *dashboard) handleAudit(w http.ResponseWriter, r *http.Request) {
	limit := int(envInt64("AUDIT_DEFAULT_LIMIT", 500))
	if value, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil {
		limit = value
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 5000 {
		limit = 5000
	}
	records := readRotated(filepath.Join(d.runtimeDir, "audit.jsonl"), limit)
	writeJSON(w, 200, map[string]any{"records": records})
}

var liveConfigKeys = map[string]string{
	"routing_mode": "enum", "firewall_enabled": "bool", "adaptive_firewall_enabled": "bool",
	"max_datagram_size": "uint", "max_packets_per_second_per_ip": "uint",
	"global_packets_per_second": "uint", "max_handshakes_per_minute": "uint",
	"ping_limit_per_second_per_ip": "uint", "risk_decay_per_minute": "uint",
	"risk_warning_threshold": "uint", "risk_ban_threshold": "uint",
	"progressive_ban_seconds": "list", "health_failure_threshold": "uint",
	"health_recovery_threshold":           "uint",
	"incident_mode_enabled":               "bool",
	"incident_trigger_packets_per_second": "uint",
	"incident_trigger_drop_ratio":         "ratio",
	"incident_min_packets_per_second":     "uint",
	"incident_recovery_seconds":           "uint",
	"incident_rate_divisor":               "uint",
	"incident_handshake_divisor":          "uint",
	"packet_capture_enabled":              "bool",
	"capture_outgoing":                    "bool", "packet_hex_preview_bytes": "uint",
	"stats_interval_seconds": "uint", "state_interval_ms": "uint",
}

func parseLiveConfig(content string) (map[string]string, error) {
	values := map[string]string{}
	for number, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("line %d must use key=value", number+1)
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		kind, ok := liveConfigKeys[key]
		if !ok {
			return nil, fmt.Errorf("unsupported live key: %s", key)
		}
		switch kind {
		case "bool":
			if value != "true" && value != "false" && value != "1" && value != "0" {
				return nil, fmt.Errorf("%s must be true or false", key)
			}
		case "uint":
			if _, err := strconv.ParseUint(value, 10, 64); err != nil {
				return nil, fmt.Errorf("%s must be an unsigned number", key)
			}
		case "ratio":
			number, err := strconv.ParseFloat(value, 64)
			if err != nil || number < 0.01 || number > 1 {
				return nil, fmt.Errorf("%s must be between 0.01 and 1", key)
			}
		case "list":
			for _, item := range strings.Split(value, ",") {
				if _, err := strconv.ParseUint(strings.TrimSpace(item), 10, 64); err != nil {
					return nil, fmt.Errorf("%s contains an invalid duration", key)
				}
			}
		case "enum":
			if value != "primary" && value != "failover" && value != "round_robin" && value != "least_sessions" {
				return nil, fmt.Errorf("invalid routing_mode")
			}
		}
		values[key] = value
	}
	if warning, ok := values["risk_warning_threshold"]; ok {
		if ban, ok2 := values["risk_ban_threshold"]; ok2 {
			w, _ := strconv.Atoi(warning)
			b, _ := strconv.Atoi(ban)
			if b <= w {
				return nil, errors.New("risk_ban_threshold must exceed risk_warning_threshold")
			}
		}
	}
	return values, nil
}

func (d *dashboard) queueCommand(command, argument, argument2 string) error {
	line := fmt.Sprintf("%d|%s|%s|%s\n", time.Now().UnixMilli(), sanitizeCommand(command), sanitizeCommand(argument), sanitizeCommand(argument2))
	return appendFile(filepath.Join(d.runtimeDir, "commands.log"), []byte(line))
}

func (d *dashboard) scheduleCommand(delay time.Duration, command, argument, argument2 string) {
	go func() {
		time.Sleep(delay)
		if err := d.queueCommand(command, argument, argument2); err != nil {
			log.Printf("[Ninj-OS Proxie] unable to queue %s: %v", command, err)
		}
	}()
}

func (d *dashboard) handleConfig(w http.ResponseWriter, r *http.Request) {
	actor := principalFromRequest(r)
	if r.Method == http.MethodGet {
		bytes, _ := os.ReadFile(d.liveConfigPath)
		values, _ := parseLiveConfig(string(bytes))
		writeJSON(w, 200, map[string]any{"content": string(bytes), "values": values, "path": d.liveConfigPath, "backupAvailable": fileExists(d.liveConfigPath + ".bak")})
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
	values, err := parseLiveConfig(body.Content)
	if err != nil {
		d.recordPolicyVersion(actor.Username, body.Content, "rejected: "+err.Error(), false)
		d.appendAudit(r, actor, "config.apply", "rejected", map[string]any{"error": err.Error()})
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	d.fileMu.Lock()
	if current, readErr := os.ReadFile(d.liveConfigPath); readErr == nil {
		_ = os.WriteFile(d.liveConfigPath+".bak", current, 0644)
	}
	temporary := d.liveConfigPath + ".tmp"
	if err = os.WriteFile(temporary, []byte(body.Content), 0644); err == nil {
		err = os.Rename(temporary, d.liveConfigPath)
	}
	if err == nil {
		err = d.queueCommand("reload", "", "")
	}
	d.fileMu.Unlock()
	if err != nil {
		d.appendAudit(r, actor, "config.apply", "failed", map[string]any{"error": err.Error()})
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	d.recordPolicyVersion(actor.Username, body.Content, "queued", true)
	d.appendAudit(r, actor, "config.apply", "queued", values)
	writeJSON(w, 202, map[string]any{"queued": true, "values": values})
}

func fileExists(path string) bool { _, err := os.Stat(path); return err == nil }

func (d *dashboard) handleConfigRollback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]string{"error": "Method not allowed"})
		return
	}
	actor := principalFromRequest(r)
	backup := d.liveConfigPath + ".bak"
	bytes, err := os.ReadFile(backup)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "No configuration backup is available"})
		return
	}
	if _, err = parseLiveConfig(string(bytes)); err != nil {
		writeJSON(w, 409, map[string]string{"error": "Backup is invalid: " + err.Error()})
		return
	}
	d.fileMu.Lock()
	err = os.WriteFile(d.liveConfigPath, bytes, 0644)
	if err == nil {
		err = d.queueCommand("reload", "", "")
	}
	d.fileMu.Unlock()
	if err != nil {
		d.appendAudit(r, actor, "config.rollback", "failed", map[string]any{"error": err.Error()})
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	d.recordPolicyVersion(actor.Username, string(bytes), "rollback queued", true)
	d.appendAudit(r, actor, "config.rollback", "queued", map[string]any{"path": backup})
	writeJSON(w, 202, map[string]bool{"queued": true})
}

func (d *dashboard) handleMetrics(w http.ResponseWriter, r *http.Request) {
	provided := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if d.metricsToken != "" && !hmac.Equal([]byte(provided), []byte(d.metricsToken)) {
		w.WriteHeader(401)
		return
	}
	gateway := d.combinedRuntimeState()
	counters, _ := gateway["counters"].(map[string]any)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "ninjos_gateway_active_sessions %v\n", gateway["activeSessions"])
	fmt.Fprintf(w, "ninjos_gateway_tracked_ips %v\n", gateway["trackedIps"])
	for key, name := range map[string]string{"droppedPackets": "dropped_packets_total", "rateLimited": "rate_limited_total", "temporaryBans": "temporary_bans_total", "adaptiveWarnings": "adaptive_warnings_total", "configReloadFailures": "config_reload_failures_total"} {
		fmt.Fprintf(w, "ninjos_gateway_%s %v\n", name, counters[key])
	}
	for _, companion := range d.companionStates() {
		metrics, _ := companion["metrics"].(map[string]any)
		server := textField(companion, "serverId")
		fmt.Fprintf(w, "ninjos_companion_connected{server=%q} %d\n", server, boolNumber(companion["connected"]))
		fmt.Fprintf(w, "ninjos_bds_tps{server=%q} %v\n", server, metrics["currentTps"])
		fmt.Fprintf(w, "ninjos_bds_mspt{server=%q} %v\n", server, metrics["currentMspt"])
		fmt.Fprintf(w, "ninjos_bds_online_players{server=%q} %v\n", server, metrics["onlinePlayers"])
	}
}
func boolNumber(value any) int {
	if flag, ok := value.(bool); ok && flag {
		return 1
	}
	return 0
}

func abs64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}
func firstNonempty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func readCPUUsage() uint64 {
	bytes, err := os.ReadFile("/sys/fs/cgroup/cpu.stat")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(bytes), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "usage_usec" {
			value, _ := strconv.ParseUint(fields[1], 10, 64)
			return value
		}
	}
	return 0
}
func (d *dashboard) cpuSampler() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		usage := readCPUUsage()
		elapsed := now.Sub(d.lastCPUAt).Microseconds()
		d.cpuMu.Lock()
		if elapsed > 0 && usage >= d.lastCPUUsage {
			d.cpuPercent = float64(usage-d.lastCPUUsage) / float64(elapsed) * 100
		}
		d.lastCPUUsage = usage
		d.lastCPUAt = now
		d.cpuMu.Unlock()
	}
}
func readInt(path string) (int64, bool) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	value := strings.TrimSpace(string(bytes))
	if value == "max" {
		return 0, false
	}
	n, err := strconv.ParseInt(value, 10, 64)
	return n, err == nil
}
func networkBytes() (rx, tx int64) {
	file, err := os.Open("/proc/net/dev")
	if err != nil {
		return
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.Contains(line, ":") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if strings.TrimSpace(parts[0]) == "lo" {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) >= 9 {
			a, _ := strconv.ParseInt(fields[0], 10, 64)
			b, _ := strconv.ParseInt(fields[8], 10, 64)
			rx += a
			tx += b
		}
	}
	return
}
func (d *dashboard) systemMetrics() map[string]any {
	memory, _ := readInt("/sys/fs/cgroup/memory.current")
	limit, hasLimit := readInt("/sys/fs/cgroup/memory.max")
	var stat syscall.Statfs_t
	_ = syscall.Statfs(d.runtimeDir, &stat)
	diskTotal := int64(stat.Blocks) * int64(stat.Bsize)
	diskFree := int64(stat.Bavail) * int64(stat.Bsize)
	rx, tx := networkBytes()
	d.cpuMu.Lock()
	cpu := d.cpuPercent
	d.cpuMu.Unlock()
	load := [3]float64{}
	if bytes, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(bytes))
		for i := 0; i < 3 && i < len(fields); i++ {
			load[i], _ = strconv.ParseFloat(fields[i], 64)
		}
	}
	result := map[string]any{"cpuPercent": cpu, "memoryBytes": memory, "diskUsedBytes": diskTotal - diskFree, "diskTotalBytes": diskTotal, "networkRxBytes": rx, "networkTxBytes": tx, "dashboardUptimeSeconds": time.Since(d.started).Seconds(), "loadAverage": load, "goRoutines": runtime.NumGoroutine()}
	if hasLimit {
		result["memoryLimitBytes"] = limit
	}
	return result
}

func (d *dashboard) discordMode() string {
	if d.discordWebhook != "" {
		return "webhook"
	}
	if d.discordBotToken != "" && d.discordChannel != "" {
		return "bot"
	}
	return "disabled"
}
func (d *dashboard) discordSend(payload map[string]any) error {
	if d.discordMode() == "disabled" {
		return nil
	}
	bytes, _ := json.Marshal(payload)
	url := d.discordWebhook
	request, err := http.NewRequest(http.MethodPost, url, strings.NewReader(string(bytes)))
	if d.discordMode() == "bot" {
		url = "https://discord.com/api/v10/channels/" + d.discordChannel + "/messages"
		request, err = http.NewRequest(http.MethodPost, url, strings.NewReader(string(bytes)))
		request.Header.Set("Authorization", "Bot "+d.discordBotToken)
	}
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return errors.New(response.Status)
	}
	return nil
}
func (d *dashboard) discordWatcher() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	path := filepath.Join(d.runtimeDir, "events.jsonl")
	for range ticker.C {
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		stat, _ := file.Stat()
		if d.eventOffset == 0 {
			d.eventOffset = stat.Size()
			file.Close()
			continue
		}
		if stat.Size() < d.eventOffset {
			d.eventOffset = 0
		}
		_, _ = file.Seek(d.eventOffset, io.SeekStart)
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			d.eventOffset += int64(len(scanner.Bytes()) + 1)
			var event map[string]any
			if json.Unmarshal(scanner.Bytes(), &event) != nil {
				continue
			}
			severity := textField(event, "severity")
			eventType := textField(event, "type")
			if severity != "warning" && severity != "error" && !strings.HasPrefix(eventType, "backend.") && !strings.HasPrefix(eventType, "transfer.") {
				continue
			}
			_ = d.discordSend(map[string]any{"embeds": []any{map[string]any{"title": "Ninj-OS: " + eventType, "description": textField(event, "message"), "color": map[bool]int{true: 15158332, false: 16753920}[severity == "error"], "timestamp": time.Now().UTC().Format(time.RFC3339)}}})
		}
		file.Close()
	}
}
func (d *dashboard) discordSummaryLoop() {
	ticker := time.NewTicker(d.discordSummary)
	defer ticker.Stop()
	for range ticker.C {
		if d.discordMode() == "disabled" {
			continue
		}
		gateway := d.combinedRuntimeState()
		companion := safeMap(filepath.Join(d.runtimeDir, "companion-state.json"))
		description := fmt.Sprintf("Sessions: %v\nRouting: %v\nTPS: %v\nMSPT: %v", gateway["activeSessions"], gateway["routingMode"], nested(companion, "metrics", "currentTps"), nested(companion, "metrics", "currentMspt"))
		_ = d.discordSend(map[string]any{"embeds": []any{map[string]any{"title": "Ninj-OS Proxie Metrics", "description": description, "color": 2619391, "timestamp": time.Now().UTC().Format(time.RFC3339)}}})
	}
}
func nested(value map[string]any, keys ...string) any {
	var current any = value
	for _, key := range keys {
		object, ok := current.(map[string]any)
		if !ok {
			return "—"
		}
		current = object[key]
	}
	if current == nil {
		return "—"
	}
	return current
}
