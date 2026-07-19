#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

printf '[test] Go control plane\n'
(
    cd "${ROOT}/dashboard"
    gofmt -w ./*.go
    timeout 120s go test ./...
)

printf '[test] Session Core and universal adapters\n'
( cd "${ROOT}/session-core" && npm run check && npm test )
( cd "${ROOT}/vanilla-agent" && go test ./... )
timeout 30s python3 "${ROOT}/tests/integration/test_session_core_startup.py"
python3 -m json.tool "${ROOT}/bridges/vanilla-addon/NinjOS-Vanilla-Bridge-BP/manifest.json" >/dev/null
node --check "${ROOT}/bridges/vanilla-addon/NinjOS-Vanilla-Bridge-BP/scripts/main.js"

printf '[test] Dashboard assets and startup script\n'
node --check "${ROOT}/dashboard/public/app.js"
bash -n "${ROOT}/start-runtime.sh"
bash -n "${ROOT}/ninjos-dashboard-account.sh"

printf '[test] Standalone installer\n'
timeout 60s "${ROOT}/tests/integration/test_standalone_installer.sh"

printf '[test] Dashboard login input persistence\n'
timeout 60s python3 "${ROOT}/tests/integration/test_dashboard_login_input.py"

printf '[test] Startup splash regression\n'
timeout 60s python3 "${ROOT}/tests/integration/test_startup_splash.py"

printf '[test] Generated companion documentation and source package\n'
timeout 60s python3 "${ROOT}/tests/integration/test_companion_release_assets.py"

printf '[test] First-run owner setup and recovery\n'
timeout 120s python3 "${ROOT}/tests/integration/test_dashboard_first_run_setup.py"

printf '[test] Unified configuration compiler\n'
TEMP="$(mktemp -d)"
trap 'rm -rf "${TEMP}"' EXIT
cp "${ROOT}/config/edge-fabric.example.ini" "${TEMP}/edge-fabric.ini"
mkdir -p "${TEMP}/runtime"
DASHBOARD_TOKEN=test-dashboard-token \
COMPANION_SHARED_SECRET=test-companion-secret \
"${ROOT}/prebuilt/linux-x86_64/NinjOSDashboard" \
    --prepare-config "${TEMP}/edge-fabric.ini" "${TEMP}/runtime" "${TEMP}/gateway.conf"

test -s "${TEMP}/gateway.conf"
test -s "${TEMP}/runtime/generated/dashboard.env"
test -s "${TEMP}/runtime/companion-secrets.properties"

printf '[test] Universal proxy modes and signed identity bridge\n'
timeout 120s python3 "${ROOT}/tests/integration/test_universal_proxy_modes.py"

printf '[test] Gateway firewall and incident behavior\n'
timeout 90s python3 "${ROOT}/tests/integration/test_gateway_firewall.py"

printf '[test] Configuration and companion persistence\n'
timeout 120s python3 "${ROOT}/tests/integration/test_config_companion_persistence.py"

printf '[test] Multi-server Endstone performance fleet\n'
timeout 90s python3 "${ROOT}/tests/integration/test_multi_server_endstone_performance.py"

printf '[test] Dashboard control plane\n'
timeout 240s python3 "${ROOT}/tests/integration/test_dashboard_control_plane.py"

printf '[test] Automated backend health actions\n'
timeout 120s python3 "${ROOT}/tests/integration/test_backend_health_actions.py"

printf '[test] Unified backend management\n'
timeout 240s python3 "${ROOT}/tests/integration/test_unified_management.py"
