FROM node:22-bookworm-slim AS session-deps

RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential cmake python3 \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /build/session-core
COPY session-core/package.json session-core/package-lock.json ./
COPY session-core/scripts ./scripts
COPY session-core/protocol-packs ./protocol-packs
RUN npm ci --omit=dev \
    && npm cache clean --force

FROM node:22-bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends bash coreutils mawk ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --system --gid 10001 ninjos \
    && useradd --system --uid 10001 --gid 10001 --home-dir /opt/ninjos --shell /usr/sbin/nologin ninjos

WORKDIR /opt/ninjos

COPY --chown=ninjos:ninjos prebuilt/linux-x86_64/NinjOSEdge ./NinjOSEdge
COPY --chown=ninjos:ninjos prebuilt/linux-x86_64/NinjOSDashboard ./NinjOSDashboard
COPY --chown=ninjos:ninjos start-runtime.sh ninjos-dashboard-account.sh ./
COPY --chown=ninjos:ninjos config/edge-fabric.example.ini ./edge-fabric.example.ini
COPY --chown=ninjos:ninjos manifest.json README.md INSTALLATION.md CHANGELOG.md LICENSE NOTICE.md ./
COPY --chown=ninjos:ninjos docs ./docs
COPY --chown=ninjos:ninjos session-core/package.json session-core/package-lock.json ./session-core/
COPY --chown=ninjos:ninjos session-core/src ./session-core/src
COPY --chown=ninjos:ninjos session-core/scripts ./session-core/scripts
COPY --chown=ninjos:ninjos session-core/protocol-packs ./session-core/protocol-packs
COPY --from=session-deps --chown=ninjos:ninjos /build/session-core/node_modules ./session-core/node_modules
COPY --chown=ninjos:ninjos bridges/vanilla-addon/NinjOS-Vanilla-Bridge-v7.3.15.mcpack ./bridges/
COPY --chown=ninjos:ninjos prebuilt/agents ./agents

RUN chmod 0755 NinjOSEdge NinjOSDashboard start-runtime.sh ninjos-dashboard-account.sh \
    && mkdir -p config runtime \
    && chown -R ninjos:ninjos /opt/ninjos

ENV NINJOS_ROOT_DIR=/opt/ninjos
USER ninjos

VOLUME ["/opt/ninjos/config", "/opt/ninjos/runtime"]
ENTRYPOINT ["/opt/ninjos/start-runtime.sh"]
