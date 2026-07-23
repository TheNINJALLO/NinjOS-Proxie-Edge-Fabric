// Copyright (C) 2026 Ninj-OS contributors.
// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const version = "7.3.13"

type config struct {
	ServerID        string `json:"serverId"`
	DashboardURL    string `json:"dashboardUrl"`
	SharedSecret    string `json:"sharedSecret"`
	PermissionsFile string `json:"permissionsFile"`
	ProcessName     string `json:"processName"`
	IntervalSeconds int    `json:"intervalSeconds"`
	SyncOperators   bool   `json:"syncOperators"`
	InsecureHTTP    bool   `json:"insecureHttp"`
}

type operatorEntry struct {
	XUID       string `json:"xuid"`
	Gamertag   string `json:"gamertag"`
	Permission string `json:"permission"`
	Role       string `json:"role"`
}

type snapshot struct {
	ServerID    string          `json:"serverId"`
	Operators   []operatorEntry `json:"operators"`
	GeneratedAt int64           `json:"generatedAt"`
}

type permissionRow struct {
	Permission string `json:"permission"`
	XUID       string `json:"xuid"`
}

func main() {
	configPath := flag.String("config", "agent.json", "path to agent configuration")
	once := flag.Bool("once", false, "run one report/sync cycle and exit")
	printVersion := flag.Bool("version", false, "print version")
	flag.Parse()
	if *printVersion {
		fmt.Println(version)
		return
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fatal(err)
	}
	if err := validate(cfg); err != nil {
		fatal(err)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	interval := time.Duration(cfg.IntervalSeconds) * time.Second
	if interval < 10*time.Second {
		interval = 30 * time.Second
	}

	fmt.Printf("[Ninj-OS Vanilla Agent] v%s active for %s on %s\n", version, cfg.ServerID, runtime.GOOS)
	for {
		if err := cycle(client, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "[Ninj-OS Vanilla Agent] %v\n", err)
		}
		if *once {
			return
		}
		time.Sleep(interval)
	}
}

func loadConfig(path string) (config, error) {
	var cfg config
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	err = json.Unmarshal(b, &cfg)
	return cfg, err
}

func validate(cfg config) error {
	cfg.ServerID = strings.TrimSpace(cfg.ServerID)
	if cfg.ServerID == "" || cfg.DashboardURL == "" || len(cfg.SharedSecret) < 16 {
		return errors.New("serverId, dashboardUrl and a sharedSecret of at least 16 characters are required")
	}
	if strings.HasPrefix(strings.ToLower(cfg.DashboardURL), "http://") && !cfg.InsecureHTTP {
		host := strings.TrimPrefix(strings.ToLower(cfg.DashboardURL), "http://")
		if !strings.HasPrefix(host, "127.0.0.1") && !strings.HasPrefix(host, "localhost") && !strings.HasPrefix(host, "10.") && !strings.HasPrefix(host, "192.168.") && !strings.HasPrefix(host, "172.16.") {
			return errors.New("plain HTTP is permitted only for local/private addresses unless insecureHttp is explicitly enabled")
		}
	}
	return nil
}

func cycle(client *http.Client, cfg config) error {
	running, rss := processStatus(cfg.ProcessName)
	metrics := map[string]any{
		"type": "metrics", "timestamp": time.Now().UnixMilli(), "companionVersion": "vanilla-agent-" + version,
		"serverType": "vanilla", "processRunning": running, "processMemoryBytes": rss,
		"platform": runtime.GOOS + "/" + runtime.GOARCH, "playersOnline": 0, "playersMax": 0,
		"queueDepth": 0, "uploadFailures": 0,
	}
	payload := map[string]any{"serverId": cfg.ServerID, "records": []any{metrics}}
	if err := signedPost(client, cfg, "/ingest", payload, nil); err != nil {
		return fmt.Errorf("heartbeat failed: %w", err)
	}
	if cfg.SyncOperators && cfg.PermissionsFile != "" {
		var snap snapshot
		if err := signedPost(client, cfg, "/api/bridge/v1/permissions", map[string]any{"serverId": cfg.ServerID}, &snap); err != nil {
			return fmt.Errorf("permission snapshot failed: %w", err)
		}
		if err := updatePermissions(cfg.PermissionsFile, snap.Operators); err != nil {
			return fmt.Errorf("permissions sync failed: %w", err)
		}
	}
	return nil
}

func signedPost(client *http.Client, cfg config, endpoint string, payload any, result any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	mac := hmac.New(sha256.New, []byte(cfg.SharedSecret))
	_, _ = mac.Write([]byte(timestamp + "\n"))
	_, _ = mac.Write(body)
	signature := hex.EncodeToString(mac.Sum(nil))
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, strings.TrimRight(cfg.DashboardURL, "/")+endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-NinjOS-Server", strings.ToLower(strings.TrimSpace(cfg.ServerID)))
	req.Header.Set("X-NinjOS-Timestamp", timestamp)
	req.Header.Set("X-NinjOS-Signature", signature)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	if result != nil && len(responseBody) > 0 {
		return json.Unmarshal(responseBody, result)
	}
	return nil
}

func updatePermissions(path string, operators []operatorEntry) error {
	existing := []permissionRow{}
	if b, err := os.ReadFile(path); err == nil && len(bytes.TrimSpace(b)) > 0 {
		_ = json.Unmarshal(b, &existing)
	}
	desired := map[string]permissionRow{}
	for _, row := range existing {
		if row.XUID != "" && row.Permission != "operator" {
			desired[row.XUID] = row
		}
	}
	for _, op := range operators {
		if op.XUID != "" {
			desired[op.XUID] = permissionRow{Permission: "operator", XUID: op.XUID}
		}
	}
	output := make([]permissionRow, 0, len(desired))
	for _, row := range desired {
		output = append(output, row)
	}
	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil && filepath.Dir(path) != "." {
		return err
	}
	tmp := path + ".ninjos.tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func processStatus(name string) (bool, int64) {
	if strings.TrimSpace(name) == "" {
		name = "bedrock_server"
	}
	if runtime.GOOS == "windows" {
		cmd := exec.Command("powershell", "-NoProfile", "-Command", fmt.Sprintf("$p=Get-Process -Name '%s' -ErrorAction SilentlyContinue | Select-Object -First 1; if($p){Write-Output ($p.WorkingSet64)}", strings.TrimSuffix(name, ".exe")))
		b, err := cmd.Output()
		if err != nil {
			return false, 0
		}
		v, _ := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
		return v > 0, v
	}
	cmd := exec.Command("sh", "-c", fmt.Sprintf("ps -C %s -o rss= 2>/dev/null | head -n1", shellWord(name)))
	b, err := cmd.Output()
	if err != nil {
		return false, 0
	}
	kb, _ := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	return kb > 0, kb * 1024
}

func shellWord(value string) string {
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._-", r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func fatal(err error) { fmt.Fprintln(os.Stderr, "[Ninj-OS Vanilla Agent] ERROR:", err); os.Exit(1) }
