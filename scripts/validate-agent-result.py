#!/usr/bin/env python3
"""Validate the bounded result produced by clean CI's devctl executable."""

from __future__ import annotations

import argparse
import json
from pathlib import Path
from typing import Any


MAX_AGENT_RESULT_BYTES = 16 * 1024
EXPECTED_CHECKS = {
    "technology-detection",
    "git-status",
    "go-environment",
    "go-build",
    "go-test",
    "go-test-race",
    "secret-scan",
}


def require(condition: bool, message: str) -> None:
    if not condition:
        raise ValueError(message)


def load_one_result(path: Path) -> tuple[dict[str, Any], bytes]:
    data = path.read_bytes()
    require(data.endswith(b"\n"), "agent result must end with one newline")
    require(len(data) <= MAX_AGENT_RESULT_BYTES, "agent result exceeds 16 KiB")
    require(b'"raw_output"' not in data, "agent result contains raw output")
    value = json.loads(data)
    require(isinstance(value, dict), "agent result must be one JSON object")
    return value, data


def validate(result_path: Path, stderr_path: Path, expected_commit: str) -> None:
    result, data = load_one_result(result_path)
    require(stderr_path.read_bytes() == b"", "agent verification wrote to stderr")
    require(result.get("schema_version") == "1", "unexpected agent schema")
    require(result.get("operation") == "verify", "unexpected operation")
    require(result.get("verification_class") == "local-full", "unexpected verification class")
    require(result.get("accepted") is True, "agent verification was not accepted")
    require(result.get("exit_code") == 0, "agent verification exit code was not zero")
    require(result.get("overall") == "PASS", "clean CI result was not PASS")
    require(result.get("devctl_commit") == expected_commit, "devctl commit does not match CI commit")
    require(result.get("repository_revision") == expected_commit, "repository revision does not match CI commit")
    require(result.get("devctl_dirty", False) is False, "devctl provenance is dirty")
    require(result.get("repository_dirty", False) is False, "repository provenance is dirty")
    require(bool(result.get("repository_fingerprint")), "repository fingerprint is missing")
    require(bool(result.get("policy_version")), "policy version is missing")
    require(result.get("error") is None, "agent result contains an error")
    require(result.get("failure_packet") is None, "clean CI result contains a failure packet")

    checks = result.get("checks")
    require(isinstance(checks, list), "agent checks are missing")
    check_ids = [check.get("check_id") for check in checks]
    require(len(check_ids) == len(set(check_ids)), "agent result contains duplicate checks")
    require(set(check_ids) == EXPECTED_CHECKS, "agent result check vector is incomplete or unexpected")
    require(result.get("checks_total") == len(checks), "checks_total does not match checks")
    require(result.get("checks_returned") == len(checks), "checks_returned does not match checks")
    for check in checks:
        require(check.get("status") == "PASS", f"check {check.get('check_id')} was not PASS")
        require(bool(check.get("check_version")), f"check {check.get('check_id')} has no version")

    flow = result.get("information_flow")
    require(isinstance(flow, dict), "information-flow metrics are missing")
    require(flow.get("local_evidence_measured") is True, "local evidence was not measured")
    require(flow.get("local_evidence_bytes", 0) > 0, "local evidence byte count is empty")
    require(flow.get("raw_subprocess_bytes", -1) >= flow.get("retained_subprocess_bytes", 0), "subprocess byte counts are inconsistent")
    require(flow.get("output_truncated") is False, "clean CI subprocess output was truncated")
    require(flow.get("agent_response_bytes") == len(data), "agent response byte count is inconsistent")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--result", required=True, type=Path)
    parser.add_argument("--stderr", required=True, type=Path)
    parser.add_argument("--commit", required=True)
    args = parser.parse_args()
    validate(args.result, args.stderr, args.commit)
    print("clean agent verification contract: PASS")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
