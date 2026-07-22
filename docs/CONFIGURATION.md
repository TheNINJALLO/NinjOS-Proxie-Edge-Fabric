# Unified configuration

Canonical file:

```text
config/edge-fabric.ini
```

Every dashboard save creates a backup, writes atomically, reads the file back, and records the resulting SHA-256 revision in `runtime/config-save-status.json`.

## Main sections

```text
[edge]          public host, managed UDP ports, primary backend
[dashboard]     listener and dashboard sessions
[session_core]  Full Proxy packet schema, advertised client version, MOTD, internal token
[companion]     bridge and telemetry defaults
[transfer]      optional transfer-port pool
[firewall]      transport protection
[incident]      automatic incident thresholds
[health]        backend and companion health policies
[logging]       retention and capture settings
[discord]       optional Discord delivery
[backend.*]     one section per server
[profile.*]     destination protection profiles
```

## Backend fields

```ini
[backend.example]
connection_mode = transparent       # transparent or full_proxy
backend_adapter = proxy_only         # endstone, vanilla_bridge, vanilla_agent, proxy_only
backend_online_mode = true
require_proxy_identity = false
capacity = 50
fallback_backend =
display_name = Example
host = 127.0.0.1
backend_port = 19132
public_port = 19133
enabled = true
fallback = false
protection_profile = default
companion_secret = env:COMPANION_EXAMPLE_SECRET
```

Validation rules:

- `full_proxy` requires `backend_online_mode=false`.
- `require_proxy_identity=true` is valid only for Full Proxy Mode.
- A Full Proxy backend requires its own public listener.
- Fallback targets must name a configured backend.
- Capacity must be positive.
- Every integrated backend should use a unique secret.

## Secret sources

A secret may be dashboard-managed or reference an environment variable:

```ini
companion_secret = env:COMPANION_EXAMPLE_SECRET
```

An empty referenced variable is rejected rather than treated as a saved secret.

## Session Core output

Only enabled Full Proxy backends are written to `runtime/session-core.json`. Only enabled Transparent Auth backends are written to `gateway.conf`. This prevents both engines from binding the same route accidentally.
