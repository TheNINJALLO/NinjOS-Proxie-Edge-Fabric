# Ninj-OS Proxie Endstone Companion: Complete Setup Guide

This guide covers installation, configuration, building, verification, upgrades,
and troubleshooting for the Ninj-OS Proxie Endstone Companion.

```text
Edge Fabric release: 7.3.14
Companion release: 3.7.1
Default Endstone build target: 0.11.6
Default dashboard and companion API port: 25571/TCP
Plugin platform: Linux x86_64
```

## 1. What the companion does

Ninj-OS Proxie protects Bedrock transport without the companion. The Endstone
companion adds the backend information that a transport relay cannot observe by
itself:

- Current and average TPS
- Current and average MSPT
- Tick usage
- Online and maximum players
- Player-presence events
- Packet names, directions, and selected payload metadata
- Backend CPU, memory, and network counters
- Upload queue health and failure totals
- Transfer-ticket requests and arrival confirmation

Every Endstone backend uses its own `server_id` and companion shared secret. A
network with four Endstone servers should normally have four companion
installations and four unique secrets.

## 2. Requirements

Before starting, collect the following information for each backend:

```text
Backend ID from the Edge Fabric dashboard
Backend Minecraft/BDS version
Backend Endstone version
Backend operating system and CPU architecture
Dashboard host or IP reachable from the backend
Dashboard TCP port
```

The supplied build workflow creates a Linux x86_64 `.so`. It is intended for
Endstone servers running on Linux, including Pterodactyl containers. Compile the
plugin against the exact Endstone release used by the backend.

The dashboard must be reachable from the Endstone server over TCP. The default
port is:

```text
25571/TCP
```

This is separate from Bedrock UDP route ports.

## 3. Add and verify the backend in Edge Fabric

1. Sign in to the Ninj-OS Proxie dashboard.
2. Open **Backends**.
3. Create or edit the backend.
4. Give it a short, unique ID such as `kingdom`, `zoo`, or `creative`.
5. Enter the real BDS host and port.
6. Save the backend.
7. Confirm the dashboard reports a saved configuration revision.
8. Use the backend test action to confirm the gateway can reach BDS.

The backend ID becomes the companion `server_id`. IDs are normalized to lowercase,
but using the exact dashboard spelling keeps audits and troubleshooting simple.

## 4. Create the companion shared secret

The companion does not use any of these credentials:

- Dashboard owner username
- Dashboard owner password
- Browser session
- Operator token
- Viewer token
- Metrics token

Use a separate companion secret for each backend.

### Recommended: dashboard-managed secret

1. Open **Configuration & Secrets**.
2. Open **Secret Vault**.
3. Select the backend's **Companion shared secret**.
4. Choose **Dashboard-managed**.
5. Generate a new value or enter a random secret of at least 32 characters.
6. Save the secret.
7. Confirm the dashboard shows a successful save and configuration revision.
8. Open **Companion Builder & Downloads**.
9. Select the backend.
10. Record the displayed 12-character secret fingerprint.

### Environment-variable secret

Environment mode is useful when a hosting panel or service manager owns the
secret. The selected variable must exist in the running proxy environment.

Example:

```text
COMPANION_KINGDOM_SECRET
```

Set the value in Pterodactyl Startup variables, a systemd environment file, Docker
Compose, or the shell that launches the proxy. Restart the proxy after changing
the environment. Edge Fabric rejects an empty environment reference instead of
pretending it is configured.

## 5. Choose the correct dashboard address

The companion connects from the Endstone backend to the Edge Fabric dashboard.
Use an address that is reachable from that backend.

### Proxy and Endstone on the same Linux host

Use the host address that the Endstone container can reach. `127.0.0.1` works only
when both processes share the same network namespace.

Common choices include:

```properties
dashboard_host=127.0.0.1
dashboard_port=25571
```

or the host's private LAN address:

