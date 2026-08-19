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

## Progressive disclosure reads exact generated evidence

Agent retrieval names an exact run ID and reads only under that selected
project's generated `.devctl/evidence/<run-id>` directory. Failure IDs are the
run-scoped check IDs in Stage 7E-B. Report-supplied paths are presentation data;
they are never followed to retrieve raw content.

Level 2 lists unresolved checks, Level 3 returns one normalized failure, and
Level 4 returns one small redacted fragment from the generated raw check log.
All JSON responses are versioned and capped at 16 KiB. Paging cursors describe
failure/finding indexes or raw byte offsets; retrieval never changes the stored
verification result and exits successfully when it retrieves a historical
`FAIL`.

## Shared toolchain state is a scheduler resource

`go-test`, `go-test-race`, and `go-build` all declare the same
`go-toolchain` resource. They may be logically independent, but concurrent
cold-cache execution can contend long enough to trigger a healthy check's
inactivity limit. Serialization fixes that resource conflict without extending
timeouts, changing policy, or hiding an unavailable race toolchain.

## Verified fixes and reusable lessons are different records

A Stage 7F-A Fix Record proves one project-local closure against exact pre-fix
and post-fix evidence. `_devctl`, not candidate prose, derives `VERIFIED`, check
transitions, provenance, report hashes, record time and the current-state
fingerprint match.

A reusable Lesson is a separately reviewed generalization. Recording one fix
does not automatically claim that its solution applies to another project,
tool version or repository state. Stage 7F-B will define explicit promotion and
global identity; Stage 7F-A does not silently write either lesson store.

Fix Records are append-only individual files. An existing ID is never updated.
A correction creates a new record naming the old ID in `supersedes`, and reads
or later appends fail closed if existing record integrity is broken. The local
content hash detects accidental or unapproved edits but is not represented as
a remote signature against an attacker who controls the project filesystem.
