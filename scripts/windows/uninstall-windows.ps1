#requires -Version 5.1

[CmdletBinding(SupportsShouldProcess)]
param(
    [string]$Distro = "Ubuntu",
    [switch]$RemoveData
)

$ErrorActionPreference = "Stop"
$taskName = "Ninj-OS Proxie WSL Startup"
$firewallGroup = "Ninj-OS Proxie"
$wslCreatorId = "{40E0AC32-46A5-438A-A0B2-2B479E8F2E90}"

if ($PSCmdlet.ShouldProcess("Ninj-OS Proxie", "Stop and remove Windows integration")) {
    & wsl.exe -d $Distro -u root -- bash -lc "systemctl disable --now ninjos-proxie.service >/dev/null 2>&1 || true"

    if ($RemoveData) {
        & wsl.exe -d $Distro -u root -- bash -lc "rm -rf /opt/ninjos-proxie /etc/ninjos-proxie.env /etc/systemd/system/ninjos-proxie.service && systemctl daemon-reload"
    }

    Unregister-ScheduledTask -TaskName $taskName -Confirm:$false -ErrorAction SilentlyContinue
    Get-NetFirewallRule -Group $firewallGroup -ErrorAction SilentlyContinue | Remove-NetFirewallRule -ErrorAction SilentlyContinue

    if (Get-Command Get-NetFirewallHyperVRule -ErrorAction SilentlyContinue) {
        Get-NetFirewallHyperVRule -VMCreatorId $wslCreatorId -ErrorAction SilentlyContinue |
            Where-Object { $_.Name -like "NinjOS-Proxie-*" } |
            Remove-NetFirewallHyperVRule -ErrorAction SilentlyContinue
    }

    Remove-Item -LiteralPath (Join-Path $env:ProgramData "NinjOS-Proxie") -Recurse -Force -ErrorAction SilentlyContinue

    Write-Host "Windows integration removed. The WSL distribution was not unregistered." -ForegroundColor Green
    if (-not $RemoveData) {
        Write-Host "Proxy configuration and runtime data remain inside WSL." -ForegroundColor Yellow
    }
}
