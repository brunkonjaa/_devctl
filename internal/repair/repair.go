package repair

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"devctl/internal/events"
	"devctl/internal/handoff"
	"devctl/internal/model"
	"devctl/internal/registry"
)

const (
	ProtocolVersion            = "1"
	maxTaskText                = 512
	maxTaskChecks              = 16
	maxTaskFindings            = 8
	maxPatchFiles              = 16
	maxPatchBytes              = 256 * 1024
	maxTaskIDLength            = 64
	forbiddenPathPolicyVersion = "stage7d-a-forbidden-paths-v1"
)

var taskIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

var (
	ErrDirtyBaseline     = errors.New("repair requires a clean baseline")
	ErrBaselineChanged   = errors.New("repair baseline changed before apply")
	ErrDiffMismatch      = errors.New("repair diff does not match approved proposal")
	ErrForbiddenPath     = errors.New("repair proposal contains a forbidden path")
	ErrUntrustedPath     = errors.New("repair proposal contains an untrusted path")
	ErrPatchPreflight    = errors.New("repair patch preflight failed")
	ErrApply             = errors.New("repair patch application failed")
	ErrRollback          = errors.New("repair rollback could not prove exact restoration")
	ErrPostState         = errors.New("repair post-state does not match approved patch")
	ErrSecondAttempt     = errors.New("repair allows only one attempt")
	ErrApprovalRejected  = errors.New("repair proposal was rejected")
	ErrApprovalCancelled = errors.New("repair approval was cancelled")
	ErrCancelled         = errors.New("repair orchestration was cancelled")
	ErrInvalidTaskID     = errors.New("repair task_id is not a safe identifier")
	ErrWorkerIdentity    = errors.New("repair worker identity does not match trusted configuration")
)

type VerifyFunc func(context.Context, string) model.Report

type VerificationExitCodeFunc func(model.Report) int

type ProposeFunc func(context.Context, Task) (Proposal, error)

type ApproveFunc func(ApprovalRequest) (ApprovalDecision, error)

type Options struct {
	ProjectPath          string
	ProjectID            string
	TaskID               string
	Worker               string
	Attempt              int
	AllowedPaths         []string
	Verify               VerifyFunc
	VerificationExitCode VerificationExitCodeFunc
	Propose              ProposeFunc
	Approve              ApproveFunc
	ProgressSink         events.Sink
	WriteFile            func(path string, data []byte, mode fs.FileMode) error
}

type Task struct {
	SchemaVersion              string                   `json:"schema_version"`
	TaskID                     string                   `json:"task_id"`
	ProjectID                  string                   `json:"project_id"`
	WorkerID                   string                   `json:"worker_id"`
	RunID                      string                   `json:"run_id"`
	CanonicalProject           CanonicalProjectMetadata `json:"canonical_project"`
	AllowedPaths               []string                 `json:"allowed_paths"`
	ForbiddenPathPolicyVersion string                   `json:"forbidden_path_policy_version"`
	ForbiddenPathClasses       []string                 `json:"forbidden_path_classes"`
	DevctlProvenance           DevctlProvenance         `json:"devctl_provenance"`
	PolicyProvenanceHash       string                   `json:"policy_provenance_hash"`
	Failure                    model.FailurePacket      `json:"failure"`
	Attempt                    int                      `json:"attempt"`
}

type Proposal struct {
	SchemaVersion string       `json:"schema_version"`
	TaskID        string       `json:"task_id"`
	Worker        string       `json:"worker"`
	Changes       []FileChange `json:"changes"`
}

type FileChange struct {
	Path    string `json:"path"`
	Content []byte `json:"content"`
}

type ApprovalRequest struct {
	TaskID         string
	ProjectID      string
	BaselineRunID  string
	WorkerID       string
	Protocol       string
	Failure        model.FailurePacket
	CanonicalPatch []byte
	DiffHash       string
	DisplayDiff    string
	Evidence       ApprovalEvidenceView
}

type ApprovalEvidenceView struct {
	CanonicalProject           CanonicalProjectMetadata
	ForbiddenPathPolicyVersion string
	ForbiddenPathClasses       []string
	DevctlProvenance           DevctlProvenance
	PolicyProvenanceHash       string
	PatchArtifact              string
	EvidencePath               string
	Files                      []ApprovalFileEvidence
}

type ApprovalFileEvidence struct {
	Path      string
	PreHash   string
	PostHash  string
	PreBytes  int64
	PostBytes int64
	PreMode   fs.FileMode
	PostMode  fs.FileMode
}

type ApprovalOutcome string

const (
	ApprovalApproved  ApprovalOutcome = "APPROVED"
	ApprovalRejected  ApprovalOutcome = "REJECTED"
	ApprovalCancelled ApprovalOutcome = "CANCELLED"
)

type ApprovalDecision struct {
	Outcome  ApprovalOutcome
	Approved bool // Deprecated compatibility field; Outcome is authoritative.
	DiffHash string
}

type Event struct {
	Sequence int       `json:"sequence"`
	Type     string    `json:"type"`
	At       time.Time `json:"at"`
	Message  string    `json:"message,omitempty"`
}

type FileProvenance struct {
	Path      string `json:"path"`
	Change    string `json:"change"`
	PreHash   string `json:"pre_hash"`
	PostHash  string `json:"post_hash"`
	PreBytes  int64  `json:"pre_bytes"`
	PostBytes int64  `json:"post_bytes"`
}

type Result struct {
	SchemaVersion   string           `json:"schema_version"`
	TaskID          string           `json:"task_id"`
	ProjectID       string           `json:"project_id"`
	Attempt         int              `json:"attempt"`
	BaselineRunID   string           `json:"baseline_run_id,omitempty"`
	FinalRunID      string           `json:"final_run_id,omitempty"`
	InitialStatus   model.Status     `json:"initial_status"`
	FinalStatus     model.Status     `json:"final_status"`
	InitialExitCode int              `json:"initial_exit_code"`
	FinalExitCode   int              `json:"final_exit_code"`
	DiffHash        string           `json:"diff_hash,omitempty"`
	ActualDiffHash  string           `json:"actual_diff_hash,omitempty"`
	PatchArtifact   string           `json:"patch_artifact,omitempty"`
	DisplayDiff     string           `json:"display_diff,omitempty"`
	DisplayHash     string           `json:"display_hash,omitempty"`
	WorkerID        string           `json:"worker_id,omitempty"`
	Approved        bool             `json:"approved"`
	Task            *Task            `json:"task,omitempty"`
	Proposal        *Proposal        `json:"proposal,omitempty"`
	Approval        ApprovalEvidence `json:"approval"`
	Baseline        BaselineEvidence `json:"baseline"`
	Files           []FileProvenance `json:"files,omitempty"`
	Events          []Event          `json:"events"`
	EvidencePath    string           `json:"evidence_path,omitempty"`
	Error           string           `json:"error,omitempty"`
}

