# Stage 7E-B — Progressive Failure and Evidence Retrieval

This is the implementation and acceptance guide for Stage 7E-B. Every task is
numbered and marked `DONE` or `TO BE DONE`. A task becomes `DONE` only when its
implementation and named evidence exist.

## Objective

Let an agent retrieve only the failed checks, one normalized failure, or one
bounded selected raw-evidence fragment from an existing `_devctl` run. Retrieval
must not execute project commands, change verification truth, or expose the
rest of the run's raw output.

## Frozen command contract

```text
devctl failures [--json] [--project <path>] [--offset <index>] <run-id>
devctl failure [--json] [--project <path>] [--offset <index>] <run-id> <check-id>
devctl evidence [--json] [--project <path>] [--offset <raw-byte-offset>] <run-id> <failure-id>
```

`failure-id` equals the failed check ID in Stage 7E-B. It is stable within the
named run. Existing `devctl evidence rebuild|latest [--json] <project>` behavior
must remain compatible.

Flags may appear before or after positional arguments. The default project is
the current directory.

## Fixed boundaries

- Retrieval reads an existing exact run ID under the selected project's
  `.devctl/evidence` directory.
- Retrieval never invokes the scheduler, a check, a project command, repair,
  Git mutation, or verification.
- Level 2 returns failed or otherwise unresolved checks only.
- Level 3 returns one normalized failure and its bounded findings, without raw
  output.
- Level 4 reads only the selected run's generated `raw/<check>.log`; it never
  follows an evidence path stored inside a report.
- JSON responses contain exactly one object, include a terminating newline,
  and are capped at `16 KiB`.
- Selected raw evidence uses a raw-byte offset. Responses report raw bytes
  read, total raw bytes, and the next raw offset independently of text
  normalization and redaction.
- Retrieval success exits `0` even when the historical verification result was
  `FAIL`. Arguments, containment failures, missing runs/failures, unavailable
  raw evidence, or encoding failures exit `2`.
- Agent-facing report fields and evidence fragments are untrusted data. They
  are normalized, redacted, and bounded before serialization.

## Numbered task list

1. **DONE — Confirm the Stage 7E-A prerequisite.**
   - `verify --agent` is implemented and locally accepted.
   - Clean committed hosted CI remains separate acceptance work.

2. **DONE — Freeze the Stage 7E-B public contract.**
   - Fix the commands, identifiers, output bound, paging semantics and exit
     behavior above.
   - Preserve the existing evidence-index commands.

3. **DONE — Add failing public-boundary tests.**
   - Cover Levels 2, 3 and 4 through command functions.
   - Capture stdout and stderr separately.
   - Prove flags work before or after positional arguments.

4. **DONE — Add contained exact-run loading.**
   - Validate run IDs before path construction.
   - Bound report reads and require the report's run ID to match.
   - Reject missing, malformed, oversized or escaping evidence.

5. **DONE — Implement Level 2 failure listing.**
   - Preserve report order, status, blocking state, check version and
     verification provenance.
   - Preserve total and returned counts under truncation.

6. **DONE — Implement Level 3 failure detail.**
   - Resolve one exact failed check.
   - Return bounded normalized findings and whether generated raw evidence is
     available.
   - Never include `CheckResult.RawOutput`.

7. **DONE — Implement Level 4 selected evidence.**
   - Read a small bounded chunk from the generated raw check log only.
   - Strip terminal controls and redact secret-like values.
   - Return truthful byte-oriented continuation metadata.

8. **DONE — Add a shared hard response encoder.**
   - Include exact serialized response bytes.
   - Truncate collections or selected content deterministically.
   - Return a bounded structured error if safe representation is impossible.

9. **DONE — Add structured JSON errors.**
   - Keep JSON-mode stderr empty.
   - Cover malformed arguments, invalid run IDs, missing runs, missing
     failures and unavailable raw evidence.

10. **DONE — Preserve ordinary CLI compatibility.**
    - Preserve `verify`, `verify --agent`, `worker verify`, `handoff`, and
      `evidence rebuild|latest` behavior.

11. **DONE — Add negative containment and hostile-data tests.**
    - Cover traversal identifiers, mismatched reports, link escapes, terminal
      controls, secret-like data and oversized collections.

12. **DONE — Add controlled PASS and FAIL run acceptance.**
    - A PASS run returns an empty Level 2 list.
    - A FAIL run supports Levels 2–4 without exposing unrelated successful
      output.

13. **DONE — Add clean CI coverage for Stage 7E-A and 7E-B.**
    - Build a commit-pinned executable.
    - Run the agent verification boundary from a clean checkout.
    - Validate its one-object, empty-stderr, size, provenance and check-vector
      contract.
    - Run progressive contract tests in the configured CI environment.

14. **DONE — Run focused and broad verification.**
    - Run retrieval/evidence/CLI tests, full tests, race tests, vet, build and
      `_devctl` self-verification.

15. **DONE — Perform an independent final audit.**
    - Check specification and engineering quality separately.
    - Confirm no policy weakening, generated source, unrelated HearthLink
      edits, command execution, or arbitrary-path evidence reads.

16. **DONE — Record final acceptance and limitations.**
    - Record exact commands, results, response sizes and evidence paths.
    - Keep hosted CI `NOT TESTED` until a real remote run passes.

## Current truthful state

