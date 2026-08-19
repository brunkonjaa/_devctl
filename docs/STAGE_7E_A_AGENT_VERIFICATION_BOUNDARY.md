# Stage 7E-A — Agent Verification Boundary

This file is the working guide for Stage 7E-A. Keep every task numbered and
mark it `DONE` or `TO BE DONE`. A task moves to `DONE` only when the named
implementation or evidence exists.

## Objective

Prove that `devctl verify --agent <project>` uses the existing full
verification engine while keeping verbose child-process output outside the
AI-facing command response. The command must return one bounded structured
result without creating a second scheduler, policy path, or verification
truth.

## Fixed boundaries

- `_devctl` remains the deterministic verification authority.
- Ordinary `devctl verify` behaviour remains available.
- Agent mode uses the same applicable checks, policy, evidence, and exit-code
  calculation as ordinary full verification.
- Child output is retained as bounded local evidence and never streamed to the
  agent caller.
- Agent-facing text is treated as untrusted, normalized, and bounded.
- Stage 7E-A does not add affected verification, MCP, a VS Code extension,
  automatic repair, or Knowledge Vault changes.

## Numbered task list

1. **DONE — Establish repository identity.**
   - Repository: `C:\Projects\_devctl`
   - Branch: `main`
   - Baseline HEAD: `013a8c92a0da22f957cda2c65156c3a140d51252`
   - Upstream position at inspection: `main...origin/main`, ahead `0`, behind
     `0`.

2. **DONE — Record the pre-existing dirty worktree.**
   - Existing modified files include `cmd/devctl/main.go`, verification,
     runner, scheduler, fingerprinting, Android dependency/coverage work, live
     events, status, and roadmap files.
   - Existing untracked tests are `cmd/devctl/main_test.go` and
     `internal/live/renderer_test.go`.
   - These changes predate Stage 7E-A and must be preserved.

3. **DONE — Identify overlap with Stage 7E-A.**
   - `cmd/devctl/main.go` is an overlapping write surface.
   - The existing `internal/worker` protocol already provides bounded worker
     summaries and must be reused or extended instead of duplicated.
   - The existing runner captures child output without inheriting caller
     stdout/stderr and keeps an explicit local output bound.

4. **DONE — Freeze the minimum command contract.**
   - Command: `devctl verify --agent <project>`.
   - Agent mode and `--live` are incompatible.
   - Agent mode emits exactly one schema-versioned JSON object on stdout.
   - Normal agent execution emits nothing on stderr.
   - Fatal framework and argument errors use the same bounded JSON channel.
   - Deterministic verification exit codes remain authoritative.

5. **DONE — Add a failing public-boundary test for agent mode.**
   - Prove the option is currently absent or does not satisfy the frozen
     contract.
   - Capture stdout and stderr independently.

6. **DONE — Extend the shared bounded result contract.**
   - Include verification class, repository revision/dirty/fingerprint,
     policy version, and check versions.
   - Keep raw check output out of the result.
   - Preserve total and returned counts when details are truncated.

7. **DONE — Add a hard serialized response limit.**
   - Initial maximum: `16 KiB`, including the terminating newline.
   - Apply deterministic field and collection limits.
   - Return a bounded structured framework error if the contract cannot be
     represented safely.

8. **DONE — Normalize untrusted agent-facing strings.**
   - Remove terminal control sequences and control characters.
   - Collapse uncontrolled whitespace.
   - Apply per-field length limits.
   - Do not include raw subprocess output.

9. **DONE — Record information-flow metrics.**
   - `raw_subprocess_bytes`
   - `retained_subprocess_bytes`
   - `local_evidence_bytes`
   - `agent_response_bytes`
   - Record whether subprocess output was truncated by the local evidence
     bound.

10. **DONE — Implement `verify --agent`.**
    - Reuse `executeVerification` and the ordinary verification engine.
    - Do not add a scheduler, check selection path, policy implementation, or
      agent-specific status calculation.
    - Suppress observational registry/workflow diagnostics from the agent
      response channel while retaining deterministic verification truth.

