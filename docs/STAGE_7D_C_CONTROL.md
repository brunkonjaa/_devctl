# Stage 7D-C human-facing controlled repair CLI

Status: IMPLEMENTED / WINDOWS VISIBLE ACCEPTANCE PARTIAL

Baseline: Stage 7D-B merged `main` at
`a775542fb40b48b3a7b548774c7d223714b05bbc`.

This document defines and records the human-facing terminal boundary for the
accepted Stage 7D-B repair engine. The CLI implementation is in
`internal/repaircli` and `cmd/devctl`; it does not add a worker transport or
another repair engine.

## Purpose

Stage 7D-C makes one bounded repair attempt usable from a terminal while
keeping `_devctl` responsible for verification, approval binding, path policy,
patch application, rollback, evidence, and the final status.

The intended command is:

```text
devctl repair [--json] [--verbose] [--proposal <proposal.json>] [--allow <path,...>] <project>
```

The command is a presentation and orchestration boundary above
`internal/repair.Run`. It must not duplicate Stage 7D-B validation or patch
application logic.

## Scope

Implemented in this slice:

- one terminal repair invocation;
- an explicit controlled proposal-provider seam;
- human approval, rejection, cancellation, and technical-detail views;
- live observational progress;
- human-readable, verbose, and technical presentation levels;
- strict separation of human output and structured JSON output;
- Windows-aware Ctrl+C and terminal behavior;
- acceptance tests using a controlled provider and the existing synthetic
  repair fixture.

Excluded from this slice:

- Codex, DeepSeek, OpenAI API, or any other external AI transport;
- a general worker adapter;
- automatic commits, pushes, merges, branches, or history rewriting;
- a repair loop, retry, or second repair attempt;
- changes to the Stage 7D-B security, provenance, or patch contract. This
  prerequisite includes the narrow post-modification cancellation hardening
  required to ensure `CANCELLED` never leaves an approved repair applied;
- additions, deletions, renames, binary patches, or mode changes unless the
  existing repair engine later accepts them as a separate contract change.

## Authority boundary

The command package only connects terminal input/output and dependencies:

```text
terminal command
      |
      +--> controlled proposal provider
      |
      +--> approval UI adapter
      |
      +--> observational ProgressSink
                    |
                    v
             existing repair.Run
                    |
                    v
       Stage 7D-B validation, approval binding,
       application, rollback, evidence, and verify
```

The CLI must not know how a patch is represented or applied. It must pass the
controlled proposal provider and approval adapter into `repair.Run`. It must
not recreate diff validation, hash checks, path-policy checks, pre-apply
validation, file writes, rollback, post-state validation, or deterministic
re-verification.

The proposal provider is a dependency seam for acceptance testing in this
stage. `repaircli.Options.Propose` is the context-aware injectable seam;
`--proposal` is the current local file adapter. If no provider is available,
the command returns a bounded framework error and does not attempt approval or
modification. A real external worker belongs to a later stage.

## User-facing workflow

Normal output uses clear human language and does not require knowledge of Git
internals, hashes, provenance terminology, or `_devctl` architecture.

The normal flow is conceptually:

```text
Checking project...

Build                  Passed
Unit tests             Failed
Lint                   Passed

1 problem needs attention.

Preparing a possible fix...
Possible fix ready.

File: calculator.go

Change:

- return a - b
+ return a + b

Apply this fix? [A] Apply [R] Reject [D] Details [C] Cancel
```

After approval, the command reports progress in plain language:

```text
Checking nothing changed since the fix was shown...
Applying fix...
Checking that only the approved files changed...
Testing the project again...

Fix successful.
```

The exact words may change, but the meaning and ordering must remain clear.
Technical event names such as `PRE_APPLY_STATE_VALIDATED` must not be the only
normal presentation.

## Presentation levels

### Normal

Show human-readable phases and outcomes:

- checking the project;
- problem found;
- preparing a possible fix;
- possible fix ready;
- waiting for approval;
- applying the fix;
- checking changed files;
- testing the project again;
- fix successful, fix failed, rejected, cancelled, or nothing changed.

### Detailed / verbose

`--verbose` may show:

- individual check IDs and statuses;
- durations;
- changed file paths;
- bounded failure reasons;
- rollback status;
- deterministic verification detail;
- the evidence result path.

### Technical / evidence

The `D` interaction must make the exact underlying evidence available before
approval, including:

