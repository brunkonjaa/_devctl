# Stage 7F-A — Structured Fix Records

This is the implementation and acceptance guide for Stage 7F-A. Every task is
numbered and marked `DONE` or `TO BE DONE`. A task becomes `DONE` only when its
implementation and named evidence exist.

## Objective

Close a material diagnosed fix only after `_devctl` validates the exact
pre-fix failure, the exact post-fix verification, the project identity and the
current repository fingerprint. Preserve the result as an immutable
project-local Fix Record so later work can understand what happened without
turning an AI explanation into trusted engineering truth.

## Frozen command contract

```text
devctl fixes record [--json] --input <candidate.json> <project>
devctl fixes list [--json] [--limit <count>] <project>
devctl fixes show [--json] <project> <fix-id>
```

`record` is the only mutating operation. It reads one bounded candidate,
validates objective evidence, derives all verification/provenance fields, and
creates one new immutable record. `list` and `show` are read-only and never run
verification or project commands.

## Candidate input

The candidate is descriptive input, not a trusted result. It cannot contain a
status, record timestamp, repository fingerprint, check transition, report
hash or record hash.

```json
{
  "schema_version": "1",
  "id": "FIX-PROJECT-0001",
  "title": "Go tests contended for one build cache",
  "project_id": "project-example",
  "problem": "The race check reached its inactivity limit during another Go check.",
  "symptoms": ["go-test-race ended with inactivity_timeout"],
  "root_cause": "Independent checks used the same Go build cache concurrently.",
  "affected_components": ["scheduler", "Go adapter"],
  "affected_files": ["internal/adapters/golang/golang.go"],
  "attempts": [
    {
      "outcome": "FAILED",
      "description": "Increase the timeout.",
      "reason": "The cache contention remained."
    }
  ],
  "final_fix": "Serialize Go toolchain checks through one scheduler resource.",
  "pre_run_id": "20260819T080000.000000000Z",
  "post_run_id": "20260819T081000.000000000Z",
  "check_ids": ["go-test-race"],
  "known_limitations": ["This record proves only the named project and evidence pair."],
  "applicability": "Projects whose Go checks share one build cache.",
  "relevant_versions": {"go": "1.25"},
  "related_fix_ids": [],
  "tags": ["go", "scheduler"]
}
```

The input must be one normal file no larger than `64 KiB`. Decoding rejects
unknown fields, duplicate object keys and extra JSON values. Text and arrays
are bounded; terminal controls, private-key markers, AWS access-key patterns
and long secret assignments are rejected. Attempts may be only `FAILED` or
`INCONCLUSIVE`. Optional patch evidence requires both a canonical relative
path under `.devctl/evidence` and its lowercase SHA-256.

## Storage boundary

```text
<project>/.devctl/knowledge/fix-records/<fix-id>.json
```

- Stage 7F-A records are project-local.
- Every record is a separate normal file created with create-only semantics.
- An existing record is never rewritten, deleted, or silently repaired.
- A correction or replacement is a new record whose `supersedes` field names
  the older record. The older bytes remain unchanged.
- The record contains its own canonical content hash. Reads fail closed if the
  stored bytes no longer match that hash.
- Each run reference includes the SHA-256 of the exact stored `report.json`
  bytes used at closure time.
- Global identifiers, promotion, generated indexes and cross-project search
  remain Stage 7F-B/7F-C work.

## Fix closure rule v1

A record may receive `VERIFIED` only when all of these are true:

1. The candidate schema, identifier and bounded descriptive fields are valid.
2. The selected project and Fix Record directories are canonically contained
   normal directories.
3. The named pre-fix and post-fix runs are different exact generated evidence
   runs under the selected project.
4. Both reports have valid schema/run/evidence identity, unique valid check
   IDs, the same non-empty project identity and complete provenance.
5. The candidate project identity matches both reports.
6. Every target check exists in both reports.
7. Every target check is unresolved before the fix and exactly `PASS` after
   the fix. `PASS`, `SKIP` and `NOT_APPLICABLE` are not pre-fix failures.
8. The complete post-fix report has deterministic exit code `0`.
9. The post-fix run finished after the pre-fix run.
10. The current repository fingerprint still equals the post-fix report
    fingerprint. Stale evidence cannot close current work.
11. Any optional patch artifact is a bounded normal file canonically contained
    under `.devctl/evidence`, and its SHA-256 matches the candidate.
12. Every named related or superseded Fix Record already exists and is valid.
13. The candidate contains no terminal controls, secret-like values, private
    key material or unrestricted raw logs.
14. The destination Fix Record ID does not already exist.

All objective fields are derived from evidence by `_devctl`; candidate text
cannot choose statuses, repository provenance, check transitions, record hash,
record time or `VERIFIED` state.

## Trust boundary

The content hash is an integrity checksum, not a remote signature. Stage 7F-A
trusts the selected project's local generated evidence at closure time, binds
the exact report bytes into the record, and proves that the current project
fingerprint still matches the post-fix run. It does not claim protection from
an attacker who can replace evidence and recompute every local hash.

`show` and `list` validate the immutable record itself but deliberately do not
rerun checks or require the project to remain at the recorded fingerprint. A
later project change therefore makes the historical record stale for current
work without erasing what the record proved when it was created. Compatibility
and staleness-aware retrieval remain Stage 7F-B/7F-C work.

## Fix Record and Lesson distinction

```text
Fix Record
  one project
  one diagnosed problem
  exact pre/post runs
  exact check transitions and repository state
  failed attempts and final repair

Reusable Lesson
  generalized engineering rule
  separately reviewed and promoted
  may reference one or more verified Fix Records
```

