#requires -Version 5.1

[CmdletBinding()]
param(
    [string]$Distro = "Ubuntu",
    [string]$RuntimeArchive = "",
    [int]$DashboardPort = 25571,
    [int[]]$PublicUdpPorts = @(25566, 25571),
    [int]$TransferPortStart = 25572,
    [int]$TransferPortEnd = 25581,
    [switch]$SkipFirewall,
    [switch]$SkipStartupTask
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version 2.0

$ProductVersion = "7.3.14"
$WslCreatorId = "{40E0AC32-46A5-438A-A0B2-2B479E8F2E90}"
$FirewallGroup = "Ninj-OS Proxie"
$StartupTaskName = "Ninj-OS Proxie WSL Startup"

function Write-Step {
    param([string]$Message)
    Write-Host "[Ninj-OS Windows Installer] $Message" -ForegroundColor Cyan
}

function Write-WarningLine {
    param([string]$Message)
    Write-Host "[Ninj-OS Windows Installer] WARNING: $Message" -ForegroundColor Yellow
}

function Test-Administrator {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($identity)
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Invoke-WslRoot {
    param([string]$Command)
    & wsl.exe -d $Distro -u root -- bash -lc $Command
    if ($LASTEXITCODE -ne 0) {
        throw "WSL command failed with exit code ${LASTEXITCODE}: $Command"
    }
}

function Get-InstalledDistros {
    $output = @(& wsl.exe -l -q 2>$null)
    return @($output | ForEach-Object { ($_ -replace "`0", "").Trim() } | Where-Object { $_ })
}

function Set-WslMirroredConfiguration {
    $path = Join-Path $env:USERPROFILE ".wslconfig"
    $desired = @(
        "networkingMode=mirrored",
        "firewall=true",
        "dnsTunneling=true",
        "autoProxy=true"
    )

    $lines = @()
    if (Test-Path $path) {
        $lines = @(Get-Content -LiteralPath $path)
    }

    $output = New-Object System.Collections.Generic.List[string]
    $insideWsl2 = $false
    $foundWsl2 = $false

    foreach ($line in $lines) {
        if ($line -match '^\s*\[([^]]+)\]\s*$') {
            $insideWsl2 = ($Matches[1] -ieq "wsl2")
            $output.Add($line)
            if ($insideWsl2) {
                $foundWsl2 = $true
                foreach ($setting in $desired) {
                    $output.Add($setting)
                }
            }
            continue
        }

        if ($insideWsl2 -and $line -match '^\s*(networkingMode|firewall|dnsTunneling|autoProxy)\s*=') {
            continue
        }

        $output.Add($line)
    }

    if (-not $foundWsl2) {
        if ($output.Count -gt 0 -and $output[$output.Count - 1] -ne "") {
            $output.Add("")
        }
        $output.Add("[wsl2]")
        foreach ($setting in $desired) {
            $output.Add($setting)
        }
    }

    Set-Content -LiteralPath $path -Value $output -Encoding ASCII
    Write-Step "Configured WSL2 mirrored networking in $path."
}

function Enable-WslSystemd {
    $command = @'
set -e
file=/etc/wsl.conf
[ -e "$file" ] || touch "$file"
tmp="$(mktemp)"
awk '
BEGIN { inboot=0; bootseen=0; written=0 }
/^\[[^]]+\][[:space:]]*$/ {
    if (inboot && !written) { print "systemd=true"; written=1 }
    inboot = ($0 ~ /^\[boot\][[:space:]]*$/)
    if (inboot) bootseen=1
    print
    next
}
{
    if (inboot && $0 ~ /^[[:space:]]*systemd[[:space:]]*=/) {
        if (!written) { print "systemd=true"; written=1 }
        next
    }
    print
}
END {
    if (inboot && !written) print "systemd=true"
    if (!bootseen) { print ""; print "[boot]"; print "systemd=true" }
}' "$file" > "$tmp"
install -m 0644 "$tmp" "$file"
rm -f "$tmp"
'@
    Invoke-WslRoot $command
}

function Install-WindowsFirewallRules {
    Get-NetFirewallRule -Group $FirewallGroup -ErrorAction SilentlyContinue | Remove-NetFirewallRule -ErrorAction SilentlyContinue

    New-NetFirewallRule `
        -Name "NinjOS-Proxie-Dashboard-TCP" `
        -DisplayName "Ninj-OS Proxie Dashboard TCP $DashboardPort" `
        -Group $FirewallGroup `
        -Direction Inbound `
        -Action Allow `
        -Protocol TCP `
        -LocalPort $DashboardPort | Out-Null

    foreach ($port in ($PublicUdpPorts | Sort-Object -Unique)) {
        New-NetFirewallRule `
            -Name "NinjOS-Proxie-Route-UDP-$port" `
            -DisplayName "Ninj-OS Proxie Route UDP $port" `
            -Group $FirewallGroup `
            -Direction Inbound `
            -Action Allow `
            -Protocol UDP `
            -LocalPort $port | Out-Null
    }

    if ($TransferPortEnd -ge $TransferPortStart) {
        New-NetFirewallRule `
            -Name "NinjOS-Proxie-Transfer-UDP" `
            -DisplayName "Ninj-OS Proxie Transfer UDP $TransferPortStart-$TransferPortEnd" `
            -Group $FirewallGroup `
            -Direction Inbound `
            -Action Allow `
            -Protocol UDP `
            -LocalPort "$TransferPortStart-$TransferPortEnd" | Out-Null
    }

    Write-Step "Installed Windows Defender Firewall rules."
}

function Install-HyperVFirewallRules {
    if (-not (Get-Command New-NetFirewallHyperVRule -ErrorAction SilentlyContinue)) {
        Write-WarningLine "Hyper-V firewall cmdlets are unavailable. Update Windows and WSL, then review docs/WINDOWS_INSTALL.md."
        return
    }

    Get-NetFirewallHyperVRule -VMCreatorId $WslCreatorId -ErrorAction SilentlyContinue |
        Where-Object { $_.Name -like "NinjOS-Proxie-*" } |
        Remove-NetFirewallHyperVRule -ErrorAction SilentlyContinue

    New-NetFirewallHyperVRule `
        -Name "NinjOS-Proxie-Dashboard-TCP" `
        -DisplayName "Ninj-OS Proxie Dashboard TCP $DashboardPort" `
        -Direction Inbound `
        -VMCreatorId $WslCreatorId `
        -Protocol TCP `
        -LocalPorts $DashboardPort | Out-Null

    foreach ($port in ($PublicUdpPorts | Sort-Object -Unique)) {
        New-NetFirewallHyperVRule `
            -Name "NinjOS-Proxie-Route-UDP-$port" `
            -DisplayName "Ninj-OS Proxie Route UDP $port" `
            -Direction Inbound `
            -VMCreatorId $WslCreatorId `
            -Protocol UDP `
            -LocalPorts $port | Out-Null
    }

    if ($TransferPortEnd -ge $TransferPortStart) {
        New-NetFirewallHyperVRule `
            -Name "NinjOS-Proxie-Transfer-UDP" `
            -DisplayName "Ninj-OS Proxie Transfer UDP $TransferPortStart-$TransferPortEnd" `
            -Direction Inbound `
            -VMCreatorId $WslCreatorId `
            -Protocol UDP `
            -LocalPorts "$TransferPortStart-$TransferPortEnd" | Out-Null
    }

    Write-Step "Installed Hyper-V firewall rules for WSL."
}

function Install-StartupTask {
    $taskDirectory = Join-Path $env:ProgramData "NinjOS-Proxie"
    $commandFile = Join-Path $taskDirectory "start-wsl-service.cmd"
    New-Item -ItemType Directory -Path $taskDirectory -Force | Out-Null

    $commandText = "@echo off`r`nwsl.exe -d `"$Distro`" -u root -- /bin/bash -lc `"systemctl start ninjos-proxie.service`"`r`n"
    Set-Content -LiteralPath $commandFile -Value $commandText -Encoding ASCII

    $currentUser = [Security.Principal.WindowsIdentity]::GetCurrent().Name
    $action = New-ScheduledTaskAction -Execute "$env:SystemRoot\System32\cmd.exe" -Argument "/c `"$commandFile`""
    $trigger = New-ScheduledTaskTrigger -AtLogOn -User $currentUser
    $principal = New-ScheduledTaskPrincipal -UserId $currentUser -LogonType Interactive -RunLevel Highest
    $settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -ExecutionTimeLimit (New-TimeSpan -Minutes 5)

    Register-ScheduledTask `
        -TaskName $StartupTaskName `
        -Action $action `
        -Trigger $trigger `
        -Principal $principal `
        -Settings $settings `
        -Description "Starts Ninj-OS Proxie inside WSL2 when the installing user signs in." `
        -Force | Out-Null

    Write-Step "Installed scheduled task '$StartupTaskName'."
}

if (-not (Test-Administrator)) {
    throw "Run PowerShell as Administrator."
}

$windowsBuild = [Environment]::OSVersion.Version.Build
if ($windowsBuild -lt 22621) {
    throw "Public UDP hosting requires Windows 11 22H2 or newer. Detected build $windowsBuild."
}

if (-not (Get-Command wsl.exe -ErrorAction SilentlyContinue)) {
    throw "wsl.exe is unavailable. Install current Windows updates and rerun this installer."
}

Write-Step "Preparing Ninj-OS Proxie Edge Fabric v$ProductVersion for Windows WSL2."

$distros = Get-InstalledDistros
if ($distros -notcontains $Distro) {
    Write-Step "Installing WSL2 distribution '$Distro'."
    & wsl.exe --install -d $Distro
    Write-Host ""
    Write-Host "WSL installation was requested. Restart Windows if prompted, complete the Linux distribution initialization, and run this installer again." -ForegroundColor Yellow
    exit 3010
}

& wsl.exe --set-default-version 2 | Out-Null
& wsl.exe --set-version $Distro 2 | Out-Null
if ($LASTEXITCODE -ne 0) {
    throw "Unable to set '$Distro' to WSL2."
}

Set-WslMirroredConfiguration
Enable-WslSystemd
& wsl.exe --shutdown
Start-Sleep -Seconds 2

& wsl.exe -d $Distro -u root -- bash -lc "systemctl is-system-running --wait >/dev/null 2>&1 || true"
if ($LASTEXITCODE -ne 0) {
    Write-WarningLine "WSL started, but systemd readiness could not be confirmed immediately."
}

if ([string]::IsNullOrWhiteSpace($RuntimeArchive)) {
    $RuntimeArchive = Join-Path $PSScriptRoot "NinjOS-Proxie-Edge-Fabric-v$ProductVersion-Runtime.tar.gz"
}

$linuxInstaller = Join-Path $PSScriptRoot "install-standalone.sh"
if (-not (Test-Path -LiteralPath $RuntimeArchive)) {
    throw "Runtime archive not found: $RuntimeArchive"
}
if (-not (Test-Path -LiteralPath $linuxInstaller)) {
    throw "Linux installer not found beside this script: $linuxInstaller"
}

$runtimeResolved = (Resolve-Path -LiteralPath $RuntimeArchive).Path
$installerResolved = (Resolve-Path -LiteralPath $linuxInstaller).Path
$runtimeWsl = (& wsl.exe -d $Distro -- wslpath -a $runtimeResolved).Trim()
$installerWsl = (& wsl.exe -d $Distro -- wslpath -a $installerResolved).Trim()

if ([string]::IsNullOrWhiteSpace($runtimeWsl) -or [string]::IsNullOrWhiteSpace($installerWsl)) {
    throw "Unable to translate deployment paths into WSL paths."
}

Write-Step "Installing the runtime inside WSL2."
& wsl.exe -d $Distro -u root -- bash $installerWsl $runtimeWsl
if ($LASTEXITCODE -ne 0) {
    throw "The Linux runtime installer failed with exit code $LASTEXITCODE."
}

if (-not $SkipFirewall) {
    Install-WindowsFirewallRules
    Install-HyperVFirewallRules
}

if (-not $SkipStartupTask) {
    Install-StartupTask
}

Invoke-WslRoot "systemctl restart ninjos-proxie.service"
Start-Sleep -Seconds 2

Write-Host ""
Write-Host "Ninj-OS Proxie Edge Fabric v$ProductVersion is installed." -ForegroundColor Green
Write-Host "Dashboard: http://localhost:$DashboardPort" -ForegroundColor Green
Write-Host ""

& wsl.exe -d $Distro -u root -- bash -lc "if [ -s /opt/ninjos-proxie/runtime/FIRST_RUN_SETUP.txt ]; then cat /opt/ninjos-proxie/runtime/FIRST_RUN_SETUP.txt; else echo 'Owner setup is already complete.'; fi"

Write-Host ""
Write-Host "Use .\manage-windows.ps1 status to check the service." -ForegroundColor Cyan
Write-Host "Review docs\WINDOWS_INSTALL.md before exposing the dashboard publicly." -ForegroundColor Cyan
