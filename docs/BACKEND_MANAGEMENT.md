# Backend Management

## Add a server

1. Add a new UDP allocation to the proxy server in Pterodactyl.
2. Add the port to **Managed public UDP ports** under Managed Settings.
3. Open **Backends & Sessions**.
4. Click **Add Server**.
5. Enter the server ID, display name, backend host and port, public route port,
   profile, and optional companion environment variable.
6. Test the backend.
7. Save.

The gateway restarts using the new topology while the dashboard records the
change in the audit log.

## Edit a server

Every backend field can be changed. Renaming the backend ID also updates the
primary backend when necessary. After renaming, review its companion key and
redownload the companion package because `server_id` changes.

## Companion key

Use the **Companion** action on a backend row, then click **Set / Rotate Backend
Key**. The key may be dashboard-managed, an environment reference, or inherited
from the default companion key.

## Port restrictions

The permanent route port:

- Must be a valid UDP port
- Must be listed in `edge.managed_public_udp_ports`
- Must not duplicate another backend route
- Must not fall inside the temporary transfer-ticket pool

## Save and apply behavior in v7.3.0

When **Save Server** is pressed, the dashboard performs this transaction:

1. Validate the server ID, addresses, ports, profile, and secret reference.
2. Write a backup of the existing unified configuration.
3. Write the new backend registry to `config/edge-fabric.ini`.
4. Regenerate `gateway.conf`, `runtime/topology.properties`, and companion secret mappings.
5. Read the backend registry back from disk and compare it with the requested registry.
6. Return a confirmed save response to the browser.
7. Restart only the gateway process. The dashboard remains online.

If steps 3 through 5 fail, the previous configuration is restored automatically.

## Adding more servers

There is no fixed two-server limit. Add additional backends from **Backends → Add Server**. Practical limits are available Pterodactyl allocations, UDP ports, CPU, memory, and backend capacity.

Use a public port of `0` for an internal or failover-only backend. For a publicly reachable route, first assign a UDP allocation to this Pterodactyl server and add that port to:

```ini
[edge]
managed_public_udp_ports = 25566,25571,25572-25581,25582
```

The transfer range is shared. Ports assigned permanently to backends are automatically excluded from temporary transfer tickets.
