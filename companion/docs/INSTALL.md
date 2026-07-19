# Installation

Read [COMPLETE_SETUP.md](COMPLETE_SETUP.md) for the full build, installation,
multi-server, firewall, upgrade, and troubleshooting procedure.

Quick installation:

1. Stop the Endstone backend.
2. Copy `ninjos_proxie_companion.so` into `plugins/`.
3. Copy the generated `companion.properties` into
   `plugins/ninjos_proxie_companion/`.
4. Delete any old cached copy under `plugins/.local/`.
5. Start the backend.
6. Confirm the enabled log includes the expected dashboard, backend ID, and secret
   fingerprint.
7. Run `/npm probe` and `/npm status` from the server console.

A `401 Unauthorized` probe result means the dashboard was reached but the backend
ID or secret does not match. A timeout or connection-refused result points to the
host, TCP port, allocation, firewall, or NAT path.
