package progressive

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"devctl/internal/evidence"
	"devctl/internal/model"
)

func TestFailuresReturnsAnEmptyProvenancedResultForPassRun(t *testing.T) {
	root := t.TempDir()
	report := progressiveReport("run-pass", model.Pass, []model.CheckResult{{ID: "go-test", CheckVersion: "go-v1", Status: model.Pass, Summary: "passed"}})
	if _, err := evidence.Write(root, report); err != nil {
		t.Fatal(err)
	}
	result, err := Failures(root, report.RunID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Accepted || result.FailuresTotal != 0 || result.FailuresReturned != 0 || len(result.Failures) != 0 {
		t.Fatalf("unexpected PASS failure list: %+v", result)
	}
	if result.RepositoryRevision != report.RepositoryRevision || result.RepositoryFingerprint != report.RepositoryFingerprint || result.PolicyVersion != report.PolicyVersion {
		t.Fatalf("verification provenance missing: %+v", result)
	}
}

func TestEncodePaginatesOversizedHostileFailureCollections(t *testing.T) {
	checks := make([]model.CheckResult, 0, 200)
	secretValue := "abcdefghijklmnop"
	for index := 0; index < 200; index++ {
		checks = append(checks, model.CheckResult{
			ID:           fmtIndex("check-", index),
			CheckVersion: "version-1",
			Status:       model.Fail,
			Blocking:     true,
			Summary:      "\x1b[31mpassword=" + secretValue + " " + strings.Repeat("x", 2000),
		})
	}
	root := t.TempDir()
	report := progressiveReport("run-large", model.Fail, checks)
	if _, err := evidence.Write(root, report); err != nil {
		t.Fatal(err)
	}
	result, err := Failures(root, report.RunID, 0)
	if err != nil {
		t.Fatal(err)
	}
	originalSummary := result.Failures[0].Summary
	encoded, err := Encode(result)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > MaxResultBytes {
		t.Fatalf("response exceeded %d bytes: %d", MaxResultBytes, len(encoded))
	}
	if bytes.Contains(encoded, []byte(secretValue)) || bytes.Contains(encoded, []byte("\x1b[31m")) {
		t.Fatalf("hostile text crossed the boundary: %s", encoded)
	}
	var decoded Result
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.Truncated || decoded.FailuresTotal != len(checks) || decoded.FailuresReturned >= decoded.FailuresTotal || decoded.NextItemsOffset != int64(decoded.FailuresReturned) || decoded.Next == "" {
		t.Fatalf("pagination is not truthful: %+v", decoded)
	}
	if decoded.AgentResponseBytes != int64(len(encoded)) {
		t.Fatalf("response size mismatch: metric=%d actual=%d", decoded.AgentResponseBytes, len(encoded))
	}
	if result.Failures[0].Summary != originalSummary {
		t.Fatal("encoding mutated the caller-owned result")
	}
}

func TestFailureDetailPaginatesFindingsAndEvidenceReferences(t *testing.T) {
	findings := make([]model.Finding, 0, 100)
	evidenceItems := make([]model.Evidence, 0, maxEvidencePaths+10)
	for index := 0; index < 100; index++ {
		findings = append(findings, model.Finding{FindingID: fmtIndex("finding-", index), Issue: strings.Repeat("issue ", 300)})
	}
	for index := 0; index < maxEvidencePaths+10; index++ {
		evidenceItems = append(evidenceItems, model.Evidence{Path: "evidence/path-" + strconv.Itoa(index)})
	}
	root := t.TempDir()
	report := progressiveReport("run-detail", model.Fail, []model.CheckResult{{ID: "go-test", Status: model.Fail, Blocking: true, Summary: "failed", Findings: findings, Evidence: evidenceItems}})
	if _, err := evidence.Write(root, report); err != nil {
		t.Fatal(err)
	}
	result, err := FailureDetail(root, report.RunID, "go-test", 0)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := Encode(result)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Result
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Failure == nil || decoded.Failure.FindingsTotal != len(findings) || decoded.Failure.FindingsReturned >= decoded.Failure.FindingsTotal {
		t.Fatalf("finding pagination is incomplete: %+v", decoded)
	}
	if decoded.Failure.EvidencePathsTotal != len(evidenceItems) || decoded.Failure.EvidencePathsReturned != maxEvidencePaths {
		t.Fatalf("evidence reference counts are not truthful: %+v", decoded.Failure)
	}
	if !decoded.Truncated || decoded.Next == "" {
		t.Fatalf("detail truncation is not actionable: %+v", decoded)
	}
}

func TestEncodeKeepsEvidenceReferenceReturnedCountTruthful(t *testing.T) {
	long := strings.Repeat("x", 2000)
	paths := make([]string, maxEvidencePaths)
	for index := range paths {
		paths[index] = long + strconv.Itoa(index)
	}
	result := Result{
		SchemaVersion:         SchemaVersion,
		Operation:             "failure",
		Accepted:              true,
		RunID:                 long,
		ProjectID:             long,
		EvidencePath:          long,
		DevctlVersion:         long,
		DevctlCommit:          long,
		PolicyVersion:         long,
		RepositoryRevision:    long,
		RepositoryFingerprint: long,
		FailuresTotal:         1,
		FailuresReturned:      1,
		Evidence:              &EvidenceFragment{Content: strings.Repeat("y", 4000)},
		Failure: &Failure{
			FailureID:             long,
			CheckID:               long,
			CheckVersion:          long,
			Status:                model.Fail,
			Summary:               long,
			Reason:                long,
			EvidencePathsTotal:    len(paths),
			EvidencePathsReturned: len(paths),
			EvidencePaths:         paths,
		},
	}
	encoded, err := Encode(result)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Result
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Failure == nil || len(decoded.Failure.EvidencePaths) >= len(paths) {
		t.Fatalf("fixture did not force evidence-reference truncation: %+v", decoded.Failure)
	}
	if decoded.Failure.EvidencePathsReturned != len(decoded.Failure.EvidencePaths) {
		t.Fatalf("evidence reference count is stale: returned=%d actual=%d", decoded.Failure.EvidencePathsReturned, len(decoded.Failure.EvidencePaths))
	}
}

func TestLoadRunRejectsMismatchedAndDuplicateEvidence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*model.Report)
	}{
		{name: "mismatched run", mutate: func(report *model.Report) { report.RunID = "different-run" }},
		{name: "duplicate check", mutate: func(report *model.Report) { report.Checks = append(report.Checks, report.Checks[0]) }},
		{name: "invalid check", mutate: func(report *model.Report) { report.Checks[0].ID = "../check" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			report := progressiveReport("run-invalid", model.Fail, []model.CheckResult{{ID: "go-test", Status: model.Fail, Summary: "failed"}})
			if _, err := evidence.Write(root, report); err != nil {
				t.Fatal(err)
			}
			test.mutate(&report)
			report.EvidencePath = filepath.ToSlash(filepath.Join(".devctl", "evidence", "run-invalid"))
			data, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, ".devctl", "evidence", "run-invalid", "report.json")
			if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Failures(root, "run-invalid", 0); ErrorCode(err) != "evidence_invalid" {
				t.Fatalf("expected evidence_invalid, got %v (%s)", err, ErrorCode(err))
			}
		})
	}
}

