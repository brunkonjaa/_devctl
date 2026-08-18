package repaircli

import (
	"bytes"
	"context"
	"devctl/internal/model"
	"devctl/internal/repair"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBaselinePassDoesNotNeedProposalOrApproval(t *testing.T) {
	var diagnostics bytes.Buffer
	result, err := Run(context.Background(), Options{ProjectPath: t.TempDir(), AllowedPaths: []string{"source.go"}, Diagnostics: &diagnostics, Verify: func(context.Context, string) model.Report { return model.Report{RunID: "run", Overall: model.Pass} }})
	if err != nil || result.ExitCode != ExitOK || result.Result.FinalStatus != model.Pass {
		t.Fatalf("unexpected baseline result: %+v %v", result, err)
	}
}

func TestMissingProviderIsOneJSONResult(t *testing.T) {
	var output bytes.Buffer
	result, err := Run(context.Background(), Options{ProjectPath: t.TempDir(), AllowedPaths: []string{"source.go"}, JSON: true, Output: &output, Diagnostics: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	var decoded Output
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result.Kind != "DETERMINISTIC_RESULT" || decoded.ExitCode != ExitVerificationFailure || decoded.Status != model.InsufficientEvidence {
		t.Fatalf("unexpected result: %+v", decoded)
	}
}

func TestBaselineExitPolicyPreservesWarningAndErrorCodes(t *testing.T) {
	tests := []struct {
		name   string
		report model.Report
		want   int
	}{
		{name: "non-blocking warn", report: model.Report{RunID: "warn", Overall: model.Warn, Checks: []model.CheckResult{{ID: "check", Status: model.Warn}}}, want: ExitOK},
		{name: "blocking warn", report: model.Report{RunID: "blocking-warn", Overall: model.Warn, Checks: []model.CheckResult{{ID: "check", Status: model.Warn, Blocking: true}}}, want: ExitVerificationFailure},
		{name: "error", report: model.Report{RunID: "error", Overall: model.Error}, want: ExitFramework},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := Run(context.Background(), Options{ProjectPath: t.TempDir(), AllowedPaths: []string{"source.go"}, Diagnostics: &bytes.Buffer{}, Verify: func(context.Context, string) model.Report { return test.report }})
			if err != nil || result.ExitCode != test.want || result.Result.FinalExitCode != test.want {
				t.Fatalf("unexpected exit policy: %+v %v", result, err)
			}
		})
	}
}

func TestApprovalRetriesInvalidInputAndUsesEngineEvidence(t *testing.T) {
	var diagnostics bytes.Buffer
	request := repair.ApprovalRequest{TaskID: "task", ProjectID: "project", BaselineRunID: "run", WorkerID: "worker", Protocol: "1", DiffHash: "hash", DisplayDiff: "- old\n+ new", Evidence: repair.ApprovalEvidenceView{CanonicalProject: repair.CanonicalProjectMetadata{Root: "C:\\project", ProjectID: "project"}, PatchArtifact: ".devctl/evidence/repair/x.patch"}}
	decision, err := approve(context.Background(), request, Options{Interactive: true, Input: strings.NewReader("x\na\n"), Diagnostics: &diagnostics})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Outcome != repair.ApprovalApproved || decision.DiffHash != "hash" {
		t.Fatalf("unexpected decision: %+v", decision)
	}
	if !strings.Contains(diagnostics.String(), "Please choose") || !strings.Contains(diagnostics.String(), request.DisplayDiff) {
		t.Fatalf("approval presentation missing: %s", diagnostics.String())
	}
}

func TestControlledProposalRunsThroughRepairEngineAndKeepsJSONClean(t *testing.T) {
	root := syntheticRepository(t)
	proposalPath := filepath.Join(t.TempDir(), "proposal.json")
	proposal := repair.Proposal{SchemaVersion: repair.ProtocolVersion, TaskID: "repair-cli-001", Worker: "controlled-cli", Changes: []repair.FileChange{{Path: "calculator.go", Content: []byte("package calculator\n\nfunc Add(left, right int) int { return left + right }\n")}}}
	data, err := json.Marshal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(proposalPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	var stdout, diagnostics bytes.Buffer
	output, err := Run(context.Background(), Options{ProjectPath: root, ProjectID: "synthetic-cli", AllowedPaths: []string{"calculator.go"}, ProposalPath: proposalPath, Input: strings.NewReader("a\n"), Output: &stdout, Diagnostics: &diagnostics, Interactive: true, JSON: true, Verify: func(context.Context, string) model.Report {
		calls++
		status := model.Fail
		if calls > 1 {
			status = model.Pass
		}
		return model.Report{RunID: fmt.Sprintf("run-%d", calls), Overall: status, Project: &model.Project{Identity: "synthetic-cli", Path: root}}
	}})
	if err != nil {
		t.Fatal(err)
	}
	if output.ExitCode != ExitOK || output.Result.Approval.Outcome != repair.ApprovalApproved || calls != 2 {
		t.Fatalf("unexpected repair output: %+v calls=%d", output, calls)
	}
	var structured Output
	if err := json.Unmarshal(stdout.Bytes(), &structured); err != nil {
		t.Fatalf("stdout was not one JSON result: %v", err)
	}
	if !strings.Contains(diagnostics.String(), "Patch SHA-256") || !strings.Contains(diagnostics.String(), "Testing the project again") {
		t.Fatalf("missing terminal workflow output: %s", diagnostics.String())
	}
	content, err := os.ReadFile(filepath.Join(root, "calculator.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "left + right") {
		t.Fatalf("approved content was not applied: %s", content)
	}
}

func TestInjectedProviderReceivesContextAndPreservesFinalErrorExit(t *testing.T) {
	root := syntheticRepository(t)
	proposal := repair.Proposal{SchemaVersion: repair.ProtocolVersion, TaskID: "repair-cli-001", Worker: "controlled-cli", Changes: []repair.FileChange{{Path: "calculator.go", Content: []byte("package calculator\nfunc Add(left, right int) int { return left + right }\n")}}}
	calls := 0
	providerCalled := false
	result, err := Run(context.Background(), Options{ProjectPath: root, ProjectID: "synthetic-cli", AllowedPaths: []string{"calculator.go"}, Input: strings.NewReader("a\n"), Output: &bytes.Buffer{}, Diagnostics: &bytes.Buffer{}, Interactive: true, Propose: func(providerContext context.Context, task repair.Task) (repair.Proposal, error) {
		providerCalled = providerContext != nil && task.TaskID != ""
		return proposal, nil
	}, Verify: func(context.Context, string) model.Report {
		calls++
		status := model.Fail
		if calls > 1 {
			status = model.Error
		}
		return model.Report{RunID: fmt.Sprintf("run-%d", calls), Overall: status, Project: &model.Project{Identity: "synthetic-cli", Path: root}}
	}})
	if err != nil || !providerCalled || result.ExitCode != ExitFramework || result.Result.FinalExitCode != ExitFramework {
		t.Fatalf("provider/final error semantics failed: %+v %v", result, err)
	}
}

func TestInjectedProviderCancellationIsCancelled(t *testing.T) {
	root := syntheticRepository(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	called := make(chan struct{})
	done := make(chan Output, 1)
	go func() {
		result, _ := Run(ctx, Options{ProjectPath: root, ProjectID: "synthetic-cli", AllowedPaths: []string{"calculator.go"}, Diagnostics: &bytes.Buffer{}, Propose: func(providerContext context.Context, _ repair.Task) (repair.Proposal, error) {
			close(called)
			<-providerContext.Done()
			return repair.Proposal{}, providerContext.Err()
		}, Verify: func(context.Context, string) model.Report {
			return model.Report{RunID: "run", Overall: model.Fail, Project: &model.Project{Identity: "synthetic-cli", Path: root}}
		}})
		done <- result
	}()
	<-called
	cancel()
	result := <-done
	if result.Kind != "CANCELLED" || result.ExitCode != ExitCancelled {
		t.Fatalf("provider cancellation was not preserved: %+v", result)
	}
}

func TestInteractiveRejectionCancellationAndEOFRemainDistinct(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		interactive bool
		wantCode    int
		wantOutcome repair.ApprovalOutcome
	}{
		{name: "rejected", input: "r\n", interactive: true, wantCode: ExitRejected, wantOutcome: repair.ApprovalRejected},
		{name: "cancelled", input: "c\n", interactive: true, wantCode: ExitCancelled, wantOutcome: repair.ApprovalCancelled},
		{name: "eof", input: "", interactive: true, wantCode: ExitCancelled, wantOutcome: repair.ApprovalCancelled},
		{name: "non interactive", input: "a\n", interactive: false, wantCode: ExitFramework, wantOutcome: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := syntheticRepository(t)
			proposalPath := controlledProposalFile(t)
			var output bytes.Buffer
			result, err := Run(context.Background(), Options{ProjectPath: root, ProjectID: "synthetic-cli", AllowedPaths: []string{"calculator.go"}, ProposalPath: proposalPath, Input: strings.NewReader(test.input), Output: &output, Diagnostics: &bytes.Buffer{}, Interactive: test.interactive, JSON: true, Verify: func(context.Context, string) model.Report {
				return model.Report{RunID: "run", Overall: model.Fail, Project: &model.Project{Identity: "synthetic-cli", Path: root}}
			}})
			if err != nil || result.ExitCode != test.wantCode || result.Result.Approval.Outcome != test.wantOutcome {
				t.Fatalf("unexpected interactive outcome: %+v %v", result, err)
			}
			content, readErr := os.ReadFile(filepath.Join(root, "calculator.go"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			if strings.Contains(string(content), "left + right") {
				t.Fatal("non-approval outcome changed project content")
			}
		})
	}
}

func syntheticRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{"go.mod": "module example.com/synthetic-cli\n\ngo 1.22\n", "calculator.go": "package calculator\n\nfunc Add(left, right int) int { return left - right }\n", "devctl.json": `{"version":"1","project_id":"synthetic-cli","checks":{"go-test-race":{"enabled":false}}}`, ".gitignore": ".devctl/\n"}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init"}, {"config", "user.email", "repair-cli@example.invalid"}, {"config", "user.name", "repair-cli"}, {"add", "."}, {"commit", "-m", "baseline"}} {
		command := exec.Command("git", append([]string{"-C", root}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, output)
		}
	}
	return root
}

func controlledProposalFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "proposal.json")
	proposal := repair.Proposal{SchemaVersion: repair.ProtocolVersion, TaskID: "repair-cli-001", Worker: "controlled-cli", Changes: []repair.FileChange{{Path: "calculator.go", Content: []byte("package calculator\nfunc Add(left, right int) int { return left + right }\n")}}}
	data, err := json.Marshal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
