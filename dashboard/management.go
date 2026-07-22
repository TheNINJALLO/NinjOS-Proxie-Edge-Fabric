// Copyright (C) 2026 Ninj-OS contributors.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type settingDescriptor struct {
	ID          string   `json:"id"`
	Section     string   `json:"section"`
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Type        string   `json:"type"`
	Value       string   `json:"value"`
	Options     []string `json:"options,omitempty"`
	Minimum     *int     `json:"minimum,omitempty"`
	Maximum     *int     `json:"maximum,omitempty"`
	Restart     bool     `json:"restartRequired"`
}

type secretDescriptor struct {
	ID                           string `json:"id"`
	Component                    string `json:"component"`
	Label                        string `json:"label"`
	Mode                         string `json:"mode"`
	Reference                    string `json:"reference"`
	EnvironmentVariable          string `json:"environmentVariable,omitempty"`
	SuggestedEnvironmentVariable string `json:"suggestedEnvironmentVariable,omitempty"`
	Configured                   bool   `json:"configured"`
	Fingerprint                  string `json:"fingerprint,omitempty"`
	CanGenerate                  bool   `json:"canGenerate"`
	CanInherit                   bool   `json:"canInherit"`
	RestartRequired              bool   `json:"restartRequired"`
	MinimumLength                int    `json:"minimumLength"`
}

type secretLocation struct {
	Section     string
	Key         string
	Component   string
	Label       string
	CanGenerate bool
	CanInherit  bool
	DefaultEnv  string
	Kind        string
	Required    bool
}

var environmentVariablePattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{1,127}$`)

const minimumSecretLength = 12

func intPointer(value int) *int { return &value }

func managedSettingSchema() []settingDescriptor {
	return []settingDescriptor{
		{ID: "edge.instance_name", Section: "edge", Key: "instance_name", Label: "Instance name", Description: "Name shown in the dashboard and console.", Type: "text", Restart: true},
		{ID: "edge.public_host", Section: "edge", Key: "public_host", Label: "Public host", Description: "Public IP address or DNS name players use.", Type: "text", Restart: true},
		{ID: "edge.primary_allocation_port", Section: "edge", Key: "primary_allocation_port", Label: "Primary allocation UDP port", Description: "Primary gateway listener used when no static route is selected.", Type: "number", Minimum: intPointer(1), Maximum: intPointer(65535), Restart: true},
		{ID: "edge.managed_public_udp_ports", Section: "edge", Key: "managed_public_udp_ports", Label: "Managed public UDP ports", Description: "Comma-separated ports and ranges already assigned in Pterodactyl.", Type: "text", Restart: true},
		{ID: "dashboard.port", Section: "dashboard", Key: "port", Label: "Dashboard TCP port", Description: "TCP listener for the dashboard and companion API.", Type: "number", Minimum: intPointer(1), Maximum: intPointer(65535), Restart: true},
		{ID: "dashboard.public_host", Section: "dashboard", Key: "public_host", Label: "Dashboard public host", Description: "Address companions use to reach the dashboard API.", Type: "text", Restart: true},
		{ID: "dashboard.session_minutes", Section: "dashboard", Key: "session_minutes", Label: "Dashboard session minutes", Description: "Lifetime of browser session tokens.", Type: "number", Minimum: intPointer(15), Maximum: intPointer(10080), Restart: true},
		{ID: "session_core.protocol_capture_enabled", Section: "session_core", Key: "protocol_capture_enabled", Label: "Protocol inspection enabled", Description: "Record bounded Full Proxy protocol observations.", Type: "boolean", Restart: true},
		{ID: "session_core.protocol_capture_mode", Section: "session_core", Key: "protocol_capture_mode", Label: "Protocol inspection tier", Description: "Metadata only, redacted decoded values, selected wire bytes, or all safe tiers.", Type: "select", Options: []string{"metadata", "decoded", "wire", "full"}, Restart: true},
		{ID: "session_core.protocol_capture_packet_ids", Section: "session_core", Key: "protocol_capture_packet_ids", Label: "Wire and round-trip packet IDs", Description: "Comma-separated application packet IDs allowed for wire capture and round-trip checks.", Type: "text", Restart: true},
		{ID: "session_core.protocol_capture_max_packet_bytes", Section: "session_core", Key: "protocol_capture_max_packet_bytes", Label: "Maximum captured bytes per packet", Description: "Hard limit for each selected wire or decode-failure sample.", Type: "number", Minimum: intPointer(64), Maximum: intPointer(1048576), Restart: true},
		{ID: "session_core.protocol_capture_decode_failures", Section: "session_core", Key: "protocol_capture_decode_failures", Label: "Capture decode failures", Description: "Record bounded failure details before an undecodable packet is discarded.", Type: "boolean", Restart: true},
		{ID: "session_core.protocol_observation_max_bytes", Section: "session_core", Key: "protocol_observation_max_bytes", Label: "Protocol observation log size", Description: "Maximum bytes per backend observation log before rotation.", Type: "number", Minimum: intPointer(65536), Maximum: intPointer(1073741824), Restart: true},
		{ID: "companion.presence_ttl_seconds", Section: "companion", Key: "presence_ttl_seconds", Label: "Presence TTL seconds", Description: "How long a companion/player heartbeat remains current.", Type: "number", Minimum: intPointer(30), Maximum: intPointer(3600), Restart: true},
		{ID: "companion.capture_mode", Section: "companion", Key: "capture_mode", Label: "Companion capture mode", Description: "Default gameplay packet capture mode for downloaded configs.", Type: "select", Options: []string{"off", "metadata", "selected", "all"}, Restart: false},
		{ID: "companion.selected_packet_ids", Section: "companion", Key: "selected_packet_ids", Label: "Selected packet IDs", Description: "Comma-separated packet IDs captured in selected mode.", Type: "text", Restart: false},
		{ID: "companion.payload_limit", Section: "companion", Key: "payload_limit", Label: "Payload preview limit", Description: "Maximum gameplay payload bytes included per record.", Type: "number", Minimum: intPointer(0), Maximum: intPointer(65536), Restart: false},
		{ID: "companion.redact_packet_ids", Section: "companion", Key: "redact_packet_ids", Label: "Redacted packet IDs", Description: "Packet IDs whose payloads are never exported.", Type: "text", Restart: false},
		{ID: "companion.queue_capacity", Section: "companion", Key: "queue_capacity", Label: "Companion queue capacity", Description: "Maximum queued companion records.", Type: "number", Minimum: intPointer(100), Maximum: intPointer(1000000), Restart: false},
		{ID: "companion.batch_size", Section: "companion", Key: "batch_size", Label: "Companion batch size", Description: "Records sent per dashboard upload.", Type: "number", Minimum: intPointer(1), Maximum: intPointer(10000), Restart: false},
		{ID: "companion.flush_ms", Section: "companion", Key: "flush_ms", Label: "Companion flush interval", Description: "Maximum milliseconds before queued data is uploaded.", Type: "number", Minimum: intPointer(10), Maximum: intPointer(60000), Restart: false},
		{ID: "companion.movement_sample_rate", Section: "companion", Key: "movement_sample_rate", Label: "Movement sample rate", Description: "Capture one movement packet out of this many.", Type: "number", Minimum: intPointer(1), Maximum: intPointer(10000), Restart: false},
		{ID: "companion.metrics_interval_ticks", Section: "companion", Key: "metrics_interval_ticks", Label: "Metrics interval ticks", Description: "Server ticks between companion metric snapshots.", Type: "number", Minimum: intPointer(1), Maximum: intPointer(12000), Restart: false},
		{ID: "companion.reconnect_seconds", Section: "companion", Key: "reconnect_seconds", Label: "Companion reconnect seconds", Description: "Delay after a failed dashboard upload.", Type: "number", Minimum: intPointer(1), Maximum: intPointer(600), Restart: false},
		{ID: "companion.presence_enabled", Section: "companion", Key: "presence_enabled", Label: "Presence enabled", Description: "Include online-player presence in downloaded companion configs.", Type: "boolean", Restart: false},
		{ID: "companion.presence_include_address", Section: "companion", Key: "presence_include_address", Label: "Include player address", Description: "Include player addresses in companion presence records.", Type: "boolean", Restart: false},
		{ID: "companion.transfer_enabled", Section: "companion", Key: "transfer_enabled", Label: "Companion transfers enabled", Description: "Allow companion transfer commands.", Type: "boolean", Restart: false},
		{ID: "companion.drop_receive_ids", Section: "companion", Key: "drop_receive_ids", Label: "Dropped receive packet IDs", Description: "Comma-separated incoming packet IDs cancelled by downloaded configs.", Type: "text", Restart: false},
		{ID: "companion.drop_send_ids", Section: "companion", Key: "drop_send_ids", Label: "Dropped send packet IDs", Description: "Comma-separated outgoing packet IDs cancelled by downloaded configs.", Type: "text", Restart: false},
		{ID: "transfer.enabled", Section: "transfer", Key: "enabled", Label: "Transfer broker enabled", Description: "Enable one-use transfer tickets.", Type: "boolean", Restart: true},
		{ID: "transfer.public_host", Section: "transfer", Key: "public_host", Label: "Transfer public host", Description: "Host returned in transfer tickets.", Type: "text", Restart: true},
		{ID: "transfer.port_start", Section: "transfer", Key: "port_start", Label: "Transfer port start", Description: "First temporary UDP transfer port.", Type: "number", Minimum: intPointer(1), Maximum: intPointer(65535), Restart: true},
		{ID: "transfer.port_end", Section: "transfer", Key: "port_end", Label: "Transfer port end", Description: "Last temporary UDP transfer port.", Type: "number", Minimum: intPointer(1), Maximum: intPointer(65535), Restart: true},
		{ID: "transfer.ticket_ttl_seconds", Section: "transfer", Key: "ticket_ttl_seconds", Label: "Transfer ticket TTL", Description: "Seconds a one-use route remains valid.", Type: "number", Minimum: intPointer(5), Maximum: intPointer(300), Restart: true},
		{ID: "transfer.require_source_ip", Section: "transfer", Key: "require_source_ip", Label: "Require source IP", Description: "Bind transfer tickets to the requesting address.", Type: "boolean", Restart: true},
		{ID: "firewall.enabled", Section: "firewall", Key: "enabled", Label: "Firewall enabled", Description: "Enable gateway traffic protection.", Type: "boolean", Restart: true},
		{ID: "firewall.adaptive", Section: "firewall", Key: "adaptive", Label: "Adaptive risk scoring", Description: "Enable weighted risk and progressive bans.", Type: "boolean", Restart: true},
		{ID: "firewall.risk_decay_per_minute", Section: "firewall", Key: "risk_decay_per_minute", Label: "Risk decay per minute", Description: "Risk points removed each minute.", Type: "number", Minimum: intPointer(0), Maximum: intPointer(1000), Restart: true},
		{ID: "firewall.risk_warning_threshold", Section: "firewall", Key: "risk_warning_threshold", Label: "Risk warning threshold", Description: "Score that creates a warning event.", Type: "number", Minimum: intPointer(1), Maximum: intPointer(10000), Restart: true},
		{ID: "firewall.risk_ban_threshold", Section: "firewall", Key: "risk_ban_threshold", Label: "Risk ban threshold", Description: "Score that creates a progressive ban.", Type: "number", Minimum: intPointer(2), Maximum: intPointer(10000), Restart: true},
		{ID: "firewall.progressive_ban_seconds", Section: "firewall", Key: "progressive_ban_seconds", Label: "Progressive ban schedule", Description: "Comma-separated ban lengths in seconds.", Type: "text", Restart: true},
		{ID: "firewall.max_datagram_size", Section: "firewall", Key: "max_datagram_size", Label: "Maximum datagram size", Description: "Global fallback UDP datagram limit.", Type: "number", Minimum: intPointer(512), Maximum: intPointer(65535), Restart: true},
		{ID: "firewall.max_packets_per_second_per_ip", Section: "firewall", Key: "max_packets_per_second_per_ip", Label: "Packets/sec per IP", Description: "Global fallback per-address packet limit.", Type: "number", Minimum: intPointer(100), Maximum: intPointer(1000000), Restart: true},
		{ID: "firewall.global_packets_per_second", Section: "firewall", Key: "global_packets_per_second", Label: "Global packets/sec", Description: "Whole-gateway packet ceiling.", Type: "number", Minimum: intPointer(1000), Maximum: intPointer(10000000), Restart: true},
		{ID: "firewall.max_handshakes_per_minute", Section: "firewall", Key: "max_handshakes_per_minute", Label: "Handshakes/min per IP", Description: "Fallback new-session handshake limit.", Type: "number", Minimum: intPointer(1), Maximum: intPointer(100000), Restart: true},
		{ID: "firewall.max_sessions", Section: "firewall", Key: "max_sessions", Label: "Maximum sessions", Description: "Maximum concurrent gateway sessions.", Type: "number", Minimum: intPointer(1), Maximum: intPointer(100000), Restart: true},
		{ID: "firewall.max_sessions_per_ip", Section: "firewall", Key: "max_sessions_per_ip", Label: "Sessions per IP", Description: "Fallback concurrent-session limit per address.", Type: "number", Minimum: intPointer(1), Maximum: intPointer(1000), Restart: true},
		{ID: "incident.enabled", Section: "incident", Key: "enabled", Label: "Incident mode enabled", Description: "Automatically tighten limits during attacks.", Type: "boolean", Restart: true},
		{ID: "incident.trigger_packets_per_second", Section: "incident", Key: "trigger_packets_per_second", Label: "Incident trigger PPS", Description: "Immediate global traffic trigger.", Type: "number", Minimum: intPointer(1000), Maximum: intPointer(10000000), Restart: true},
		{ID: "incident.trigger_drop_ratio", Section: "incident", Key: "trigger_drop_ratio", Label: "Incident drop ratio", Description: "Dropped fraction used with the minimum PPS trigger.", Type: "text", Restart: true},
		{ID: "incident.minimum_packets_per_second", Section: "incident", Key: "minimum_packets_per_second", Label: "Incident minimum PPS", Description: "Minimum traffic before drop-ratio detection applies.", Type: "number", Minimum: intPointer(100), Maximum: intPointer(10000000), Restart: true},
		{ID: "incident.recovery_seconds", Section: "incident", Key: "recovery_seconds", Label: "Incident recovery seconds", Description: "Stable traffic required before clearing incident mode.", Type: "number", Minimum: intPointer(5), Maximum: intPointer(3600), Restart: true},
		{ID: "incident.rate_divisor", Section: "incident", Key: "rate_divisor", Label: "Incident rate divisor", Description: "Divides packet limits during incident mode.", Type: "number", Minimum: intPointer(1), Maximum: intPointer(100), Restart: true},
		{ID: "incident.handshake_divisor", Section: "incident", Key: "handshake_divisor", Label: "Incident handshake divisor", Description: "Divides handshake limits during incident mode.", Type: "number", Minimum: intPointer(1), Maximum: intPointer(100), Restart: true},
		{ID: "health.enabled", Section: "health", Key: "enabled", Label: "Backend health checks", Description: "Probe configured backends.", Type: "boolean", Restart: true},
		{ID: "health.interval_seconds", Section: "health", Key: "interval_seconds", Label: "Health interval seconds", Description: "Seconds between backend checks.", Type: "number", Minimum: intPointer(1), Maximum: intPointer(300), Restart: true},
		{ID: "health.timeout_ms", Section: "health", Key: "timeout_ms", Label: "Health timeout ms", Description: "Timeout for a backend probe.", Type: "number", Minimum: intPointer(100), Maximum: intPointer(30000), Restart: true},
		{ID: "health.failure_threshold", Section: "health", Key: "failure_threshold", Label: "Health failure threshold", Description: "Consecutive failures before marking unhealthy.", Type: "number", Minimum: intPointer(1), Maximum: intPointer(100), Restart: true},
		{ID: "health.recovery_threshold", Section: "health", Key: "recovery_threshold", Label: "Health recovery threshold", Description: "Consecutive successes before recovery.", Type: "number", Minimum: intPointer(1), Maximum: intPointer(100), Restart: true},
		{ID: "health.automatic_actions", Section: "health", Key: "automatic_actions", Label: "Automatic health actions", Description: "Disable and restore routes based on health.", Type: "boolean", Restart: true},
		{ID: "health.require_companion", Section: "health", Key: "require_companion", Label: "Require companion health", Description: "Treat a missing companion as unhealthy.", Type: "boolean", Restart: true},
		{ID: "health.companion_stale_seconds", Section: "health", Key: "companion_stale_seconds", Label: "Companion stale seconds", Description: "Age before a companion heartbeat is stale.", Type: "number", Minimum: intPointer(5), Maximum: intPointer(3600), Restart: true},
		{ID: "health.degraded_after_seconds", Section: "health", Key: "degraded_after_seconds", Label: "Degraded action delay", Description: "Unhealthy duration before disabling transfers.", Type: "number", Minimum: intPointer(5), Maximum: intPointer(3600), Restart: true},
		{ID: "health.stable_recovery_seconds", Section: "health", Key: "stable_recovery_seconds", Label: "Stable recovery seconds", Description: "Healthy duration before restoring transfers.", Type: "number", Minimum: intPointer(5), Maximum: intPointer(3600), Restart: true},
		{ID: "health.minimum_tps", Section: "health", Key: "minimum_tps", Label: "Minimum TPS", Description: "TPS floor for automated health actions.", Type: "number", Minimum: intPointer(1), Maximum: intPointer(20), Restart: true},
		{ID: "health.maximum_mspt", Section: "health", Key: "maximum_mspt", Label: "Maximum MSPT", Description: "MSPT ceiling for automated health actions.", Type: "number", Minimum: intPointer(1), Maximum: intPointer(10000), Restart: true},
		{ID: "logging.packet_capture", Section: "logging", Key: "packet_capture", Label: "Transport packet capture", Description: "Record transport packet metadata.", Type: "boolean", Restart: true},
		{ID: "logging.capture_outgoing", Section: "logging", Key: "capture_outgoing", Label: "Capture outgoing packets", Description: "Record gateway-to-client transport metadata.", Type: "boolean", Restart: true},
		{ID: "logging.packet_hex_preview_bytes", Section: "logging", Key: "packet_hex_preview_bytes", Label: "Transport hex preview bytes", Description: "Bytes included in transport previews. Zero disables previews.", Type: "number", Minimum: intPointer(0), Maximum: intPointer(2048), Restart: true},
		{ID: "logging.packet_log_max_bytes", Section: "logging", Key: "packet_log_max_bytes", Label: "Packet log maximum bytes", Description: "Maximum transport log size before rotation.", Type: "number", Minimum: intPointer(1048576), Maximum: intPointer(1073741824), Restart: true},
		{ID: "logging.event_log_max_bytes", Section: "logging", Key: "event_log_max_bytes", Label: "Event log maximum bytes", Description: "Maximum event log size before rotation.", Type: "number", Minimum: intPointer(1048576), Maximum: intPointer(1073741824), Restart: true},
		{ID: "logging.audit_log_max_bytes", Section: "logging", Key: "audit_log_max_bytes", Label: "Audit log maximum bytes", Description: "Maximum administrative audit log size.", Type: "number", Minimum: intPointer(1048576), Maximum: intPointer(1073741824), Restart: true},
		{ID: "logging.metric_sample_seconds", Section: "logging", Key: "metric_sample_seconds", Label: "Metric sample seconds", Description: "Historical metrics sampling interval.", Type: "number", Minimum: intPointer(1), Maximum: intPointer(3600), Restart: true},
		{ID: "logging.metric_retention_days", Section: "logging", Key: "metric_retention_days", Label: "Metric retention days", Description: "Historical metric retention.", Type: "number", Minimum: intPointer(1), Maximum: intPointer(3650), Restart: true},
		{ID: "discord.channel_id", Section: "discord", Key: "channel_id", Label: "Discord channel ID", Description: "Channel used by bot-token delivery.", Type: "text", Restart: true},
		{ID: "discord.summary_minutes", Section: "discord", Key: "summary_minutes", Label: "Discord summary minutes", Description: "Interval between periodic summaries.", Type: "number", Minimum: intPointer(1), Maximum: intPointer(1440), Restart: true},
	}
}

func (d *dashboard) handleManagedSettings(w http.ResponseWriter, r *http.Request) {
	schema := managedSettingSchema()
	switch r.Method {
	case http.MethodGet:
		document, err := loadINI(d.configPath)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		for index := range schema {
			schema[index].Value = document.get(schema[index].Section, schema[index].Key, "")
		}
		writeJSON(w, 200, map[string]any{"settings": schema, "advancedEditorAvailable": true})
	case http.MethodPut:
		var request struct {
			Values map[string]string `json:"values"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024*1024))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			writeJSON(w, 400, map[string]string{"error": "Invalid settings request"})
			return
		}
		allowed := map[string]settingDescriptor{}
		for _, descriptor := range schema {
			allowed[descriptor.ID] = descriptor
		}
		document, err := loadINI(d.configPath)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		for id, value := range request.Values {
			descriptor, ok := allowed[id]
			if !ok {
				writeJSON(w, 400, map[string]string{"error": "Unsupported managed setting: " + id})
				return
			}
			value = strings.TrimSpace(value)
			if strings.ContainsAny(value, "\r\n") {
				writeJSON(w, 400, map[string]string{"error": descriptor.Label + " cannot contain line breaks"})
				return
			}
			if descriptor.Type == "boolean" {
				if value != "true" && value != "false" {
					writeJSON(w, 400, map[string]string{"error": descriptor.Label + " must be true or false"})
					return
				}
			}
			if descriptor.Type == "number" {
				number, err := strconv.Atoi(value)
				if err != nil {
					writeJSON(w, 400, map[string]string{"error": descriptor.Label + " must be an integer"})
					return
				}
				if descriptor.Minimum != nil && number < *descriptor.Minimum {
					writeJSON(w, 400, map[string]string{"error": fmt.Sprintf("%s must be at least %d", descriptor.Label, *descriptor.Minimum)})
					return
				}
				if descriptor.Maximum != nil && number > *descriptor.Maximum {
					writeJSON(w, 400, map[string]string{"error": fmt.Sprintf("%s must be at most %d", descriptor.Label, *descriptor.Maximum)})
					return
				}
			}
			if descriptor.Type == "select" {
				valid := false
				for _, option := range descriptor.Options {
					if value == option {
						valid = true
						break
					}
				}
				if !valid {
					writeJSON(w, 400, map[string]string{"error": descriptor.Label + " contains an unsupported option"})
					return
				}
			}
			document.section(descriptor.Section)[descriptor.Key] = value
		}
		if err := d.persistUnifiedDocument(r, document, "config.settings.apply", map[string]any{"updated": len(request.Values)}); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 202, map[string]any{"queued": true, "saved": true, "updated": len(request.Values), "revision": configRevision(document)})
	default:
		writeJSON(w, 405, map[string]string{"error": "Method not allowed"})
	}
}

