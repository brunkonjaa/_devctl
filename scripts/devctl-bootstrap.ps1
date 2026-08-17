param(
    [string]$DevctlRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path,
    [string]$InstallRoot = (Join-Path $env:LOCALAPPDATA 'devctl\bin'),
    [switch]$Startup,
    [Parameter(ValueFromRemainingArguments=$true)][string[]]$Arguments
)

$ErrorActionPreference = 'Stop'
$root = (Resolve-Path -LiteralPath $DevctlRoot -ErrorAction Stop).Path
$git = Get-Command git -ErrorAction SilentlyContinue
$go = Get-Command go -ErrorAction SilentlyContinue
if (-not $git) { throw 'git is required to verify devctl source provenance.' }
if (-not $go) { throw 'go is required to rebuild devctl.' }

$head = (& $git.Source -C $root rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($head)) { throw "Could not read devctl source HEAD: $root" }
$sourceDirty = -not [string]::IsNullOrWhiteSpace((& $git.Source -C $root status --porcelain))

New-Item -ItemType Directory -Path $InstallRoot -Force | Out-Null
$binary = Join-Path $InstallRoot 'devctl.exe'
$needsBuild = -not (Test-Path -LiteralPath $binary -PathType Leaf)
if (-not $needsBuild) {
    try {
        $provenance = (& $binary version --json | ConvertFrom-Json)
        $needsBuild = ([string]$provenance.commit -ne $head) -or ([bool]$provenance.dirty -ne $sourceDirty)
    } catch {
        $needsBuild = $true
    }
}

if ($needsBuild) {
    $temporary = Join-Path $InstallRoot ("devctl-$PID.tmp.exe")
    Remove-Item -LiteralPath $temporary -Force -ErrorAction SilentlyContinue
    & $go.Source -C $root build -buildvcs=true -ldflags "-X devctl/internal/version.Commit=$head" -o $temporary ./cmd/devctl
    if ($LASTEXITCODE -ne 0) { throw "devctl rebuild failed with exit code $LASTEXITCODE" }
    $rebuilt = (& $temporary version --json | ConvertFrom-Json)
    if ([string]$rebuilt.commit -ne $head -or [bool]$rebuilt.dirty -ne $sourceDirty) {
        Remove-Item -LiteralPath $temporary -Force -ErrorAction SilentlyContinue
        throw 'rebuilt devctl failed its provenance check.'
    }
    Move-Item -LiteralPath $temporary -Destination $binary -Force
}

if ($Startup) {
    $startup = Join-Path $root 'scripts\devctl-startup.ps1'
    & powershell.exe -NoProfile -ExecutionPolicy Bypass -File $startup -Devctl $binary -OpenWorkspace -Prompt
    exit $LASTEXITCODE
}

if ($Arguments) {
    & $binary @Arguments
    exit $LASTEXITCODE
}

& $binary version
exit $LASTEXITCODE
