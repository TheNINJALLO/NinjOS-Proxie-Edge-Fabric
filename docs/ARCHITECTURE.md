# Architecture

## Edge Shield

The static C++20 gateway owns Transparent Auth UDP listeners, rate controls, address/session limits, incident behavior, health probes, transfer tickets, and transport logs. It forwards opaque traffic and leaves Microsoft authentication end-to-end.

## Bedrock Session Core

The Node.js Session Core uses the pinned `bedrock-protocol` dependency for Full Proxy listeners. It authenticates the upstream player session, opens a separate offline downstream connection, tracks network players, handles proxy commands, prepares signed one-use identity grants, and redirects players to configured public fallback listeners.

Each enabled Full Proxy backend has its own public UDP listener. Current v7.3.7 switching uses a controlled Bedrock transfer to another Ninj-OS listener. A retained-session downstream hot-swap remains a later hardening target and is not represented as complete.

## Control Plane

The static Go dashboard:

- serves the web UI and APIs
- manages owner authentication
- validates backend mode/adapter combinations
- compiles `edge-fabric.ini` into gateway and Session Core runtime files
- stores players, profiles, audits, metrics, and grants in SQLite/in-memory state
- verifies companion and agent HMAC reports
- issues and consumes short-lived identity grants

## Backend adapters

- Endstone Bridge: native OP and permission attachments, command-list refresh, telemetry.
- Vanilla Bridge: Script API identity consume, role tags, live command permission level.
- Vanilla Agent: process health and atomic `permissions.json` operator synchronization on Linux or Windows.
- Proxy Only: no backend integration; use Transparent Auth for full native identity compatibility.

## Generated runtime files

```text
config/edge-fabric.ini
       |
       +--> gateway.conf                         Transparent Auth routes
       +--> runtime/session-core.json            Full Proxy routes
       +--> runtime/generated/dashboard.env
       +--> runtime/companion-secrets.properties
       +--> runtime/config-summary.json
```

## Trust boundary

Transparent Auth trusts Microsoft authentication completed by each backend. Full Proxy trusts authentication completed by Ninj-OS, then requires a signed one-use grant inside an integrated backend. Offline-mode backend ports must be private regardless of adapter.
