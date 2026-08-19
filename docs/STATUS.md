# devctl status

This is the short handoff point for continuing work on `_devctl`.

Navigation: [documentation index](README.md) · [roadmap](ROADMAP.md) · [decisions](DECISIONS.md) · [lessons](LESSONS.md)

## Current state

Stages 4C through 6 are complete. Stage 7 is now the multi-project deterministic developer automation platform work. HearthLink is the first Android integration, not the boundary of devctl.

Current local stage:

```text
Stage 7F-C deterministic cross-project knowledge search
```

Stage 7E-A, 7E-B, 7F-A, local 7F-B and local 7F-C focused acceptance pass. The GitHub
Actions workflow is ready, but hosted acceptance remains
`NOT TESTED` until the owner explicitly authorizes publication and a remote run
passes.

AI remains an optional worker inside a workflow. It is not the execution authority. Do not add automatic Git actions or unbounded repair loops as part of Stage 7C.

The current local interfaces are documented in [PLATFORM_CONTEXT.md](PLATFORM_CONTEXT.md). Lessons and cache entries are advisory and cannot override a fresh deterministic repository result.

The Stage 7C control contract is [STAGE_7C_CONTROL.md](STAGE_7C_CONTROL.md). Stage 7C is frozen at the merged baseline. The Stage 7D-A contract and Stage 7D-B implementation are now accepted; later work remains outside this slice.

## Completed stages

### Stage 1 — Core verification engine

- Project discovery and technology detection
- Allowlisted external commands
- Normalized check results
- Truthful statuses, including `NOT_TESTED`
- Exit codes: `0` non-blocking, `1` policy or verification failure, `2` framework error

### Stage 2 — Planner and scheduler

- Dependency graph planning and cycle detection
- Bounded concurrency and resource locks
- Dependency-failure skipping
- Cancellation and hard/inactivity timeouts
- Process-tree cleanup

### Stage 3 — Android/Gradle verification

- Java, Gradle wrapper, and Android structure checks
- Build, unit test, and lint checks
- Secret scanning
- Coverage and dependency checks with truthful `NOT_TESTED` fallbacks
- JSON policy and persistent evidence

### Stage 4A — Go self-verification

- Go detection through `go.mod`
- Go environment, build, test, and race-test checks
- Local race test reports `NOT_TESTED` when cgo or a C compiler is unavailable
- `_devctl` verifies itself on Windows and Ubuntu 24.04 CI runs the race detector
- Provenance includes devctl version, commit, policy version, and check version
- `LESSON-0002` records the ignored-source-file reproducibility failure

### Stage 4B — HearthLink CI reuse

- HearthLink checks out a pinned private `_devctl` commit using `DEVCTL_REPO_TOKEN`
- The exact pinned devctl is built before HearthLink verification
- HearthLink evidence is uploaded separately
- Generated `/.devctl/` state is ignored by HearthLink onboarding
- Clean CI result: Android build, tests, lint, secret scan, and Git status pass; coverage remains a blocking `FAIL` below 70%, while dependency evidence can use the resolved Gradle graph and OSV query service

### Stage 4C — HearthLink JaCoCo coverage

- HearthLink exposes `:app:jacocoTestReport` with XML and HTML reports.
- devctl reads the JaCoCo `LINE` counter and applies `80%` preferred and `70%` blocking thresholds.
- Coverage reports are copied to `.devctl/evidence/<run>/artifacts/android-coverage.xml`.
- JaCoCo scope review removed only Room KSP `*_Impl` generated classes. The report now measures `8` covered and `685` missed authored production lines (`1.15%`), so the configured blocking result remains truthful `FAIL`.
- HearthLink pins `_devctl` commit `5b4a6bf3413f16658e72f8809590093cedc8718b`; its CI run `31398424112` built that pin and uploaded the copied XML evidence artifact.

## Important commits

- `_devctl` `66c29e5e89e39c8c5e6286fc3dd1f4e5510a72a5`
- HearthLink `a4d0fd7ff6786e1663d4b74851b388e6575e3507`
- Stage 4C `_devctl` `5b4a6bf3413f16658e72f8809590093cedc8718b`
- Stage 4C HearthLink `101498b3c602c524e032f28be94af9db5630a8a3`

## Current policy facts

```text
android-coverage                 enabled, required=false, preferred=80, minimum=70
dependency-vulnerability-scan   enabled, required=false
```

## Stage 7A — Deterministic core repair and platform foundations — COMPLETE

The accepted contract is:

- `_devctl` is multi-project and deterministic.
- AI may propose or modify repository source, but devctl decides what executes.
- Runner output is bounded and captured without a stdout/stderr pipe race.
- Process failures are `FAIL`; executor and framework failures are `ERROR`.
- Windows automation verifies binary provenance before it runs.
- Project configuration is versioned and validated.
- Project identity and later per-project workflow locking remain generic concepts.

