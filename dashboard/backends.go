// Copyright (C) 2026 Ninj-OS contributors.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const topologySchemaVersion = 1

var backendIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,31}$`)

type backendDefinition struct {
	ID                   string `json:"id"`
	DisplayName          string `json:"displayName"`
	Host                 string `json:"host"`
	BackendPort          int    `json:"backendPort"`
	PublicPort           int    `json:"publicPort"`
	Fallback             bool   `json:"fallback"`
	FallbackBackend      string `json:"fallbackBackend"`
	Profile              string `json:"profile"`
	Enabled              bool   `json:"enabled"`
	ConnectionMode       string `json:"connectionMode"`
	BackendAdapter       string `json:"backendAdapter"`
	BackendOnlineMode    bool   `json:"backendOnlineMode"`
	RequireProxyIdentity bool   `json:"requireProxyIdentity"`
	Capacity             int    `json:"capacity"`
	CompanionSecretEnv   string `json:"companionSecretEnv"`
}

type topologyConfig struct {
	Version        int                 `json:"version"`
	PrimaryBackend string              `json:"primaryBackend"`
	RoutingMode    string              `json:"routingMode"`
	Backends       []backendDefinition `json:"backends"`
}

type backendRegistryResponse struct {
	Topology               topologyConfig `json:"topology"`
	AllowedPorts           []int          `json:"allowedPublicPorts"`
	AvailableTransferPorts []int          `json:"availableTransferPorts"`
	RestartPending         bool           `json:"restartPending"`
	Path                   string         `json:"path"`
}

func defaultTopology() topologyConfig {
	return topologyConfig{
		Version:        topologySchemaVersion,
		PrimaryBackend: "kingdom",
		RoutingMode:    "primary",
		Backends: []backendDefinition{
			{
				ID: "kingdom", DisplayName: "The Kingdom", Host: "185.83.152.144",
				BackendPort: 25565, PublicPort: 25566, Profile: "kingdom", Enabled: true,
				ConnectionMode: "transparent", BackendAdapter: "endstone", BackendOnlineMode: true, Capacity: 50,
				CompanionSecretEnv: "COMPANION_KINGDOM_SECRET",
			},
			{
				ID: "zoo", DisplayName: "Zoo", Host: "185.83.152.144",
				BackendPort: 19431, PublicPort: 25571, Profile: "zoo", Enabled: true,
				ConnectionMode: "transparent", BackendAdapter: "proxy_only", BackendOnlineMode: true, Capacity: 50,
				CompanionSecretEnv: "COMPANION_ZOO_SECRET",
			},
		},
	}
}

func parseTopologyProperties(content string) (topologyConfig, error) {
	values := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(strings.SplitN(scanner.Text(), "#", 2)[0])
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return topologyConfig{}, fmt.Errorf("invalid topology line: %q", scanner.Text())
		}
		values[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	if err := scanner.Err(); err != nil {
		return topologyConfig{}, err
	}

	topology := topologyConfig{
		Version:        topologySchemaVersion,
		PrimaryBackend: strings.TrimSpace(values["primary_backend"]),
		RoutingMode:    strings.TrimSpace(values["routing_mode"]),
	}
	if rawVersion := strings.TrimSpace(values["version"]); rawVersion != "" {
		version, err := strconv.Atoi(rawVersion)
		if err != nil {
			return topologyConfig{}, errors.New("topology version must be an integer")
		}
		topology.Version = version
	}

	displayNames := parseNameMap(values["display_names"])
	publicPorts, err := parseStaticRoutes(values["static_routes"])
	if err != nil {
		return topologyConfig{}, err
	}

	for _, raw := range strings.Split(values["backends"], ";") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		fields := strings.Split(raw, "|")
		if len(fields) < 3 {
			return topologyConfig{}, fmt.Errorf("invalid backend entry %q", raw)
		}
		backendPort, err := strconv.Atoi(strings.TrimSpace(fields[2]))
		if err != nil {
			return topologyConfig{}, fmt.Errorf("invalid backend port in %q", raw)
		}
		definition := backendDefinition{
			ID:                strings.TrimSpace(fields[0]),
			Host:              strings.TrimSpace(fields[1]),
			BackendPort:       backendPort,
			Fallback:          false,
			Enabled:           true,
			ConnectionMode:    "transparent",
			BackendAdapter:    "proxy_only",
			BackendOnlineMode: true,
			Capacity:          50,
		}
		if len(fields) >= 4 {
			definition.Fallback = parseRegistryBool(fields[3], false)
		}
		if len(fields) >= 5 {
			definition.Profile = strings.TrimSpace(fields[4])
		}
		if len(fields) >= 6 {
			definition.Enabled = parseRegistryBool(fields[5], true)
		}
		if definition.Profile == "" {
			definition.Profile = definition.ID
		}
		definition.DisplayName = displayNames[definition.ID]
		if definition.DisplayName == "" {
			definition.DisplayName = humanizeBackendID(definition.ID)
		}
		definition.PublicPort = publicPorts[definition.ID]
		topology.Backends = append(topology.Backends, definition)
	}

	if topology.PrimaryBackend == "" && len(topology.Backends) > 0 {
		topology.PrimaryBackend = topology.Backends[0].ID
	}
	if topology.RoutingMode == "" {
		topology.RoutingMode = "primary"
	}
	if err := validateTopology(topology); err != nil {
		return topologyConfig{}, err
	}
	return topology, nil
}

func serializeTopology(topology topologyConfig) string {
	var backends []string
	var routes []string
	var names []string
	for _, backend := range topology.Backends {
		profile := backend.Profile
		if profile == "" {
			profile = backend.ID
		}
		backends = append(backends, strings.Join([]string{
			backend.ID,
			backend.Host,
			strconv.Itoa(backend.BackendPort),
			strconv.FormatBool(backend.Fallback),
			profile,
			strconv.FormatBool(backend.Enabled),
		}, "|"))
		if backend.PublicPort > 0 {
			routes = append(routes, fmt.Sprintf("%d|%s", backend.PublicPort, backend.ID))
		}
		names = append(names, backend.ID+"|"+backend.DisplayName)
	}

	return fmt.Sprintf(
		"# Managed by Ninj-OS Edge Fabric. Use the dashboard or edit while the proxy is stopped.\n"+
			"version=%d\n"+
			"primary_backend=%s\n"+
			"routing_mode=%s\n"+
			"backends=%s\n"+
			"static_routes=%s\n"+
			"display_names=%s\n",
		topologySchemaVersion,
		topology.PrimaryBackend,
		topology.RoutingMode,
		strings.Join(backends, ";"),
		strings.Join(routes, ";"),
		strings.Join(names, ";"),
	)
}

func parseNameMap(raw string) map[string]string {
	result := map[string]string{}
	for _, item := range strings.Split(raw, ";") {
		fields := strings.SplitN(strings.TrimSpace(item), "|", 2)
		if len(fields) == 2 {
			result[strings.TrimSpace(fields[0])] = strings.TrimSpace(fields[1])
		}
	}
	return result
}

func parseStaticRoutes(raw string) (map[string]int, error) {
	result := map[string]int{}
	used := map[int]string{}
	for _, item := range strings.Split(raw, ";") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		fields := strings.Split(item, "|")
		if len(fields) != 2 {
			return nil, fmt.Errorf("invalid static route %q", item)
		}
		port, err := strconv.Atoi(strings.TrimSpace(fields[0]))
		if err != nil || port < 1 || port > 65535 {
			return nil, fmt.Errorf("invalid public port in route %q", item)
		}
		id := strings.TrimSpace(fields[1])
		if previous, exists := used[port]; exists {
			return nil, fmt.Errorf("public port %d is assigned to %s and %s", port, previous, id)
		}
		used[port] = id
		result[id] = port
	}
	return result, nil
}

func parseRegistryBool(raw string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func humanizeBackendID(id string) string {
	words := strings.Fields(strings.NewReplacer("-", " ", "_", " ").Replace(id))
	for index := range words {
		words[index] = strings.ToUpper(words[index][:1]) + words[index][1:]
	}
	return strings.Join(words, " ")
}

func validateTopology(topology topologyConfig) error {
	if topology.Version != topologySchemaVersion {
		return fmt.Errorf("unsupported topology version %d", topology.Version)
	}
	validModes := map[string]bool{
		"primary": true, "failover": true, "round_robin": true, "least_sessions": true,
	}
	if !validModes[topology.RoutingMode] {
		return fmt.Errorf("unsupported routing mode %q", topology.RoutingMode)
	}
	if len(topology.Backends) == 0 {
		return errors.New("at least one backend is required")
	}

	ids := map[string]bool{}
	publicPorts := map[int]string{}
	primaryFound := false
	for index, backend := range topology.Backends {
		if !backendIDPattern.MatchString(backend.ID) {
			return fmt.Errorf("backend %d has an invalid ID; use lowercase letters, numbers, dashes, or underscores", index+1)
		}
		if ids[backend.ID] {
			return fmt.Errorf("backend ID %q is duplicated", backend.ID)
		}
		ids[backend.ID] = true
		if backend.ID == topology.PrimaryBackend {
			primaryFound = true
		}
		if strings.TrimSpace(backend.DisplayName) == "" {
			return fmt.Errorf("backend %q needs a display name", backend.ID)
		}
		if strings.ContainsAny(backend.DisplayName, "|;\r\n") {
			return fmt.Errorf("backend %q display name contains an unsupported character", backend.ID)
		}
		if strings.TrimSpace(backend.Host) == "" || strings.ContainsAny(backend.Host, "|;\r\n") {
			return fmt.Errorf("backend %q has an invalid host", backend.ID)
		}
		if backend.BackendPort < 1 || backend.BackendPort > 65535 {
			return fmt.Errorf("backend %q has an invalid backend port", backend.ID)
		}
		if backend.PublicPort < 0 || backend.PublicPort > 65535 {
			return fmt.Errorf("backend %q has an invalid public port", backend.ID)
		}
		if backend.PublicPort > 0 {
			if previous, exists := publicPorts[backend.PublicPort]; exists {
				return fmt.Errorf("public port %d is already assigned to %s", backend.PublicPort, previous)
			}
			publicPorts[backend.PublicPort] = backend.ID
		}
		if backend.Profile == "" || strings.ContainsAny(backend.Profile, "|;\r\n") {
			return fmt.Errorf("backend %q has an invalid protection profile", backend.ID)
		}
		if backend.ConnectionMode == "" {
			backend.ConnectionMode = "transparent"
		}
		if backend.ConnectionMode != "transparent" && backend.ConnectionMode != "full_proxy" {
			return fmt.Errorf("backend %q connection mode must be transparent or full_proxy", backend.ID)
		}
		validAdapters := map[string]bool{"endstone": true, "vanilla_bridge": true, "vanilla_agent": true, "proxy_only": true}
		if backend.BackendAdapter == "" {
			backend.BackendAdapter = "proxy_only"
		}
		if !validAdapters[backend.BackendAdapter] {
			return fmt.Errorf("backend %q has unsupported adapter %q", backend.ID, backend.BackendAdapter)
		}
		if backend.Capacity < 1 || backend.Capacity > 100000 {
			return fmt.Errorf("backend %q capacity must be between 1 and 100000", backend.ID)
		}
		if backend.ConnectionMode == "full_proxy" && backend.BackendOnlineMode {
			return fmt.Errorf("backend %q full_proxy mode requires backend_online_mode=false; use transparent mode to keep online-mode=true", backend.ID)
		}
		if backend.ConnectionMode == "full_proxy" && backend.PublicPort == 0 {
			return fmt.Errorf("backend %q full_proxy mode requires a public proxy UDP port", backend.ID)
		}
		if backend.RequireProxyIdentity && backend.ConnectionMode != "full_proxy" {
			return fmt.Errorf("backend %q can require proxy identity only in full_proxy mode", backend.ID)
		}
		if backend.FallbackBackend != "" && backend.FallbackBackend == backend.ID {
			return fmt.Errorf("backend %q cannot fall back to itself", backend.ID)
		}
		if backend.CompanionSecretEnv != "" {
			if !regexp.MustCompile(`^[A-Z][A-Z0-9_]{2,63}$`).MatchString(backend.CompanionSecretEnv) {
				return fmt.Errorf("backend %q has an invalid companion secret environment variable", backend.ID)
			}
		}
	}
	if !primaryFound {
		return fmt.Errorf("primary backend %q does not exist", topology.PrimaryBackend)
	}
	return nil
}

func (d *dashboard) loadTopology() (topologyConfig, error) {
	document, err := loadINI(d.configPath)
	if os.IsNotExist(err) {
		if err := ensureUnifiedConfig(d.configPath); err != nil {
			return topologyConfig{}, err
		}
		document, err = loadINI(d.configPath)
	}
	if err != nil {
		return topologyConfig{}, err
	}
	return topologyFromUnified(document)
}

func (d *dashboard) saveTopology(topology topologyConfig) error {
	if err := validateTopology(topology); err != nil {
		return err
	}

	d.fileMu.Lock()
	defer d.fileMu.Unlock()

	original, err := os.ReadFile(d.configPath)
	if err != nil {
		return err
	}
	document, err := loadINI(d.configPath)
	if err != nil {
		return err
	}
	if err := applyTopologyToUnified(document, topology); err != nil {
		return err
	}

	restore := func(cause error) error {
		if restoreErr := os.WriteFile(d.configPath, original, 0600); restoreErr != nil {
			return fmt.Errorf("%v; rollback also failed: %w", cause, restoreErr)
		}
		_ = prepareUnifiedConfig(d.configPath, d.runtimeDir, d.gatewayConfigPath)
		return cause
	}

	if err := writeUnifiedConfig(d.configPath, document); err != nil {
		return err
	}
	if err := prepareUnifiedConfig(d.configPath, d.runtimeDir, d.gatewayConfigPath); err != nil {
		return restore(fmt.Errorf("configuration was not applied: %w", err))
	}

	persisted, err := d.loadTopology()
	if err != nil {
		return restore(fmt.Errorf("configuration was written but could not be read back: %w", err))
	}
	if !reflect.DeepEqual(normalizedTopology(persisted), normalizedTopology(topology)) {
		return restore(errors.New("configuration verification failed; the previous backend registry was restored"))
	}
	return nil
}

func normalizedTopology(topology topologyConfig) topologyConfig {
	copyValue := topology
	copyValue.Backends = append([]backendDefinition(nil), topology.Backends...)
	for index := range copyValue.Backends {
		// Secret source metadata is intentionally outside backend topology.
		copyValue.Backends[index].CompanionSecretEnv = ""
	}
	sort.Slice(copyValue.Backends, func(left, right int) bool {
		return copyValue.Backends[left].ID < copyValue.Backends[right].ID
	})
	return copyValue
}

func (d *dashboard) allowedPublicPorts() []int {
	ports := append([]int(nil), d.managedPublicPorts...)
	if document, err := loadINI(d.configPath); err == nil {
		if configured := parsePortList(document.get("edge", "managed_public_udp_ports", "")); len(configured) > 0 {
			ports = configured
		}
	}
	sort.Ints(ports)
	return ports
}

func staticPublicPorts(topology topologyConfig) map[int]string {
	ports := make(map[int]string)
	for _, backend := range topology.Backends {
		if backend.PublicPort > 0 {
			ports[backend.PublicPort] = backend.ID
		}
	}
	return ports
}

func (d *dashboard) availableTransferPorts() []int {
	start, end := d.transferPoolRange()
	assigned := map[int]string{}
	if topology, err := d.loadTopology(); err == nil {
		assigned = staticPublicPorts(topology)
	}
	ports := make([]int, 0, end-start+1)
	for port := start; port <= end; port++ {
		if _, used := assigned[port]; !used {
			ports = append(ports, port)
		}
	}
	return ports
}

func (d *dashboard) transferPoolRange() (int, int) {
	document, err := loadINI(d.configPath)
	if err != nil {
		return d.transferPortStart, d.transferPortEnd
	}
	start := document.getInt("transfer", "port_start", d.transferPortStart, 1, 65535)
	end := document.getInt("transfer", "port_end", d.transferPortEnd, 1, 65535)
	if start > end {
		start, end = end, start
	}
	return start, end
}

func parsePortList(raw string) []int {
	seen := map[int]bool{}
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if strings.Contains(item, "-") {
			fields := strings.SplitN(item, "-", 2)
			start, startErr := strconv.Atoi(strings.TrimSpace(fields[0]))
			end, endErr := strconv.Atoi(strings.TrimSpace(fields[1]))
			if startErr == nil && endErr == nil {
				if start > end {
					start, end = end, start
				}
				for port := start; port <= end && port <= 65535; port++ {
					if port > 0 {
						seen[port] = true
					}
				}
			}
			continue
		}
		if port, err := strconv.Atoi(item); err == nil && port > 0 && port <= 65535 {
			seen[port] = true
		}
	}
	ports := make([]int, 0, len(seen))
	for port := range seen {
		ports = append(ports, port)
	}
	sort.Ints(ports)
	return ports
}

func (d *dashboard) validateManagedPort(port int) error {
	if port == 0 {
		return nil
	}
	allowedPorts := d.allowedPublicPorts()
	if len(allowedPorts) == 0 {
		return nil
	}
	for _, allowed := range allowedPorts {
		if port == allowed {
			return nil
		}
	}
	return fmt.Errorf("public port %d is not listed in edge.managed_public_udp_ports; add the Pterodactyl UDP allocation first", port)
}

func (d *dashboard) handleBackendRegistry(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		topology, err := d.loadTopology()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, backendRegistryResponse{
			Topology: topology, AllowedPorts: d.allowedPublicPorts(),
			AvailableTransferPorts: d.availableTransferPorts(),
			RestartPending:         fileExists(filepath.Join(d.runtimeDir, "topology-restart.pending")),
			Path:                   d.configPath,
		})
	case http.MethodPost:
		d.createBackend(w, r)
	case http.MethodPut:
		d.updateBackend(w, r)
	case http.MethodDelete:
		d.deleteBackend(w, r)
	default:
		writeJSON(w, 405, map[string]string{"error": "Method not allowed"})
	}
}

func (d *dashboard) decodeBackendDefinition(r *http.Request) (backendDefinition, error) {
	var definition backendDefinition
	decoder := json.NewDecoder(io.LimitReader(r.Body, 128*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&definition); err != nil {
		return definition, fmt.Errorf("invalid backend request: %w", err)
	}
	legacyRequest := strings.TrimSpace(definition.ConnectionMode) == ""
	definition.ID = strings.ToLower(strings.TrimSpace(definition.ID))
	definition.DisplayName = strings.TrimSpace(definition.DisplayName)
	definition.Host = strings.TrimSpace(definition.Host)
	definition.Profile = strings.TrimSpace(definition.Profile)
	definition.FallbackBackend = strings.ToLower(strings.TrimSpace(definition.FallbackBackend))
	definition.ConnectionMode = strings.ToLower(strings.TrimSpace(definition.ConnectionMode))
	definition.BackendAdapter = strings.ToLower(strings.TrimSpace(definition.BackendAdapter))
	// Credential sources are managed exclusively through the Vault. Accept the
	// legacy field for API compatibility, but never apply it from this endpoint.
	definition.CompanionSecretEnv = ""
	if definition.ConnectionMode == "" {
		definition.ConnectionMode = "transparent"
	}
	if definition.BackendAdapter == "" {
		definition.BackendAdapter = "proxy_only"
	}
	if definition.Capacity == 0 {
		definition.Capacity = 50
	}
	if legacyRequest {
		definition.BackendOnlineMode = true
	}
	if definition.Profile == "" {
		definition.Profile = definition.ID
	}
	return definition, nil
}

func (d *dashboard) createBackend(w http.ResponseWriter, r *http.Request) {
	definition, err := d.decodeBackendDefinition(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if err := d.validateManagedPort(definition.PublicPort); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	topology, err := d.loadTopology()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	for _, backend := range topology.Backends {
		if backend.ID == definition.ID {
			writeJSON(w, 409, map[string]string{"error": "A backend with that ID already exists"})
			return
		}
	}
	topology.Backends = append(topology.Backends, definition)
	if err := d.applyTopologyChange(r, topology, "backend.create", definition); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	persisted, _ := d.loadTopology()
	writeJSON(w, 201, map[string]any{
		"saved": true, "backend": definition, "topology": persisted,
		"gatewayRestartQueued": true, "configPath": d.configPath,
	})
}

func (d *dashboard) updateBackend(w http.ResponseWriter, r *http.Request) {
	originalID := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("id")))
	if originalID == "" {
		writeJSON(w, 400, map[string]string{"error": "The id query parameter is required"})
		return
	}
	definition, err := d.decodeBackendDefinition(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if err := d.validateManagedPort(definition.PublicPort); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	topology, err := d.loadTopology()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if definition.ID != originalID {
		for _, backend := range topology.Backends {
			if backend.ID == definition.ID {
				writeJSON(w, 409, map[string]string{"error": "The new backend ID is already in use"})
				return
			}
		}
	}

	found := false
	for index := range topology.Backends {
		if topology.Backends[index].ID != originalID {
			continue
		}
		found = true
		topology.Backends[index] = definition
		if topology.PrimaryBackend == originalID {
			topology.PrimaryBackend = definition.ID
		}
		break
	}
	if !found {
		writeJSON(w, 404, map[string]string{"error": "Backend not found"})
		return
	}
	if err := d.applyTopologyChange(r, topology, "backend.update", map[string]any{"from": originalID, "backend": definition}); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	persisted, _ := d.loadTopology()
	writeJSON(w, 200, map[string]any{
		"saved": true, "backend": definition, "topology": persisted,
		"gatewayRestartQueued": true, "configPath": d.configPath,
	})
}

func (d *dashboard) deleteBackend(w http.ResponseWriter, r *http.Request) {
	id := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("id")))
	if id == "" {
		writeJSON(w, 400, map[string]string{"error": "The id query parameter is required"})
		return
	}
	topology, err := d.loadTopology()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if len(topology.Backends) == 1 {
		writeJSON(w, 409, map[string]string{"error": "The last backend cannot be deleted"})
		return
	}
	next := make([]backendDefinition, 0, len(topology.Backends)-1)
	removed := false
	for _, backend := range topology.Backends {
		if backend.ID == id {
			removed = true
			continue
		}
		next = append(next, backend)
	}
	if !removed {
		writeJSON(w, 404, map[string]string{"error": "Backend not found"})
		return
	}
	topology.Backends = next
	if topology.PrimaryBackend == id {
		topology.PrimaryBackend = topology.Backends[0].ID
	}
	if err := d.applyTopologyChange(r, topology, "backend.delete", map[string]string{"id": id}); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	persisted, _ := d.loadTopology()
	writeJSON(w, 200, map[string]any{
		"saved": true, "deleted": id, "topology": persisted,
		"gatewayRestartQueued": true, "configPath": d.configPath,
	})
}

func (d *dashboard) handleTopologySettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeJSON(w, 405, map[string]string{"error": "Method not allowed"})
		return
	}
	var request struct {
		PrimaryBackend string `json:"primaryBackend"`
		RoutingMode    string `json:"routingMode"`
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 64*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, 400, map[string]string{"error": "Invalid settings request"})
		return
	}
	topology, err := d.loadTopology()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	topology.PrimaryBackend = strings.TrimSpace(request.PrimaryBackend)
	topology.RoutingMode = strings.TrimSpace(request.RoutingMode)
	if err := d.applyTopologyChange(r, topology, "topology.settings", request); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	persisted, _ := d.loadTopology()
	writeJSON(w, 200, map[string]any{
		"saved": true, "topology": persisted,
		"gatewayRestartQueued": true, "configPath": d.configPath,
	})
}

func (d *dashboard) applyTopologyChange(r *http.Request, topology topologyConfig, action string, details any) error {
	if err := validateTopology(topology); err != nil {
		return err
	}
	for _, backend := range topology.Backends {
		if !backend.Enabled {
			continue
		}
		if err := d.validateManagedPort(backend.PublicPort); err != nil {
			return err
		}
	}
	if err := d.saveTopology(topology); err != nil {
		return err
	}
	if err := d.removeTicketsForStaticPorts(topology); err != nil {
		d.appendJSONL(filepath.Join(d.runtimeDir, "events.jsonl"), map[string]any{
			"timestamp": time.Now().UnixMilli(), "type": "transfer.pool_cleanup_failed",
			"severity": "warning", "message": err.Error(),
		}, 20*1024*1024)
	}
	if err := os.WriteFile(filepath.Join(d.runtimeDir, "topology-restart.pending"), []byte(strconv.FormatInt(time.Now().UnixMilli(), 10)), 0644); err != nil {
		return err
	}
	if err := d.queueCommand("topology_restart", "", ""); err != nil {
		return err
	}
	d.appendAudit(r, principalFromRequest(r), action, "restart_queued", details)
	return nil
}

func (d *dashboard) removeTicketsForStaticPorts(topology topologyConfig) error {
	assigned := staticPublicPorts(topology)
	if len(assigned) == 0 {
		return nil
	}
	d.fileMu.Lock()
	defer d.fileMu.Unlock()
	tickets, err := d.loadTransferTickets()
	if err != nil {
		return err
	}
	kept := tickets[:0]
	for _, ticket := range tickets {
		if backendID, conflict := assigned[ticket.Port]; conflict {
			if d.ledger != nil {
				d.ledger.updateTransfer(ticket.ID, "failed", "port assigned permanently to backend "+backendID, time.Now().UnixMilli())
			}
			continue
		}
		kept = append(kept, ticket)
	}
	return d.writeTransferTickets(kept)
}

func (d *dashboard) handleBackendTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]string{"error": "Method not allowed"})
		return
	}
	var request struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 64*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, 400, map[string]string{"error": "Invalid test request"})
		return
	}
	started := time.Now()
	motd, err := pingBedrockBackend(strings.TrimSpace(request.Host), request.Port, 1800*time.Millisecond)
	latency := time.Since(started).Milliseconds()
	if err != nil {
		writeJSON(w, 200, map[string]any{"reachable": false, "latencyMs": latency, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"reachable": true, "latencyMs": latency, "motd": motd})
}

func pingBedrockBackend(host string, port int, timeout time.Duration) (string, error) {
	if host == "" || port < 1 || port > 65535 {
		return "", errors.New("a valid backend host and port are required")
	}
	address, err := net.ResolveUDPAddr("udp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return "", err
	}
	connection, err := net.DialUDP("udp", nil, address)
	if err != nil {
		return "", err
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(timeout))

	magic := []byte{0x00, 0xff, 0xff, 0x00, 0xfe, 0xfe, 0xfe, 0xfe, 0xfd, 0xfd, 0xfd, 0xfd, 0x12, 0x34, 0x56, 0x78}
	packet := &bytes.Buffer{}
	packet.WriteByte(0x01)
	_ = binary.Write(packet, binary.BigEndian, uint64(time.Now().UnixMilli()))
	packet.Write(magic)
	_ = binary.Write(packet, binary.BigEndian, uint64(0x4e696e6a4f535f36))
	if _, err := connection.Write(packet.Bytes()); err != nil {
		return "", err
	}

	response := make([]byte, 4096)
	length, err := connection.Read(response)
	if err != nil {
		return "", fmt.Errorf("no RakNet pong received: %w", err)
	}
	if length < 35 || response[0] != 0x1c {
		return "", errors.New("backend responded, but not with a Bedrock RakNet pong")
	}
	motdLength := int(binary.BigEndian.Uint16(response[33:35]))
	if 35+motdLength > length {
		motdLength = length - 35
	}
	if motdLength < 0 {
		motdLength = 0
	}
	return string(response[35 : 35+motdLength]), nil
}
