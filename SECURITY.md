# Security Policy

## Supported release

Security fixes are applied to the current release line.

## Reporting a vulnerability

Do not publish credentials, setup codes, packet captures, private server addresses, or exploitable details in a public issue.

Use GitHub private vulnerability reporting when enabled for the repository. Include the affected version, deployment platform, reproduction steps, impact, and any proposed mitigation.

## Secret handling

Never commit:

- Dashboard passwords or recovery values
- Companion shared secrets
- Discord bot tokens or webhook URLs
- `config/edge-fabric.ini` from a live server
- `/etc/ninjos-proxie.env`
- `runtime/edge-fabric.db`
- Support bundles containing private operational data