func secretLocationForID(document *iniDocument, id string) (secretLocation, error) {
	static := map[string]secretLocation{
		"dashboard.operator_token":    {Section: "dashboard", Key: "operator_token", Component: "dashboard", Label: "Operator access token", CanGenerate: true, DefaultEnv: "DASHBOARD_OPERATOR_TOKEN", Kind: "token"},
		"dashboard.viewer_token":      {Section: "dashboard", Key: "viewer_token", Component: "dashboard", Label: "Viewer access token", CanGenerate: true, DefaultEnv: "DASHBOARD_READONLY_TOKEN", Kind: "token"},
		"dashboard.metrics_token":     {Section: "dashboard", Key: "metrics_token", Component: "dashboard", Label: "Metrics access token", CanGenerate: true, DefaultEnv: "METRICS_TOKEN", Kind: "token"},
		"dashboard.totp_secret":       {Section: "dashboard", Key: "totp_secret", Component: "dashboard", Label: "Owner TOTP secret", CanGenerate: true, DefaultEnv: "DASHBOARD_TOTP_SECRET", Kind: "totp"},
		"session_core.internal_token": {Section: "session_core", Key: "internal_token", Component: "session core", Label: "Session Core internal token", CanGenerate: true, DefaultEnv: "SESSION_CORE_TOKEN", Kind: "token"},
		"companion.default_secret":    {Section: "companion", Key: "default_secret", Component: "companion", Label: "Default companion shared secret (not dashboard login)", CanGenerate: true, DefaultEnv: "COMPANION_SHARED_SECRET", Kind: "secret", Required: true},
		"discord.webhook_url":         {Section: "discord", Key: "webhook_url", Component: "discord", Label: "Discord webhook URL", DefaultEnv: "DISCORD_WEBHOOK_URL", Kind: "url"},
		"discord.bot_token":           {Section: "discord", Key: "bot_token", Component: "discord", Label: "Discord bot token", DefaultEnv: "DISCORD_BOT_TOKEN", Kind: "secret"},
	}
	if location, ok := static[id]; ok {
		return location, nil
	}
	if strings.HasPrefix(id, "backend.") && strings.HasSuffix(id, ".companion_secret") {
		backendID := strings.TrimSuffix(strings.TrimPrefix(id, "backend."), ".companion_secret")
		if !backendIDPattern.MatchString(backendID) {
			return secretLocation{}, errors.New("Invalid backend secret ID")
		}
		if document.Sections["backend."+backendID] == nil {
			return secretLocation{}, errors.New("Backend not found")
		}
		return secretLocation{
			Section: "backend." + backendID, Key: "companion_secret", Component: "companion." + backendID,
			Label: humanizeBackendID(backendID) + " companion shared secret (not dashboard login)", CanGenerate: true, CanInherit: true,
			DefaultEnv: "COMPANION_" + strings.ToUpper(strings.ReplaceAll(backendID, "-", "_")) + "_SECRET", Kind: "secret",
		}, nil
	}
	return secretLocation{}, errors.New("Unknown secret ID")
}

