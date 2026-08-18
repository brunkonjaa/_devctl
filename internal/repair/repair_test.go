package repair

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"devctl/internal/model"
	"devctl/internal/verify"
)

func TestSyntheticGoRepairLifecycle(t *testing.T) {
	root := makeSyntheticProject(t)
	originalTest := readProjectFile(t, root, "calculator_test.go")
	var verificationCalls int
	var approvedPatch []byte
	var approvedDisplay string
	options := Options{
		ProjectPath:  root,
		ProjectID:    "synthetic-repair",
		TaskID:       "repair-synthetic-001",
		Worker:       "codex-test",
		Attempt:      1,
		AllowedPaths: []string{"calculator.go"},
		Verify: func(ctx context.Context, path string) model.Report {
			verificationCalls++
			return verify.ProjectWithOptions(ctx, path, verify.Options{RunID: fmt.Sprintf("synthetic-run-%d", verificationCalls)})
		},
		Propose: func(task Task) (Proposal, error) {
			if task.Failure.Overall != model.Fail || task.ProjectID != "synthetic-repair" {
				t.Fatalf("unexpected repair task: %+v", task)
			}
			return Proposal{SchemaVersion: ProtocolVersion, TaskID: task.TaskID, Worker: "codex-test", Changes: []FileChange{{Path: "calculator.go", Content: []byte("package calculator\n\nfunc Add(left, right int) int {\n\treturn left + right\n}\n")}}}, nil
		},
		Approve: func(request ApprovalRequest) (ApprovalDecision, error) {
			if request.DiffHash == "" || request.ProjectID != "synthetic-repair" || request.WorkerID != "codex-test" || request.Protocol != ProtocolVersion || !strings.Contains(request.DisplayDiff, "-\treturn left - right") || !strings.Contains(request.DisplayDiff, "+\treturn left + right") || len(request.CanonicalPatch) == 0 {
				t.Fatalf("approval did not receive exact bounded patch: %+v", request)
			}
			canonical, err := decodePatchArtifact(request.CanonicalPatch)
			if err != nil {
				t.Fatalf("approval received an invalid canonical patch: %v", err)
			}
			display, err := displayDiff(canonical)
			if err != nil || display != request.DisplayDiff {
				t.Fatalf("display was not derived from canonical patch: err=%v display=%q request=%q", err, display, request.DisplayDiff)
			}
			if got := sha256Hex(request.CanonicalPatch); got != request.DiffHash {
				t.Fatalf("approval hash did not cover exact canonical bytes: got=%s want=%s", request.DiffHash, got)
			}
			approvedPatch = append([]byte(nil), request.CanonicalPatch...)
			approvedDisplay = request.DisplayDiff
			return ApprovalDecision{Approved: true, DiffHash: request.DiffHash}, nil
		},
	}
	result, err := Run(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if verificationCalls != 2 {
		t.Fatalf("expected one baseline and one re-verification, got %d", verificationCalls)
	}
	if result.InitialStatus != model.Fail || (result.FinalStatus != model.Pass && result.FinalStatus != model.Warn) {
		t.Fatalf("unexpected lifecycle statuses: %+v", result)
	}
	if !result.Approved || result.DiffHash == "" || len(result.Files) != 1 {
		t.Fatalf("missing approval or provenance: %+v", result)
	}
	if result.ActualDiffHash != result.DiffHash {
		t.Fatalf("actual post-change hash did not match approval: %+v", result)
	}
	if got := string(readProjectFile(t, root, filepath.ToSlash(result.PatchArtifact))); got != string(approvedPatch) {
		t.Fatal("persisted canonical patch differed from the approved bytes")
	}
	if result.DisplayDiff != approvedDisplay {
		t.Fatal("persisted displayed diff differed from the approved display")
	}
	if result.EvidencePath == "" {
		t.Fatal("repair evidence path was not recorded")
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(result.EvidencePath))); err != nil {
		t.Fatalf("repair evidence was not persisted: %v", err)
	}
	evidence := string(readProjectFile(t, root, filepath.ToSlash(result.EvidencePath)))
	if !strings.Contains(evidence, result.EvidencePath) {
		t.Fatalf("persisted repair evidence omitted its path: %s", evidence)
	}
	if got := string(readProjectFile(t, root, "calculator.go")); !strings.Contains(got, "left + right") {
		t.Fatalf("approved production change was not applied: %s", got)
	}
	if got := readProjectFile(t, root, "calculator_test.go"); string(got) != string(originalTest) {
		t.Fatalf("test fixture was unexpectedly changed: %s", got)
	}
	wantEvents := []string{"REPAIR_TASK_CREATED", "BASELINE_CAPTURED", "WORKER_PROPOSAL_RECEIVED", "PROPOSAL_VALIDATED", "PATCH_ARTIFACT_STORED", "DIFF_DISPLAYED", "APPROVED", "PRE_APPLY_STATE_VALIDATED", "PATCH_PREFLIGHT", "PATCH_APPLIED", "POST_STATE_CAPTURED", "DELTA_VALIDATED", "VERIFY_STARTED", "VERIFY_FINISHED", "REPAIR_STOPPED"}
	if got := eventTypes(result.Events); strings.Join(got, ",") != strings.Join(wantEvents, ",") {
		t.Fatalf("unexpected repair event sequence: %v", got)
	}
}

