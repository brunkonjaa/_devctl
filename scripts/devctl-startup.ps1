param(
    [string]$Devctl = (Join-Path $PSScriptRoot '..\devctl.exe'),
    [switch]$OpenWorkspace
)

$ErrorActionPreference = 'Stop'
$devctlPath = [IO.Path]::GetFullPath($Devctl)
if (-not (Test-Path -LiteralPath $devctlPath -PathType Leaf)) { Write-Error "devctl executable was not found: $devctlPath"; exit 2 }
& $devctlPath session resume
if ($LASTEXITCODE -ne 0) { Write-Warning 'No usable session state was found. Use devctl-recovery.ps1.'; exit 0 }
if ($OpenWorkspace) {
    $state = (& $devctlPath session status) | ConvertFrom-Json
    if ($state.project_path -and (Test-Path -LiteralPath $state.project_path -PathType Container)) {
        $code = Get-Command code -ErrorAction SilentlyContinue
        if ($code) { Start-Process -WindowStyle Hidden -FilePath $code.Source -ArgumentList @('--reuse-window', $state.project_path) }
        else { Write-Warning 'VS Code command was not found; project was not opened.' }
    } else { Write-Warning 'Saved project path is missing; project was not opened.' }
}
