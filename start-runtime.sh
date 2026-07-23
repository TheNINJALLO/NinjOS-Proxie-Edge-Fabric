#!/usr/bin/env bash
set -Eeuo pipefail

readonly ROOT_DIR="${NINJOS_ROOT_DIR:-/home/container}"
readonly RELEASE_VERSION="7.3.8"
readonly CONFIG_DIR="${ROOT_DIR}/config"
RUNTIME_DIR="${ROOT_DIR}/runtime"
readonly CONFIG_FILE="${EDGE_CONFIG_FILE:-${CONFIG_DIR}/edge-fabric.ini}"
readonly GATEWAY_CONFIG="${ROOT_DIR}/gateway.conf"
readonly GENERATED_ENV="${RUNTIME_DIR}/generated/dashboard.env"
readonly FIRST_RUN_SETUP_FILE="${RUNTIME_DIR}/FIRST_RUN_SETUP.txt"
readonly LOCAL_NODE_BIN="${ROOT_DIR}/node-runtime/bin/node"

DASHBOARD_PID=""
GATEWAY_PID=""
SESSION_CORE_PID=""
CONSOLE_PID=""
readonly SESSION_CORE_TOKEN_FILE="${RUNTIME_DIR}/session-core.token"

log() { printf '[Ninj-OS Proxie] %s\n' "$*"; }
fail() { printf '[Ninj-OS Proxie] ERROR: %s\n' "$*" >&2; exit 1; }

print_first_run_setup() {
    if [[ -s "${FIRST_RUN_SETUP_FILE}" ]]; then
        local setup_code dashboard_url
        setup_code="$(awk -F': ' '$1 == "Setup code" {print $2; exit}' "${FIRST_RUN_SETUP_FILE}")"
        dashboard_url="$(awk -F': ' '$1 == "Dashboard" {print substr($0, index($0, ": ") + 2); exit}' "${FIRST_RUN_SETUP_FILE}")"
        printf '\n[Ninj-OS Proxie] FIRST-RUN OWNER SETUP REQUIRED\n'
        printf '[Ninj-OS Proxie] Dashboard : %s\n' "${dashboard_url:-http://127.0.0.1:${DASHBOARD_PORT}}"
        printf '[Ninj-OS Proxie] Setup code: %s\n' "${setup_code}"
        printf '[Ninj-OS Proxie] Open the dashboard and choose the owner username and password.\n'
        printf '[Ninj-OS Proxie] The setup code is deleted after successful setup.\n\n'
    else
        log "Dashboard owner setup is complete. Sign in with the configured username and password."
    fi
}

print_startup_splash() {
    local cyan="" blue="" green="" yellow="" bold="" reset=""
    if [[ -t 1 && "${TERM:-}" != "dumb" ]]; then
        cyan=$'\033[38;5;51m'
        blue=$'\033[38;5;39m'
        green=$'\033[38;5;82m'
        yellow=$'\033[38;5;220m'
        bold=$'\033[1m'
        reset=$'\033[0m'
    fi

    printf '\n%s%s' "${cyan}" "${bold}"
    cat <<'NINJOS_BANNER'
 _   _ ___ _   _     _        ___  ____
| \ | |_ _| \ | |   | |      / _ \/ ___|
|  \| || ||  \| |_  | |_____| | | \___ \
| |\  || || |\  | |_| |_____| |_| |___) |
|_| \_|___|_| \_|\___/        \___/|____/
                 P R O X I E
