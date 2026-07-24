#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TEMP="$(mktemp -d)"
trap 'rm -rf "${TEMP}"' EXIT
STAGE="${TEMP}/stage"
INSTALL_ROOT="${TEMP}/install"
ENV_FILE="${TEMP}/ninjos.env"
ARCHIVE="${TEMP}/NinjOS-Proxie-Edge-Fabric-v7.3.16-Runtime.tar.gz"
mkdir -p "${STAGE}"

cp "${ROOT}/prebuilt/linux-x86_64/NinjOSEdge" "${STAGE}/"
cp "${ROOT}/prebuilt/linux-x86_64/NinjOSDashboard" "${STAGE}/"
cp "${ROOT}/start-runtime.sh" "${STAGE}/"
cp "${ROOT}/ninjos-dashboard-account.sh" "${STAGE}/"
cp "${ROOT}/config/edge-fabric.example.ini" "${STAGE}/"
cp "${ROOT}/manifest.json" "${STAGE}/"
chmod +x "${STAGE}/NinjOSEdge" "${STAGE}/NinjOSDashboard" "${STAGE}/start-runtime.sh" "${STAGE}/ninjos-dashboard-account.sh"
tar -C "${STAGE}" -czf "${ARCHIVE}" .
sha256sum "${ARCHIVE}" | sed 's# .*/#  #' > "${ARCHIVE}.sha256"

run_installer() {
    NINJOS_SKIP_SYSTEMD=1 \
    NINJOS_INSTALL_ROOT="${INSTALL_ROOT}" \
    NINJOS_ENV_FILE="${ENV_FILE}" \
    NINJOS_SERVICE_USER="$(id -un)" \
    NINJOS_SERVICE_GROUP="$(id -gn)" \
    "${ROOT}/scripts/install-standalone.sh" "${ARCHIVE}"
}

run_installer >/dev/null
[[ -x "${INSTALL_ROOT}/NinjOSEdge" ]]
[[ -x "${INSTALL_ROOT}/NinjOSDashboard" ]]
[[ -x "${INSTALL_ROOT}/start-runtime.sh" ]]
[[ -x "${INSTALL_ROOT}/ninjos-dashboard-account.sh" ]]
[[ -s "${INSTALL_ROOT}/config/edge-fabric.ini" ]]
[[ -s "${ENV_FILE}" ]]
grep -q '^DASHBOARD_TOKEN=$' "${ENV_FILE}"
grep -q '^DASHBOARD_SETUP_CODE=$' "${ENV_FILE}"
grep -q '^COMPANION_SHARED_SECRET=[0-9a-f]\{64\}$' "${ENV_FILE}"

printf 'keep-config\n' >> "${INSTALL_ROOT}/config/edge-fabric.ini"
mkdir -p "${INSTALL_ROOT}/runtime"
printf 'keep-db\n' > "${INSTALL_ROOT}/runtime/preserved.db"
run_installer >/dev/null
grep -q 'keep-config' "${INSTALL_ROOT}/config/edge-fabric.ini"
grep -q 'keep-db' "${INSTALL_ROOT}/runtime/preserved.db"

cp "${ARCHIVE}.sha256" "${TEMP}/good.sha256"
printf '%064d  %s\n' 0 "$(basename "${ARCHIVE}")" > "${ARCHIVE}.sha256"
if run_installer >/dev/null 2>&1; then
    printf 'installer accepted an invalid checksum\n' >&2
    exit 1
fi
mv "${TEMP}/good.sha256" "${ARCHIVE}.sha256"

printf 'standalone-installer-v7.3.16: PASS\n'
