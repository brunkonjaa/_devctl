param([Parameter(Mandatory=$true)][string]$ProjectPath,[string]$Project='',[string]$Task='')
$ErrorActionPreference = 'Stop'
$resolved = Resolve-Path -LiteralPath $ProjectPath -ErrorAction Stop
$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$bootstrap = Join-Path $PSScriptRoot 'devctl-bootstrap.ps1'
if (-not (Test-Path -LiteralPath $bootstrap -PathType Leaf)) { throw "devctl bootstrapper was not found: $bootstrap" }
& $bootstrap -DevctlRoot $root session record --project $Project --path $resolved.Path --task $Task
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
& $bootstrap -DevctlRoot $root session resume
