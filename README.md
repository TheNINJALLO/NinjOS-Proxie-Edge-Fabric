<div align="center">

# Ninj-OS Proxie Edge Fabric

### A protected, multi-backend edge and management fabric for Minecraft Bedrock networks

[![Release](https://img.shields.io/github/v/release/TheNINJALLO/NinjOS-Proxie-Edge-Fabric?style=for-the-badge&logo=github&color=2ea44f)](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/releases/latest)
[![Wiki](https://img.shields.io/badge/Documentation-Wiki-0969da?style=for-the-badge&logo=readthedocs&logoColor=white)](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/wiki)
[![Packages](https://img.shields.io/badge/GitHub-Packages-8250df?style=for-the-badge&logo=github&logoColor=white)](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/packages)
[![License](https://img.shields.io/github/license/TheNINJALLO/NinjOS-Proxie-Edge-Fabric?style=for-the-badge&color=orange)](LICENSE)

[![Edge Fabric](https://img.shields.io/badge/Edge_Fabric-7.3.13-111827?style=flat-square)](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/releases/tag/v7.3.13)
[![Endstone Companion](https://img.shields.io/badge/Companion-3.7.0-8b5cf6?style=flat-square)](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/wiki/Endstone-Companion)
[![Endstone](https://img.shields.io/badge/Endstone_Target-0.11.6-22c55e?style=flat-square)](companion/docs/COMPATIBILITY.md)
[![Minecraft Bedrock](https://img.shields.io/badge/Vanilla_Bridge-1.26.30+-62b47a?style=flat-square&logo=minecraft&logoColor=white)](docs/VANILLA_BRIDGE_INSTALL.md)
[![bedrock-protocol](https://img.shields.io/badge/bedrock--protocol-3.57.0-f59e0b?style=flat-square)](session-core/package.json)

[![C++20](https://img.shields.io/badge/C++-20-00599c?style=flat-square&logo=cplusplus&logoColor=white)](gateway/)
[![Go](https://img.shields.io/badge/Go-1.23+-00add8?style=flat-square&logo=go&logoColor=white)](dashboard/)
[![Node.js](https://img.shields.io/badge/Node.js-22+-339933?style=flat-square&logo=nodedotjs&logoColor=white)](session-core/)
[![JavaScript](https://img.shields.io/badge/JavaScript-Script_API-f7df1e?style=flat-square&logo=javascript&logoColor=111)](bridges/vanilla-addon/)
[![Python](https://img.shields.io/badge/Python-3-3776ab?style=flat-square&logo=python&logoColor=white)](scripts/)
[![PowerShell](https://img.shields.io/badge/PowerShell-Windows-5391fe?style=flat-square&logo=powershell&logoColor=white)](install-windows.ps1)
[![Shell](https://img.shields.io/badge/Shell-Linux-4eaa25?style=flat-square&logo=gnubash&logoColor=white)](install-standalone.sh)

[**Get started**](#get-started) · [**Read the Wiki**](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/wiki) · [**Download v7.3.13**](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/releases/tag/v7.3.13) · [**View Packages**](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/packages) · [**Report an issue**](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/issues)

</div>

---

## What is Ninj-OS Proxie Edge Fabric?

Ninj-OS Proxie Edge Fabric is a universal Minecraft Bedrock network proxy, protection layer, session router, and management platform. A single deployment can front ordinary Mojang Bedrock Dedicated Servers, Endstone servers, or a mixed network while allowing each backend to use the authentication model that fits it.

It combines a native UDP edge, a protocol-aware Bedrock session core, a browser-based control plane, persistent SQLite state, backend health and policy automation, and optional Endstone or vanilla integrations. Operators can manage public listeners, private backends, authentication modes, transfers, roles, secrets, health actions, logs, and performance from one fabric.

Full Proxy also includes [Protocol Weave](docs/PROTOCOL_WEAVE.md), a reviewed,
data-driven protocol-pack and translation layer for responding to Bedrock
hotfixes while the pinned upstream codec catches up. Its tiered inspector can
compare metadata, redacted decoded values, selected post-decryption wire bytes,
decode failures, and decode/re-encode results without exposing authentication
packets.

> New operator? Follow the [Quick Start](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/wiki/Quick-Start), then use the complete [Installation Index](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/wiki/Installation-Index).

## Why use it?

- Put protected public UDP listeners in front of private Bedrock backends.
- Mix native Microsoft/Xbox authentication and proxy-owned sessions in one network.
- Route players across Endstone and vanilla backends with per-backend policy.
- Preserve verified identity, XUID-based roles, operator state, and command permissions.
- Operate from a role-aware web dashboard where the owner can maintain individual admin, operator, and viewer accounts alongside TOTP, secrets, health actions, metrics, audit history, transfers, and redacted support bundles.
- Manage service tokens, environment bindings, TOTP, Discord credentials, and per-backend companion secrets from one validated Secret Vault.
- Inspect Full Proxy packets through structured decoded, translated, wire, failure, and round-trip views with bounded on-demand detail loading.
- Deploy through Pterodactyl, native Linux/systemd, Docker, or Windows through WSL2.
- Integrate Endstone with a compiled companion plugin or Mojang BDS with a Script API behavior pack and optional host agent.
- Install from a complete offline runtime or consume versioned GitHub container/npm packages.

## Architecture at a glance

```text
Minecraft Bedrock clients
          |
          | public UDP
          v
  +-----------------------+        +--------------------------+
  | NinjOSEdge            |<------>| NinjOSDashboard          |
  | C++20 edge gateway    |        | Go control plane + UI    |
  +-----------+-----------+        +------------+-------------+
              |                                 |
              |                                 +-- SQLite/WAL state
              |                                 +-- accounts and roles
              |                                 +-- Secret Vault
              |                                 +-- metrics and audit
              |
       +------+----------------------+-------------------+
       |                             |                   |
       v                             v                   v
 Transparent backend         Full Proxy backend   Fallback/transfer
 online-mode=true            online-mode=false    listeners
                                     |
                           Node.js Session Core
                                     |
                    +----------------+----------------+
                    |                                 |
             Endstone Companion              Vanilla Bridge
             C++ plugin + metrics             Script API .mcpack
                                                      |
                                             optional Host Agent
                                             Go / Linux + Windows
```

For component boundaries, traffic flow, trust boundaries, and persistence, read the [Architecture Wiki page](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/wiki/Architecture).

## Connection modes

Full Proxy uses a lossless hybrid relay. It terminates encryption for routing,
identity, controls, and inspection, while native gameplay packets cross the relay
as their original decrypted bytes. Packets are serialized again only when a
reviewed Protocol Weave rule changes them.

| Capability | Transparent Auth | Full Proxy |
|---|---|---|
| Backend setting | `online-mode=true` | `online-mode=false` |
| Microsoft/Xbox authentication | Backend remains authoritative | Edge authenticates the player |
| Identity/XUID | Reaches backend natively | Forwarded through a signed one-use grant |
| Existing `/op`, `/deop`, and permissions | Native behavior | Restored by companion/bridge integration |
| Retained proxy-owned session | No | Yes |
| Proxy commands and fallback routing | Limited to transfer behavior | Supported by Session Core |
| Backend may be publicly reachable | Subject to normal hardening | **No—must remain private** |
| Best fit | Maximum native compatibility | Controlled routing, private backends, proxy commands |

One fabric can run both modes at the same time. See [Connection Modes](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/wiki/Connection-Modes), [Transparent Auth](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/wiki/Transparent-Auth-Mode), and [Full Proxy](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/wiki/Full-Proxy-Mode).

## Compatibility matrix

| Component | Version or target | Platform | Important boundary |
|---|---|---|---|
| Edge Fabric | `7.3.13` | Linux x86_64 | Windows hosts run the Linux runtime through WSL2; no native Windows edge binary is claimed. |
| Endstone Companion | `3.7.0` | Linux x86_64 `.so` | Default build target is exact Endstone `0.11.6`. Native API/ABI compatibility must not be assumed across Endstone patch versions. |
| Companion toolchain | C++20, LLVM/Clang 18, libc++ | Ubuntu 22.04 build target | Intended for glibc 2.35 or older-compatible Linux environments. |
| Vanilla Bridge | `7.3.13` | Mojang Bedrock Dedicated Server | Declares minimum engine `1.26.30`, `@minecraft/server` `2.8.0`, and `@minecraft/server-net` `1.0.0-beta.1.26.30-preview.20`. |
| Session Core | `7.3.13` | Node.js 22+ | Pins `bedrock-protocol` `3.57.0`; use the runtime’s pinned dependency set. |
| Dashboard | `7.3.13` | Linux x86_64 | Go 1.23+ is required when building from source. |
| Vanilla Host Agent | `7.3.13` | Linux x86_64 and Windows x86_64 | Used with the Vanilla Bridge when host metrics and `permissions.json` synchronization are needed. |
| Database | SQLite with WAL | Runtime-local persistent storage | Keep runtime databases private and back them up before upgrades. |

The runtime carries pinned Bedrock protocol data used by this release. A packaged protocol dataset is not a promise that every historical or future BDS build is interchangeable. Validate the exact client, BDS, Endstone, Script API, operating system, and architecture combination in staging. Read the [Companion Compatibility Policy](companion/docs/COMPATIBILITY.md).

## Get started

### 1. Choose a deployment

| Environment | Primary guide | Download |
|---|---|---|
| Pterodactyl | [Pterodactyl installation](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/wiki/Pterodactyl-Installation) | [Pterodactyl egg](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/releases/download/v7.3.13/egg-ninjos-proxie-edge-fabric-v7.3.13.json) |
| Linux VPS/dedicated host | [Standalone Linux installation](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/wiki/Standalone-Linux-Installation) | [Linux installer](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/releases/download/v7.3.13/install-standalone.sh) |
| Docker on Linux | [Docker installation](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/wiki/Docker-Installation) | [Container package](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/pkgs/container/ninjos-proxie-edge-fabric) |
| Windows 11/Windows Server | [Windows/WSL2 installation](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/wiki/Windows-Installation) | [Windows installer](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/releases/download/v7.3.13/install-windows.ps1) |
| Manual/offline deployment | [Installation index](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/wiki/Installation-Index) | [Complete runtime](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/releases/download/v7.3.13/NinjOS-Proxie-Edge-Fabric-v7.3.13-Runtime.tar.gz) |

### 2. Claim the dashboard

A fresh installation prints a single-use setup code and temporarily writes it to:

```text
runtime/FIRST_RUN_SETUP.txt
```

Open the dashboard, enter the setup code, and create the permanent owner username and password. The setup file is removed after the account is created. Continue with the [Dashboard Login guide](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/wiki/Dashboard-Login).

### 3. Add and secure backends

For every backend, choose its connection mode and adapter, assign the public listener and private backend address, set capacity/fallback behavior, and generate a unique integration secret. Never reuse the dashboard password as a companion, bridge, or agent secret.

### 4. Install the backend integration

| Backend | Integration | Guide | Download |
|---|---|---|---|
| Endstone | Endstone Companion | [Complete companion setup](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/wiki/Endstone-Companion) | [Compiled Linux package](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/releases/download/v7.3.13/NinjOS-Endstone-Companion-v3.7.0-Endstone-0.11.6-Linux-x86_64.zip) |
| Mojang BDS | Vanilla Bridge | [Vanilla Bridge installation](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/wiki/Vanilla-Bridge) | [Behavior pack](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/releases/download/v7.3.13/NinjOS-Vanilla-Bridge-v7.3.13.mcpack) |
| Mojang BDS + host telemetry | Vanilla Bridge + Host Agent | [Vanilla Host Agent](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/wiki/Vanilla-Host-Agent) | Agent binaries are included in the runtime/source build outputs. |
| Untouched online backend | Proxy Only + Transparent Auth | [Transparent Auth](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/wiki/Transparent-Auth-Mode) | No backend plugin required. |

### 5. Test before production

Verify listener reachability, backend isolation, Xbox authentication, XUID/operator behavior, companion or bridge status, transfers, fallback routing, dashboard roles, secret access, restart persistence, logs, backups, and recovery against a disposable backend before connecting a production world.

## GitHub Packages

### Container image

```bash
docker pull ghcr.io/theninjallo/ninjos-proxie-edge-fabric:7.3.13
```

The moving `latest` tag is also published. Pin `7.3.13` in production for repeatable deployments.

### Session Core npm package

Add the GitHub Packages registry for the account scope:

```ini
@theninjallo:registry=https://npm.pkg.github.com
```

Then install the matching component version:

```bash
npm install @theninjallo/ninjos-proxie-session-core@7.3.13
```

Session Core is an internal fabric component; most operators should use the complete runtime or container rather than assembling it independently.

## Components and languages

| Path | Component | Primary technology |
|---|---|---|
| `gateway/` | Datagram edge, transparent shield, policy enforcement | C++20, Linux sockets, `epoll` |
| `dashboard/` | Control plane, web server, accounts, backends, SQLite storage | Go 1.23+, HTML, CSS, JavaScript |
| `session-core/` | Protocol-aware Full Proxy relay and signed session support | Node.js 22+, CommonJS, `bedrock-protocol` |
| `session-core/protocol-packs/` | Reviewed Bedrock compatibility packs and declarative translators | JSON |
| `companion/` | Endstone identity, permission, metrics, and event integration | C++20, Endstone API, LLVM 18 |
| `bridges/vanilla-addon/` | Mojang BDS identity and live command-permission bridge | JavaScript, Bedrock Script API |
| `vanilla-agent/` | Host metrics and `permissions.json` synchronization | Go, Linux and Windows |
| `deploy/` | systemd, Docker Compose, Docker image, Windows/WSL2 | YAML, Dockerfile, PowerShell |
| `scripts/` and `tests/` | Builds, releases, installation, integration verification | Shell, Python, PowerShell |

## Proxy commands

Full Proxy Mode currently intercepts these commands before they reach the backend:

```text
/server
/server <backend>
/hub
/lobby
/glist
/find <player>
/proxie
```

All other command requests are forwarded to the selected backend unchanged.
Dashboard login roles and Minecraft network-player roles are separate. Assign an
XUID's role under **Network Players**, then have that player reconnect so the
signed Full Proxy identity grant can apply Endstone permissions and refresh its
available commands.

## Security essentials

> [!CAUTION]
> Never expose an `online-mode=false` backend directly to the internet. Restrict it to the proxy host or a private network and enforce the restriction with host and provider firewalls.

- Use a unique, randomly generated secret for every companion, bridge, and agent.
- Never reuse dashboard owner credentials as integration secrets.
- Use HTTPS whenever dashboard or integration traffic leaves a trusted private network.
- Keep `config/`, `runtime/`, `.env`, `agent.json`, databases, recovery tokens, and generated bridge configuration out of source control.
- Retain provider-level DDoS protection. The edge shield is an additional control, not a replacement.
- Review [Firewall and Networking](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/wiki/Firewall-and-Networking), [Secrets and Vault](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/wiki/Secrets-and-Vault), and the [Security Policy](SECURITY.md).

## Build from source

Linux build requirements:

- CMake 3.20+
- C++20 compiler
- Go 1.23+
- Node.js 22+
- Python 3
- SQLite development files

```bash
./scripts/build.sh
./scripts/test.sh
./scripts/package-release.sh ./dist
```

The Endstone Companion has a separate LLVM 18 build workflow because it targets an exact native Endstone API/ABI version. See [Companion Build Instructions](companion/docs/BUILD.md).

## Documentation

The [complete Wiki](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/wiki) includes:

- Architecture and product overview
- Quick start and installation selection
- Pterodactyl, Linux, Docker, and Windows/WSL2 deployment
- Dashboard ownership, account recovery, and management
- Backend configuration and connection modes
- Firewall, port allocation, and network isolation
- Secret Vault and credential handling
- Endstone Companion, Vanilla Bridge, and Host Agent setup
- Upgrades, migration, troubleshooting, known fixes, and release operations

## Support and project information

- [Wiki](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/wiki)
- [Troubleshooting](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/wiki/Troubleshooting)
- [Issue tracker](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/issues)
- [Security policy](SECURITY.md)
- [Contributing guide](CONTRIBUTING.md)
- [Changelog](CHANGELOG.md)

## Provenance and license

Ninj-OS Proxie Edge Fabric is developed with reference to and inspired by [ProxyPass by SculkCatalystMC](https://github.com/SculkCatalystMC/ProxyPass). It is not represented as an official ProxyPass fork and is not endorsed by the upstream project. See [Acknowledgements](docs/ACKNOWLEDGEMENTS.md) and [Notice](NOTICE.md).

Ninj-OS Proxie Edge Fabric is distributed under the [GNU Affero General Public License v3.0 only](LICENSE). Third-party components retain their respective licenses. The Session Core uses the MIT-licensed PrismarineJS `bedrock-protocol` package; dependency notices are retained in the packaged runtime.

---

<div align="center">

**Build carefully. Keep backends private. Test before production.**

[Wiki](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/wiki) · [Releases](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/releases) · [Packages](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/packages) · [Issues](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/issues)

</div>