func secretFingerprint(value string) string {
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return strings.ToUpper(hex.EncodeToString(sum[:6]))
}

func generateManagedSecret(kind string) (string, error) {
	size := 32
	if kind == "totp" {
		size = 20
	}
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	if kind == "totp" {
		return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buffer), nil
	}
	return hex.EncodeToString(buffer), nil
}

func secretDescriptorFor(document *iniDocument, id string, location secretLocation) secretDescriptor {
	reference := strings.TrimSpace(document.get(location.Section, location.Key, ""))
	mode := "unset"
	displayReference := "Not configured"
	variable := envNameFromReference(reference)
	resolved := ""
	if reference == "" && location.CanInherit {
		mode = "inherit"
		displayReference = "Inherit default companion secret"
		resolved = resolveConfigValue(document.get("companion", "default_secret", ""))
	} else if variable != "" {
		mode = "environment"
		displayReference = "env:" + variable
		resolved = strings.TrimSpace(os.Getenv(variable))
	} else if reference != "" {
		mode = "dashboard"
		displayReference = "Dashboard-managed secret"
		resolved = reference
	}
	return secretDescriptor{
		ID: id, Component: location.Component, Label: location.Label, Mode: mode,
		Reference: displayReference, EnvironmentVariable: variable, SuggestedEnvironmentVariable: location.DefaultEnv, Configured: resolved != "",
		Fingerprint: secretFingerprint(resolved), CanGenerate: location.CanGenerate,
		CanInherit: location.CanInherit, RestartRequired: true,
		MinimumLength: minimumSecretLength,
	}
}

