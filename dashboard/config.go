// Copyright (C) 2026 Ninj-OS contributors.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const unifiedConfigVersion = 1

type iniDocument struct {
	Sections map[string]map[string]string
}

func newINIDocument() *iniDocument {
	return &iniDocument{Sections: map[string]map[string]string{}}
}

func parseINI(content string) (*iniDocument, error) {
	document := newINIDocument()
	section := "edge"
	document.Sections[section] = map[string]string{}

	scanner := bufio.NewScanner(strings.NewReader(content))
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" || strings.HasPrefix(raw, "#") || strings.HasPrefix(raw, ";") {
			continue
		}
		if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
			section = strings.ToLower(strings.TrimSpace(raw[1 : len(raw)-1]))
			if section == "" {
				return nil, fmt.Errorf("line %d: empty section name", lineNumber)
			}
			if document.Sections[section] == nil {
				document.Sections[section] = map[string]string{}
			}
			continue
		}
		parts := strings.SplitN(raw, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("line %d: expected key = value", lineNumber)
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		value := strings.TrimSpace(parts[1])
		if key == "" {
			return nil, fmt.Errorf("line %d: empty key", lineNumber)
		}
		document.Sections[section][key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return document, nil
}

func loadINI(path string) (*iniDocument, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseINI(string(content))
}

func (d *iniDocument) section(name string) map[string]string {
	name = strings.ToLower(strings.TrimSpace(name))
	if d.Sections[name] == nil {
		d.Sections[name] = map[string]string{}
	}
	return d.Sections[name]
}

func (d *iniDocument) get(section, key, fallback string) string {
	values := d.Sections[strings.ToLower(section)]
	if values == nil {
		return fallback
	}
	value := strings.TrimSpace(values[strings.ToLower(key)])
	if value == "" {
		return fallback
	}
	return value
}

func (d *iniDocument) getBool(section, key string, fallback bool) bool {
	switch strings.ToLower(d.get(section, key, "")) {
	case "1", "true", "yes", "on", "enabled":
		return true
	case "0", "false", "no", "off", "disabled":
		return false
	default:
		return fallback
	}
}

func (d *iniDocument) getInt(section, key string, fallback, minimum, maximum int) int {
	value, err := strconv.Atoi(d.get(section, key, strconv.Itoa(fallback)))
	if err != nil || value < minimum || value > maximum {
		return fallback
	}
	return value
}

func resolveConfigValue(value string) string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(strings.ToLower(value), "env:") {
		return value
	}
	variable := strings.TrimSpace(value[4:])
	if variable == "" {
		return ""
	}
	return strings.TrimSpace(os.Getenv(variable))
}

func envNameFromReference(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(strings.ToLower(value), "env:") {
		return strings.TrimSpace(value[4:])
	}
	return ""
}

func defaultUnifiedConfig() string {
	return `# Ninj-OS Edge Fabric unified configuration
#
# Service secret values should normally reference environment variables:
#   default_secret = env:COMPANION_SHARED_SECRET
# Owner username and password are created in the browser setup wizard.
#
# A raw value also works, but environment references keep secrets out of
# downloadable configuration and support bundles.

[edge]
config_version = 1
instance_name = Ninj-OS Edge Fabric
public_host = 185.83.152.144
primary_allocation_port = 25566
managed_public_udp_ports = 25566,25571,25572-25581
primary_backend = kingdom
routing_mode = primary

[dashboard]
port = 25571
public_host = 185.83.152.144
operator_token = env:DASHBOARD_OPERATOR_TOKEN
viewer_token = env:DASHBOARD_READONLY_TOKEN
metrics_token = env:METRICS_TOKEN
totp_secret = env:DASHBOARD_TOTP_SECRET
session_minutes = 480

[session_core]
enabled = true
version = 1.26.30
advertised_version = 1.26.33
protocol_capture_enabled = true
protocol_capture_mode = metadata
protocol_observation_max_bytes = 10485760
protocol_capture_packet_ids = 30,77
protocol_capture_max_packet_bytes = 65536
protocol_capture_decode_failures = true
listen_host = 0.0.0.0
internal_token = env:SESSION_CORE_TOKEN
motd = Ninj-OS Proxie Network
sub_motd = Protected Bedrock Network
require_transfer_ticket = false
command_prefix = /
profiles_folder = runtime/session-core-profiles

[companion]
default_secret = env:COMPANION_SHARED_SECRET
presence_ttl_seconds = 120
capture_mode = metadata
selected_packet_ids = 30,77
payload_limit = 512
redact_packet_ids = 1,3,4
queue_capacity = 50000
batch_size = 200
flush_ms = 100
movement_sample_rate = 20
metrics_interval_ticks = 20
reconnect_seconds = 3
presence_enabled = true
presence_include_address = false
transfer_enabled = true
drop_receive_ids =
drop_send_ids =

[transfer]
enabled = true
public_host = 185.83.152.144
port_start = 25572
port_end = 25581
ticket_ttl_seconds = 20
require_source_ip = true

[firewall]
enabled = true
adaptive = true
risk_decay_per_minute = 5
risk_warning_threshold = 40
risk_ban_threshold = 100
progressive_ban_seconds = 30,300,3600,86400
max_datagram_size = 2048
max_packets_per_second_per_ip = 6000
global_packets_per_second = 250000
max_handshakes_per_minute = 30
max_sessions = 512
max_sessions_per_ip = 4

[incident]
enabled = true
trigger_packets_per_second = 180000
trigger_drop_ratio = 0.20
minimum_packets_per_second = 5000
recovery_seconds = 60
rate_divisor = 3
handshake_divisor = 3

[health]
enabled = true
interval_seconds = 5
timeout_ms = 1500
failure_threshold = 3
recovery_threshold = 2
automatic_actions = true
require_companion = false
companion_stale_seconds = 45
degraded_after_seconds = 30
stable_recovery_seconds = 60
minimum_tps = 12
maximum_mspt = 80

[logging]
packet_capture = true
capture_outgoing = true
packet_hex_preview_bytes = 0
packet_log_max_bytes = 52428800
event_log_max_bytes = 10485760
audit_log_max_bytes = 26214400
metric_sample_seconds = 15
metric_retention_days = 30

[discord]
webhook_url = env:DISCORD_WEBHOOK_URL
bot_token = env:DISCORD_BOT_TOKEN
channel_id = env:DISCORD_CHANNEL_ID
summary_minutes = 15

[backend.kingdom]
display_name = The Kingdom
host = 185.83.152.144
backend_port = 25565
public_port = 25566
enabled = true
fallback = false
protection_profile = kingdom
companion_secret = env:COMPANION_KINGDOM_SECRET
connection_mode = transparent
backend_adapter = endstone
backend_online_mode = true
require_proxy_identity = false
capacity = 50
fallback_backend =

[backend.zoo]
display_name = Zoo
host = 185.83.152.144
backend_port = 19431
public_port = 25571
enabled = true
fallback = false
protection_profile = zoo
companion_secret = env:COMPANION_ZOO_SECRET
connection_mode = transparent
backend_adapter = proxy_only
backend_online_mode = true
require_proxy_identity = false
capacity = 50
fallback_backend =

[profile.default]
max_datagram_size = 2048
max_packets_per_second_per_ip = 6000
max_handshakes_per_minute = 30
max_sessions_per_ip = 4
allow_new_sessions_during_incident = false

[profile.kingdom]
max_packets_per_second_per_ip = 7000
max_handshakes_per_minute = 35
max_sessions_per_ip = 5

[profile.zoo]
max_packets_per_second_per_ip = 4000
max_handshakes_per_minute = 20
max_sessions_per_ip = 3
`
}

