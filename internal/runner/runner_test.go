package runner

import (
	"context"
	"testing"
)

func TestRunRejectsNonAllowlistedCommand(t *testing.T) {
	_, err := Run(context.Background(), t.TempDir(), CommandID("arbitrary-command"))
	if err == nil {
		t.Fatal("expected non-allowlisted command to be rejected")
	}
}

func TestRunRejectsNonDirectory(t *testing.T) {
	_, err := Run(context.Background(), t.TempDir()+".missing", GitStatus)
	if err == nil {
		t.Fatal("expected missing project path to be rejected")
	}
}
