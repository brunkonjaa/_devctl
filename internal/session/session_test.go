package session

import (
	"os"
	"path/filepath"
	"testing"

	"devctl/internal/model"
)

func TestRecordIsAtomicAndLoadRejectsCorruptState(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DEVCTL_STATE_DIR", dir)
	path, err := Record(model.SessionState{Project: "demo", ProjectPath: filepath.Join(dir, "project"), LastResult: model.Pass})
	if err != nil {
		t.Fatal(err)
	}
	state, err := Load()
	if err != nil || state.SchemaVersion != "1" || state.Project != "demo" || state.UpdatedAt.IsZero() {
		t.Fatalf("unexpected state: %#v, %v", state, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("state file was not written: %v", err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("expected corrupt state to be rejected")
	}
}

func TestRecordPreservesLatestTaskWhenProjectChanges(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DEVCTL_STATE_DIR", dir)
	if _, err := Record(model.SessionState{Project: "first", ProjectPath: filepath.Join(dir, "first"), CurrentTask: "latest task"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Record(model.SessionState{Project: "second", ProjectPath: filepath.Join(dir, "second")}); err != nil {
		t.Fatal(err)
	}
	state, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.Project != "second" || state.CurrentTask != "latest task" {
		t.Fatalf("expected latest project with preserved task, got %#v", state)
	}
}
