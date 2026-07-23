# Changelog

## 7.3.10 - 2026-07-23

### Full Proxy player roles and plugin commands

- Added an owner/admin role editor to Network XUID Profiles so Minecraft roles
  can be managed without calling the control-plane API manually.
- Kept dashboard account authorization separate from Minecraft player roles and
  documented the required disconnect/rejoin after changing a network role.
- Rejected unknown role values at the API boundary instead of persisting an
  unusable permission state.
- Added privacy-safe `CommandRequest` inspection showing the command name and
  whether Ninj-OS intercepted it or forwarded it unchanged to Endstone.
- Added integration coverage for valid and invalid network-role updates and
  Session Core coverage for command diagnostics that never retain arguments.

## 7.3.9 - 2026-07-23

### Mojang packet catalog and block-action diagnostics

- Generated a 229-entry packet ID/name catalog from Mojang's official Bedrock
  protocol documentation at a recorded source commit.
- Added catalog source, Minecraft version, protocol version, and packet count to
  Packet Inspector responses and the dashboard summary.
- Replaced generic unknown labels with Mojang names when a numeric ID is known.
- Added safe `PlayerAuthInput` summaries for input flags, block-action counts,
  block-action names, item interactions, stack requests, and client ticks.
- Documented how to distinguish missing client block actions from backend game
  mode, spawn-protection, or permission-plugin rejection.

## 7.3.8 - 2026-07-23

### Lossless Full Proxy gameplay forwarding

- Forwarded native gameplay packets using their original decrypted bytes instead
  of decoding and serializing them through an older hotfix schema.
- Continued to terminate and re-establish encryption on each side of Full Proxy,
  preserving its routing, identity, inspection, and protection features.
- Re-encoded packets only when an explicit reviewed Protocol Weave translation
  actually changes their decoded fields.
- Preserved proxy command interception, client-cache handling, packet inspection,
  and pre-join packet queues on the hybrid raw/decoded relay path.
- Added dependency-patch regression checks for bidirectional raw forwarding.

## 7.3.7 - 2026-07-22

### Reliable Endstone identity and permission restoration

- Retried the transient Full Proxy join race where Endstone became ready before
  Session Core's signed identity grant reached the dashboard.
- Preserved operator status already configured on the Endstone backend when a
  network member grant does not request an elevation.
- Continued to apply network-role elevation, permission attachments, permission
  recalculation, and command refresh after a verified identity handoff.
- Added release regression checks for grant retry and non-destructive OP handling.

## 7.3.6 - 2026-07-22

### Data-plane restart and port ownership

- Reserved enabled Full Proxy public ports from the temporary transfer pool so
  the transparent gateway and Session Core cannot bind the same UDP allocation.
- Restarted Session Core together with the gateway after backend, mode, port, or
  service configuration changes instead of leaving stale listeners running.
- Added post-bind health checks and a truthful `Runtime ready` marker for
  Pterodactyl, while retaining the legacy marker for already imported eggs.
- Made listener startup fail clearly when a process cannot bind or publish health
  rather than leaving the panel indefinitely in a starting state.
- Added regression coverage for transfer-pool reservations and Session Core PID
  replacement during dashboard-managed topology restarts.

## 7.3.5 - 2026-07-22

### Zoo Full Proxy recovery

- Fixed backend protocol compatibility reporting so the dashboard uses the
  listener's configured codec protocol before any player connects.
- Resolved relative protocol-pack paths from the installation root and added a
  reviewed built-in protocol `1001` baseline for partial or in-place upgrades.
- Isolated packet inspection errors from the transport path so observation and
  decoding diagnostics cannot disconnect an otherwise valid player session.
- Added regression coverage for startup from a non-install working directory and
  for operation when the external protocol-pack directory is unavailable.

## 7.3.4 - 2026-07-22

### Unified credential management

- Made the Secret Vault the single dashboard editor for service keys, tokens,
  TOTP, Discord credentials, and per-backend companion secrets.
- Enforced the same 12-character minimum and type-specific validation for direct
  values and populated environment references.
- Prevented backend and advanced INI saves from overwriting Vault-managed values,
  and added the Session Core internal token to the Vault inventory.

### Protocol Weave foundation

- Added reviewed, data-driven Bedrock protocol packs with fail-closed protocol
  selection and limited declarative packet translators.
- Added tiered protocol inspection with metadata, redacted decoded objects,
  selected post-decryption wire samples, decode-failure evidence, and bounded
  decode/re-encode comparisons.
