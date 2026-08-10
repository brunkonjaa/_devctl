# devctl status

This is the short handoff point for continuing work on `_devctl`.

Navigation: [documentation index](README.md) · [roadmap](ROADMAP.md) · [decisions](DECISIONS.md) · [lessons](LESSONS.md)

## Current state

Stage 4C is complete. HearthLink produces a JaCoCo XML report, devctl parses line coverage and applies the configured thresholds, and coverage XML is copied into the uploaded evidence tree.

Next:

```text
Stage 4D — Dependency evidence
```

Do not start dependency scanning, caching, changed-file logic, or AI escalation as part of Stage 4C.

## Completed stages

### Stage 1 — Core verification engine

- Project discovery and technology detection
- Allowlisted external commands
- Normalized check results
- Truthful statuses, including `NOT_TESTED`
- Exit codes: `0` non-blocking, `1` policy or verification failure, `2` framework error

### Stage 2 — Planner and scheduler

- Dependency graph planning and cycle detection
- Bounded concurrency and resource locks
- Dependency-failure skipping
- Cancellation and hard/inactivity timeouts
- Process-tree cleanup

### Stage 3 — Android/Gradle verification

- Java, Gradle wrapper, and Android structure checks
- Build, unit test, and lint checks
- Secret scanning
- Coverage and dependency check surfaces that truthfully report `NOT_TESTED`
- JSON policy and persistent evidence

### Stage 4A — Go self-verification

- Go detection through `go.mod`
- Go environment, build, test, and race-test checks
- Local race test reports `NOT_TESTED` when cgo or a C compiler is unavailable
- `_devctl` verifies itself on Windows and Ubuntu 24.04 CI runs the race detector
- Provenance includes devctl version, commit, policy version, and check version
- `LESSON-0002` records the ignored-source-file reproducibility failure

### Stage 4B — HearthLink CI reuse

- HearthLink checks out a pinned private `_devctl` commit using `DEVCTL_REPO_TOKEN`
- The exact pinned devctl is built before HearthLink verification
- HearthLink evidence is uploaded separately
- Generated `.devctl/evidence/` no longer causes a Git warning
- Clean CI result: Android build, tests, lint, secret scan, and Git status pass; coverage and dependency scanning remain `NOT_TESTED`; exit `0`

### Stage 4C — HearthLink JaCoCo coverage

- HearthLink exposes `:app:jacocoTestReport` with XML and HTML reports.
- devctl reads the JaCoCo `LINE` counter and applies `80%` preferred and `70%` blocking thresholds.
- Coverage reports are copied to `.devctl/evidence/<run>/artifacts/android-coverage.xml`.
- JaCoCo scope review removed only Room KSP `*_Impl` generated classes. The report now measures `8` covered and `685` missed authored production lines (`1.15%`), so the configured blocking result remains truthful `FAIL`.
- HearthLink pins `_devctl` commit `5b4a6bf3413f16658e72f8809590093cedc8718b`; its CI run `31398424112` built that pin and uploaded the copied XML evidence artifact.

## Important commits

- `_devctl` `66c29e5e89e39c8c5e6286fc3dd1f4e5510a72a5`
- HearthLink `a4d0fd7ff6786e1663d4b74851b388e6575e3507`
- Stage 4C `_devctl` `5b4a6bf3413f16658e72f8809590093cedc8718b`
- Stage 4C HearthLink `101498b3c602c524e032f28be94af9db5630a8a3`

## Current policy facts

```text
android-coverage                 enabled, required=false, preferred=80, minimum=70
dependency-vulnerability-scan   enabled, required=false
```

Do not turn an unavailable evidence producer into `PASS`.