- project ID, canonical project path, and task ID;
- baseline and verification run IDs;
- worker identity and protocol version;
- complete canonical old-to-new patch display;
- exact patch SHA-256 hash;
- pre/post file hashes, sizes, and modes;
- policy and `_devctl` provenance;
- rollback and evidence paths where available.

`repair.Run` constructs and persists the canonical patch artifact, reloads it,
derives the display, and supplies copies of the artifact, display, hash, and
engine-owned `ApprovalEvidenceView` to the approval adapter. The technical
display must use those supplied values and must not reread the live filesystem,
Git state, policy configuration, or provenance. The approval decision is
**integrity-bound to the canonical patch hash**. SHA-256 identifies the
approved patch bytes; it does not authenticate the human's identity.

Returning from the details view must leave the repair undecided and must not
modify project files.

## Proposal and approval interaction

The approval adapter receives the existing bounded `ApprovalRequest`, including
the exact canonical patch bytes, displayed diff, `DiffHash`, and an immutable
engine-owned view containing canonical project metadata, pre/post file hashes,
sizes and modes, policy and `_devctl` provenance, and patch/evidence paths. It
returns the existing approval outcome and hash to `repair.Run`. The proposal
provider receives the run `context.Context` so provider timeout and
cancellation use the same cancellation boundary as the orchestration.

Input is case-insensitive:

```text
A / a    Apply
R / r    Reject
D / d    Technical details, then return to the prompt
C / c    Cancel
```

Invalid input writes this message to the human stream and re-prompts without
changing the proposal or repository:

```text
Please choose A, R, D, or C.
```

EOF while waiting for approval is `CANCELLED` with no write. Reject and cancel
remain distinct outcomes. Approval is never inferred from an empty, ambiguous,
or malformed response.

If stdin is unavailable or non-interactive for a repair requiring approval,
the command emits `APPROVAL UNAVAILABLE`, returns a framework error, and stops
before approval or file modification. A non-interactive caller must use a
separate future contract; piping an approval byte sequence is not silently
accepted as a human terminal.

## Live progress

The Stage 7D-B result contains lifecycle events after execution. The CLI also
needs progress while `repair.Run` is still executing. The implementation
reuses the existing `internal/events` sink and event delivery infrastructure.
`repair.Run` may emit repair lifecycle events through an
optional `events.Sink`; it must not introduce a competing event framework.

```go
type ProgressSink events.Sink
```

The event may contain a bounded phase, human message, technical event type, and
elapsed duration. It may report:

- checking the project;
- preparing a possible fix;
- possible fix ready;
- waiting for approval;
- checking nothing changed;
- applying the fix;
- checking changed files;
- testing the project again;
- final outcome.

The sink is observational only. It cannot approve, reject, cancel, modify,
replace, reorder, retry, or otherwise influence `repair.Run`. If the sink
fails, repair authority and the final result must not change; the CLI may
report the rendering problem as a bounded framework error only where the
underlying command cannot safely continue.

## Cancellation and Ctrl+C

The command must use a controlled cancellation context and handle Windows
`Ctrl+C` consistently with other supported platforms.

Before application:

```text
CANCELLED
no project files changed
exit 4
```

Once any approved modification has occurred, cancellation must stop further
repair processing, use the existing Stage 7D-B rollback path, and return
`CANCELLED` only after the exact baseline is proved restored. This includes
cancellation immediately after `PATCH_APPLIED`, during post-state validation,
and during or after deterministic re-verification. If exact restoration cannot
be proved, the result is a framework/rollback error and exit `2`, not ordinary
cancellation.

After cancellation, no second proposal, approval, application, verification,
commit, push, merge, or retry is allowed.

## Output streams and JSON

Human and machine output are separate:

- progress goes to `stderr`;
- diff display and approval prompts go to `stderr`;
- invalid-input and technical-detail output goes to `stderr`;
- `--json` writes exactly one complete structured result to `stdout`;
- no progress, prompt, ANSI rendering, or diagnostic text may corrupt JSON
  stdout.

This rule applies to success, deterministic failure, rejection, cancellation,
provider failure, approval unavailability, rollback failure, malformed
proposal, and all framework errors. If JSON encoding itself fails, the command
returns exit `2` and reports the failure on `stderr`.

The structured result must preserve the bounded Stage 7D-B result and evidence
references. Human wording must not replace statuses, hashes, task IDs, run IDs,
provenance, or rollback evidence.

## Exit codes

The command extends the existing `_devctl` semantics without changing the
meaning of `0`, `1`, or `2`:

