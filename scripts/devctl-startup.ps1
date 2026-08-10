param(
    [string]$Devctl = (Join-Path $PSScriptRoot '..\devctl.exe'),
    [string]$ProjectsRoot = 'C:\Projects',
    [switch]$OpenWorkspace,
    [switch]$Prompt
)

$ErrorActionPreference = 'Stop'
$devctlPath = [IO.Path]::GetFullPath($Devctl)
if (-not (Test-Path -LiteralPath $devctlPath -PathType Leaf)) { Write-Error "devctl executable was not found: $devctlPath"; exit 2 }
$sessionAvailable = $true
& $devctlPath session resume 2>$null | Out-Null
if ($LASTEXITCODE -ne 0) {
    $sessionAvailable = $false
    $latestProject = Get-ChildItem -LiteralPath $ProjectsRoot -Directory -Force -ErrorAction SilentlyContinue |
        Where-Object { Test-Path -LiteralPath (Join-Path $_.FullName '.git') } |
        Sort-Object LastWriteTime -Descending |
        Select-Object -First 1
    if (-not $latestProject) { Write-Warning "No saved session or Git project was found under $ProjectsRoot."; exit 0 }
    $state = [pscustomobject]@{
        project = $latestProject.Name
        project_path = $latestProject.FullName
        branch = ''
        last_commit = ''
        current_task = ''
        last_result = ''
        evidence_path = ''
        ci_state = ''
    }
} else {
    $state = (& $devctlPath session status) | ConvertFrom-Json
    if (-not (Test-Path -LiteralPath $state.project_path -PathType Container)) {
        Write-Warning "Saved project path is unavailable; using the latest Git project under $ProjectsRoot."
        $latestProject = Get-ChildItem -LiteralPath $ProjectsRoot -Directory -Force -ErrorAction SilentlyContinue |
            Where-Object { Test-Path -LiteralPath (Join-Path $_.FullName '.git') } |
            Sort-Object LastWriteTime -Descending |
            Select-Object -First 1
        if ($latestProject) {
            $state.project = $latestProject.Name
            $state.project_path = $latestProject.FullName
            $state.branch = ''
            $state.last_commit = ''
            $state.last_result = ''
            $state.evidence_path = ''
            $state.ci_state = ''
        }
    }
}
$decision = 'exit_windows'

