# Stage 7C control and acceptance contract

Status: first vertical slice only

Stage 7C introduces an optional external worker boundary. The worker may
request deterministic verification, but `_devctl` remains the authority for
what executes, how results are classified, and whether the workflow stops.

## First vertical slice

```text
external worker
      |
      v
worker verify request (JSON)
      |
      v
_devctl validates the request
      |
      v
existing deterministic verification path
      |
      v
versioned structured result (JSON)
```

There is no repair loop, knowledge-vault lookup, arbitrary command field, or
worker-generated shell execution in this slice.

## Request schema

The request file is decoded with unknown fields rejected.

```json
{
  "schema_version": "1",
  "request_id": "req-20260817-001",
  "operation": "verify",
  "project_id": "devctl-project-id"
}
```

Required fields:

- `schema_version`: exactly `1`.
- `request_id`: non-empty, bounded identifier used to correlate the result.
- `operation`: exactly `verify`.
- `project_id`: an existing approved project identity in the devctl registry.

Protocol limits are fixed by `_devctl`, not by the worker: the request JSON is
limited to 64 KiB and `request_id` and `project_id` are limited to 128 Unicode
characters. Oversized or malformed requests are rejected before execution.

The registry resolves `project_id` to its canonical path and then reads the
project identity from the current `devctl.json` before execution. If the
registered identity and the current on-disk identity differ, the request is
rejected with `project_identity_mismatch`. The request has no
path override and no fields for commands, arguments, environment variables,
check selection, thresholds, retries, shell text, or policy changes.

## Result schema

Accepted requests return a bounded verification summary inside a versioned
envelope. The complete `report.json` remains in the project evidence
directory; it is not copied into the worker response.

```json
{
  "schema_version": "1",
  "request_id": "req-20260817-001",
  "operation": "verify",
  "accepted": true,
  "exit_code": 0,
  "project_id": "devctl-project-id",
  "run_id": "20260817T220000.000000000Z",
  "overall": "PASS",
  "evidence_path": ".devctl/evidence/20260817T220000.000000000Z",
  "checks": [],
  "failure_packet": {}
}
```

`failure_packet` is present when the report contains a failed, blocking, or
framework-error result. It is derived from the existing handoff format and
hard-bounded by the worker protocol. It contains summaries, findings, and
evidence paths rather than raw process output. Check summaries contain status,
blocking state, summary,
reason, and duration only. `raw_output` and complete check executions are
never part of the worker response. The packet is capped at 16 failure items,
8 findings and 16 evidence paths per item; packet text fields are capped at
512 characters. Error codes, messages and correlation fields are bounded too.

`devctl worker verify --live --request <file>` enables the existing live
renderer on stderr. It changes visibility only; the structured stdout result,
checks, status, evidence, and exit code remain the same as the non-live worker
request.

Rejected protocol requests return the same envelope with `accepted: false`,
an `error.code`, and an explanatory `error.message`. A deterministic report is
not fabricated for a request that was not accepted.

The process exit code remains aligned with ordinary `devctl verify`:

- `0`: non-blocking result, including `WARN`.
- `1`: deterministic `FAIL` or blocking verification result.
- `2`: protocol, registry/framework, or verification `ERROR`.

## Authority and allowed operations

The worker is allowed to submit one `verify` request for an already registered
project identity and consume the returned structured result. `_devctl` alone
decides:

- which adapters and checks apply;
- which commands are allowlisted and executed;
- status classification and exit code;
- policy thresholds and blocking behavior;
- evidence paths and lifecycle recording.

The worker cannot weaken thresholds, disable checks, bypass failures, provide
commands, provide shell text, or turn an `ERROR` into a `PASS`.

## Concurrency and cancellation

- Each accepted request creates one normal verification run and has one
  `run_id` in its report.
- Different approved projects may run concurrently with independent registry entries,
  workflow journals, and evidence directories.
- A second active request for the same project is rejected by the existing
  per-project run-state boundary.
- An external process interruption does not create a synthetic result. The
  registry can recover the interrupted run as stale on the next request.
- This first slice has no worker retry or repair loop. Any retry decision is
  outside the protocol and must submit a new bounded request.

## Parity acceptance

For the same project state and approved environment, ordinary `devctl verify`
and `devctl worker verify [--live] --request <file>` must use the same verification
function and produce the same bounded check vector, overall status, evidence
semantics, and exit-code mapping. The worker envelope adds correlation and
failure-packet fields but does not change deterministic verification.

## Explicit exclusions

Not part of this slice:

- iterative Codex repair;
- automatic file modification approval flows;
- arbitrary worker commands or shell execution;
- worker-controlled thresholds or check configuration;
- automatic commits, pushes, merges, or test disabling;
- knowledge-vault or lesson retrieval.
