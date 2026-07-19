// Copyright (C) 2026 Ninj-OS contributors.
// SPDX-License-Identifier: AGPL-3.0-only

package main

/*
#cgo LDFLAGS: -lsqlite3 -ldl -lpthread -lm
#include <sqlite3.h>
#include <stdlib.h>

static int ninjos_bind_text(sqlite3_stmt *stmt, int index, const char *value) {
    return sqlite3_bind_text(stmt, index, value, -1, SQLITE_TRANSIENT);
}
*/
import "C"

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"
)

type ledger struct {
	db *C.sqlite3
	mu sync.Mutex
}

type ledgerRow map[string]any

func openLedger(path string) (*ledger, error) {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))

	var db *C.sqlite3
	flags := C.int(C.SQLITE_OPEN_READWRITE | C.SQLITE_OPEN_CREATE | C.SQLITE_OPEN_FULLMUTEX)
	if rc := C.sqlite3_open_v2(cpath, &db, flags, nil); rc != C.SQLITE_OK {
		message := "unable to open SQLite ledger"
		if db != nil {
			message = C.GoString(C.sqlite3_errmsg(db))
			C.sqlite3_close(db)
		}
		return nil, errors.New(message)
	}

	l := &ledger{db: db}
	schema := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=ON",
		`CREATE TABLE IF NOT EXISTS transfer_transactions (
			id TEXT PRIMARY KEY,
			ticket_id TEXT UNIQUE,
			xuid TEXT,
			player_name TEXT,
			source_server TEXT,
			destination TEXT,
			source_ip TEXT,
			proxy_port INTEGER,
			state TEXT NOT NULL,
			requested_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			proxy_connected_at INTEGER,
			arrived_at INTEGER,
			failure_reason TEXT
		)`,
		"CREATE INDEX IF NOT EXISTS idx_transfers_xuid_updated ON transfer_transactions(xuid, updated_at DESC)",
		"CREATE INDEX IF NOT EXISTS idx_transfers_state_updated ON transfer_transactions(state, updated_at DESC)",
		`CREATE TABLE IF NOT EXISTS audit_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts INTEGER NOT NULL,
			actor TEXT,
			role TEXT,
			action TEXT,
			result TEXT,
			remote_ip TEXT,
			details_json TEXT
		)`,
		"CREATE INDEX IF NOT EXISTS idx_audit_ts ON audit_log(ts DESC)",
		`CREATE TABLE IF NOT EXISTS policy_versions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts INTEGER NOT NULL,
			actor TEXT,
			content TEXT NOT NULL,
			content_hash TEXT NOT NULL,
			result TEXT,
			active INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS player_profiles (
			xuid TEXT PRIMARY KEY,
			gamertag TEXT,
			first_seen INTEGER NOT NULL,
			last_seen INTEGER NOT NULL,
			current_server TEXT,
			network_role TEXT,
			access_json TEXT NOT NULL DEFAULT '{}',
			network_banned INTEGER NOT NULL DEFAULT 0,
			notes TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS metric_samples (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts INTEGER NOT NULL,
			scope TEXT NOT NULL,
			server_id TEXT,
			name TEXT NOT NULL,
			value REAL NOT NULL
		)`,
		"CREATE INDEX IF NOT EXISTS idx_metrics_lookup ON metric_samples(name, server_id, ts DESC)",
		`CREATE TABLE IF NOT EXISTS alerts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts INTEGER NOT NULL,
			rule_name TEXT NOT NULL,
			severity TEXT NOT NULL,
			server_id TEXT,
			message TEXT NOT NULL,
			state TEXT NOT NULL DEFAULT 'open',
			resolved_at INTEGER
		)`,
		"CREATE INDEX IF NOT EXISTS idx_alerts_ts ON alerts(ts DESC)",
		`CREATE TABLE IF NOT EXISTS dashboard_sessions (
			token_hash TEXT PRIMARY KEY,
			username TEXT NOT NULL,
			role TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL,
			last_seen INTEGER NOT NULL,
			totp_verified INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS firewall_snapshots (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts INTEGER NOT NULL,
			ip TEXT,
			risk INTEGER,
			ban_level INTEGER,
			banned_until INTEGER,
			reason TEXT
		)`,
	}
	for _, statement := range schema {
		if err := l.exec(statement); err != nil {
			l.close()
			return nil, fmt.Errorf("initialize SQLite ledger: %w", err)
		}
	}
	return l, nil
}

