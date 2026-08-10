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