type ApprovalEvidence struct {
	Outcome     ApprovalOutcome `json:"outcome,omitempty"`
	TaskID      string          `json:"task_id,omitempty"`
	ProjectID   string          `json:"project_id,omitempty"`
	WorkerID    string          `json:"worker_id,omitempty"`
	Protocol    string          `json:"protocol,omitempty"`
	DiffHash    string          `json:"diff_hash,omitempty"`
	DisplayHash string          `json:"display_hash,omitempty"`
}

type FileSnapshot struct {
	Path  string      `json:"path"`
	Hash  string      `json:"hash"`
	Bytes int64       `json:"bytes"`
	Mode  fs.FileMode `json:"mode"`
}

type BaselineEvidence struct {
	Head                       string                   `json:"head"`
	Branch                     string                   `json:"branch"`
	IndexHash                  string                   `json:"index_hash"`
	Status                     string                   `json:"status"`
	ProjectID                  string                   `json:"project_id"`
	ProvenanceHash             string                   `json:"provenance_hash"`
	PolicyProvenanceHash       string                   `json:"policy_provenance_hash"`
	CanonicalProject           CanonicalProjectMetadata `json:"canonical_project"`
	ForbiddenPathPolicyVersion string                   `json:"forbidden_path_policy_version"`
	ForbiddenPathClasses       []string                 `json:"forbidden_path_classes"`
	DevctlProvenance           DevctlProvenance         `json:"devctl_provenance"`
	VerificationRunID          string                   `json:"verification_run_id"`
	Files                      []FileSnapshot           `json:"files"`
}

type fileState struct {
	Hash  string      `json:"hash"`
	Bytes int64       `json:"bytes"`
	Mode  fs.FileMode `json:"mode"`
}

type snapshot struct {
	Root       string
	Head       string
	Branch     string
	IndexHash  string
	Status     string
	Identity   string
	Provenance string
	Files      map[string]fileState
}

type CanonicalProjectMetadata struct {
	Root      string `json:"root"`
	ProjectID string `json:"project_id"`
}

type DevctlProvenance struct {
	Version string `json:"version,omitempty"`
	Commit  string `json:"commit,omitempty"`
	Dirty   bool   `json:"dirty"`
}

type patchArtifact struct {
	Changes []canonicalFileChange `json:"changes"`
}

type canonicalFileChange struct {
	Path      string `json:"path"`
	Preimage  []byte `json:"preimage"`
	Postimage []byte `json:"postimage"`
}

type fileBackup struct {
	path string
	data []byte
	mode fs.FileMode
}

