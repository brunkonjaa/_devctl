# devctl roadmap

Keep increments narrow enough that a CI result explains what changed.

Navigation: [documentation index](README.md) · [current status](STATUS.md) · [decisions](DECISIONS.md) · [lessons](LESSONS.md)

## Completed: Stage 4C — HearthLink JaCoCo coverage

1. Configure JaCoCo in HearthLink.
2. Make Gradle produce an XML coverage report.
3. Make devctl locate and parse the report.
4. Apply thresholds: `>=80%` PASS, `70%` to `<80%` WARN, `<70%` FAIL and blocking.
5. Upload coverage evidence and run HearthLink CI.

Leave dependency vulnerability scanning as `NOT_TESTED`.

The JaCoCo scope review excluded Room KSP `*_Impl` generated classes only; it measured `8/693` authored production lines (`1.15%`), below the `70%` minimum. HearthLink CI run `31398424112` used pinned devctl commit `5b4a6bf3413f16658e72f8809590093cedc8718b`, preserved the blocking `FAIL`, and uploaded the copied coverage XML with the result evidence.

## Completed: Stage 4D — Dependency evidence

- Add supported Gradle dependency evidence.
- Run OSV Scanner only when usable evidence exists.
- Normalize findings with source, tool version, project, and evidence path.
- Keep missing evidence truthful and policy-controlled.

The scanner remains `NOT_TESTED` when supported dependency evidence or the scanner is unavailable. Malformed scanner output is `ERROR`, and parsed findings retain package, advisory, source, and evidence information.

## Completed: Stage 5 — Session state and startup recovery

- Save the last project, branch, commit, task, verification result, CI result, and timestamp.
- Add a deterministic Windows startup summary.
- Handle missing network and stale state without failing startup.
- Keep state small and free of secrets.

`devctl session record`, `status`, and `resume` use an atomic secret-free JSON file under the user config directory. `scripts/devctl-startup.ps1` is the startup entry point and `scripts/devctl-recovery.ps1` is the manual recovery path.

## Completed: Stage 6 — Failure evidence and AI handoff

- Extract concise evidence from failed checks.
- Route only unresolved or reasoning-heavy evidence to AI.
- Let devctl remain the authority for deterministic re-verification.

`devctl handoff` emits a bounded packet from an existing `report.json`. It contains failed or blocking checks, findings, evidence paths, and provenance without raw logs or project command execution.

## Current: Stage 7C — Controlled optional agent worker

Stage 7 is a multi-project platform milestone. The core must work for `_devctl`, HearthLink and Smart Schedule without putting project-specific rules into the workflow controller.

### Stage 7A — Deterministic core repair and platform foundations

- Complete. The deterministic core, Windows bootstrap, configuration boundary, project identity, controlled Go race environment and execution provenance are in place.

### Stage 7B — Visible deterministic execution — COMPLETE

- Shared lifecycle and process events now flow from verification, scheduler and runner through observer sinks.
- `devctl verify --live` renders to stderr, including live child output, while `--json` remains valid on stdout.
- `.devctl/workflow/current.md` is regenerated atomically and lifecycle events are bounded in `.devctl/workflow/events.jsonl`.
- The first multi-project registry slice stores project identity, path, run state, status, timestamps and active process identity in a versioned user-state file.
- Registry updates use a lock and atomic replacement. Stale or PID-reused process state and committed-ID path moves are handled without moving event or evidence data into the registry.
- Windows `GetProcessTimes` provenance now makes the active process identity meaningful; a reused PID is treated as stale rather than active.
- A fresh concurrent `_devctl` plus HearthLink matrix verified independent registry entries, per-project workflow/evidence outputs and live/non-live parity. A killed run was also restarted successfully through stale recovery.
- Acceptance is recorded in [STAGE_7B_ACCEPTANCE.md](STAGE_7B_ACCEPTANCE.md). No further feature work is planned for this stage.

### Stage 7C — Controlled optional agent worker

- Add explicit approval before file modification.
- Send bounded task and failure packets to Codex.
- Route `FAIL` back to Codex and stop on `ERROR`.
- Enforce retry, repeated-failure and human-review limits.

Hard exclusions for Stage 7 are automatic commits, pushes, merges, AI-created shell commands, AI changes to policy thresholds, test disabling and unbounded repair loops.

## Later

- Changed-file and incremental verification
- Repository reproducibility checks
- More technology adapters and verification packs
- Central rules, questions, and lesson inheritance
- GitHub CI result retrieval
- Multi-project reporting
- Windows/Linux packaging
- Caching with provenance and invalidation
- Controlled AI escalation

The system should select applicable checks from project type, configuration, dependencies, resources, and eventually changed files. It should not run every available check on every project.