func TestSelectedEvidenceRejectsLinkAndRedactsPrivateKeyAcrossChunks(t *testing.T) {
	root := t.TempDir()
	begin := privateKeyMarkers("BEGIN")[0]
	end := privateKeyMarkers("END")[0]
	keyMaterial := strings.Repeat("SENSITIVEKEYMATERIAL", 300)
	raw := begin + "\n" + keyMaterial + "\n" + end + "\n"
	report := progressiveReport("run-key", model.Fail, []model.CheckResult{{ID: "go-test", Status: model.Fail, Blocking: true, Summary: "failed", RawOutput: raw}})
	if _, err := evidence.Write(root, report); err != nil {
		t.Fatal(err)
	}
	first, err := SelectedEvidence(root, report.RunID, "go-test", 0)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := Encode(first)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("SENSITIVEKEYMATERIAL")) || !bytes.Contains(encoded, []byte("[REDACTED_PRIVATE_KEY]")) {
		t.Fatalf("private key material was exposed: %s", encoded)
	}
	if first.Evidence == nil || first.Evidence.NextOffset == 0 {
		t.Fatalf("fixture did not cross a chunk boundary: %+v", first)
	}
	second, err := SelectedEvidence(root, report.RunID, "go-test", first.Evidence.NextOffset)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err = Encode(second)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("SENSITIVEKEYMATERIAL")) {
		t.Fatalf("continued private key material was exposed: %s", encoded)
	}

	rawPath := filepath.Join(root, ".devctl", "evidence", report.RunID, "raw", "go-test.log")
	outside := filepath.Join(t.TempDir(), "outside.log")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(rawPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, rawPath); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	if _, err := SelectedEvidence(root, report.RunID, "go-test", 0); ErrorCode(err) != "evidence_boundary" {
		t.Fatalf("expected evidence_boundary, got %v (%s)", err, ErrorCode(err))
	}
}