The Windows race evidence records the controlled Go environment, the resolved C compiler path and version, and the actual `go test -race -count=1 ./...` execution. The remaining Stage 7 work is outside this completed deterministic-core boundary.

## Stage 7B — Visible deterministic execution — COMPLETE

- Shared event types and sequence assignment are renderer-independent.
- Scheduler and runner emit check, process and lifecycle events.
- `devctl verify --live` renders to stderr and does not change JSON stdout, check statuses or exit codes.
- `.devctl/workflow/events.jsonl` stores lifecycle events only and `.devctl/workflow/current.md` is regenerated atomically.
- The first multi-project registry slice is implemented. It stores project/run metadata in a versioned user-state registry, uses atomic writes and a cross-process lock, recognizes committed project IDs after a path move, rejects identity collisions, and marks dead or PID-reused `running` entries as `stale`.
- Active-run checks validate the stored process start identity as well as the PID, so a later process that reuses the old PID is not treated as the original verification.
- The registry does not store check events, raw process output, evidence contents, or adapter-specific fields. Those remain in the per-project workflow and evidence locations.
- A fresh Windows matrix ran `_devctl` and HearthLink concurrently. Both were observed as `running` with separate registry entries, then finished independently. Each produced its own JSON result, workflow journal/current file and evidence directory. Live/non-live parity held for both projects.
- Killing a live `_devctl` verification left its registry entry `running`; a new verification then replaced the old run and finished successfully using process-identity stale recovery.
- The durable acceptance record is [STAGE_7B_ACCEPTANCE.md](STAGE_7B_ACCEPTANCE.md). It records the Windows/Linux process-identity boundary, representative concurrent matrix, live/non-live parity, workflow/evidence isolation, kill/restart recovery, and CLI identity cases.

Do not turn an unavailable evidence producer into `PASS`.

## Stage 7C — Controlled optional agent worker — COMPLETE / FROZEN

- The merged baseline is `99dd1e6d3c1b8c31bb617e8322c170b87b88673b`.
- The reviewed worker slice accepts only a bounded `verify` request for an approved project identity.
- The worker receives a bounded structured result and cannot select commands, checks, thresholds, policy, or exit status.
- Live rendering remains on stderr and structured worker JSON remains on stdout.
- Repair orchestration, source modification approval, retries, and the knowledge vault are intentionally outside this completed slice.

## Stage 7D-A — Controlled repair orchestration contract — COMPLETE

The contract is documented in [STAGE_7D_A_CONTROL.md](STAGE_7D_A_CONTROL.md) and frozen in commit `0156e3a7cb2e33fa53a4e68c9c686be61bc4debb`.

## Stage 7D-B — Minimal controlled repair orchestration implementation — COMPLETE / ACCEPTED

The initial implementation is in [STAGE_7D_B_IMPLEMENTATION.md](STAGE_7D_B_IMPLEMENTATION.md). `internal/repair` proves one synthetic Go repair through bounded proposal, exact approval hash, pre-apply revalidation, patch preflight, rollback-based all-or-nothing application, post-state validation, evidence, and deterministic re-verification. The cancellation hardening also requires rollback and exact baseline proof after any modification before returning `CANCELLED`. A Codex transport remains outside this slice; the visible controlled repair CLI is recorded in [STAGE_7D_C_CONTROL.md](STAGE_7D_C_CONTROL.md).

## Stage 7D-C — Human-facing controlled repair CLI — IMPLEMENTED / WINDOWS VISIBLE ACCEPTANCE PARTIAL

The design contract and Windows process acceptance record are documented in [STAGE_7D_C_CONTROL.md](STAGE_7D_C_CONTROL.md). The production CLI supplies the terminal workflow, controlled proposal-file seam, engine-owned approval evidence, observational live progress through `internal/events`, approval input, strict JSON/stdout separation and defined exit codes. Fresh visible-terminal evidence passes `A`, `R`, `C`, `D` then decision, invalid then valid input, TTY EOF, JSON separation, and the exact hash-bound apply path. A direct native visible-terminal Ctrl+C-before-application case also returned exit `4` with unchanged baseline evidence. Timing-sensitive Ctrl+C during or after mutation remains `NOT_TESTED` because the small fixture completes its apply and verification phases before a manual signal can reliably reach those windows; the deterministic package rollback tests remain the evidence for those seams. Deterministic verification-cache reuse remains outside this stage.

The compact context, lessons, cache and evidence-index interfaces are documented in [PLATFORM_CONTEXT.md](PLATFORM_CONTEXT.md).

## Stage 7E-A — Agent verification boundary — CLEAN ISOLATED ACCEPTANCE PASS

