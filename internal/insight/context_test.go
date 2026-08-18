package insight

import (
	"devctl/internal/evidence"
	"devctl/internal/knowledge"
	"devctl/internal/model"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildReturnsBoundedCurrentContextAndRelevantLesson(t *testing.T) {
	root := t.TempDir()
	if err := writeFile(root, "go.mod", []byte("module example.test\ngo 1.22\n")); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(root, "devctl.json", []byte(`{"version":"1","project_id":"context-project"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := evidence.Write(root, model.Report{RunID: "run-1", RepositoryRevision: "old-head", RepositoryFingerprint: "old-worktree", Project: &model.Project{Name: "context", Path: root}, Checks: []model.CheckResult{{ID: "go-test", Status: model.Fail, Summary: "tests failed"}}, Overall: model.Fail}); err != nil {
		t.Fatal(err)
	}
	if _, err := knowledge.Write(root, knowledge.Lesson{Project: "context-project", Check: "go-test", Problem: "tests failed", RootCause: "known fixture", Solution: "use the documented fixture", Success: true}); err != nil {
		t.Fatal(err)
	}
	value, err := Build(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(value.CurrentFailures) != 1 || value.CurrentFailures[0].CheckID != "go-test" {
		t.Fatalf("unexpected failures: %+v", value.CurrentFailures)
	}
	if value.Repository.EvidenceCurrent {
		t.Fatal("evidence with a different worktree fingerprint was reported current")
	}
	if len(value.RelevantLessons) != 1 {
		t.Fatalf("expected relevant lesson: %+v", value.RelevantLessons)
	}
	data, err := JSON(root)
	if err != nil || len(data) > maxContextBytes {
		t.Fatalf("context was not bounded: %v %d", err, len(data))
	}
}

func writeFile(root, name string, data []byte) error {
	return os.WriteFile(filepath.Join(root, name), data, 0o600)
}
