// Copyright (C) 2026 Ninj-OS contributors.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"crypto/hmac"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	dashboardUsersVersion = 2
	passwordIterations    = 210000
	passwordSaltBytes     = 24
	passwordKeyBytes      = 32
)

type dashboardUsersFile struct {
	Version       int             `json:"version"`
	SetupComplete bool            `json:"setupComplete"`
	Users         []dashboardUser `json:"users"`
}

func pbkdf2SHA256(password, salt []byte, iterations, keyLength int) []byte {
	if iterations < 1 || keyLength < 1 {
		return nil
	}
	hashLength := sha256.Size
	blocks := (keyLength + hashLength - 1) / hashLength
	derived := make([]byte, 0, blocks*hashLength)
	counter := make([]byte, 4)
	for block := 1; block <= blocks; block++ {
		binary.BigEndian.PutUint32(counter, uint32(block))
		mac := hmac.New(sha256.New, password)
		_, _ = mac.Write(salt)
		_, _ = mac.Write(counter)
		u := mac.Sum(nil)
		t := append([]byte(nil), u...)
		for round := 1; round < iterations; round++ {
			mac = hmac.New(sha256.New, password)
			_, _ = mac.Write(u)
			u = mac.Sum(nil)
			for index := range t {
				t[index] ^= u[index]
			}
		}
		derived = append(derived, t...)
	}
	return derived[:keyLength]
}

func newPasswordCredentials(password string) (hash, salt string, iterations int, err error) {
	saltBytes := make([]byte, passwordSaltBytes)
	if _, err = cryptorand.Read(saltBytes); err != nil {
		return "", "", 0, err
	}
	key := pbkdf2SHA256([]byte(password), saltBytes, passwordIterations, passwordKeyBytes)
	return base64.RawStdEncoding.EncodeToString(key), base64.RawStdEncoding.EncodeToString(saltBytes), passwordIterations, nil
}

func verifyPassword(user dashboardUser, password string) bool {
	if user.PasswordHash == "" || user.PasswordSalt == "" {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(user.PasswordSalt)
	if err != nil {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(user.PasswordHash)
	if err != nil || len(expected) == 0 {
		return false
	}
	iterations := user.PasswordIterations
	if iterations < 50000 {
		return false
	}
	actual := pbkdf2SHA256([]byte(password), salt, iterations, len(expected))
	return hmac.Equal(actual, expected)
}

func verifyUserCredential(user dashboardUser, provided string) bool {
	if user.PasswordHash != "" {
		return verifyPassword(user, provided)
	}
	if user.TokenHash != "" {
		return hmac.Equal([]byte(tokenHash(provided)), []byte(strings.ToLower(user.TokenHash)))
	}
	return false
}

func validateOwnerUsername(username string) error {
	if len(username) < 3 || len(username) > 32 {
		return errors.New("Username must contain 3 to 32 characters")
	}
	for _, value := range username {
		if (value >= 'a' && value <= 'z') || (value >= 'A' && value <= 'Z') ||
			(value >= '0' && value <= '9') || value == '_' || value == '-' || value == '.' {
			continue
		}
		return errors.New("Username may contain only letters, numbers, periods, underscores, and hyphens")
	}
	return nil
}

func validateOwnerPassword(username, password string) error {
	if len(password) < 12 {
		return errors.New("Password must contain at least 12 characters")
	}
	if len(password) > 256 {
		return errors.New("Password must contain no more than 256 characters")
	}
	if strings.EqualFold(username, password) {
		return errors.New("Password cannot match the username")
	}
	return nil
}

func (d *dashboard) loadUsersFile() dashboardUsersFile {
	payload := dashboardUsersFile{Version: dashboardUsersVersion, Users: []dashboardUser{}}
	bytes, err := os.ReadFile(d.usersPath)
	if err != nil {
		return payload
	}
	if json.Unmarshal(bytes, &payload) != nil {
		return dashboardUsersFile{Version: dashboardUsersVersion, Users: []dashboardUser{}}
	}
	// Version 1 did not have SetupComplete. Any populated v1 file represents an
	// existing installation and remains usable through legacy token login.
	if payload.Version < dashboardUsersVersion {
		payload.SetupComplete = len(payload.Users) > 0
		payload.Version = dashboardUsersVersion
	}
	if payload.Users == nil {
		payload.Users = []dashboardUser{}
	}
	return payload
}

func (d *dashboard) saveUsersFile(payload dashboardUsersFile) error {
	payload.Version = dashboardUsersVersion
	bytes, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(d.usersPath), 0755); err != nil {
		return err
	}
	temporary := d.usersPath + ".tmp"
	if err := os.WriteFile(temporary, bytes, 0600); err != nil {
		return err
	}
	if err := os.Chmod(temporary, 0600); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return os.Rename(temporary, d.usersPath)
}

func (d *dashboard) setupRequired() bool {
	payload := d.loadUsersFile()
	if !payload.SetupComplete {
		return true
	}
	for _, user := range payload.Users {
		if user.Enabled && strings.EqualFold(user.Role, "owner") {
			return false
		}
	}
	return true
}

func setupCodeFromFile(path string) string {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, raw := range strings.Split(string(bytes), "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "Setup code:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Setup code:"))
		}
	}
	return ""
}