func Run(ctx context.Context, options Options) (Result, error) {
	result := Result{SchemaVersion: ProtocolVersion, TaskID: options.TaskID, ProjectID: options.ProjectID, Attempt: options.Attempt, WorkerID: options.Worker}
	progressCtx := ctx
	if options.ProgressSink != nil {
		progressCtx = events.WithSink(ctx, events.NewStream(options.ProgressSink))
	}
	if options.Attempt != 1 {
		return stop(result, model.Error, ErrSecondAttempt)
	}
	if options.Verify == nil || options.Propose == nil || options.Approve == nil {
		return stop(result, model.Error, errors.New("repair verifier, proposer, and approver are required"))
	}
	if err := validateTaskID(options.TaskID); err != nil {
		return stop(result, model.Error, err)
	}
	if strings.TrimSpace(options.Worker) == "" {
		return stop(result, model.Error, errors.New("trusted repair worker identity must not be empty"))
	}
	root, err := canonicalRoot(options.ProjectPath)
	if err != nil {
		return stop(result, model.Error, err)
	}
	if err := checkCancelled(ctx); err != nil {
		return stopAt(root, result, model.Error, err)
	}
	if err := validateAllowedPaths(options.AllowedPaths); err != nil {
		return stopAt(root, result, model.Error, err)
	}
	baselineReport := options.Verify(progressCtx, root)
	if err := checkCancelled(ctx); err != nil {
		return stopAt(root, result, model.Error, err)
	}
	result.InitialStatus = baselineReport.Overall
	result.InitialExitCode = verificationExitCode(options, baselineReport)
	result.BaselineRunID = boundedText(baselineReport.RunID)
	if baselineReport.Overall != model.Fail {
		result.FinalStatus = baselineReport.Overall
		result.FinalExitCode = result.InitialExitCode
		addRepairEvent(progressCtx, &result, "REPAIR_STOPPED", "baseline was not FAIL")
		return persist(root, result)
	}
	baseline, err := captureSnapshot(root)
	if err != nil {
		return stopAt(root, result, model.Error, err)
	}
	if baseline.Status != "" {
		return stopAt(root, result, model.Error, fmt.Errorf("%w: %s", ErrDirtyBaseline, baseline.Status))
	}
	if options.ProjectID != "" && baseline.Identity != options.ProjectID {
		return stopAt(root, result, model.Error, fmt.Errorf("project identity mismatch: expected %q, got %q", options.ProjectID, baseline.Identity))
	}
	result.ProjectID = baseline.Identity
	result.Baseline = baseline.evidence(baselineReport)
	progressCtx = events.WithMetadata(progressCtx, baselineReport.RunID, baseline.Identity)
	packet := boundedFailurePacket(handoff.FromReport(baselineReport))
	policyClasses := forbiddenPathClasses()
	task := Task{
		SchemaVersion:              ProtocolVersion,
		TaskID:                     options.TaskID,
		ProjectID:                  baseline.Identity,
		WorkerID:                   options.Worker,
		RunID:                      baselineReport.RunID,
		CanonicalProject:           CanonicalProjectMetadata{Root: baseline.Root, ProjectID: baseline.Identity},
		AllowedPaths:               append([]string(nil), options.AllowedPaths...),
		ForbiddenPathPolicyVersion: forbiddenPathPolicyVersion,
		ForbiddenPathClasses:       policyClasses,
		DevctlProvenance:           devctlProvenance(baselineReport),
		PolicyProvenanceHash:       baseline.Provenance,
		Failure:                    packet,
		Attempt:                    1,
	}
	taskForWorker := cloneTask(task)
	result.Task = &task
	addRepairEvent(progressCtx, &result, "REPAIR_TASK_CREATED", "bounded failure task created")
	addRepairEvent(progressCtx, &result, "BASELINE_CAPTURED", "clean repository state captured")
	if err := checkCancelled(ctx); err != nil {
		return stopAt(root, result, model.Error, err)
	}
	proposal, err := options.Propose(progressCtx, taskForWorker)
	if err != nil {
		return stopAt(root, result, model.Error, err)
	}
	if err := checkCancelled(ctx); err != nil {
		return stopAt(root, result, model.Error, err)
	}
	addRepairEvent(progressCtx, &result, "WORKER_PROPOSAL_RECEIVED", "proposal received")
	canonical, changes, err := canonicalizeProposal(root, task, proposal)
	if err != nil {
		return stopAt(root, result, model.Error, err)
	}
	hash := sha256Hex(canonical)
	result.DiffHash = hash
	addRepairEvent(progressCtx, &result, "PROPOSAL_VALIDATED", "proposal scope and patch bounds validated")
	artifactPath, err := persistPatchArtifact(root, task.TaskID, canonical)
	if err != nil {
		return stopAt(root, result, model.Error, err)
	}
	result.PatchArtifact = artifactPath
	storedPatch, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(artifactPath)))
	if err != nil {
		return stopAt(root, result, model.Error, err)
	}
	if sha256Hex(storedPatch) != hash {
		return stopAt(root, result, model.Error, ErrDiffMismatch)
	}
	changes, err = decodePatchArtifact(storedPatch)
	if err != nil {
		return stopAt(root, result, model.Error, err)
	}
	diffText, err := displayDiff(changes)
	if err != nil {
		return stopAt(root, result, model.Error, err)
	}
	result.DisplayDiff = diffText
	result.DisplayHash = sha256Hex([]byte(diffText))
	result.Proposal = &Proposal{SchemaVersion: proposal.SchemaVersion, TaskID: proposal.TaskID, Worker: options.Worker, Changes: proposalChanges(changes)}
	result.Approval = ApprovalEvidence{TaskID: task.TaskID, ProjectID: task.ProjectID, WorkerID: options.Worker, Protocol: ProtocolVersion, DiffHash: hash, DisplayHash: result.DisplayHash}
	evidencePath, _, err := safeRepairPath(root, task.TaskID, ".json")
	if err != nil {
		return stopAt(root, result, model.Error, err)
	}
	approvalView, err := buildApprovalEvidence(task, baseline, changes, artifactPath, evidencePath)
	if err != nil {
		return stopAt(root, result, model.Error, err)
	}
	addRepairEvent(progressCtx, &result, "PATCH_ARTIFACT_STORED", "immutable canonical patch artifact stored")
	addRepairEvent(progressCtx, &result, "DIFF_DISPLAYED", "exact diff displayed for approval")
	if err := checkCancelled(ctx); err != nil {
		result.Approval.Outcome = ApprovalCancelled
		addRepairEvent(progressCtx, &result, "CANCELLED", "orchestration was cancelled before approval")
		return stopAt(root, result, model.Error, err)
	}
	decision, err := options.Approve(ApprovalRequest{TaskID: task.TaskID, ProjectID: task.ProjectID, BaselineRunID: task.RunID, WorkerID: options.Worker, Protocol: ProtocolVersion, Failure: task.Failure, CanonicalPatch: append([]byte(nil), storedPatch...), DiffHash: hash, DisplayDiff: diffText, Evidence: cloneApprovalEvidence(approvalView)})
	if err != nil {
		if errors.Is(err, ErrApprovalCancelled) || errors.Is(err, context.Canceled) {
			result.Approval.Outcome = ApprovalCancelled
			addRepairEvent(progressCtx, &result, "CANCELLED", "human approval was cancelled")
			if ctx.Err() != nil {
				return stopAt(root, result, model.Error, checkCancelled(ctx))
			}
			return stopAt(root, result, model.Error, ErrApprovalCancelled)
		}
		return stopAt(root, result, model.Error, err)
	}
	outcome := decision.Outcome
	if outcome == "" {
		if decision.Approved {
			outcome = ApprovalApproved
		} else {
			outcome = ApprovalRejected
		}
	}
	result.Approval.Outcome = outcome
	if outcome == ApprovalCancelled {
		addRepairEvent(progressCtx, &result, "CANCELLED", "human approval was cancelled")
		return stopAt(root, result, model.Error, ErrApprovalCancelled)
	}
	if outcome != ApprovalApproved {
		addRepairEvent(progressCtx, &result, "REJECTED", "human rejected the exact diff")
		return stopAt(root, result, model.Error, ErrApprovalRejected)
	}
	if decision.DiffHash != hash {
		return stopAt(root, result, model.Error, fmt.Errorf("%w: approval hash %q, proposal hash %q", ErrDiffMismatch, decision.DiffHash, hash))
	}
	result.Approved = true
	addRepairEvent(progressCtx, &result, "APPROVED", "human approved the exact diff")
	if err := checkCancelled(ctx); err != nil {
		result.Approval.Outcome = ApprovalCancelled
		addRepairEvent(progressCtx, &result, "CANCELLED", "orchestration was cancelled after approval")
		return stopAt(root, result, model.Error, err)
	}
	storedPatch, err = os.ReadFile(filepath.Join(root, filepath.FromSlash(artifactPath)))
	if err != nil {
		return stopAt(root, result, model.Error, err)
	}
	if sha256Hex(storedPatch) != hash {
		return stopAt(root, result, model.Error, ErrDiffMismatch)
	}
	changes, err = decodePatchArtifact(storedPatch)
	if err != nil {
		return stopAt(root, result, model.Error, err)
	}
	if err := checkCancelled(ctx); err != nil {
		result.Approval.Outcome = ApprovalCancelled
		addRepairEvent(progressCtx, &result, "CANCELLED", "orchestration was cancelled before pre-apply validation")
		return stopAt(root, result, model.Error, err)
	}
	current, err := captureSnapshot(root)
	if err != nil {
		return stopAt(root, result, model.Error, err)
	}
	if !snapshotsEqual(baseline, current) {
		return stopAt(root, result, model.Error, ErrBaselineChanged)
	}
	addRepairEvent(progressCtx, &result, "PRE_APPLY_STATE_VALIDATED", "approved baseline still matches")
	if err := checkCancelled(ctx); err != nil {
		result.Approval.Outcome = ApprovalCancelled
		addRepairEvent(progressCtx, &result, "CANCELLED", "orchestration was cancelled before patch preflight")
		return stopAt(root, result, model.Error, err)
	}
	if err := preflight(root, changes, baseline); err != nil {
		return stopAt(root, result, model.Error, fmt.Errorf("%w: %v", ErrPatchPreflight, err))
	}
	addRepairEvent(progressCtx, &result, "PATCH_PREFLIGHT", "complete patch applicability validated")
	if err := checkCancelled(ctx); err != nil {
		result.Approval.Outcome = ApprovalCancelled
		addRepairEvent(progressCtx, &result, "CANCELLED", "orchestration was cancelled before patch application")
		return stopAt(root, result, model.Error, err)
	}
	writer := options.WriteFile
	if writer == nil {
		writer = atomicWrite
	}
	backups, err := applyTransaction(ctx, root, changes, baseline, writer)
	if err != nil {
		if errors.Is(err, ErrCancelled) {
			result.Approval.Outcome = ApprovalCancelled
			addRepairEvent(progressCtx, &result, "CANCELLED", "orchestration was cancelled during patch application")
		}
		return stopAt(root, result, model.Error, fmt.Errorf("%w: %w", ErrApply, err))
	}
	addRepairEvent(progressCtx, &result, "PATCH_APPLIED", "approved patch applied as one transaction")
	if cancelledResult, cancellationErr, stopped := rollbackOnPostApplyCancellation(ctx, progressCtx, root, result, backups, baseline, "orchestration was cancelled after patch application"); stopped {
		return cancelledResult, cancellationErr
	}
	post, err := captureSnapshot(root)
	if cancelledResult, cancellationErr, stopped := rollbackOnPostApplyCancellation(ctx, progressCtx, root, result, backups, baseline, "orchestration was cancelled during post-state validation"); stopped {
		return cancelledResult, cancellationErr
	}
	if err != nil {
		if rollbackErr := rollbackPostApplyAndProve(root, backups, baseline); rollbackErr != nil {
			return stopAt(root, result, model.Error, rollbackErr)
		}
		return stopAt(root, result, model.Error, err)
	}
	actualPatch, err := reconstructActualPatch(root, changes)
	if cancelledResult, cancellationErr, stopped := rollbackOnPostApplyCancellation(ctx, progressCtx, root, result, backups, baseline, "orchestration was cancelled during post-state validation"); stopped {
		return cancelledResult, cancellationErr
	}
	if err != nil {
		if rollbackErr := rollbackPostApplyAndProve(root, backups, baseline); rollbackErr != nil {
			return stopAt(root, result, model.Error, rollbackErr)
		}
		addRepairEvent(progressCtx, &result, "POST_STATE_VALIDATION_FAILED", "post-apply patch could not be reconstructed")
		return stopAt(root, result, model.Error, fmt.Errorf("%w: %v", ErrPostState, err))
	}
	result.ActualDiffHash = sha256Hex(actualPatch)
	if cancelledResult, cancellationErr, stopped := rollbackOnPostApplyCancellation(ctx, progressCtx, root, result, backups, baseline, "orchestration was cancelled during post-state validation"); stopped {
		return cancelledResult, cancellationErr
	}
	if result.ActualDiffHash != result.DiffHash {
		if rollbackErr := rollbackPostApplyAndProve(root, backups, baseline); rollbackErr != nil {
			return stopAt(root, result, model.Error, rollbackErr)
		}
		addRepairEvent(progressCtx, &result, "POST_STATE_VALIDATION_FAILED", "actual post-change diff hash did not match the approved hash")
		return stopAt(root, result, model.Error, fmt.Errorf("%w: actual hash %q, approved hash %q", ErrPostState, result.ActualDiffHash, result.DiffHash))
	}
	provenance, err := deltaProvenance(baseline, post, changes)
	if cancelledResult, cancellationErr, stopped := rollbackOnPostApplyCancellation(ctx, progressCtx, root, result, backups, baseline, "orchestration was cancelled during post-state validation"); stopped {
		return cancelledResult, cancellationErr
	}
	if err != nil {
		if rollbackErr := rollbackPostApplyAndProve(root, backups, baseline); rollbackErr != nil {
			return stopAt(root, result, model.Error, rollbackErr)
		}
		addRepairEvent(progressCtx, &result, "POST_STATE_VALIDATION_FAILED", "post-apply state did not match the approved patch")
		return stopAt(root, result, model.Error, fmt.Errorf("%w: %v", ErrPostState, err))
	}
	result.Files = provenance
	addRepairEvent(progressCtx, &result, "POST_STATE_CAPTURED", "post-apply state captured")
	addRepairEvent(progressCtx, &result, "DELTA_VALIDATED", "actual delta matches approved patch")
	if cancelledResult, cancellationErr, stopped := rollbackOnPostApplyCancellation(ctx, progressCtx, root, result, backups, baseline, "orchestration was cancelled before re-verification"); stopped {
		return cancelledResult, cancellationErr
	}
	addRepairEvent(progressCtx, &result, "VERIFY_STARTED", "deterministic re-verification started")
	finalReport := options.Verify(progressCtx, root)
	result.FinalRunID = boundedText(finalReport.RunID)
	result.FinalStatus = finalReport.Overall
	result.FinalExitCode = verificationExitCode(options, finalReport)
	if cancelledResult, cancellationErr, stopped := rollbackOnPostApplyCancellation(ctx, progressCtx, root, result, backups, baseline, "orchestration was cancelled after re-verification"); stopped {
		return cancelledResult, cancellationErr
	}
	addRepairEvent(progressCtx, &result, "VERIFY_FINISHED", "deterministic re-verification finished")
	addRepairEvent(progressCtx, &result, "REPAIR_STOPPED", "one repair attempt completed")
	if cancelledResult, cancellationErr, stopped := rollbackOnPostApplyCancellation(ctx, progressCtx, root, result, backups, baseline, "orchestration was cancelled after re-verification"); stopped {
		return cancelledResult, cancellationErr
	}
	return persist(root, result)
}