NINJOS_BANNER
    printf '%s' "${reset}"
    printf '%s%sVerified Identity Gateway%s\n' "${blue}" "${bold}" "${reset}"
    printf '%s━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n' "${blue}" "${reset}"
    printf '%s  Version         :%s v%s\n' "${blue}" "${reset}" "${RELEASE_VERSION}"
    printf '%s  Gateway Mode    :%s Universal dual-mode Bedrock edge\n' "${blue}" "${reset}"
    printf '%s  Player Identity :%s %sTransparent native auth or signed full-proxy identity%s\n' \
        "${blue}" "${reset}" "${green}" "${reset}"
    printf '%s  Configuration   :%s Unified dashboard-managed INI\n' "${blue}" "${reset}"
    printf '%s  Engine          :%s Ninj-OS Edge Datagram Engine\n' "${blue}" "${reset}"
    printf '%s  Implementation  :%s Ninj-OS protocol-agnostic transport core\n' "${blue}" "${reset}"
    printf '%s  Reference       :%s ProxyPass by SculkCatalystMC\n' "${blue}" "${reset}"
    printf '%s  Reference URL   :%s github.com/SculkCatalystMC/ProxyPass\n' "${blue}" "${reset}"
    printf '%s━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n' "${blue}" "${reset}"
    printf '%sBackends may use transparent online-mode=true or signed full-proxy identity.%s\n\n' "${green}" "${reset}"
}

configured_backends() {
    awk '
        /^\[backend\.[^]]+\]$/ {
            value=$0
            sub(/^\[backend\./, "", value)
            sub(/\]$/, "", value)
            names[++count]=value
        }
        END {
            if (count == 0) {
                printf "none"
                exit
            }
            for (i=1; i<=count; i++) {
                printf "%s%s", i == 1 ? "" : ", ", names[i]
            }
        }
    ' "${CONFIG_FILE}"
}

configured_backend_count() {
    awk '/^\[backend\.[^]]+\]$/ { count++ } END { print count + 0 }' "${CONFIG_FILE}"
}

print_runtime_details() {
    local public_host="${DASHBOARD_PUBLIC_HOST:-127.0.0.1}"
    local dashboard_url="http://${public_host}:${DASHBOARD_PORT}"
    local backend_names backend_count
    backend_names="$(configured_backends)"
    backend_count="$(configured_backend_count)"

    printf '[Ninj-OS Proxie] Startup settings applied from the unified configuration.\n'
    printf '[Ninj-OS Proxie] Configuration: %s\n' "${CONFIG_FILE}"
    printf '[Ninj-OS Proxie] Generated: %s\n' "${GATEWAY_CONFIG}"
    printf '[Ninj-OS Proxie] Dashboard ready: %s\n' "${dashboard_url}"
    printf '[Ninj-OS Proxie] Backends ready: %s configured (%s)\n' \
        "${backend_count}" "${backend_names}"
    printf '[Ninj-OS Proxie] Transfer pool: %s:%s-%s/UDP\n' \
        "${TRANSFER_PUBLIC_HOST:-${public_host}}" "${TRANSFER_PORT_START}" "${TRANSFER_PORT_END}"
    printf '[Ninj-OS Proxie] Configuration prepared; starting data-plane listeners...\n\n'
}

stop_dashboard() {
    if [[ -n "${DASHBOARD_PID}" ]] && kill -0 "${DASHBOARD_PID}" 2>/dev/null; then
        kill "${DASHBOARD_PID}" 2>/dev/null || true
        wait "${DASHBOARD_PID}" 2>/dev/null || true
    fi
    DASHBOARD_PID=""
}

stop_session_core() {
    if [[ -n "${SESSION_CORE_PID}" ]] && kill -0 "${SESSION_CORE_PID}" 2>/dev/null; then
        kill -TERM "${SESSION_CORE_PID}" 2>/dev/null || true
        wait "${SESSION_CORE_PID}" 2>/dev/null || true
    fi
    SESSION_CORE_PID=""
}

stop_gateway() {
    if [[ -n "${GATEWAY_PID}" ]] && kill -0 "${GATEWAY_PID}" 2>/dev/null; then
        kill -TERM "${GATEWAY_PID}" 2>/dev/null || true
        wait "${GATEWAY_PID}" 2>/dev/null || true
    fi
    GATEWAY_PID=""
}

shutdown() {
    stop_gateway
    stop_session_core
    stop_dashboard
    if [[ -n "${CONSOLE_PID}" ]] && kill -0 "${CONSOLE_PID}" 2>/dev/null; then
        kill "${CONSOLE_PID}" 2>/dev/null || true
    fi
}
trap shutdown EXIT INT TERM

