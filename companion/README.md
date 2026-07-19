# Ninj-OS Proxie Endstone Companion

The companion sends packet metadata, player presence, TPS, MSPT, backend resource
metrics, and transfer confirmation from an Endstone backend to Ninj-OS Proxie
Edge Fabric.

Transport protection remains active when the companion is unavailable. Gameplay
packet visibility, presence details, and server-performance metrics require a
successful companion connection.

## Start here

Read the complete operator guide before installing:

[Complete Companion Setup Guide](docs/COMPLETE_SETUP.md)

The guide covers GitHub Actions builds, local Linux builds, Pterodactyl and
standalone installation, multi-server deployments, secrets, firewall rules,
verification, upgrades, and troubleshooting.

## Credentials

`shared_secret` is a per-backend companion secret. It is not the dashboard owner
username, owner password, browser session, operator token, viewer token, or
metrics token. Use the configured install package generated for the matching
backend whenever possible.

## Runtime commands

```text
/npm status
/npm reload
/npm probe
```

`/npm status` prints the target dashboard, backend ID, secret fingerprint, queue
statistics, upload totals, last successful upload, and last connection error.

`/npm probe` sends a signed test record directly to the dashboard. Use it after
installation or after changing `companion.properties`.

## Build

The included GitHub Actions workflow builds against a selected Endstone API
release on Ubuntu 22.04 and checks the resulting shared library for glibc 2.35
compatibility.

Every build regenerates `docs/COMPLETE_SETUP.md` from the versioned template and
places `COMPANION-HOWTO.md` in the compiled install package.

Local build instructions are in [docs/BUILD.md](docs/BUILD.md).