func stop(result Result, status model.Status, err error) (Result, error) {
	result.FinalStatus = status
	if err != nil {
		result.Error = boundedText(err.Error())
	}
	addEvent(&result, "REPAIR_STOPPED", "repair stopped")
	return result, err
}

func stopAt(root string, result Result, status model.Status, err error) (Result, error) {
	result, runErr := stop(result, status, err)
	persisted, writeErr := persist(root, result)
	if runErr != nil {
		return persisted, runErr
	}
	return persisted, writeErr
}

func rollbackOnPostApplyCancellation(ctx, progressCtx context.Context, root string, result Result, backups []fileBackup, baseline snapshot, message string) (Result, error, bool) {
	cancellationErr := checkCancelled(ctx)
	if cancellationErr == nil {
		return result, nil, false
	}
	result.Approval.Outcome = ApprovalCancelled
	addRepairEvent(progressCtx, &result, "CANCELLED", message)
	if rollbackErr := rollbackPostApplyAndProve(root, backups, baseline); rollbackErr != nil {
		addRepairEvent(progressCtx, &result, "ROLLBACK_FAILED", "cancelled repair could not prove baseline restoration")
		stopped, stopErr := stopAt(root, result, model.Error, rollbackErr)
		return stopped, stopErr, true
	}
	stopped, stopErr := stopAt(root, result, model.Error, cancellationErr)
	return stopped, stopErr, true
}

