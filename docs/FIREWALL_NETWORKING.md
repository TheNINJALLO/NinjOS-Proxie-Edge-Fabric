# Firewall and networking

Ninj-OS Proxie uses UDP for Minecraft Bedrock traffic and TCP for its dashboard.
TCP and UDP are separate transports, so the same numeric port can be used once for
UDP and once for TCP.

## Example ports

The bundled example uses:

```text
25566/UDP       Primary public Bedrock route
25571/UDP       Secondary public Bedrock route
25571/TCP       Dashboard and companion API
25572-25581/UDP Temporary transfer-ticket pool
```

Replace these with the ports actually configured on your server.

## UFW

```bash
sudo ufw allow 25566/udp
sudo ufw allow 25571/udp
sudo ufw allow 25571/tcp
sudo ufw allow 25572:25581/udp
sudo ufw reload
sudo ufw status
```

Restrict dashboard access to a trusted administrator address when practical:

```bash
sudo ufw delete allow 25571/tcp
sudo ufw allow from ADMIN.PUBLIC.IP to any port 25571 proto tcp
```

## firewalld

```bash
sudo firewall-cmd --permanent --add-port=25566/udp
sudo firewall-cmd --permanent --add-port=25571/udp
sudo firewall-cmd --permanent --add-port=25571/tcp
sudo firewall-cmd --permanent --add-port=25572-25581/udp
sudo firewall-cmd --reload
sudo firewall-cmd --list-ports
```

## Home router or NAT

Forward the public UDP route ports and UDP transfer range to the machine running
Ninj-OS Proxie. Forward the dashboard TCP port only when remote dashboard access is
needed. Prefer a VPN or source-IP firewall rule over exposing the dashboard to the
entire internet.

Each permanent backend route needs its own public UDP port. Do not assign a
permanent backend to a port reserved for temporary transfer tickets.

## Same-host backends

When the proxy and Bedrock server are on the same machine, the backend may use
`127.0.0.1` or a private interface address. Ensure the Bedrock server listens on a
different UDP port from the public proxy route.

Example:

```text
Public players -> 203.0.113.10:25566/UDP
Ninj-OS route   -> 127.0.0.1:19132/UDP
Dashboard       -> 203.0.113.10:25571/TCP
```

## Cloud providers

Open the same ports in both places:

1. the operating-system firewall; and
2. the provider security group, network firewall, or edge firewall.

A port opened in only one layer can still appear unreachable.

## Verification

Confirm the service is listening:

```bash
sudo ss -lunp | grep -E '25566|25571|2557[2-9]|2558[01]'
sudo ss -ltnp | grep 25571
```

UDP does not establish a persistent listening connection like TCP, so generic web
port-check sites may incorrectly report Bedrock UDP ports as closed. Test UDP with
a Bedrock client or an appropriate UDP-aware network tool.

## Windows 11 with WSL2

The Windows installer creates both Windows Defender Firewall rules and Hyper-V firewall rules for the configured ports.

Review standard firewall rules:

```powershell
Get-NetFirewallRule -Group "Ninj-OS Proxie"
```

Review WSL Hyper-V firewall rules:

```powershell
Get-NetFirewallHyperVRule `
  -VMCreatorId '{40E0AC32-46A5-438A-A0B2-2B479E8F2E90}'
```

WSL2 must use mirrored networking for direct LAN access:

```ini
[wsl2]
networkingMode=mirrored
firewall=true
```

After changing `%UserProfile%\.wslconfig`, apply it with:

```powershell
wsl --shutdown
.\manage-windows.ps1 start
```

Home routers and upstream firewalls must still forward the public UDP routes, transfer range, and any remotely exposed dashboard TCP port to the Windows host.
