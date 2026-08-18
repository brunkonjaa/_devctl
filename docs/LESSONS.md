# devctl lesson index

The structured source of truth is `knowledge/lessons.yaml`. This file is a quick index for a new session.

Navigation: [documentation index](README.md) · [current status](STATUS.md) · [roadmap](ROADMAP.md) · [decisions](DECISIONS.md)

## LESSON-0001 — Healthy long-running tools can exceed a fixed timeout

Area: process execution.

A fixed wall-clock timeout terminated a healthy Gradle workload. The correction was per-check hard and inactivity timeouts, streamed output tracking, and complete process-tree cleanup.

## LESSON-0002 — Local verification can pass with required ignored files

Area: repository reproducibility.

A required source file existed locally but was excluded by `.gitignore`. Local verification passed, while clean CI failed because the file was absent from the repository.

The current deterministic safeguard is clean CI verification. A future repository-state check may classify suspicious ignored or untracked source-like files, but should not treat every ignored file as a defect.

## LESSON-0003 — Visible acceptance must preserve the real terminal boundary

Area: process acceptance.

A redirected PowerShell wrapper is not proof of a visible Windows terminal
interaction. The corrected acceptance boundary uses a fresh committed FAIL
fixture for every case, asserts HEAD/status/hash/baseline immediately before
launch, never injects approval input, keeps the real child streams visible, and
records the child PID, exit code, hashes, file content and Git status. Approval
is line-based, so the operator types `A`, `R`, `D`, or `C` and presses Enter.

Ctrl+C timing must be tested directly in an ordinary visible terminal. If a
small fixture completes before a requested post-mutation timing window can be
reached, the row stays `NOT_TESTED` and is tied to the deterministic rollback
tests instead of being reported as process-level evidence.

## Project-wide lessons

The following rules apply across `_devctl`, not only Stage 7D-C:

- `LESSON-0004` — Keep execution status separate from policy blocking. Never
  turn `WARN`, `NOT_TESTED`, or `ERROR` into a convenient `PASS`.
- `LESSON-0005` — Resolve canonical evidence paths and reject link-aware
  containment escapes. Generated evidence is not project source.
- `LESSON-0006` — Optional workers receive bounded requests and summaries, not
  commands, raw logs, thresholds, or authority over deterministic decisions.
- `LESSON-0007` — A Windows PID needs process-start identity. PID reuse alone
  is not proof that a recorded process is still running.
- `LESSON-0008` — Registry and workflow state are observational. Collisions or
  stale-state recovery must not change the project's check vector or exit code.
- `LESSON-0009` — Cancellation after mutation requires exact rollback proof;
  rollback failure is an error with exit `2`.
- `LESSON-0010` — Approval binds immutable canonical patch bytes to one bounded
  repair attempt. No regenerated patch, retry, commit, push, or merge follows.
- `LESSON-0011` — Evidence and cache freshness require a full relevant
  fingerprint. A dirty boolean alone is not enough.
- `LESSON-0012` — Missing CGO or a C compiler blocks race acceptance; it is not
  a passing result.
- `LESSON-0013` — Canonical patch bytes use UTF-8/LF and repository text files
  need an explicit cross-platform line-ending policy.
- `LESSON-0014` — Generated evidence, local cache, source staging and Git
  publication are separate boundaries requiring separate review.

## Lesson workflow

```text
failure
→ root cause
→ why existing checks missed it
→ deterministic rule or review question
→ regression coverage
→ structured lesson
```

Do not record ordinary coding mistakes unless they reveal a reusable weakness in the engineering or verification system.
