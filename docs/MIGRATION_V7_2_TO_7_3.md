# Migration from v7.2.x to v7.3.0

Existing backends are migrated to `transparent`, `proxy_only`, `backend_online_mode=true`, and `require_proxy_identity=false`. This preserves their current behavior.

Do not convert a production backend to Full Proxy Mode during the same restart as the proxy upgrade. Upgrade Ninj-OS first, verify Transparent Mode, create a test backend, then validate the Session Core and selected bridge.

Back up `config/` and `runtime/`. The new runtime adds `session-core/`, `runtime/session-core.json`, and `runtime/session-core.token`. Existing owner accounts and SQLite data remain in place.
