package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"devctl/internal/model"
	"devctl/internal/registry"
	"devctl/internal/worker"
)

func TestWorkerVerifyUsesTheOrdinaryVerificationPath(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("DEVCTL_STATE_DIR", stateDir)
	t.Setenv("APPDATA", t.TempDir())
	projectPath := filepath.Join(t.TempDir(), "worker-project")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectPath, "go.mod"), []byte("module example.com/worker-project\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ordinaryReport, ordinaryExit, ordinaryErr := executeVerification(projectPath, false)
	if ordinaryErr != nil {
		t.Fatal(ordinaryErr)
	}
	entry, err := registry.DetectProject(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	requestPath := filepath.Join(t.TempDir(), "request.json")
	requestData, err := json.Marshal(worker.Request{SchemaVersion: worker.ProtocolVersion, RequestID: "req-command-1", Operation: "verify", ProjectID: entry.ProjectID})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(requestPath, requestData, 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr := captureStreams(t, func() int {
		return workerCommand([]string{"verify", "--live", "--request", requestPath})
	})
	if !bytes.Contains(stderr, []byte("DEVCTL VERIFY")) {
		t.Fatalf("live renderer did not write to stderr: %s", stderr)
	}
	if bytes.Contains(stdout, []byte("DEVCTL VERIFY")) {
		t.Fatalf("live renderer contaminated structured stdout: %s", stdout)
	}
	var result worker.Result
	if err := json.Unmarshal(stdout, &result); err != nil {
		t.Fatalf("worker stdout was not one structured JSON result: %v\n%s", err, stdout)
	}
	if !result.Accepted || result.RequestID != "req-command-1" {
		t.Fatalf("unexpected worker result: %+v", result)
	}
	if result.ExitCode != ordinaryExit || result.Overall != ordinaryReport.Overall {
		t.Fatalf("worker result diverged from ordinary verification: worker=%s/%d ordinary=%s/%d", result.Overall, result.ExitCode, ordinaryReport.Overall, ordinaryExit)
	}
	if summaryVector(result.Checks) != checkVector(ordinaryReport.Checks) {
		t.Fatalf("worker check vector diverged:\nworker=%s\nordinary=%s", summaryVector(result.Checks), checkVector(ordinaryReport.Checks))
	}
}

func TestWorkerVerifyRejectsChangedProjectIdentity(t *testing.T) {
	t.Setenv("DEVCTL_STATE_DIR", t.TempDir())
	projectPath := filepath.Join(t.TempDir(), "identity-project")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectPath, "go.mod"), []byte("module example.com/identity-project\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectPath, "devctl.json"), []byte("{\n  \"version\": \"1\",\n  \"project_id\": \"project-alpha\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry, err := registry.DetectProject(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(entry); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectPath, "devctl.json"), []byte("{\n  \"version\": \"1\",\n  \"project_id\": \"project-beta\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	requestPath := filepath.Join(t.TempDir(), "request.json")
	requestData, err := json.Marshal(worker.Request{SchemaVersion: worker.ProtocolVersion, RequestID: "req-identity", Operation: "verify", ProjectID: "project-alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(requestPath, requestData, 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, _ := captureStreams(t, func() int {
		return workerCommand([]string{"verify", "--request", requestPath})
	})
	var result worker.Result
	if err := json.Unmarshal(stdout, &result); err != nil {
		t.Fatalf("worker stdout was not structured JSON: %v\n%s", err, stdout)
	}
	if result.Accepted || result.Error == nil || result.Error.Code != "project_identity_mismatch" {
		t.Fatalf("changed project identity was not rejected: %+v", result)
	}
}

func captureStreams(t *testing.T, run func() int) ([]byte, []byte) {
	t.Helper()
	oldStdout, oldStderr := os.Stdout, os.Stderr
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = stdoutWriter, stderrWriter
	type readResult struct {
		data []byte
		err  error
	}
	stdoutChannel := make(chan readResult, 1)
	stderrChannel := make(chan readResult, 1)
	go func() {
		data, readErr := io.ReadAll(stdoutReader)
		stdoutChannel <- readResult{data: data, err: readErr}
	}()
	go func() {
		data, readErr := io.ReadAll(stderrReader)
		stderrChannel <- readResult{data: data, err: readErr}
	}()
	exitCode := run()
	if err := stdoutWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stderrWriter.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = oldStdout, oldStderr
	stdout := <-stdoutChannel
	stderr := <-stderrChannel
	if stdout.err != nil {
		t.Fatal(stdout.err)
	}
	if stderr.err != nil {
		t.Fatal(stderr.err)
	}
	_ = stdoutReader.Close()
	_ = stderrReader.Close()
	if exitCode != 0 && len(stdout.data) == 0 {
		t.Fatalf("worker command returned %d without structured output", exitCode)
	}
	return bytes.TrimSpace(stdout.data), stderr.data
}

func checkVector(checks []model.CheckResult) string {
	var buffer bytes.Buffer
	for _, check := range checks {
		buffer.WriteString(check.ID)
		buffer.WriteByte(':')
		buffer.WriteString(string(check.Status))
		buffer.WriteByte(';')
	}
	return buffer.String()
}

func summaryVector(checks []worker.CheckSummary) string {
	var buffer bytes.Buffer
	for _, check := range checks {
		buffer.WriteString(check.ID)
		buffer.WriteByte(':')
		buffer.WriteString(string(check.Status))
		buffer.WriteByte(';')
	}
	return buffer.String()
}
