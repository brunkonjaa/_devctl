package evidence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"devctl/internal/model"
)

func TestWriteCreatesReconstructableEvidenceTree(t *testing.T) {
	root := t.TempDir()
	report := model.Report{
		RunID:   "20260810T120000.000000000Z",
		Project: &model.Project{Name: "Example"},
		Checks:  []model.CheckResult{{ID: "secret-scan", CheckVersion: "secret-pack-v1", Status: model.Pass, Summary: "clean", RawOutput: "scanner output"}},
		Overall: model.Pass,
	}
	path, err := Write(root, report)
	if err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"report.json", "summary.txt", filepath.Join("checks", "secret-scan.json"), filepath.Join("raw", "secret-scan.log")} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path), relative)); err != nil {
			t.Fatalf("missing evidence file %s: %v", relative, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path), "checks", "secret-scan.json"))
	if err != nil {
		t.Fatal(err)
	}
	var persisted model.CheckResult
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.CheckVersion != "secret-pack-v1" {
		t.Fatalf("expected check version in per-check evidence, got %#v", persisted)
	}
}

func TestWriteCopiesCoverageReportArtifact(t *testing.T) {
	root := t.TempDir()
	reportPath := filepath.Join(root, "app", "build", "reports", "jacoco", "jacocoTestReport.xml")
	if err := os.MkdirAll(filepath.Dir(reportPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reportPath, []byte("<report/>"), 0o644); err != nil {
		t.Fatal(err)
	}

	path, err := Write(root, model.Report{
		RunID:   "coverage-run",
		Project: &model.Project{Name: "Example", Path: root},
		Checks:  []model.CheckResult{{ID: "android-coverage", Status: model.Pass, Evidence: []model.Evidence{{Type: "coverage-report", Path: reportPath}}}},
		Overall: model.Pass,
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(root, filepath.FromSlash(path), "artifacts", "android-coverage.xml")
	data, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "<report/>" {
		t.Fatalf("unexpected coverage artifact: %q", data)
	}
}

func TestWriteRejectsSymlinkedEvidenceDirectory(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, ".devctl")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, err := Write(root, model.Report{RunID: "escape", Project: &model.Project{Name: "Example"}})
	if err == nil {
		t.Fatal("expected symlinked evidence path to be rejected")
	}
}
