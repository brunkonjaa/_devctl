package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devctl/internal/events"
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

func TestRunCapturesShortProcessOutputReliably(t *testing.T) {
	for attempt := 0; attempt < 25; attempt++ {
		result, err := RunWithOptions(context.Background(), t.TempDir(), GoVersion, TimeoutPolicy{Hard: 5 * time.Second, Inactivity: time.Second})
		if err != nil {
			t.Fatalf("attempt %d failed: %v", attempt, err)
		}
		if !strings.Contains(result.Output, "go version") {
			t.Fatalf("attempt %d lost process output: %q", attempt, result.Output)
		}
		if result.OutputTruncated {
			t.Fatalf("attempt %d unexpectedly truncated short output", attempt)
		}
	}
}

func TestGoBuildIsProjectGeneric(t *testing.T) {
	spec := allowed[GoBuild]
	if len(spec.Args) != 2 || spec.Args[0] != "build" || spec.Args[1] != "./..." {
		t.Fatalf("expected generic Go build command, got %#v", spec.Args)
	}
}

func TestGoCommandsDisablePersistentGoEnvironment(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example\n\ngo 1.22\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "fail_test.go"), []byte("package example\n\nimport \"testing\"\n\nfunc TestIntentionalFailure(t *testing.T) { t.Fatal(\"expected failure\") }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	goEnvFile := filepath.Join(t.TempDir(), "go-env")
	if err := os.WriteFile(goEnvFile, []byte("GOFLAGS=-run=DOESNOTEXIST\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOENV", goEnvFile)

	result, err := Run(context.Background(), root, GoTest)
	if err == nil || result.ExitCode == 0 || !strings.Contains(result.Output, "expected failure") {
		t.Fatalf("expected persistent Go settings to be ignored while the test still ran, result=%#v err=%v", result, err)
	}
	if result.EnvironmentProfile != "go-controlled" {
		t.Fatalf("expected controlled Go environment profile, got %q", result.EnvironmentProfile)
	}
}

func TestResolveExecutableUsesProjectDirectoryForRelativeProgram(t *testing.T) {
	root := t.TempDir()
	tool := filepath.Join(root, "tool")
	if err := os.WriteFile(tool, []byte("placeholder"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveExecutable(Spec{Program: "./tool"}, root)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != tool {
		t.Fatalf("expected project-local executable %q, got %q", tool, resolved)
	}
}

func TestResolveExecutableRejectsEscapingRelativeProgram(t *testing.T) {
	root := t.TempDir()
	if _, err := resolveExecutable(Spec{Program: "../outside"}, root); err == nil {
		t.Fatal("expected project-relative executable escaping the project to be rejected")
	}
}

func TestProcessExitFailureEmitsFailInsteadOfError(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example\n\ngo 1.22\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "fail_test.go"), []byte("package example\n\nimport \"testing\"\n\nfunc TestIntentionalFailure(t *testing.T) { t.Fatal(\"expected failure\") }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sink := &runnerEventSink{}
	ctx := events.WithSink(context.Background(), events.NewStream(sink))
	result, err := Run(ctx, root, GoTest)
	if err == nil || result.ExitCode == 0 {
		t.Fatalf("expected failing child process, result=%#v err=%v", result, err)
	}
	for _, event := range sink.events {
		if event.EventType == events.ProcessFinished {
			if event.Status != "FAIL" {
				t.Fatalf("expected process failure event, got %#v", event)
			}
			return
		}
	}
	t.Fatal("missing process_finished event")
}

type runnerEventSink struct {
	events []events.Event
}

func (sink *runnerEventSink) Publish(event events.Event) {
	sink.events = append(sink.events, event)
}

func TestRaceEnvironmentUsesControlledCompilerProfile(t *testing.T) {
	t.Setenv("PATH", filepath.Dir(os.Args[0])+string(os.PathListSeparator)+os.Getenv("PATH"))
	environment, profile, values := executionEnvironment(GoRaceEnvironment)
	if profile != "go-race-controlled" {
		t.Fatalf("expected race profile, got %q", profile)
	}
	keys := environmentKeys(environment)
	if !containsEnvironmentKey(keys, "GOENV") || !containsEnvironmentKey(keys, "CGO_ENABLED") {
		t.Fatalf("expected controlled Go race keys, got %#v", keys)
	}
	if values["GOENV"] != "off" || values["CGO_ENABLED"] != "1" {
		t.Fatalf("expected controlled race environment values, got %#v", values)
	}
}

func containsEnvironmentKey(keys []string, wanted string) bool {
	for _, key := range keys {
		if key == wanted {
			return true
		}
	}
	return false
}
