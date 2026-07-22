#!/usr/bin/env bash
set -Eeuo pipefail

readonly VERSION="7.3.3"
readonly INSTALL_ROOT="${NINJOS_INSTALL_ROOT:-/opt/ninjos-proxie}"
readonly SERVICE_USER="${NINJOS_SERVICE_USER:-ninjos}"
readonly SERVICE_GROUP="${NINJOS_SERVICE_GROUP:-ninjos}"
readonly ENV_FILE="${NINJOS_ENV_FILE:-/etc/ninjos-proxie.env}"
readonly UNIT_FILE="${NINJOS_UNIT_FILE:-/etc/systemd/system/ninjos-proxie.service}"
readonly SERVICE_NAME="${NINJOS_SERVICE_NAME:-ninjos-proxie.service}"
readonly SKIP_SYSTEMD="${NINJOS_SKIP_SYSTEMD:-0}"
readonly NODE_VERSION="22.23.1"
readonly NODE_LINUX_X64_SHA256="9749e988f437343b7fa832c69ded82a312e41a03116d766797ac14f6f9eee578"
readonly INSTALL_PORTABLE_NODE="${NINJOS_INSTALL_PORTABLE_NODE:-1}"

log() { printf '[Ninj-OS Standalone Installer] %s\n' "$*"; }
fail() { printf '[Ninj-OS Standalone Installer] ERROR: %s\n' "$*" >&2; exit 1; }

require_command() {
    command -v "$1" >/dev/null 2>&1 || fail "Required command not found: $1"
}

generate_secret() {
    if command -v openssl >/dev/null 2>&1; then
        openssl rand -hex 32
    else
        od -An -N32 -tx1 /dev/urandom | tr -d ' \n'
    fi
}


node_is_compatible() {
    command -v node >/dev/null 2>&1 || return 1
    local major
    major="$(node -p 'process.versions.node.split(".")[0]' 2>/dev/null || printf 0)"
    [[ "${major}" -ge 22 ]]
}

download_file() {
    local url="$1" destination="$2"
    if command -v curl >/dev/null 2>&1; then
        curl --fail --location --silent --show-error "${url}" --output "${destination}"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO "${destination}" "${url}"
    else
        fail "Node.js installation requires curl or wget. Install Node.js 22 manually or install one of those download tools."
    fi
}

install_portable_node() {
    if [[ ! -d "${INSTALL_ROOT}/session-core" ]]; then
        return 0
    fi
    if node_is_compatible; then
        log "System Node.js $(node --version) is compatible with Full Proxy Mode."
        return
    fi
    [[ "${INSTALL_PORTABLE_NODE}" == "1" ]] || fail "Node.js 22+ is required for Full Proxy Mode and portable installation was disabled."
    [[ "$(uname -m)" == "x86_64" || "$(uname -m)" == "amd64" ]] || fail "Automatic Node.js installation currently supports Linux x86_64. Install Node.js 22+ manually on this architecture."
    require_command xz
    local archive="${stage}/node-v${NODE_VERSION}-linux-x64.tar.xz"
    local url="https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-x64.tar.xz"
    log "Installing the pinned Node.js ${NODE_VERSION} runtime for Full Proxy Mode."
    download_file "${url}" "${archive}"
    local actual
    actual="$(sha256sum "${archive}" | awk '{print tolower($1)}')"
    [[ "${actual}" == "${NODE_LINUX_X64_SHA256}" ]] || fail "Node.js runtime checksum mismatch."
    rm -rf "${INSTALL_ROOT}/node-runtime"
    install -d -m 0755 "${INSTALL_ROOT}/node-runtime"
    tar -xJf "${archive}" --strip-components=1 -C "${INSTALL_ROOT}/node-runtime"
    "${INSTALL_ROOT}/node-runtime/bin/node" --version >/dev/null
}

