# Upgrade to v7.3.14

v7.3.14 fixes Full Proxy gameplay disconnects caused by decoding and re-encoding
hotfix packets through an older schema. Native packets now retain their original
decrypted bytes between the two encrypted sessions; only reviewed Protocol Weave
translations are re-encoded. Pterodactyl users should update the runtime and egg.

## Back up

Stop the service and back up:

```text
config/
runtime/
```

Also retain `/etc/ninjos-proxie.env` on standalone Linux and the Docker `.env` file when applicable.

## From v7.2.x

v7.3.14 adds per-backend connection modes, the Session Core, signed identity grants, Vanilla Bridge, and native host agents. Existing backend sections default to Transparent Auth behavior. Do not change a production backend to Full Proxy until its bridge is installed and its private port is firewalled.

## Pterodactyl

1. Import and assign the v7.3.14 egg.
2. Reinstall.
3. Upload the v7.3.14 runtime archive.
4. Start and verify the console version.
5. Review each backend's connection mode and adapter.
6. Replace the Endstone companion with v3.7.1 and restart the backend.

## Linux

```bash
sudo ./install-standalone.sh ./NinjOS-Proxie-Edge-Fabric-v7.3.14-Runtime.tar.gz
```

The installer preserves persistent data and installs the portable Node.js runtime only when required.

## Docker

Rebuild the image and recreate the container without deleting volumes.

## Rollback

Return every backend to its previous authentication mode before rolling back. Restore the prior release files and matching `config/` and `runtime/` backup. An offline-mode backend must never remain publicly exposed during rollback.
