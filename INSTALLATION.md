# Ninj-OS Proxie Edge Fabric v7.3.0 installation index

v7.3.0 is one universal package for Pterodactyl, standalone Linux, Docker on Linux, and Windows through WSL2. The same proxy can mix native `online-mode=true` routes with proxy-authenticated `online-mode=false` routes.

## Package contents

The deployment ZIP contains:

```text
NinjOS-Proxie-Edge-Fabric-v7.3.0-Runtime.tar.gz
NinjOS-Proxie-Edge-Fabric-v7.3.0-Runtime.tar.gz.sha256
egg-ninjos-proxie-edge-fabric-v7.3.0.json
install-standalone.sh
install-windows.ps1
manage-windows.ps1
uninstall-windows.ps1
NinjOS-Vanilla-Bridge-v7.3.0.mcpack
NinjOS-Vanilla-Agent-Linux-v7.3.0.zip
NinjOS-Vanilla-Agent-Windows-v7.3.0.zip
NinjOS-Endstone-Companion-v3.6.0-GitHub-Clean.zip
ENDSTONE-COMPANION-HOWTO.md
docs/
```

## Step 1: Install the proxy host

- Pterodactyl: [`docs/PTERODACTYL_INSTALL.md`](docs/PTERODACTYL_INSTALL.md)
- Linux/systemd: [`docs/LINUX_INSTALL.md`](docs/LINUX_INSTALL.md)
- Docker on Linux: [`docs/DOCKER_INSTALL.md`](docs/DOCKER_INSTALL.md)
- Windows through WSL2: [`docs/WINDOWS_INSTALL.md`](docs/WINDOWS_INSTALL.md)

## Step 2: Claim the dashboard

On first start, copy the single-use code from the console or `runtime/FIRST_RUN_SETUP.txt`. Open the dashboard TCP address, enter the code, and choose the permanent owner username and password.

## Step 3: Choose a connection mode per backend

### Transparent Auth Mode

Use when the backend should keep native Microsoft authentication:

```text
backend online-mode=true
connection mode=transparent
require proxy identity=false
```

Best for existing Endstone or vanilla servers where native XUID, OP, `permissions.json`, and current commands must remain unchanged.

### Full Proxy Mode

Use when Ninj-OS should authenticate the player and own the public Bedrock session:

```text
backend online-mode=false
connection mode=full_proxy
require proxy identity=true
```

The backend port must be private. Install an Endstone or vanilla bridge when the backend must receive verified XUID, roles, OP, and permission state.

Read [`docs/CONNECTION_MODES.md`](docs/CONNECTION_MODES.md).

## Step 4: Choose an adapter

| Adapter | Installation | Use case |
|---|---|---|
| Endstone | [`docs/ENDSTONE_FULL_PROXY.md`](docs/ENDSTONE_FULL_PROXY.md) | Native Endstone OP, permissions, commands, and metrics |
| Vanilla Bridge | [`docs/VANILLA_BRIDGE_INSTALL.md`](docs/VANILLA_BRIDGE_INSTALL.md) | Mojang BDS with behavior-pack identity and live command permission |
| Vanilla Agent | [`docs/VANILLA_AGENT_INSTALL.md`](docs/VANILLA_AGENT_INSTALL.md) | Vanilla Bridge plus Linux/Windows process metrics and `permissions.json` synchronization |
| Proxy Only | [`docs/TRANSPARENT_AUTH_MODE.md`](docs/TRANSPARENT_AUTH_MODE.md) | Untouched backend; complete identity compatibility requires Transparent Auth |

## Step 5: Configure ports

Each public Bedrock listener is UDP. The dashboard/bridge API is TCP. A Full Proxy backend needs one public Ninj-OS listener and a different private backend UDP port.

Example:

```text
19132/UDP public Transparent Auth listener
19133/UDP public Full Proxy listener
25571/TCP dashboard and bridge API
19142/UDP private backend listener, not internet-facing
```

## Step 6: Generate unique secrets

Create a separate companion/bridge secret for every integrated backend. Never reuse the dashboard password. Store secrets in the Pterodactyl Startup page, Linux environment file, Docker `.env`, or dashboard Secret Vault.

## Step 7: Test before production

For each backend:

1. Verify the configured mode matches the backend `online-mode` value.
2. Confirm the public listener works.
3. Confirm the private offline-mode backend is blocked from the internet.
4. Verify player identity and XUID.
5. Verify normal member commands.
6. Verify operator commands.
7. Verify `/server` and fallback on Full Proxy routes.
8. Verify the companion/bridge/agent card reports correctly.
9. Back up the final configuration revision.
