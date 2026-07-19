# Vanilla Bridge configuration

Edit `scripts/config.js` before installing the pack. `serverId` must match the dashboard backend ID. `sharedSecret` must match that backend's companion/bridge secret. Use HTTPS when the dashboard is outside the private host network.

`requireProxyIdentity=true` rejects players for whom no unconsumed signed grant exists. A full-proxy backend should keep this enabled and its UDP port must also be private at the firewall.
