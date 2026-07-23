# Ninj-OS Proxie Session Core

`@theninjallo/ninjos-proxie-session-core` is the protocol-aware Bedrock session layer used by Ninj-OS Proxie Edge Fabric. It supplies the Node.js relay and HMAC/session support used alongside the native edge gateway and dashboard.

It also ships Protocol Weave: validated protocol packs, fail-closed compatibility
selection, tiered metadata/decoded/wire/failure/round-trip packet inspection,
and limited declarative translation.
See `protocol-packs/PACK_FORMAT.md` and the repository's
`docs/PROTOCOL_WEAVE.md` for the safety and update workflow.

This package is a component of the complete Edge Fabric, not a standalone end-user proxy distribution. Most operators should install the full runtime from the [v7.3.10 Release](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/releases/tag/v7.3.10) and follow the [Wiki](https://github.com/TheNINJALLO/NinjOS-Proxie-Edge-Fabric/wiki).

## Requirements

- Node.js 22 or newer
- The matching Ninj-OS Proxie Edge Fabric release
- Runtime configuration and secrets supplied through the full Edge Fabric installation

## Registry installation

Configure npm to use GitHub Packages for the `@theninjallo` scope, authenticate with a GitHub token that can read packages, and install the exact matching version:

```ini
@theninjallo:registry=https://npm.pkg.github.com
```

```bash
npm install @theninjallo/ninjos-proxie-session-core@7.3.10
```

See the repository documentation for configuration, security, networking, upgrades, and compatibility. Licensed under AGPL-3.0-only.