```properties
dashboard_host=10.0.0.25
dashboard_port=25571
```

### Separate Pterodactyl servers on one node

Pterodactyl containers normally do not share loopback. Use the node's reachable
private or public address and make sure the dashboard TCP allocation is bound.

### Different hosts or hosting providers

Use the proxy host's public IP or DNS name. Permit the dashboard TCP port through:

- The proxy server firewall
- Provider or cloud firewall rules
- Router NAT when the proxy is behind a home router
- Pterodactyl allocations when applicable

Restrict dashboard access to trusted source addresses whenever possible.

## 6. Download the companion source

From **Configuration & Secrets → Companion Builder & Downloads**:

1. Click **Download GitHub Source**.
2. Extract the ZIP.
3. Create a GitHub repository for the companion.
4. Upload every extracted file and folder to the repository root.
5. Confirm this file exists at the exact path:

```text
.github/workflows/build-companion.yml
```

Do not upload only `src/`. The workflow, CMake files, documentation generator,
configuration example, and release metadata are all required.

## 7. Build with GitHub Actions

1. Open the companion repository on GitHub.
2. Select **Actions**.
3. Enable workflows if GitHub prompts for approval.
4. Select **Build Endstone Companion**.
5. Choose **Run workflow**.
6. Enter the exact Endstone release without a leading `v`.

Example:

```text
0.11.6
```

7. Start the workflow.
8. Wait for the Linux x86_64 job to finish.
9. Open the completed workflow run.
10. Download the artifact named:

```text
NinjOS-Endstone-Companion-Endstone-<version>
```

The artifact contains:

```text
NinjOS-Endstone-Companion-Linux-x86_64-Endstone-<version>.zip
NinjOS-Endstone-Companion-Linux-x86_64-Endstone-<version>.zip.sha256
```

Every companion build regenerates this guide from the versioned template and
places `COMPANION-HOWTO.md` in the compiled install package.

## 8. Build locally on Linux

GitHub Actions is the normal build path. For a local Ubuntu 22.04 build:

```bash
sudo apt-get update
sudo apt-get install -y \
  clang-18 \
  cmake \
  git \
  libc++-18-dev \
  libc++abi-18-dev \
  ninja-build \
  python3

python3 scripts/generate-documentation.py

cmake \
  -S . \
  -B build \
  -G Ninja \
  -DCMAKE_BUILD_TYPE=Release \
  -DENDSTONE_API_VERSION=0.11.6 \
  -DCMAKE_C_COMPILER=clang-18 \
  -DCMAKE_CXX_COMPILER=clang++-18 \
  -DCMAKE_CXX_FLAGS="-stdlib=libc++" \
  -DCMAKE_SHARED_LINKER_FLAGS="-stdlib=libc++"

cmake --build build --parallel 2
find build -type f -name '*ninjos_proxie_companion*.so'
```

A plugin compiled for a different Endstone native API or ABI may fail to load.
Keep separate builds when servers use different Endstone versions.

## 9. Upload the compiled plugin to Edge Fabric

1. Return to **Companion Builder & Downloads**.
2. Select **Upload Compiled Artifact**.
3. Upload either:
   - `ninjos_proxie_companion.so`, or
   - the artifact ZIP downloaded from GitHub Actions.
4. Confirm the dashboard accepts the Linux x86_64 ELF library.

The dashboard stores one compiled companion artifact. The same artifact can be
used for multiple backends only when those backends use a compatible Endstone
version, Linux runtime, and x86_64 architecture.

When backend servers use different Endstone versions, compile and retain a
matching artifact for each version. Upload the correct artifact immediately before
generating that backend's install package.

## 10. Generate an install package for each backend

Repeat these steps for every Endstone backend:

1. Select the backend in **Companion Builder & Downloads**.
2. Confirm its secret is shown as configured.
3. Compare the expected fingerprint with the value recorded earlier.
4. Click **Download Install Package**.
5. Save the ZIP with the backend name.

