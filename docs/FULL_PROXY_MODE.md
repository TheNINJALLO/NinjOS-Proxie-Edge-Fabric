# Full Proxy Mode

Full Proxy Mode uses the Ninj-OS Bedrock Session Core. The player authenticates with the proxy, and the proxy creates an offline downstream connection to the selected private backend.

## Backend settings

```properties
online-mode=false
```

Dashboard settings:

```text
Connection Mode: Full Proxy
Backend Online Mode: Disabled
Require Proxy Identity: Enabled
Backend Adapter: Endstone, Vanilla Bridge, or Vanilla Agent
```

## Required firewall layout

Only the public proxy listener is internet-facing. The backend UDP port accepts traffic from the proxy host/private network only. This is mandatory because an offline backend cannot safely distinguish a direct username impersonation by itself.

## Identity lifecycle

1. Session Core authenticates the Xbox profile.
2. The XUID is checked against the Ninj-OS profile database.
3. A one-use grant is created for the target backend.
4. The downstream connection begins.
5. The Endstone or vanilla bridge consumes the grant.
6. Operator and permission state are applied.
7. The grant is marked consumed and cannot be replayed.

## Proxy commands

`/server`, `/hub`, `/lobby`, `/glist`, `/find`, and `/proxie` are intercepted by the Session Core.

## Fallback behavior

Set `fallback_backend` to another enabled Full Proxy listener or a public transparent listener. When the downstream fails, the Session Core sends a Bedrock transfer to the fallback public address. The current implementation uses a controlled transfer packet; retained in-place downstream swapping will continue to mature in later v7 updates.

## Protocol package

The Session Core pins a specific `bedrock-protocol` version in `package-lock.json`. Do not run an unreviewed dependency upgrade on a production proxy. Build and test the new lockfile against a test backend first.
