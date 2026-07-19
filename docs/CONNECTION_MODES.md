# Connection modes

Ninj-OS v7.3.0 does not force one authentication model on an entire network. The mode is chosen per backend.

## Transparent Auth Mode

The C++ Edge Shield forwards the original encrypted Bedrock session. The backend remains `online-mode=true` and performs Microsoft/Xbox authentication itself.

Use it for existing production servers, untouched vanilla BDS, servers whose operator files must remain authoritative, and any backend that does not require a retained proxy session.

## Full Proxy Mode

The Node Session Core accepts and authenticates the Bedrock client, then creates a separate downstream connection to an `online-mode=false` backend. It creates a one-use identity grant containing the verified profile and proxy role.

Use it for proxy commands, proxy-owned player sessions, fallback listeners, protected offline backends, and centralized XUID roles.

Full Proxy Mode requires either an Endstone or Vanilla Bridge for secure identity/permission restoration inside the backend. `proxy_only` remains available for controlled test cases, but it cannot make an untouched offline BDS understand the proxy's verified XUID.

## Safe combinations

| Mode | Backend online mode | Adapter | Result |
|---|---:|---|---|
| Transparent | true | Endstone | Native XUID/OP plus telemetry |
| Transparent | true | Proxy Only | Untouched vanilla compatibility |
| Transparent | true | Vanilla Agent | Native identity plus host metrics |
| Full Proxy | false | Endstone | Signed identity and native plugin permission restoration |
| Full Proxy | false | Vanilla Bridge | Signed identity and live BDS command-level restoration |
| Full Proxy | false | Vanilla Agent | Vanilla Bridge plus operator-file and host synchronization |
| Full Proxy | false | Proxy Only | Limited; firewall required and no backend identity guarantee |

A transparent backend must not enable `require_proxy_identity`. A full-proxy backend must not set `backend_online_mode=true`.