func (l *ledger) close() {
	if l == nil || l.db == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	C.sqlite3_close(l.db)
	l.db = nil
}

func (l *ledger) exec(statement string, args ...any) error {
	if l == nil || l.db == nil {
		return errors.New("ledger unavailable")
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	csql := C.CString(statement)
	defer C.free(unsafe.Pointer(csql))
	var stmt *C.sqlite3_stmt
	if rc := C.sqlite3_prepare_v2(l.db, csql, -1, &stmt, nil); rc != C.SQLITE_OK {
		return errors.New(C.GoString(C.sqlite3_errmsg(l.db)))
	}
	defer C.sqlite3_finalize(stmt)

	for index, value := range args {
		position := C.int(index + 1)
		var rc C.int
		switch typed := value.(type) {
		case nil:
			rc = C.sqlite3_bind_null(stmt, position)
		case string:
			cvalue := C.CString(typed)
			rc = C.ninjos_bind_text(stmt, position, cvalue)
			C.free(unsafe.Pointer(cvalue))
		case []byte:
			cvalue := C.CBytes(typed)
			rc = C.sqlite3_bind_blob(stmt, position, cvalue, C.int(len(typed)), nil)
			C.free(cvalue)
		case bool:
			if typed {
				rc = C.sqlite3_bind_int64(stmt, position, 1)
			} else {
				rc = C.sqlite3_bind_int64(stmt, position, 0)
			}
		case int:
			rc = C.sqlite3_bind_int64(stmt, position, C.sqlite3_int64(typed))
		case int64:
			rc = C.sqlite3_bind_int64(stmt, position, C.sqlite3_int64(typed))
		case uint64:
			rc = C.sqlite3_bind_int64(stmt, position, C.sqlite3_int64(typed))
		case float64:
			rc = C.sqlite3_bind_double(stmt, position, C.double(typed))
		default:
			rc = C.ninjos_bind_text(stmt, position, C.CString(fmt.Sprint(value)))
		}
		if rc != C.SQLITE_OK {
			return errors.New(C.GoString(C.sqlite3_errmsg(l.db)))
		}
	}

	rc := C.sqlite3_step(stmt)
	if rc != C.SQLITE_DONE && rc != C.SQLITE_ROW {
		return errors.New(C.GoString(C.sqlite3_errmsg(l.db)))
	}
	return nil
}

func (l *ledger) query(statement string, args ...any) ([]ledgerRow, error) {
	if l == nil || l.db == nil {
		return nil, errors.New("ledger unavailable")
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	csql := C.CString(statement)
	defer C.free(unsafe.Pointer(csql))
	var stmt *C.sqlite3_stmt
	if rc := C.sqlite3_prepare_v2(l.db, csql, -1, &stmt, nil); rc != C.SQLITE_OK {
		return nil, errors.New(C.GoString(C.sqlite3_errmsg(l.db)))
	}
	defer C.sqlite3_finalize(stmt)

	for index, value := range args {
		position := C.int(index + 1)
		var rc C.int
		switch typed := value.(type) {
		case nil:
			rc = C.sqlite3_bind_null(stmt, position)
		case string:
			cvalue := C.CString(typed)
			rc = C.ninjos_bind_text(stmt, position, cvalue)
			C.free(unsafe.Pointer(cvalue))
		case int:
			rc = C.sqlite3_bind_int64(stmt, position, C.sqlite3_int64(typed))
		case int64:
			rc = C.sqlite3_bind_int64(stmt, position, C.sqlite3_int64(typed))
		case float64:
			rc = C.sqlite3_bind_double(stmt, position, C.double(typed))
		default:
			cvalue := C.CString(fmt.Sprint(value))
			rc = C.ninjos_bind_text(stmt, position, cvalue)
			C.free(unsafe.Pointer(cvalue))
		}
		if rc != C.SQLITE_OK {
			return nil, errors.New(C.GoString(C.sqlite3_errmsg(l.db)))
		}
	}

	rows := make([]ledgerRow, 0)
	for {
		rc := C.sqlite3_step(stmt)
		if rc == C.SQLITE_DONE {
			break
		}
		if rc != C.SQLITE_ROW {
			return nil, errors.New(C.GoString(C.sqlite3_errmsg(l.db)))
		}
		row := ledgerRow{}
		columns := int(C.sqlite3_column_count(stmt))
		for index := 0; index < columns; index++ {
			name := C.GoString(C.sqlite3_column_name(stmt, C.int(index)))
			switch C.sqlite3_column_type(stmt, C.int(index)) {
			case C.SQLITE_INTEGER:
				row[name] = int64(C.sqlite3_column_int64(stmt, C.int(index)))
			case C.SQLITE_FLOAT:
				row[name] = float64(C.sqlite3_column_double(stmt, C.int(index)))
			case C.SQLITE_NULL:
				row[name] = nil
			default:
				text := C.sqlite3_column_text(stmt, C.int(index))
				if text == nil {
					row[name] = ""
				} else {
					row[name] = C.GoString((*C.char)(unsafe.Pointer(text)))
				}
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (l *ledger) recordAudit(ts int64, actor, role, action, result, remoteIP string, details any) {
	bytes, _ := json.Marshal(details)
	_ = l.exec(
		`INSERT INTO audit_log(ts, actor, role, action, result, remote_ip, details_json)
		 VALUES(?,?,?,?,?,?,?)`,
		ts, actor, role, action, result, remoteIP, string(bytes),
	)
}

func (l *ledger) createTransfer(ticket transferTicket) {
	_ = l.exec(
		`INSERT OR REPLACE INTO transfer_transactions
		 (id,ticket_id,xuid,player_name,source_server,destination,source_ip,proxy_port,state,requested_at,updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		ticket.ID, ticket.ID, ticket.XUID, ticket.PlayerName, ticket.SourceServer,
		ticket.Destination, ticket.SourceIP, ticket.Port, "ticketed",
		ticket.CreatedAt, ticket.CreatedAt,
	)
}

func (l *ledger) updateTransfer(ticketID, state, failure string, timestamp int64) {
	if timestamp == 0 {
		timestamp = time.Now().UnixMilli()
	}
	switch state {
	case "proxy_connected":
		_ = l.exec(
			`UPDATE transfer_transactions SET state=?, updated_at=?, proxy_connected_at=?,
			 failure_reason=? WHERE ticket_id=? AND state='ticketed'`,
			state, timestamp, timestamp, failure, ticketID,
		)
	case "arrived":
		_ = l.exec(
			`UPDATE transfer_transactions SET state=?, updated_at=?, arrived_at=?,
			 failure_reason=? WHERE ticket_id=?`,
			state, timestamp, timestamp, failure, ticketID,
		)
	default:
		_ = l.exec(
			`UPDATE transfer_transactions SET state=?, updated_at=?, failure_reason=?
			 WHERE ticket_id=?`,
			state, timestamp, failure, ticketID,
		)
	}
}

func (l *ledger) confirmArrival(xuid, serverID string, timestamp int64) {
	if xuid == "" || serverID == "" {
		return
	}
	_ = l.exec(
		`UPDATE transfer_transactions
		 SET state='arrived', updated_at=?, arrived_at=?
		 WHERE id=(
		   SELECT id FROM transfer_transactions
		   WHERE xuid=? AND destination=? AND state IN ('ticketed','proxy_connected')
		     AND requested_at>=?
		   ORDER BY requested_at DESC LIMIT 1
		 )`,
		timestamp, timestamp, xuid, serverID, timestamp-5*60*1000,
	)
}

func (l *ledger) expireTransfers(now int64) {
	_ = l.exec(
		`UPDATE transfer_transactions SET state='expired', updated_at=?,
		 failure_reason='arrival not confirmed before transaction timeout'
		 WHERE state IN ('ticketed','proxy_connected') AND requested_at<?`,
		now, now-5*60*1000,
	)
}

func (l *ledger) recentTransfers(limit int) []ledgerRow {
	if limit < 1 {
		limit = 100
	}
	if limit > 5000 {
		limit = 5000
	}
	rows, _ := l.query(
		`SELECT id,ticket_id,xuid,player_name,source_server,destination,source_ip,
		        proxy_port,state,requested_at,updated_at,proxy_connected_at,arrived_at,
		        failure_reason
		 FROM transfer_transactions ORDER BY requested_at DESC LIMIT ?`,
		limit,
	)
	return rows
}

func (l *ledger) upsertProfile(xuid, gamertag, serverID string, timestamp int64) {
	if xuid == "" {
		return
	}
	_ = l.exec(
		`INSERT INTO player_profiles
		 (xuid,gamertag,first_seen,last_seen,current_server,network_role,access_json,network_banned,notes)
		 VALUES(?,?,?,?,?,'member','{}',0,'')
		 ON CONFLICT(xuid) DO UPDATE SET
		   gamertag=excluded.gamertag,
		   last_seen=excluded.last_seen,
		   current_server=excluded.current_server`,
		xuid, gamertag, timestamp, timestamp, serverID,
	)
}

func (l *ledger) profileIdentity(xuid string) (string, bool, map[string]bool, string) {
	if strings.TrimSpace(xuid) == "" {
		return "member", false, map[string]bool{}, ""
	}
	rows, err := l.query(`SELECT network_role,network_banned,access_json,notes FROM player_profiles WHERE xuid=? LIMIT 1`, xuid)
	if err != nil || len(rows) == 0 {
		return "member", false, map[string]bool{}, ""
	}
	role, _ := rows[0]["network_role"].(string)
	if role == "" {
		role = "member"
	}
	banned := false
	switch value := rows[0]["network_banned"].(type) {
	case int64:
		banned = value != 0
	case int:
		banned = value != 0
	}
	access := map[string]bool{}
	raw, _ := rows[0]["access_json"].(string)
	_ = json.Unmarshal([]byte(raw), &access)
	notes, _ := rows[0]["notes"].(string)
	return role, banned, access, notes
}

func (l *ledger) profileAccess(xuid, destination string) (bool, string) {
	if xuid == "" {
		return true, ""
	}
	rows, err := l.query(
		`SELECT network_banned,access_json FROM player_profiles WHERE xuid=? LIMIT 1`,
		xuid,
	)
	if err != nil || len(rows) == 0 {
		return true, ""
	}
	if banned, ok := rows[0]["network_banned"].(int64); ok && banned != 0 {
		return false, "Player is banned from the Ninj-OS network"
	}
	raw, _ := rows[0]["access_json"].(string)
	access := map[string]bool{}
	_ = json.Unmarshal([]byte(raw), &access)
	if allowed, exists := access[destination]; exists && !allowed {
		return false, "Player does not have access to destination " + destination
	}
	return true, ""
}

func (l *ledger) profiles(limit int) []ledgerRow {
	if limit < 1 {
		limit = 500
	}
	if limit > 10000 {
		limit = 10000
	}
	rows, _ := l.query(
		`SELECT xuid,gamertag,first_seen,last_seen,current_server,network_role,
		        access_json,network_banned,notes
		 FROM player_profiles ORDER BY last_seen DESC LIMIT ?`,
		limit,
	)
	return rows
}

func (l *ledger) updateProfileAccess(xuid, role string, banned bool, access map[string]bool, notes string) error {
	bytes, _ := json.Marshal(access)
	return l.exec(
		`UPDATE player_profiles SET network_role=?,network_banned=?,access_json=?,notes=?
		 WHERE xuid=?`,
		role, banned, string(bytes), notes, xuid,
	)
}

func (l *ledger) recordPolicy(ts int64, actor, content, hash, result string, active bool) {
	if active {
		_ = l.exec("UPDATE policy_versions SET active=0 WHERE active=1")
	}
	_ = l.exec(
		`INSERT INTO policy_versions(ts,actor,content,content_hash,result,active)
		 VALUES(?,?,?,?,?,?)`,
		ts, actor, content, hash, result, active,
	)
}

func (l *ledger) recordMetric(ts int64, scope, serverID, name string, value float64) {
	_ = l.exec(
		`INSERT INTO metric_samples(ts,scope,server_id,name,value) VALUES(?,?,?,?,?)`,
		ts, scope, serverID, name, value,
	)
}

func (l *ledger) metricHistory(name, serverID string, since int64, limit int) []ledgerRow {
	if limit < 1 {
		limit = 1000
	}
	if limit > 10000 {
		limit = 10000
	}
	query := `SELECT ts,scope,server_id,name,value FROM metric_samples
	          WHERE name=? AND ts>=?`
	args := []any{name, since}
	if serverID != "" {
		query += " AND server_id=?"
		args = append(args, serverID)
	}
	query += " ORDER BY ts ASC LIMIT ?"
	args = append(args, limit)
	rows, _ := l.query(query, args...)
	return rows
}

func (l *ledger) pruneMetrics(before int64) {
	_ = l.exec("DELETE FROM metric_samples WHERE ts<?", before)
}

func (l *ledger) recordAlert(rule, severity, serverID, message string) {
	now := time.Now().UnixMilli()
	_ = l.exec(
		`INSERT INTO alerts(ts,rule_name,severity,server_id,message,state)
		 VALUES(?,?,?,?,?,'open')`,
		now, rule, severity, serverID, message,
	)
}

func (l *ledger) alerts(limit int) []ledgerRow {
	if limit < 1 {
		limit = 200
	}
	rows, _ := l.query(
		`SELECT id,ts,rule_name,severity,server_id,message,state,resolved_at
		 FROM alerts ORDER BY ts DESC LIMIT ?`,
		limit,
	)
	return rows
}

func (l *ledger) createSession(tokenHash, username, role string, expiresAt int64, totp bool) error {
	now := time.Now().UnixMilli()
	return l.exec(
		`INSERT OR REPLACE INTO dashboard_sessions
		 (token_hash,username,role,created_at,expires_at,last_seen,totp_verified)
		 VALUES(?,?,?,?,?,?,?)`,
		tokenHash, username, role, now, expiresAt, now, totp,
	)
}

func (l *ledger) session(tokenHash string) (principal, bool) {
	now := time.Now().UnixMilli()
	rows, err := l.query(
		`SELECT username,role,expires_at FROM dashboard_sessions
		 WHERE token_hash=? LIMIT 1`,
		tokenHash,
	)
	if err != nil || len(rows) == 0 {
		return principal{}, false
	}
	expires, _ := rows[0]["expires_at"].(int64)
	if expires <= now {
		_ = l.exec("DELETE FROM dashboard_sessions WHERE token_hash=?", tokenHash)
		return principal{}, false
	}
	username, _ := rows[0]["username"].(string)
	role, _ := rows[0]["role"].(string)
	_ = l.exec(
		"UPDATE dashboard_sessions SET last_seen=? WHERE token_hash=?",
		now, tokenHash,
	)
	return principal{Username: username, Role: role}, true
}

func (l *ledger) deleteSession(tokenHash string) {
	_ = l.exec("DELETE FROM dashboard_sessions WHERE token_hash=?", tokenHash)
}

func (l *ledger) cleanupSessions() {
	_ = l.exec(
		"DELETE FROM dashboard_sessions WHERE expires_at<?",
		time.Now().UnixMilli(),
	)
}

func (l *ledger) clearSessions() {
	_ = l.exec("DELETE FROM dashboard_sessions")
}

func (l *ledger) firewallSnapshot(ts int64, rows []map[string]any) {
	for _, row := range rows {
		ip := fmt.Sprint(row["ip"])
		risk, _ := strconv.Atoi(fmt.Sprint(row["risk"]))
		level, _ := strconv.Atoi(fmt.Sprint(row["banLevel"]))
		bannedUntil, _ := strconv.ParseInt(fmt.Sprint(row["bannedUntil"]), 10, 64)
		reason := fmt.Sprint(row["reason"])
		_ = l.exec(
			`INSERT INTO firewall_snapshots(ts,ip,risk,ban_level,banned_until,reason)
			 VALUES(?,?,?,?,?,?)`,
			ts, ip, risk, level, bannedUntil, reason,
		)
	}
}