```text
0  repair completed successfully, or no blocking repair was required
1  deterministic verification failure remains after the one repair attempt
2  framework, provider, protocol, validation, JSON, rollback, or internal error
3  user rejected the proposed repair
4  user cancelled the operation, including controlled Ctrl+C or approval EOF
```

The command must not reinterpret a deterministic `FAIL` as `PASS`. A rejection
or cancellation is not a software failure, but it must remain explicit in the
structured result and evidence.

## Fail-closed requirements

The command must stop without applying a repair when:

- stdin is unavailable for interactive approval;
- the proposal provider fails, times out, or returns a malformed proposal;
- approval is ambiguous, rejected, cancelled, or interrupted;
- the displayed artifact cannot be shown from the persisted canonical patch;
- the approval hash does not match the canonical patch hash;
- `HEAD`, branch, index, worktree, protected file content, identity, policy,
  or provenance changes;
- an unexpected file, mode, or byte change is detected;
- a pre-application deterministic or protocol error prevents safe application;
- rollback cannot prove exact restoration.

After a patch has been applied and its actual delta has passed Stage 7D-B
integrity validation, a final deterministic verification `FAIL` or `ERROR` is
recorded as the final result and the validated approved delta remains applied.
That post-application outcome is different from a pre-application fail-closed
error, an apply/post-state integrity failure that rolls back, and cancellation,
which restores the baseline before returning.

The CLI must rely on `repair.Run` for these checks and must not implement a
parallel interpretation of them.

## Git and attempt boundaries

Each invocation permits at most the single Stage 7D-B repair attempt. The
command must not commit, push, merge, create a branch, rewrite history, or
automatically retry. A clean baseline remains required. Dirty-state
preservation is not added by this stage.

## Acceptance matrix and implementation record

The focused local suite covers the repair-engine adapter, interactive approval,
JSON separation, warning/error exit policy, provider context propagation,
provider cancellation, rejection, invalid input, evidence display and a
successful controlled proposal. The final Windows process pass below records
what was exercised through a compiled `devctl.exe` and what still needs a
real interactive Windows host.

The implementation location is `internal/repaircli/cli.go`, with command
wiring in `cmd/devctl/platform.go`. The engine remains the authority for all
patch validation, provenance, application, rollback and re-verification.

The release matrix is:

1. successful interactive repair;
2. user rejection;
3. user cancellation before application;
4. Ctrl+C before application;
5. Ctrl+C during application with successful rollback;
6. Ctrl+C immediately after `PATCH_APPLIED` with successful rollback;
7. Ctrl+C during post-state validation with successful rollback;
8. Ctrl+C during deterministic re-verification with successful rollback;
9. rollback/restoration failure not being reported as ordinary cancellation;
10. unavailable or non-interactive stdin;
11. project change after the fix is displayed;
12. approved patch/hash mismatch;
13. unexpected file change;
14. deterministic verification still failing after repair;
15. deterministic verification returning `ERROR` after an approved validated delta;
16. baseline `PASS` or `WARN` stopping with exit `0` without provider or approval;
17. baseline `ERROR` stopping with exit `2` without provider, approval, or write;
18. normal output being human-readable;
19. technical details exposing only the immutable evidence supplied by `repair.Run`;
20. `--json` stdout being exactly one valid structured result;
21. live progress being emitted before `repair.Run` returns;
22. live output remaining on `stderr`;
23. provider error or timeout;
24. provider cancellation while blocked;
25. malformed provider proposal;
26. invalid approval input followed by valid approval;
27. repeated invalid approval input causing no writes;
28. EOF while waiting for approval;
29. rejection, cancellation, and provider failure each producing valid JSON;
30. controlled Ctrl+C producing the defined result where shutdown is possible;
31. a progress observer being unable to influence repair decisions;
32. no commit, push, merge, branch creation, or second repair attempt.

Windows coverage must be explicit for console input, `Ctrl+C`, path handling,
stderr/stdout separation, and process exit codes. The tests must use a
controlled proposal provider and must not connect an external AI service.

## Final Windows process acceptance record — 2026-08-18

The executable was built from the current worktree with:

```powershell
go build -o "$env:TEMP\devctl-stage7d-c-<run>\devctl.exe" ./cmd/devctl
```

The fixture was a clean temporary Git repository containing `go.mod`,
`devctl.json`, and a deliberately broken `calculator.go`. The proposal file
used the existing `repair.Proposal` JSON shape and changed only
`calculator.go`. Evidence was captured outside this repository under
`%TEMP%\devctl-stage7d-c-*`.

