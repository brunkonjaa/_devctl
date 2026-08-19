package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"devctl/internal/worker"
)

func TestVerifyAgentUnavailableProjectReturnsOneBoundedResult(t *testing.T) {
	t.Setenv("DEVCTL_STATE_DIR", t.TempDir())
	missing := filepath.Join(t.TempDir(), "missing-project")

	stdout, stderr := captureStreams(t, func() int {
		return verifyCommand([]string{"--agent", missing})
	})

	if len(stderr) != 0 {
		t.Fatalf("agent mode wrote to stderr: %q", stderr)
	}
	if len(stdout)+1 > worker.MaxAgentResultBytes {
		t.Fatalf("agent result exceeded %d bytes: %d", worker.MaxAgentResultBytes, len(stdout)+1)
	}
	result := decodeOneAgentResult(t, stdout)
	if result.Accepted || result.ExitCode != exitInternal || result.Error == nil || result.Error.Code != "verification_unavailable" {
		t.Fatalf("unexpected unavailable-project result: %+v", result)
	}
}

func TestVerifyAgentRejectsLiveThroughTheStructuredChannel(t *testing.T) {
	stdout, stderr := captureStreams(t, func() int {
		return verifyCommand([]string{"--agent", "--live", "."})
	})

	if len(stderr) != 0 {
		t.Fatalf("agent argument failure wrote to stderr: %q", stderr)
	}
	result := decodeOneAgentResult(t, stdout)
	if result.Accepted || result.ExitCode != exitInternal || result.Error == nil || result.Error.Code != "invalid_arguments" {
		t.Fatalf("unexpected invalid-arguments result: %+v", result)
	}
}

func TestVerifyAgentArgumentErrorsStayOnTheStructuredChannel(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "missing project", args: []string{"--agent"}},
		{name: "json combination", args: []string{"--agent", "--json", "."}},
		{name: "unknown option", args: []string{"--agent", "--unknown", "."}},
		{name: "malformed agent value", args: []string{"--agent=not-a-boolean", "."}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stdout, stderr := captureStreams(t, func() int {
				return verifyCommand(test.args)
			})
			if len(stderr) != 0 {
				t.Fatalf("agent argument error wrote to stderr: %q", stderr)
			}
			result := decodeOneAgentResult(t, stdout)
			if result.Accepted || result.ExitCode != exitInternal || result.Error == nil || result.Error.Code != "invalid_arguments" {
				t.Fatalf("unexpected argument-error result: %+v", result)
			}
		})
	}
}