func checkCancelled(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("%w: %v", ErrCancelled, ctx.Err())
	default:
		return nil
	}
}

func verificationExitCode(options Options, report model.Report) int {
	if options.VerificationExitCode == nil {
		return 0
	}
	code := options.VerificationExitCode(report)
	if code < 0 || code > 255 {
		return 2
	}
	return code
}

func persistPatchArtifact(root, taskID string, data []byte) (string, error) {
	relative, path, err := safeRepairPath(root, taskID, ".patch")
	if err != nil {
		return "", err
	}
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".patch-*.tmp")
	if err != nil {
		return "", err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return "", err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return "", err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return "", err
	}
	if err := temporary.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return "", err
	}
	return relative, nil
}

func persist(root string, result Result) (Result, error) {
	relative, path, err := safeRepairPath(root, result.TaskID, ".json")
	if err != nil {
		return result, err
	}
	result.EvidencePath = relative
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return result, err
	}
	data = append(data, '\n')
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".repair-*.tmp")
	if err != nil {
		return result, err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return result, err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return result, err
	}
	if err := temporary.Close(); err != nil {
		return result, err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return result, err
	}
	return result, nil
}

func validateTaskID(taskID string) error {
	if len(taskID) == 0 || len(taskID) > maxTaskIDLength || !taskIDPattern.MatchString(taskID) || strings.Contains(taskID, "..") || filepath.VolumeName(taskID) != "" {
		return ErrInvalidTaskID
	}
	return nil
}

func safeRepairPath(root, taskID, suffix string) (string, string, error) {
	if err := validateTaskID(taskID); err != nil {
		return "", "", err
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", "", err
	}
	directory := filepath.Join(root, ".devctl", "evidence", "repair")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", "", err
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", err
	}
	realDirectory, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return "", "", err
	}
	if !pathWithin(realRoot, realDirectory) {
		return "", "", fmt.Errorf("%w: repair evidence directory is outside project", ErrInvalidTaskID)
	}
	path := filepath.Join(realDirectory, taskID+suffix)
	if !pathWithin(realDirectory, path) {
		return "", "", ErrInvalidTaskID
	}
	return filepath.ToSlash(filepath.Join(".devctl", "evidence", "repair", taskID+suffix)), path, nil
}