func ensureUnifiedConfig(path string) error {
	if stat, err := os.Stat(path); err == nil && stat.Size() > 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(defaultUnifiedConfig()), 0600)
}

func serializeUnifiedConfig(document *iniDocument) (string, error) {
	if err := validateUnifiedConfig(document); err != nil {
		return "", err
	}

	ordered := []string{"edge", "dashboard", "session_core", "companion", "transfer", "firewall", "incident", "health", "logging", "discord"}
	backends := make([]string, 0)
	profiles := make([]string, 0)
	extras := make([]string, 0)
	for section := range document.Sections {
		switch {
		case strings.HasPrefix(section, "backend."):
			backends = append(backends, section)
		case strings.HasPrefix(section, "profile."):
			profiles = append(profiles, section)
		default:
			found := false
			for _, standard := range ordered {
				if section == standard {
					found = true
					break
				}
			}
			if !found {
				extras = append(extras, section)
			}
		}
	}
	sort.Strings(backends)
	sort.Strings(profiles)
	sort.Strings(extras)
	ordered = append(ordered, backends...)
	ordered = append(ordered, profiles...)
	ordered = append(ordered, extras...)

	keyOrder := map[string][]string{
		"edge":         {"config_version", "instance_name", "public_host", "primary_allocation_port", "managed_public_udp_ports", "primary_backend", "routing_mode"},
		"dashboard":    {"port", "public_host", "owner_username", "owner_token", "operator_token", "viewer_token", "metrics_token", "totp_secret", "session_minutes"},
		"session_core": {"enabled", "version", "advertised_version", "protocol_capture_enabled", "protocol_capture_mode", "protocol_observation_max_bytes", "protocol_capture_packet_ids", "protocol_capture_max_packet_bytes", "protocol_capture_decode_failures", "listen_host", "internal_token", "motd", "sub_motd", "require_transfer_ticket", "command_prefix", "profiles_folder"},
		"companion":    {"default_secret", "presence_ttl_seconds", "capture_mode", "selected_packet_ids", "payload_limit", "redact_packet_ids", "queue_capacity", "batch_size", "flush_ms", "movement_sample_rate", "metrics_interval_ticks", "reconnect_seconds", "presence_enabled", "presence_include_address", "transfer_enabled", "drop_receive_ids", "drop_send_ids"},
		"transfer":     {"enabled", "public_host", "port_start", "port_end", "ticket_ttl_seconds", "require_source_ip"},
		"firewall":     {"enabled", "adaptive", "risk_decay_per_minute", "risk_warning_threshold", "risk_ban_threshold", "progressive_ban_seconds", "max_datagram_size", "max_packets_per_second_per_ip", "global_packets_per_second", "max_handshakes_per_minute", "max_sessions", "max_sessions_per_ip"},
		"incident":     {"enabled", "trigger_packets_per_second", "trigger_drop_ratio", "minimum_packets_per_second", "recovery_seconds", "rate_divisor", "handshake_divisor"},
		"health":       {"enabled", "interval_seconds", "timeout_ms", "failure_threshold", "recovery_threshold", "automatic_actions", "require_companion", "companion_stale_seconds", "degraded_after_seconds", "stable_recovery_seconds", "minimum_tps", "maximum_mspt"},
		"logging":      {"packet_capture", "capture_outgoing", "packet_hex_preview_bytes", "packet_log_max_bytes", "event_log_max_bytes", "audit_log_max_bytes", "metric_sample_seconds", "metric_retention_days"},
		"discord":      {"webhook_url", "bot_token", "channel_id", "summary_minutes"},
	}

	var output strings.Builder
	output.WriteString("# Ninj-OS Edge Fabric unified configuration\n")
	output.WriteString("# Use env:VARIABLE for secrets stored in Pterodactyl Startup.\n\n")
	for _, section := range ordered {
		values := document.Sections[section]
		if values == nil {
			continue
		}
		output.WriteString("[" + section + "]\n")
		keys := keyOrder[section]
		if strings.HasPrefix(section, "backend.") {
			keys = []string{"display_name", "host", "backend_port", "public_port", "enabled", "fallback", "fallback_backend", "protection_profile", "connection_mode", "backend_adapter", "backend_online_mode", "require_proxy_identity", "capacity", "companion_secret"}
		} else if strings.HasPrefix(section, "profile.") {
			keys = []string{"max_datagram_size", "max_packets_per_second_per_ip", "max_handshakes_per_minute", "max_sessions_per_ip", "allow_new_sessions_during_incident"}
		}
		written := map[string]bool{}
		for _, key := range keys {
			if value, ok := values[key]; ok {
				output.WriteString(key + " = " + value + "\n")
				written[key] = true
			}
		}
		remaining := make([]string, 0)
		for key := range values {
			if !written[key] {
				remaining = append(remaining, key)
			}
		}
		sort.Strings(remaining)
		for _, key := range remaining {
			output.WriteString(key + " = " + values[key] + "\n")
		}
		output.WriteString("\n")
	}

	return output.String(), nil
}