func TestVerifyAgentMatchesOrdinaryFullVerification(t *testing.T) {
	t.Setenv("DEVCTL_STATE_DIR", t.TempDir())
	t.Setenv("APPDATA", t.TempDir())
	projectPath := newAgentFixture(t, false)

	ordinary, ordinaryExit, err := executeVerificationWithOptions(projectPath, verificationExecutionOptions{Diagnostics: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	stdout, stderr := captureStreams(t, func() int {
		return verifyCommand([]string{"--agent", projectPath})
	})
	if len(stderr) != 0 {
		t.Fatalf("agent mode wrote to stderr: %q", stderr)
	}
	result := decodeOneAgentResult(t, stdout)
	if !result.Accepted || result.ExitCode != ordinaryExit || result.Overall != ordinary.Overall {
		t.Fatalf("agent result diverged: agent=%s/%d ordinary=%s/%d", result.Overall, result.ExitCode, ordinary.Overall, ordinaryExit)
	}
	if summaryVector(result.Checks) != checkVector(ordinary.Checks) {
		t.Fatalf("agent check vector diverged:\nagent=%s\nordinary=%s", summaryVector(result.Checks), checkVector(ordinary.Checks))
	}
	if result.VerificationClass != "local-full" || result.RepositoryFingerprint == "" || result.PolicyVersion == "" {
		t.Fatalf("agent provenance is incomplete: %+v", result)
	}
	if result.InformationFlow.RawSubprocessBytes == 0 || result.InformationFlow.LocalEvidenceBytes == 0 || !result.InformationFlow.LocalEvidenceMeasured {
		t.Fatalf("agent information-flow metrics are incomplete: %+v", result.InformationFlow)
	}
	if result.InformationFlow.AgentResponseBytes != int64(len(stdout)+1) {
		t.Fatalf("agent response size mismatch: metric=%d actual=%d", result.InformationFlow.AgentResponseBytes, len(stdout)+1)
	}
	t.Logf("PASS acceptance: result=%s exit=%d raw=%d retained=%d evidence=%d response=%d", result.Overall, result.ExitCode, result.InformationFlow.RawSubprocessBytes, result.InformationFlow.RetainedSubprocessBytes, result.InformationFlow.LocalEvidenceBytes, result.InformationFlow.AgentResponseBytes)
}

func TestVerifyAgentContainsVerboseFailureInEvidenceOnly(t *testing.T) {
	t.Setenv("DEVCTL_STATE_DIR", t.TempDir())
	t.Setenv("APPDATA", t.TempDir())
	projectPath := newAgentFixture(t, true)

	stdout, stderr := captureStreams(t, func() int {
		return verifyCommand([]string{"--agent", projectPath})
	})
	if len(stderr) != 0 {
		t.Fatalf("agent mode wrote to stderr: %q", stderr)
	}
	if bytes.Contains(stdout, []byte("VERBOSE_CHILD_OUTPUT_SENTINEL")) || bytes.Contains(stderr, []byte("VERBOSE_CHILD_OUTPUT_SENTINEL")) {
		t.Fatal("verbose child output crossed the agent boundary")
	}
	result := decodeOneAgentResult(t, stdout)
	if !result.Accepted || result.ExitCode != 1 || result.Overall != "FAIL" {
		t.Fatalf("unexpected controlled failure result: %+v", result)
	}
	if len(stdout)+1 > worker.MaxAgentResultBytes {
		t.Fatalf("agent result exceeded hard limit: %d", len(stdout)+1)
	}
	flow := result.InformationFlow
	if flow.RawSubprocessBytes <= flow.RetainedSubprocessBytes || !flow.OutputTruncated || !flow.LocalEvidenceMeasured {
		t.Fatalf("verbose output truncation was not measured truthfully: %+v", flow)
	}
	rawPath := filepath.Join(projectPath, filepath.FromSlash(result.EvidencePath), "raw", "go-test.log")
	raw, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("VERBOSE_CHILD_OUTPUT_SENTINEL")) {
		t.Fatalf("controlled child output was not retained in local evidence: %s", rawPath)
	}
	if int64(len(raw)) >= flow.RawSubprocessBytes || flow.LocalEvidenceBytes < int64(len(raw)) {
		t.Fatalf("evidence metrics do not match retained output: flow=%+v raw=%d", flow, len(raw))
	}
	t.Logf("FAIL acceptance: result=%s exit=%d raw=%d retained=%d evidence=%d response=%d raw_log=%d", result.Overall, result.ExitCode, flow.RawSubprocessBytes, flow.RetainedSubprocessBytes, flow.LocalEvidenceBytes, flow.AgentResponseBytes, len(raw))
}

func newAgentFixture(t *testing.T, verboseFailure bool) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "agent-fixture")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		".gitignore":  ".devctl/\n",
		"go.mod":      "module example.com/agentfixture\n\ngo 1.22\n",
		"main.go":     "package agentfixture\n\nfunc Value() int { return 1 }\n",
		"devctl.json": "{\n  \"version\": \"1\",\n  \"checks\": {\n    \"go-test-race\": {\"enabled\": false}\n  }\n}\n",
	}
	if verboseFailure {
		files["main_test.go"] = "package agentfixture\n\nimport (\n  \"fmt\"\n  \"strings\"\n  \"testing\"\n)\n\nfunc TestControlledVerboseFailure(t *testing.T) {\n  chunk := strings.Repeat(\"VERBOSE_CHILD_OUTPUT_SENTINEL-\", 2048)\n  for index := 0; index < 48; index++ { fmt.Print(chunk) }\n  t.Fatal(\"controlled failure\")\n}\n"
	} else {
		files["main_test.go"] = "package agentfixture\n\nimport \"testing\"\n\nfunc TestValue(t *testing.T) { if Value() != 1 { t.Fatal(\"unexpected value\") } }\n"
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runFixtureGit(t, root, "init")
	runFixtureGit(t, root, "config", "user.email", "devctl@example.invalid")
	runFixtureGit(t, root, "config", "user.name", "devctl test")
	runFixtureGit(t, root, "add", ".")
	runFixtureGit(t, root, "commit", "-m", "fixture baseline")
	return root
}

func runFixtureGit(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func decodeOneAgentResult(t *testing.T, data []byte) worker.Result {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	var result worker.Result
	if err := decoder.Decode(&result); err != nil {
		t.Fatalf("decode agent result: %v\n%s", err, data)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("agent stdout did not contain exactly one JSON object: %v\n%s", err, data)
	}
	return result
}