`devctl verify --agent <project>` uses the ordinary full verification path and
returns one bounded JSON object. It does not stream child output to the caller,
does not add a second scheduler or policy path, and preserves the ordinary
verification exit code. The result includes repository/check provenance and
measured raw, retained, evidence and response byte counts.

Controlled PASS and verbose FAIL fixtures passed. The verbose fixture observed
`2,949,361` child-output bytes and returned a `2,387`-byte result. A fresh
`_devctl` self-verification run `20260819T035859.010689900Z` returned exit `0`
with overall `WARN` for the expected dirty worktree; Go build, tests, race tests
and secret scan passed. The detailed checklist and acceptance record is
[STAGE_7E_A_AGENT_VERIFICATION_BOUNDARY.md](STAGE_7E_A_AGENT_VERIFICATION_BOUNDARY.md).
An independent clean snapshot also passed all seven checks with exact clean
commit provenance, a `1,903`-byte agent result and empty stderr. Hosted GitHub
Actions acceptance remains `NOT TESTED`.

## Stage 7E-B — Progressive failure and evidence retrieval — CLEAN ISOLATED ACCEPTANCE PASS

`devctl failures`, `devctl failure`, and selected `devctl evidence` retrieval
implement Levels 2–4 against one exact existing run. They do not execute checks
or follow evidence paths stored in reports. JSON is capped at `16 KiB`; selected
raw content is read in bounded chunks, normalized and redacted, with truthful
raw-byte continuation metadata.

Focused command, paging, hostile-data, split-boundary redaction, containment,
legacy evidence, worker and agent compatibility tests pass. Broad tests, vet,
diff checks and a commit-pinned clean isolated verification also pass. The
clean binary retrieved an empty PASS list and one real historical failure; a
missing raw log returned structured `evidence_unavailable` instead of following
another path. The guide and exact evidence are
[STAGE_7E_B_PROGRESSIVE_DISCLOSURE.md](STAGE_7E_B_PROGRESSIVE_DISCLOSURE.md).

## Stage 7F-A — Structured Fix Records — CLEAN ISOLATED ACCEPTANCE PASS

`devctl fixes record` accepts one strict bounded candidate and creates a
project-local `VERIFIED` record only when exact pre/post verification reports,
target check transitions, project identity, complete provenance, run order and
the current repository fingerprint satisfy `fix-closure-v1`. The record binds
the SHA-256 of both exact report files and an optional contained patch artifact.

Every record is a separate create-only normal file under
`.devctl/knowledge/fix-records`. Duplicate IDs cannot overwrite old bytes;
corrections create a new record naming `supersedes`. Stored content carries a
canonical integrity hash, and `list`, `show` and later appends fail closed when
the store is malformed or a record was edited. Concurrent same-ID writes allow
exactly one winner.

Fix Records remain distinct from advisory or generalized Lessons. Creation
does not write `.devctl/knowledge/lessons.json` or promote anything into
`knowledge/lessons.yaml`. Focused, full, race, vet, build, diff and clean
isolated-snapshot acceptance pass. The exact contract and remaining Stage
7F-B/7F-C boundary are in
[STAGE_7F_A_STRUCTURED_FIX_RECORDS.md](STAGE_7F_A_STRUCTURED_FIX_RECORDS.md).

## Stage 7F-B - authoritative knowledge and explicit promotion

The local 7F-B implementation is now present in `internal/knowledgevault`.
It stores project-local and global lesson revisions as create-only authoritative
JSON files with UUID identity, display IDs, lifecycle status, compatibility
metadata, source Fix Record IDs and content hashes. The generated lesson index
can be deleted and rebuilt from those files.

Lesson drafts without objective Fix Record evidence remain
`REQUIRES_REVIEW`. A verified Fix Record can produce a candidate, but a human
review operation is still required before `VERIFIED`. Global promotion is a
separate operation with explicit approval and sanitization for secrets,
private paths and raw-log content.

Focused, full, race, vet, build, diff and clean isolated-snapshot acceptance
pass after the 7F-C hardening repairs. Stage 7F-C deterministic metadata search
is locally accepted; staleness-aware policy remains outside this slice. Hosted
GitHub Actions and publication remain `NOT TESTED`/`NOT DONE`.

## Control-plane completion notes

Dependency evidence remains `NOT_TESTED` when supported Gradle evidence or OSV Scanner is unavailable. Evidence writing validates canonical project containment. Session state is atomic and stored outside project repositories. Windows startup and manual recovery are provided by `scripts/devctl-startup.ps1` and `scripts/devctl-recovery.ps1`. `devctl handoff` emits bounded failure evidence without changing deterministic statuses.

If a generated coverage report is a link whose canonical target escapes the project, the coverage result remains truthful but the XML is not copied; a bounded `*-not-copied.txt` marker is retained inside evidence instead of following the link.