func writeUnifiedConfig(path string, document *iniDocument) error {
	content, err := serializeUnifiedConfig(document)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	temporary := path + ".tmp"
	backup := path + ".bak"
	if current, err := os.ReadFile(path); err == nil && len(current) > 0 {
		if err := os.WriteFile(backup, current, 0600); err != nil {
			return err
		}
	}
	if err := os.WriteFile(temporary, []byte(content), 0600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func validateUnifiedConfig(document *iniDocument) error {
	version := document.getInt("edge", "config_version", unifiedConfigVersion, 1, 100)
	if version != unifiedConfigVersion {
		return fmt.Errorf("unsupported config_version %d", version)
	}
	topology, err := topologyFromUnified(document)
	if err != nil {
		return err
	}
	if err := validateTopology(topology); err != nil {
		return err
	}
	dashboardPort := document.getInt("dashboard", "port", 25571, 1, 65535)
	transferStart := document.getInt("transfer", "port_start", 25572, 1, 65535)
	transferEnd := document.getInt("transfer", "port_end", 25581, 1, 65535)
	if transferEnd < transferStart {
		return errors.New("transfer.port_end must be greater than or equal to transfer.port_start")
	}
	captureMode := document.get("session_core", "protocol_capture_mode", "metadata")
	validCaptureModes := map[string]bool{"metadata": true, "decoded": true, "wire": true, "full": true, "redacted_payload": true}
	if !validCaptureModes[captureMode] {
		return fmt.Errorf("session_core.protocol_capture_mode must be metadata, decoded, wire, or full")
	}
	for _, raw := range strings.Split(document.get("session_core", "protocol_capture_packet_ids", "30,77"), ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		packetID, packetErr := strconv.Atoi(raw)
		if packetErr != nil || packetID < 0 || packetID > 1023 {
			return fmt.Errorf("session_core.protocol_capture_packet_ids contains invalid packet ID %q", raw)
		}
	}
	for key, bounds := range map[string][2]int{
		"protocol_capture_max_packet_bytes": {64, 1048576},
		"protocol_observation_max_bytes":    {65536, 1073741824},
	} {
		raw := strings.TrimSpace(document.get("session_core", key, ""))
		if raw == "" {
			continue
		}
		value, valueErr := strconv.Atoi(raw)
		if valueErr != nil || value < bounds[0] || value > bounds[1] {
			return fmt.Errorf("session_core.%s must be between %d and %d", key, bounds[0], bounds[1])
		}
	}
	if err := validateUnifiedSecrets(document); err != nil {
		return err
	}
	for _, backend := range topology.Backends {
		if backend.PublicPort == dashboardPort {
			// Same numeric port is valid because the backend route is UDP and dashboard is TCP.
			continue
		}
	}
	return nil
}

func validateUnifiedSecrets(document *iniDocument) error {
	ids := []string{
		"dashboard.operator_token", "dashboard.viewer_token", "dashboard.metrics_token",
		"dashboard.totp_secret", "session_core.internal_token", "companion.default_secret",
		"discord.webhook_url", "discord.bot_token",
	}
	for section := range document.Sections {
		if strings.HasPrefix(section, "backend.") {
			ids = append(ids, section+".companion_secret")
		}
	}
	for _, id := range ids {
		location, err := secretLocationForID(document, id)
		if err != nil {
			return err
		}
		reference := strings.TrimSpace(document.get(location.Section, location.Key, ""))
		if reference == "" {
			continue
		}
		value := reference
		if variable := envNameFromReference(reference); variable != "" {
			value = strings.TrimSpace(os.Getenv(variable))
			// Optional Startup variables may be populated only when the service
			// restarts. The Vault rejects empty variables when selecting them.
			if value == "" {
				continue
			}
		}
		if err := validateManagedSecret(location, value); err != nil {
			return fmt.Errorf("%s: %w", location.Label, err)
		}
	}
	return nil
}

func topologyFromUnified(document *iniDocument) (topologyConfig, error) {
	topology := topologyConfig{
		Version:        topologySchemaVersion,
		PrimaryBackend: document.get("edge", "primary_backend", ""),
		RoutingMode:    document.get("edge", "routing_mode", "primary"),
	}
	sections := make([]string, 0)
	for section := range document.Sections {
		if strings.HasPrefix(section, "backend.") {
			sections = append(sections, section)
		}
	}
	sort.Strings(sections)
	for _, section := range sections {
		id := strings.TrimPrefix(section, "backend.")
		values := document.Sections[section]
		secretReference := strings.TrimSpace(values["companion_secret"])
		topology.Backends = append(topology.Backends, backendDefinition{
			ID:                   id,
			DisplayName:          valueOr(values, "display_name", humanizeBackendID(id)),
			Host:                 valueOr(values, "host", "127.0.0.1"),
			BackendPort:          intOr(values, "backend_port", 19132),
			PublicPort:           intOr(values, "public_port", 0),
			Fallback:             boolOr(values, "fallback", false),
			FallbackBackend:      valueOr(values, "fallback_backend", ""),
			Profile:              valueOr(values, "protection_profile", id),
			Enabled:              boolOr(values, "enabled", true),
			ConnectionMode:       valueOr(values, "connection_mode", "transparent"),
			BackendAdapter:       valueOr(values, "backend_adapter", "proxy_only"),
			BackendOnlineMode:    boolOr(values, "backend_online_mode", true),
			RequireProxyIdentity: boolOr(values, "require_proxy_identity", false),
			Capacity:             intOr(values, "capacity", 50),
			CompanionSecretEnv:   envNameFromReference(secretReference),
		})
	}
	if topology.PrimaryBackend == "" && len(topology.Backends) > 0 {
		topology.PrimaryBackend = topology.Backends[0].ID
	}
	return topology, validateTopology(topology)
}

func applyTopologyToUnified(document *iniDocument, topology topologyConfig) error {
	if err := validateTopology(topology); err != nil {
		return err
	}
	existingSecrets := map[string]string{}
	for section, values := range document.Sections {
		if strings.HasPrefix(section, "backend.") {
			existingSecrets[strings.TrimPrefix(section, "backend.")] = values["companion_secret"]
			delete(document.Sections, section)
		}
	}
	edge := document.section("edge")
	edge["primary_backend"] = topology.PrimaryBackend
	edge["routing_mode"] = topology.RoutingMode
	for _, backend := range topology.Backends {
		section := document.section("backend." + backend.ID)
		section["display_name"] = backend.DisplayName
		section["host"] = backend.Host
		section["backend_port"] = strconv.Itoa(backend.BackendPort)
		section["public_port"] = strconv.Itoa(backend.PublicPort)
		section["enabled"] = strconv.FormatBool(backend.Enabled)
		section["fallback"] = strconv.FormatBool(backend.Fallback)
		section["fallback_backend"] = backend.FallbackBackend
		section["protection_profile"] = backend.Profile
		section["connection_mode"] = backend.ConnectionMode
		section["backend_adapter"] = backend.BackendAdapter
		section["backend_online_mode"] = strconv.FormatBool(backend.BackendOnlineMode)
		section["require_proxy_identity"] = strconv.FormatBool(backend.RequireProxyIdentity)
		section["capacity"] = strconv.Itoa(backend.Capacity)
		// Credential sources are exclusively managed through the Secret Vault.
		// A topology save must never rotate or erase an existing secret.
		section["companion_secret"] = existingSecrets[backend.ID]
	}
	return nil
}

func valueOr(values map[string]string, key, fallback string) string {
	if value := strings.TrimSpace(values[key]); value != "" {
		return value
	}
	return fallback
}

func intOr(values map[string]string, key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(values[key]))
	if err != nil {
		return fallback
	}
	return value
}

func boolOr(values map[string]string, key string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(values[key])) {
	case "1", "true", "yes", "on", "enabled":
		return true
	case "0", "false", "no", "off", "disabled":
		return false
	default:
		return fallback
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func prepareUnifiedConfig(configPath, runtimeDir, gatewayPath string) error {
	if err := ensureUnifiedConfig(configPath); err != nil {
		return err
	}
	document, err := loadINI(configPath)
	if err != nil {
		return err
	}
	if err := validateUnifiedConfig(document); err != nil {
		return fmt.Errorf("invalid unified configuration: %w", err)
	}
	topology, _ := topologyFromUnified(document)
	if err := os.MkdirAll(filepath.Join(runtimeDir, "generated"), 0755); err != nil {
		return err
	}

	backendEntries := make([]string, 0, len(topology.Backends))
	routeEntries := make([]string, 0, len(topology.Backends))
	fullProxyBackends := make([]map[string]any, 0)
	fullProxyEnabledCount := 0
	transparentCount := 0
	defaultSecret := resolveConfigValue(document.get("companion", "default_secret", "env:COMPANION_SHARED_SECRET"))
	companionLines := []string{"default=" + defaultSecret}
	for _, backend := range topology.Backends {
		section := document.section("backend." + backend.ID)
		secret := resolveConfigValue(section["companion_secret"])
		if secret == "" {
			secret = defaultSecret
		}
		companionLines = append(companionLines, backend.ID+"="+secret)

		if backend.ConnectionMode == "full_proxy" {
			if backend.Enabled {
				fullProxyEnabledCount++
			}
			fullProxyBackends = append(fullProxyBackends, map[string]any{
				"id":                   backend.ID,
				"displayName":          backend.DisplayName,
				"host":                 backend.Host,
				"backendPort":          backend.BackendPort,
				"publicPort":           backend.PublicPort,
				"enabled":              backend.Enabled,
				"adapter":              backend.BackendAdapter,
				"requireProxyIdentity": backend.RequireProxyIdentity,
				"capacity":             backend.Capacity,
				"fallbackBackend":      backend.FallbackBackend,
				"companionSecret":      secret,
			})
			continue
		}

		transparentCount++
		backendEntries = append(backendEntries, strings.Join([]string{
			backend.ID, backend.Host, strconv.Itoa(backend.BackendPort),
			strconv.FormatBool(backend.Fallback), backend.Profile, strconv.FormatBool(backend.Enabled),
		}, "|"))
		if backend.PublicPort > 0 {
			routeEntries = append(routeEntries, fmt.Sprintf("%d|%s", backend.PublicPort, backend.ID))
		}
	}

	gatewayPrimary := topology.PrimaryBackend
	if transparentCount > 0 {
		found := false
		for _, backend := range topology.Backends {
			if backend.ConnectionMode != "full_proxy" && backend.ID == gatewayPrimary {
				found = true
				break
			}
		}
		if !found {
			for _, backend := range topology.Backends {
				if backend.ConnectionMode != "full_proxy" {
					gatewayPrimary = backend.ID
					break
				}
			}
		}
	}

	gateway := fmt.Sprintf(`listen_port=%d
runtime_dir=%s
topology_file=%s
backends=%s
static_routes=%s
primary_backend=%s
routing_mode=%s

transfer_enabled=%t
transfer_port_start=%d
transfer_port_end=%d
transfer_ticket_file=%s
transfer_ticket_reload_ms=200
transfer_require_source_ip=%t

idle_timeout_seconds=45
handshake_timeout_seconds=12
cleanup_interval_seconds=5
max_sessions=%d
max_sessions_per_ip=%d

firewall_enabled=%t
adaptive_firewall_enabled=%t
risk_decay_per_minute=%d
risk_warning_threshold=%d
risk_ban_threshold=%d
progressive_ban_seconds=%s
firewall_allowlist_file=%s
firewall_denylist_file=%s
firewall_bans_file=%s
protection_profiles_file=%s
max_datagram_size=%d
max_packets_per_second_per_ip=%d
global_packets_per_second=%d
max_handshakes_per_minute=%d
strike_limit=10
temp_ban_seconds=300

ping_cache_enabled=true
ping_cache_refresh_seconds=3
ping_limit_per_second_per_ip=15
maintenance_motd=The Kingdom | Maintenance
drain_motd=The Kingdom | Restarting Soon

health_enabled=%t
health_interval_seconds=%d
health_timeout_ms=%d
health_failure_threshold=%d
health_recovery_threshold=%d

incident_mode_enabled=%t
incident_trigger_packets_per_second=%d
incident_trigger_drop_ratio=%s
incident_min_packets_per_second=%d
incident_recovery_seconds=%d
incident_rate_divisor=%d
incident_handshake_divisor=%d

packet_capture_enabled=%t
capture_outgoing=%t
packet_hex_preview_bytes=%d
packet_log_max_bytes=%d
event_log_max_bytes=%d
socket_receive_buffer=8388608
socket_send_buffer=8388608
stats_interval_seconds=30
state_interval_ms=1000
command_poll_ms=250
live_config_file=%s
live_config_reload_ms=1000
`,
		document.getInt("edge", "primary_allocation_port", 25566, 1, 65535),
		runtimeDir,
		filepath.Join(runtimeDir, "gateway-topology.properties"),
		strings.Join(backendEntries, ";"), strings.Join(routeEntries, ";"),
		gatewayPrimary, topology.RoutingMode,
		document.getBool("transfer", "enabled", true),
		document.getInt("transfer", "port_start", 25572, 1, 65535),
		document.getInt("transfer", "port_end", 25581, 1, 65535),
		filepath.Join(runtimeDir, "transfer-tickets.tsv"),
		document.getBool("transfer", "require_source_ip", true),
		document.getInt("firewall", "max_sessions", 512, 1, 100000),
		document.getInt("firewall", "max_sessions_per_ip", 4, 1, 1000),
		document.getBool("firewall", "enabled", true),
		document.getBool("firewall", "adaptive", true),
		document.getInt("firewall", "risk_decay_per_minute", 5, 0, 1000),
		document.getInt("firewall", "risk_warning_threshold", 40, 1, 10000),
		document.getInt("firewall", "risk_ban_threshold", 100, 2, 10000),
		document.get("firewall", "progressive_ban_seconds", "30,300,3600,86400"),
		filepath.Join(runtimeDir, "firewall-allowlist.txt"),
		filepath.Join(runtimeDir, "firewall-denylist.txt"),
		filepath.Join(runtimeDir, "firewall-bans.tsv"),
		filepath.Join(runtimeDir, "protection-profiles.properties"),
		document.getInt("firewall", "max_datagram_size", 2048, 512, 65535),
		document.getInt("firewall", "max_packets_per_second_per_ip", 6000, 100, 1000000),
		document.getInt("firewall", "global_packets_per_second", 250000, 1000, 10000000),
		document.getInt("firewall", "max_handshakes_per_minute", 30, 1, 100000),
		document.getBool("health", "enabled", true),
		document.getInt("health", "interval_seconds", 5, 1, 300),
		document.getInt("health", "timeout_ms", 1500, 100, 30000),
		document.getInt("health", "failure_threshold", 3, 1, 100),
		document.getInt("health", "recovery_threshold", 2, 1, 100),
		document.getBool("incident", "enabled", true),
		document.getInt("incident", "trigger_packets_per_second", 180000, 1000, 10000000),
		document.get("incident", "trigger_drop_ratio", "0.20"),
		document.getInt("incident", "minimum_packets_per_second", 5000, 100, 10000000),
		document.getInt("incident", "recovery_seconds", 60, 5, 3600),
		document.getInt("incident", "rate_divisor", 3, 1, 100),
		document.getInt("incident", "handshake_divisor", 3, 1, 100),
		document.getBool("logging", "packet_capture", true),
		document.getBool("logging", "capture_outgoing", true),
		document.getInt("logging", "packet_hex_preview_bytes", 0, 0, 2048),
		document.getInt("logging", "packet_log_max_bytes", 52428800, 1048576, 1073741824),
		document.getInt("logging", "event_log_max_bytes", 10485760, 1048576, 1073741824),
		filepath.Join(runtimeDir, "live-config.properties"),
	)
	if err := os.WriteFile(gatewayPath, []byte(gateway), 0600); err != nil {
		return err
	}

	sessionCoreConfig := map[string]any{
		"schemaVersion":                 1,
		"enabled":                       document.getBool("session_core", "enabled", true),
		"version":                       document.get("session_core", "version", "1.26.30"),
		"advertisedVersion":             document.get("session_core", "advertised_version", "1.26.33"),
		"protocolPackDirectory":         filepath.Join("session-core", "protocol-packs"),
		"protocolObservationDirectory":  filepath.Join(runtimeDir, "protocol-observations"),
		"protocolCaptureEnabled":        document.getBool("session_core", "protocol_capture_enabled", true),
		"protocolCaptureMode":           document.get("session_core", "protocol_capture_mode", "metadata"),
		"protocolObservationMaxBytes":   document.getInt("session_core", "protocol_observation_max_bytes", 10485760, 65536, 1073741824),
		"protocolCapturePacketIds":      document.get("session_core", "protocol_capture_packet_ids", "30,77"),
		"protocolCaptureMaxPacketBytes": document.getInt("session_core", "protocol_capture_max_packet_bytes", 65536, 64, 1048576),
		"protocolCaptureDecodeFailures": document.getBool("session_core", "protocol_capture_decode_failures", true),
		"listenHost":                    document.get("session_core", "listen_host", "0.0.0.0"),
		"publicHost":                    document.get("edge", "public_host", "127.0.0.1"),
		"dashboardUrl":                  "http://127.0.0.1:" + strconv.Itoa(document.getInt("dashboard", "port", 25571, 1, 65535)),
		"internalToken":                 resolveConfigValue(document.get("session_core", "internal_token", "env:SESSION_CORE_TOKEN")),
		"motd":                          document.get("session_core", "motd", "Ninj-OS Proxie Network"),
		"subMotd":                       document.get("session_core", "sub_motd", "Protected Bedrock Network"),
		"requireTransferTicket":         document.getBool("session_core", "require_transfer_ticket", false),
		"profilesFolder":                document.get("session_core", "profiles_folder", filepath.Join(runtimeDir, "session-core-profiles")),
		"stateFile":                     filepath.Join(runtimeDir, "session-core-state.json"),
		"primaryBackend":                topology.PrimaryBackend,
		"backends":                      fullProxyBackends,
	}
	sessionBytes, _ := json.MarshalIndent(sessionCoreConfig, "", "  ")
	if err := os.WriteFile(filepath.Join(runtimeDir, "session-core.json"), sessionBytes, 0600); err != nil {
		return err
	}

	if err := os.WriteFile(filepath.Join(runtimeDir, "companion-secrets.properties"), []byte(strings.Join(companionLines, "\n")+"\n"), 0600); err != nil {
		return err
	}

	profileSections := make([]string, 0)
	for section := range document.Sections {
		if strings.HasPrefix(section, "profile.") {
			profileSections = append(profileSections, section)
		}
	}
	sort.Strings(profileSections)
	var profiles strings.Builder
	profiles.WriteString("# Generated from edge-fabric.ini. Edit the unified config or use the dashboard.\n")
	for _, section := range profileSections {
		name := strings.TrimPrefix(section, "profile.")
		keys := []string{"max_datagram_size", "max_packets_per_second_per_ip", "max_handshakes_per_minute", "max_sessions_per_ip", "allow_new_sessions_during_incident"}
		for _, key := range keys {
			if value := strings.TrimSpace(document.Sections[section][key]); value != "" {
				profiles.WriteString(name + "." + key + "=" + value + "\n")
			}
		}
	}
	if err := os.WriteFile(filepath.Join(runtimeDir, "protection-profiles.properties"), []byte(profiles.String()), 0644); err != nil {
		return err
	}

	healthActions := fmt.Sprintf("enabled=%t\nrequire_companion=%t\ncompanion_stale_seconds=%d\ndegraded_after_seconds=%d\nrecovery_seconds=%d\nminimum_tps=%d\nmaximum_mspt=%d\n",
		document.getBool("health", "automatic_actions", true),
		document.getBool("health", "require_companion", false),
		document.getInt("health", "companion_stale_seconds", 45, 5, 3600),
		document.getInt("health", "degraded_after_seconds", 30, 5, 3600),
		document.getInt("health", "stable_recovery_seconds", 60, 5, 3600),
		document.getInt("health", "minimum_tps", 12, 1, 20),
		document.getInt("health", "maximum_mspt", 80, 1, 10000),
	)
	if err := os.WriteFile(filepath.Join(runtimeDir, "health-actions.properties"), []byte(healthActions), 0644); err != nil {
		return err
	}

	ownerToken := resolveConfigValue(document.get("dashboard", "owner_token", ""))
	operatorToken := resolveConfigValue(document.get("dashboard", "operator_token", "env:DASHBOARD_OPERATOR_TOKEN"))
	viewerToken := resolveConfigValue(document.get("dashboard", "viewer_token", "env:DASHBOARD_READONLY_TOKEN"))
	metricsToken := resolveConfigValue(document.get("dashboard", "metrics_token", "env:METRICS_TOKEN"))
	if metricsToken == "" {
		metricsToken = ownerToken
	}
	dashboardEnv := map[string]string{
		"EDGE_CONFIG_FILE":            configPath,
		"TOPOLOGY_FILE":               filepath.Join(runtimeDir, "topology.properties"),
		"RUNTIME_DIR":                 runtimeDir,
		"DASHBOARD_PORT":              strconv.Itoa(document.getInt("dashboard", "port", 25571, 1, 65535)),
		"DASHBOARD_TITLE":             document.get("edge", "instance_name", "Ninj-OS Edge Fabric"),
		"DASHBOARD_OWNER_USERNAME":    document.get("dashboard", "owner_username", "owner"),
		"DASHBOARD_TOKEN":             ownerToken,
		"DASHBOARD_OPERATOR_TOKEN":    operatorToken,
		"DASHBOARD_READONLY_TOKEN":    viewerToken,
		"METRICS_TOKEN":               metricsToken,
		"DASHBOARD_TOTP_SECRET":       resolveConfigValue(document.get("dashboard", "totp_secret", "env:DASHBOARD_TOTP_SECRET")),
		"DASHBOARD_SESSION_MINUTES":   strconv.Itoa(document.getInt("dashboard", "session_minutes", 480, 15, 10080)),
		"COMPANION_SHARED_SECRET":     defaultSecret,
		"PRESENCE_TTL_SECONDS":        strconv.Itoa(document.getInt("companion", "presence_ttl_seconds", 120, 30, 3600)),
		"TRANSFER_PUBLIC_HOST":        document.get("transfer", "public_host", document.get("edge", "public_host", "127.0.0.1")),
		"TRANSFER_PORT_START":         strconv.Itoa(document.getInt("transfer", "port_start", 25572, 1, 65535)),
		"TRANSFER_PORT_END":           strconv.Itoa(document.getInt("transfer", "port_end", 25581, 1, 65535)),
		"TRANSFER_TICKET_TTL_SECONDS": strconv.Itoa(document.getInt("transfer", "ticket_ttl_seconds", 20, 5, 300)),
		"SESSION_CORE_TOKEN":          resolveConfigValue(document.get("session_core", "internal_token", "env:SESSION_CORE_TOKEN")),
		"SESSION_CORE_CONFIG":         filepath.Join(runtimeDir, "session-core.json"),
		"TRANSPARENT_BACKEND_COUNT":   strconv.Itoa(transparentCount),
		"FULL_PROXY_BACKEND_COUNT":    strconv.Itoa(fullProxyEnabledCount),
		"TRANSFER_REQUIRE_SOURCE_IP":  strconv.FormatBool(document.getBool("transfer", "require_source_ip", true)),
		"MANAGED_PUBLIC_UDP_PORTS":    document.get("edge", "managed_public_udp_ports", ""),
		"DASHBOARD_PUBLIC_HOST":       document.get("dashboard", "public_host", document.get("edge", "public_host", "127.0.0.1")),
		"AUDIT_LOG_MAX_BYTES":         strconv.Itoa(document.getInt("logging", "audit_log_max_bytes", 26214400, 1048576, 1073741824)),
		"METRIC_SAMPLE_SECONDS":       strconv.Itoa(document.getInt("logging", "metric_sample_seconds", 15, 1, 3600)),
		"METRIC_RETENTION_DAYS":       strconv.Itoa(document.getInt("logging", "metric_retention_days", 30, 1, 3650)),
		"DISCORD_WEBHOOK_URL":         resolveConfigValue(document.get("discord", "webhook_url", "env:DISCORD_WEBHOOK_URL")),
		"DISCORD_BOT_TOKEN":           resolveConfigValue(document.get("discord", "bot_token", "env:DISCORD_BOT_TOKEN")),
		"DISCORD_CHANNEL_ID":          resolveConfigValue(document.get("discord", "channel_id", "env:DISCORD_CHANNEL_ID")),
		"DISCORD_SUMMARY_MINUTES":     strconv.Itoa(document.getInt("discord", "summary_minutes", 15, 1, 1440)),
	}
	envKeys := make([]string, 0, len(dashboardEnv))
	for key := range dashboardEnv {
		envKeys = append(envKeys, key)
	}
	sort.Strings(envKeys)
	var envOutput strings.Builder
	for _, key := range envKeys {
		envOutput.WriteString("export " + key + "=" + shellQuote(dashboardEnv[key]) + "\n")
	}
	if err := os.WriteFile(filepath.Join(runtimeDir, "generated", "dashboard.env"), []byte(envOutput.String()), 0600); err != nil {
		return err
	}

	topologyOutput := serializeTopology(topology)
	if err := os.WriteFile(filepath.Join(runtimeDir, "topology.properties"), []byte(topologyOutput), 0644); err != nil {
		return err
	}

	gatewayTopology := topologyConfig{
		Version:        topologySchemaVersion,
		PrimaryBackend: gatewayPrimary,
		RoutingMode:    topology.RoutingMode,
	}
	for _, backend := range topology.Backends {
		if backend.ConnectionMode != "full_proxy" {
			gatewayTopology.Backends = append(gatewayTopology.Backends, backend)
		}
	}
	if err := os.WriteFile(filepath.Join(runtimeDir, "gateway-topology.properties"), []byte(serializeTopology(gatewayTopology)), 0644); err != nil {
		return err
	}

	summary := map[string]any{
		"configVersion": unifiedConfigVersion,
		"configPath":    configPath,
		"instanceName":  document.get("edge", "instance_name", "Ninj-OS Edge Fabric"),
		"publicHost":    document.get("edge", "public_host", ""),
		"dashboardPort": document.getInt("dashboard", "port", 25571, 1, 65535),
		"backends":      topology.Backends,
		"secretSources": map[string]any{
			"dashboardOperator": secretSourceSummary(document.get("dashboard", "operator_token", "")),
			"dashboardViewer":   secretSourceSummary(document.get("dashboard", "viewer_token", "")),
			"dashboardTOTP":     secretSourceSummary(document.get("dashboard", "totp_secret", "")),
			"companionDefault":  secretSourceSummary(document.get("companion", "default_secret", "")),
		},
	}
	summaryBytes, _ := json.MarshalIndent(summary, "", "  ")
	return os.WriteFile(filepath.Join(runtimeDir, "config-summary.json"), summaryBytes, 0644)
}

func migrateLegacyRoutes(legacyPath, configPath string) error {
	if err := ensureUnifiedConfig(configPath); err != nil {
		return err
	}
	document, err := loadINI(configPath)
	if err != nil {
		return err
	}
	content, err := os.ReadFile(legacyPath)
	if err != nil {
		return err
	}
	legacy := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		key := strings.TrimSpace(parts[0])
		value := strings.Trim(strings.TrimSpace(parts[1]), "'\"")
		legacy[key] = value
	}
	topologyContent := fmt.Sprintf("version=1\nprimary_backend=%s\nrouting_mode=%s\nbackends=%s\nstatic_routes=%s\n",
		valueOr(legacy, "PRIMARY_BACKEND", "kingdom"),
		valueOr(legacy, "ROUTING_MODE", "primary"),
		valueOr(legacy, "BACKENDS", ""),
		valueOr(legacy, "STATIC_ROUTES", ""),
	)
	topology, err := parseTopologyProperties(topologyContent)
	if err == nil && len(topology.Backends) > 0 {
		if err := applyTopologyToUnified(document, topology); err != nil {
			return err
		}
	}
	if value := legacy["DASHBOARD_PORT"]; value != "" {
		document.section("dashboard")["port"] = value
	}
	if value := legacy["DASHBOARD_PUBLIC_HOST"]; value != "" {
		document.section("dashboard")["public_host"] = value
	}
	if value := legacy["TRANSFER_PUBLIC_HOST"]; value != "" {
		document.section("transfer")["public_host"] = value
	}
	if value := legacy["TRANSFER_PORT_START"]; value != "" {
		document.section("transfer")["port_start"] = value
	}
	if value := legacy["TRANSFER_PORT_END"]; value != "" {
		document.section("transfer")["port_end"] = value
	}
	return writeUnifiedConfig(configPath, document)
}

func secretSourceSummary(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unset"
	}
	if variable := envNameFromReference(value); variable != "" {
		return "env:" + variable
	}
	return "dashboard-managed"
}

