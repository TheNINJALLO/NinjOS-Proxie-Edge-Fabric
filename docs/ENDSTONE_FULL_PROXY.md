# Endstone Full Proxy setup

1. Add the backend in the dashboard with `full_proxy`, adapter `endstone`, backend online mode disabled, and proxy identity required.
2. Generate a unique backend secret in Secret Vault.
3. Build Endstone Companion v3.6.0 against the exact Endstone API installed on the backend.
4. Install the `.so` under the backend's `plugins/` folder.
5. Configure `companion.properties`:

```properties
dashboard_host=PROXY_PRIVATE_IP
dashboard_port=25571
server_id=kingdom
shared_secret=UNIQUE_BACKEND_SECRET
identity_bridge_enabled=true
require_proxy_identity=true
```

6. Set BDS `online-mode=false`.
7. Firewall the backend UDP port so only the proxy can reach it.
8. Start the proxy before the backend.
9. Run `/npm probe` from Endstone.
10. Join through the Full Proxy public port and verify `/npm status`, `/op`, member plugin commands, and the dashboard player card.

On join, Companion v3.6.0 retrieves the one-use grant asynchronously, sets operator status, attaches returned permission nodes, recalculates permissions, and calls `updateCommands()` immediately and again after initialization.

For Transparent Auth Mode, set both identity options to false and keep BDS `online-mode=true`.
