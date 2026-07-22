# Quick start

## 1. Pick a host

- Pterodactyl: import the v7.3.5 egg and upload the runtime archive.
- Linux: run `sudo ./install-standalone.sh ./NinjOS-Proxie-Edge-Fabric-v7.3.5-Runtime.tar.gz`.
- Windows: run `install-windows.ps1` as Administrator; the gateway runs in WSL2.
- Docker: use `deploy/docker/docker-compose.yml` on Linux.

## 2. Create the dashboard owner

Start the proxy, copy the one-use code from the console or `runtime/FIRST_RUN_SETUP.txt`, open TCP port 25571, and choose the owner username and password.

## 3. Choose a mode for every backend

### Keep native authentication

```ini
connection_mode = transparent
backend_online_mode = true
require_proxy_identity = false
```

Use this for vanilla or Endstone servers that should preserve native XUIDs, OPs, and existing commands.

### Use the full proxy

```ini
connection_mode = full_proxy
backend_online_mode = false
require_proxy_identity = true
```

Keep the backend port private. Install the Endstone Bridge or Vanilla Bridge when the backend must restore verified identity and permissions.

## 4. Test

- Join every public listener.
- Verify native commands on Transparent Auth routes.
- Verify identity, OP, member commands, `/server`, and fallback on Full Proxy routes.
- Confirm offline-mode backend ports are unreachable directly.
- Confirm each companion or agent reports to its own dashboard card.
