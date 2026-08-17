param([string]$DevctlRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path)

$ErrorActionPreference = 'Stop'
$root = (Resolve-Path -LiteralPath $DevctlRoot -ErrorAction Stop).Path
$startup = Join-Path $root 'scripts\devctl-startup.ps1'
$bootstrap = Join-Path $root 'scripts\devctl-bootstrap.ps1'
if (-not (Test-Path -LiteralPath $startup -PathType Leaf)) { throw "Startup script was not found: $startup" }
if (-not (Test-Path -LiteralPath $bootstrap -PathType Leaf)) { throw "Bootstrap script was not found: $bootstrap" }

$taskName = 'devctl startup resume'
$action = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument "-NoProfile -ExecutionPolicy Bypass -File `"$bootstrap`" -DevctlRoot `"$root`" -Startup"
$user = "$env:USERDOMAIN\$env:USERNAME"
$triggers = @(
    (New-ScheduledTaskTrigger -AtLogOn -User $user),
    (New-ScheduledTaskTrigger -Daily -At 9:00AM)
)
$principal = New-ScheduledTaskPrincipal -UserId $user -LogonType Interactive -RunLevel Limited
Register-ScheduledTask -TaskName $taskName -Action $action -Trigger $triggers -Principal $principal -Description 'Prompt for the saved devctl task at logon and daily at 9:00 AM.' -Force | Out-Null
Write-Output "Registered: $taskName"