Direct compiled-process evidence:

```powershell
& $exe repair --json $passingProject
# process exit 0; stdout was one valid JSON object; baseline/final PASS

& $exe repair --json --proposal $proposal --allow calculator.go $brokenProject
# stdin/stdout/stderr redirected; process exit 2; approval was unavailable;
# stdout was one valid JSON object and no project file changed

python scripts/stage7d-c-windows-acceptance.py `
  --exe $exe --source $brokenProject --proposal $proposal --case reject
# corrected ConPTY host: approval input is sent only after the synchronous
# "Apply this fix? [A] Apply [R] Reject [D] Details [C] Cancel" prompt is
# observed, and input_read/output_write are released after CreateProcessW
# invalid-reject and details-reject now wait for their second interaction
# prompt before sending R; all six cases still ended in cancellation/EOF
```

The corrected visible-terminal sidecar was then used for fresh cases under
`%TEMP%\devctl-stage7d-c-visible-finalAvisible4`. The real child remained
attached to the visible terminal, approval was typed by the operator, and the
sidecar captured the child result without injecting input. The six ordinary
cases passed: `A`, `R`, `C`, `D` then `R`, invalid then `R`, and TTY EOF.
Each case had an independent committed fixture with an exact `FAIL` baseline,
complete result/finalization evidence, unchanged or expected calculator hashes,
and the expected process exit code.

The direct native visible-terminal Ctrl+C case under
`%TEMP%\devctl-stage7d-c-visible-finalAvisible7\cases\CtrlC-before-application`
also passed. Immediately before launch it recorded the expected HEAD
`1d0baef610f198867b166ec75a6a56b47fd92686`, clean Git status, broken
`calculator.go` hash
`c7afd3ac8e0a0ec340b6a3f0e35425bee4ba09b4f77c5b2a0d1294916c3db223`, and
baseline `FAIL`. A real Ctrl+C at approval produced `CANCELLED`, JSON exit
code `4`, PowerShell `$LASTEXITCODE` `4`, unchanged calculator hash, unchanged
HEAD, and clean Git status.

The fresh `CtrlC-around-application` case under
`%TEMP%\devctl-stage7d-c-visible-finalAvisible8` was also started from an
independent `FAIL` baseline, but the operator's `A` completed the atomic apply
and re-verification before a Ctrl+C could be delivered. That case is consumed
and remains `NOT_TESTED` for the timing requirement; it is not counted as
Ctrl+C evidence.

The acceptance helper correction is limited to the Windows test boundary. It
does not change `repair.Run` or the CLI approval logic. The asynchronous
`Waiting for approval.` lifecycle message is recorded for diagnostics only; it
does not control input injection. Microsoft documents the
pipe handles supplied to `CreatePseudoConsole` as host-side handles that should
be released after child creation; the helper now follows that lifetime rule.
The corrected host still cannot provide trusted approval evidence in that
redirected automation environment. Those ConPTY runs remain diagnostic only;
the ordinary approval results above come from the visible-terminal sidecar,
and the Ctrl+C-before result comes from a direct native visible-terminal
process.

The deterministic package evidence is separate from Windows process evidence:

| Package-level requirement | Evidence | Scope |
|---|---|---|
| approval choices, details, invalid input, reject and cancel | `go test ./internal/repaircli -count=20` | adapter/state-machine tests, not a Windows console |
| warning/error exit policy and JSON result mapping | `go test ./internal/repaircli -count=20` | deterministic package behavior, not process/TTY behavior |
| provider context cancellation | `go test -race ./internal/repaircli -count=5` | cancellation seam, not a Windows signal |
| rollback after mutation, rollback failure, and approval/hash binding | `go test ./internal/repair -count=1` through the full suite | repair-engine safety evidence, not Windows process evidence |

These package results satisfy their deterministic test scope only. They do not
resolve the Windows process rows below.

| Windows acceptance scenario | Result | Direct evidence or limitation |
|---|---|---|
| Successful interactive repair | PASS | Fresh visible-terminal A case completed with child exit 0, valid JSON stdout, separate stderr capture, after-state evidence, and successful final verification; final WARN reflects the intentional uncommitted repaired file |
| `A` apply | PASS | Visible prompt accepted `a` plus Enter; the approved and actual diff hashes matched, only `calculator.go` changed, and the child exited 0 after deterministic re-verification |
| `R` reject, exit 3 | PASS | Fresh visible-terminal R case returned child exit 3 with `approved:false`, valid JSON stdout, complete finalization evidence, and unchanged calculator/hash snapshots |
| `C` cancel, exit 4 | PASS | Fresh visible-terminal C case returned child exit 4 with cancellation outcome, valid JSON stdout, complete finalization evidence, and unchanged calculator/hash snapshots |
| Details `D` then decision | PASS | Visible D→R case captured repeated details/prompt interaction, returned exit 3, finalized completely, and left calculator/hash snapshots unchanged |
| Invalid input then valid decision | PASS | Visible X→R case captured the invalid-input warning and reprompt, returned exit 3, finalized completely, and left calculator/hash snapshots unchanged |
| EOF while waiting for approval, exit 4 | PASS | Fresh visible-terminal Ctrl+Z EOF case returned exit 4, reported baseline restoration, finalized completely, and left calculator/hash snapshots unchanged |
| Non-interactive stdin, exit 2 | PASS | Compiled process with redirected stdin and proposal; no write occurred |
| `--json` exactly one stdout object | PASS | Redirected compiled process output parsed as one JSON object |
| Human/progress output on stderr | PASS | Redirected compiled process emitted diagnostics separately on stderr |
| Baseline PASS, exit 0 | PASS | Compiled `repair --json` against clean passing fixture |
| Baseline blocking WARN, exit 1 | NOT_TESTED | Requires a real project policy/check fixture not exposed by the CLI adapter |
| Final FAIL, exit 1 | NOT_TESTED | Requires interactive approval of a deliberately ineffective proposal |
| Final ERROR, exit 2 | NOT_TESTED | Requires interactive approval and post-apply error fixture |
| Provider/proposal parse failure, exit 2 | PASS | Compiled process with malformed proposal returned framework JSON result |
| Ctrl+C before application | PASS | Direct native visible-terminal case `finalAvisible7` returned JSON cancellation and exit 4; HEAD, Git status, calculator content and SHA-256 remained at the independent FAIL baseline |
| Ctrl+C during provider work | NOT_TESTED | CLI proposal-file provider has no controllable delay seam |
| Ctrl+C during application | NOT_TESTED | Fresh direct case `finalAvisible8` completed after approval before Ctrl+C could be delivered; no timing evidence was claimed |
| Ctrl+C after file change before post-state | NOT_TESTED | Requires a controllable real-process timing seam |
| Ctrl+C during post-state validation | NOT_TESTED | Requires a controllable real-process timing seam |
| Ctrl+C during re-verification | NOT_TESTED | Requires a controllable real-process timing seam |
| Cancellation before mutation leaves files unchanged | PASS | Direct visible Ctrl+C-before case left the clean failing fixture unchanged and returned exit 4 |
| Cancellation after mutation rolls back exactly | NOT_TESTED | Windows process-level signal timing not reached; package-level rollback tests remain green |
| Rollback failure returns exit 2, not 4 | NOT_TESTED | No process-level fault-injection seam exists |
| One repair attempt maximum | PASS | Compiled result contains one bounded lifecycle and no retry; no second process/engine was started |
| No unauthorized Git mutation | PASS | Fixture remained on its original branch with clean index after fail-closed process |
| Approval remains hash-bound | PASS | Visible A evidence records approval `diff_hash` equal to `actual_diff_hash` (`f27595f31f50a26524dd6300775010cb58f85c47530f78106c0acbcda9cbcb7f`) |
| Windows path handling | PASS | Compiled result recorded and used absolute Windows fixture paths |
| Ctrl+C process exit mapping | NOT_TESTED | Exit 4 is directly proven before application; post-mutation Ctrl+C exit mapping remains untested |
| No external worker transport | PASS | Process used only the controlled local proposal file |
| Deterministic verification cache reuse | NOT_APPLICABLE | Deliberately outside Stage 7D-C and not implemented |

The current ConPTY limitation is unresolved at the automated pseudoconsole
boundary; an environmental cause is suspected but not proven. The automation
host itself runs with
redirected standard streams, and its pseudo-console child received EOF rather
than the injected approval bytes even after the helper correction. This is not
treated as evidence that the CLI's approval state machine passed. The remaining
manual rows must use an ordinary visible Windows Terminal or `conhost.exe`.
Computer-use automation cannot be used for this terminal boundary because its
Windows safety rules prohibit automating terminal applications. The residual
manual command is:

```powershell
& $exe repair --json --proposal $proposal --allow calculator.go $brokenProject
```

The ordinary approval matrix is already recorded above. For any remaining row,
run the command from the visible terminal with a newly prepared clean failing
fixture, record the numeric process exit code, save stdout and stderr
separately, and compare the fixture tree and `git status --short` with the
baseline. Repeat Ctrl+C during provider work, after application, during
post-state validation, and during re-verification only where the timing is
reachable. The required evidence is the same matrix row plus the rollback
proof or the exit-2 rollback-error result. Do not reuse a case after an
attempted execution.

## Visible Windows TTY evidence preparation and remaining operator actions

The acceptance-only preparation tool is
`scripts/stage7d-c-windows-visible-acceptance.ps1`. Running:

```powershell
& powershell -NoProfile -ExecutionPolicy Bypass -File `
  scripts/stage7d-c-windows-visible-acceptance.ps1 -Action Prepare
```