func (d *dashboard) ensureFirstRunSetup() {
	if !d.setupRequired() {
		d.setupCode = ""
		_ = os.Remove(d.setupCodePath)
		return
	}
	setupCode := strings.TrimSpace(os.Getenv("DASHBOARD_SETUP_CODE"))
	if len(setupCode) < 16 {
		setupCode = setupCodeFromFile(d.setupCodePath)
	}
	if len(setupCode) < 16 {
		generated, err := randomSessionToken()
		if err != nil {
			log.Fatalf("Unable to generate first-run setup code: %v", err)
		}
		setupCode = generated
	}
	d.setupCode = setupCode
	publicHost := env("DASHBOARD_PUBLIC_HOST", "127.0.0.1")
	contents := fmt.Sprintf(`Ninj-OS Proxie first-run owner setup
Dashboard: http://%s:%s
Setup code: %s

Open the dashboard, create the owner username and password, and enter this single-use setup code.
This file is deleted automatically after setup succeeds.
`, publicHost, d.port, setupCode)
	if err := os.WriteFile(d.setupCodePath, []byte(contents), 0600); err != nil {
		log.Fatalf("Unable to save first-run setup code: %v", err)
	}
	_ = os.Chmod(d.setupCodePath, 0600)
}

func (d *dashboard) ensureSecurityFiles() {
	payload := d.loadUsersFile()

	if len(payload.Users) == 0 && os.Getenv("NINJOS_ENABLE_LEGACY_OWNER_TOKEN") == "1" && d.token != "" {
		username := strings.TrimSpace(env("DASHBOARD_OWNER_USERNAME", "owner"))
		if username == "" {
			username = "owner"
		}
		payload.Users = append(payload.Users, dashboardUser{
			Username: username, Role: "owner", TokenHash: tokenHash(d.token), Enabled: true,
		})
		payload.SetupComplete = true
	}
	for index, user := range payload.Users {
		if !strings.EqualFold(user.Role, "owner") {
			continue
		}
		if user.PasswordHash == "" && d.token != "" {
			payload.Users[index].TokenHash = tokenHash(d.token)
		}
		payload.Users[index].TOTPSecret = normalizeTOTPSecret(os.Getenv("DASHBOARD_TOTP_SECRET"))
	}

	// Preserve legacy installations and optional non-owner token accounts. A
	// fresh install deliberately receives no owner account until the browser
	// setup wizard completes.
	managed := map[string]dashboardUser{}
	if token := strings.TrimSpace(os.Getenv("DASHBOARD_OPERATOR_TOKEN")); token != "" {
		managed["operator"] = dashboardUser{Username: "operator", Role: "operator", TokenHash: tokenHash(token), Enabled: true}
	}
	if token := strings.TrimSpace(os.Getenv("DASHBOARD_READONLY_TOKEN")); token != "" {
		managed["observer"] = dashboardUser{Username: "observer", Role: "viewer", TokenHash: tokenHash(token), Enabled: true}
	}
	users := make([]dashboardUser, 0, len(payload.Users)+len(managed))
	for _, user := range payload.Users {
		name := strings.ToLower(strings.TrimSpace(user.Username))
		if _, isManaged := managed[name]; isManaged {
			continue
		}
		users = append(users, user)
	}
	names := make([]string, 0, len(managed))
	for name := range managed {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		users = append(users, managed[name])
	}
	payload.Users = users
	if err := d.saveUsersFile(payload); err != nil {
		log.Fatalf("Unable to initialize dashboard users: %v", err)
	}

	// Keep manually configured per-server secrets, but synchronize the default
	// fallback secret with COMPANION_SHARED_SECRET.
	secrets := map[string]string{}
	if bytes, err := os.ReadFile(d.companionSecretsPath); err == nil {
		for _, raw := range strings.Split(string(bytes), "\n") {
			line := strings.TrimSpace(raw)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				if key != "" && key != "default" {
					secrets[key] = strings.TrimSpace(parts[1])
				}
			}
		}
	}
	var builder strings.Builder
	builder.WriteString("# server_id=shared_secret\n# Use a unique secret per backend when possible.\n")
	if d.secret != "" {
		builder.WriteString("default=" + d.secret + "\n")
	}
	keys := make([]string, 0, len(secrets))
	for key := range secrets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		builder.WriteString(key + "=" + secrets[key] + "\n")
	}
	_ = os.WriteFile(d.companionSecretsPath, []byte(builder.String()), 0600)
}

