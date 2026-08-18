package repaircli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"devctl/internal/events"
	"devctl/internal/model"
	"devctl/internal/repair"
	"devctl/internal/verify"
)

const (
	ExitOK                  = 0
	ExitVerificationFailure = 1
	ExitFramework           = 2
	ExitRejected            = 3
	ExitCancelled           = 4
)

var ErrProvider = errors.New("repair proposal provider failure")

type Options struct {
	ProjectPath          string
	ProjectID            string
	TaskID               string
	Worker               string
	AllowedPaths         []string
	ProposalPath         string
	Propose              repair.ProposeFunc
	Input                io.Reader
	Output               io.Writer
	Diagnostics          io.Writer
	Interactive          bool
	Verbose              bool
	JSON                 bool
	Verify               repair.VerifyFunc
	VerificationExitCode repair.VerificationExitCodeFunc
}

type Output struct {
	SchemaVersion string        `json:"schema_version"`
	Status        model.Status  `json:"status"`
	Kind          string        `json:"kind"`
	ExitCode      int           `json:"exit_code"`
	Result        repair.Result `json:"result"`
	Error         string        `json:"error,omitempty"`
}

type terminal struct {
	out     io.Writer
	err     io.Writer
	verbose bool
	mu      sync.Mutex
}

type synchronizedWriter struct {
	mu     sync.Mutex
	writer io.Writer
}

func (writer *synchronizedWriter) Write(data []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.writer.Write(data)
}

