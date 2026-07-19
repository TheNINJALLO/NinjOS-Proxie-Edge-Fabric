# Dashboard Management Guide

The dashboard adjusts its navigation to the signed-in account. Viewers receive
the read-only operational picture, operators also receive daily controls, admins
receive configuration and security tools, and the owner alone receives **Team &
Access**. This keeps routine work focused and avoids presenting controls that an
account cannot use.

## Backends & Sessions

The Backend Registry provides complete server management:

- Add a backend
- Edit its name, host, backend UDP port, public proxy UDP port, profile, secret
  reference, enabled state, and fallback state
- Test the backend's RakNet response
- Change the primary backend and routing mode
- Delete a backend
- Jump directly to its companion builder

Live reachability, latency, and active-session counts are displayed in the same
registry row as each backend's configuration. There is no separate duplicate
backend table to reconcile.

## Team & Access

The owner uses this page to maintain their own credentials and individual staff
accounts. Create admin, operator, or viewer accounts; change a role; reset a
password; disable access temporarily; or delete an account. Account changes are
written to the audit log, and existing sessions are revoked whenever access or
credentials change.

A public UDP port must already exist as a Pterodactyl allocation and must be
listed in `edge.managed_public_udp_ports`.

## Configuration & Secrets

### Managed Settings

This structured editor exposes every standard non-secret setting grouped by:

```text
edge
dashboard
companion
transfer
firewall
incident
health
logging
discord
```

Saving validates the full unified configuration before restarting services.

### Advanced Configuration

The advanced editor exposes the complete canonical INI file. Use it for custom
or future keys not yet represented in the structured editor.

Raw secrets appear as `[REDACTED]`. Saving the displayed redacted value preserves
the existing secret.

### Secret Vault

Each entry shows:

- Purpose
- Storage source
- Configured or missing state
- Short SHA-256 fingerprint
- Set/rotate action

The vault supports:

```text
Dashboard-managed value
Pterodactyl Startup environment reference
Inherit default companion secret
Clear optional value
Generate secure value
```

Generated values are revealed once and copied to the clipboard when browser
permissions permit.

### Companion Builder & Downloads

Select a backend to:

- Preview its generated companion configuration with the secret masked
- Set or rotate its companion key
- Download `companion.properties`
- Download the GitHub-ready source repository
- Upload a compiled `.so` or GitHub Actions artifact ZIP
- Download an install-ready server package

The install package contains:

```text
plugins/ninjos_proxie_companion.so
plugins/ninjos_proxie_companion/companion.properties
INSTALL.txt
SHA256SUMS.txt
```

## Endstone Performance fleet

The overview renders a separate performance card for every configured backend.
Cards are not limited to connected companions, so a newly added server appears as
**Never reported** until its companion completes the first signed upload. Stale,
offline, degraded, and connected states are shown independently for each server.

The top summary reports connected companions, average TPS, average and worst MSPT,
combined Endstone player counts, queue depth, and upload failures.