func TestRepairRejectsForbiddenPolicyPathBeforeApprovalWriteOrReverification(t *testing.T) {
	root := makeSyntheticProject(t)
	policyPath := filepath.Join(root, "config", "defaults.json")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte("{\"version\":\"synthetic-policy\"}\n")
	if err := os.WriteFile(policyPath, original, 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "config/defaults.json")
	git(t, root, "commit", "-m", "add policy fixture")

	verificationCalls := 0
	approvalCalls := 0
	writeCalls := 0
	options := fakeOptions(root, []string{"calculator.go"}, passAfterRepair())
	options.Verify = func(context.Context, string) model.Report {
		verificationCalls++
		return model.Report{RunID: fmt.Sprintf("run-%d", verificationCalls), Overall: model.Fail, Project: &model.Project{Identity: "synthetic-repair", Path: root}, Checks: []model.CheckResult{{ID: "synthetic-check", Status: model.Fail, Blocking: true, Summary: "FAIL"}}}
	}
	options.Propose = func(task Task) (Proposal, error) {
		return Proposal{SchemaVersion: ProtocolVersion, TaskID: task.TaskID, Worker: "test", Changes: []FileChange{{Path: "config/defaults.json", Content: []byte("{\"version\":\"tampered\"}\n")}}}, nil
	}
	options.Approve = func(ApprovalRequest) (ApprovalDecision, error) {
		approvalCalls++
		return ApprovalDecision{Approved: true}, nil
	}
	options.WriteFile = func(string, []byte, fs.FileMode) error {
		writeCalls++
		return nil
	}
	_, err := Run(context.Background(), options)
	if !errors.Is(err, ErrForbiddenPath) {
		t.Fatalf("expected forbidden policy path rejection, got %v", err)
	}
	if verificationCalls != 1 || approvalCalls != 0 || writeCalls != 0 {
		t.Fatalf("unexpected lifecycle counts: verification=%d approval=%d writes=%d", verificationCalls, approvalCalls, writeCalls)
	}
	if got := readProjectFile(t, root, "config/defaults.json"); string(got) != string(original) {
		t.Fatal("forbidden policy file changed")
	}
}

func TestRepairRejectsForbiddenPathInAllowlistBeforeWorkerInvocation(t *testing.T) {
	root := makeSyntheticProject(t)
	workerCalled := false
	options := fakeOptions(root, []string{"calculator.go", "config/defaults.json"}, passAfterRepair())
	options.Propose = func(task Task) (Proposal, error) {
		workerCalled = true
		return Proposal{}, nil
	}
	_, err := Run(context.Background(), options)
	if !errors.Is(err, ErrForbiddenPath) {
		t.Fatalf("expected malformed allowlist rejection, got %v", err)
	}
	if workerCalled {
		t.Fatal("worker was invoked with a forbidden allowlist entry")
	}
}

func TestDisplayDiffShowsTerminalNewlineState(t *testing.T) {
	withNewline, err := displayDiff([]canonicalFileChange{{Path: "calculator.go", Preimage: []byte("package x\n"), Postimage: []byte("package y\n")}})
	if err != nil {
		t.Fatal(err)
	}
	withoutNewline, err := displayDiff([]canonicalFileChange{{Path: "calculator.go", Preimage: []byte("package x"), Postimage: []byte("package y")}})
	if err != nil {
		t.Fatal(err)
	}
	if withNewline == withoutNewline {
		t.Fatal("terminal newline state was not represented in the displayed diff")
	}
	if !strings.Contains(withoutNewline, `\ No newline at end of file`) || strings.Contains(withNewline, `\ No newline at end of file`) {
		t.Fatalf("unexpected terminal newline markers: with=%q without=%q", withNewline, withoutNewline)
	}
}

