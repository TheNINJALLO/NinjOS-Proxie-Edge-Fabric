#requires -Version 5.1

[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [ValidateSet("start", "stop", "restart", "status", "logs", "setup-code", "reset-setup")]
    [string]$Action = "status",
    [string]$Distro = "Ubuntu"
)

$ErrorActionPreference = "Stop"

function Invoke-WslRoot {
    param([string]$Command)
    & wsl.exe -d $Distro -u root -- bash -lc $Command
    if ($LASTEXITCODE -ne 0) {
        throw "WSL command failed with exit code $LASTEXITCODE."
    }
}

switch ($Action) {
    "start" {
        Invoke-WslRoot "systemctl start ninjos-proxie.service"
        Invoke-WslRoot "systemctl --no-pager --full status ninjos-proxie.service"
    }
    "stop" {
        Invoke-WslRoot "systemctl stop ninjos-proxie.service"
    }
    "restart" {
        Invoke-WslRoot "systemctl restart ninjos-proxie.service"
        Invoke-WslRoot "systemctl --no-pager --full status ninjos-proxie.service"
    }
    "status" {
        Invoke-WslRoot "systemctl --no-pager --full status ninjos-proxie.service"
    }
    "logs" {
        & wsl.exe -d $Distro -u root -- journalctl -u ninjos-proxie.service -f
    }
    "setup-code" {
        Invoke-WslRoot "cat /opt/ninjos-proxie/runtime/FIRST_RUN_SETUP.txt"
    }
    "reset-setup" {
        Invoke-WslRoot "/opt/ninjos-proxie/ninjos-dashboard-account.sh reset-setup"
        Invoke-WslRoot "systemctl restart ninjos-proxie.service"
        Invoke-WslRoot "cat /opt/ninjos-proxie/runtime/FIRST_RUN_SETUP.txt"
    }
}
