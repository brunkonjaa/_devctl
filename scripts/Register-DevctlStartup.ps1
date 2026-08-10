param([string]$DevctlRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path)

$ErrorActionPreference = 'Stop'
$root = (Resolve-Path -LiteralPath $DevctlRoot -ErrorAction Stop).Path
$startup = Join-Path $root 'scripts\devctl-startup.ps1'
$devctl = Join-Path $root 'devctl.exe'
if (-not (Test-Path -LiteralPath $startup -PathType Leaf)) { throw "Startup script was not found: $startup" }
if (-not (Test-Path -LiteralPath $devctl -PathType Leaf)) { throw "Build devctl.exe before registering startup: $devctl" }

$taskName = 'devctl startup resume'
$action = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument "-NoProfile -ExecutionPolicy Bypass -File `"$startup`" -Devctl `"$devctl`" -OpenWorkspace"
$trigger = New-ScheduledTaskTrigger -AtLogOn -User "$env:USERDOMAIN\$env:USERNAME"
$principal = New-ScheduledTaskPrincipal -UserId "$env:USERDOMAIN\$env:USERNAME" -LogonType Interactive -RunLevel LeastPrivilege
Register-ScheduledTask -TaskName $taskName -Action $action -Trigger $trigger -Principal $principal -Description 'Resume the last configured devctl project at Windows logon.' -Force | Out-Null
Write-Output "Registered: $taskName"