func (d *dashboard) secretDescriptors(document *iniDocument) []secretDescriptor {
	ids := []string{
		"dashboard.operator_token", "dashboard.viewer_token",
		"dashboard.metrics_token", "dashboard.totp_secret", "companion.default_secret",
		"session_core.internal_token",
		"discord.webhook_url", "discord.bot_token",
	}
	topology, _ := topologyFromUnified(document)
	for _, backend := range topology.Backends {
		ids = append(ids, "backend."+backend.ID+".companion_secret")
	}
	descriptors := make([]secretDescriptor, 0, len(ids))
	for _, id := range ids {
		location, err := secretLocationForID(document, id)
		if err != nil {
			continue
		}
		descriptors = append(descriptors, secretDescriptorFor(document, id, location))
	}
	sort.Slice(descriptors, func(i, j int) bool {
		if descriptors[i].Component == descriptors[j].Component {
			return descriptors[i].Label < descriptors[j].Label
		}
		return descriptors[i].Component < descriptors[j].Component
	})
	return descriptors
}

func validateManagedSecret(location secretLocation, value string) error {
	value = strings.TrimSpace(value)
	if strings.ContainsAny(value, "\r\n") {
		return errors.New("Secret values cannot contain line breaks")
	}
	if location.Kind == "url" {
		if value == "" {
			return nil
		}
		if len(value) < minimumSecretLength {
			return fmt.Errorf("Secret values must contain at least %d characters", minimumSecretLength)
		}
		if !strings.HasPrefix(value, "https://") && !strings.HasPrefix(value, "http://") {
			return errors.New("Webhook URL must begin with https:// or http://")
		}
		return nil
	}
	if location.Kind == "totp" {
		if value == "" {
			return nil
		}
		value = strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(value, " ", ""), "-", ""))
		if len(value) < minimumSecretLength {
			return fmt.Errorf("Secret values must contain at least %d characters", minimumSecretLength)
		}
		if _, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(value); err != nil {
			return errors.New("TOTP secret must be valid Base32")
		}
		return nil
	}
	if value != "" && len(value) < minimumSecretLength {
		return fmt.Errorf("Secret values must contain at least %d characters", minimumSecretLength)
	}
	return nil
}