function Get-ProjectOverview([string]$root, $savedState) {
    $projects = @(
        Get-ChildItem -LiteralPath $root -Directory -Force -ErrorAction SilentlyContinue |
            Where-Object { Test-Path -LiteralPath (Join-Path $_.FullName '.git') } |
            ForEach-Object {
                $path = $_.FullName
                $branch = ((& git -C $path branch --show-current 2>$null) -join '').Trim()
                $commitDateText = ((& git -C $path log -1 --format=%cI 2>$null) -join '').Trim()
                $commitDate = $_.LastWriteTime
                $parsedDate = [datetime]::MinValue
                if ([datetime]::TryParse($commitDateText, [Globalization.CultureInfo]::InvariantCulture, [Globalization.DateTimeStyles]::RoundtripKind, [ref]$parsedDate)) { $commitDate = $parsedDate.ToLocalTime() }
                $statusLines = @(& git -C $path status --short 2>$null)
                $notes = @(
                    Get-ChildItem -LiteralPath $path -File -Force -ErrorAction SilentlyContinue |
                        Where-Object { $_.Name -match '^(goal|plan|roadmap|todo).*\.md$' }
                    Get-ChildItem -LiteralPath (Join-Path $path 'docs') -File -Force -ErrorAction SilentlyContinue |
                        Where-Object { $_.Name -match '^(goal|plan|roadmap|todo).*\.md$' }
                )
                $nextLines = @()
                foreach ($note in $notes | Sort-Object LastWriteTime -Descending | Select-Object -First 2) {
                    $nextLines += ((Get-Content -LiteralPath $note.FullName -ErrorAction SilentlyContinue) | Where-Object { $_ -match '(?i)(next|todo|remaining|upcoming|plan|must|should)' } | Select-Object -First 4)
                }
                $activity = if ($_.LastWriteTime -gt $commitDate) { $_.LastWriteTime } else { $commitDate }
                [pscustomobject]@{ Name = $_.Name; Path = $path; Branch = $branch; Activity = $activity; Status = $statusLines; Notes = $notes; Next = $nextLines }
            }
    ) | Sort-Object Activity -Descending

    $lines = [System.Collections.Generic.List[string]]::new()
    [void]$lines.Add("PROJECT OVERVIEW (latest activity first)")
    [void]$lines.Add((Get-Date -Format 'yyyy-MM-dd HH:mm'))
    [void]$lines.Add('')
    foreach ($project in $projects) {
        $projectLines = [System.Collections.Generic.List[string]]::new()
        $marker = if ($savedState.project_path -and ([IO.Path]::GetFullPath($savedState.project_path) -eq [IO.Path]::GetFullPath($project.Path))) { ' [LATEST SAVED SESSION]' } else { '' }
        $status = if ($project.Status.Count -gt 0) { "worktree has $($project.Status.Count) changed item(s)" } else { 'worktree clean' }
        [void]$projectLines.Add("$($project.Name)$marker")
        [void]$projectLines.Add("Path: $($project.Path)")
        [void]$projectLines.Add("Latest activity: $($project.Activity.ToString('yyyy-MM-dd HH:mm')) | Branch: $($project.Branch) | $status")
        if ($project.Next.Count -gt 0) {
            [void]$projectLines.Add('Next according to available plan notes:')
            foreach ($line in $project.Next) { [void]$projectLines.Add("- $($line.Trim())") }
        } else {
            [void]$projectLines.Add('Next according to available plan notes: no matching plan note was found.')
        }
        if ($marker) {
            $taskText = if ($savedState.current_task) { $savedState.current_task } else { 'No current task recorded' }
            [void]$projectLines.Add("Saved task: $taskText | Last result: $($savedState.last_result)")
        }
        $projectWords = ($projectLines -join "`n") -split '\s+'
        if ($projectWords.Count -gt 1000) {
            $projectLines = [System.Collections.Generic.List[string]]::new(@($projectWords[0..949] -join ' ', '[Project report shortened to 1,000 words.]'))
        }
        foreach ($projectLine in $projectLines) { [void]$lines.Add($projectLine) }
        [void]$lines.Add('')
    }
    if ($projects.Count -eq 0) { [void]$lines.Add("No Git projects were found directly under $root.") }
    return ($lines -join "`n")
}

function Write-CodexHandoff($savedState, [string]$overviewText) {
    $handoffDirectory = Join-Path $env:APPDATA 'devctl'
    $handoffPath = Join-Path $handoffDirectory 'codex-handoff.md'
    New-Item -ItemType Directory -Path $handoffDirectory -Force | Out-Null
    $taskText = if ($savedState.current_task) { $savedState.current_task } else { 'No current task recorded' }
    $content = @(
        '# Codex task handoff'
        ''
        "- Project: $($savedState.project)"
        "- Project path: $($savedState.project_path)"
        "- Branch: $($savedState.branch)"
        "- Last commit: $($savedState.last_commit)"
        "- Current task: $taskText"
        "- Last devctl result: $($savedState.last_result)"
        ''
        '## Instructions'
        ''
        'Continue the current task from the repository state above. Inspect the repository instructions and plan before changing code. Do not claim completion without fresh deterministic verification.'
        ''
        '## Project overview'
        ''
        $overviewText
    )
    Set-Content -LiteralPath $handoffPath -Value ($content -join "`r`n") -Encoding UTF8
    return $handoffPath
}

