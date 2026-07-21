#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUTPUT="${ROOT}/prebuilt/linux-x86_64"
AGENT_OUTPUT="${ROOT}/prebuilt/agents"
mkdir -p "${OUTPUT}" "${AGENT_OUTPUT}/linux-x86_64" "${AGENT_OUTPUT}/windows-x86_64"

printf '[build] companion documentation and source package\n'
python3 "${ROOT}/companion/scripts/generate-documentation.py" --root-copy docs/COMPANION.md
python3 "${ROOT}/scripts/package-companion-source.py"

printf '[build] vanilla bridge package\n'
python3 - "${ROOT}" <<'PY'
from pathlib import Path
import sys, zipfile
root=Path(sys.argv[1])
pack=root/'bridges/vanilla-addon/NinjOS-Vanilla-Bridge-BP'
out=root/'bridges/vanilla-addon/NinjOS-Vanilla-Bridge-v7.3.2.mcpack'
with zipfile.ZipFile(out,'w',zipfile.ZIP_DEFLATED) as z:
    for p in sorted(pack.rglob('*')):
        if p.is_file(): z.write(p,p.relative_to(pack).as_posix())
print(f'[build] created {out}')
PY

printf '[build] Bedrock Session Core\n'
(
    cd "${ROOT}/session-core"
    if [[ ! -d node_modules/bedrock-protocol ]]; then
        npm ci --omit=dev
    fi
    npm run check
    npm test
)

printf '[build] vanilla host agents\n'
(
    cd "${ROOT}/vanilla-agent"
    gofmt -w ./cmd/ninjos-vanilla-agent/*.go
    go test ./...
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o "${AGENT_OUTPUT}/linux-x86_64/ninjos-vanilla-agent" ./cmd/ninjos-vanilla-agent
    CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o "${AGENT_OUTPUT}/windows-x86_64/ninjos-vanilla-agent.exe" ./cmd/ninjos-vanilla-agent
)

printf '[build] dashboard\n'
(
    cd "${ROOT}/dashboard"
    CGO_ENABLED=1 go build \
        -trimpath \
        -ldflags='-s -w -linkmode external -extldflags "-static"' \
        -o "${OUTPUT}/NinjOSDashboard" .
)

printf '[build] gateway\n'
cmake -S "${ROOT}" -B "${ROOT}/build" -DCMAKE_BUILD_TYPE=Release -DNINJOS_STATIC_LINK=ON
cmake --build "${ROOT}/build" --parallel 2
cp "${ROOT}/build/NinjOSEdge" "${OUTPUT}/NinjOSEdge"
chmod +x "${OUTPUT}/NinjOSDashboard" "${OUTPUT}/NinjOSEdge" \
    "${AGENT_OUTPUT}/linux-x86_64/ninjos-vanilla-agent"

file "${OUTPUT}/NinjOSDashboard"
file "${OUTPUT}/NinjOSEdge"
file "${AGENT_OUTPUT}/linux-x86_64/ninjos-vanilla-agent"
file "${AGENT_OUTPUT}/windows-x86_64/ninjos-vanilla-agent.exe"
