# devctl documentation

Start here when continuing work in a new chat:

1. [Current status](STATUS.md)
2. [Roadmap](ROADMAP.md)
3. [Architecture decisions](DECISIONS.md)
4. [Lesson index](LESSONS.md)
5. [Stage 7B acceptance record](STAGE_7B_ACCEPTANCE.md)
6. [Platform context and knowledge boundary](PLATFORM_CONTEXT.md)
7. [Stage 7D-C repair CLI control and acceptance](STAGE_7D_C_CONTROL.md)
8. [Stage 7E-A agent verification boundary](STAGE_7E_A_AGENT_VERIFICATION_BOUNDARY.md)
9. [Stage 7E-B progressive failure and evidence retrieval](STAGE_7E_B_PROGRESSIVE_DISCLOSURE.md)
10. [Stage 7F-A structured Fix Records](STAGE_7F_A_STRUCTURED_FIX_RECORDS.md)
11. [Stage 7F-B authoritative knowledge](STAGE_7F_B_AUTHORITATIVE_KNOWLEDGE.md)
12. [Stage 7F-C deterministic search](STAGE_7F_C_DETERMINISTIC_SEARCH.md)

The older structured lesson index is in [`../knowledge/lessons.yaml`](../knowledge/lessons.yaml).
Stage 7F-B authoritative lesson files are created under the project-local or
global `knowledge/authoritative-lessons` directories described in its stage
document.

Stage 7E-A, 7E-B, 7F-A and local 7F-B/7F-C pass focused, broad and clean
isolated-checkout acceptance. Hosted GitHub Actions remains `NOT TESTED` until
these changes are explicitly published and the remote workflow passes. Stage
7F-C deterministic search is locally accepted; later staleness-aware policy is
outside this slice.
IDs and explicit promotion of reviewed Fix Records into reusable lessons.

Daily operations:

- `devctl verify <project>` runs deterministic checks and records session state.
- `devctl verify --live <project>` renders the same verification events to stderr while checks run.
- `devctl verify --agent <project>` runs the same full verification while returning one bounded JSON result and keeping child output in local evidence.
- `devctl failures <run-id> --json --project <project>` returns bounded failed or unresolved check summaries from one existing run.
- `devctl failure <run-id> <check-id> --json --project <project>` returns one bounded normalized failure without raw output.
- `devctl evidence <run-id> <failure-id> --json --project <project>` returns one redacted bounded raw-evidence fragment; use its raw-byte `next_offset` for continuation.
- `devctl worker verify [--live] --request <request.json>` accepts one versioned external-worker verification request and returns a structured result; `--live` renders the same deterministic events to stderr.
- `devctl session resume` shows the last saved project state.
- `scripts/devctl-startup.ps1 -OpenWorkspace` is the Windows startup entry point.
- `scripts/devctl-recovery.ps1 -ProjectPath <path>` records a project manually after startup recovery.
- `devctl handoff <evidence>\\report.json` produces bounded failure evidence for investigation.
- `devctl context --json <project>` produces a bounded current-state package for an optional coding agent.
- `devctl lessons query --json <project>` retrieves relevant successful and failed approaches.
- `devctl fixes record --input <candidate.json> <project>` closes one fix only when exact pre/post evidence and the current project fingerprint satisfy the Stage 7F-A rule.
- `devctl fixes list --json <project>` lists bounded immutable project-local Fix Records without running verification.
- `devctl fixes show --json <project> <fix-id>` validates and returns one stored Fix Record without changing project state.
- `devctl knowledge candidate --input <draft.json> <project>` stores a non-trusted lesson candidate.
- `devctl knowledge review --id <uuid> --reviewer <name> [--approve] <project>` appends a reviewed lifecycle revision.
- `devctl knowledge promote --id <uuid> --global-root <root> --reviewer <name> --approve <project>` explicitly publishes a sanitized local lesson.
- `devctl knowledge search [flags] [query]` deterministically searches local and global authoritative lessons.
- `devctl knowledge show [--global] [<root>] <lesson-id-or-display-id>` reads one current authoritative lesson.
- `devctl evidence rebuild --json <project>` rebuilds the evidence index without changing historical reports.
- `devctl cache status --json <project>` inspects advisory cache entries and validity metadata.

Windows setup:

1. Install Go. `scripts/devctl-bootstrap.ps1` builds or refreshes the canonical binary under `%LOCALAPPDATA%\devctl\bin` after checking source provenance.
2. Run `scripts/Register-DevctlStartup.ps1` once as the interactive user. It launches the bootstrapper, which validates or rebuilds devctl before the daily prompt.
3. Use `scripts/devctl-recovery.ps1 -ProjectPath <path>` when the saved path is missing or stale.

`devctl version --json` reports the devctl version, source commit, dirty-source state and Go version. The Windows startup task must use the bootstrapper rather than an arbitrary executable copied into the repository.

The Stage 7C worker boundary is defined in [STAGE_7C_CONTROL.md](STAGE_7C_CONTROL.md). The first slice accepts only `operation: verify`; it does not accept commands, shell text, thresholds, check overrides, retries, or repair instructions.

Session state is stored under the user configuration directory, normally `%APPDATA%\\devctl\\session.json`. It is separate from `.devctl/evidence/`, which remains project-local verification evidence.

Live verification also writes lifecycle events to `.devctl/workflow/events.jsonl` and regenerates `.devctl/workflow/current.md`. Child-process output is rendered live but is not copied into the workflow event journal.

The multi-project registry is stored outside repositories under the devctl user state directory, normally `%APPDATA%\\devctl\\registry.json`. It contains project identity and current run metadata only. Check events, process output and verification evidence remain in each project's existing workflow and evidence directories. Registry writes are atomic, and a dead process or reused PID leaves a `stale` state that can be detected on the next read.

The prompt offers `Continue with current task`, `Continue with overall check`, and `Exit this check and use Windows`. The current task and project are taken from the latest saved session, regardless of which project was last used. If session state is missing or stale, the newest Git project under `C:\Projects` is used instead. Decisions are recorded in session state. The current-task option writes `%APPDATA%\\devctl\\codex-handoff.md` with the task, repository state, and overview, then opens that handoff with the workspace in VS Code. The overall-check option runs `devctl verify` for the saved project and shows its result before opening the workspace.
