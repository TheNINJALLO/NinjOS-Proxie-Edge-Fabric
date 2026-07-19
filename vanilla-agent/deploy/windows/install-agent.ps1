param([string]$InstallDir = "C:\NinjOS\VanillaAgent", [string]$Config = "C:\NinjOS\VanillaAgent\agent.json")
$ErrorActionPreference = "Stop"
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
Write-Host "Copy ninjos-vanilla-agent.exe and agent.json to $InstallDir."
Write-Host "Create a scheduled task or use your preferred Windows service wrapper with:"
Write-Host "  $InstallDir\ninjos-vanilla-agent.exe --config $Config"