func TestRepairTaskCarriesBaselineMetadataAndRejectsWorkerMetadataMutation(t *testing.T) {
	root := makeSyntheticProject(t)
	var observed Task
	options := fakeOptions(root, []string{"calculator.go"}, passAfterRepair())
	options.Propose = func(task Task) (Proposal, error) {
		observed = cloneTask(task)
		task.CanonicalProject.Root = "tampered-root"
		task.ForbiddenPathPolicyVersion = "tampered-policy-version"
		task.ForbiddenPathClasses[0] = "tampered-class"
		task.DevctlProvenance.Commit = "tampered-commit"
		task.PolicyProvenanceHash = "tampered-policy-hash"
		return Proposal{SchemaVersion: ProtocolVersion, TaskID: task.TaskID, Worker: "test", Changes: []FileChange{{Path: "calculator.go", Content: []byte("package calculator\nfunc Add(left, right int) int { return left + right }\n")}}}, nil
	}
	result, err := Run(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if observed.CanonicalProject.Root != result.Baseline.CanonicalProject.Root || observed.CanonicalProject.ProjectID != result.Baseline.CanonicalProject.ProjectID {
		t.Fatalf("worker did not receive baseline canonical project metadata: observed=%+v baseline=%+v", observed.CanonicalProject, result.Baseline.CanonicalProject)
	}
	if observed.ForbiddenPathPolicyVersion != result.Baseline.ForbiddenPathPolicyVersion || strings.Join(observed.ForbiddenPathClasses, "|") != strings.Join(result.Baseline.ForbiddenPathClasses, "|") {
		t.Fatalf("worker did not receive baseline forbidden policy: observed=%+v baseline=%+v", observed, result.Baseline)
	}
	if observed.DevctlProvenance != result.Baseline.DevctlProvenance || observed.PolicyProvenanceHash != result.Baseline.PolicyProvenanceHash {
		t.Fatalf("worker did not receive baseline provenance: observed=%+v baseline=%+v", observed, result.Baseline)
	}
	if result.Task.CanonicalProject.Root == "tampered-root" || result.Task.ForbiddenPathPolicyVersion == "tampered-policy-version" || result.Task.ForbiddenPathClasses[0] == "tampered-class" || result.Task.DevctlProvenance.Commit == "tampered-commit" || result.Task.PolicyProvenanceHash == "tampered-policy-hash" {
		t.Fatalf("worker mutated persisted task metadata: %+v", result.Task)
	}
	persisted := readPersistedResult(t, root, result)
	if persisted.Task == nil || persisted.Task.CanonicalProject != result.Task.CanonicalProject || persisted.Task.DevctlProvenance != result.Task.DevctlProvenance || persisted.Task.PolicyProvenanceHash != result.Task.PolicyProvenanceHash || strings.Join(persisted.Task.ForbiddenPathClasses, "|") != strings.Join(result.Task.ForbiddenPathClasses, "|") {
		t.Fatalf("persisted task metadata changed: %+v", persisted.Task)
	}
}

func TestRepairPersistsCompleteBaselineAndApprovalEvidence(t *testing.T) {
	root := makeSyntheticProject(t)
	result, err := Run(context.Background(), fakeOptions(root, []string{"calculator.go"}, passAfterRepair()))
	if err != nil {
		t.Fatal(err)
	}
	persisted := readPersistedResult(t, root, result)
	if persisted.Baseline.Head == "" || persisted.Baseline.IndexHash == "" || persisted.Baseline.ProjectID != "synthetic-repair" || persisted.Baseline.ProvenanceHash == "" || len(persisted.Baseline.Files) == 0 {
		t.Fatalf("baseline evidence is incomplete: %+v", persisted.Baseline)
	}
	if persisted.Task == nil || persisted.Task.WorkerID != "test" || persisted.Task.Failure.Overall != model.Fail {
		t.Fatalf("task envelope was not persisted: %+v", persisted.Task)
	}
	if persisted.Proposal == nil || len(persisted.Proposal.Changes) != 1 || persisted.Proposal.Worker != "test" {
		t.Fatalf("proposal envelope was not persisted: %+v", persisted.Proposal)
	}
	if persisted.Approval.Outcome != ApprovalApproved || persisted.Approval.WorkerID != "test" || persisted.Approval.DiffHash != result.DiffHash || persisted.Approval.DisplayHash != result.DisplayHash {
		t.Fatalf("approval evidence was not persisted: %+v", persisted.Approval)
	}
}

func TestRepairRejectsUnsafeTaskIDs(t *testing.T) {
	for _, taskID := range []string{"", "../../escape", `..\..\escape`, `C:\temp\x`, "/x", "a/b", `a\b`, "..", ".", "a..b", strings.Repeat("a", maxTaskIDLength+1)} {
		if err := validateTaskID(taskID); !errors.Is(err, ErrInvalidTaskID) {
			t.Fatalf("task ID %q was accepted: %v", taskID, err)
		}
	}
	if err := validateTaskID("valid-task_123.abc"); err != nil {
		t.Fatalf("valid task ID was rejected: %v", err)
	}
	root := makeSyntheticProject(t)
	if _, _, err := safeRepairPath(root, "../../escape", ".patch"); !errors.Is(err, ErrInvalidTaskID) {
		t.Fatalf("path containment did not reject traversal: %v", err)
	}
}

func TestRepairRejectsWorkerIdentityMismatch(t *testing.T) {
	root := makeSyntheticProject(t)
	options := fakeOptions(root, []string{"calculator.go"}, passAfterRepair())
	options.Propose = func(task Task) (Proposal, error) {
		return Proposal{SchemaVersion: ProtocolVersion, TaskID: task.TaskID, Worker: "untrusted-worker", Changes: []FileChange{{Path: "calculator.go", Content: []byte("package calculator\n")}}}, nil
	}
	_, err := Run(context.Background(), options)
	if !errors.Is(err, ErrWorkerIdentity) {
		t.Fatalf("expected worker identity rejection, got %v", err)
	}
}

func TestRepairRejectsOversizedMalformedAndDeletionProposals(t *testing.T) {
	for _, test := range []struct {
		name     string
		proposal func(Task) Proposal
		wantText string
	}{
		{name: "oversized", proposal: func(task Task) Proposal {
			return Proposal{SchemaVersion: ProtocolVersion, TaskID: task.TaskID, Worker: "test", Changes: []FileChange{{Path: "calculator.go", Content: []byte(strings.Repeat("x", maxPatchBytes+1))}}}
		}, wantText: "empty or oversized"},
		{name: "malformed envelope", proposal: func(task Task) Proposal {
			return Proposal{SchemaVersion: "wrong", TaskID: task.TaskID, Worker: "test", Changes: []FileChange{{Path: "calculator.go", Content: []byte("package calculator\n")}}}
		}, wantText: "invalid repair proposal envelope"},
		{name: "deletion", proposal: func(task Task) Proposal {
			return Proposal{SchemaVersion: ProtocolVersion, TaskID: task.TaskID, Worker: "test", Changes: []FileChange{{Path: "calculator.go", Content: nil}}}
		}, wantText: "empty or oversized"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := makeSyntheticProject(t)
			options := fakeOptions(root, []string{"calculator.go"}, passAfterRepair())
			options.Propose = func(task Task) (Proposal, error) { return test.proposal(task), nil }
			_, err := Run(context.Background(), options)
			if err == nil || !strings.Contains(err.Error(), test.wantText) {
				t.Fatalf("expected proposal rejection containing %q, got %v", test.wantText, err)
			}
		})
	}
}

