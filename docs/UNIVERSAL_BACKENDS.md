# Universal backend adapters

## Endstone

Use the Endstone Companion for native TPS/MSPT, gameplay packet metadata, identity grant consumption, OP restoration, permission attachments, command-list refresh, transfers, and detailed diagnostics.

## Vanilla Bridge

Use the behavior pack when the server runs Mojang's normal `bedrock_server` executable without Endstone. It consumes the identity grant and restores the live command permission level with Script API.

## Vanilla Agent

Add the host agent when the vanilla server also needs process health and automatic `permissions.json` operator synchronization. Linux and Windows binaries are included.

## Proxy Only

Use no backend software. This is fully suitable for Transparent Auth Mode because the backend authenticates the player normally. In Full Proxy Mode it provides routing only and cannot securely restore the verified identity inside an untouched offline backend.