```text
Stage 7E-A local acceptance: PASS
Stage 7E-A clean isolated-checkout acceptance: PASS
Stage 7E-A hosted GitHub Actions acceptance: NOT TESTED
Stage 7E-B contract: FROZEN
Stage 7E-B implementation: IMPLEMENTED
Stage 7E-B focused acceptance: PASS
Stage 7E-B broad acceptance: PASS
Stage 7E-B clean isolated-checkout acceptance: PASS
Stage 7E-B hosted GitHub Actions acceptance: NOT TESTED
```

## Focused evidence

The initial public-boundary test failed to compile because `failuresCommand`
and `failureCommand` did not exist. After the vertical slice was implemented,
the focused progressive, evidence, handoff, worker and CLI packages passed.

Compiled CLI checks against preserved real `_devctl` evidence returned:

```text
FAIL run 20260819T035506.706526800Z
Level 2: 1/1 failures, 817-byte response
Level 3: secret-scan detail with 1/1 finding, 1223-byte response

WARN run 20260819T035859.010689900Z
Level 2: 0/0 failures, 619-byte response
```

The clean CI workflow now runs `verify --agent` from the commit-pinned binary,
writes stdout and stderr outside the repository, and validates the exact
commit/revision, clean provenance, complete seven-check PASS vector, response
bound, information-flow counts and empty stderr. A hosted run is still needed
before hosted clean CI can be marked `PASS`.

## Acceptance fixes retained as reusable knowledge

The first clean isolated run, `20260819T101425.876991100Z`, returned `ERROR`
because `go-test` and `go-test-race` competed for the same Go compilation cache
and the race check reached its inactivity timeout. The fix gives `go-test`,
`go-test-race`, and `go-build` one shared `go-toolchain` scheduler resource.
The checks now run serially without changing either timeout or any policy rule.
`LESSON-0015` records this reusable rule.

The final source audit also found two pagination edge cases before release:

- a private-key marker or short secret assignment split at the 2 KiB boundary
  could avoid a chunk-local pattern;
- last-mile response truncation could remove an evidence reference without
  updating `evidence_paths_returned`.

Regression tests failed against the previous implementation and pass after the
fixes. Redaction now carries line and private-key state across raw-byte pages,
and every hard-encoder removal updates its returned count. `LESSON-0016` and
`LESSON-0017` retain both rules in `knowledge/lessons.yaml`.

## Broad and clean acceptance evidence

Focused packages:

```text
go test ./internal/progressive ./internal/adapters/golang ./internal/evidence \
  ./internal/handoff ./internal/worker ./cmd/devctl -count=1
```

Result: `PASS` for all six packages.

Broad repository commands:

```text
go test ./... -count=1
go vet ./...
git diff --check
```

Result: `PASS`. The broad package run completed in `106.2` seconds. The later
clean acceptance reran the final source through both ordinary and race-enabled
full package tests.

Final clean acceptance snapshot:

```text
temporary acceptance commit: c0e963e8ec1bc824f56bea28353c1097ba3db31a
run: 20260819T103717.918216100Z
command: devctl.exe verify --agent <clean-isolated-checkout>
exit: 0
overall: PASS
checks: 7/7 PASS
go-test: PASS, 100410 ms
go-test-race: PASS, 180990 ms
secret-scan: PASS
stderr: 0 bytes
raw subprocess: 2547 bytes
retained subprocess: 2547 bytes
local evidence: 28346 bytes
agent response: 1903 bytes
evidence: .devctl/evidence/20260819T103717.918216100Z
```

The temporary commit exists only in the independent acceptance clone. It does
not commit or publish the working repository. The validator confirmed the
exact commit in both devctl and repository provenance, clean Git state, the
complete expected check vector, one newline-terminated JSON object under
16 KiB, consistent byte accounting, no failure packet and empty stderr.

The same clean binary then produced these progressive responses:

```text
clean PASS Level 2: 0 failures, 575 bytes
historical FAIL Level 2: 1 failure, 817 bytes
historical FAIL Level 3: secret-scan detail, 1223 bytes
missing generated Level 4 raw evidence: structured evidence_unavailable, 244 bytes
```

Positive Level 4 paging, raw-byte continuation, containment, control removal,
secret redaction and split-boundary cases pass in the controlled package and
public-command fixtures. The historical `secret-scan` failure correctly
returned `evidence_unavailable` because that run did not generate a raw log;
retrieval did not invent one or follow another path.

## Final audit

- Levels 2-4 read one existing exact run and never invoke verification,
  project commands, repair, Git mutation or policy evaluation.
- Level 4 derives `raw/<safe-check-id>.log` inside the canonical run. Report
  evidence paths are descriptive only and are never opened.
- IDs, reports, files, directories, sizes, offsets and canonical containment
  fail closed. Link escapes and duplicate or mismatched report identities are
  rejected.
- Level 2 and Level 3 never serialize `RawOutput`. Level 4 reads at most 2 KiB
  per request from a raw log capped at 2 MiB and reports original byte offsets
  independently from normalized content.
- The `16 KiB` encoder preserves totals, returned counts, truncation and next
  metadata without mutating caller-owned values.
- Existing verification, worker and legacy `evidence rebuild|latest` behavior
  remains covered. No threshold, timeout, status, secret rule or test was
  weakened.
- No HearthLink file was changed. Generated `.devctl` evidence and the
  unrelated untracked `.vscode/mcp.json` were excluded from acceptance source.

## Remaining limitation

The GitHub Actions workflow is implemented but has not run with these changes
on GitHub. Hosted clean CI therefore remains `NOT TESTED` until the owner
explicitly authorizes repository publication and a real remote run passes.
