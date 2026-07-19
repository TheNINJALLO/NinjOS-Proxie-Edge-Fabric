# Ninj-OS Proxie Edge Fabric v7.3.0

Ninj-OS Proxie Edge Fabric is a universal Minecraft Bedrock network proxy and management platform. One installation can protect and route ordinary Mojang Bedrock Dedicated Servers, Endstone servers, and mixed networks while allowing every backend to choose the authentication model that fits it.

The project includes two connection engines:

- **Transparent Auth Mode** keeps the backend on `online-mode=true`. Microsoft/Xbox authentication, native XUIDs, `permissions.json`, operator status, and existing vanilla or Endstone commands remain authoritative.
- **Full Proxy Mode** authenticates the player at Ninj-OS, owns the Bedrock session, opens a separate downstream connection to an `online-mode=false` backend, and issues a one-use identity grant to an Endstone or vanilla bridge.

A single network can use both modes at the same time.

```text
Players
   |
   v
Ninj-OS Edge Shield
   |
   +-- Transparent Auth listeners ----------> online-mode=true backends
   |
   +-- Bedrock Session Core ----------------> online-mode=false backends
             |                                      |
             +-- Xbox identity                      +-- Endstone Bridge
             +-- proxy commands                     +-- Vanilla Bridge
             +-- fallback routes                    +-- Vanilla Host Agent
             +-- signed join grants                 +-- Proxy-only adapter
```

## What v7.3.0 contains

- C++ UDP Edge Shield for protected transparent forwarding
- Node.js Bedrock Session Core pinned to `bedrock-protocol` 3.56.1
- Go dashboard and control plane
- SQLite player, transfer, audit, and network profile database
- Endstone Companion v3.6.0 source with identity and permission restoration
- Ninj-OS Vanilla Bridge behavior pack for unmodified Mojang BDS executables
- Linux and Windows Vanilla Host Agent binaries
- Pterodactyl egg
- Native Linux/systemd installer
- Linux Docker Compose deployment
- Windows 11/Windows Server deployment through WSL2
- First-run dashboard owner setup
- Full operator, secrets, backend, performance, health, and configuration pages

## Connection modes

### Transparent Auth Mode

Use this when native authentication and maximum compatibility matter more than proxy-owned server switching.

```properties
online-mode=true
```

Benefits:

- Native Microsoft/Xbox authentication remains intact
- Real XUIDs reach the backend normally
- Vanilla `permissions.json`, `/op`, and `/deop` continue to work
- Existing Endstone command defaults and permissions are unchanged
- No behavior pack or backend bridge is required
- Works with ordinary vanilla BDS and Endstone

Limitations:

- The proxy cannot keep one authenticated Bedrock session while changing downstream servers
- Full-proxy identity forwarding and permission replacement are not used
- Transfers use Bedrock transfer behavior rather than a retained session

### Full Proxy Mode

Use this for proxy-owned sessions, protected backend addresses, proxy commands, signed identity forwarding, and fallback routing.

```properties
online-mode=false
```

Requirements:

- The backend UDP port must not be publicly reachable
- Ninj-OS authenticates the player before opening the backend connection
- The backend should use the Endstone Bridge or Vanilla Bridge when operator and permission restoration is required
- Every backend must have a unique shared secret

Benefits:

- Verified XUID and player identity are stored at the proxy
- `/server`, `/hub`, `/lobby`, `/glist`, and `/find` are handled by the proxy
- The player can be redirected to a configured fallback listener
- Operators and roles are keyed by verified XUID
- Endstone restores operator state, permission attachments, and the Bedrock command list
- Vanilla Bridge restores the live BDS command permission level
- Vanilla Host Agent synchronizes proxy operators into `permissions.json`
- Direct backend joins can be rejected

The Full Proxy Session Core is new in v7.3.0. Its configuration, identity, command, and packaging tests are included, but every operator should stage it with a test backend before moving a production world.

## Backend adapters

Every backend selects one adapter independently:

| Adapter | Backend software | Identity inside backend | Permission restoration | Host metrics |
|---|---|---:|---:|---:|
| `endstone` | Endstone BDS | Yes | Native plugin attachments and OP | Native Endstone metrics |
| `vanilla_bridge` | Mojang BDS + behavior pack | Yes | Live Script API command level | Script heartbeat |
| `vanilla_agent` | Mojang BDS + behavior pack + host agent | Yes | Live level plus `permissions.json` | Process and memory metrics |
| `proxy_only` | Untouched BDS | Proxy only | Native backend rules only | Health checks only |

For an untouched server that must preserve native operator behavior, use `proxy_only` with **Transparent Auth Mode** and `online-mode=true`.

## Proxy commands

Full Proxy Mode currently provides:

```text
/server
/server <backend>
/hub
/lobby
/glist
/find <player>
/proxie
```

The backend never needs to register these commands because the Session Core intercepts them before they are forwarded.

