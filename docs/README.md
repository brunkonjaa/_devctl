# devctl documentation

Start here when continuing work in a new chat:

1. [Current status](STATUS.md)
2. [Roadmap](ROADMAP.md)
3. [Architecture decisions](DECISIONS.md)
4. [Lesson index](LESSONS.md)

The structured lesson data is in [`../knowledge/lessons.yaml`](../knowledge/lessons.yaml).

Daily operations:

- `devctl verify <project>` runs deterministic checks and records session state.
- `devctl session resume` shows the last saved project state.
- `scripts/devctl-startup.ps1 -OpenWorkspace` is the Windows startup entry point.
- `scripts/devctl-recovery.ps1 -ProjectPath <path>` records a project manually after startup recovery.
- `devctl handoff <evidence>\\report.json` produces bounded failure evidence for investigation.

Windows setup:

1. Install Go and build `devctl.exe` with `go build -o devctl.exe ./cmd/devctl`.
2. Run `scripts/Register-DevctlStartup.ps1` once as the interactive user. It prompts at logon and daily at 09:00.
3. Use `scripts/devctl-recovery.ps1 -ProjectPath <path>` when the saved path is missing or stale.

Session state is stored under the user configuration directory, normally `%LOCALAPPDATA%\\devctl\\session.json`. It is separate from `.devctl/evidence/`, which remains project-local verification evidence.

The prompt offers `Continue`, `Snooze`, and `Skip today`. Decisions are recorded in session state; `Snooze` and `Skip today` do not open the workspace.