func TestRepairRejectsHeadMutationBeforeApply(t *testing.T) {
	root := makeSyntheticProject(t)
	options := fakeOptions(root, []string{"calculator.go"}, passAfterRepair())
	options.Approve = func(request ApprovalRequest) (ApprovalDecision, error) {
		git(t, root, "commit", "--allow-empty", "-m", "unexpected head mutation")
		return ApprovalDecision{Outcome: ApprovalApproved, DiffHash: request.DiffHash}, nil
	}
	_, err := Run(context.Background(), options)
	if !errors.Is(err, ErrBaselineChanged) {
		t.Fatalf("expected HEAD mutation rejection, got %v", err)
	}
}

func TestRepairRejectsWorkerTimeout(t *testing.T) {
	root := makeSyntheticProject(t)
	options := fakeOptions(root, []string{"calculator.go"}, passAfterRepair())
	options.Propose = func(Task) (Proposal, error) { return Proposal{}, context.DeadlineExceeded }
	_, err := Run(context.Background(), options)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected worker timeout, got %v", err)
	}
}

func TestRepairDistinguishesApprovalRejectionAndCancellation(t *testing.T) {
	root := makeSyntheticProject(t)
	options := fakeOptions(root, []string{"calculator.go"}, passAfterRepair())
	options.Approve = func(ApprovalRequest) (ApprovalDecision, error) {
		return ApprovalDecision{Outcome: ApprovalRejected}, nil
	}
	result, err := Run(context.Background(), options)
	if !errors.Is(err, ErrApprovalRejected) || result.Approval.Outcome != ApprovalRejected {
		t.Fatalf("expected explicit rejection, got result=%+v err=%v", result, err)
	}

	root = makeSyntheticProject(t)
	options = fakeOptions(root, []string{"calculator.go"}, passAfterRepair())
	options.Approve = func(ApprovalRequest) (ApprovalDecision, error) {
		return ApprovalDecision{Outcome: ApprovalCancelled}, nil
	}
	result, err = Run(context.Background(), options)
	if !errors.Is(err, ErrApprovalCancelled) || result.Approval.Outcome != ApprovalCancelled {
		t.Fatalf("expected explicit cancellation, got result=%+v err=%v", result, err)
	}

	root = makeSyntheticProject(t)
	options = fakeOptions(root, []string{"calculator.go"}, passAfterRepair())
	options.Approve = func(ApprovalRequest) (ApprovalDecision, error) { return ApprovalDecision{}, context.Canceled }
	result, err = Run(context.Background(), options)
	if !errors.Is(err, ErrApprovalCancelled) || result.Approval.Outcome != ApprovalCancelled {
		t.Fatalf("expected context cancellation from approval seam, got result=%+v err=%v", result, err)
	}
}

func TestRepairCancelledBeforeApprovalDoesNotModifyRepository(t *testing.T) {
	root := makeSyntheticProject(t)
	original := readProjectFile(t, root, "calculator.go")
	ctx, cancel := context.WithCancel(context.Background())
	options := fakeOptions(root, []string{"calculator.go"}, passAfterRepair())
	approvalCalled := false
	options.Approve = func(ApprovalRequest) (ApprovalDecision, error) { approvalCalled = true; return ApprovalDecision{}, nil }
	cancel()
	result, err := Run(ctx, options)
	if !errors.Is(err, ErrCancelled) || result.FinalStatus != model.Error || approvalCalled {
		t.Fatalf("expected cancellation before approval, got result=%+v err=%v approval=%v", result, err, approvalCalled)
	}
	if got := readProjectFile(t, root, "calculator.go"); string(got) != string(original) {
		t.Fatal("cancelled repair changed the repository")
	}
}

