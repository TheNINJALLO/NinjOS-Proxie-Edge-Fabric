#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="7.3.9"
COMPANION_VERSION="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["companionVersion"])' "${ROOT}/companion/release-metadata.json")"
COMPANION_ARCHIVE="NinjOS-Endstone-Companion-v${COMPANION_VERSION}-GitHub-Clean.zip"
VANILLA_BRIDGE_ARCHIVE="NinjOS-Vanilla-Bridge-v${VERSION}.mcpack"
OUT="${1:-${ROOT}/dist}"
STAGE="$(mktemp -d)"
trap 'rm -rf "${STAGE}"' EXIT
rm -rf "${OUT}"
mkdir -p "${OUT}" "${STAGE}/runtime"

python3 "${ROOT}/companion/scripts/generate-documentation.py" --root-copy docs/COMPANION.md
python3 "${ROOT}/scripts/package-companion-source.py"

if [[ "${NINJOS_SKIP_VERIFY:-0}" != "1" ]]; then
    "${ROOT}/scripts/build.sh"
    "${ROOT}/scripts/test.sh"
fi

# Complete offline runtime. Persistent config/ and runtime/ directories are intentionally excluded.
cp "${ROOT}/prebuilt/linux-x86_64/NinjOSEdge" "${STAGE}/runtime/"
cp "${ROOT}/prebuilt/linux-x86_64/NinjOSDashboard" "${STAGE}/runtime/"
cp "${ROOT}/start-runtime.sh" "${STAGE}/runtime/"
cp "${ROOT}/ninjos-dashboard-account.sh" "${STAGE}/runtime/"
cp "${ROOT}/config/edge-fabric.example.ini" "${STAGE}/runtime/"
cp "${ROOT}/manifest.json" "${STAGE}/runtime/"
cp "${ROOT}/README.md" "${ROOT}/INSTALLATION.md" "${ROOT}/CHANGELOG.md" \
   "${ROOT}/LICENSE" "${ROOT}/NOTICE.md" "${STAGE}/runtime/"
cp -a "${ROOT}/docs" "${STAGE}/runtime/docs"
mkdir -p "${STAGE}/runtime/session-core"
rsync -a \
  --exclude='/test/' \
  --exclude='/node_modules/typescript/' \
  --exclude='/node_modules/raknet-node/' \
  --include='/node_modules/minecraft-data/minecraft-data/data/bedrock/common/***' \
  --include='/node_modules/minecraft-data/minecraft-data/data/bedrock/latest/***' \
  --include='/node_modules/minecraft-data/minecraft-data/data/bedrock/1.26.30/***' \
  --include='/node_modules/minecraft-data/minecraft-data/data/bedrock/1.26.10/***' \
  --include='/node_modules/minecraft-data/minecraft-data/data/bedrock/1.21.111/***' \
  --include='/node_modules/minecraft-data/minecraft-data/data/bedrock/1.21.60/***' \
  --include='/node_modules/minecraft-data/minecraft-data/data/bedrock/1.21.80/***' \
  --include='/node_modules/minecraft-data/minecraft-data/data/bedrock/1.19.10/***' \
  --include='/node_modules/minecraft-data/minecraft-data/data/bedrock/1.17.0/***' \
  --include='/node_modules/minecraft-data/minecraft-data/data/bedrock/1.19.1/***' \
  --include='/node_modules/minecraft-data/minecraft-data/data/bedrock/1.16.201/***' \
  --include='/node_modules/minecraft-data/minecraft-data/data/bedrock/1.21.70/***' \
  --exclude='/node_modules/minecraft-data/minecraft-data/data/bedrock/*' \
  --include='/node_modules/minecraft-data/minecraft-data/data/pc/common/***' \
  --include='/node_modules/minecraft-data/minecraft-data/data/pc/1.17/***' \
  --exclude='/node_modules/minecraft-data/minecraft-data/data/pc/*' \
  "${ROOT}/session-core/" "${STAGE}/runtime/session-core/"

# Ship the reviewed native RakNet prebuild and the Bedrock protocol data used by v7.3.9.
SESSION_STAGE="${STAGE}/runtime/session-core"
find "${SESSION_STAGE}/node_modules" -type d \
  \( -name test -o -name tests -o -name docs -o -name doc -o -name examples -o -name example \
     -o -name benchmark -o -name benchmarks \) -prune -exec rm -rf {} + || true
find "${SESSION_STAGE}/node_modules" -type f \
  \( -name '*.md' -o -name '*.map' -o -name '*.ts' \) -delete || true