func TestSelectedEvidenceRedactsPrivateKeyMarkerSplitAtChunkBoundary(t *testing.T) {
	root := t.TempDir()
	begin := privateKeyMarkers("BEGIN")[0]
	end := privateKeyMarkers("END")[0]
	split := len(begin) / 2
	raw := strings.Repeat("x", maxRawChunkBytes-split) + begin + "\nPRIVATEKEYBODYMUSTNOTLEAK\n" + end + "\n"
	report := progressiveReport("run-split-key", model.Fail, []model.CheckResult{{ID: "go-test", Status: model.Fail, Blocking: true, Summary: "failed", RawOutput: raw}})
	if _, err := evidence.Write(root, report); err != nil {
		t.Fatal(err)
	}

	second, err := SelectedEvidence(root, report.RunID, "go-test", maxRawChunkBytes)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := Encode(second)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("PRIVATEKEYBODYMUSTNOTLEAK")) {
		t.Fatalf("private key body crossed a split marker boundary: %s", encoded)
	}
}

func TestSelectedEvidenceRedactsTrailingPartialSecretLine(t *testing.T) {
	root := t.TempDir()
	partial := "token=ABCD"
	raw := strings.Repeat("x", maxRawChunkBytes-len(partial)) + partial + "EFGHIJKLMNOPQRSTUVWXYZ\n"
	report := progressiveReport("run-split-secret", model.Fail, []model.CheckResult{{ID: "go-test", Status: model.Fail, Blocking: true, Summary: "failed", RawOutput: raw}})
	if _, err := evidence.Write(root, report); err != nil {
		t.Fatal(err)
	}

	first, err := SelectedEvidence(root, report.RunID, "go-test", 0)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := Encode(first)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(partial)) {
		t.Fatalf("partial secret assignment crossed the chunk boundary: %s", encoded)
	}
}

func TestSanitizeProtectsHumanReadableFields(t *testing.T) {
	secretValue := "abcdefghijklmnop"
	result := Sanitize(Result{
		Operation: "failure",
		Failure: &Failure{
			FailureID: "go-test",
			CheckID:   "go-test",
			Status:    model.Fail,
			Summary:   "\x1b[31mtoken=" + secretValue,
		},
	})
	if result.Failure == nil || strings.Contains(result.Failure.Summary, secretValue) || strings.Contains(result.Failure.Summary, "\x1b[31m") {
		t.Fatalf("human-readable fields were not sanitized: %+v", result.Failure)
	}
}

func progressiveReport(runID string, overall model.Status, checks []model.CheckResult) model.Report {
	return model.Report{
		SchemaVersion:         "1",
		Command:               "verify",
		RunID:                 runID,
		PolicyVersion:         "policy-1",
		DevctlVersion:         "0.1.0",
		DevctlCommit:          "devctl-commit",
		RepositoryRevision:    "repository-commit",
		RepositoryFingerprint: "repository-fingerprint",
		EvidencePath:          filepath.ToSlash(filepath.Join(".devctl", "evidence", runID)),
		StartedAt:             time.Unix(1, 0).UTC(),
		FinishedAt:            time.Unix(2, 0).UTC(),
		Project:               &model.Project{Name: "fixture", Identity: "project-fixture"},
		Checks:                checks,
		Overall:               overall,
	}
}

func fmtIndex(prefix string, value int) string {
	text := strconv.Itoa(value)
	return prefix + strings.Repeat("0", 3-len(text)) + text
}
