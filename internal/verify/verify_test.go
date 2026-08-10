package verify

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"devctl/internal/model"
)

func TestProjectReportsInsufficientEvidenceForUnknownProject(t *testing.T) {
	root := t.TempDir()
	report := Project(context.Background(), root)
	if report.Overall != model.InsufficientEvidence {
		t.Fatalf("expected insufficient evidence, got %s", report.Overall)
	}
}

func TestProjectDetectsTechnologyAndGitStatus(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	report := Project(context.Background(), root)
	if report.Project == nil || len(report.Project.Technologies) != 1 {
		t.Fatalf("expected detected project technology: %#v", report.Project)
	}
	if len(report.Checks) != 3 {
		t.Fatalf("expected three checks, got %d", len(report.Checks))
	}
	if report.Checks[1].Status != model.Error {
		t.Fatalf("expected git error outside a repository, got %s", report.Checks[1].Status)
	}
}
