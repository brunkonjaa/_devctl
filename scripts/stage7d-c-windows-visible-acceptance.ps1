[CmdletBinding()]
param(
    [ValidateSet('Prepare', 'Run', 'Capture', 'Audit')]
    [string]$Action = 'Prepare',
    [string]$EvidenceRoot,
    [string]$CasePath,
    [int]$ExitCode = 125
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$scriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = Split-Path -Parent $scriptRoot

function Write-Utf8NoBom([string]$Path, [string]$Text) {
    [IO.File]::WriteAllText($Path, $Text, [Text.UTF8Encoding]::new($false))
}

function Invoke-Git([string[]]$Arguments, [string]$WorkingDirectory) {
    & git -C $WorkingDirectory @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "git command failed with exit code ${LASTEXITCODE}: git $($Arguments -join ' ')"
    }
}

function Get-ProjectHashes([string]$ProjectPath) {
    $projectFull = (Resolve-Path -LiteralPath $ProjectPath).Path.TrimEnd('\') + '\'
    $items = @(
        Get-ChildItem -LiteralPath $ProjectPath -File -Recurse |
            Where-Object {
                $relative = $_.FullName.Substring($projectFull.Length)
                -not $relative.StartsWith('.git' + [IO.Path]::DirectorySeparatorChar) -and
                $relative -ne '.git' -and
                -not $relative.StartsWith('.devctl' + [IO.Path]::DirectorySeparatorChar) -and
                $relative -ne '.devctl'
            } |
            ForEach-Object {
                $relative = $_.FullName.Substring($projectFull.Length).Replace('\', '/')
                $hash = (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
                [ordered]@{
                    path = $relative
                    sha256 = $hash
                    bytes = $_.Length
                }
            }
    )
    @($items | Sort-Object path)
}

function Save-Snapshot([string]$ProjectPath, [string]$Destination) {
    New-Item -ItemType Directory -Force -Path $Destination | Out-Null
    Copy-Item -LiteralPath (Join-Path $ProjectPath 'calculator.go') -Destination (Join-Path $Destination 'calculator.go')
    $hashes = Get-ProjectHashes $ProjectPath
    Write-Utf8NoBom (Join-Path $Destination 'hashes.json') (($hashes | ConvertTo-Json -Depth 5) + "`n")
    Push-Location $ProjectPath
    try {
        $gitStatus = (@(& git status --short) -join "`n")
        $gitDiffStat = (@(& git diff --stat) -join "`n")
        Write-Utf8NoBom (Join-Path $Destination 'git-status.txt') ($gitStatus + "`n")
        Write-Utf8NoBom (Join-Path $Destination 'git-diff-stat.txt') ($gitDiffStat + "`n")
    }
    finally {
        Pop-Location
    }
}

function Get-CalculatorHash([string]$ProjectPath) {
    (Get-FileHash -LiteralPath (Join-Path $ProjectPath 'calculator.go') -Algorithm SHA256).Hash.ToLowerInvariant()
}

function Get-GitStatusText([string]$ProjectPath) {
    (@(& git -C $ProjectPath status --porcelain) -join "`n").Trim()
}

function Invoke-VerifyPreflight([object]$Metadata, [string]$CasePath) {
    $stdoutPath = Join-Path $CasePath 'preflight-stdout.json'
    $stderrPath = Join-Path $CasePath 'preflight-stderr.txt'
    $psi = [Diagnostics.ProcessStartInfo]::new()
    $psi.FileName = $Metadata.exe
    $psi.Arguments = 'verify --json "' + $Metadata.project + '"'
    $psi.WorkingDirectory = $Metadata.project
    $psi.UseShellExecute = $false
    $psi.CreateNoWindow = $true
    $psi.RedirectStandardInput = $false
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $process = [Diagnostics.Process]::new()
    $process.StartInfo = $psi
    [void]$process.Start()
    $processId = $process.Id
    $stdout = $process.StandardOutput.ReadToEnd()
    $stderr = $process.StandardError.ReadToEnd()
    $process.WaitForExit()
    $code = $process.ExitCode
    Write-Utf8NoBom $stdoutPath $stdout
    Write-Utf8NoBom $stderrPath $stderr
    $json = $null
    $jsonError = $null
    try { $json = $stdout | ConvertFrom-Json } catch { $jsonError = $_.Exception.Message }
    $statusCandidates = @()
    if ($null -ne $json) {
        if ($json.PSObject.Properties.Name -contains 'overall') { $statusCandidates += $json.overall }
        if ($json.PSObject.Properties.Name -contains 'status') { $statusCandidates += $json.status }
        if ($json.PSObject.Properties.Name -contains 'result' -and $null -ne $json.result) {
            if ($json.result.PSObject.Properties.Name -contains 'overall') { $statusCandidates += $json.result.overall }
            if ($json.result.PSObject.Properties.Name -contains 'status') { $statusCandidates += $json.result.status }
        }
    }
    $statusCandidates = @($statusCandidates | Where-Object { $_ })
    $status = if ($statusCandidates.Count -gt 0) { [string]$statusCandidates[0] } else { '' }
    $record = [ordered]@{
        process = [ordered]@{ executable = $Metadata.exe; pid = $processId; arguments = $psi.Arguments }
        exit_code = $code
        status = $status
        json_valid = ($null -ne $json)
        json_error = $jsonError
        calculator_sha256 = Get-CalculatorHash $Metadata.project
        git_head = (& git -C $Metadata.project rev-parse HEAD).Trim()
        git_status = Get-GitStatusText $Metadata.project
    }
    Write-Utf8NoBom (Join-Path $CasePath 'preflight-record.json') (($record | ConvertTo-Json -Depth 8) + "`n")
    [pscustomobject]$record
}

function Assert-CaseReady([object]$Metadata, [string]$CasePath) {
    if (Test-Path -LiteralPath (Join-Path $CasePath 'execution-started.json')) {
        throw "Case has already been attempted and must be regenerated: $CasePath"
    }
    $before = Get-Content -Raw (Join-Path $CasePath 'before\hashes.json') | ConvertFrom-Json
    $expectedCalculator = ($before | Where-Object path -eq 'calculator.go').sha256
    $head = (& git -C $Metadata.project rev-parse HEAD).Trim()
    $statusBefore = Get-GitStatusText $Metadata.project
    $hashBefore = Get-CalculatorHash $Metadata.project
    $preflight = Invoke-VerifyPreflight $Metadata $CasePath
    $headAfter = (& git -C $Metadata.project rev-parse HEAD).Trim()
    $statusAfter = Get-GitStatusText $Metadata.project
    $hashAfter = Get-CalculatorHash $Metadata.project
    $record = [ordered]@{
        expected_head = $Metadata.baseline_head
        head_before = $head
        head_after = $headAfter
        expected_calculator_sha256 = $expectedCalculator
        calculator_sha256_before = $hashBefore
        calculator_sha256_after = $hashAfter
        git_status_before = $statusBefore
        git_status_after = $statusAfter
        baseline_verification = $preflight.status
        preflight_process = $preflight.process
        ready = ($head -eq $Metadata.baseline_head -and $headAfter -eq $Metadata.baseline_head -and [string]::IsNullOrEmpty($statusBefore) -and [string]::IsNullOrEmpty($statusAfter) -and $hashBefore -eq $expectedCalculator -and $hashAfter -eq $expectedCalculator -and $preflight.status -eq 'FAIL')
    }
    Write-Utf8NoBom (Join-Path $CasePath 'ready-check.json') (($record | ConvertTo-Json -Depth 8) + "`n")
    if (-not $record.ready) {
        Write-Utf8NoBom (Join-Path $CasePath 'harness-violation.json') (($record | ConvertTo-Json -Depth 8) + "`n")
        throw "Case preflight failed; evidence preserved: $CasePath"
    }
}

function Capture-Case([string]$Path, [int]$Code) {
    $project = Join-Path $Path 'project'
    $after = Join-Path $Path 'after'
    Save-Snapshot $project $after
    Write-Utf8NoBom (Join-Path $Path 'exit-code.txt') ("$Code`n")
    $beforeHash = Get-Content -Raw (Join-Path $Path 'before\hashes.json') | ConvertFrom-Json
    $afterHash = Get-Content -Raw (Join-Path $Path 'after\hashes.json') | ConvertFrom-Json
    $beforeCalculator = Get-Content -Raw (Join-Path $Path 'before\calculator.go')
    $afterCalculator = Get-Content -Raw (Join-Path $Path 'after\calculator.go')
    $record = [ordered]@{
        captured_at = (Get-Date).ToUniversalTime().ToString('o')
        exit_code = $Code
        calculator_unchanged = ($beforeCalculator -ceq $afterCalculator)
        hashes_unchanged = (($beforeHash | ConvertTo-Json -Depth 5) -ceq ($afterHash | ConvertTo-Json -Depth 5))
        before_hashes = $beforeHash
        after_hashes = $afterHash
    }
    Write-Utf8NoBom (Join-Path $Path 'capture.json') (($record | ConvertTo-Json -Depth 8) + "`n")
}

function Finalize-Case([string]$Path, [int]$Code) {
    $record = [ordered]@{
        finalized_at = (Get-Date).ToUniversalTime().ToString('o')
        exit_code = $Code
        success = $false
        error = $null
    }
    try {
        Capture-Case $Path $Code
        $record.success = $true
    }
    catch {
        $record.error = $_.Exception.Message
    }
    Write-Utf8NoBom (Join-Path $Path 'finalization-record.json') (($record | ConvertTo-Json -Depth 8) + "`n")
    if (-not $record.success) {
        throw "Acceptance evidence finalization failed; see finalization-record.json: $Path"
    }
}

function Quote-RunnerArgument([string]$Value) {
    '"' + $Value.Replace('"', '\"') + '"'
}

function Run-VisibleCase([string]$Path) {
    $metadata = Get-Content -Raw (Join-Path $Path 'case.json') | ConvertFrom-Json
    Assert-CaseReady $metadata $Path
    $runnerRecordPath = Join-Path $Path 'runner-record.json'
    $runnerArguments = '--stdout ' + (Quote-RunnerArgument $metadata.stdout) +
        ' --stderr ' + (Quote-RunnerArgument $metadata.stderr) +
        ' --run-record ' + (Quote-RunnerArgument $runnerRecordPath) +
        ' -- ' + (Quote-RunnerArgument $metadata.exe) +
        ' repair --json --proposal ' + (Quote-RunnerArgument $metadata.proposal) +
        ' --allow calculator.go ' + (Quote-RunnerArgument $metadata.project)
    $startedRecord = [ordered]@{
        started_at = (Get-Date).ToUniversalTime().ToString('o')
        process = [ordered]@{ executable = $metadata.exe; pid = $null; arguments = $metadata.command }
        runner = [ordered]@{ executable = $metadata.runner; pid = $null; arguments = $runnerArguments }
        child_pid = $null
        approval_input_injected_by_harness = $false
    }
    Write-Utf8NoBom (Join-Path $Path 'execution-started.json') (($startedRecord | ConvertTo-Json -Depth 8) + "`n")
    Write-Host 'The real _devctl repair CLI is attached to this visible terminal.'
    Write-Host 'The acceptance harness will not type or redirect the approval interaction.'

    $process = $null
    $code = 125
    $started = $false
    $applicationPhaseSeen = $false
    $mutationObserved = $false
    $violation = $null
    $childPid = $null
    $finalizationError = $null
    $expectedHash = ((Get-Content -Raw (Join-Path $Path 'before\hashes.json') | ConvertFrom-Json) | Where-Object path -eq 'calculator.go').sha256
    try {
        # Start the native sidecar as an independent foreground-console child.
        # PowerShell Ctrl+C must not tear down the sidecar before it records the
        # actual _devctl child result.
        $process = Start-Process -FilePath $metadata.runner -ArgumentList $runnerArguments -WorkingDirectory $metadata.project -NoNewWindow -PassThru
        $started = $true
        $startedRecord.runner.pid = $process.Id
        Write-Utf8NoBom (Join-Path $Path 'execution-started.json') (($startedRecord | ConvertTo-Json -Depth 8) + "`n")
        while (-not $process.HasExited) {
            Start-Sleep -Milliseconds 50
            $runnerRecordPath = Join-Path $Path 'runner-record.json'
            if ((Test-Path -LiteralPath $runnerRecordPath) -and $null -eq $childPid) {
                try {
                    $runnerRecord = Get-Content -Raw $runnerRecordPath | ConvertFrom-Json
                    if ($runnerRecord.PSObject.Properties.Name -contains 'child_pid') {
                        $childPid = [int]$runnerRecord.child_pid
                        $startedRecord.child_pid = $childPid
                        Write-Utf8NoBom (Join-Path $Path 'execution-started.json') (($startedRecord | ConvertTo-Json -Depth 8) + "`n")
                    }
                }
                catch {
                    # The native sidecar may be replacing the record atomically.
                }
            }
            $stderrText = if (Test-Path -LiteralPath $metadata.stderr) { Get-Content -Raw $metadata.stderr } else { '' }
            if ($stderrText -match 'Applying the fix|Applying fix|PATCH_APPLIED|Checking changed files|Testing the project again') {
                $applicationPhaseSeen = $true
            }
            $currentHash = Get-CalculatorHash $metadata.project
            if ($currentHash -ne $expectedHash) {
                $mutationObserved = $true
                if (-not $applicationPhaseSeen) {
                    $violation = [ordered]@{
                        violation = 'calculator.go changed before an application phase was observed'
                        mutation_process = [ordered]@{ executable = $metadata.exe; pid = $childPid; arguments = $metadata.command }
                        observed_at = (Get-Date).ToUniversalTime().ToString('o')
                        expected_sha256 = $expectedHash
                        observed_sha256 = $currentHash
                        stderr_observed = $stderrText
                    }
                    Write-Utf8NoBom (Join-Path $Path 'harness-violation.json') (($violation | ConvertTo-Json -Depth 8) + "`n")
                    if ($null -ne $childPid) {
                        Stop-Process -Id $childPid -Force -ErrorAction SilentlyContinue
                    }
                    break
                }
            }
        }
        if ($started) { $process.WaitForExit() }
        if (Test-Path -LiteralPath $runnerRecordPath) {
            try {
                $runnerRecord = Get-Content -Raw $runnerRecordPath | ConvertFrom-Json
                if ($runnerRecord.PSObject.Properties.Name -contains 'child_exit_code') {
                    $code = [int]$runnerRecord.child_exit_code
                }
                if ($runnerRecord.PSObject.Properties.Name -contains 'child_pid') {
                    $childPid = [int]$runnerRecord.child_pid
                }
            }
            catch {
                $code = if ($started) { $process.ExitCode } else { 125 }
            }
        }
        if (-not (Test-Path -LiteralPath $metadata.stdout)) { Write-Utf8NoBom $metadata.stdout '' }
        if (-not (Test-Path -LiteralPath $metadata.stderr)) { Write-Utf8NoBom $metadata.stderr '' }
        $runRecord = [ordered]@{
            child_exit_code = $code
            process = [ordered]@{ executable = $metadata.exe; pid = $childPid; arguments = $metadata.command }
            runner = [ordered]@{ executable = $metadata.runner; pid = $process.Id; arguments = $runnerArguments }
            approval_input_injected_by_harness = $false
            visible_child_output_relayed = $true
            child_stdin_inherited = $true
            application_phase_observed = $applicationPhaseSeen
            calculator_mutation_observed = $mutationObserved
            harness_violation = ($null -ne $violation)
        }
        Write-Utf8NoBom (Join-Path $Path 'run-result.json') (($runRecord | ConvertTo-Json -Depth 8) + "`n")
    }
    finally {
        # Ctrl+C can interrupt the PowerShell wait while the native sidecar is
        # still waiting for the child to finish. Give the sidecar a bounded
        # opportunity to write the real child exit record before final capture.
        try {
            if ($started -and -not $process.HasExited) {
                [void]$process.WaitForExit(10000)
            }
            $resultDeadline = (Get-Date).AddSeconds(10)
            while ($started -and (Get-Date) -lt $resultDeadline) {
                if (Test-Path -LiteralPath $runnerRecordPath) {
                    try {
                        $runnerResult = Get-Content -Raw $runnerRecordPath | ConvertFrom-Json
                        if ($runnerResult.PSObject.Properties.Name -contains 'child_exit_code') {
                            $code = [int]$runnerResult.child_exit_code
                            if ($runnerResult.PSObject.Properties.Name -contains 'child_pid') {
                                $childPid = [int]$runnerResult.child_pid
                            }
                            break
                        }
                    }
                    catch {}
                }
                Start-Sleep -Milliseconds 100
            }
        }
        catch {}
        if (-not (Test-Path -LiteralPath $metadata.stdout)) { Write-Utf8NoBom $metadata.stdout '' }
        if (-not (Test-Path -LiteralPath $metadata.stderr)) { Write-Utf8NoBom $metadata.stderr '' }
        if (-not (Test-Path -LiteralPath (Join-Path $Path 'run-result.json'))) {
            $fallbackRun = [ordered]@{
                child_exit_code = $code
                process = [ordered]@{ executable = $metadata.exe; pid = $childPid; arguments = $metadata.command }
                runner = [ordered]@{ executable = $metadata.runner; pid = if ($started) { $process.Id } else { $null }; arguments = $runnerArguments }
                approval_input_injected_by_harness = $false
                visible_child_output_relayed = $true
                child_stdin_inherited = $true
                application_phase_observed = $applicationPhaseSeen
                calculator_mutation_observed = $mutationObserved
                harness_violation = ($null -ne $violation)
                finalization_stage = 'finally-fallback'
            }
            Write-Utf8NoBom (Join-Path $Path 'run-result.json') (($fallbackRun | ConvertTo-Json -Depth 8) + "`n")
        }
        if (Test-Path -LiteralPath $Path) {
            try { Finalize-Case $Path $code } catch { $finalizationError = $_ }
        }
        try { $process.Dispose() } catch {}
    }
    if ($null -ne $finalizationError) {
        throw $finalizationError
    }
    if ($null -ne $violation) {
        throw 'Acceptance harness stopped after an unauthorized pre-approval calculator.go mutation; see harness-violation.json.'
    }
    return $code
}

function Audit-Evidence([string]$Root) {
    $manifestPath = Join-Path $Root 'manifest.json'
    if (-not (Test-Path -LiteralPath $manifestPath)) {
        throw "Evidence manifest not found: $manifestPath"
    }
    $manifest = Get-Content -Raw $manifestPath | ConvertFrom-Json
    $expectedExit = @{
        'A-apply' = 0
        'R-reject' = 3
        'C-cancel' = 4
        'D-then-R' = 3
        'invalid-then-R' = 3
        'TTY-EOF' = 4
    }
    $rows = @()
    foreach ($case in $manifest.cases) {
        $casePath = Split-Path -Parent $case.stdout
        $capturePath = Join-Path $casePath 'capture.json'
        if (-not (Test-Path -LiteralPath $capturePath)) {
            $rows += [ordered]@{ case = $case.case; result = 'NOT_TESTED'; reason = 'No visible-TTY capture exists.' }
            continue
        }
        $capture = Get-Content -Raw $capturePath | ConvertFrom-Json
        $stdoutPath = $case.stdout
        $jsonValid = $false
        $jsonError = $null
        try {
            $null = Get-Content -Raw $stdoutPath | ConvertFrom-Json
            $jsonValid = $true
        }
        catch {
            $jsonError = $_.Exception.Message
        }
        $expected = $expectedExit[$case.case]
        $exitOk = $true
        if ($null -ne $expected) {
            $exitOk = ($capture.exit_code -eq $expected)
        }
        $mutationExpected = ($case.case -eq 'A-apply')
        $mutationOk = if ($mutationExpected) { -not $capture.calculator_unchanged } else { $capture.calculator_unchanged }
        if ($case.case.StartsWith('CtrlC-')) {
            $ctrlCResult = if ($capture.exit_code -eq 125 -or -not $jsonValid) { 'NOT_TESTED' } else { 'REVIEW_REQUIRED' }
            $ctrlCReason = if ($ctrlCResult -eq 'NOT_TESTED') {
                'Wrapper/child did not produce a valid captured process result; Ctrl+C remains untested.'
            }
            else {
                'Timing-sensitive Ctrl+C capture requires manual rollback/phase review.'
            }
            $rows += [ordered]@{
                case = $case.case
                result = $ctrlCResult
                reason = $ctrlCReason
                exit_code = $capture.exit_code
                hashes_unchanged = $capture.hashes_unchanged
                stdout_json = $jsonValid
            }
            continue
        }
        $result = if ($exitOk -and $mutationOk -and $jsonValid) { 'PASS' } else { 'FAIL' }
        $rows += [ordered]@{
            case = $case.case
            result = $result
            expected_exit = $expected
            exit_code = $capture.exit_code
            mutation_expected = $mutationExpected
            mutation_ok = $mutationOk
            stdout_json = $jsonValid
            json_error = $jsonError
        }
    }
    $audit = [ordered]@{
        audited_at = (Get-Date).ToUniversalTime().ToString('o')
        evidence_root = $Root
        rows = $rows
        all_ordinary_rows_pass = (@($rows | Where-Object { $_.case -notlike 'CtrlC-*' -and $_.result -ne 'PASS' }).Count -eq 0)
        all_required_rows_resolved = (@($rows | Where-Object { $_.result -in @('NOT_TESTED', 'FAIL', 'REVIEW_REQUIRED') }).Count -eq 0)
    }
    Write-Utf8NoBom (Join-Path $Root 'audit.json') (($audit | ConvertTo-Json -Depth 8) + "`n")
    Write-Output ($audit | ConvertTo-Json -Depth 8)
}

if ($Action -eq 'Capture') {
    if ([string]::IsNullOrWhiteSpace($CasePath)) {
        throw '-CasePath is required for Capture.'
    }
    Capture-Case ([IO.Path]::GetFullPath($CasePath)) $ExitCode
    return
}

if ($Action -eq 'Run') {
    if ([string]::IsNullOrWhiteSpace($CasePath)) {
        throw '-CasePath is required for Run.'
    }
    $runCode = Run-VisibleCase ([IO.Path]::GetFullPath($CasePath))
    Write-Output "CHILD_EXIT_CODE=$runCode"
    return
}

if ($Action -eq 'Audit') {
    if ([string]::IsNullOrWhiteSpace($EvidenceRoot)) {
        throw '-EvidenceRoot is required for Audit.'
    }
    Audit-Evidence ([IO.Path]::GetFullPath($EvidenceRoot))
    return
}

if ([string]::IsNullOrWhiteSpace($EvidenceRoot)) {
    $EvidenceRoot = Join-Path ([IO.Path]::GetTempPath()) ('devctl-stage7d-c-visible-' + (Get-Date -Format 'yyyyMMdd-HHmmss'))
}
$EvidenceRoot = [IO.Path]::GetFullPath($EvidenceRoot)
if (Test-Path -LiteralPath $EvidenceRoot) {
    throw "EvidenceRoot already exists. Refusing to reuse acceptance evidence: $EvidenceRoot"
}
New-Item -ItemType Directory -Force -Path $EvidenceRoot | Out-Null

$exe = Join-Path $EvidenceRoot 'devctl.exe'
$runner = Join-Path $EvidenceRoot 'stage7d-c-windows-visible-runner.exe'
Push-Location $repoRoot
try {
    & go build -o $exe ./cmd/devctl
    if ($LASTEXITCODE -ne 0) {
        throw "Windows executable build failed with exit code $LASTEXITCODE."
    }
    & go build -o $runner ./scripts/stage7d-c-windows-visible-runner.go
    if ($LASTEXITCODE -ne 0) {
        throw "Visible runner build failed with exit code $LASTEXITCODE."
    }
}
finally {
    Pop-Location
}

$source = Join-Path $EvidenceRoot 'fixture-source'
New-Item -ItemType Directory -Force -Path $source | Out-Null
$projectId = 'acceptance-broken-' + ([IO.Path]::GetFileName($EvidenceRoot) -replace '[^A-Za-z0-9_-]', '-')
Write-Utf8NoBom (Join-Path $source '.gitignore') ".devctl/`n"
Write-Utf8NoBom (Join-Path $source 'go.mod') "module acceptance.local/broken`n`ngo 1.22`n"
Write-Utf8NoBom (Join-Path $source 'calculator.go') "package calculator`n`nfunc Add(left, right int) int { return left + }`n"
$projectConfig = '{"version":"1","project_id":"' + $projectId + '","checks":{"go-environment":{"enabled":true,"required":true},"go-test":{"enabled":true,"required":true},"go-test-race":{"enabled":false},"go-build":{"enabled":true,"required":true}}}'
Write-Utf8NoBom (Join-Path $source 'devctl.json') ($projectConfig + "`n")

Invoke-Git @('init', '--initial-branch=main') $source
Invoke-Git @('config', 'user.email', 'stage7d-c-acceptance@example.invalid') $source
Invoke-Git @('config', 'user.name', 'Stage 7D-C Acceptance') $source
Invoke-Git @('add', '.') $source
Invoke-Git @('commit', '-m', 'acceptance baseline') $source
$baselineHead = (& git -C $source rev-parse HEAD).Trim()

$fixedContent = "package calculator`n`nfunc Add(left, right int) int { return left + right }`n"
$proposal = [ordered]@{
    schema_version = '1'
    task_id = 'repair-cli-001'
    changes = @([ordered]@{
        path = 'calculator.go'
        content = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($fixedContent))
    })
    worker = 'controlled-cli'
}
$proposalPath = Join-Path $EvidenceRoot 'proposal-pass.json'
Write-Utf8NoBom $proposalPath (($proposal | ConvertTo-Json -Depth 6 -Compress) + "`n")

$runScript = Join-Path $repoRoot 'scripts\stage7d-c-windows-visible-acceptance.ps1'
$cases = [ordered]@{
    'A-apply' = 'At the real approval prompt, type A and press Enter.'
    'R-reject' = 'At the real approval prompt, type R and press Enter.'
    'C-cancel' = 'At the real approval prompt, type C and press Enter.'
    'D-then-R' = 'At the real approval prompt, type D and press Enter; wait for details and the next prompt, then type R and press Enter.'
    'invalid-then-R' = 'At the real approval prompt, type X and press Enter; wait for the invalid-input warning and next prompt, then type R and press Enter.'
    'TTY-EOF' = 'At the approval prompt, send real console EOF (Ctrl+Z, then Enter) without choosing A/R/D/C.'
    'CtrlC-before-application' = 'At or before approval, press Ctrl+C once.'
    'CtrlC-around-application' = 'Press A, then press Ctrl+C immediately if the process is still active.'
    'CtrlC-after-mutation' = 'Press Ctrl+C immediately after the applying phase reports a change, if reachable.'
    'CtrlC-post-state' = 'Press Ctrl+C during post-state validation, if reachable.'
    'CtrlC-reverification' = 'Press Ctrl+C during re-verification, if reachable.'
}
$manifestCases = @()
foreach ($entry in $cases.GetEnumerator()) {
    $casePath = Join-Path $EvidenceRoot ('cases\' + $entry.Key)
    $projectPath = Join-Path $casePath 'project'
    New-Item -ItemType Directory -Force -Path $casePath | Out-Null
    Copy-Item -LiteralPath $source -Destination $projectPath -Recurse
    $caseProjectId = $projectId + '-' + ($entry.Key -replace '[^A-Za-z0-9_-]', '-')
    $caseProjectConfig = '{"version":"1","project_id":"' + $caseProjectId + '","checks":{"go-environment":{"enabled":true,"required":true},"go-test":{"enabled":true,"required":true},"go-test-race":{"enabled":false},"go-build":{"enabled":true,"required":true}}}'
    Write-Utf8NoBom (Join-Path $projectPath 'devctl.json') ($caseProjectConfig + "`n")
    Invoke-Git @('add', 'devctl.json') $projectPath
    Invoke-Git @('commit', '--amend', '--no-edit') $projectPath
    $caseBaselineHead = (& git -C $projectPath rev-parse HEAD).Trim()
    Save-Snapshot $projectPath (Join-Path $casePath 'before')
    $metadata = [ordered]@{
        case = $entry.Key
        instruction = $entry.Value
        project_id = $caseProjectId
        exe = $exe
        runner = $runner
        proposal = $proposalPath
        project = $projectPath
        baseline_head = $caseBaselineHead
        stdout = (Join-Path $casePath 'stdout.json')
        stderr = (Join-Path $casePath 'stderr.txt')
        command = "& `"$exe`" repair --json --proposal `"$proposalPath`" --allow calculator.go `"$projectPath`""
    }
    Write-Utf8NoBom (Join-Path $casePath 'case.json') (($metadata | ConvertTo-Json -Depth 6) + "`n")
    Assert-CaseReady $metadata $casePath
    $run = @"
`$ErrorActionPreference = 'Stop'
& '$runScript' -Action Run -CasePath '$casePath'
    Write-Host 'Visible case finished; child exit code and evidence are in run-result.json and capture.json.'
"@
    Write-Utf8NoBom (Join-Path $casePath 'run-visible.ps1') $run
    $manifestCases += $metadata
}

$manifest = [ordered]@{
    created_at = (Get-Date).ToUniversalTime().ToString('o')
    evidence_root = $EvidenceRoot
    executable = $exe
    runner = $runner
    proposal = $proposalPath
    source_fixture = $source
    baseline_head = $baselineHead
    cases = $manifestCases
    operator_boundary = 'Run one cases/<name>/run-visible.ps1 from a visible Windows Terminal/PowerShell session and perform only the printed keyboard action.'
}
Write-Utf8NoBom (Join-Path $EvidenceRoot 'manifest.json') (($manifest | ConvertTo-Json -Depth 8) + "`n")
Write-Utf8NoBom (Join-Path $EvidenceRoot 'README.txt') @"
Stage 7D-C visible Windows acceptance preparation

This directory is disposable and outside the repository.
Run one case at a time from an ordinary visible Windows Terminal/PowerShell session:

  & <case-directory>\run-visible.ps1

Perform exactly the printed keyboard action, including Enter. The native sidecar
keeps the child CLI output visible while capturing stdout and stderr separately.
The wrapper records the child exit code, file content and SHA-256 snapshots, and
git status.
Do not use the Python ConPTY helper for this acceptance.

The Ctrl+C cases are timing-sensitive. If a requested phase is not reachable,
leave that case captured as NOT_TESTED and retain the package-test evidence.
"@
Write-Output (Get-Content -Raw (Join-Path $EvidenceRoot 'manifest.json'))
