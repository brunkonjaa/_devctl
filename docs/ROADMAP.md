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

## Current: Stage 7D-C and platform context — implementation complete, visible acceptance partial

- The controlled repair CLI is wired as a thin adapter over `internal/repair.Run`.
- `context`, `status`, `history`, `lessons`, `cache` and `evidence` provide stable local JSON/text seams for AI agents and developers.
- Lessons are persisted as bounded structured records, cache entries carry fingerprints, and evidence indexes are rebuildable.
- Fresh visible Windows TTY evidence passes the ordinary approval matrix and Ctrl+C before application. Post-mutation Ctrl+C timing and fault-injection rows remain explicitly `NOT_TESTED`; the deterministic repair tests cover those rollback seams. Deterministic verification-cache reuse remains later work.

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

### Stage 7C — Controlled optional agent worker — COMPLETE / FROZEN

- First slice merged in `99dd1e6d3c1b8c31bb617e8322c170b87b88673b`.
- `worker verify` uses the existing deterministic verification path and returns a bounded versioned result.
- Project identity is registry-approved and revalidated before execution.
- No repair loop, file modification approval flow, knowledge vault, or worker retry is included.

### Stage 7D-A — Controlled repair orchestration contract — COMPLETE

- Frozen in `0156e3a7cb2e33fa53a4e68c9c686be61bc4debb`.
- The exact-diff, clean-baseline, pre-apply revalidation, all-or-nothing, and fail-closed contract is documented in [STAGE_7D_A_CONTROL.md](STAGE_7D_A_CONTROL.md).

### Stage 7D-B — Minimal controlled repair orchestration implementation — COMPLETE / ACCEPTED

- Implement one bounded repair attempt in `internal/repair`.
- Use the synthetic Go fixture only.
- Keep proposal, approval, and deterministic verification as explicit seams.
- Post-modification cancellation now rolls back and proves the exact baseline
  before returning `CANCELLED`; provider context and engine-owned approval
  evidence are exposed for the next human-facing boundary.
- Test the positive lifecycle and negative matrix before adding a real worker transport or CLI.

### Stage 7D-C — Human-facing controlled repair CLI — IMPLEMENTED / WINDOWS VISIBLE ACCEPTANCE PARTIAL

- Define the terminal presentation and approval boundary above `internal/repair.Run`.
- Preserve exact patch approval, fail-closed behavior, evidence, and one-attempt limits from Stage 7D-B.
- Keep the proposal provider controlled and do not connect an external AI transport.
- The production CLI and local context/knowledge/cache/evidence slices are implemented. Visible Windows evidence passes the ordinary terminal matrix and Ctrl+C before application. The remaining process rows are timing-sensitive Ctrl+C after mutation, rollback-failure process mapping, and selected final WARN/FAIL/ERROR fixtures; package-level fault-injection evidence is recorded separately.

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
