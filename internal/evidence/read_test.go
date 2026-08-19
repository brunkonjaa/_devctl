package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"devctl/internal/model"
)

func TestReadRunLoadsOnlyExactContainedReport(t *testing.T) {
	root := t.TempDir()
	report := model.Report{SchemaVersion: "1", RunID: "run-1", EvidencePath: filepath.ToSlash(filepath.Join(".devctl", "evidence", "run-1")), StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC(), Checks: []model.CheckResult{{ID: "go-test", Status: model.Fail}}}
	path := filepath.Join(root, ".devctl", "evidence", report.RunID, "report.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := ReadRun(root, report.RunID)
	if err != nil {
		t.Fatal(err)
	}
	expectedHash := sha256.Sum256(data)
	if loaded.Report.RunID != report.RunID || loaded.CanonicalRoot == "" || loaded.ReportSHA256 != hex.EncodeToString(expectedHash[:]) {
		t.Fatalf("unexpected loaded run: %+v", loaded)
	}
}

func TestReadRunRejectsUnknownFieldsAndNonNormalEvidenceRoot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".devctl", "evidence")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRun(root, "run-1"); ErrorCode(err) != "evidence_boundary" {
		t.Fatalf("expected evidence_boundary, got %v (%s)", err, ErrorCode(err))
	}

	root = t.TempDir()
	reportPath := filepath.Join(root, ".devctl", "evidence", "run-1", "report.json")
	if err := os.MkdirAll(filepath.Dir(reportPath), 0o700); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"schema_version":"1","run_id":"run-1","evidence_path":".devctl/evidence/run-1","started_at":"0001-01-01T00:00:00Z","finished_at":"0001-01-01T00:00:00Z","overall":"PASS","unknown":true}`)
	if err := os.WriteFile(reportPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRun(root, "run-1"); ErrorCode(err) != "evidence_invalid" {
		t.Fatalf("expected evidence_invalid, got %v (%s)", err, ErrorCode(err))
	}
}

func TestReadRunRejectsMismatchedDuplicateAndTraversalEvidence(t *testing.T) {
	tests := []struct {
		name   string
		runID  string
		mutate func(*model.Report)
		code   string
	}{
		{name: "invalid request id", runID: "../run", code: "invalid_run_id"},
		{name: "mismatched report", runID: "run-1", mutate: func(report *model.Report) { report.RunID = "run-2" }, code: "evidence_invalid"},
		{name: "duplicate check", runID: "run-1", mutate: func(report *model.Report) { report.Checks = append(report.Checks, report.Checks[0]) }, code: "evidence_invalid"},
		{name: "invalid check", runID: "run-1", mutate: func(report *model.Report) { report.Checks[0].ID = "../check" }, code: "evidence_invalid"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			report := model.Report{SchemaVersion: "1", RunID: "run-1", EvidencePath: filepath.ToSlash(filepath.Join(".devctl", "evidence", "run-1")), Checks: []model.CheckResult{{ID: "go-test", Status: model.Fail}}}
			if test.mutate != nil {
				test.mutate(&report)
			}
			path := filepath.Join(root, ".devctl", "evidence", "run-1", "report.json")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := ReadRun(root, test.runID); ErrorCode(err) != test.code {
				t.Fatalf("expected %s, got %v (%s)", test.code, err, ErrorCode(err))
			}
		})
	}
}
