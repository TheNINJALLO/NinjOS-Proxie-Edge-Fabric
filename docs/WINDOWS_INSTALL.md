# Windows installation

Ninj-OS uses Linux socket and `epoll` facilities, so the supported Windows proxy deployment is Windows 11 or Windows Server with WSL2 rather than an untested native gateway port. The vanilla host agent does include a native Windows x86_64 binary.

## Requirements

- Windows 11 22H2 or newer, or a compatible Windows Server release
- Administrator PowerShell
- WSL2 with an Ubuntu distribution
- Mirrored networking for public UDP listeners
- Runtime archive, checksum, `install-standalone.sh`, and `install-windows.ps1` in one folder

## Install the proxy

Open PowerShell as Administrator:

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\install-windows.ps1
```

When WSL must be installed, restart Windows, complete Ubuntu initialization, and run the command again.

The installer:

1. Enables WSL2.
2. Configures mirrored networking in `%USERPROFILE%\.wslconfig`.
3. Enables systemd inside Ubuntu.
4. Runs the Linux installer inside WSL2.
5. Installs the pinned Node.js runtime when Full Proxy Mode requires it.
6. Creates Windows Defender Firewall rules.
7. Creates Hyper-V firewall rules when the cmdlets are available.
8. Creates a startup task that starts the WSL systemd service.

## Manage the proxy

```powershell
.\manage-windows.ps1 status
.\manage-windows.ps1 start
.\manage-windows.ps1 stop
.\manage-windows.ps1 restart
.\manage-windows.ps1 logs
.\manage-windows.ps1 setup-code
```

Open the dashboard at `http://localhost:25571` or the host LAN address.

## Router and firewall

Forward public UDP listeners to the Windows host. Do not forward the private backend ports. The installer creates local firewall rules, but router/NAT and hosting-provider rules remain the operator's responsibility.

## Native Windows vanilla agent

For a Mojang BDS running directly on Windows, use:

```text
NinjOS-Vanilla-Agent-Windows-v7.3.8.zip
```

Edit `agent.json`, then run:

```powershell
.\ninjos-vanilla-agent.exe --config .\agent.json
```

The supplied PowerShell helper explains scheduled-task or service-wrapper installation. The behavior pack remains required when the backend needs in-game identity and command-permission restoration.

## Limitations

- The gateway itself runs in WSL2, not as a native Windows executable.
- Public UDP behavior depends on a current WSL mirrored-networking implementation.
- Test every public listener from a device outside the host before production use.