func pathWithin(base, target string) bool {
	relative, err := filepath.Rel(base, target)
	if err != nil || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

func canonicalRoot(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("repair project path must not be empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

func captureSnapshot(root string) (snapshot, error) {
	project, err := registry.DetectProject(root)
	if err != nil {
		return snapshot{}, err
	}
	head, err := gitOutput(root, "rev-parse", "HEAD")
	if err != nil {
		return snapshot{}, err
	}
	branch, err := gitOutput(root, "branch", "--show-current")
	if err != nil {
		return snapshot{}, err
	}
	status, err := gitOutput(root, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return snapshot{}, err
	}
	index, err := gitOutputBytes(root, "diff", "--cached", "--binary")
	if err != nil {
		return snapshot{}, err
	}
	files, err := walkFiles(root)
	if err != nil {
		return snapshot{}, err
	}
	provenance, err := projectProvenance(root)
	if err != nil {
		return snapshot{}, err
	}
	return snapshot{Root: root, Head: strings.TrimSpace(head), Branch: strings.TrimSpace(branch), IndexHash: sha256Hex(index), Status: status, Identity: project.ProjectID, Provenance: provenance, Files: files}, nil
}

func (value snapshot) evidence(report model.Report) BaselineEvidence {
	paths := make([]string, 0, len(value.Files))
	for path := range value.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	files := make([]FileSnapshot, 0, len(paths))
	for _, path := range paths {
		state := value.Files[path]
		files = append(files, FileSnapshot{Path: path, Hash: state.Hash, Bytes: state.Bytes, Mode: state.Mode})
	}
	return BaselineEvidence{
		Head: value.Head, Branch: value.Branch, IndexHash: value.IndexHash, Status: value.Status,
		ProjectID: value.Identity, ProvenanceHash: value.Provenance, PolicyProvenanceHash: value.Provenance,
		CanonicalProject:           CanonicalProjectMetadata{Root: value.Root, ProjectID: value.Identity},
		ForbiddenPathPolicyVersion: forbiddenPathPolicyVersion, ForbiddenPathClasses: forbiddenPathClasses(),
		DevctlProvenance: devctlProvenance(report), VerificationRunID: boundedText(report.RunID), Files: files,
	}
}

func devctlProvenance(report model.Report) DevctlProvenance {
	return DevctlProvenance{Version: report.DevctlVersion, Commit: report.DevctlCommit, Dirty: report.DevctlDirty}
}

func forbiddenPathClasses() []string {
	return []string{
		"tests-and-fixtures",
		"devctl-policy-and-configuration",
		"project-verification-configuration",
		"git-metadata",
		"ci-and-workflow-files",
		"generated-evidence-and-journals",
		"build-outputs-and-dependency-caches",
		"verification-bypass-scripts",
	}
}

func cloneTask(value Task) Task {
	clone := value
	clone.AllowedPaths = append([]string(nil), value.AllowedPaths...)
	clone.ForbiddenPathClasses = append([]string(nil), value.ForbiddenPathClasses...)
	clone.Failure.Failures = make([]model.FailureItem, len(value.Failure.Failures))
	for index, failure := range value.Failure.Failures {
		clone.Failure.Failures[index] = failure
		clone.Failure.Failures[index].EvidencePaths = append([]string(nil), failure.EvidencePaths...)
		clone.Failure.Failures[index].Findings = append([]model.Finding(nil), failure.Findings...)
	}
	return clone
}

func buildApprovalEvidence(task Task, baseline snapshot, changes []canonicalFileChange, patchArtifact, evidencePath string) (ApprovalEvidenceView, error) {
	files := make([]ApprovalFileEvidence, 0, len(changes))
	for _, change := range changes {
		state, ok := baseline.Files[change.Path]
		if !ok {
			return ApprovalEvidenceView{}, fmt.Errorf("approval evidence is missing baseline file: %s", change.Path)
		}
		files = append(files, ApprovalFileEvidence{
			Path: change.Path, PreHash: state.Hash, PostHash: sha256Hex(change.Postimage),
			PreBytes: state.Bytes, PostBytes: int64(len(change.Postimage)), PreMode: state.Mode, PostMode: state.Mode,
		})
	}
	return ApprovalEvidenceView{
		CanonicalProject:           task.CanonicalProject,
		ForbiddenPathPolicyVersion: task.ForbiddenPathPolicyVersion,
		ForbiddenPathClasses:       append([]string(nil), task.ForbiddenPathClasses...),
		DevctlProvenance:           task.DevctlProvenance,
		PolicyProvenanceHash:       task.PolicyProvenanceHash,
		PatchArtifact:              patchArtifact,
		EvidencePath:               evidencePath,
		Files:                      files,
	}, nil
}

func cloneApprovalEvidence(value ApprovalEvidenceView) ApprovalEvidenceView {
	clone := value
	clone.ForbiddenPathClasses = append([]string(nil), value.ForbiddenPathClasses...)
	clone.Files = append([]ApprovalFileEvidence(nil), value.Files...)
	return clone
}

func gitOutput(root string, args ...string) (string, error) {
	data, err := gitOutputBytes(root, args...)
	return string(data), err
}

func gitOutputBytes(root string, args ...string) ([]byte, error) {
	commandArgs := append([]string{"-C", root}, args...)
	command := exec.Command("git", commandArgs...)
	data, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return data, nil
}

func walkFiles(root string) (map[string]fileState, error) {
	files := make(map[string]fileState)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == ".git" || strings.HasPrefix(rel, ".git/") || rel == ".devctl" || strings.HasPrefix(rel, ".devctl/") {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported non-regular file %s", rel)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[rel] = fileState{Hash: sha256Hex(data), Bytes: int64(len(data)), Mode: info.Mode().Perm()}
		return nil
	})
	return files, err
}

func projectProvenance(root string) (string, error) {
	paths := []string{filepath.Join(root, "devctl.json"), filepath.Join(filepath.Dir(root), "_devctl", "config", "defaults.json")}
	var data []byte
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		data = append(data, []byte(filepath.ToSlash(path)+"\x00")...)
		data = append(data, content...)
		data = append(data, 0)
	}
	return sha256Hex(data), nil
}

func snapshotsEqual(left, right snapshot) bool {
	if left.Root != right.Root || left.Head != right.Head || left.Branch != right.Branch || left.IndexHash != right.IndexHash || left.Status != right.Status || left.Identity != right.Identity || left.Provenance != right.Provenance || len(left.Files) != len(right.Files) {
		return false
	}
	for path, state := range left.Files {
		if right.Files[path] != state {
			return false
		}
	}
	return true
}

func validateAllowedPaths(paths []string) error {
	if len(paths) == 0 || len(paths) > maxPatchFiles {
		return ErrUntrustedPath
	}
	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		clean, err := cleanRelative(path)
		if err != nil || seen[clean] {
			return ErrUntrustedPath
		}
		if forbiddenPath(clean) {
			return ErrForbiddenPath
		}
		seen[clean] = true
	}
	return nil
}

func canonicalizeProposal(root string, task Task, proposal Proposal) ([]byte, []canonicalFileChange, error) {
	if proposal.SchemaVersion != ProtocolVersion || proposal.TaskID != task.TaskID || proposal.Worker != task.WorkerID {
		if proposal.Worker != task.WorkerID {
			return nil, nil, fmt.Errorf("%w: expected %q, got %q", ErrWorkerIdentity, task.WorkerID, proposal.Worker)
		}
		return nil, nil, errors.New("invalid repair proposal envelope")
	}
	if len(proposal.Changes) == 0 || len(proposal.Changes) > maxPatchFiles {
		return nil, nil, errors.New("repair proposal has an invalid file count")
	}
	allowed := make(map[string]bool, len(task.AllowedPaths))
	for _, path := range task.AllowedPaths {
		clean, _ := cleanRelative(path)
		allowed[clean] = true
	}
	changes := make([]canonicalFileChange, len(proposal.Changes))
	for index, change := range proposal.Changes {
		changes[index] = canonicalFileChange{Path: change.Path, Postimage: append([]byte(nil), change.Content...)}
	}
	for index := range changes {
		clean, err := cleanRelative(changes[index].Path)
		if err != nil {
			return nil, nil, err
		}
		if forbiddenPath(clean) {
			return nil, nil, fmt.Errorf("%w: %s", ErrForbiddenPath, clean)
		}
		if !allowed[clean] {
			return nil, nil, fmt.Errorf("%w: %s", ErrUntrustedPath, clean)
		}
		changes[index].Path = clean
		if len(changes[index].Postimage) == 0 || len(changes[index].Postimage) > maxPatchBytes {
			return nil, nil, errors.New("repair file content is empty or oversized")
		}
		if !utf8.Valid(changes[index].Postimage) || bytes.Contains(changes[index].Postimage, []byte{0}) || bytes.Contains(changes[index].Postimage, []byte{'\r'}) || bytes.HasPrefix(changes[index].Postimage, []byte{0xef, 0xbb, 0xbf}) {
			return nil, nil, errors.New("repair content must be canonical UTF-8 text with LF line endings")
		}
	}
	sort.Slice(changes, func(left, right int) bool { return changes[left].Path < changes[right].Path })
	for index := 1; index < len(changes); index++ {
		if changes[index-1].Path == changes[index].Path {
			return nil, nil, errors.New("repair proposal contains a duplicate path")
		}
	}
	for index, change := range changes {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(change.Path)))
		if err != nil || !info.Mode().IsRegular() {
			return nil, nil, fmt.Errorf("repair target is not an existing regular file: %s", change.Path)
		}
		before, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(change.Path)))
		if err != nil {
			return nil, nil, err
		}
		changes[index].Preimage = append([]byte(nil), before...)
	}
	artifact, err := encodePatchArtifact(changes)
	if err != nil {
		return nil, nil, err
	}
	if len(artifact) > maxPatchBytes {
		return nil, nil, errors.New("canonical repair patch is oversized")
	}
	return artifact, changes, nil
}

