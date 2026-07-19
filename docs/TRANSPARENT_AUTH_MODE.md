# Transparent Auth Mode

Transparent Auth Mode is the compatibility path for servers that want Ninj-OS protection and management without disabling Microsoft authentication.

## Backend settings

```properties
online-mode=true
```

Dashboard backend settings:

```text
Connection Mode: Transparent Auth
Backend Adapter: Endstone, Vanilla Agent, or Proxy Only
Backend Online Mode: Enabled
Require Proxy Identity: Disabled
```

## What remains native

The backend receives the real Xbox-authenticated connection. XUID, UUID, allowlist, `permissions.json`, `/op`, `/deop`, vanilla commands, and Endstone permission defaults continue to behave as they did before Ninj-OS.

## What Ninj-OS adds

- Public UDP edge listener
- Rate limiting and incident controls
- Hidden/restricted backend address
- Dashboard backend management
- Health checks
- Endstone or host telemetry when installed
- Protected transfer-ticket routes
- Audit and configuration management

## Restrictions

Transparent Mode does not terminate the Bedrock login session. It therefore cannot retain that session while swapping its downstream connection. Use Full Proxy Mode only for backends that need proxy-owned switching and signed identity restoration.