Stage 7F-A never writes `.devctl/knowledge/lessons.json` and never promotes a
Fix Record into `knowledge/lessons.yaml`. A verified fix is evidence for a
future lesson; it is not automatically a reusable lesson.

## Acceptance matrix

| Case | Required result |
| --- | --- |
| Exact pre-fix failure and current post-fix PASS | Create one `VERIFIED` record |
| Failed attempts supplied | Preserve them without treating them as successful |
| Duplicate Fix Record ID | Reject and preserve original bytes |
| New record supersedes an existing valid record | Append new file; preserve old bytes |
| Missing or malformed evidence run | Reject without writing |
| Mismatched project identity | Reject without writing |
| Pre-fix target already `PASS`, `SKIP` or `NOT_APPLICABLE` | Reject without writing |
| Post-fix target is not `PASS` | Reject without writing |
| Another post-fix check still blocks | Reject without writing |
| Post-fix fingerprint is stale | Reject without writing |
| Reversed/equal run order | Reject without writing |
| Traversal ID, link escape or non-normal evidence/record file | Reject without writing |
| Patch path/hash mismatch | Reject without writing |
| Secret-like or control-bearing candidate text | Reject without writing |
| Stored record is later edited | `show` and `list` fail integrity validation |
| Concurrent writes use the same ID | Exactly one succeeds; no overwrite |
| `list` or `show` | Read existing records only; execute no verification |
| Fix Record creation | Does not create or modify a Lesson |

## Numbered task list

1. **DONE — Freeze the Stage 7F-A boundary.**
   - Fix the command, closure, storage, append-only and trust rules above.

2. **DONE — Extract contained exact-run report loading.**
   - Reuse one evidence reader for progressive disclosure and Fix Records.
   - Preserve existing Stage 7E-B error behavior.

3. **DONE — Add failing Fix Record contract tests.**
   - Cover the positive closure path and negative acceptance matrix before the
     implementation exists.

4. **DONE — Implement bounded candidate decoding.**
   - Accept exactly one schema-versioned object with no unknown fields.
   - Reject unsafe or unbounded descriptive content.

5. **DONE — Implement deterministic closure validation.**
   - Derive project, run, repository, check and verification fields from exact
     evidence and the current fingerprint.

6. **DONE — Implement immutable record persistence.**
   - Use one create-only record file and canonical integrity hash.
   - Fail closed on duplicate IDs, malformed existing records or tampering.

7. **DONE — Implement explicit supersession.**
   - Require the older record to exist and remain byte-for-byte unchanged.

8. **DONE — Add `fixes record|list|show`.**
   - Keep JSON/text output deterministic and bounded.
   - Do not add Git, repair, verification, lesson or promotion actions.

9. **DONE — Prove Fix Record/Lesson separation.**
   - Assert that successful record creation does not write a lesson store.

10. **DONE — Run focused and broad verification.**
    - Run package/CLI tests, full tests, race tests, vet, diff checks and a
      clean isolated `_devctl` acceptance run.

11. **DONE — Perform the independent final audit.**
    - Review specification and engineering quality separately.
    - Confirm no policy, threshold, timeout, test or Git boundary was weakened.

12. **DONE — Record acceptance and remaining Stage 7F-B work.**
    - Keep global IDs, authoritative-vs-index rules, explicit promotion and
      cross-project compatibility outside Stage 7F-A.

## Remaining numbered knowledge-vault work

13. **TO BE DONE — Freeze Stage 7F-B authoritative storage.**
    - Separate human-reviewed authoritative knowledge files from disposable,
      rebuildable indexes and project-local generated records.

14. **TO BE DONE — Introduce globally unique knowledge IDs.**
    - Add global identity without rewriting or pretending Stage 7F-A local Fix
      Record IDs were already globally unique.

15. **TO BE DONE — Implement explicit Fix Record promotion.**
    - Require a reviewed promotion record that names one or more valid Fix
      Records and produces a separate reusable Lesson.

16. **TO BE DONE — Freeze and test the knowledge acceptance matrix.**
    - Cover accepted, rejected, superseded, malformed, stale, incompatible and
      partially supported knowledge without allowing AI to choose trust state.

17. **TO BE DONE — Add compatibility and staleness rules.**
    - Compare project type, affected components, relevant tool versions and
      current fingerprints before presenting historical knowledge as current.

18. **TO BE DONE — Add generated cross-project retrieval indexes.**
    - Rebuild indexes from authoritative files and verified local records;
      never make an index the only copy or source of truth.

## Current truthful state

```text
Stage 7F-A contract: FROZEN
Stage 7F-A implementation: PASS
Stage 7F-A focused acceptance: PASS
Stage 7F-A broad/clean acceptance: PASS
Hosted GitHub Actions: NOT TESTED
```

Local acceptance covers strict JSON, exact evidence loading, closure status,
project and provenance mismatches, stale fingerprints, blocking post-fix
checks, optional patch hashes, append-only duplicates, explicit supersession,
tampered stores, concurrent same-ID writes, read-only retrieval and Fix
Record/Lesson separation. The full non-race and race suites pass. The
`go vet ./...`, `go build ./cmd/devctl`, `git diff --check` and independent
clean snapshot verification gates also pass. Hosted GitHub Actions remains
`NOT TESTED` until publication and a remote run are explicitly authorized.

The first clean snapshot attempt was intentionally left as `FAIL`: the
repository secret scan found a complete token-shaped literal inside the new
negative test, and PowerShell pipeline redirection transcoded agent JSON into
UTF-16 before the byte-level validator read it. The test now constructs the
value from harmless runtime fragments, and clean acceptance captures stdout and
stderr through process-level file redirection. `LESSON-0022` and
`LESSON-0023` retain both rules for future work.
