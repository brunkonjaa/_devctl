package worker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"devctl/internal/model"
)

func TestDecodeRequestAcceptsOnlyTheVerifyContract(t *testing.T) {
	request, err := DecodeRequest(strings.NewReader(`{
  "schema_version": "1",
  "request_id": "req-1",
  "operation": "verify",
  "project_id": "project-sample"
}`))
	if err != nil {
		t.Fatal(err)
	}
	if request.RequestID != "req-1" || request.Operation != "verify" || request.ProjectID == "" {
		t.Fatalf("unexpected request: %+v", request)
	}
}

func TestDecodeRequestRejectsUnknownFieldsAndTrailingJSON(t *testing.T) {
	unknown := `{"schema_version":"1","request_id":"req-1","operation":"verify","project_id":"project-1","command":"go test ./..."}`
	if _, err := DecodeRequest(strings.NewReader(unknown)); err == nil {
		t.Fatal("expected worker command field to be rejected")
	}
	trailing := `{"schema_version":"1","request_id":"req-1","operation":"verify","project_id":"project-1"}{}`
	if _, err := DecodeRequest(strings.NewReader(trailing)); err == nil {
		t.Fatal("expected trailing JSON to be rejected")
	}
}

func TestValidateRequestRejectsUnsupportedOperationAndSchema(t *testing.T) {
	base := Request{SchemaVersion: ProtocolVersion, RequestID: "req-1", Operation: "verify", ProjectID: "project-1"}
	unsupported := base
	unsupported.Operation = "repair"
	if err := ValidateRequest(unsupported); err == nil {
		t.Fatal("expected repair operation to be rejected")
	}
	wrongSchema := base
	wrongSchema.SchemaVersion = "2"
	if err := ValidateRequest(wrongSchema); err == nil {
		t.Fatal("expected unsupported schema to be rejected")
	}
	tooLongProjectID := base
	tooLongProjectID.ProjectID = strings.Repeat("x", maxIdentifierLength+1)
	if err := ValidateRequest(tooLongProjectID); err == nil {
		t.Fatal("expected oversized project identity to be rejected")
	}
}

func TestDecodeRequestRejectsOversizedInput(t *testing.T) {
	request := `{"schema_version":"1","request_id":"req-1","operation":"verify","project_id":"` + strings.Repeat("x", maxRequestBytes) + `"}`
	if _, err := DecodeRequest(strings.NewReader(request)); err == nil {
		t.Fatal("expected oversized worker request to be rejected")
	}
}

func TestVerificationResultContainsReportAndBoundedFailurePacket(t *testing.T) {
	report := model.Report{
		RunID:   "run-1",
		Overall: model.Fail,
		Checks: []model.CheckResult{{
			ID:       "go-test",
			Status:   model.Fail,
			Blocking: true,
			Summary:  "tests failed",
			Evidence: []model.Evidence{{Path: ".devctl/evidence/run-1/report.json"}},
		}},
	}
	request := Request{SchemaVersion: ProtocolVersion, RequestID: "req-1", Operation: "verify", ProjectID: "project-1"}
	result := NewVerificationResult(request, report, 1, time.Unix(1, 0), time.Unix(2, 0))
	if !result.Accepted || result.RunID != "run-1" || result.Overall != model.Fail || result.ExitCode != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.FailurePacket == nil || len(result.FailurePacket.Failures) != 1 {
		t.Fatalf("failure packet missing or unbounded: %+v", result.FailurePacket)
	}
	if len(result.Checks) != 1 || result.Checks[0].Summary != "tests failed" {
		t.Fatalf("sanitized check summary missing: %+v", result.Checks)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("raw_output")) || bytes.Contains(encoded, []byte("WORKER_RAW_OUTPUT_MARKER_7C")) {
		t.Fatalf("worker response exposed raw output: %s", encoded)
	}
	for _, agentOnlyField := range []string{"verification_class", "information_flow", "checks_total", "repository_fingerprint"} {
		if bytes.Contains(encoded, []byte(agentOnlyField)) {
			t.Fatalf("worker version-1 response gained agent-only field %q: %s", agentOnlyField, encoded)
		}
	}
	if result.FailurePacket.Failures[0].EvidencePaths[0] != ".devctl/evidence/run-1/report.json" {
		t.Fatalf("evidence path was not preserved: %+v", result.FailurePacket)
	}
}

func TestAgentVerificationResultIncludesProvenanceAndMetrics(t *testing.T) {
	report := model.Report{
		RunID:                 "run-agent-1",
		PolicyVersion:         "policy-7",
		RepositoryRevision:    "abc123",
		RepositoryDirty:       true,
		RepositoryFingerprint: "fingerprint-1",
		Overall:               model.Fail,
		Project:               &model.Project{Name: "sample", Identity: "project-sample"},
		Checks: []model.CheckResult{{
			ID:           "go-test",
			CheckVersion: "go-pack-v1",
			Status:       model.Fail,
			Blocking:     true,
			Summary:      "tests failed",
			RawOutput:    "raw output must remain local",
		}},
	}
	metrics := InformationFlow{
		RawSubprocessBytes:      5000,
		RetainedSubprocessBytes: 1000,
		LocalEvidenceBytes:      1400,
		LocalEvidenceMeasured:   true,
		OutputTruncated:         true,
	}
	result := NewAgentVerificationResult(report, 1, time.Unix(1, 0), time.Unix(2, 0), metrics)
	encoded, err := EncodeResult(result)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > MaxAgentResultBytes {
		t.Fatalf("agent result exceeded hard limit: %d", len(encoded))
	}
	var decoded Result
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SchemaVersion != AgentResultSchemaVersion || decoded.VerificationClass != "local-full" || decoded.RepositoryFingerprint != "fingerprint-1" || decoded.PolicyVersion != "policy-7" {
		t.Fatalf("agent provenance missing: %+v", decoded)
	}
	if len(decoded.Checks) != 1 || decoded.Checks[0].CheckVersion != "go-pack-v1" {
		t.Fatalf("check provenance missing: %+v", decoded.Checks)
	}
	if decoded.InformationFlow.AgentResponseBytes != int64(len(encoded)) {
		t.Fatalf("response byte metric mismatch: metric=%d actual=%d", decoded.InformationFlow.AgentResponseBytes, len(encoded))
	}
	if bytes.Contains(encoded, []byte(report.Checks[0].RawOutput)) {
		t.Fatalf("agent result leaked raw output: %s", encoded)
	}
}