- Rebuilt the dashboard packet details view around decoded fields, translated
  values, hexadecimal/ASCII wire data, round-trip results, and capture tiers.

## 7.3.3 - 2026-07-21

### Bedrock 1.26.33 Full Proxy and health reporting

- Added Microsoft OIDC login verification for Bedrock `1.26.33` while retaining
  its wire-compatible `1.26.30` packet schema (protocol `1001`).
- Corrected the `voxel_shapes` schema used by current Endstone servers and made
  the native RakNet prebuild portable across supported Linux kernel versions.
- Added a reproducible compatibility step so clean CI, npm, and release builds
  contain the same fixes that were validated on the live Zoo route.
- Added Session Core health publication and merged Full Proxy listeners into the
  dashboard's backend registry, health API, metrics, and automatic actions.
- Fixed recovered companions being shown as degraded because of historical,
  cumulative upload failures.
- Separated the transparent gateway's live topology from Full Proxy routes so both
  runtimes cannot bind the same public UDP port.
- Rebuilt the pinned Session Core dependency lock and release runtime assets.
- Fixed container publishing so it builds current binaries and the versioned
  Vanilla Bridge package before constructing the image.
- Updated installers, packages, documentation, and release automation for `v7.3.3`.

## 7.3.1 - 2026-07-19

### Dashboard access and interface update

- Added owner-managed admin, operator, and viewer accounts with password reset,
  immediate session revocation, temporary disablement, deletion, and audit events.
- Made dashboard navigation role-aware so each account sees the tools it can use.
- Consolidated backend configuration and live health into one registry and removed
  duplicate routing and backend displays.
- Moved owner credentials into a dedicated Team & Access area and clearly labeled
  the raw INI editor as Advanced Configuration.

## 7.3.0 - 2026-07-19

- Added dual connection modes: Transparent Auth Mode for `online-mode=true` and Full Proxy Mode for `online-mode=false`.
- Added the protocol-aware Bedrock Session Core with Xbox-authenticated upstream sessions, backend relays, proxy commands, fallback routing, and one-use identity grants.
- Added Endstone identity and permission restoration, including operator state, permission attachments, and command-list refresh.
- Added the Ninj-OS Vanilla Bridge behavior pack for ordinary Mojang BDS backends.
- Added the cross-platform Vanilla Host Agent for operator synchronization and process telemetry on Linux and Windows.
- Added backend adapter selection, capacity, fallback, identity enforcement, and online-mode validation to the dashboard and unified configuration.
- Added complete Pterodactyl, Linux, Windows/WSL2, Docker, Endstone, vanilla, transparent-mode, full-proxy, and migration documentation.


## 7.2.3 - 2026-07-18

### Companion documentation and release packaging

- Added a complete Endstone companion operator guide covering requirements,
  per-backend secrets, GitHub Actions and local builds, Pterodactyl and standalone
  installation, multi-server layouts, firewall rules, configuration fields,
  probes, upgrades, secret rotation, removal, and troubleshooting.
- Added a versioned documentation template and generator inside the companion
  source repository.
- Regenerate the root companion guide and companion GitHub source archive during
  every Edge Fabric build.
- Regenerate the companion guide during every companion GitHub Actions build and
  include `COMPANION-HOWTO.md` in each compiled install package.
- Added release validation for the generated guide, source archive contents, and
  companion workflow packaging.

### Splash correction

- Replaced the console ASCII banner that visually rendered `NINI-OS` with a clear
  `NINJ-OS` banner.
- Updated the splash documentation and regression test so the former banner
  cannot return unnoticed.

## 7.2.2 - 2026-07-18

### Multi-server Endstone performance

- Replaced the single-server Endstone Performance block with a fleet dashboard that renders one card for every configured backend.
- Added network-wide TPS, MSPT, player, queue, and upload-failure summaries across all reporting companions.
- Configured servers remain visible before their first report and are marked as never reported, stale, offline, degraded, or connected.
- Added per-server TPS, MSPT, players, BDS CPU, memory, queue, upload failures, companion version, Minecraft version, protocol version, gateway health, session count, secret readiness, and report age.
- Expanded the Companion Fleet table to include configured servers that have not reported yet.
- Added a regression test covering multiple configured backends with mixed connected and never-reported companion states.

## 7.2.1 - 2026-07-18

### Fixed

