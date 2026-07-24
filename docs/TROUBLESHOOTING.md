# Troubleshooting

## NetherNet during login with packet 0x34 or 0xD1

Upgrade to v7.3.16 or newer. `CraftingData` (`0x34`) and `VoxelShapes` (`0xD1`)
are large, schema-volatile clientbound login packets. Session Core now identifies
their packet envelope before decoding, records metadata, and forwards the
original bytes unchanged. A lagging inspection schema can no longer parse or
dump these packets on the live login path.

The Packet Inspector action for these records is `lossless_passthrough`. This is
expected and does not mean the packet was dropped.

## Blocks and plugin commands are denied through Full Proxy

Confirm the Minecraft XUID has the intended **network player role**. Dashboard
login roles control access to the web interface; they do not grant Minecraft
permissions. In **Network Players > Network XUID Profiles**, select `operator`
for the player, save, then fully disconnect and rejoin through the proxy. Role
changes are included in the next signed identity grant.

In v7.3.16 and newer, changing the dropdown saves immediately and temporarily
pauses the live player-table refresh. If the selector returns to its previous
value, read the dashboard error notification and verify the signed-in dashboard
account is an owner or administrator.

To trace a command, open Packet Inspector and filter for packet ID `77`
(`CommandRequest`). The Gameplay summary records only the command name, whether
it was handled by Ninj-OS, and whether it was forwarded to the backend. Command
arguments are intentionally omitted because they can contain secrets. Commands
other than `/server`, `/hub`, `/lobby`, `/glist`, `/find`, and `/proxie` must be
reported as forwarded to the backend.

If the player is already an operator and the command was forwarded, use the
Endstone console to confirm the plugin is loaded and inspect the plugin's own
permission node or game-mode requirements.

## Movement works, but break, place, use, or interact disconnects

Upgrade Edge Fabric to v7.3.16 or newer. Earlier Full Proxy builds decoded and
serialized every gameplay packet, which could alter hotfix packet layouts even
when the client and backend both used protocol 1001. The lossless relay in v7.3.16
forwards original decrypted packet bytes unless a reviewed translator explicitly
changes the packet. If a disconnect remains, retain the Session Core error and
the matching Protocol Inspector decode-failure record.

If the player remains connected but the block does not break, open Packet
Inspector and filter for packet ID `144` (`PlayerAuthInput`). In v7.3.16 or newer,
the Gameplay summary shows the block actions seen from the client. No
`start_break`/`continue_break` actions indicates client input permissions or game
mode; present actions indicate the backend is rejecting them, so check adventure
mode, spawn protection, claims, and protection-plugin permissions.

## Configuration or Secret Vault changes do not save

v7.3.0 fixes a lock inversion that could leave a save request waiting indefinitely
while transfer-ticket cleanup attempted to reacquire the configuration lock.
Upgrade the runtime before troubleshooting an older installation further.

After saving, the dashboard should display a shortened configuration revision.
The full revision and timestamp are also written to:

```text
runtime/config-save-status.json
```

Verify the canonical configuration directly:

```bash
grep -n "companion_secret\|capture_mode\|dashboard" config/edge-fabric.ini
cat runtime/config-save-status.json
```

Dashboard-managed secrets appear in the INI file but are redacted from the browser.
Environment-backed values appear as `env:VARIABLE`; the actual value must exist in
the running service environment.

If an environment variable was added through Pterodactyl Startup, systemd, Docker,
or a shell after the service started, restart the service before choosing
environment mode.

## Companion says no secret is configured

Open the Secret Vault and configure either:

```text
Default companion shared secret
```

or the selected backend's companion shared secret. The companion secret is not the
dashboard owner password or browser session token.

## Companion is not reporting TPS or gameplay packets

Transport protection does not depend on the companion, so the gateway may remain
healthy while gameplay metrics are unavailable.

1. Confirm the backend uses companion v3.7.1.
2. Run `/npm status` in the Endstone console.
3. Compare its backend ID and secret fingerprint with the selected backend in the
   dashboard.
4. Run `/npm probe`.
5. Read the exact error and follow the matching section below.
6. After changing `companion.properties`, run `/npm reload` and `/npm probe` again.

Delete old cached plugin copies under `plugins/.local/` before restarting Endstone.

## Companion reports 401 Unauthorized

The dashboard was reached, but the key or backend ID did not match. Compare the
fingerprint from `/npm status` with the dashboard's expected fingerprint. Download
and install a fresh package for the correct backend after any secret rotation.

## Companion connection times out

The Endstone server cannot reach the dashboard TCP port. Confirm:

```properties
dashboard_host=<reachable proxy/dashboard address>
dashboard_port=25571
```

Check Pterodactyl TCP allocations, host firewall rules, cloud firewall rules,
router forwarding, Docker/WSL networking, and whether the dashboard listens on the
configured address.

Useful Linux checks:

```bash
ss -lntp | grep 25571
nc -vz DASHBOARD_HOST 25571
```

## Companion receives connection refused

The host responded but no service is listening on the selected TCP port. Verify the
dashboard process is running and that the companion is not pointed at a Bedrock UDP
allocation.

## Reset dashboard access

Set `DASHBOARD_RECOVERY_TOKEN` to a private value containing at least 16 characters,
restart, then sign in with:

```text
Username: recovery
Password: the recovery token value
```

Open **Team & Access > Owner Account**, choose a replacement username
and password, save, clear `DASHBOARD_RECOVERY_TOKEN`, and restart again.

Standalone and Docker installations use the same recovery flow through their
environment file. Shell-access installations may reset first-run setup entirely:

```bash
./ninjos-dashboard-account.sh reset-setup
```

## Dashboard port opens but the page does not load

Confirm that the dashboard allocation is TCP. Bedrock routes and transfer ports are
UDP. The example configuration intentionally uses `25571` for both a secondary UDP
route and the dashboard TCP listener; protocol-specific firewall rules must allow
the correct transport.

## Backend changes do not appear live

Check that the save returned a new configuration revision. Then inspect:

```text
runtime/config-save-status.json
runtime/topology-restart.pending
runtime/events.jsonl
```

The gateway reload normally keeps the dashboard online. Changes to dashboard,
companion, transfer, logging, or Discord service settings may schedule a full
service restart.

## GitHub companion workflow is missing

The workflow must be located exactly at:

```text
.github/workflows/build-companion.yml
```

Upload the extracted repository contents, not the outer folder or unextracted ZIP.

## GLIBC error when loading companion

Run the supplied GitHub workflow. It builds on Ubuntu 22.04 and verifies a glibc
2.35-or-older compatibility target.