func isSecretConfigKey(section, key string) bool {
	section = strings.ToLower(section)
	key = strings.ToLower(key)
	if section == "dashboard" {
		switch key {
		case "owner_token", "operator_token", "viewer_token", "metrics_token", "totp_secret":
			return true
		}
	}
	if section == "companion" && key == "default_secret" {
		return true
	}
	if section == "session_core" && key == "internal_token" {
		return true
	}
	if section == "discord" && (key == "webhook_url" || key == "bot_token") {
		return true
	}
	return strings.HasPrefix(section, "backend.") && key == "companion_secret"
}

func redactUnifiedDocument(document *iniDocument) *iniDocument {
	redacted := newINIDocument()
	for section, values := range document.Sections {
		target := redacted.section(section)
		for key, value := range values {
			if isSecretConfigKey(section, key) && value != "" && !strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "env:") {
				target[key] = "[REDACTED]"
			} else {
				target[key] = value
			}
		}
	}
	return redacted
}

func mergeRedactedSecrets(candidate, current *iniDocument) {
	for section, values := range candidate.Sections {
		for key, value := range values {
			if value != "[REDACTED]" || !isSecretConfigKey(section, key) {
				continue
			}
			if currentValues := current.Sections[section]; currentValues != nil {
				values[key] = currentValues[key]
			}
		}
	}
}

