# devctl roadmap

Keep increments narrow enough that a CI result explains what changed.

Navigation: [documentation index](README.md) · [current status](STATUS.md) · [decisions](DECISIONS.md) · [lessons](LESSONS.md)

## Next: Stage 4C — HearthLink JaCoCo coverage

1. Configure JaCoCo in HearthLink.
2. Make Gradle produce an XML coverage report.
3. Make devctl locate and parse the report.
4. Apply thresholds: `>=80%` PASS, `70%` to `<80%` WARN, `<70%` FAIL and blocking.
5. Upload coverage evidence and run HearthLink CI.

Leave dependency vulnerability scanning as `NOT_TESTED`.

Local implementation is complete. The remaining integration step is to commit the devctl changes, update HearthLink's pinned private devctl commit, and run CI. The JaCoCo scope review excluded Room KSP `*_Impl` generated classes only; it measured `8/693` authored production lines (`1.15%`), below the `70%` minimum.

## Stage 4D — Dependency evidence

- Add supported Gradle dependency evidence.
- Run OSV Scanner only when usable evidence exists.
- Normalize findings with source, tool version, project, and evidence path.
- Keep missing evidence truthful and policy-controlled.

## Stage 5 — Session state and startup recovery

- Save the last project, branch, commit, task, verification result, CI result, and timestamp.
- Add a deterministic Windows startup summary.
- Handle missing network and stale state without failing startup.
- Keep state small and free of secrets.

## Stage 6 — Failure evidence and AI handoff

- Extract concise evidence from failed checks.
- Route only unresolved or reasoning-heavy evidence to AI.
- Let devctl remain the authority for deterministic re-verification.

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