ensure_session_core_token() {
    mkdir -p "${RUNTIME_DIR}"
    if [[ -n "${SESSION_CORE_TOKEN:-}" ]]; then
        return
    fi
    if [[ -s "${SESSION_CORE_TOKEN_FILE}" ]]; then
        SESSION_CORE_TOKEN="$(tr -d '\r\n' < "${SESSION_CORE_TOKEN_FILE}")"
    else
        SESSION_CORE_TOKEN="$(od -An -N32 -tx1 /dev/urandom | tr -d ' \n')"
        printf '%s\n' "${SESSION_CORE_TOKEN}" > "${SESSION_CORE_TOKEN_FILE}"
        chmod 600 "${SESSION_CORE_TOKEN_FILE}"
    fi
    export SESSION_CORE_TOKEN
}

prepare_config() {
    local legacy_owner_token="${DASHBOARD_TOKEN:-}"
    mkdir -p "${CONFIG_DIR}" "${RUNTIME_DIR}" "${RUNTIME_DIR}/generated"
    ensure_session_core_token

    if [[ ! -s "${CONFIG_FILE}" && -s "${ROOT_DIR}/edge-fabric.example.ini" ]]; then
        cp "${ROOT_DIR}/edge-fabric.example.ini" "${CONFIG_FILE}"
        chmod 600 "${CONFIG_FILE}"
        log "Created ${CONFIG_FILE} from the supplied example."
    fi

    if [[ -s "${ROOT_DIR}/routes.conf" && ! -e "${CONFIG_DIR}/.legacy-routes-migrated" ]]; then
        ./NinjOSDashboard --migrate-legacy "${ROOT_DIR}/routes.conf" "${CONFIG_FILE}"
        touch "${CONFIG_DIR}/.legacy-routes-migrated"
        log "Migrated the existing routes.conf into the unified configuration."
    fi

    ./NinjOSDashboard --prepare-config "${CONFIG_FILE}" "${RUNTIME_DIR}" "${GATEWAY_CONFIG}"
    [[ -s "${GENERATED_ENV}" ]] || fail "Configuration preparation did not create ${GENERATED_ENV}."

    # shellcheck disable=SC1090
    source "${GENERATED_ENV}"
    if [[ "${NINJOS_ENABLE_LEGACY_OWNER_TOKEN:-0}" == "1" && -z "${DASHBOARD_TOKEN:-}" ]]; then
        export DASHBOARD_TOKEN="${legacy_owner_token}"
    fi
    export GATEWAY_CONFIG_FILE="${GATEWAY_CONFIG}"
}

start_dashboard() {
    stop_dashboard
    local log_file="${RUNTIME_DIR}/dashboard.log"
    printf '[Ninj-OS Proxie] Starting dashboard on 0.0.0.0:%s/TCP\n' "${DASHBOARD_PORT}" > "${log_file}"
    ./NinjOSDashboard >> "${log_file}" 2>&1 &
    DASHBOARD_PID=$!

    local ready=0
    for _ in $(seq 1 50); do
        if ! kill -0 "${DASHBOARD_PID}" 2>/dev/null; then
            break
        fi
        if timeout 1 bash -c "</dev/tcp/127.0.0.1/${DASHBOARD_PORT}" 2>/dev/null; then
            ready=1
            break
        fi
        sleep 0.2
    done

    if [[ "${ready}" != 1 ]]; then
        cat "${log_file}" >&2 || true
        fail "Dashboard failed its internal health check on TCP port ${DASHBOARD_PORT}."
    fi
}