func TestRepairCancelledAfterApprovalDoesNotApplyPatch(t *testing.T) {
	root := makeSyntheticProject(t)
	original := readProjectFile(t, root, "calculator.go")
	ctx, cancel := context.WithCancel(context.Background())
	options := fakeOptions(root, []string{"calculator.go"}, passAfterRepair())
	options.Approve = func(request ApprovalRequest) (ApprovalDecision, error) {
		cancel()
		return ApprovalDecision{Outcome: ApprovalApproved, DiffHash: request.DiffHash}, nil
	}
	result, err := Run(ctx, options)
	if !errors.Is(err, ErrCancelled) || result.Approval.Outcome != ApprovalCancelled {
		t.Fatalf("expected cancellation after approval, got result=%+v err=%v", result, err)
	}
	if got := readProjectFile(t, root, "calculator.go"); string(got) != string(original) {
		t.Fatal("cancelled approved repair changed the repository")
	}
}

func TestRepairCancellationDuringApplyRestoresExactBaseline(t *testing.T) {
	root := makeSyntheticProject(t)
	original := readProjectFile(t, root, "calculator.go")
	ctx, cancel := context.WithCancel(context.Background())
	options := fakeOptions(root, []string{"calculator.go"}, passAfterRepair())
	firstWrite := true
	options.WriteFile = func(path string, data []byte, mode fs.FileMode) error {
		if err := atomicWrite(path, data, mode); err != nil {
			return err
		}
		if firstWrite {
			firstWrite = false
			cancel()
		}
		return nil
	}
	_, err := Run(ctx, options)
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("expected cancellation during apply, got %v", err)
	}
	if got := readProjectFile(t, root, "calculator.go"); string(got) != string(original) {
		t.Fatal("cancellation during apply did not restore the baseline")
	}
}

func TestRepairRejectsDirtyBaseline(t *testing.T) {
	root := makeSyntheticProject(t)
	os.WriteFile(filepath.Join(root, "calculator.go"), []byte("dirty\n"), 0o644)
	options := fakeOptions(root, []string{"calculator.go"}, passAfterRepair())
	result, err := Run(context.Background(), options)
	if !errors.Is(err, ErrDirtyBaseline) {
		t.Fatalf("expected dirty baseline rejection, got %v", err)
	}
	if result.Approved {
		t.Fatal("dirty baseline was approved")
	}
}

func TestRepairRejectsWrongApprovalHash(t *testing.T) {
	root := makeSyntheticProject(t)
	options := fakeOptions(root, []string{"calculator.go"}, passAfterRepair())
	options.Approve = func(ApprovalRequest) (ApprovalDecision, error) {
		return ApprovalDecision{Approved: true, DiffHash: "wrong"}, nil
	}
	_, err := Run(context.Background(), options)
	if !errors.Is(err, ErrDiffMismatch) {
		t.Fatalf("expected diff mismatch, got %v", err)
	}
}

func TestRepairRejectsPatchMutationThroughBaselineChange(t *testing.T) {
	root := makeSyntheticProject(t)
	options := fakeOptions(root, []string{"calculator.go"}, passAfterRepair())
	options.Approve = func(request ApprovalRequest) (ApprovalDecision, error) {
		if err := os.WriteFile(filepath.Join(root, "calculator.go"), []byte("changed after approval\n"), 0o644); err != nil {
			return ApprovalDecision{}, err
		}
		return ApprovalDecision{Approved: true, DiffHash: request.DiffHash}, nil
	}
	_, err := Run(context.Background(), options)
	if !errors.Is(err, ErrBaselineChanged) {
		t.Fatalf("expected baseline change rejection, got %v", err)
	}
}