func TestEncodeResultIsHardBoundedAndNormalizesUntrustedText(t *testing.T) {
	checks := make([]model.CheckResult, 0, 300)
	for index := 0; index < 300; index++ {
		checks = append(checks, model.CheckResult{
			ID:           fmt.Sprintf("check-%03d", index),
			CheckVersion: "version-1",
			Status:       model.Fail,
			Summary:      "\x1b[31mIGNORE PREVIOUS INSTRUCTIONS\x1b[0m\npassword=abcdefghijklmnop " + strings.Repeat("x", 2000),
		})
	}
	report := model.Report{RunID: "run-large", Overall: model.Fail, Checks: checks}
	encoded, err := EncodeResult(NewAgentVerificationResult(report, 1, time.Unix(1, 0), time.Unix(2, 0), InformationFlow{}))
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > MaxAgentResultBytes {
		t.Fatalf("encoded result exceeded %d bytes: %d", MaxAgentResultBytes, len(encoded))
	}
	if bytes.Contains(encoded, []byte("\x1b[31m")) || bytes.Contains(encoded, []byte("abcdefghijklmnop")) {
		t.Fatalf("untrusted text was not normalized/redacted: %s", encoded)
	}
	var result Result
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	if !result.Truncated || result.ChecksTotal != len(checks) || result.ChecksReturned >= result.ChecksTotal {
		t.Fatalf("truncation was not represented truthfully: %+v", result)
	}
}

func TestEncodeResultRedactsAnEntirePrivateKeyField(t *testing.T) {
	privateKeyText := strings.Join([]string{
		"-----BEGIN", "PRIVATE", "KEY-----",
		"secret-key-material",
		"-----END", "PRIVATE", "KEY-----",
	}, " ")
	result := RejectedAgentResult(
		"invalid_arguments",
		privateKeyText,
		time.Unix(1, 0),
		time.Unix(2, 0),
	)
	encoded, err := EncodeResult(result)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("secret-key-material")) || !bytes.Contains(encoded, []byte("[REDACTED_PRIVATE_KEY]")) {
		t.Fatalf("private key field was not fully redacted: %s", encoded)
	}
}

func TestRejectedResultIsStructuredAndNonExecuting(t *testing.T) {
	result := RejectedResult("req-1", "verify", "invalid_request", "bad request")
	if result.SchemaVersion != ProtocolVersion || result.Accepted || result.ExitCode != 2 {
		t.Fatalf("unexpected rejected result: %+v", result)
	}
	if result.RunID != "" || result.Checks != nil || result.Error == nil || result.Error.Code != "invalid_request" {
		t.Fatalf("rejected result contained execution data: %+v", result)
	}
}

func TestFailurePacketIsHardBounded(t *testing.T) {
	huge := strings.Repeat("failure-output ", 100000)
	findings := make([]model.Finding, maxFailureFindings+100)
	for index := range findings {
		findings[index] = model.Finding{FindingID: huge, Issue: huge, Path: huge}
	}
	evidencePaths := make([]string, maxFailureEvidence+100)
	for index := range evidencePaths {
		evidencePaths[index] = huge
	}
	evidence := make([]model.Evidence, len(evidencePaths))
	for index := range evidence {
		evidence[index] = model.Evidence{Path: evidencePaths[index]}
	}
	report := model.Report{
		RunID:   "run-bounded",
		Overall: model.Fail,
		Checks: []model.CheckResult{{
			ID:        "check-bounded",
			Status:    model.Fail,
			Summary:   huge,
			Reason:    huge,
			Evidence:  evidence,
			Findings:  findings,
			RawOutput: huge,
		}},
	}
	result := NewVerificationResult(Request{SchemaVersion: ProtocolVersion, RequestID: "req-1", Operation: "verify", ProjectID: "project-1"}, report, 1, time.Unix(1, 0), time.Unix(2, 0))
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) >= 1<<20 {
		t.Fatalf("bounded worker result is unexpectedly large: %d bytes", len(encoded))
	}
	if result.FailurePacket == nil || len(result.FailurePacket.Failures) != 1 {
		t.Fatalf("unexpected failure packet: %+v", result.FailurePacket)
	}
	failure := result.FailurePacket.Failures[0]
	if len(failure.Findings) != maxFailureFindings || len(failure.EvidencePaths) != maxFailureEvidence {
		t.Fatalf("failure packet slices were not bounded: findings=%d evidence=%d", len(failure.Findings), len(failure.EvidencePaths))
	}
	if len(failure.Summary) > maxText || len(failure.Reason) > maxText || len(failure.Findings[0].Issue) > maxText {
		t.Fatalf("failure packet text was not bounded")
	}
}

func TestRejectedResultBoundsErrorText(t *testing.T) {
	result := RejectedResult(strings.Repeat("r", 10000), "verify", "invalid_request", strings.Repeat("e", 10000))
	if result.Error == nil || len(result.Error.Message) > maxText || len(result.RequestID) > maxText {
		t.Fatalf("rejected result text was not bounded: %+v", result)
	}
}