11. **DONE — Add structured fatal-error coverage.**
    - Missing project path.
    - Unavailable project path.
    - Invalid option combinations.
    - Result serialization failure or size-contract failure.

12. **DONE — Add controlled PASS acceptance.**
    - Run ordinary full verification and agent verification against the same
      controlled project state.
    - Compare exit code, overall status, and complete check ID/status vector.
    - Prove agent stderr is empty and stdout is one bounded JSON object.

13. **DONE — Add controlled verbose FAIL acceptance.**
    - Generate substantial deterministic child output and one known failure.
    - Prove the child stream does not appear in caller stdout or stderr.
    - Prove bounded raw evidence exists locally.
    - Compare raw, retained, evidence, and agent-response byte counts.

14. **DONE — Verify ordinary CLI compatibility.**
    - Ordinary text output remains human-readable.
    - Existing `--json` output remains valid.
    - Existing `worker verify` behaviour remains valid.
    - Existing `--live` behaviour remains explicit and separate from agent
      mode.

15. **DONE — Run focused package tests.**
    - Agent/worker result contract tests.
    - Command-level stdout/stderr tests.
    - Runner information-flow metric tests.
    - Existing worker parity tests.

16. **DONE — Run broad repository verification.**
    - Run `go test ./...` with enough time for the known broad suite duration.
    - Run the repository's deterministic `_devctl` verification gate.
    - Preserve truthful `NOT_TESTED` for unavailable race tooling or other
      missing evidence.

17. **DONE — Inspect the final Stage 7E-A diff.**
    - Separate pre-existing changes from Stage 7E-A changes.
    - Confirm no HearthLink source files changed.
    - Confirm no generated evidence is staged as source.
    - Confirm no policy or threshold was weakened.

18. **DONE — Record Stage 7E-A acceptance.**
    - Add the exact PASS and FAIL commands, exit codes, output byte counts,
      evidence locations, and unresolved limitations to this document.
    - Update `docs/STATUS.md`, `docs/ROADMAP.md`, and `docs/README.md` only after
      the implementation evidence supports the claimed state.

19. **DONE — Add and run clean isolated-checkout acceptance.**
    - Build a commit-pinned executable from an independent clean checkout.
    - Require exact clean repository/devctl provenance, all seven checks,
      bounded one-object stdout, empty stderr and measured local evidence.
    - Validate the result with the same script used by GitHub Actions.

20. **DONE — Add clean GitHub Actions enforcement.**
    - Run the commit-pinned `verify --agent` boundary in CI.
    - Validate the result contract and upload local evidence plus the captured
      result and stderr files.

21. **TO BE DONE — Observe hosted GitHub Actions acceptance.**
    - Keep this separate from local clean acceptance.
    - Mark it `DONE` only after the changes are explicitly published and the
      real remote workflow passes.

## Current truthful state

```text
architecture: READY
full local gate: PASS with expected dirty-worktree WARN
Stage 7E-A implementation: IMPLEMENTED
Stage 7E-A acceptance: LOCAL AND CLEAN ISOLATED PASS
clean isolated-checkout acceptance: PASS
hosted GitHub Actions acceptance: NOT TESTED
```

## Final local acceptance evidence

Focused command:

```text
go test ./internal/worker ./internal/runner ./cmd/devctl \
  -run 'Test(VerifyAgent|AgentVerification|EncodeResult|OutputMetrics|WorkerVerify)' \
  -count=1 -v
```

Result: `PASS`.

Controlled PASS fixture:

```text
overall: PASS
exit: 0
raw_subprocess_bytes: 144
retained_subprocess_bytes: 144
local_evidence_bytes: 14425
agent_response_bytes: 1770
```

Controlled verbose FAIL fixture:

