param(
    [string]$Devctl = (Join-Path $PSScriptRoot '..\devctl.exe'),
    [switch]$OpenWorkspace,
    [switch]$Prompt
)

$ErrorActionPreference = 'Stop'
$devctlPath = [IO.Path]::GetFullPath($Devctl)
if (-not (Test-Path -LiteralPath $devctlPath -PathType Leaf)) { Write-Error "devctl executable was not found: $devctlPath"; exit 2 }
& $devctlPath session resume
if ($LASTEXITCODE -ne 0) { Write-Warning 'No usable session state was found. Use devctl-recovery.ps1.'; exit 0 }
$state = (& $devctlPath session status) | ConvertFrom-Json
$decision = 'continue'
if ($Prompt) {
    Add-Type -AssemblyName System.Windows.Forms
    Add-Type -AssemblyName System.Drawing
    $task = if ($state.current_task) { $state.current_task } else { 'saved development session' }
    $form = New-Object Windows.Forms.Form
    $form.Text = 'devctl daily resume'
    $form.StartPosition = 'CenterScreen'
    $form.Size = New-Object Drawing.Size(520, 230)
    $form.TopMost = $true
    $label = New-Object Windows.Forms.Label
    $label.Location = New-Object Drawing.Point(20, 20)
    $label.Size = New-Object Drawing.Size(465, 90)
    $label.Text = "Would you like to continue?`r`n`r`nProject: $($state.project)`r`nTask: $task`r`nLast result: $($state.last_result)"
    $form.Controls.Add($label)
    $continue = New-Object Windows.Forms.Button
    $continue.Text = 'Continue'
    $continue.Location = New-Object Drawing.Point(20, 130)
    $continue.Size = New-Object Drawing.Size(140, 35)
    $continue.Add_Click({ $script:decision = 'continue'; $form.Close() })
    $snooze = New-Object Windows.Forms.Button
    $snooze.Text = 'Snooze'
    $snooze.Location = New-Object Drawing.Point(180, 130)
    $snooze.Size = New-Object Drawing.Size(140, 35)
    $snooze.Add_Click({ $script:decision = 'snooze'; $form.Close() })
    $skip = New-Object Windows.Forms.Button
    $skip.Text = 'Skip today'
    $skip.Location = New-Object Drawing.Point(340, 130)
    $skip.Size = New-Object Drawing.Size(140, 35)
    $skip.Add_Click({ $script:decision = 'skip'; $form.Close() })
    $form.Controls.AddRange(@($continue, $snooze, $skip))
    [void]$form.ShowDialog()
    & $devctlPath session record --project $state.project --path $state.project_path --task $state.current_task --result $state.last_result --evidence $state.evidence_path --ci $state.ci_state --decision $decision --prompt-date (Get-Date -Format 'yyyy-MM-dd') | Out-Null
    if ($decision -ne 'continue') { Write-Output "Daily task decision: $decision"; exit 0 }
}
if ($OpenWorkspace -and $state.project_path -and (Test-Path -LiteralPath $state.project_path -PathType Container)) {
    $code = Get-Command code -ErrorAction SilentlyContinue
    if ($code) { Start-Process -WindowStyle Hidden -FilePath $code.Source -ArgumentList @('--reuse-window', $state.project_path) }
    else { Write-Warning 'VS Code command was not found; project was not opened.' }
} elseif ($OpenWorkspace) {
    Write-Warning 'Saved project path is missing; project was not opened.'
}