func preserveVaultManagedSecrets(candidate, current *iniDocument) {
	for section, currentValues := range current.Sections {
		candidateValues := candidate.Sections[section]
		if candidateValues == nil {
			continue
		}
		for key, value := range currentValues {
			if !isSecretConfigKey(section, key) {
				continue
			}
			candidateValues[key] = value
		}
	}
	// A backend created in the advanced editor starts without credentials and
	// must be configured through the Vault after it exists.
	for section, values := range candidate.Sections {
		if current.Sections[section] != nil {
			continue
		}
		for key := range values {
			if isSecretConfigKey(section, key) {
				values[key] = ""
			}
		}
	}
}

func configRevision(document *iniDocument) string {
	content, err := serializeUnifiedConfig(document)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func renderUnifiedConfig(document *iniDocument) string {
	content, err := serializeUnifiedConfig(document)
	if err != nil {
		return ""
	}
	return content
}

func iniSectionEqual(left, right *iniDocument, section string) bool {
	leftValues := left.Sections[section]
	rightValues := right.Sections[section]
	if len(leftValues) != len(rightValues) {
		return false
	}
	for key, value := range leftValues {
		if rightValues[key] != value {
			return false
		}
	}
	return true
}

func iniKeyChanged(left, right *iniDocument, section, key string) bool {
	return left.get(section, key, "") != right.get(section, key, "")
}

func unifiedConfigRestartScope(current, candidate *iniDocument) string {
	for _, section := range []string{"dashboard", "companion", "transfer", "logging", "discord"} {
		if !iniSectionEqual(current, candidate, section) {
			return "services"
		}
	}
	for _, key := range []string{"instance_name", "public_host"} {
		if iniKeyChanged(current, candidate, "edge", key) {
			return "services"
		}
	}
	return "gateway"
}

func (d *dashboard) writeAndVerifyUnifiedConfig(candidate, current *iniDocument) error {
	d.fileMu.Lock()
	currentContent, _ := serializeUnifiedConfig(current)
	if err := writeUnifiedConfig(d.configPath, candidate); err != nil {
		d.fileMu.Unlock()
		return err
	}
	rollback := func(cause error) error {
		if currentContent != "" {
			_ = os.WriteFile(d.configPath, []byte(currentContent), 0600)
			_ = prepareUnifiedConfig(d.configPath, d.runtimeDir, d.gatewayConfigPath)
		}
		d.fileMu.Unlock()
		return cause
	}
	if err := prepareUnifiedConfig(d.configPath, d.runtimeDir, d.gatewayConfigPath); err != nil {
		return rollback(fmt.Errorf("configuration generation failed: %w", err))
	}
	persisted, err := loadINI(d.configPath)
	if err != nil {
		return rollback(fmt.Errorf("configuration could not be read back: %w", err))
	}
	candidateContent, _ := serializeUnifiedConfig(candidate)
	persistedContent, _ := serializeUnifiedConfig(persisted)
	if candidateContent != persistedContent {
		return rollback(errors.New("configuration verification failed; previous configuration restored"))
	}
	d.fileMu.Unlock()

	// Transfer ticket cleanup has its own file lock. Run it only after the
	// configuration transaction releases the lock to avoid a self-deadlock.
	if topology, topologyErr := topologyFromUnified(candidate); topologyErr == nil {
		if err := d.removeTicketsForStaticPorts(topology); err != nil {
			d.appendJSONL(filepath.Join(d.runtimeDir, "events.jsonl"), map[string]any{
				"timestamp": time.Now().UnixMilli(), "type": "transfer.pool_cleanup_failed",
				"severity": "warning", "message": err.Error(),
			}, 20*1024*1024)
		}
	}
	return nil
}

func writeConfigSaveStatus(runtimeDir, revision, action, restartScope string) error {
	payload := map[string]any{
		"revision":     revision,
		"action":       action,
		"restartScope": restartScope,
		"savedAt":      time.Now().UnixMilli(),
	}
	bytes, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	temporary := filepath.Join(runtimeDir, "config-save-status.json.tmp")
	final := filepath.Join(runtimeDir, "config-save-status.json")
	if err := os.WriteFile(temporary, bytes, 0644); err != nil {
		return err
	}
	return os.Rename(temporary, final)
}

func (d *dashboard) handleUnifiedConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		document, err := loadINI(d.configPath)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{
			"path":     d.configPath,
			"content":  renderUnifiedConfig(redactUnifiedDocument(document)),
			"revision": configRevision(document),
			"note":     "Credentials are redacted and read-only here. Use the Secret Vault to set, rotate, inherit, or bind every managed credential.",
		})
	case http.MethodPost, http.MethodPut:
		var request struct {
			Content string `json:"content"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024*1024))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			writeJSON(w, 400, map[string]string{"error": "Invalid configuration request"})
			return
		}
		candidate, err := parseINI(request.Content)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		current, err := loadINI(d.configPath)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		mergeRedactedSecrets(candidate, current)
		preserveVaultManagedSecrets(candidate, current)
		if err := validateUnifiedConfig(candidate); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		restartScope := unifiedConfigRestartScope(current, candidate)
		if err := d.writeAndVerifyUnifiedConfig(candidate, current); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		if err := os.WriteFile(filepath.Join(d.runtimeDir, "topology-restart.pending"), []byte(strconv.FormatInt(time.Now().UnixMilli(), 10)), 0644); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		command := "topology_restart"
		delay := 750 * time.Millisecond
		message := "Configuration saved and verified. Only the gateway will restart; the dashboard stays online."
		if restartScope == "services" {
			command = "service_restart"
			delay = 3 * time.Second
			message = "Configuration saved and verified. Dashboard and gateway restart is scheduled."
		}
		actor := principalFromRequest(r)
		d.appendAudit(r, actor, "config.unified.apply", restartScope+"_restart_scheduled", map[string]any{"path": d.configPath})
		revision := configRevision(candidate)
		_ = writeConfigSaveStatus(d.runtimeDir, revision, "config.unified.apply", restartScope)
		writeJSON(w, 202, map[string]any{
			"saved": true, "restartScope": restartScope, "path": d.configPath,
			"revision": revision, "message": message,
		})
		d.scheduleCommand(delay, command, "", "")
	default:
		writeJSON(w, 405, map[string]string{"error": "Method not allowed"})
	}
}

func (d *dashboard) handleSecuritySources(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, 405, map[string]string{"error": "Method not allowed"})
		return
	}
	document, err := loadINI(d.configPath)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	sources := make([]map[string]any, 0)
	for _, descriptor := range d.secretDescriptors(document) {
		sources = append(sources, map[string]any{
			"id":                  descriptor.ID,
			"component":           descriptor.Component,
			"label":               descriptor.Label,
			"reference":           descriptor.Reference,
			"environmentVariable": descriptor.EnvironmentVariable,
			"configured":          descriptor.Configured,
			"fingerprint":         descriptor.Fingerprint,
			"mode":                descriptor.Mode,
		})
	}
	writeJSON(w, 200, map[string]any{"sources": sources})
}
