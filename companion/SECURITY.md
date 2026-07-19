# Security

Do not commit any of the following:

- `COMPANION_SHARED_SECRET`
- `DASHBOARD_TOKEN`
- Discord webhook URLs
- Discord bot tokens
- Production player data
- Captured authentication payloads

Use placeholders in repository files. Configure real values only in the
Pterodactyl server's private configuration.

The build workflow does not require repository secrets.
