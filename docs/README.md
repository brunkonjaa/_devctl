# devctl documentation

Start here when continuing work in a new chat:

1. [Current status](STATUS.md)
2. [Roadmap](ROADMAP.md)
3. [Architecture decisions](DECISIONS.md)
4. [Lesson index](LESSONS.md)
5. [Stage 7B acceptance record](STAGE_7B_ACCEPTANCE.md)
6. [Platform context and knowledge boundary](PLATFORM_CONTEXT.md)
7. [Stage 7D-C repair CLI control and acceptance](STAGE_7D_C_CONTROL.md)

The structured lesson data is in [`../knowledge/lessons.yaml`](../knowledge/lessons.yaml).

Daily operations:

- `devctl verify <project>` runs deterministic checks and records session state.
- `devctl verify --live <project>` renders the same verification events to stderr while checks run.
- `devctl worker verify [--live] --request <request.json>` accepts one versioned external-worker verification request and returns a structured result; `--live` renders the same deterministic events to stderr.
- `devctl session resume` shows the last saved project state.
- `scripts/devctl-startup.ps1 -OpenWorkspace` is the Windows startup entry point.
- `scripts/devctl-recovery.ps1 -ProjectPath <path>` records a project manually after startup recovery.
- `devctl handoff <evidence>\\report.json` produces bounded failure evidence for investigation.
- `devctl context --json <project>` produces a bounded current-state package for an optional coding agent.
- `devctl lessons query --json <project>` retrieves relevant successful and failed approaches.
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