creates a disposable `%TEMP%\devctl-stage7d-c-visible-*` evidence root. The
root contains a fresh x86-64 Windows executable, a clean Git fixture, the
controlled proposal, baseline file content and SHA-256 snapshots, baseline
 Git status, and isolated runner/capture directories for `A`, `R`, `C`,
`D` then `R`, invalid then `R`, TTY EOF, and the five prepared Ctrl+C timing
cases. Each `run-visible.ps1` launches the native
`stage7d-c-windows-visible-runner.exe`. That sidecar keeps stdin inherited,
relays the real child stdout and stderr to the visible terminal, captures both
streams separately, records the child exit code, and writes the after-state
hashes, content, and Git status. The PowerShell wrapper no longer uses
`DataReceivedEventHandler` callbacks or redirects the visible diagnostic
stream. Approval is line-based, so the operator types the choice and presses
Enter, for example `A` then Enter.

The preparation command does not exercise a terminal interaction. The
operator must run one case runner at a time from a visible Windows
Terminal/PowerShell session and perform only the printed keyboard action.
Until those runner outputs exist, all corresponding process-level matrix rows
remain `NOT_TESTED`.

After the operator captures cases, the acceptance-only audit can be run with:

```powershell
& powershell -NoProfile -ExecutionPolicy Bypass -File `
  scripts/stage7d-c-windows-visible-acceptance.ps1 -Action Audit `
  -EvidenceRoot $evidenceRoot
```

