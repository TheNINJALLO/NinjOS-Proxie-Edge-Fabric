# Ninj-OS Proxie v7.3.0 backend save fix

## Problem corrected

Backend changes could be written while the supervisor restarted the dashboard
that was returning the Save response. On a real Pterodactyl server this could
interrupt the request, leave the edit dialog looking unchanged, and make a
successful or partially completed save look like nothing happened.

## New save transaction

Every backend create, edit, rename, delete, enable, disable, and routing change
now performs the following steps:

1. Validate the complete backend registry.
2. Back up `config/edge-fabric.ini`.
3. Write the requested registry.
4. Regenerate `gateway.conf` and runtime mappings.
5. Read the registry back from disk.
6. Compare the persisted values with the requested values.
7. Return a confirmed response to the dashboard.
8. Restart only the gateway process.

The dashboard remains online throughout backend changes. When read-back
verification fails, the previous configuration is restored automatically.

## Adding additional backend servers

The dashboard is not limited to Kingdom and Zoo. Use:

```text
Backends -> Add Server
```

A public route requires a UDP allocation already assigned to the same
Pterodactyl server. The port must also be listed in:

```ini
[edge]
managed_public_udp_ports = 25566,25571,25572-25581,25582
```

Use `Public proxy UDP port = 0` for an internal-only or failover-only backend.
Ports in the transfer-ticket range can be permanent routes. Assigned ports are removed from temporary ticket availability.
