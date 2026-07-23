# Troubleshooting

## Movement works, but break, place, use, or interact disconnects

Upgrade Edge Fabric to v7.3.8 or newer. Earlier Full Proxy builds decoded and
serialized every gameplay packet, which could alter hotfix packet layouts even
when the client and backend both used protocol 1001. The lossless relay in v7.3.8
forwards original decrypted packet bytes unless a reviewed translator explicitly
changes the packet. If a disconnect remains, retain the Session Core error and
the matching Protocol Inspector decode-failure record.

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

1. Confirm the backend uses companion v3.6.1.
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