The audit emits `audit.json` and does not promote missing cases. A prepared run
starts with every process-level case as `NOT_TESTED`; after visible actions,
only cases with complete evidence are promoted. The ordinary matrix and the
direct Ctrl+C-before case are recorded above. The timing-sensitive rows remain
`NOT_TESTED` when the small fixture completes before the requested signal
window.

### Acceptance tooling mutation audit

The previous A-case anomaly was traced to reuse of an existing explicit
evidence root. The old preparation path used `Copy-Item` into an already
existing case project, so it merged a fresh source over a project that had
already been modified by an earlier `devctl.exe repair` attempt. The later
run therefore saw a WARN baseline and retained old `.devctl` evidence. The
actual calculator mutation was performed by the previously launched
`devctl.exe repair` child; the old runner did not retain its PID, approval
input, or phase timeline, so the exact earlier keyboard cause cannot be
reconstructed from that snapshot.

The acceptance tool now refuses to reuse any evidence root, creates each case
in a new independently copied fixture, assigns each fixture its own committed
`project_id` so the registry cannot confuse cases, and runs a non-mutating
`verify --json` preflight. It asserts the expected HEAD, clean Git status,
broken-file hash, and exact `FAIL` baseline immediately before repair launch.
It writes a one-attempt marker, injects no input, watches `calculator.go`
during the child process, records the responsible child PID, stops and
preserves evidence on a pre-application mutation, and does not use `exit` in
the visible runner to terminate the parent PowerShell session. After the child
terminates, the native sidecar finalizes stdout and stderr before returning;
the PowerShell wrapper then finalizes the child exit code, after-state hashes,
file content, Git status, `run-result.json`, `capture.json`, and a
`finalization-record.json`, including a recorded failure if finalization itself
cannot complete.

## Implemented boundary

The coding slice adds only the command wiring, terminal presentation,
approval/input adapter, JSON result handling, and tests required by this
document. It uses the context-aware proposal seam, engine-owned approval
evidence view, and the optional observational progress hook provided by
`repair.Run` through `internal/events`.

It must not move Stage 7D-B policy decisions into `cmd/devctl`, alter the
canonical patch format, weaken approval binding, add retries, or add external
worker transport. Any change to those controls requires a new contract and
review rather than being hidden inside the CLI work.