The generated package contains:

```text
plugins/ninjos_proxie_companion.so
plugins/ninjos_proxie_companion/companion.properties
INSTALL.txt
SHA256SUMS.txt
```

The generated properties file includes the selected backend's host, port, secret,
ID, capture settings, queue settings, presence settings, and transfer settings.

Never reuse one backend's generated properties file on another backend.

## 11. Install on a Pterodactyl Endstone server

1. Stop the Endstone server completely.
2. Open the server's Files page or connect by SFTP.
3. Upload the backend-specific install ZIP to the server root.
4. Extract it into the server root.
5. Confirm these paths exist:

```text
plugins/ninjos_proxie_companion.so
plugins/ninjos_proxie_companion/companion.properties
```

6. Delete old generated or cached copies under:

```text
plugins/.local/
```

7. Start the Endstone server.
8. Watch the console for the companion enabled line.
9. Run `/npm status` in the server console.
10. Run `/npm probe`.

If the hosting panel does not allow ZIP extraction, extract the package locally
and upload the two plugin paths manually.

## 12. Install on a standalone Linux Endstone server

1. Stop Endstone using its service manager or console.
2. Extract the backend package in the Endstone server root:

```bash
unzip NinjOS-Companion-kingdom-Install.zip -d /path/to/endstone
```

3. Remove any stale cached copy:

```bash
rm -f /path/to/endstone/plugins/.local/*ninjos_proxie_companion*.so
```

4. Confirm permissions allow the Endstone service account to read the files:

```bash
chown -R endstone:endstone \
  /path/to/endstone/plugins/ninjos_proxie_companion.so \
  /path/to/endstone/plugins/ninjos_proxie_companion
```

5. Start Endstone.
6. Run `/npm status` and `/npm probe`.

Adjust the service account and server path to match the installation.

## 13. Verify the companion from the Endstone console

### Status

Run:

```text
/npm status
```

The status output includes:

- Companion version
- Dashboard host and port
- Backend server ID
- Secret fingerprint
- Player count
- Queue depth
- Dropped record count
- Uploaded record count
- Upload failure count
- Capture mode
- Presence state
- Transfer state
- Last upload attempt
- Last successful upload
- Last error

The secret fingerprint must match the expected fingerprint shown for that backend
in the Edge Fabric dashboard.

### Probe

Run:

```text
/npm probe
```

A successful response resembles:

```text
Companion probe accepted by proxy.example.net:25571 for server kingdom.
```

A successful probe proves that the following agree:

- DNS or IP routing
- Dashboard TCP port
- Backend ID
- Companion shared secret
- Request signing
- Dashboard companion endpoint

### Reload after editing

After editing `companion.properties`, run:

```text
/npm reload
/npm status
/npm probe
```

A full server restart is still recommended after replacing the `.so` file.

## 14. Verify the dashboard fleet view

Open **Overview → Endstone Performance**.

Every configured backend should have its own card. A backend can show:

```text
Connected
Degraded
Stale
Offline
Never reported
```

A connected card should update with:

- TPS and MSPT
- Tick usage
- Online players
- CPU and memory
- Network counters
- Upload queue and failures
- Companion and Minecraft versions
- Last report age

A server that has never reported remains visible. This makes it possible to spot a
missing installation, wrong backend ID, bad secret, or blocked TCP route without
switching the selected backend.

## 15. Configuration reference

The companion reads:

```text
plugins/ninjos_proxie_companion/companion.properties
```

### Connection settings

| Setting | Default | Purpose |
|---|---:|---|
| `dashboard_host` | `185.83.152.144` | Host or IP of the Edge Fabric dashboard |
| `dashboard_port` | `25571` | Dashboard and companion API TCP port |
| `shared_secret` | `CHANGE_ME_NOW` | Secret assigned to this backend |
| `server_id` | `kingdom` | Backend ID from the dashboard |

### Packet capture settings