func (t *terminal) Publish(event events.Event) {
	if event.EventType != events.RepairLifecycle {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	message := event.Message
	switch event.Status {
	case "REPAIR_TASK_CREATED":
		message = "Checking the project..."
	case "BASELINE_CAPTURED":
		message = "Preparing a possible fix..."
	case "WORKER_PROPOSAL_RECEIVED", "PROPOSAL_VALIDATED":
		message = "Possible fix ready."
	case "DIFF_DISPLAYED":
		message = "Waiting for approval."
	case "PRE_APPLY_STATE_VALIDATED":
		message = "Checking nothing changed since the fix was shown..."
	case "PATCH_APPLIED":
		message = "Applying fix..."
	case "POST_STATE_CAPTURED", "DELTA_VALIDATED":
		message = "Checking that only the approved files changed..."
	case "VERIFY_STARTED":
		message = "Testing the project again..."
	case "VERIFY_FINISHED":
		message = "Deterministic verification finished."
	case "CANCELLED":
		message = "Cancelled; restoring the baseline..."
	case "ROLLBACK_FAILED":
		message = "Rollback failed; manual inspection is required."
	case "REJECTED":
		message = "Repair rejected."
	case "REPAIR_STOPPED":
		if message == "one repair attempt completed" {
			message = "Repair attempt finished."
		}
	}
	if t.verbose {
		fmt.Fprintf(t.err, "repair %-28s %s\n", event.Status, message)
	} else {
		fmt.Fprintln(t.err, message)
	}
}

func Run(ctx context.Context, options Options) (Output, error) {
	if options.Output == nil {
		options.Output = io.Discard
	}
	if options.Diagnostics == nil {
		options.Diagnostics = io.Discard
	}
	options.Diagnostics = &synchronizedWriter{writer: options.Diagnostics}
	if options.Input == nil {
		options.Input = strings.NewReader("")
	}
	if options.TaskID == "" {
		options.TaskID = "repair-cli-001"
	}
	if options.Worker == "" {
		options.Worker = "controlled-cli"
	}
	if options.Verify == nil {
		options.Verify = func(ctx context.Context, path string) model.Report {
			return verify.ProjectWithOptions(ctx, path, verify.Options{})
		}
	}
	verificationExitCode := options.VerificationExitCode
	if verificationExitCode == nil {
		verificationExitCode = verify.ExitCode
	}
	term := &terminal{out: options.Output, err: options.Diagnostics, verbose: options.Verbose}
	sink := events.NewAsyncSink(term, 256)
	sinkClosed := false
	closeSink := func() {
		if !sinkClosed {
			sink.Close()
			sinkClosed = true
		}
	}
	defer closeSink()
	approval := func(request repair.ApprovalRequest) (repair.ApprovalDecision, error) {
		return approve(ctx, request, options)
	}
	propose := options.Propose
	if propose != nil {
		provider := propose
		propose = func(providerContext context.Context, task repair.Task) (repair.Proposal, error) {
			proposal, err := provider(providerContext, task)
			if err != nil {
				if providerContext.Err() != nil {
					return repair.Proposal{}, providerContext.Err()
				}
				return repair.Proposal{}, fmt.Errorf("%w: %v", ErrProvider, err)
			}
			return proposal, nil
		}
	} else {
		propose = func(context.Context, repair.Task) (repair.Proposal, error) {
			if options.ProposalPath == "" {
				return repair.Proposal{}, fmt.Errorf("%w: no controlled proposal provider is configured", ErrProvider)
			}
			proposal, err := readProposal(options.ProposalPath)
			if err != nil {
				return repair.Proposal{}, fmt.Errorf("%w: %v", ErrProvider, err)
			}
			return proposal, nil
		}
	}
	result, runErr := repair.Run(ctx, repair.Options{ProjectPath: options.ProjectPath, ProjectID: options.ProjectID, TaskID: options.TaskID, Worker: options.Worker, Attempt: 1, AllowedPaths: options.AllowedPaths, Verify: options.Verify, VerificationExitCode: verificationExitCode, Propose: propose, Approve: approval, ProgressSink: sink})
	closeSink()
	out := Output{SchemaVersion: "1", Status: result.FinalStatus, Kind: "DETERMINISTIC_RESULT", ExitCode: ExitOK, Result: result}
	if runErr != nil {
		out.Error = runErr.Error()
		out.Kind = classify(runErr, result)
		out.ExitCode = exitCode(runErr, result)
		if out.Status == "" {
			out.Status = model.Error
		}
	}
	if runErr == nil {
		out.ExitCode = result.FinalExitCode
		if result.Approval.Outcome == repair.ApprovalRejected {
			out.Kind = "REJECTED"
			out.ExitCode = ExitRejected
		}
		if result.Approval.Outcome == repair.ApprovalCancelled {
			out.Kind = "CANCELLED"
			out.ExitCode = ExitCancelled
		}
	}
	return finish(out, options)
}

func approve(ctx context.Context, request repair.ApprovalRequest, options Options) (repair.ApprovalDecision, error) {
	if !options.Interactive {
		fmt.Fprintln(options.Diagnostics, "APPROVAL UNAVAILABLE")
		return repair.ApprovalDecision{}, errors.New("interactive approval is unavailable")
	}
	reader := bufio.NewReader(options.Input)
	for {
		fmt.Fprintln(options.Diagnostics, "\nProposed change:")
		fmt.Fprintln(options.Diagnostics, request.DisplayDiff)
		for _, file := range request.Evidence.Files {
			fmt.Fprintf(options.Diagnostics, "Affected file: %s\n", file.Path)
		}
		fmt.Fprintf(options.Diagnostics, "Patch SHA-256: %s\n", request.DiffHash)
		fmt.Fprintln(options.Diagnostics, "Apply this fix? [A] Apply [R] Reject [D] Details [C] Cancel")
		line, err := readLine(ctx, reader)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
				return repair.ApprovalDecision{Outcome: repair.ApprovalCancelled}, repair.ErrApprovalCancelled
			}
			return repair.ApprovalDecision{}, err
		}
		switch strings.ToUpper(strings.TrimSpace(line)) {
		case "A":
			return repair.ApprovalDecision{Outcome: repair.ApprovalApproved, Approved: true, DiffHash: request.DiffHash}, nil
		case "R":
			return repair.ApprovalDecision{Outcome: repair.ApprovalRejected}, nil
		case "C":
			return repair.ApprovalDecision{Outcome: repair.ApprovalCancelled}, nil
		case "D":
			printDetails(options.Diagnostics, request)
		default:
			fmt.Fprintln(options.Diagnostics, "Please choose A, R, D, or C.")
		}
	}
}