func cloneChanges(changes []FileChange) []FileChange {
	cloned := make([]FileChange, len(changes))
	for index, change := range changes {
		cloned[index] = FileChange{Path: change.Path, Content: append([]byte(nil), change.Content...)}
	}
	return cloned
}

func cloneCanonicalChanges(changes []canonicalFileChange) []canonicalFileChange {
	cloned := make([]canonicalFileChange, len(changes))
	for index, change := range changes {
		cloned[index] = canonicalFileChange{Path: change.Path, Preimage: append([]byte(nil), change.Preimage...), Postimage: append([]byte(nil), change.Postimage...)}
	}
	return cloned
}

func proposalChanges(changes []canonicalFileChange) []FileChange {
	result := make([]FileChange, len(changes))
	for index, change := range changes {
		result[index] = FileChange{Path: change.Path, Content: append([]byte(nil), change.Postimage...)}
	}
	return result
}

func encodePatchArtifact(changes []canonicalFileChange) ([]byte, error) {
	return json.Marshal(patchArtifact{Changes: changes})
}

func decodePatchArtifact(data []byte) ([]canonicalFileChange, error) {
	var artifact patchArtifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		return nil, fmt.Errorf("decode stored repair patch: %w", err)
	}
	if len(artifact.Changes) == 0 || len(artifact.Changes) > maxPatchFiles {
		return nil, errors.New("stored repair patch has an invalid file count")
	}
	for _, change := range artifact.Changes {
		if _, err := cleanRelative(change.Path); err != nil || forbiddenPath(change.Path) || len(change.Preimage) == 0 || len(change.Postimage) == 0 || !utf8.Valid(change.Preimage) || !utf8.Valid(change.Postimage) || bytes.Contains(change.Preimage, []byte{0}) || bytes.Contains(change.Postimage, []byte{0}) || bytes.Contains(change.Preimage, []byte{'\r'}) || bytes.Contains(change.Postimage, []byte{'\r'}) {
			return nil, errors.New("stored repair patch failed textual validation")
		}
	}
	return cloneCanonicalChanges(artifact.Changes), nil
}

func cleanRelative(path string) (string, error) {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" || strings.HasPrefix(path, "/") || filepath.VolumeName(path) != "" {
		return "", ErrUntrustedPath
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "\x00") {
		return "", ErrUntrustedPath
	}
	return clean, nil
}

func forbiddenPath(path string) bool {
	parts := strings.Split(path, "/")
	for _, part := range parts {
		if part == ".git" || part == ".devctl" || part == "build" || part == "node_modules" || part == "target" || part == "test" || part == "tests" || part == "testdata" || part == "scripts" {
			return true
		}
	}
	base := filepath.Base(path)
	return strings.HasSuffix(base, "_test.go") || base == "devctl.json" || path == "config/defaults.json" || strings.HasPrefix(path, ".github/") || strings.HasPrefix(path, ".github")
}

func preflight(root string, changes []canonicalFileChange, baseline snapshot) error {
	for _, change := range changes {
		path := filepath.Join(root, filepath.FromSlash(change.Path))
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		state, ok := baseline.Files[change.Path]
		if !ok || sha256Hex(data) != state.Hash || !bytes.Equal(data, change.Preimage) {
			return fmt.Errorf("target changed before preflight: %s", change.Path)
		}
	}
	return nil
}

func applyTransaction(ctx context.Context, root string, changes []canonicalFileChange, baseline snapshot, writer func(string, []byte, fs.FileMode) error) ([]fileBackup, error) {
	if writer == nil {
		writer = atomicWrite
	}
	backups := make([]fileBackup, 0, len(changes))
	for _, change := range changes {
		path := filepath.Join(root, filepath.FromSlash(change.Path))
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		state := baseline.Files[change.Path]
		if sha256Hex(data) != state.Hash {
			return nil, fmt.Errorf("target changed before apply: %s", change.Path)
		}
		backups = append(backups, fileBackup{path: path, data: append([]byte(nil), data...), mode: state.Mode})
	}
	attempted := make([]fileBackup, 0, len(backups))
	for index, change := range changes {
		backup := backups[index]
		if err := checkCancelled(ctx); err != nil {
			return nil, err
		}
		attempted = append(attempted, backup)
		if err := writer(backup.path, change.Postimage, backup.mode); err != nil {
			if rollbackErr := rollbackAndProve(root, attempted, baseline, writer); rollbackErr != nil {
				return nil, rollbackErr
			}
			return nil, err
		}
		if err := checkCancelled(ctx); err != nil {
			if rollbackErr := rollbackAndProve(root, attempted, baseline, writer); rollbackErr != nil {
				return nil, rollbackErr
			}
			return nil, err
		}
	}
	return backups, nil
}

func rollbackAndProve(root string, attempted []fileBackup, baseline snapshot, writer func(string, []byte, fs.FileMode) error) error {
	for rollbackIndex := len(attempted) - 1; rollbackIndex >= 0; rollbackIndex-- {
		item := attempted[rollbackIndex]
		if rollbackErr := writer(item.path, item.data, item.mode); rollbackErr != nil {
			return fmt.Errorf("%w: rollback %s: %v", ErrRollback, item.path, rollbackErr)
		}
	}
	restored, snapshotErr := captureSnapshot(root)
	if snapshotErr != nil {
		return fmt.Errorf("%w: capture restored state: %v", ErrRollback, snapshotErr)
	}
	if !snapshotsEqual(baseline, restored) {
		return fmt.Errorf("%w: restored state differs from baseline", ErrRollback)
	}
	return nil
}