if ($Prompt) {
    Add-Type -AssemblyName System.Windows.Forms
    Add-Type -AssemblyName System.Drawing
    $task = if ($state.current_task) { $state.current_task } else { 'No current task recorded' }
    $form = New-Object Windows.Forms.Form
    $form.Text = 'devctl daily task check'
    $form.StartPosition = 'CenterScreen'
    $form.Size = New-Object Drawing.Size(980, 720)
    $form.TopMost = $true
    $overview = New-Object Windows.Forms.TextBox
    $overview.Location = New-Object Drawing.Point(20, 20)
    $overview.Size = New-Object Drawing.Size(925, 565)
    $overview.Multiline = $true
    $overview.ReadOnly = $true
    $overview.ScrollBars = 'Vertical'
    $overview.Font = New-Object Drawing.Font('Consolas', 10)
    $overview.Text = Get-ProjectOverview -root $ProjectsRoot -savedState $state
    $form.Controls.Add($overview)
    $question = New-Object Windows.Forms.Label
    $question.Location = New-Object Drawing.Point(20, 595)
    $question.Size = New-Object Drawing.Size(925, 25)
    $question.Text = "Current saved task: $task`r`nWould you like to continue?"
    $form.Controls.Add($question)
    $continueTask = New-Object Windows.Forms.Button
    $continueTask.Text = 'Continue with current task'
    $continueTask.Location = New-Object Drawing.Point(20, 630)
    $continueTask.Size = New-Object Drawing.Size(175, 45)
    $continueTask.Add_Click({ $script:decision = 'continue_task'; $form.Close() })
    $overallCheck = New-Object Windows.Forms.Button
    $overallCheck.Text = 'Continue with overall check'
    $overallCheck.Location = New-Object Drawing.Point(210, 630)
    $overallCheck.Size = New-Object Drawing.Size(175, 45)
    $overallCheck.Add_Click({ $script:decision = 'continue_overall_check'; $form.Close() })
    $exitWindows = New-Object Windows.Forms.Button
    $exitWindows.Text = 'Exit this check and use Windows'
    $exitWindows.Location = New-Object Drawing.Point(400, 630)
    $exitWindows.Size = New-Object Drawing.Size(175, 45)
    $exitWindows.Add_Click({ $script:decision = 'exit_windows'; $form.Close() })
    $form.Controls.AddRange(@($continueTask, $overallCheck, $exitWindows))
    [void]$form.ShowDialog()
    if ($decision -eq 'continue_task' -and [string]::IsNullOrWhiteSpace($state.current_task)) {
        Add-Type -AssemblyName Microsoft.VisualBasic
        $enteredTask = [Microsoft.VisualBasic.Interaction]::InputBox('What task should be continued?', 'Current task required', '')
        if ([string]::IsNullOrWhiteSpace($enteredTask)) {
            $decision = 'exit_windows'
        } else {
            $state.current_task = $enteredTask.Trim()
        }
    }
    $recordArgs = @('session', 'record', '--project', $state.project, '--path', $state.project_path, '--result', $state.last_result, '--evidence', $state.evidence_path, '--ci', $state.ci_state, '--decision', $decision, '--prompt-date', (Get-Date -Format 'yyyy-MM-dd'))
    if ($state.current_task) { $recordArgs += @('--task', $state.current_task) }
    & $devctlPath @recordArgs | Out-Null
    if ($decision -eq 'exit_windows') { Write-Output 'Daily task decision: exit_windows'; exit 0 }
    if ($decision -eq 'continue_overall_check') {
        $verification = (& $devctlPath verify --json $state.project_path | ConvertFrom-Json)
        $result = if ($verification.overall) { $verification.overall } else { 'UNKNOWN' }
        [Windows.Forms.MessageBox]::Show("Overall check result: $result`r`n`r`nEvidence: $($verification.evidence_path)", 'devctl overall check', [Windows.Forms.MessageBoxButtons]::OK, [Windows.Forms.MessageBoxIcon]::Information) | Out-Null
    }
    if ($decision -eq 'continue_task') {
        $handoffPath = Write-CodexHandoff -savedState $state -overviewText $overview.Text
    }
}
if ($OpenWorkspace -and $state.project_path -and (Test-Path -LiteralPath $state.project_path -PathType Container)) {
    $code = Get-Command code -ErrorAction SilentlyContinue
    if ($code) {
        $codeArguments = @('--reuse-window', $state.project_path)
        if ($handoffPath -and (Test-Path -LiteralPath $handoffPath -PathType Leaf)) { $codeArguments += $handoffPath }
        Start-Process -WindowStyle Normal -FilePath $code.Source -ArgumentList $codeArguments
    }
    else { Write-Warning 'VS Code command was not found; project was not opened.' }
} elseif ($OpenWorkspace) {
    Write-Warning 'Saved project path is missing; project was not opened.'
}