# Refuse to package a trimmed runtime unless the exact protocol data and relay implementation load.
(
  cd "${SESSION_STAGE}"
  node - <<'NODE'
const data = require('minecraft-data')('bedrock_1.26.30')
if (!data || data.version.minecraftVersion !== '1.26.30' || !data.protocol || !data.blocks || !data.items) {
  throw new Error('Trimmed Bedrock 1.26.30 runtime data is incomplete')
}
const { NinjOSRelay } = require('./src/ninjos-relay')
if (typeof NinjOSRelay !== 'function') throw new Error('NinjOSRelay did not load')
NODE
)
mkdir -p "${STAGE}/runtime/bridges" "${STAGE}/runtime/agents/linux-x86_64" "${STAGE}/runtime/agents/windows-x86_64"
cp "${ROOT}/bridges/vanilla-addon/${VANILLA_BRIDGE_ARCHIVE}" "${STAGE}/runtime/bridges/"
cp -a "${ROOT}/bridges/vanilla-addon/README.md" "${ROOT}/bridges/vanilla-addon/docs" "${STAGE}/runtime/bridges/"
cp "${ROOT}/prebuilt/agents/linux-x86_64/ninjos-vanilla-agent" "${STAGE}/runtime/agents/linux-x86_64/"
cp "${ROOT}/prebuilt/agents/windows-x86_64/ninjos-vanilla-agent.exe" "${STAGE}/runtime/agents/windows-x86_64/"
cp "${ROOT}/vanilla-agent/examples/agent.json" "${ROOT}/vanilla-agent/README.md" "${STAGE}/runtime/agents/"
cp "${ROOT}/${COMPANION_ARCHIVE}" "${STAGE}/runtime/"
cp "${ROOT}/docs/COMPANION.md" "${STAGE}/runtime/ENDSTONE-COMPANION-HOWTO.md"
printf 'version=%s\nbuilt_at=%s\n' "${VERSION}" "$(date -u +%FT%TZ)" > "${STAGE}/runtime/NINJOS_RUNTIME_VERSION.txt"
chmod +x "${STAGE}/runtime/NinjOSEdge" "${STAGE}/runtime/NinjOSDashboard" \
    "${STAGE}/runtime/start-runtime.sh" "${STAGE}/runtime/ninjos-dashboard-account.sh" \
    "${STAGE}/runtime/agents/linux-x86_64/ninjos-vanilla-agent"

RUNTIME="NinjOS-Proxie-Edge-Fabric-v${VERSION}-Runtime.tar.gz"
tar -C "${STAGE}/runtime" -czf "${OUT}/${RUNTIME}" .
sha256sum "${OUT}/${RUNTIME}" | awk '{print $1 "  " $2}' | sed 's# .*/#  #' > "${OUT}/${RUNTIME}.sha256"

cp "${ROOT}/dist-assets/egg-ninjos-proxie-edge-fabric-v${VERSION}.json" "${OUT}/"
cp "${ROOT}/bridges/vanilla-addon/${VANILLA_BRIDGE_ARCHIVE}" "${OUT}/"
cp "${ROOT}/README.md" "${ROOT}/INSTALLATION.md" "${ROOT}/CHANGELOG.md" \
   "${ROOT}/CONTRIBUTING.md" "${ROOT}/SECURITY.md" "${ROOT}/SUPPORT.md" \
   "${ROOT}/LICENSE" "${ROOT}/NOTICE.md" "${OUT}/"
mkdir -p "${OUT}/docs" "${OUT}/deploy" "${OUT}/agents/linux-x86_64" "${OUT}/agents/windows-x86_64" "${OUT}/bridges"
cp -a "${ROOT}/docs/." "${OUT}/docs/"
cp -a "${ROOT}/deploy/." "${OUT}/deploy/"
cp "${ROOT}/scripts/install-standalone.sh" "${OUT}/install-standalone.sh"
cp "${ROOT}/install-windows.ps1" "${OUT}/install-windows.ps1"
cp "${ROOT}/manage-windows.ps1" "${OUT}/manage-windows.ps1"
cp "${ROOT}/uninstall-windows.ps1" "${OUT}/uninstall-windows.ps1"
cp "${ROOT}/ninjos-dashboard-account.sh" "${OUT}/ninjos-dashboard-account.sh"
cp "${ROOT}/${COMPANION_ARCHIVE}" "${OUT}/"
cp "${ROOT}/docs/COMPANION.md" "${OUT}/ENDSTONE-COMPANION-HOWTO.md"
cp "${ROOT}/bridges/vanilla-addon/${VANILLA_BRIDGE_ARCHIVE}" "${OUT}/bridges/"
cp "${ROOT}/prebuilt/agents/linux-x86_64/ninjos-vanilla-agent" "${OUT}/agents/linux-x86_64/"
cp "${ROOT}/prebuilt/agents/windows-x86_64/ninjos-vanilla-agent.exe" "${OUT}/agents/windows-x86_64/"
cp "${ROOT}/vanilla-agent/examples/agent.json" "${OUT}/agents/"
chmod +x "${OUT}/install-standalone.sh" "${OUT}/ninjos-dashboard-account.sh" "${OUT}/agents/linux-x86_64/ninjos-vanilla-agent"

