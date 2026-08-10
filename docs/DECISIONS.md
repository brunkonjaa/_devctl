# devctl architecture decisions

Navigation: [documentation index](README.md) · [current status](STATUS.md) · [roadmap](ROADMAP.md) · [lessons](LESSONS.md)

## Deterministic work stays deterministic

Builds, tests, lint, secret scans, coverage parsing, dependency scans, Git inspection, scheduling, policy evaluation, and evidence writing use ordinary tools and code.

AI is reserved for implementation, interpretation, root-cause analysis, and questions that cannot be established objectively.

## Go is the current implementation language

Go was selected for the control plane because it provides reliable process orchestration, bounded concurrency, cancellation, portable filesystem and Git operations, JSON handling, memory safety, and practical Windows/Linux single-executable distribution.

Python may still be used for specialized analysis tools. Rust is a possible later choice for high-risk or performance-sensitive components, but no rewrite is currently planned.

## Projects keep independent Git repositories

`C:\Projects\_devctl` is the central automation repository. HearthLink and future projects remain separate repositories.

## Checks declare; the scheduler decides

Adapters declare check IDs, dependencies, resources, timeouts, and runners. The central planner and scheduler decide execution order, concurrency, locks, cancellation, and dependency skipping.

Logical dependencies and resource locks are different concepts.

## Evidence is truthful and reconstructable

`PASS`, `WARN`, `FAIL`, `ERROR`, `SKIP`, `NOT_TESTED`, `NOT_APPLICABLE`, `INSUFFICIENT_EVIDENCE`, and `REQUIRES_REVIEW` are separate states. Policy decides whether a result blocks the pipeline.

Evidence records devctl version, devctl commit, policy version, check version, timestamps, findings, and raw output where available.

## CI is a clean-environment confirmation

Local devctl verification and GitHub Actions use the same engine. CI proves that committed source can reproduce verification without ignored or untracked local files.

## Private cross-repository checkout

HearthLink uses `DEVCTL_REPO_TOKEN` to read private `_devctl`. The token should be fine-grained, limited to `_devctl`, Contents read-only, and given an expiry.

## Generated evidence is not project source

Project-controlled `.devctl` configuration may remain visible. Generated evidence is ignored specifically through `.devctl/evidence/`.