func (d *dashboard) users() []dashboardUser {
	return d.loadUsersFile().Users
}

func (d *dashboard) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, 405, map[string]string{"error": "Method not allowed"})
		return
	}
	writeJSON(w, 200, map[string]any{
		"setupRequired":         d.setupRequired(),
		"minimumPasswordLength": 12,
		"setupFile":             filepath.Base(d.setupCodePath),
	})
}

func (d *dashboard) createBrowserSession(username, role string, totp bool) (string, int64, error) {
	token, err := randomSessionToken()
	if err != nil {
		return "", 0, err
	}
	expires := time.Now().Add(d.sessionTTL).UnixMilli()
	if err := d.ledger.createSession(tokenHash(token), username, role, expires, totp); err != nil {
		return "", 0, err
	}
	return token, expires, nil
}

func (d *dashboard) handleOwnerSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]string{"error": "Method not allowed"})
		return
	}
	var body struct {
		SetupCode string `json:"setupCode"`
		Username  string `json:"username"`
		Password  string `json:"password"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&body) != nil {
		writeJSON(w, 400, map[string]string{"error": "Invalid JSON"})
		return
	}
	username := strings.TrimSpace(body.Username)
	if err := validateOwnerUsername(username); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if err := validateOwnerPassword(username, body.Password); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}

	d.authMu.Lock()
	defer d.authMu.Unlock()
	if !d.setupRequired() {
		writeJSON(w, 409, map[string]string{"error": "Owner setup is already complete"})
		return
	}
	if d.setupCode == "" || !hmac.Equal([]byte(tokenHash(body.SetupCode)), []byte(tokenHash(d.setupCode))) {
		time.Sleep(300 * time.Millisecond)
		writeJSON(w, 401, map[string]string{"error": "Invalid first-run setup code"})
		return
	}
	passwordHash, passwordSalt, iterations, err := newPasswordCredentials(body.Password)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "Unable to secure owner password"})
		return
	}
	payload := d.loadUsersFile()
	users := make([]dashboardUser, 0, len(payload.Users)+1)
	for _, user := range payload.Users {
		if strings.EqualFold(user.Role, "owner") {
			continue
		}
		users = append(users, user)
	}
	users = append(users, dashboardUser{
		Username: username, Role: "owner", PasswordHash: passwordHash,
		PasswordSalt: passwordSalt, PasswordIterations: iterations,
		TOTPSecret: normalizeTOTPSecret(os.Getenv("DASHBOARD_TOTP_SECRET")), Enabled: true,
	})
	payload.SetupComplete = true
	payload.Users = users
	if err := d.saveUsersFile(payload); err != nil {
		writeJSON(w, 500, map[string]string{"error": "Unable to save owner account"})
		return
	}
	d.setupCode = ""
	_ = os.Remove(d.setupCodePath)
	if d.ledger != nil {
		d.ledger.clearSessions()
	}
	token, expires, err := d.createBrowserSession(username, "owner", false)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "Owner created, but browser session could not be created"})
		return
	}
	d.appendAudit(r, principal{Username: username, Role: "owner"}, "owner.setup", "success", nil)
	writeJSON(w, 201, map[string]any{
		"token": token, "expiresAt": expires,
		"principal": principal{Username: username, Role: "owner"},
	})
}

func (d *dashboard) handleOwnerAccount(w http.ResponseWriter, r *http.Request) {
	actor := principalFromRequest(r)
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, 200, map[string]any{"username": actor.Username, "role": actor.Role})
	case http.MethodPut:
		var body struct {
			CurrentPassword string `json:"currentPassword"`
			Username        string `json:"username"`
			Password        string `json:"password"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&body) != nil {
			writeJSON(w, 400, map[string]string{"error": "Invalid JSON"})
			return
		}
		username := strings.TrimSpace(body.Username)
		if err := validateOwnerUsername(username); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		if err := validateOwnerPassword(username, body.Password); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		d.authMu.Lock()
		defer d.authMu.Unlock()
		payload := d.loadUsersFile()
		ownerIndex := -1
		for index, user := range payload.Users {
			if user.Enabled && strings.EqualFold(user.Role, "owner") {
				ownerIndex = index
				break
			}
		}
		if ownerIndex < 0 {
			writeJSON(w, 409, map[string]string{"error": "Owner account is missing; use recovery setup"})
			return
		}
		owner := payload.Users[ownerIndex]
		if actor.Username != "recovery" && !verifyUserCredential(owner, body.CurrentPassword) {
			time.Sleep(250 * time.Millisecond)
			writeJSON(w, 401, map[string]string{"error": "Current password is incorrect"})
			return
		}
		passwordHash, passwordSalt, iterations, err := newPasswordCredentials(body.Password)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "Unable to secure owner password"})
			return
		}
		owner.Username = username
		owner.PasswordHash = passwordHash
		owner.PasswordSalt = passwordSalt
		owner.PasswordIterations = iterations
		owner.TokenHash = ""
		payload.Users[ownerIndex] = owner
		payload.SetupComplete = true
		if err := d.saveUsersFile(payload); err != nil {
			writeJSON(w, 500, map[string]string{"error": "Unable to update owner account"})
			return
		}
		if d.ledger != nil {
			d.ledger.clearSessions()
		}
		token, expires, err := d.createBrowserSession(username, "owner", owner.TOTPSecret != "")
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "Account updated, but browser session could not be created"})
			return
		}
		d.appendAudit(r, principal{Username: username, Role: "owner"}, "owner.credentials.update", "success", nil)
		writeJSON(w, 200, map[string]any{
			"token": token, "expiresAt": expires,
			"principal": principal{Username: username, Role: "owner"},
		})
	default:
		writeJSON(w, 405, map[string]string{"error": "Method not allowed"})
	}
}
