// Copyright (C) 2026 Ninj-OS contributors.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type identityGrant struct {
	GrantID         string   `json:"grantId"`
	SessionID       string   `json:"sessionId"`
	ServerID        string   `json:"serverId"`
	Username        string   `json:"username"`
	XUID            string   `json:"xuid"`
	UUID            string   `json:"uuid"`
	OriginalIP      string   `json:"originalIp"`
	DeviceOS        string   `json:"deviceOs,omitempty"`
	ClientVersion   string   `json:"clientVersion,omitempty"`
	ProtocolVersion int      `json:"protocolVersion,omitempty"`
	Role            string   `json:"role"`
	Operator        bool     `json:"operator"`
	Permissions     []string `json:"permissions"`
	CreatedAt       int64    `json:"createdAt"`
	ExpiresAt       int64    `json:"expiresAt"`
	ConsumedAt      int64    `json:"consumedAt,omitempty"`
}

func roleOperator(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "owner", "administrator", "admin", "operator":
		return true
	default:
		return false
	}
}

func rolePermissions(role string) []string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "owner":
		return []string{"*"}
	case "administrator", "admin":
		return []string{"ninjos.admin.*", "ninjos.command.*", "endstone.command.*"}
	case "operator":
		return []string{"ninjos.command.*", "endstone.command.*"}
	case "moderator", "mod":
		return []string{"ninjos.command.server", "ninjos.command.hub", "ninjos.moderation.*"}
	default:
		return []string{"ninjos.command.server", "ninjos.command.hub", "ninjos.command.glist"}
	}
}

func (d *dashboard) sessionCoreAuthorized(r *http.Request) bool {
	supplied := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if supplied == "" || d.sessionCoreToken == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(supplied), []byte(d.sessionCoreToken)) == 1
}

func (d *dashboard) cleanupIdentityGrants(now int64) {
	for id, grant := range d.identityGrants {
		if grant.ExpiresAt < now || (grant.ConsumedAt > 0 && now-grant.ConsumedAt > 60000) {
			delete(d.identityGrants, id)
		}
	}
}

func (d *dashboard) handleIdentityGrant(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]string{"error": "Method not allowed"})
		return
	}
	if !d.sessionCoreAuthorized(r) {
		writeJSON(w, 401, map[string]string{"error": "Session core authorization failed"})
		return
	}
	var request identityGrant
	if json.NewDecoder(io.LimitReader(r.Body, 128*1024)).Decode(&request) != nil {
		writeJSON(w, 400, map[string]string{"error": "Invalid identity grant"})
		return
	}
	request.ServerID = normalizeCompanionServerID(request.ServerID)
	request.Username = strings.TrimSpace(request.Username)
	request.XUID = strings.TrimSpace(request.XUID)
	request.SessionID = strings.TrimSpace(request.SessionID)
	if request.ServerID == "" || request.Username == "" || request.SessionID == "" {
		writeJSON(w, 400, map[string]string{"error": "serverId, username, and sessionId are required"})
		return
	}

	role, banned, access, _ := d.ledger.profileIdentity(request.XUID)
	if banned {
		writeJSON(w, 403, map[string]string{"error": "Player is network banned"})
		return
	}
	if allowed, exists := access[request.ServerID]; exists && !allowed {
		writeJSON(w, 403, map[string]string{"error": "Player is not allowed on this backend"})
		return
	}
	if role == "" {
		role = "member"
	}
	request.Role = role
	request.Operator = roleOperator(role)
	request.Permissions = rolePermissions(role)
	request.CreatedAt = time.Now().UnixMilli()
	if request.ExpiresAt <= request.CreatedAt || request.ExpiresAt-request.CreatedAt > 120000 {
		request.ExpiresAt = request.CreatedAt + 30000
	}
	if request.GrantID == "" {
		id, err := randomTicketID()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		request.GrantID = id
	}

	d.identityMu.Lock()
	d.cleanupIdentityGrants(request.CreatedAt)
	d.identityGrants[request.GrantID] = request
	d.identityMu.Unlock()

	d.ledger.upsertProfile(request.XUID, request.Username, request.ServerID, request.CreatedAt)
	d.appendJSONL(filepathJoin(d.runtimeDir, "events.jsonl"), map[string]any{
		"timestamp": request.CreatedAt,
		"type":      "identity.grant_created",
		"severity":  "info",
		"serverId":  request.ServerID,
		"player":    request.Username,
		"xuid":      request.XUID,
		"message":   fmt.Sprintf("Signed identity prepared for %s on %s", request.Username, request.ServerID),
	}, 20*1024*1024)
	writeJSON(w, 201, request)
}