## Dashboard backend setup

When adding a backend, choose:

1. **Connection mode**: `transparent` or `full_proxy`
2. **Backend adapter**: Endstone, Vanilla Bridge, Vanilla Agent, or Proxy Only
3. **Backend online mode**
4. **Public UDP listener**
5. **Private backend host and port**
6. **Capacity**
7. **Fallback backend**
8. **Require proxy identity**
9. **Unique companion/bridge secret**

The dashboard rejects unsafe combinations. A Full Proxy backend cannot be saved with `backend_online_mode=true`, while identity enforcement cannot be enabled for a transparent backend.

## Quick installation choice

| Platform | Start here |
|---|---|
| Pterodactyl | [`docs/PTERODACTYL_INSTALL.md`](docs/PTERODACTYL_INSTALL.md) |
| Linux VPS/dedicated server | [`docs/LINUX_INSTALL.md`](docs/LINUX_INSTALL.md) |
| Docker on Linux | [`docs/DOCKER_INSTALL.md`](docs/DOCKER_INSTALL.md) |
| Windows 11/Windows Server | [`docs/WINDOWS_INSTALL.md`](docs/WINDOWS_INSTALL.md) |
| Endstone full-proxy backend | [`docs/ENDSTONE_FULL_PROXY.md`](docs/ENDSTONE_FULL_PROXY.md) |
| Vanilla behavior-pack backend | [`docs/VANILLA_BRIDGE_INSTALL.md`](docs/VANILLA_BRIDGE_INSTALL.md) |
| Vanilla Linux/Windows host agent | [`docs/VANILLA_AGENT_INSTALL.md`](docs/VANILLA_AGENT_INSTALL.md) |
| Existing online-mode=true server | [`docs/TRANSPARENT_AUTH_MODE.md`](docs/TRANSPARENT_AUTH_MODE.md) |

The full installation index is in [`INSTALLATION.md`](INSTALLATION.md).

## First dashboard login

A fresh installation prints a single-use setup code in the console and writes it temporarily to:

```text
runtime/FIRST_RUN_SETUP.txt
```

Open the dashboard, enter that code, and choose the permanent owner username and password. The setup file is removed after the account is created.

## Example mixed network

```ini
[backend.kingdom]
connection_mode = transparent
backend_adapter = endstone
backend_online_mode = true
require_proxy_identity = false
host = 127.0.0.1
backend_port = 19140
public_port = 19132
enabled = true
capacity = 50

[backend.lobby]
connection_mode = full_proxy
backend_adapter = vanilla_agent
backend_online_mode = false
require_proxy_identity = true
host = 127.0.0.1
backend_port = 19141
public_port = 19133
enabled = true
capacity = 25
fallback_backend = kingdom
```

The Kingdom keeps normal Xbox authentication and native Endstone permissions. The lobby uses proxy-owned authentication and the Vanilla Bridge plus host agent.

## Security rules

- Never expose an `online-mode=false` backend directly to the internet.
- Use a unique bridge secret for every backend.
- Use HTTPS when bridge traffic leaves a private host or private network.
- Do not use the dashboard password as a companion secret.
- Keep `config/`, `runtime/`, `agent.json`, and bridge configuration out of public repositories.
- Test Full Proxy Mode against a disposable backend before connecting a production world.
- Keep provider-level DDoS protection and host firewalls enabled. The Edge Shield is an additional layer, not a replacement.

## Repository layout

```text
gateway/                 C++ transparent Edge Shield
dashboard/               Go control plane and web dashboard
session-core/            protocol-aware Bedrock Full Proxy engine
companion/               Endstone Companion v3.6.0 source
bridges/vanilla-addon/   Vanilla Bridge behavior pack and .mcpack
vanilla-agent/           Linux/Windows host agent source
prebuilt/                release binaries
deploy/                  systemd, Docker, and Windows deployment files
docs/                    complete operator documentation
tests/                   integration and packaging tests
```

## Building

Linux build requirements:

```text
CMake 3.20+
C++20 compiler
Go 1.23+
Node.js 22+
Python 3
SQLite development files
```

Build and test:

```bash
./scripts/build.sh
./scripts/test.sh
```

Create release packages:

```bash
./scripts/package-release.sh ./dist
```

## Project relationship and license

Ninj-OS Proxie Edge Fabric is developed with reference to and inspired by **ProxyPass by SculkCatalystMC**. It is not represented as an official ProxyPass fork and is not endorsed by the upstream project. See [`docs/ACKNOWLEDGEMENTS.md`](docs/ACKNOWLEDGEMENTS.md) and [`NOTICE.md`](NOTICE.md).

The Ninj-OS project is distributed under AGPL-3.0. Third-party components retain their own licenses. The Session Core uses the MIT-licensed PrismarineJS `bedrock-protocol` package; its dependency license notices are retained in the packaged runtime.
