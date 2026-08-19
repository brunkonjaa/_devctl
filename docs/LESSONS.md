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
- `LESSON-0015` — Serialize checks that share a build cache through one named
  scheduler resource. Do not hide contention by extending verification limits.
- `LESSON-0016` — Evidence redaction carries line and private-key state across
  byte pages and never emits an unfinished line before it can be classified.
- `LESSON-0017` — Hard response encoders update returned counts whenever they
  remove an item after the initial protocol object was built.
- `LESSON-0018` — A verified fix status must be derived from exact pre/post
  evidence and the current repository fingerprint, never accepted from prose.
- `LESSON-0019` — Knowledge history is append-only: corrections supersede old
  records, while malformed or edited existing records block further appends.
- `LESSON-0020` — Structured control input rejects unknown fields, duplicate
  keys, extra JSON values and unbounded content before it reaches authority.
- `LESSON-0021` — Deterministic validation checks fields in a fixed order; it
  never chooses the first reported failure by ranging over a Go map.
- `LESSON-0022` — Secret-handling tests construct scanner-shaped values at
  runtime instead of embedding a repository-scannable secret literal.
- `LESSON-0023` — Byte-exact CLI acceptance uses process-level stdout/stderr
  files; Windows PowerShell pipeline redirection can transcode protocol bytes.

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