func filepathJoin(parts ...string) string {
	if len(parts) == 0 {
		return ""
	}
	result := strings.TrimRight(parts[0], "/\\")
	for _, part := range parts[1:] {
		result += "/" + strings.Trim(part, "/\\")
	}
	return result
}

func (d *dashboard) verifyBridgeBody(r *http.Request, body []byte) error {
	if err := d.verifySignedBody(r, body); err == nil {
		return nil
	}
	serverID := normalizeCompanionServerID(r.Header.Get("X-NinjOS-Server"))
	supplied := strings.TrimSpace(r.Header.Get("X-NinjOS-Bridge-Token"))
	expected := d.companionSecret(serverID)
	if serverID == "" || supplied == "" || expected == "" {
		return fmt.Errorf("Unauthorized")
	}
	if subtle.ConstantTimeCompare([]byte(supplied), []byte(expected)) != 1 {
		return fmt.Errorf("Unauthorized")
	}
	return nil
}

func (d *dashboard) handleIdentityConsume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]string{"error": "Method not allowed"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 128*1024))
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "Invalid request"})
		return
	}
	if err := d.verifyBridgeBody(r, body); err != nil {
		writeJSON(w, 401, map[string]string{"error": err.Error()})
		return
	}
	var request struct {
		ServerID  string `json:"serverId"`
		Username  string `json:"username"`
		SessionID string `json:"sessionId"`
	}
	if json.Unmarshal(body, &request) != nil {
		writeJSON(w, 400, map[string]string{"error": "Invalid JSON"})
		return
	}
	request.ServerID = normalizeCompanionServerID(request.ServerID)
	request.Username = strings.TrimSpace(request.Username)
	now := time.Now().UnixMilli()

	d.identityMu.Lock()
	defer d.identityMu.Unlock()
	d.cleanupIdentityGrants(now)
	var selected identityGrant
	selectedID := ""
	for id, grant := range d.identityGrants {
		if grant.ConsumedAt != 0 || grant.ExpiresAt < now || grant.ServerID != request.ServerID {
			continue
		}
		if request.SessionID != "" && grant.SessionID != request.SessionID {
			continue
		}
		if !strings.EqualFold(grant.Username, request.Username) {
			continue
		}
		if selectedID == "" || grant.CreatedAt > selected.CreatedAt {
			selected, selectedID = grant, id
		}
	}
	if selectedID == "" {
		writeJSON(w, 404, map[string]string{"error": "No valid proxy identity grant is waiting for this player"})
		return
	}
	selected.ConsumedAt = now
	d.identityGrants[selectedID] = selected
	writeJSON(w, 200, selected)
}

func (d *dashboard) handlePermissionSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]string{"error": "Method not allowed"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil || d.verifyBridgeBody(r, body) != nil {
		writeJSON(w, 401, map[string]string{"error": "Unauthorized"})
		return
	}
	var request struct {
		ServerID string `json:"serverId"`
	}
	if json.Unmarshal(body, &request) != nil {
		writeJSON(w, 400, map[string]string{"error": "Invalid JSON"})
		return
	}
	request.ServerID = normalizeCompanionServerID(request.ServerID)
	entries := make([]map[string]any, 0)
	for _, row := range d.ledger.profiles(10000) {
		xuid := fmt.Sprint(row["xuid"])
		role := fmt.Sprint(row["network_role"])
		banned := fmt.Sprint(row["network_banned"]) != "0"
		if banned || xuid == "" || !roleOperator(role) {
			continue
		}
		allowed, _ := d.ledger.profileAccess(xuid, request.ServerID)
		if !allowed {
			continue
		}
		entries = append(entries, map[string]any{
			"xuid":       xuid,
			"gamertag":   fmt.Sprint(row["gamertag"]),
			"permission": "operator",
			"role":       role,
		})
	}
	writeJSON(w, 200, map[string]any{"serverId": request.ServerID, "operators": entries, "generatedAt": time.Now().UnixMilli()})
}