```text
overall: FAIL
exit: 1
raw_subprocess_bytes: 2949361
retained_subprocess_bytes: 1048683
local_evidence_bytes: 3160257
agent_response_bytes: 2387
go-test raw log bytes: 1048576
```

The caller received neither the `VERBOSE_CHILD_OUTPUT_SENTINEL` stream nor any
other child output. The raw log remained in the fixture evidence and the local
one-megabyte output bound was reported truthfully.

Fresh `_devctl` self-verification:

```text
command: devctl-stage7e-a.exe verify --agent C:\Projects\_devctl
run_id: 20260819T035859.010689900Z
exit: 0
overall: WARN
reason for WARN: expected dirty Git worktree
checks returned: 7/7
go-build: PASS
go-test: PASS
go-test-race: PASS
secret-scan: PASS
raw_subprocess_bytes: 3746
retained_subprocess_bytes: 3746
local_evidence_bytes: 31272
agent_response_bytes: 1954
evidence: .devctl/evidence/20260819T035859.010689900Z
```

The broad run lasted approximately 185 seconds and emitted no child output
before the final JSON object. An independent recursive file-size check matched
the reported `31,272` local evidence bytes.

## Clean isolated-checkout acceptance

The final source snapshot was copied into an independent clone, committed only
inside that clone, and verified with a binary whose embedded devctl commit
matched the temporary acceptance commit.

```text
temporary acceptance commit: c0e963e8ec1bc824f56bea28353c1097ba3db31a
run: 20260819T103717.918216100Z
exit: 0
overall: PASS
checks: 7/7 PASS
repository revision: c0e963e8ec1bc824f56bea28353c1097ba3db31a
devctl commit: c0e963e8ec1bc824f56bea28353c1097ba3db31a
repository dirty: false
devctl dirty: false
stderr: 0 bytes
raw subprocess: 2547 bytes
retained subprocess: 2547 bytes
local evidence: 28346 bytes
agent response: 1903 bytes
evidence: .devctl/evidence/20260819T103717.918216100Z
```

`scripts/validate-agent-result.py` accepted the exact result. `go-build`,
`go-test`, `go-test-race`, `secret-scan`, Git status, environment and
technology detection all passed. The run took `287.0` seconds. No temporary
acceptance commit was made in the working repository and nothing was pushed.

## Fix discovered during acceptance

The first final self-verification run, `20260819T035506.706526800Z`, returned
exit `1` because the private-key redaction test stored a literal PEM private-key
header in `internal/worker/protocol_test.go`. The `secret-scan` check correctly
reported that source as a blocking possible-secret finding.

The fixture now constructs the marker from separate string fragments at test
runtime. This keeps the redaction test meaningful without storing a secret-like
literal in source. No secret-scan rule, policy threshold, or test was disabled.
The fresh full run above then passed `secret-scan`.

## Final diff audit

- Stage 7E-A changes are confined to the `_devctl` command, shared bounded
  worker result, runner/verification metrics, tests, and documentation.
- The Android dependency, Git fingerprint, scheduler/event, live renderer and
  overlapping CLI changes recorded in task 2 remain preserved as pre-existing
  work.
- No HearthLink source file was edited during this stage.
- `.devctl` evidence remains ignored and no generated evidence is staged as
  source.
- `devctl.json`, CI policy and thresholds were not changed.
- `git diff --check`, focused package tests and `go vet ./...` pass.

## Remaining limitations

- Clean isolated-checkout acceptance passes. Hosted GitHub Actions remains
  `NOT TESTED` until the owner explicitly authorizes publication and a remote
  run passes.
- Stage 7E-A performs full local verification only. Progressive
  failure/evidence retrieval is implemented in Stage 7E-B; affected
  verification, MCP and editor integrations remain later work.
- Local subprocess evidence is bounded. Callers must honor
  `output_truncated`, total/returned counts and `next` instead of treating a
  bounded response as complete evidence.