func TestRepairUsesImmutablePatchAfterApprovalCallbackMutation(t *testing.T) {
	root := makeSyntheticProject(t)
	expected := []byte("package calculator\nfunc Add(left, right int) int { return left + right }\n")
	options := fakeOptions(root, []string{"calculator.go"}, passAfterRepair())
	options.Propose = func(task Task) (Proposal, error) {
		return Proposal{SchemaVersion: ProtocolVersion, TaskID: task.TaskID, Worker: "test", Changes: []FileChange{{Path: "calculator.go", Content: expected}}}, nil
	}
	options.Approve = func(request ApprovalRequest) (ApprovalDecision, error) {
		request.CanonicalPatch[0] ^= 0xff
		return ApprovalDecision{Approved: true, DiffHash: request.DiffHash}, nil
	}
	if _, err := Run(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	if got := readProjectFile(t, root, "calculator.go"); string(got) != string(expected) {
		t.Fatalf("approval callback mutated the applied patch: %s", got)
	}
}

func TestRepairRejectsApprovalCancellation(t *testing.T) {
	root := makeSyntheticProject(t)
	options := fakeOptions(root, []string{"calculator.go"}, passAfterRepair())
	options.Approve = func(ApprovalRequest) (ApprovalDecision, error) {
		return ApprovalDecision{Approved: false}, nil
	}
	_, err := Run(context.Background(), options)
	if !errors.Is(err, ErrApprovalRejected) {
		t.Fatalf("expected approval rejection, got %v", err)
	}
}

func TestRepairRejectsStoredArtifactMutationAfterApproval(t *testing.T) {
	root := makeSyntheticProject(t)
	options := fakeOptions(root, []string{"calculator.go"}, passAfterRepair())
	options.Approve = func(request ApprovalRequest) (ApprovalDecision, error) {
		artifactPath := filepath.Join(root, ".devctl", "evidence", "repair", "repair-test.patch")
		data, err := os.ReadFile(artifactPath)
		if err != nil {
			return ApprovalDecision{}, err
		}
		data[0] ^= 0xff
		if err := os.WriteFile(artifactPath, data, 0o600); err != nil {
			return ApprovalDecision{}, err
		}
		return ApprovalDecision{Approved: true, DiffHash: request.DiffHash}, nil
	}
	_, err := Run(context.Background(), options)
	if !errors.Is(err, ErrDiffMismatch) {
		t.Fatalf("expected stored artifact mutation rejection, got %v", err)
	}
}

func TestRepairRejectsWorkerError(t *testing.T) {
	root := makeSyntheticProject(t)
	options := fakeOptions(root, []string{"calculator.go"}, passAfterRepair())
	options.Propose = func(Task) (Proposal, error) {
		return Proposal{}, errors.New("worker stopped")
	}
	_, err := Run(context.Background(), options)
	if err == nil || !strings.Contains(err.Error(), "worker stopped") {
		t.Fatalf("expected worker error, got %v", err)
	}
}

func TestRepairRejectsPolicyChangeBeforeApply(t *testing.T) {
	root := makeSyntheticProject(t)
	options := fakeOptions(root, []string{"calculator.go"}, passAfterRepair())
	options.Approve = func(request ApprovalRequest) (ApprovalDecision, error) {
		if err := os.WriteFile(filepath.Join(root, "devctl.json"), []byte("{\n  \"version\": \"1\",\n  \"project_id\": \"changed-policy\"\n}\n"), 0o644); err != nil {
			return ApprovalDecision{}, err
		}
		return ApprovalDecision{Approved: true, DiffHash: request.DiffHash}, nil
	}
	_, err := Run(context.Background(), options)
	if !errors.Is(err, ErrBaselineChanged) {
		t.Fatalf("expected policy provenance rejection, got %v", err)
	}
}

func TestPreflightRejectsTargetThatNoLongerApplies(t *testing.T) {
	root := makeSyntheticProject(t)
	baseline, err := captureSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	change := canonicalFileChange{Path: "calculator.go", Preimage: readProjectFile(t, root, "calculator.go"), Postimage: []byte("package calculator\nfunc Add(left, right int) int { return left + right }\n")}
	if err := os.WriteFile(filepath.Join(root, "calculator.go"), []byte("changed before preflight\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := preflight(root, []canonicalFileChange{change}, baseline); err == nil {
		t.Fatal("expected patch preflight to reject changed target")
	}
}

func TestRepairRejectsActualBytesThatDifferFromApprovedPatch(t *testing.T) {
	root := makeSyntheticProject(t)
	original := readProjectFile(t, root, "calculator.go")
	options := fakeOptions(root, []string{"calculator.go"}, passAfterRepair())
	options.WriteFile = func(path string, _ []byte, mode fs.FileMode) error {
		return atomicWrite(path, []byte("package calculator\nfunc Add(left, right int) int { return 123 }\n"), mode)
	}
	result, err := Run(context.Background(), options)
	if !errors.Is(err, ErrPostState) || result.FinalStatus != model.Error || countEvent(result.Events, "POST_STATE_VALIDATION_FAILED") != 1 {
		t.Fatalf("expected explicit post-state ERROR, got result=%+v err=%v", result, err)
	}
	if got := readProjectFile(t, root, "calculator.go"); string(got) != string(original) {
		t.Fatal("post-state hash mismatch did not roll back the applied file")
	}
}

func TestRepairRejectsIndexMutationDuringApplication(t *testing.T) {
	root := makeSyntheticProject(t)
	options := fakeOptions(root, []string{"calculator.go"}, passAfterRepair())
	options.WriteFile = func(path string, data []byte, mode fs.FileMode) error {
		if err := atomicWrite(path, data, mode); err != nil {
			return err
		}
		git(t, root, "add", "calculator.go")
		return nil
	}
	_, err := Run(context.Background(), options)
	if !errors.Is(err, ErrPostState) {
		t.Fatalf("expected index mutation rejection, got %v", err)
	}
}

func TestRepairRejectsModeMutationDuringApplication(t *testing.T) {
	root := makeSyntheticProject(t)
	options := fakeOptions(root, []string{"calculator.go"}, passAfterRepair())
	mutatedMode := fs.FileMode(0o755)
	if runtime.GOOS == "windows" {
		mutatedMode = 0o400
	}
	options.WriteFile = func(path string, data []byte, mode fs.FileMode) error {
		if err := atomicWrite(path, data, mode); err != nil {
			return err
		}
		return os.Chmod(path, mutatedMode)
	}
	_, err := Run(context.Background(), options)
	if !errors.Is(err, ErrPostState) {
		t.Fatalf("expected mode mutation rejection, got %v", err)
	}
}

func TestRepairRollbackMustProveExactRestoration(t *testing.T) {
	root := makeSyntheticProject(t)
	if err := os.WriteFile(filepath.Join(root, "helper.go"), []byte("package calculator\n\nfunc Helper() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "helper.go")
	git(t, root, "commit", "-m", "add helper")
	options := fakeOptions(root, []string{"calculator.go", "helper.go"}, passAfterRepair())
	options.Propose = func(task Task) (Proposal, error) {
		return Proposal{SchemaVersion: ProtocolVersion, TaskID: task.TaskID, Worker: "test", Changes: []FileChange{
			{Path: "calculator.go", Content: []byte("package calculator\nfunc Add(left, right int) int { return left + right }\n")},
			{Path: "helper.go", Content: []byte("package calculator\nfunc Helper() int { return 2 }\n")},
		}}, nil
	}
	failingPath := filepath.Join(root, "helper.go")
	failed := false
	options.WriteFile = func(path string, data []byte, mode fs.FileMode) error {
		if path == failingPath && !failed {
			failed = true
			if err := atomicWrite(path, data, mode); err != nil {
				return err
			}
			return errors.New("injected write failure")
		}
		if path == failingPath && failed {
			return nil
		}
		return atomicWrite(path, data, mode)
	}
	_, err := Run(context.Background(), options)
	if !errors.Is(err, ErrRollback) {
		t.Fatalf("expected rollback proof failure, got %v", err)
	}
	if got := string(readProjectFile(t, root, "helper.go")); !strings.Contains(got, "return 2") {
		t.Fatal("rollback probe did not leave the deliberately un-restored file modified")
	}
}

func TestRepairRejectsBinaryProposalContent(t *testing.T) {
	root := makeSyntheticProject(t)
	options := fakeOptions(root, []string{"calculator.go"}, passAfterRepair())
	options.Propose = func(task Task) (Proposal, error) {
		return Proposal{SchemaVersion: ProtocolVersion, TaskID: task.TaskID, Worker: "test", Changes: []FileChange{{Path: "calculator.go", Content: []byte{0xff, 0xfe, 0, 1}}}}, nil
	}
	_, err := Run(context.Background(), options)
	if err == nil || !strings.Contains(err.Error(), "canonical UTF-8 text") {
		t.Fatalf("expected binary proposal rejection, got %v", err)
	}
}

func TestRepairRejectsForbiddenAndUntrustedPaths(t *testing.T) {
	for _, test := range []struct {
		name    string
		path    string
		allowed []string
		want    error
	}{
		{name: "forbidden test", path: "calculator_test.go", allowed: []string{"calculator_test.go"}, want: ErrForbiddenPath},
		{name: "untrusted source", path: "other.go", allowed: []string{"calculator.go"}, want: ErrUntrustedPath},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := makeSyntheticProject(t)
			options := fakeOptions(root, test.allowed, passAfterRepair())
			options.Propose = func(task Task) (Proposal, error) {
				return Proposal{SchemaVersion: ProtocolVersion, TaskID: task.TaskID, Worker: "test", Changes: []FileChange{{Path: test.path, Content: []byte("package calculator\n")}}}, nil
			}
			_, err := Run(context.Background(), options)
			if !errors.Is(err, test.want) {
				t.Fatalf("expected %v, got %v", test.want, err)
			}
		})
	}
}

func TestRepairRejectsSecondAttempt(t *testing.T) {
	root := makeSyntheticProject(t)
	options := fakeOptions(root, []string{"calculator.go"}, passAfterRepair())
	options.Attempt = 2
	_, err := Run(context.Background(), options)
	if !errors.Is(err, ErrSecondAttempt) {
		t.Fatalf("expected second attempt rejection, got %v", err)
	}
}

func TestRepairRollsBackPartialApplication(t *testing.T) {
	root := makeSyntheticProject(t)
	if err := os.WriteFile(filepath.Join(root, "helper.go"), []byte("package calculator\n\nfunc Helper() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "helper.go")
	git(t, root, "commit", "-m", "add helper")
	originalCalculator := readProjectFile(t, root, "calculator.go")
	originalHelper := readProjectFile(t, root, "helper.go")
	options := fakeOptions(root, []string{"calculator.go", "helper.go"}, passAfterRepair())
	options.Propose = func(task Task) (Proposal, error) {
		return Proposal{SchemaVersion: ProtocolVersion, TaskID: task.TaskID, Worker: "test", Changes: []FileChange{
			{Path: "calculator.go", Content: []byte("package calculator\nfunc Add(left, right int) int { return left + right }\n")},
			{Path: "helper.go", Content: []byte("package calculator\nfunc Helper() int { return 2 }\n")},
		}}, nil
	}
	failingPath := filepath.Join(root, "helper.go")
	failed := false
	options.WriteFile = func(path string, data []byte, mode fs.FileMode) error {
		if path == failingPath && !failed {
			failed = true
			if err := os.WriteFile(path, data, mode); err != nil {
				return err
			}
			return errors.New("injected partial write failure")
		}
		return atomicWrite(path, data, mode)
	}
	_, err := Run(context.Background(), options)
	if !errors.Is(err, ErrApply) {
		t.Fatalf("expected application failure, got %v", err)
	}
	if got := readProjectFile(t, root, "calculator.go"); string(got) != string(originalCalculator) {
		t.Fatal("calculator.go was not restored after partial application")
	}
	if got := readProjectFile(t, root, "helper.go"); string(got) != string(originalHelper) {
		t.Fatal("helper.go was not restored after partial application")
	}
}

func TestRepairRejectsUnexpectedPostState(t *testing.T) {
	root := makeSyntheticProject(t)
	extraPath := filepath.Join(root, "unexpected.go")
	options := fakeOptions(root, []string{"calculator.go"}, passAfterRepair())
	options.WriteFile = func(path string, data []byte, mode fs.FileMode) error {
		if err := atomicWrite(path, data, mode); err != nil {
			return err
		}
		return os.WriteFile(extraPath, []byte("package calculator\n"), 0o644)
	}
	_, err := Run(context.Background(), options)
	if !errors.Is(err, ErrPostState) {
		t.Fatalf("expected unexpected post-state rejection, got %v", err)
	}
}

func TestRepairRecordsVerificationFailureAfterRepair(t *testing.T) {
	root := makeSyntheticProject(t)
	options := fakeOptions(root, []string{"calculator.go"}, failAfterRepair())
	result, err := Run(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalStatus != model.Fail {
		t.Fatalf("expected failed re-verification, got %+v", result)
	}
	if countEvent(result.Events, "VERIFY_STARTED") != 1 || countEvent(result.Events, "REPAIR_TASK_CREATED") != 1 {
		t.Fatalf("unexpected retry lifecycle: %+v", result.Events)
	}
}

func TestRepairStopsOnVerificationErrorAfterRepair(t *testing.T) {
	root := makeSyntheticProject(t)
	options := fakeOptions(root, []string{"calculator.go"}, model.Error)
	result, err := Run(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalStatus != model.Error || countEvent(result.Events, "VERIFY_STARTED") != 1 {
		t.Fatalf("expected immediate verification error stop: %+v", result)
	}
}

func fakeOptions(root string, allowed []string, final model.Status) Options {
	verificationCalls := 0
	return Options{
		ProjectPath: root, ProjectID: "synthetic-repair", TaskID: "repair-test", Worker: "test", Attempt: 1, AllowedPaths: allowed,
		Verify: func(context.Context, string) model.Report {
			verificationCalls++
			status := model.Fail
			if verificationCalls > 1 {
				status = final
			}
			return model.Report{RunID: fmt.Sprintf("run-%d", verificationCalls), Overall: status, DevctlVersion: "test-devctl-version", DevctlCommit: "test-devctl-commit", DevctlDirty: false, PolicyVersion: "test-policy-version", Project: &model.Project{Identity: "synthetic-repair", Path: root}, Checks: []model.CheckResult{{ID: "synthetic-check", Status: status, Blocking: true, Summary: string(status)}}}
		},
		Propose: func(task Task) (Proposal, error) {
			return Proposal{SchemaVersion: ProtocolVersion, TaskID: task.TaskID, Worker: "test", Changes: []FileChange{{Path: "calculator.go", Content: []byte("package calculator\nfunc Add(left, right int) int { return left + right }\n")}}}, nil
		},
		Approve: func(request ApprovalRequest) (ApprovalDecision, error) {
			return ApprovalDecision{Approved: true, DiffHash: request.DiffHash}, nil
		},
	}
}

func passAfterRepair() model.Status { return model.Pass }

func failAfterRepair() model.Status { return model.Fail }

func makeSyntheticProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"go.mod":             "module example.com/synthetic-repair\n\ngo 1.22\n",
		"calculator.go":      "package calculator\n\nfunc Add(left, right int) int {\n\treturn left - right\n}\n",
		"calculator_test.go": "package calculator\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif got := Add(2, 3); got != 5 {\n\t\tt.Fatalf(\"Add() = %d\", got)\n\t}\n}\n",
		"devctl.json":        "{\n  \"version\": \"1\",\n  \"project_id\": \"synthetic-repair\",\n  \"checks\": {\n    \"go-test-race\": {\"enabled\": false}\n  }\n}\n",
		".gitignore":         ".devctl/\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git(t, root, "init")
	git(t, root, "config", "user.email", "repair-test@example.invalid")
	git(t, root, "config", "user.name", "repair-test")
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "baseline")
	return root
}

func git(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func readProjectFile(t *testing.T, root, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func readPersistedResult(t *testing.T, root string, result Result) Result {
	t.Helper()
	data := readProjectFile(t, root, filepath.ToSlash(result.EvidencePath))
	var persisted Result
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("parse persisted repair result: %v", err)
	}
	return persisted
}

func eventTypes(events []Event) []string {
	result := make([]string, 0, len(events))
	for _, event := range events {
		result = append(result, event.Type)
	}
	return result
}

func countEvent(events []Event, eventType string) int {
	count := 0
	for _, event := range events {
		if event.Type == eventType {
			count++
		}
	}
	return count
}