func (d *dashboard) handleManagedSecrets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		document, err := loadINI(d.configPath)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{
			"secrets": d.secretDescriptors(document),
			"storage": "Dashboard-managed values are stored in config/edge-fabric.ini with mode 0600 and are never returned by this API.",
		})
	case http.MethodPut:
		var request struct {
			ID                  string `json:"id"`
			Mode                string `json:"mode"`
			Value               string `json:"value"`
			EnvironmentVariable string `json:"environmentVariable"`
			Generate            bool   `json:"generate"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128*1024))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			writeJSON(w, 400, map[string]string{"error": "Invalid secret request"})
			return
		}
		document, err := loadINI(d.configPath)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		location, err := secretLocationForID(document, strings.TrimSpace(request.ID))
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		mode := strings.ToLower(strings.TrimSpace(request.Mode))
		value := strings.TrimSpace(request.Value)
		if request.Generate {
			if !location.CanGenerate {
				writeJSON(w, 400, map[string]string{"error": "This key cannot be generated automatically"})
				return
			}
			value, err = generateManagedSecret(location.Kind)
			if err != nil {
				writeJSON(w, 500, map[string]string{"error": err.Error()})
				return
			}
			mode = "dashboard"
		}
		switch mode {
		case "dashboard":
			if err := validateManagedSecret(location, value); err != nil {
				writeJSON(w, 400, map[string]string{"error": err.Error()})
				return
			}
			if location.Required && value == "" {
				writeJSON(w, 400, map[string]string{"error": location.Label + " cannot be empty"})
				return
			}
			document.section(location.Section)[location.Key] = value
		case "environment":
			variable := strings.ToUpper(strings.TrimSpace(request.EnvironmentVariable))
			if variable == "" {
				variable = location.DefaultEnv
			}
			if !environmentVariablePattern.MatchString(variable) {
				writeJSON(w, 400, map[string]string{"error": "Invalid environment variable name"})
				return
			}
			resolved := strings.TrimSpace(os.Getenv(variable))
			if resolved == "" {
				writeJSON(w, 400, map[string]string{"error": "Environment variable " + variable + " is empty in the running service. Set it in Pterodactyl Startup/systemd first, or use Dashboard-managed mode."})
				return
			}
			if err := validateManagedSecret(location, resolved); err != nil {
				writeJSON(w, 400, map[string]string{"error": "Environment variable " + variable + ": " + err.Error()})
				return
			}
			document.section(location.Section)[location.Key] = "env:" + variable
		case "inherit":
			if !location.CanInherit {
				writeJSON(w, 400, map[string]string{"error": "This key cannot inherit another key"})
				return
			}
			document.section(location.Section)[location.Key] = ""
		case "clear", "unset":
			if location.Required {
				writeJSON(w, 400, map[string]string{"error": location.Label + " cannot be cleared"})
				return
			}
			document.section(location.Section)[location.Key] = ""
		default:
			writeJSON(w, 400, map[string]string{"error": "Mode must be dashboard, environment, inherit, or clear"})
			return
		}
		if err := d.persistUnifiedDocument(r, document, "security.secret.rotate", map[string]any{
			"id": request.ID, "mode": mode, "fingerprint": secretFingerprint(value),
		}); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		if request.ID == "dashboard.owner_token" || request.ID == "dashboard.totp_secret" {
			if d.ledger != nil {
				d.ledger.clearSessions()
			}
		}
		descriptor := secretDescriptorFor(document, request.ID, location)
		response := map[string]any{
			"queued": true, "saved": true, "secret": descriptor,
			"revision":       configRevision(document),
			"sessionWillEnd": request.ID == "dashboard.owner_token" || request.ID == "dashboard.totp_secret",
		}
		if request.Generate {
			response["generatedValue"] = value
			response["oneTimeReveal"] = true
		}
		writeJSON(w, 202, response)
	default:
		writeJSON(w, 405, map[string]string{"error": "Method not allowed"})
	}
}

func (d *dashboard) persistUnifiedDocument(r *http.Request, document *iniDocument, action string, details any) error {
	if err := validateUnifiedConfig(document); err != nil {
		return err
	}
	current, err := loadINI(d.configPath)
	if err != nil {
		return err
	}
	restartScope := unifiedConfigRestartScope(current, document)
	if err := d.writeAndVerifyUnifiedConfig(document, current); err != nil {
		return err
	}
	revision := configRevision(document)
	if err := writeConfigSaveStatus(d.runtimeDir, revision, action, restartScope); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(d.runtimeDir, "topology-restart.pending"), []byte(strconv.FormatInt(time.Now().UnixMilli(), 10)), 0644); err != nil {
		return err
	}

	command := "topology_restart"
	delay := 750 * time.Millisecond
	if restartScope == "services" {
		command = "service_restart"
		delay = 1500 * time.Millisecond
	}
	d.scheduleCommand(delay, command, "", "")
	d.appendAudit(r, principalFromRequest(r), action, restartScope+"_restart_scheduled", map[string]any{
		"details":  details,
		"revision": revision,
	})
	return nil
}

func effectiveCompanionSecret(document *iniDocument, backendID string) string {
	backendID = strings.ToLower(strings.TrimSpace(backendID))
	section := document.Sections["backend."+backendID]
	if section == nil {
		return ""
	}
	secret := resolveConfigValue(strings.TrimSpace(section["companion_secret"]))
	if secret == "" {
		secret = resolveConfigValue(document.get("companion", "default_secret", ""))
	}
	return secret
}

func (d *dashboard) companionStateSummary(backendID string) map[string]any {
	path := filepath.Join(d.runtimeDir, "companion-state-"+safeFileID(backendID)+".json")
	summary := map[string]any{
		"connected":  false,
		"lastSeenAt": int64(0),
		"ageSeconds": int64(0),
		"stateFile":  filepath.Base(path),
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		return summary
	}
	var state map[string]any
	if json.Unmarshal(bytes, &state) != nil {
		return summary
	}
	timestamp := int64(0)
	switch value := state["timestamp"].(type) {
	case float64:
		timestamp = int64(value)
	case int64:
		timestamp = value
	case json.Number:
		timestamp, _ = value.Int64()
	}
	age := int64(0)
	if timestamp > 0 {
		age = (time.Now().UnixMilli() - timestamp) / 1000
		if age < 0 {
			age = 0
		}
	}
	summary["lastSeenAt"] = timestamp
	summary["ageSeconds"] = age
	summary["connected"] = timestamp > 0 && age <= 60
	if metrics, ok := state["metrics"].(map[string]any); ok {
		summary["companionVersion"] = metrics["companionVersion"]
		summary["currentTps"] = metrics["currentTps"]
		summary["currentMspt"] = metrics["currentMspt"]
		summary["uploadFailures"] = metrics["uploadFailures"]
		summary["queueDepth"] = metrics["queueDepth"]
	}
	return summary
}

func companionProperties(document *iniDocument, backendID string) (string, error) {
	backendID = strings.ToLower(strings.TrimSpace(backendID))
	section := document.Sections["backend."+backendID]
	if section == nil {
		return "", errors.New("Backend not found")
	}
	secret := effectiveCompanionSecret(document, backendID)
	if secret == "" {
		return "", errors.New("No companion secret is configured for this backend")
	}
	dashboardHost := document.get("dashboard", "public_host", document.get("edge", "public_host", "127.0.0.1"))
	dashboardPort := document.getInt("dashboard", "port", 25571, 1, 65535)
	keys := []struct{ key, fallback string }{
		{"capture_mode", "metadata"}, {"selected_packet_ids", "30,77"}, {"payload_limit", "512"},
		{"redact_packet_ids", "1,3,4"}, {"queue_capacity", "50000"}, {"batch_size", "200"},
		{"flush_ms", "100"}, {"movement_sample_rate", "20"}, {"metrics_interval_ticks", "20"},
		{"reconnect_seconds", "3"}, {"presence_enabled", "true"}, {"presence_include_address", "false"},
		{"transfer_enabled", "true"}, {"drop_receive_ids", ""}, {"drop_send_ids", ""},
	}
	var output strings.Builder
	output.WriteString("# Generated by Ninj-OS Edge Fabric for " + backendID + "\n")
	output.WriteString("# Keep this file private because it contains the companion shared secret.\n")
	output.WriteString("# This is not the dashboard username, password, or browser session token.\n")
	output.WriteString("# Effective secret fingerprint: " + secretFingerprint(secret) + "\n\n")
	output.WriteString("dashboard_host=" + dashboardHost + "\n")
	output.WriteString("dashboard_port=" + strconv.Itoa(dashboardPort) + "\n")
	output.WriteString("shared_secret=" + secret + "\n")
	output.WriteString("server_id=" + backendID + "\n\n")
	for _, item := range keys {
		output.WriteString(item.key + "=" + document.get("companion", item.key, item.fallback) + "\n")
	}
	return output.String(), nil
}

func (d *dashboard) companionArtifactStatus() map[string]any {
	status := map[string]any{
		"compiledAvailable": false,
		"artifactPath":      d.companionArtifactPath,
		"sourceAvailable":   fileExists(d.companionSourcePath),
		"sourcePath":        d.companionSourcePath,
	}
	if info, err := os.Stat(d.companionArtifactPath); err == nil {
		status["compiledAvailable"] = true
		status["compiledBytes"] = info.Size()
		status["compiledUpdatedAt"] = info.ModTime().UnixMilli()
		if bytes, err := os.ReadFile(d.companionArtifactPath); err == nil {
			sum := sha256.Sum256(bytes)
			status["compiledSHA256"] = hex.EncodeToString(sum[:])
		}
	}
	return status
}

func (d *dashboard) handleCompanionManager(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		document, err := loadINI(d.configPath)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		topology, _ := topologyFromUnified(document)
		backends := make([]map[string]any, 0, len(topology.Backends))
		for _, backend := range topology.Backends {
			properties, err := companionProperties(document, backend.ID)
			configured := err == nil
			preview := ""
			if configured {
				for _, line := range strings.Split(properties, "\n") {
					if strings.HasPrefix(line, "shared_secret=") {
						line = "shared_secret=[CONFIGURED]"
					}
					preview += line + "\n"
				}
			}
			secret := effectiveCompanionSecret(document, backend.ID)
			backends = append(backends, map[string]any{
				"id": backend.ID, "displayName": backend.DisplayName, "configured": configured,
				"secretFingerprint": secretFingerprint(secret),
				"connection":        d.companionStateSummary(backend.ID),
				"error": func() string {
					if err != nil {
						return err.Error()
					}
					return ""
				}(),
				"preview": strings.TrimSpace(preview),
			})
		}
		writeJSON(w, 200, map[string]any{"backends": backends, "artifact": d.companionArtifactStatus()})
	case http.MethodPost:
		d.handleCompanionArtifactUpload(w, r)
	case http.MethodDelete:
		if err := os.Remove(d.companionArtifactPath); err != nil && !os.IsNotExist(err) {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		d.appendAudit(r, principalFromRequest(r), "companion.artifact.delete", "success", nil)
		writeJSON(w, 200, map[string]bool{"deleted": true})
	default:
		writeJSON(w, 405, map[string]string{"error": "Method not allowed"})
	}
}

func extractCompanionSO(data []byte, filename string) ([]byte, error) {
	if strings.HasSuffix(strings.ToLower(filename), ".so") {
		return data, nil
	}
	if !strings.HasSuffix(strings.ToLower(filename), ".zip") {
		return nil, errors.New("Upload a compiled ninjos_proxie_companion.so or GitHub artifact ZIP")
	}
	return extractCompanionSOFromZip(data, 0)
}

func extractCompanionSOFromZip(data []byte, depth int) ([]byte, error) {
	if depth > 2 {
		return nil, errors.New("Companion artifact ZIP nesting is too deep")
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errors.New("Invalid ZIP archive")
	}
	for _, file := range reader.File {
		if filepath.Base(file.Name) != "ninjos_proxie_companion.so" {
			continue
		}
		handle, err := file.Open()
		if err != nil {
			return nil, err
		}
		content, err := io.ReadAll(io.LimitReader(handle, 100*1024*1024))
		_ = handle.Close()
		return content, err
	}
	for _, file := range reader.File {
		if !strings.HasSuffix(strings.ToLower(file.Name), ".zip") {
			continue
		}
		handle, err := file.Open()
		if err != nil {
			continue
		}
		nested, readErr := io.ReadAll(io.LimitReader(handle, 110*1024*1024))
		_ = handle.Close()
		if readErr != nil {
			continue
		}
		if plugin, nestedErr := extractCompanionSOFromZip(nested, depth+1); nestedErr == nil {
			return plugin, nil
		}
	}
	return nil, errors.New("ZIP does not contain ninjos_proxie_companion.so")
}

func validateCompanionELF(data []byte) error {
	if len(data) < 20 || data[0] != 0x7f || string(data[1:4]) != "ELF" {
		return errors.New("Companion artifact is not an ELF shared library")
	}
	if data[4] != 2 || data[5] != 1 {
		return errors.New("Companion artifact must be a 64-bit little-endian Linux library")
	}
	machine := uint16(data[18]) | uint16(data[19])<<8
	if machine != 62 {
		return errors.New("Companion artifact must target Linux x86-64")
	}
	return nil
}

func (d *dashboard) handleCompanionArtifactUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 120*1024*1024)
	if err := r.ParseMultipartForm(120 * 1024 * 1024); err != nil {
		writeJSON(w, 400, map[string]string{"error": "Invalid companion upload"})
		return
	}
	file, header, err := r.FormFile("artifact")
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "The artifact file is required"})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 110*1024*1024))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	plugin, err := extractCompanionSO(data, header.Filename)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if err := validateCompanionELF(plugin); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if err := os.MkdirAll(filepath.Dir(d.companionArtifactPath), 0700); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	temporary := d.companionArtifactPath + ".tmp"
	if err := os.WriteFile(temporary, plugin, 0600); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if err := os.Rename(temporary, d.companionArtifactPath); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	d.appendAudit(r, principalFromRequest(r), "companion.artifact.upload", "success", map[string]any{
		"filename": filepath.Base(header.Filename), "bytes": len(plugin),
	})
	writeJSON(w, 201, d.companionArtifactStatus())
}

func safeDownloadName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = regexp.MustCompile(`[^a-z0-9_-]+`).ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if value == "" {
		return "server"
	}
	return value
}

func (d *dashboard) handleCompanionDownload(w http.ResponseWriter, r *http.Request) {
	backendID := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("backend")))
	downloadType := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("type")))
	if downloadType == "" {
		downloadType = "properties"
	}
	document, err := loadINI(d.configPath)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if downloadType == "source" {
		if !fileExists(d.companionSourcePath) {
			writeJSON(w, 404, map[string]string{"error": "Companion source archive is not installed in this runtime"})
			return
		}
		serveDownload(w, d.companionSourcePath, "NinjOS-Endstone-Companion-v3.6.1-GitHub-Clean.zip", "application/zip")
		d.appendAudit(r, principalFromRequest(r), "companion.source.download", "success", nil)
		return
	}
	properties, err := companionProperties(document, backendID)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	name := safeDownloadName(backendID)
	if downloadType == "properties" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="companion-%s.properties"`, name))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = io.WriteString(w, properties)
		d.appendAudit(r, principalFromRequest(r), "companion.config.download", "success", map[string]string{"backend": backendID})
		return
	}
	if downloadType != "package" {
		writeJSON(w, 400, map[string]string{"error": "type must be properties, source, or package"})
		return
	}
	plugin, err := os.ReadFile(d.companionArtifactPath)
	if err != nil {
		writeJSON(w, 409, map[string]string{"error": "Upload the compiled companion .so or GitHub artifact ZIP before downloading an install-ready package"})
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="NinjOS-Companion-%s-Install.zip"`, name))
	archive := zip.NewWriter(w)
	pluginWriter, _ := archive.Create("plugins/ninjos_proxie_companion.so")
	_, _ = pluginWriter.Write(plugin)
	configWriter, _ := archive.Create("plugins/ninjos_proxie_companion/companion.properties")
	_, _ = io.WriteString(configWriter, properties)
	installWriter, _ := archive.Create("INSTALL.txt")
	_, _ = io.WriteString(installWriter, "Extract this ZIP into the Endstone server root while the server is stopped.\nDelete any old cached companion copy under plugins/.local/ before starting Endstone.\n")
	sum := sha256.Sum256(plugin)
	checksumWriter, _ := archive.Create("SHA256SUMS.txt")
	_, _ = io.WriteString(checksumWriter, hex.EncodeToString(sum[:])+"  plugins/ninjos_proxie_companion.so\n")
	_ = archive.Close()
	d.appendAudit(r, principalFromRequest(r), "companion.package.download", "success", map[string]string{"backend": backendID})
}

func serveDownload(w http.ResponseWriter, path, filename, contentType string) {
	file, err := os.Open(path)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, file)
}