func rollbackPostApplyAndProve(root string, backups []fileBackup, baseline snapshot) error {
	for rollbackIndex := len(backups) - 1; rollbackIndex >= 0; rollbackIndex-- {
		item := backups[rollbackIndex]
		if chmodErr := os.Chmod(item.path, 0o600); chmodErr != nil {
			return fmt.Errorf("%w: prepare rollback %s: %v", ErrRollback, item.path, chmodErr)
		}
		if rollbackErr := atomicWrite(item.path, item.data, item.mode); rollbackErr != nil {
			return fmt.Errorf("%w: rollback %s: %v", ErrRollback, item.path, rollbackErr)
		}
	}
	if _, resetErr := gitOutput(root, "reset", "--mixed", "HEAD"); resetErr != nil {
		return fmt.Errorf("%w: restore clean index: %v", ErrRollback, resetErr)
	}
	after, snapshotErr := captureSnapshot(root)
	if snapshotErr != nil {
		return fmt.Errorf("%w: capture post-rollback state: %v", ErrRollback, snapshotErr)
	}
	for path := range after.Files {
		if _, existed := baseline.Files[path]; existed {
			continue
		}
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			return fmt.Errorf("%w: remove unexpected file %s: %v", ErrRollback, path, err)
		}
	}
	restored, snapshotErr := captureSnapshot(root)
	if snapshotErr != nil {
		return fmt.Errorf("%w: capture restored state: %v", ErrRollback, snapshotErr)
	}
	if !snapshotsEqual(baseline, restored) {
		return fmt.Errorf("%w: restored state differs from baseline", ErrRollback)
	}
	return nil
}

func atomicWrite(path string, data []byte, mode fs.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".repair-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func reconstructActualPatch(root string, changes []canonicalFileChange) ([]byte, error) {
	actual := cloneCanonicalChanges(changes)
	for index, change := range actual {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(change.Path)))
		if err != nil {
			return nil, err
		}
		actual[index].Postimage = data
	}
	return encodePatchArtifact(actual)
}

func deltaProvenance(before, after snapshot, changes []canonicalFileChange) ([]FileProvenance, error) {
	if before.Head != after.Head || before.Branch != after.Branch || before.IndexHash != after.IndexHash || before.Identity != after.Identity || before.Provenance != after.Provenance {
		return nil, errors.New("protected repository state changed after apply")
	}
	expected := make(map[string]bool, len(changes))
	result := make([]FileProvenance, 0, len(changes))
	for _, change := range changes {
		expected[change.Path] = true
		pre, ok := before.Files[change.Path]
		post, postOK := after.Files[change.Path]
		if !ok || !postOK || pre.Hash == post.Hash {
			return nil, fmt.Errorf("approved file did not change: %s", change.Path)
		}
		if expectedHash := sha256Hex(change.Postimage); post.Hash != expectedHash {
			return nil, fmt.Errorf("actual bytes differ from approved content: %s", change.Path)
		}
		if pre.Mode != post.Mode {
			return nil, fmt.Errorf("file mode changed: %s", change.Path)
		}
		result = append(result, FileProvenance{Path: change.Path, Change: "modified", PreHash: pre.Hash, PostHash: post.Hash, PreBytes: pre.Bytes, PostBytes: post.Bytes})
	}
	for path, pre := range before.Files {
		post, ok := after.Files[path]
		if !ok || !fileStatesEqual(pre, post) && !expected[path] {
			return nil, fmt.Errorf("unexpected file delta: %s", path)
		}
		if expected[path] && pre.Mode != post.Mode {
			return nil, fmt.Errorf("approved file mode changed: %s", path)
		}
	}
	for path := range after.Files {
		if _, ok := before.Files[path]; !ok {
			return nil, fmt.Errorf("unexpected added file: %s", path)
		}
	}
	return result, nil
}

func fileStatesEqual(left, right fileState) bool {
	return left.Hash == right.Hash && left.Bytes == right.Bytes && left.Mode == right.Mode
}

func displayDiff(changes []canonicalFileChange) (string, error) {
	var builder strings.Builder
	for _, change := range changes {
		oldLines := diffLines(string(change.Preimage))
		newLines := diffLines(string(change.Postimage))
		fmt.Fprintf(&builder, "--- a/%s\n+++ b/%s\n@@ -1,%d +1,%d @@\n", change.Path, change.Path, len(oldLines), len(newLines))
		writeDiffLines(&builder, "-", oldLines, change.Preimage)
		writeDiffLines(&builder, "+", newLines, change.Postimage)
	}
	return builder.String(), nil
}

func writeDiffLines(builder *strings.Builder, prefix string, lines []string, content []byte) {
	for _, line := range lines {
		builder.WriteString(prefix)
		builder.WriteString(line)
		builder.WriteByte('\n')
	}
	if len(content) > 0 && !bytes.HasSuffix(content, []byte{'\n'}) {
		builder.WriteString("\\ No newline at end of file\n")
	}
}

func diffLines(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func boundedFailurePacket(packet model.FailurePacket) model.FailurePacket {
	packet.RunID = boundedText(packet.RunID)
	packet.Project = boundedText(packet.Project)
	packet.DevctlVersion = boundedText(packet.DevctlVersion)
	packet.DevctlCommit = boundedText(packet.DevctlCommit)
	packet.EvidencePath = boundedText(packet.EvidencePath)
	packet.NextAction = boundedText(packet.NextAction)
	if len(packet.Failures) > maxTaskChecks {
		packet.Failures = packet.Failures[:maxTaskChecks]
	}
	for index := range packet.Failures {
		failure := &packet.Failures[index]
		failure.CheckID = boundedText(failure.CheckID)
		failure.Summary = boundedText(failure.Summary)
		failure.Reason = boundedText(failure.Reason)
		if len(failure.EvidencePaths) > maxTaskFindings {
			failure.EvidencePaths = failure.EvidencePaths[:maxTaskFindings]
		}
		for evidenceIndex := range failure.EvidencePaths {
			failure.EvidencePaths[evidenceIndex] = boundedText(failure.EvidencePaths[evidenceIndex])
		}
		if len(failure.Findings) > maxTaskFindings {
			failure.Findings = failure.Findings[:maxTaskFindings]
		}
		for findingIndex := range failure.Findings {
			finding := &failure.Findings[findingIndex]
			finding.FindingID = boundedText(finding.FindingID)
			finding.Issue = boundedText(finding.Issue)
			finding.Path = boundedText(finding.Path)
		}
	}
	return packet
}

func boundedText(value string) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= maxTaskText {
		return string(runes)
	}
	return string(runes[:maxTaskText-3]) + "..."
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func addEvent(result *Result, eventType, message string) {
	result.Events = append(result.Events, Event{Sequence: len(result.Events) + 1, Type: eventType, At: time.Now().UTC(), Message: boundedText(message)})
}

func addRepairEvent(ctx context.Context, result *Result, eventType, message string) {
	addEvent(result, eventType, message)
	events.Emit(ctx, events.Event{EventType: events.RepairLifecycle, Status: eventType, Message: boundedText(message)})
}
