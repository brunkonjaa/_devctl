# Stage 7B acceptance record

Date recorded: 2026-08-17

## Decision

Stage 7B — visible deterministic execution — is complete.

The accepted boundary is:

```text
verification / scheduler / runner
              |
              v
        shared event stream
          |           |
          v           v
       evidence    --live renderer
```

The renderer observes execution. It does not select checks, alter statuses,
or control the verification result. The multi-project registry observes and
stores project/run metadata, while workflow journals and evidence remain
project-local.

## Environment and provenance

- Host: Windows 11 workstation
- Go race environment: `GOENV=off`, `CGO_ENABLED=1`
- C compiler: `C:\msys64\ucrt64\bin\gcc.exe`
- Compiler version: MSYS2 GCC 16.1.0
- Representative projects: `_devctl` (Go) and HearthLink (Android/Gradle)
- Registry state: temporary user-state directory for the concurrent matrix

## Required command results

| Command | Result |
| --- | --- |
| `go test ./...` | PASS |
| `go vet ./...` | PASS |
| `go test -race -count=1 ./...` with the controlled Windows race environment | PASS |

## Event and workflow checks

- Shared event sequence is monotonic within each `run_id`.
- Sequence is interpreted as `(run_id, sequence)` in a retained multi-run journal.
- Process output is rendered live but is not persisted in `events.jsonl`.
- Lifecycle events remain lossless and the journal append bound is enforced.
- `_devctl` and HearthLink each produced an independent `current.md`,
  `events.jsonl`, and evidence directory during the concurrent matrix.
- `--json` output remained parseable while `--live` output was sent to stderr.

The concurrent live matrix observed both projects as `running` at the same
time with different project IDs, run IDs, and PIDs. They finished independently.
The final representative results were:

```text
_devctl     WARN  exit 0
HearthLink  FAIL  exit 1
```

The HearthLink result is a deterministic project result. It is not treated as
a registry failure or converted to `PASS`.

Live/non-live comparisons produced equal check vectors and equal overall
statuses for both representative projects.

## Registry and process recovery checks

- Atomic registry replacement and cross-process locking preserved valid state.
- Registry infrastructure failure did not change verification output, check
  vectors, overall status, or exit behavior.
- A killed live `_devctl` process left its raw registry entry as `running`.
  The next verification rejected neither the project nor its committed
  identity: process-start mismatch made the old run stale and the new run
  finished under a new `run_id`.
- Windows active/stale decisions use PID plus `GetProcessTimes` start identity.
- Linux active/stale decisions use PID plus `/proc/<pid>/stat` start time.
- If `/proc/<pid>/stat` is unavailable on another Unix platform, the fallback
  identity is `pid:<PID>`. That platform therefore has PID-only protection and
  does not receive the Linux-level PID-reuse guarantee.

## CLI identity checks

The actual `devctl verify` command was used for the final identity cases:

### Moved project

```text
project_id: cli-move-id
old path:   .../move-old
new path:   .../move-new
```

After the filesystem move, the existing registry entry was updated to the new
canonical path. No duplicate project was created and verification continued.

### Identity collision

Two existing repositories claimed:

```text
project_id: cli-collision-id
```

The second invocation reported the registry update and run-state warnings,
while the registry remained associated with the original repository. The
second repository's deterministic verification still ran and returned its own
result. This preserves the control-plane boundary:

```text
registry problem       -> warning
verification truth     -> unchanged
```

## Stage boundary

Stage 7A is complete. Stage 7B event infrastructure, correctness hardening,
multi-project state, process recovery, representative concurrent execution,
and CLI identity handling are complete.

Stage 7C — controlled optional Codex worker — has not started. Automatic
commits, pushes, merges, AI-created shell commands, policy-threshold changes,
test disabling, and unbounded repair loops remain excluded.