- Fixed a configuration save deadlock caused by transfer-ticket cleanup attempting to reacquire the configuration file lock.
- Made managed settings and Secret Vault writes transactional, with disk read-back verification and a persistent configuration revision record.
- Rejected environment-backed secret selections when the chosen variable is empty in the running service.
- Added visible configuration revision confirmation to the dashboard after every settings, secret, and unified-config save.
- Normalized companion server IDs before secret lookup and ingest identity comparison.
- Added per-backend companion connection state, last-report age, TPS, MSPT, and effective secret fingerprints to the dashboard.
- Clarified that companion shared secrets are separate from dashboard usernames, passwords, and browser session tokens.
- Added detailed companion connection failures, recovery notices, `/npm probe`, and expanded `/npm status` output.
- Corrected the companion fallback dashboard port from 25570 to 25571.

### Upgrade note

Install the v7.2.1 runtime and replace the backend companion with v3.5.1. Download a fresh install package for each backend after confirming its expected secret fingerprint.

## 7.2.0 - 2026-07-18

- Added the complete Windows 11 installation path using WSL2, mirrored networking,
  systemd, Windows Defender Firewall, Hyper-V firewall rules, and a per-user
  startup task.
- Added `install-windows.ps1`, `manage-windows.ps1`, and
  `uninstall-windows.ps1`.
- Added dedicated Pterodactyl, Linux, Windows, Docker, and GitHub release guides.
- Added a repository installation index and reorganized the root README around
  supported deployment platforms.
- Added GitHub Actions for Linux builds, integration tests, CodeQL analysis, and
  tagged release packaging.
- Added issue forms, a pull-request template, CODEOWNERS, Dependabot configuration,
  contribution guidance, security policy, and support guidance.
- Added a GitHub-ready repository ZIP alongside the source and deployment
  packages.
- Retained the browser-based first-run owner setup, password authentication,
  single-use setup code, account recovery utility, and v7.0.x upgrade
  compatibility introduced in v7.1.0.
- Removed obsolete tool-oriented build instructions from the companion
  documentation.

## 7.0.2 - 2026-07-18

- Added a complete standalone Linux installation path for VPS, dedicated-server,
  home-server, and virtual-machine deployments without Pterodactyl.
- Added a checksum-aware `install-standalone.sh` installer that preserves live
  configuration and runtime data, creates a restricted service account, generates
  initial secrets, and installs a managed systemd service.
- Added documented manual and third-party process-supervisor startup methods.
- Added Linux Docker and Docker Compose deployment files with persistent named
  volumes and host networking for Bedrock UDP routes and transfer pools.
- Added UFW, firewalld, NAT, same-host backend, cloud firewall, and socket
  verification guidance.
- Reorganized Quick Start and README installation links so Pterodactyl is one
  supported deployment method rather than a product requirement.

## 7.0.1 - 2026-07-18

- Added clear development credit for ProxyPass by SculkCatalystMC throughout the
  console splash, dashboard metadata, README, notice, and provenance documents.
- Clarified that ProxyPass was used as a reference and inspiration while the
  shipped gateway, dashboard, deployment flow, and companion integration are
  maintained as the Ninj-OS implementation.
- Replaced the zero-reference packaging guard with an acknowledgement validation
  that prevents the upstream credit from being accidentally removed.
- Cleaned the source archive by excluding Python cache files and compiled bytecode.
- Included the complete documentation set in the deployment ZIP.

## 7.0.0 - 2026-07-18

- Established the Ninj-OS Edge Datagram Engine as the release's protocol-agnostic
  gateway core.
- Replaced legacy runtime-facing branding while retaining the Ninj-OS product
  identity.
- Centralized product identity and version metadata for the C++ gateway and Go
  dashboard.
- Added implementation provenance and Ninj-OS release notices.
- Rebuilt the Pterodactyl bootstrap around staged extraction and required-file
  validation.
- Added optional SHA-256 verification through a sidecar file or Startup variable.
- Added archive auto-discovery while retaining an explicit archive override.
- Preserved `config/`, `runtime/`, and legacy route migration during reinstall.
- Added reproducible release packaging and distribution validation.
- Retained the dashboard login input persistence fix, backend save verification,
  secret vault, companion builder, transfer pool, and gateway-only topology reloads.

## 6.7.x compatibility

- Existing unified INI files, SQLite data, companion secrets, route migration,
  and backend definitions remain compatible with v7.3.0.
