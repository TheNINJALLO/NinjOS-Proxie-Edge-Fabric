# Endstone Full Proxy setup

1. Add the backend in the dashboard with `full_proxy`, adapter `endstone`, backend online mode disabled, and proxy identity required.
2. Generate a unique backend secret in Secret Vault.
3. Build Endstone Companion v3.7.1 against the exact Endstone API installed on the backend.
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

Dashboard account roles and Minecraft player roles are intentionally separate.
An owner dashboard login does not automatically make the owner's XUID an
operator. After the first proxied join, open **Network Players**, find the XUID
profile, and choose `operator`. v7.3.16 saves the selection immediately. Fully
disconnect and rejoin so the new signed identity grant applies the role and
refreshes Endstone's command list.

On join, Companion v3.7.1 retrieves the one-use grant asynchronously, briefly
retries while Session Core finishes publishing that grant, applies any network
operator elevation without removing operator status already configured on the
backend, attaches returned permission nodes, recalculates permissions, and calls
`updateCommands()` immediately and again after initialization.

For Transparent Auth Mode, set both identity options to false and keep BDS `online-mode=true`.
