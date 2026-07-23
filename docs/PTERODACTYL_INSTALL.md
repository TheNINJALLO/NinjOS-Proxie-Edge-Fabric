# Pterodactyl installation

This guide installs Ninj-OS Proxie Edge Fabric v7.3.14 on one Pterodactyl server. The same proxy instance can route Transparent Auth and Full Proxy backends.

## Files

Use these files from the deployment ZIP:

```text
egg-ninjos-proxie-edge-fabric-v7.3.14.json
NinjOS-Proxie-Edge-Fabric-v7.3.14-Runtime.tar.gz
```

The runtime already contains the gateway, dashboard, Bedrock Session Core dependencies, vanilla bridge pack, Linux and Windows vanilla agents, and Endstone source package.

## 1. Import the egg

1. Open the Pterodactyl administrator panel.
2. Open **Nests** and select the target nest.
3. Choose **Import Egg**.
4. Import the v7.3.14 egg.
5. Create a new server or assign the egg to the existing proxy server.

The egg uses a Node.js 22 image because Full Proxy Mode uses the bundled Session Core. Transparent Auth Mode also works in the same image.

## 2. Assign allocations

Assign one UDP allocation for each public listener and one TCP allocation for the dashboard. Also assign every UDP port used by the optional transfer pool.

Example:

```text
19132/UDP       Transparent public listener
19133/UDP       Full Proxy public listener
25571/TCP       Dashboard, bridge, and agent API
25572-25581/UDP Optional transfer pool
```

Private backend ports should be separate server allocations and must not be published to players. An `online-mode=false` backend must accept traffic only from the proxy host or private network.

## 3. Reinstall and upload the runtime

1. Back up `config/` and `runtime/` when upgrading.
2. Use **Reinstall** after assigning the egg.
3. Upload the runtime archive to the proxy server root.
4. Keep both filenames unchanged.

The bootstrap validates the archive structure, extracts into a temporary staging directory, verifies required files, preserves `config/` and `runtime/`, then installs the release atomically. If you set `RUNTIME_SHA256` yourself, it also verifies that digest before extraction.

## 4. Startup variables

On a fresh installation, leave owner credential variables empty. The dashboard owner is created in the browser.

Recommended variables:

```text
SESSION_CORE_TOKEN=              # leave empty to generate automatically
COMPANION_SHARED_SECRET=         # set a long random default
COMPANION_KINGDOM_SECRET=        # unique per backend
COMPANION_LOBBY_SECRET=          # unique per backend
DASHBOARD_RECOVERY_TOKEN=        # empty unless recovering the account
```

Never use the owner password as a bridge secret. Every backend should have a different secret.

## 5. Start and claim the dashboard

Start the proxy. The console prints a one-use setup code and writes it temporarily to:

```text
runtime/FIRST_RUN_SETUP.txt
```

Open `http://PROXY-IP:DASHBOARD-PORT`, enter the setup code, and choose the permanent owner username and password. The setup file is deleted after successful account creation.

## 6. Add Transparent Auth backends

Use this mode for ordinary Endstone or vanilla servers that should keep native Microsoft authentication:

```text
connection_mode=transparent
backend_online_mode=true
require_proxy_identity=false
```

The backend keeps its real XUIDs, native `permissions.json`, operator state, and existing commands. No bridge is required.

## 7. Add Full Proxy backends

Use this mode only when the backend port is private:

```text
connection_mode=full_proxy
backend_online_mode=false
require_proxy_identity=true
```

Choose one adapter:

- `endstone`: install Endstone Companion v3.7.1.
- `vanilla_bridge`: install the behavior pack.
- `vanilla_agent`: install the behavior pack and host agent.
- `proxy_only`: no backend component, with limited permission integration.

The public UDP allocation belongs to Ninj-OS. The backend server listens on a different private UDP allocation.

## 8. Verify

The panel changes from **Starting** to **Running** only after the configured
listeners bind successfully and publish runtime health. The console must show
`[Ninj-OS Proxie] Runtime ready`. An `Address already in use` error stops startup
clearly; verify each allocation belongs to only one backend or transfer listener,
then restart the container.

For every route:

1. Confirm the dashboard shows the intended connection mode and adapter.
2. Confirm the public listener is reachable.
3. Confirm the private backend allocation is not directly reachable from the internet.
4. Join with a test account.
5. Verify OP and member commands.
6. Test `/server`, `/hub`, and the configured fallback for Full Proxy routes.
7. Confirm the backend card reports connected or healthy.

## Upgrading

Upload the new runtime, set `FORCE_RUNTIME_INSTALL=1` for one start when needed, then return it to `0`. Preserve `config/` and `runtime/` backups before every upgrade.
