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
