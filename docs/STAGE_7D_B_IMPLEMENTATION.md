# Stage 7D-B minimal repair implementation

Status: COMPLETE / ACCEPTED

Baseline contract: [STAGE_7D_A_CONTROL.md](STAGE_7D_A_CONTROL.md)

Stage 7D-B implements the smallest controlled repair lifecycle in
`internal/repair`. It is exercised against a synthetic Go repository. The
package is a deterministic orchestration seam; a general Codex adapter and a
terminal command are intentionally not part of this slice yet.

## Implemented boundary

`repair.Run` owns the control flow:

```text
baseline Verify callback
        |
        +-- non-FAIL -> record and stop
        |
        +-- FAIL
              |
              v
        clean Git/filesystem snapshot
              |
              v
        bounded Propose callback
              |
              v
        trusted-path validation
              |
              v
        canonical immutable patch bytes + SHA-256 diff_hash
              |
        persisted patch artifact reloaded before apply
              |
              v
        complete old -> new diff + approval envelope
              |
              v
        pre-apply snapshot revalidation
              |
              v
        complete patch preflight
              |
              v
        transactional apply with rollback on write failure
              |
              v
        actual post-state and content-hash validation
              |
              v
        one deterministic Verify callback
              |
              v
        stop
```

The callbacks are dependency seams for the synthetic fixture. They do not
give the worker execution authority. The package never accepts a worker shell
command, executable, arguments, environment, policy change, threshold, or
retry instruction.

The worker identity is selected by `Options.Worker` at the orchestration
boundary. A proposal is rejected if its self-reported worker identity does
not exactly match that trusted value. The approval request also carries the
trusted worker ID, project ID, baseline run ID, protocol version, bounded
failure packet, immutable canonical patch bytes, patch hash, and a complete
deterministic old-to-new display diff. The displayed diff includes the
removed baseline lines as well as the proposed lines and is derived from the
same reloaded patch artifact used for application.
The canonical artifact records the exact preimage and postimage for each
changed file. Its byte hash is the approval hash, and the post-apply artifact
is reconstructed from the actual files before re-verification is allowed.

## Current synthetic fixture

The test creates a clean Git repository containing `go.mod`,
`calculator.go`, `calculator_test.go`, `devctl.json`, and a `.gitignore` for
`.devctl/`. The production defect makes `Add` subtract instead of add. The
proposal changes only `calculator.go`; the test file is outside the allowlist
and remains unchanged.

The real deterministic Go verification path is used for the positive lifecycle.
The fixture disables only the unavailable race check so the result is not
misclassified as `NOT_TESTED` on a host without a race toolchain.

Repair evidence is written to:

```text
.devctl/evidence/repair/<task_id>.json
```

The evidence contains the bounded task result, event sequence, exact diff
hash, persisted patch-artifact path, complete task/proposal/approval
envelopes, protected baseline snapshot, per-file hashes, and final
verification status. The durable baseline records `HEAD`, branch, index hash,
worktree status, project identity, verification run ID, policy/verification
provenance hash, file hashes, byte counts, and modes. The task also carries
the canonical project metadata, forbidden-path policy, `_devctl` provenance,
and policy provenance hash from this baseline. This is the same snapshot
representation used by pre-apply comparison and rollback proof.
The implementation rechecks `HEAD`, branch, index hash, project identity,
policy provenance, file content, and file mode after application. Rollback is
accepted only after a fresh snapshot proves the exact pre-apply state was
restored. Proposal content must be valid UTF-8 text with LF line endings and
no NUL bytes. Evidence does not contain raw process output.

Task IDs are strict bounded identifiers. Separators, `..`, volume syntax,
NUL, absolute paths, and IDs longer than 64 characters are rejected. The
resolved evidence directory and target path are checked for filesystem
containment before either the patch artifact or evidence JSON is written.

Cancellation is an orchestration decision and is checked before approval,
pre-apply validation, preflight, application, and re-verification. If it
arrives during a write transaction, the attempted changes are rolled back and
the baseline snapshot must be proved exact before cancellation is returned.
Human rejection and human cancellation are separate approval outcomes, and
post-apply validation failure is persisted with final status `ERROR`.

## Automated negative coverage

The package tests cover:

- dirty index or worktree;
- wrong approval hash;
- changed production file after approval;
- forbidden test path;
- untrusted source path;
- policy/configuration change before apply;
- target changed before patch preflight;
- mutation of the approval callback's patch copy;
- partial write with rollback;
- actual post-apply bytes differing from approved content;
- actual post-apply diff hash mismatch with rollback;
- terminal-newline state in the displayed approval diff;
- forbidden `config/defaults.json` even when it is in `AllowedPaths`;
- forbidden path entries rejected from `AllowedPaths` before worker invocation;
- worker mutation of task path-policy and provenance metadata;
- unexpected extra post-state file;
- approval cancellation;
- worker proposal error;
- second repair attempt;
- deterministic `FAIL` after repair;
- deterministic `ERROR` after repair.
- invalid and path-traversing task IDs;
- complete durable baseline and approval evidence;
- malformed, oversized, and deletion proposals;
- trusted worker identity mismatch;
- actual old-to-new approval diff;
- HEAD mutation before apply;
- worker timeout;
- distinct approval rejection and cancellation;
- cancellation before approval, after approval, and during application.

## Deliberate limits

This slice does not yet provide:

- a Codex or external-worker transport;
- a visible interactive approval terminal;
- a `devctl repair` command;
- support for additions, deletions, renames, binary patches, or mode changes;
- preservation of a dirty starting repository;
- automatic commits, pushes, merges, or another repair attempt.

Those limits keep the first implementation aligned with the Stage 7D-A
contract. They must not be relaxed as a shortcut to make a later integration
green.
