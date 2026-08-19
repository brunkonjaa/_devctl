package golang

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"devctl/internal/model"
	"devctl/internal/runner"
)

func TestRaceEnvironmentReportsMissingCompilerAsUnavailable(t *testing.T) {
	available, reason := raceEnvironmentFor("windows", "1", func(string) bool { return false })
	if available {
		t.Fatal("expected race environment to be unavailable")
	}
	if reason == "" {
		t.Fatal("expected a reason for unavailable race environment")
	}
}

func TestRaceEnvironmentReportsCGORequirement(t *testing.T) {
	available, reason := raceEnvironmentFor("windows", "0", func(string) bool { return true })
	if available || reason != "CGO_ENABLED=0; the Go race detector requires cgo" {
		t.Fatalf("expected cgo-disabled explanation, got available=%v reason=%q", available, reason)
	}
}

func TestCommandResultClassifiesProcessExitAsFail(t *testing.T) {
	result := runner.Result{Started: true, TerminationReason: "completed", ExitCode: 1, Output: "--- FAIL: TestExample"}
	check := commandResult("go-test", result, errors.New("exit status 1"), "Go tests could not be completed")
	if check.Status != model.Fail || check.Summary != "Go tests failed" {
		t.Fatalf("expected project failure, got %#v", check)
	}
}

func TestCommandResultKeepsExecutorFailureAsError(t *testing.T) {
	result := runner.Result{Started: false}
	check := commandResult("go-test", result, errors.New("executable not found"), "Go tests could not be completed")
	if check.Status != model.Error {
		t.Fatalf("expected executor error, got %#v", check)
	}
}

func TestGoTestCannotBeSuppressedByInheritedGoFlags(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example\n\ngo 1.22\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "fail_test.go"), []byte("package example\n\nimport \"testing\"\n\nfunc TestIntentionalFailure(t *testing.T) { t.Fatal(\"expected failure\") }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOFLAGS", "-run=DOESNOTEXIST")
	check := commandCheck(model.Project{Path: root}, runner.GoTest, "Go tests")(context.Background())
	if check.Status != model.Fail {
		t.Fatalf("expected inherited GOFLAGS to be neutralized, got %#v", check)
	}
}

func TestGoToolchainChecksShareExclusiveResource(t *testing.T) {
	checks := Checks(model.Project{Path: t.TempDir()})
	want := map[string]bool{
		"go-test":      true,
		"go-test-race": true,
		"go-build":     true,
	}
	for _, check := range checks {
		if !want[check.ID] {
			continue
		}
		if len(check.Resources) != 1 || check.Resources[0] != "go-toolchain" {
			t.Fatalf("expected %s to exclusively use go-toolchain, got %v", check.ID, check.Resources)
		}
		delete(want, check.ID)
	}
	if len(want) != 0 {
		t.Fatalf("missing Go toolchain checks: %v", want)
	}
}
