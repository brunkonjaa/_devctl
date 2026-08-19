package main

import (
	"path/filepath"
	"testing"
)

func TestExecuteVerificationRejectsMissingProjectBeforeRunningChecks(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-project")
	_, exitCode, err := executeVerification(missing, false)
	if err == nil {
		t.Fatal("expected missing project error")
	}
	if exitCode != exitInternal {
		t.Fatalf("expected internal exit code %d, got %d", exitInternal, exitCode)
	}
}
