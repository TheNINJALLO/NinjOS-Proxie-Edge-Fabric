# Changelog

## 3.6.0

- Added signed full-proxy identity consumption.
- Restores operator state, Endstone permission attachments, and the Bedrock command list after join.
- Added strict rejection for direct or unverified joins in Full Proxy Mode.
- Transparent Auth Mode can disable the identity bridge and retain native Microsoft authentication.

## 3.5.1

- Added `/npm probe` for a direct signed dashboard connectivity test.
- Added target, server ID, secret fingerprint, last attempt, last success, and last error to `/npm status`.
- Added rate-limited upload failure warnings and connection-restored logging.
- Normalized server IDs and corrected the fallback dashboard port to 25571.

## 3.5.0

- Added transfer-ticket correlation.
- Added destination arrival confirmation through XUID presence.
- Added presence join and leave events.
- Added capability negotiation metadata.
- Retained packet receive/send monitoring, bounded queues, HMAC signing,
  redaction, drop rules, BDS metrics, and Linux glibc compatibility checks.