# Separate install-ready bridge and agent packages.
python3 - "${ROOT}" "${OUT}" "${VERSION}" <<'COMPONENTS'
from pathlib import Path
import sys, zipfile
root = Path(sys.argv[1]); out = Path(sys.argv[2]); version = sys.argv[3]
packages = [
    (out/f'NinjOS-Vanilla-Agent-Linux-v{version}.zip', [
        (root/'prebuilt/agents/linux-x86_64/ninjos-vanilla-agent', 'ninjos-vanilla-agent'),
        (root/'vanilla-agent/examples/agent.json', 'agent.example.json'),
        (root/'vanilla-agent/deploy/systemd/ninjos-vanilla-agent.service', 'ninjos-vanilla-agent.service'),
        (root/'docs/VANILLA_AGENT_INSTALL.md', 'INSTALL.md'),
    ]),
    (out/f'NinjOS-Vanilla-Agent-Windows-v{version}.zip', [
        (root/'prebuilt/agents/windows-x86_64/ninjos-vanilla-agent.exe', 'ninjos-vanilla-agent.exe'),
        (root/'vanilla-agent/examples/agent.json', 'agent.example.json'),
        (root/'vanilla-agent/deploy/windows/install-agent.ps1', 'install-windows-agent.ps1'),
        (root/'docs/VANILLA_AGENT_INSTALL.md', 'INSTALL.md'),
    ]),
]
for destination, files in packages:
    with zipfile.ZipFile(destination,'w',zipfile.ZIP_DEFLATED) as z:
        for source, name in files:
            z.write(source,name)
COMPONENTS

(
    cd "${OUT}"
    find . -maxdepth 3 -type f ! -name '*SHA256.txt' -print0 | sort -z | xargs -0 sha256sum > "NinjOS-Proxie-Edge-Fabric-v${VERSION}-SHA256.txt"
)

python3 - "${ROOT}" "${OUT}" "${VERSION}" <<'ZIPBUILD'
from pathlib import Path
import subprocess, sys, zipfile
root = Path(sys.argv[1]); out = Path(sys.argv[2]); version = sys.argv[3]
excluded_names = {f'NinjOS-Proxie-Edge-Fabric-v{version}-Runtime.tar.gz'}

def repository_files():
    tracked = subprocess.check_output(
        ['git', '-C', str(root), 'ls-files', '-z'],
    ).decode('utf-8').split('\0')
    for relative in sorted(filter(None, tracked)):
        rel = Path(relative)
        path = root / rel
        if not path.is_file() or path.name in excluded_names:
            continue
        yield path, rel

source_zip = out / f'NinjOS-Proxie-Edge-Fabric-v{version}-Source.zip'
github_zip = out / f'NinjOS-Proxie-Edge-Fabric-v{version}-GitHub-Repository.zip'
for destination in (source_zip, github_zip):
    with zipfile.ZipFile(destination, 'w', zipfile.ZIP_STORED) as archive:
        for path, rel in repository_files(): archive.write(path, rel.as_posix())

deploy_zip = out / f'NinjOS-Proxie-Edge-Fabric-v{version}-Deployment.zip'
with zipfile.ZipFile(deploy_zip, 'w', zipfile.ZIP_STORED) as archive:
    for path in sorted(out.rglob('*')):
        if not path.is_file() or path in {source_zip, github_zip, deploy_zip}: continue
        archive.write(path, path.relative_to(out).as_posix())
ZIPBUILD

# Final release checksum index. The deployment bundle already contains its component checksum index;
# this outer index also covers the source, GitHub, and deployment archives themselves.
(
    cd "${OUT}"
    find . -type f ! -name "NinjOS-Proxie-Edge-Fabric-v${VERSION}-SHA256.txt" -print0 \
      | sort -z | xargs -0 sha256sum > "NinjOS-Proxie-Edge-Fabric-v${VERSION}-SHA256.txt"
)

readonly CREDIT_PATTERN='ProxyPass by SculkCatalystMC'
for required in "${ROOT}/README.md" "${ROOT}/NOTICE.md" \
                "${ROOT}/docs/ACKNOWLEDGEMENTS.md" "${ROOT}/start-runtime.sh"; do
    grep -Fq "${CREDIT_PATTERN}" "${required}" || { printf '[package] Required upstream acknowledgement missing from %s.\n' "${required}" >&2; exit 1; }
done

printf '[package] Created universal runtime, deployment, source, GitHub repository, bridge, and host-agent archives in %s\n' "${OUT}"
