# devctl roadmap

Keep increments narrow enough that a CI result explains what changed.

Navigation: [documentation index](README.md) · [current status](STATUS.md) · [decisions](DECISIONS.md) · [lessons](LESSONS.md)

## Completed: Stage 4C — HearthLink JaCoCo coverage

1. Configure JaCoCo in HearthLink.
2. Make Gradle produce an XML coverage report.
3. Make devctl locate and parse the report.
4. Apply thresholds: `>=80%` PASS, `70%` to `<80%` WARN, `<70%` FAIL and blocking.
5. Upload coverage evidence and run HearthLink CI.

Keep dependency vulnerability scanning truthful while the adapter is being completed.

The JaCoCo scope review excluded Room KSP `*_Impl` generated classes only; it measured `8/693` authored production lines (`1.15%`), below the `70%` minimum. HearthLink CI run `31398424112` used pinned devctl commit `5b4a6bf3413f16658e72f8809590093cedc8718b`, preserved the blocking `FAIL`, and uploaded the copied coverage XML with the result evidence.

## Completed: Stage 4D — Dependency evidence

- Add supported Gradle dependency evidence.
- Run OSV Scanner only when usable evidence exists.
- Normalize findings with source, tool version, project, and evidence path.
- Keep missing evidence truthful and policy-controlled.

Android projects use OSV Scanner when it is installed. When it is not installed,
the adapter resolves the release runtime Gradle graph and queries OSV's bounded
`querybatch` API for the Maven components. Missing Gradle evidence, an unavailable
Gradle graph, or an unavailable OSV service remains `NOT_TESTED`; malformed output
is `ERROR`, and parsed findings retain package, advisory, source, tool and evidence
information.

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

## Completed locally: Stage 7E-A/B agent boundary and progressive disclosure

- The controlled repair CLI is wired as a thin adapter over `internal/repair.Run`.
- `context`, `status`, `history`, `lessons`, `cache` and `evidence` provide stable local JSON/text seams for AI agents and developers.
- Lessons are persisted as bounded structured records, cache entries carry fingerprints, and evidence indexes are rebuildable.
- Fresh visible Windows TTY evidence passes the ordinary approval matrix and Ctrl+C before application. Post-mutation Ctrl+C timing and fault-injection rows remain explicitly `NOT_TESTED`; the deterministic repair tests cover those rollback seams. Deterministic verification-cache reuse remains later work.
- `devctl verify --agent <project>` now runs the existing full verification path and returns one bounded structured result without streaming child output to the caller.
- Local PASS, verbose FAIL, fatal-error, hard-size, worker-compatibility and `_devctl` self-verification evidence is recorded in [STAGE_7E_A_AGENT_VERIFICATION_BOUNDARY.md](STAGE_7E_A_AGENT_VERIFICATION_BOUNDARY.md).
- `failures`, `failure`, and selected `evidence` retrieval now expose Levels
  2–4 from one exact existing run without executing project commands.
- Focused paging, containment, hostile-data, split-boundary redaction,
  legacy-compatibility and compiled CLI checks pass. Broad tests, race tests,
  vet and clean isolated-checkout acceptance also pass. Hosted GitHub Actions
  remains `NOT TESTED` until the changes are explicitly published.

## Completed locally: Stage 7F-A structured Fix Records

- The verified fix-closure rule and append-only record boundary are frozen and
  implemented.
- Fix Records remain distinct from reusable Lessons.
- Accepted records bind exact report hashes, repository/check provenance and
  the current post-fix project fingerprint.
- Focused, broad, race, vet, build, diff and clean isolated-snapshot acceptance
  pass. Hosted GitHub Actions remains `NOT TESTED` until publication is
  explicitly authorized.

## Stage 7F-B scope: authoritative knowledge and explicit promotion

- Define authoritative reviewed knowledge files separately from generated
  indexes and project-local Fix Records.
- Introduce globally unique knowledge IDs without rewriting Stage 7F-A record
  history.
- Make promotion from one or more verified Fix Records into a reusable Lesson
  explicit, reviewed and reversible through new records rather than mutation.
- Freeze the knowledge acceptance matrix before adding cross-project search.
- Keep compatibility and staleness decisions explicit; they remain Stage
  7F-B/7F-C work rather than an AI inference.

## Completed locally: Stage 7F-B authoritative knowledge and explicit promotion

- Project-local and global lesson revisions are separate authoritative files;
  generated indexes are rebuildable summaries only.