| Setting | Default | Purpose |
|---|---:|---|
| `capture_mode` | `metadata` | `off`, `metadata`, `selected`, or `all` |
| `selected_packet_ids` | `30,77` | Payload-eligible IDs when mode is `selected` |
| `payload_limit` | `512` | Maximum retained payload bytes per eligible packet |
| `redact_packet_ids` | `1,3,4` | Packet IDs whose payloads are always withheld |
| `movement_sample_rate` | `20` | Retain one movement sample per this many events |
| `drop_receive_ids` | empty | Incoming packet IDs to cancel |
| `drop_send_ids` | empty | Outgoing packet IDs to cancel |

Login and encryption handshake payloads remain redacted. Use payload capture only
when it is necessary for a specific diagnostic task.

### Queue and upload settings

| Setting | Default | Purpose |
|---|---:|---|
| `queue_capacity` | `50000` | Maximum records waiting in memory |
| `batch_size` | `200` | Maximum records per upload request |
| `flush_ms` | `100` | Maximum normal batching delay in milliseconds |
| `reconnect_seconds` | `3` | Delay before retrying after upload failure |
| `metrics_interval_ticks` | `20` | Metrics collection interval in server ticks |

A queue that grows continuously usually indicates a blocked dashboard route,
incorrect secret, or dashboard service problem.

### Presence and transfer settings

| Setting | Default | Purpose |
|---|---:|---|
| `presence_enabled` | `true` | Send player name and XUID presence updates |
| `presence_include_address` | `false` | Include player network addresses |
| `transfer_enabled` | `true` | Enable protected transfer-ticket requests |

Keep `presence_include_address=false` unless staff specifically need network
addresses and the server's privacy policy covers their collection.

## 16. Multi-server deployment rules

For every backend:

- Use a unique backend ID.
- Use a unique companion secret.
- Generate a backend-specific properties file.
- Install one companion on that Endstone server.
- Verify the secret fingerprint.
- Run a probe.
- Confirm the server has its own performance card.

Example fleet:

```text
kingdom  -> secret A -> server_id=kingdom
zoo      -> secret B -> server_id=zoo
creative -> secret C -> server_id=creative
events   -> secret D -> server_id=events
```

Do not point multiple backends at the same `server_id`. Their reports would be
combined under one identity and would overwrite the same latest-state file.

## 17. Firewall and allocation checklist

The Endstone server initiates an outbound TCP connection to the dashboard. Check:

1. The dashboard listens on the configured TCP port.
2. Pterodactyl has a TCP allocation for that port when the panel requires one.
3. The proxy host firewall permits the port.
4. A cloud firewall or security group permits the port.
5. Home-hosted servers forward the port through the router when remote backends
   must connect.
6. The Endstone host permits outbound TCP traffic.
7. The companion does not target a Bedrock UDP route port.

Linux listening-port check:

```bash
ss -lntp | grep :25571
```

Linux firewall example with UFW:

```bash
sudo ufw allow from <ENDSTONE_IP> to any port 25571 proto tcp
```

Test basic TCP reachability from the Endstone host:

```bash
nc -vz proxy.example.net 25571
```

A successful TCP test does not prove the secret is correct. `/npm probe` performs
the signed application-level check.

## 18. Common errors

### Companion is connected but OP or commands are missing

Connection status proves that signed telemetry is accepted; permissions use a
separate one-use identity grant during a Full Proxy join. Install Companion
v3.7.1 or newer, confirm `identity_bridge_enabled=true`, and join through the
backend's Full Proxy public port. The companion briefly retries while Session
Core publishes the grant and preserves OP already configured in Endstone. Look
for an `identity.verified` event rather than relying only on `npm probe`.

### `401 Unauthorized`

The dashboard was reached, but the backend ID or shared secret did not match.

Repair steps:

1. Run `/npm status`.
2. Compare the backend ID and fingerprint with the dashboard.
3. Confirm you selected the correct backend when generating the package.
4. Download a fresh package.
5. Replace `companion.properties`.
6. Run `/npm reload` and `/npm probe`.

### `Dashboard connection timed out`

The companion could not establish the TCP connection before the timeout.

Check:

- Dashboard host or DNS
- Dashboard TCP port
- Pterodactyl allocation
- Linux, Windows, cloud, and router firewalls
- Private-network routing
- Whether the dashboard is running

### `Connection refused`

The host responded, but no service accepted the configured TCP port. Confirm the
dashboard process is listening and that the port is not a Bedrock UDP-only
allocation.

### `Dashboard returned no response`

The companion reached a service that did not return valid HTTP. The configured
port may belong to a Bedrock route, reverse proxy, or unrelated service.

### `Never reported`

The backend exists in Edge Fabric, but no valid companion record has arrived.
Check installation, plugin load logs, backend ID, secret, and `/npm probe`.

### `Stale` or `Offline`

The dashboard received data previously but has not received a recent report. Run
`/npm status` and inspect the last error, queue depth, and last successful upload.

### Queue depth continually rises

The worker cannot upload as quickly as records are produced. Check connectivity
and authentication first. Then reduce capture volume, increase movement sampling,
or use metadata capture instead of all-payload capture.

### Plugin does not load

Check the Endstone console for native-library errors. Common causes include:

- Built for the wrong Endstone release
- Wrong CPU architecture
- Unsupported glibc or C++ runtime
- Upload corruption
- Old cached `.so` still loaded from `plugins/.local/`

Rebuild against the exact Endstone version and compare the supplied checksum.

## 19. Secret rotation

1. Generate or enter a new companion secret for the backend in Secret Vault.
2. Save and confirm the new configuration revision.
3. Download a new backend install package.
4. Stop Endstone or replace only the properties file while it is running.
5. Run `/npm reload`.
6. Run `/npm status` and compare fingerprints.
7. Run `/npm probe`.

The old properties file will immediately receive `401 Unauthorized` after the
proxy begins using the new secret.

## 20. Companion upgrades

When only the Edge Fabric dashboard changes, the existing companion may continue
to work. Replace the companion when the release notes call for a new companion
version or when Endstone itself is upgraded.

Upgrade procedure:

1. Note the backend's current Endstone version.
2. Build the companion against the new exact Endstone version.
3. Upload the new compiled artifact to Edge Fabric.
4. Generate a new install package for the backend.
5. Stop Endstone.
6. Replace the `.so` and properties file.
7. Delete old cached copies under `plugins/.local/`.
8. Start Endstone.
9. Run `/npm status` and `/npm probe`.
10. Confirm the backend performance card updates.

## 21. Backups and removal

Before changing the plugin, back up:

```text
plugins/ninjos_proxie_companion.so
plugins/ninjos_proxie_companion/companion.properties
```

To remove the companion:

1. Stop Endstone.
2. Delete the two paths above.
3. Delete matching cached copies under `plugins/.local/`.
4. Start Endstone.

Transport protection remains active, but gameplay packet data, presence details,
and Endstone performance metrics become unavailable.

## 22. Final verification checklist

For each backend, confirm:

```text
[ ] Backend exists and saves successfully in Edge Fabric
[ ] Unique companion secret is configured
[ ] Correct Endstone version was used for the build
[ ] Compiled artifact was accepted by the dashboard
[ ] Backend-specific install package was generated
[ ] Plugin and properties file are in the correct Endstone paths
[ ] Old plugins/.local cache was removed
[ ] /npm status shows the expected backend ID
[ ] Secret fingerprint matches the dashboard
[ ] /npm probe is accepted
[ ] Endstone Performance shows a separate live card for the backend
[ ] TPS and MSPT update over time
[ ] Queue depth remains stable
[ ] Upload failure count does not continually increase
```

When all items pass, the companion is connected and reporting correctly.
