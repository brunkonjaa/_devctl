param([Parameter(Mandatory=$true)][string]$ProjectPath,[string]$Project='',[string]$Task='')
$ErrorActionPreference = 'Stop'
$resolved = Resolve-Path -LiteralPath $ProjectPath -ErrorAction Stop
$devctl = Join-Path $PSScriptRoot '..\devctl.exe'
if (-not (Test-Path -LiteralPath $devctl -PathType Leaf)) { throw "devctl executable was not found: $devctl" }
& $devctl session record --project $Project --path $resolved.Path --task $Task
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
& $devctl session resume
