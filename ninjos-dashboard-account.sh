#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="${NINJOS_ROOT_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)}"
RUNTIME_DIR="${RUNTIME_DIR:-${ROOT_DIR}/runtime}"
USERS_FILE="${RUNTIME_DIR}/dashboard-users.json"
SETUP_FILE="${RUNTIME_DIR}/FIRST_RUN_SETUP.txt"

log() { printf '[Ninj-OS Dashboard Account] %s\n' "$*"; }
fail() { printf '[Ninj-OS Dashboard Account] ERROR: %s\n' "$*" >&2; exit 1; }

status() {
    if [[ -s "${SETUP_FILE}" ]]; then
        printf 'Status: first-run setup required\n'
        cat "${SETUP_FILE}"
        return
    fi
    if [[ ! -s "${USERS_FILE}" ]]; then
        printf 'Status: owner account is not initialized\n'
        printf 'Restart Ninj-OS Proxie to generate a first-run setup code.\n'
        return
    fi
    python3 - "${USERS_FILE}" <<'PY'
import json, sys
path=sys.argv[1]
try:
    data=json.load(open(path, encoding='utf-8'))
except Exception as exc:
    print(f'Status: unable to read owner account: {exc}')
    raise SystemExit(1)
owner=next((u for u in data.get('users',[]) if str(u.get('role','')).lower()=='owner' and u.get('enabled',False)),None)
if not data.get('setupComplete') or owner is None:
    print('Status: first-run setup required after restart')
else:
    mode='password' if owner.get('passwordHash') else 'legacy token'
    print('Status: owner account configured')
    print(f'Username: {owner.get("username", "unknown")}')
    print(f'Login mode: {mode}')
PY
}

reset_setup() {
    mkdir -p "${RUNTIME_DIR}"
    stamp="$(date -u +%Y%m%dT%H%M%SZ)"
    if [[ -s "${USERS_FILE}" ]]; then
        backup="${USERS_FILE}.${stamp}.bak"
        cp -p "${USERS_FILE}" "${backup}"
        log "Backed up the existing account file to ${backup}."
    fi
    rm -f "${USERS_FILE}" "${SETUP_FILE}" \
        "${RUNTIME_DIR}/DASHBOARD_LOGIN.txt" \
        "${RUNTIME_DIR}/dashboard-owner-token.txt"
    log "Owner setup has been reset."
    log "Restart the service, then use the new setup code printed in the console."
}

case "${1:-status}" in
    status|show)
        status
        ;;
    reset-setup)
        reset_setup
        ;;
    *)
        cat >&2 <<USAGE
Usage:
  $0 status
  $0 reset-setup

For a safer remote recovery, set DASHBOARD_RECOVERY_TOKEN, restart, sign in as
username 'recovery', replace the owner username/password, then clear the recovery
variable and restart again.
USAGE
        exit 2
        ;;
esac