select_archive() {
    if [[ $# -ge 1 && -f "$1" ]]; then
        readlink -f "$1"
        return 0
    fi
    local candidate
    candidate="$(find . -maxdepth 1 -type f -name "NinjOS-Proxie-Edge-Fabric-v${VERSION}-Runtime.tar.gz" -print -quit)"
    [[ -n "${candidate}" ]] || return 1
    readlink -f "${candidate}"
}

verify_archive() {
    local archive="$1" sidecar="${archive}.sha256" expected actual
    if [[ -s "${sidecar}" ]]; then
        expected="$(awk 'NR==1 {print tolower($1)}' "${sidecar}")"
        [[ "${expected}" =~ ^[0-9a-f]{64}$ ]] || fail "Invalid SHA-256 value in ${sidecar}."
        actual="$(sha256sum "${archive}" | awk '{print tolower($1)}')"
        [[ "${actual}" == "${expected}" ]] || fail "Runtime checksum mismatch."
        log "SHA-256 verified."
    else
        log "No checksum sidecar found at ${sidecar}; continuing after archive validation."
    fi
}

write_environment() {
    if [[ -s "${ENV_FILE}" ]]; then
        log "Preserving existing environment file ${ENV_FILE}."
        return
    fi
    install -d -m 0755 "$(dirname "${ENV_FILE}")"
    umask 077
    cat > "${ENV_FILE}" <<ENVEOF
NINJOS_ROOT_DIR=${INSTALL_ROOT}
DASHBOARD_TOKEN=
DASHBOARD_SETUP_CODE=
DASHBOARD_RECOVERY_TOKEN=
SESSION_CORE_TOKEN=$(generate_secret)
COMPANION_SHARED_SECRET=$(generate_secret)
DASHBOARD_OPERATOR_TOKEN=
DASHBOARD_READONLY_TOKEN=
METRICS_TOKEN=
DASHBOARD_TOTP_SECRET=
DISCORD_WEBHOOK_URL=
DISCORD_BOT_TOKEN=
DISCORD_CHANNEL_ID=
COMPANION_KINGDOM_SECRET=
COMPANION_ZOO_SECRET=
COMPANION_LOBBY_SECRET=
VANILLA_AGENT_TOKEN=
ENVEOF
    chmod 0600 "${ENV_FILE}"
    log "Created ${ENV_FILE} with generated secrets."
}

write_unit() {
    install -d -m 0755 "$(dirname "${UNIT_FILE}")"
    cat > "${UNIT_FILE}" <<UNITEOF
[Unit]
Description=Ninj-OS Proxie Edge Fabric
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${SERVICE_USER}
Group=${SERVICE_GROUP}
WorkingDirectory=${INSTALL_ROOT}
EnvironmentFile=${ENV_FILE}
ExecStart=${INSTALL_ROOT}/start-runtime.sh
Restart=on-failure
RestartSec=3
TimeoutStopSec=20
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=full
ProtectHome=true
ReadWritePaths=${INSTALL_ROOT}
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
UNITEOF
}

for command in tar sha256sum awk find install cp chmod chown readlink; do
    require_command "${command}"
done

if [[ "${SKIP_SYSTEMD}" != "1" && "${EUID}" -ne 0 ]]; then
    fail "Run as root with sudo, or set NINJOS_SKIP_SYSTEMD=1 for a custom test installation."
fi

archive="$(select_archive "${1:-}")" || fail "Pass the v${VERSION} runtime archive as the first argument."
verify_archive "${archive}"

stage="$(mktemp -d)"
trap 'rm -rf "${stage}"' EXIT
tar -xzf "${archive}" -C "${stage}"

[[ ! -e "${stage}/config" && ! -e "${stage}/runtime" ]] || fail "Archive contains persistent data directories and was rejected."
for required in NinjOSEdge NinjOSDashboard start-runtime.sh ninjos-dashboard-account.sh edge-fabric.example.ini manifest.json; do
    [[ -f "${stage}/${required}" ]] || fail "Archive is missing ${required}."
done

if [[ "${SKIP_SYSTEMD}" != "1" ]]; then
    for command in getent groupadd useradd systemctl; do
        require_command "${command}"
    done
    if ! getent group "${SERVICE_GROUP}" >/dev/null 2>&1; then
        groupadd --system "${SERVICE_GROUP}"
    fi
    if ! id "${SERVICE_USER}" >/dev/null 2>&1; then
        useradd --system --gid "${SERVICE_GROUP}" --home-dir "${INSTALL_ROOT}" --shell /usr/sbin/nologin "${SERVICE_USER}"
    fi
    systemctl stop "${SERVICE_NAME}" >/dev/null 2>&1 || true
else
    id "${SERVICE_USER}" >/dev/null 2>&1 || fail "Test service user ${SERVICE_USER} does not exist."
fi

install -d -m 0755 "${INSTALL_ROOT}" "${INSTALL_ROOT}/config" "${INSTALL_ROOT}/runtime"
cp -a "${stage}/." "${INSTALL_ROOT}/"
chmod 0755 "${INSTALL_ROOT}/NinjOSEdge" "${INSTALL_ROOT}/NinjOSDashboard" "${INSTALL_ROOT}/start-runtime.sh" "${INSTALL_ROOT}/ninjos-dashboard-account.sh"

if [[ ! -s "${INSTALL_ROOT}/config/edge-fabric.ini" ]]; then
    cp "${INSTALL_ROOT}/edge-fabric.example.ini" "${INSTALL_ROOT}/config/edge-fabric.ini"
    chmod 0600 "${INSTALL_ROOT}/config/edge-fabric.ini"
fi

install_portable_node
write_environment
chown -R "${SERVICE_USER}:${SERVICE_GROUP}" "${INSTALL_ROOT}"

if [[ "${SKIP_SYSTEMD}" != "1" ]]; then
    write_unit
    systemctl daemon-reload
    systemctl enable --now "${SERVICE_NAME}"
    log "Service installed and started."
    log "Check status with: systemctl status ${SERVICE_NAME}"
    setup_file="${INSTALL_ROOT}/runtime/FIRST_RUN_SETUP.txt"
    for _ in $(seq 1 40); do
        [[ -s "${setup_file}" ]] && break
        sleep 0.25
    done
    if [[ -s "${setup_file}" ]]; then
        setup_code="$(awk -F': ' '$1 == "Setup code" {print $2; exit}' "${setup_file}")"
        printf '[Ninj-OS Standalone Installer] FIRST-RUN SETUP CODE: %s\n' "${setup_code}"
        log "Open the dashboard and choose the owner username and password."
        log "The setup details are stored temporarily in ${setup_file}."
    else
        log "Read the first-run setup code with: journalctl -u ${SERVICE_NAME} -n 100"
    fi
    log "Account status/reset utility: ${INSTALL_ROOT}/ninjos-dashboard-account.sh"
else
    log "Installed files in ${INSTALL_ROOT}; systemd setup was skipped."
fi

log "Edit ${INSTALL_ROOT}/config/edge-fabric.ini or use the dashboard to replace the bundled example network values."
