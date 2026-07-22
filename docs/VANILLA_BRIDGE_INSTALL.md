# Vanilla Bridge installation

The Ninj-OS Vanilla Bridge runs as a behavior pack on Mojang Bedrock Dedicated Server. It does not require Endstone.

## Install

1. Extract `NinjOS-Vanilla-Bridge-v7.3.7.mcpack` or copy it into the BDS `behavior_packs` directory.
2. Edit `scripts/config.js` inside the installed pack.
3. Set the exact dashboard backend ID, private dashboard URL, and unique backend secret.
4. Add the pack UUID/version to the world's `world_behavior_packs.json`.
5. Configure BDS Script API module permissions using the included `server-net-permissions.json.example` and allow only the dashboard URL.
6. Enable the scripting/beta capabilities required by the `@minecraft/server-net` build shipped with BDS 1.26.30.
7. Set `online-mode=false` only when the dashboard backend uses Full Proxy Mode.
8. Firewall the private backend port.
9. Start Ninj-OS, then BDS, and join through the public proxy listener.

## What it applies

- Adds `ninjos.proxy.verified`
- Adds `ninjos.role.<role>`
- Stores the verified XUID/session/role as player dynamic properties
- Sets `CommandPermissionLevel.Admin` for verified operators
- Sets normal command level for members
- Repeats the permission assignment after player initialization
- Rejects players without a waiting identity grant when strict mode is enabled

The HTTP bridge token is a backend secret. Plain HTTP is acceptable only on localhost or a trusted private network. Use HTTPS for routed networks.