start_session_core() {
    stop_session_core
    if [[ "${FULL_PROXY_BACKEND_COUNT:-0}" == "0" ]]; then
        log "No full-proxy backends configured; protocol-aware session core is idle."
        return
    fi
    local node_command="node"
    if [[ -x "${LOCAL_NODE_BIN}" ]]; then
        node_command="${LOCAL_NODE_BIN}"
    elif ! command -v node >/dev/null 2>&1; then
        fail "Full Proxy mode requires Node.js 22 or newer. Install it or rerun the standalone installer. Transparent Auth mode does not require Node.js."
    fi
    local node_major
    node_major="$(${node_command} -p 'process.versions.node.split(".")[0]')"
    [[ "${node_major}" -ge 22 ]] || fail "Full Proxy mode requires Node.js 22 or newer; found $(${node_command} --version)."
    [[ -f ./session-core/src/index.js ]] || fail "session-core/src/index.js is missing from the runtime."
    [[ -d ./session-core/node_modules/bedrock-protocol ]] || fail "Bundled Bedrock protocol dependencies are missing."
    log "Starting protocol-aware full-proxy session core for ${FULL_PROXY_BACKEND_COUNT} backend(s)."
    rm -f "${RUNTIME_DIR}/session-core-state.json"
    SESSION_CORE_CONFIG="${SESSION_CORE_CONFIG:-${RUNTIME_DIR}/session-core.json}" "${node_command}" ./session-core/src/index.js &
    SESSION_CORE_PID=$!
    for _ in $(seq 1 50); do
        kill -0 "${SESSION_CORE_PID}" 2>/dev/null || fail "Full-proxy session core stopped during startup."
        [[ -s "${RUNTIME_DIR}/session-core-state.json" ]] && return
        sleep 0.2
    done
    fail "Full-proxy session core did not publish listener health within 10 seconds."
}

wait_for_gateway_ready() {
    for _ in $(seq 1 50); do
        kill -0 "${GATEWAY_PID}" 2>/dev/null || return 1
        [[ -s "${RUNTIME_DIR}/gateway-state.json" ]] && return 0
        sleep 0.2
    done
    return 1
}

announce_runtime_ready() {
    # Keep the legacy marker for already-imported eggs while new eggs use the
    # more accurate service-neutral readiness marker.
    log "Gateway ready"
    log "Runtime ready"
}

cd "${ROOT_DIR}"

# The splash is intentionally printed before file checks and configuration work.
# This guarantees visible startup identity even when initialization later fails.
print_startup_splash

[[ -x ./NinjOSEdge ]] || fail "NinjOSEdge is missing from the server root."
[[ -x ./NinjOSDashboard ]] || fail "NinjOSDashboard is missing from the server root."

prepare_config
start_dashboard
start_session_core
print_first_run_setup
print_runtime_details

forward_console() {
    local command
    while IFS= read -r command; do
        case "${command,,}" in
            stop|quit|exit)
                stop_gateway
                return
                ;;
        esac
    done
}
forward_console &
CONSOLE_PID=$!

if [[ "${TRANSPARENT_BACKEND_COUNT:-0}" == "0" ]]; then
    log "No transparent backends configured; waiting on the full-proxy session core."
    [[ -n "${SESSION_CORE_PID}" ]] || fail "No enabled backend services are configured."
    announce_runtime_ready
    wait "${SESSION_CORE_PID}"
    exit $?
fi

while true; do
    log "Starting transparent gateway process for ${TRANSPARENT_BACKEND_COUNT} backend(s)."
    set +e
    rm -f "${RUNTIME_DIR}/gateway-state.json"
    ./NinjOSEdge --config "${GATEWAY_CONFIG}" &
    GATEWAY_PID=$!
    gateway_ready=0
    if wait_for_gateway_ready; then
        gateway_ready=1
        announce_runtime_ready
    else
        kill -TERM "${GATEWAY_PID}" 2>/dev/null || true
    fi
    wait "${GATEWAY_PID}"
    exit_code=$?
    GATEWAY_PID=""
    set -e

    if [[ "${exit_code}" == 75 ]]; then
        log "Backend registry changed. Restarting the complete data plane."
        stop_session_core
        prepare_config
        start_session_core
        print_first_run_setup
        print_runtime_details
        continue
    fi

    if [[ "${exit_code}" == 76 ]]; then
        log "Dashboard-affecting configuration changed. Restarting managed services."
        stop_session_core
        prepare_config
        start_dashboard
        start_session_core
        print_first_run_setup
        print_runtime_details
        continue
    fi

    [[ "${gateway_ready}" == 1 ]] || fail "Transparent gateway did not bind and publish health within 10 seconds."

    stop_dashboard
    exit "${exit_code}"
done