func printDetails(out io.Writer, request repair.ApprovalRequest) {
	evidence := request.Evidence
	fmt.Fprintf(out, "Project ID: %s\nCanonical root: %s\nTask ID: %s\nBaseline run: %s\nWorker: %s\nProtocol: %s\nPatch artifact: %s\nEvidence path: %s\nPolicy provenance: %s\n", request.ProjectID, evidence.CanonicalProject.Root, request.TaskID, request.BaselineRunID, request.WorkerID, request.Protocol, evidence.PatchArtifact, evidence.EvidencePath, evidence.PolicyProvenanceHash)
	for _, file := range evidence.Files {
		fmt.Fprintf(out, "File: %s\n  pre  %s (%d bytes, mode %s)\n  post %s (%d bytes, mode %s)\n", file.Path, file.PreHash, file.PreBytes, file.PreMode, file.PostHash, file.PostBytes, file.PostMode)
	}
}

func readLine(ctx context.Context, input io.Reader) (string, error) {
	result := make(chan struct {
		line string
		err  error
	}, 1)
	go func() {
		line, err := bufio.NewReader(input).ReadString('\n')
		result <- struct {
			line string
			err  error
		}{strings.TrimSpace(line), err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case value := <-result:
		return value.line, value.err
	}
}
func readProposal(path string) (repair.Proposal, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return repair.Proposal{}, err
	}
	var proposal repair.Proposal
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&proposal); err != nil {
		return repair.Proposal{}, fmt.Errorf("parse proposal: %w", err)
	}
	return proposal, nil
}
func classify(err error, result repair.Result) string {
	switch {
	case errors.Is(err, repair.ErrApprovalRejected):
		return "REJECTED"
	case errors.Is(err, repair.ErrApprovalCancelled), errors.Is(err, repair.ErrCancelled):
		return "CANCELLED"
	case errors.Is(err, context.Canceled):
		return "CANCELLED"
	case errors.Is(err, repair.ErrRollback):
		return "ROLLBACK_FAILURE"
	case errors.Is(err, ErrProvider):
		return "PROVIDER_FAILURE"
	default:
		return "FRAMEWORK_ERROR"
	}
}
func exitCode(err error, result repair.Result) int {
	if errors.Is(err, repair.ErrApprovalRejected) {
		return ExitRejected
	}
	if errors.Is(err, repair.ErrApprovalCancelled) || errors.Is(err, repair.ErrCancelled) {
		return ExitCancelled
	}
	if errors.Is(err, context.Canceled) {
		return ExitCancelled
	}
	if errors.Is(err, repair.ErrRollback) {
		return ExitFramework
	}
	return ExitFramework
}
func finish(output Output, options Options) (Output, error) {
	if output.Status == "" {
		output.Status = model.Error
	}
	if options.JSON {
		if err := json.NewEncoder(options.Output).Encode(output); err != nil {
			return output, err
		}
	} else {
		if output.Error != "" {
			fmt.Fprintf(options.Diagnostics, "%s\n", output.Error)
		}
		switch output.Kind {
		case "REJECTED":
			fmt.Fprintln(options.Diagnostics, "Repair rejected.")
		case "CANCELLED":
			fmt.Fprintln(options.Diagnostics, "CANCELLED\nno project files changed")
		case "ROLLBACK_FAILURE":
			fmt.Fprintln(options.Diagnostics, "ROLLBACK FAILED")
		case "DETERMINISTIC_RESULT":
			if output.Result.InitialStatus != model.Fail && output.ExitCode == 0 {
				fmt.Fprintln(options.Diagnostics, "No blocking repair was required.")
			} else if output.ExitCode == 0 {
				fmt.Fprintln(options.Diagnostics, "Fix successful.")
			} else {
				fmt.Fprintf(options.Diagnostics, "Fix finished with status %s.\n", output.Status)
			}
		}
	}
	return output, nil
}