- UUID machine IDs are separate from human display IDs. Revisions are
  create-only and linked by content hash.
- A verified Fix Record can create a candidate, but objective source checks and
  explicit review are required before `VERIFIED`.
- Global publication requires an explicit reviewer approval and fails closed
  for secret-like, raw-log or private-path material.
- Semantic search and staleness-aware compatibility policy remain outside the
  deterministic Stage 7F-C slice.

## Completed locally: Stage 7F-C deterministic cross-project search

- Search reads project-local and global authoritative lesson revisions directly;
  generated indexes are not treated as truth.
- Default retrieval includes only current `VERIFIED` lessons. Lifecycle history
  requires an explicit `--include-history` request.
- Fixed metadata filters and weighted token matching provide deterministic
  ordering without embeddings, AI or current-defect verification claims.
- Results use a bounded response envelope that reports total, returned and
  truncation state. They retain lesson, Fix Record and bounded evidence-path
  provenance without returning raw evidence.

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

### Stage 7E-A — Agent verification boundary — CLEAN ISOLATED ACCEPTANCE PASS

- `verify --agent` uses the existing verification engine, scheduler, checks,
  policy, evidence writer and exit-code calculation.
- Agent stdout contains one schema-versioned result capped at `16 KiB`;
  ordinary agent execution keeps stderr empty.
- The result includes repository and check provenance, complete current check
  statuses, bounded normalized failures, and information-flow byte counts.
- A controlled verbose failure reduced `2,949,361` observed child bytes to a
  `2,387`-byte agent result while retaining a bounded `1,048,576`-byte raw
  test log locally.
- Fresh `_devctl` self-verification returned exit `0` and overall `WARN` only
  for the expected dirty worktree; build, tests, race tests and secret scan
  passed.
- A clean isolated commit-pinned run passed all seven checks with exact clean
  provenance and empty stderr. Hosted GitHub Actions remains `NOT TESTED`.

### Stage 7E-B — Progressive failure and evidence retrieval — CLEAN ISOLATED ACCEPTANCE PASS

- `failures` returns ordered failed or unresolved check summaries with
  verification and check provenance.
- `failure` returns one normalized failure and bounded finding pages without
  `RawOutput`.
- `evidence` returns only a requested generated raw-check fragment, with secret
  redaction, terminal-control removal, a 2 KiB raw read bound and byte-oriented
  continuation metadata.
- Every JSON result is capped at 16 KiB and reports exact serialized bytes,
  total/returned counts, truncation and a next command where applicable.
- Run/check IDs, report identity, duplicate checks, canonical containment,
  normal-file requirements and missing evidence fail closed.
- Existing `evidence rebuild|latest`, ordinary verification, worker and agent
  behavior remain covered by focused tests.
- The CI workflow now validates `verify --agent` from a clean commit-pinned
  checkout. Hosted clean CI remains `NOT_TESTED` until that workflow runs.
- Broad tests, race tests, vet, clean provenance validation and compiled
  Level 2/3 retrieval pass. Controlled Level 4 fixtures cover bounded raw-byte
  paging, split-boundary redaction and containment failures.
- Acceptance details are recorded in
  [STAGE_7E_B_PROGRESSIVE_DISCLOSURE.md](STAGE_7E_B_PROGRESSIVE_DISCLOSURE.md).

### Stage 7F-A — Structured Fix Records — CLEAN ISOLATED ACCEPTANCE PASS

- `fixes record` reads one strict bounded candidate and derives `VERIFIED`,
  timestamps, project/check provenance, exact report hashes and the change
  fingerprint from deterministic evidence.
- Closure requires distinct ordered runs, unresolved target checks before the
  fix, exact target `PASS` afterward, a non-blocking complete post report and a
  current repository fingerprint equal to the post-fix report.
- Optional patch evidence must be a bounded normal file canonically contained
  under `.devctl/evidence` with an exact SHA-256 match.
- Records are individual create-only files. Duplicate IDs, malformed stores,
  tampering, path escapes and unsafe candidate content fail closed.
- Supersession appends a new record and leaves old bytes unchanged. Concurrent
  same-ID writers allow exactly one successful create.
- `fixes list` and `fixes show` validate stored records without running checks
  or requiring the project to remain at the historical fingerprint.
- Fix Record creation cannot write or promote a Lesson.
- Acceptance details are recorded in
  [STAGE_7F_A_STRUCTURED_FIX_RECORDS.md](STAGE_7F_A_STRUCTURED_FIX_RECORDS.md).

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
