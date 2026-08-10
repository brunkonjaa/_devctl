package evidence

import (
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
		Checks:  []model.CheckResult{{ID: "secret-scan", Status: model.Pass, Summary: "clean", RawOutput: "scanner output"}},
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
}
