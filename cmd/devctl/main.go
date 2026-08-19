package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"devctl/internal/discovery"
	"devctl/internal/events"
	"devctl/internal/handoff"
	"devctl/internal/live"
	"devctl/internal/model"
	"devctl/internal/registry"
	"devctl/internal/runner"
	"devctl/internal/session"
	"devctl/internal/verify"
	"devctl/internal/version"
	"devctl/internal/worker"
	"devctl/internal/workflow"
)

const (
	exitOK       = 0
	exitInternal = 2
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(exitInternal)
	}

	var exitCode int
	switch os.Args[1] {
	case "version":
		exitCode = versionCommand(os.Args[2:])
	case "discover":
		exitCode = discoverCommand(os.Args[2:])
	case "verify":
		exitCode = verifyCommand(os.Args[2:])
	case "worker":
		exitCode = workerCommand(os.Args[2:])
	case "session":
		exitCode = sessionCommand(os.Args[2:])
	case "handoff":
		exitCode = handoffCommand(os.Args[2:])
	case "failures":
		exitCode = failuresCommand(os.Args[2:])
	case "failure":
		exitCode = failureCommand(os.Args[2:])
	case "repair":
		exitCode = repairCommand(os.Args[2:])
	case "context":
		exitCode = contextCommand(os.Args[2:])
	case "status":
		exitCode = statusCommand(os.Args[2:])
	case "evidence":
		exitCode = evidenceCommand(os.Args[2:])
	case "history":
		exitCode = historyCommand(os.Args[2:])
	case "lessons":
		exitCode = lessonsCommand(os.Args[2:])
	case "knowledge":
		exitCode = knowledgeCommand(os.Args[2:])
	case "fixes":
		exitCode = fixesCommand(os.Args[2:])
	case "cache":
		exitCode = cacheCommand(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
		exitCode = exitInternal
	}
	os.Exit(exitCode)
}

func versionCommand(args []string) int {
	flags := flag.NewFlagSet("version", flag.ContinueOnError)
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "version accepts only --json")
		return exitInternal
	}
	info := version.Current()
	if *jsonOutput {
		return printJSON(info)
	}
	fmt.Printf("devctl %s\ncommit: %s\ndirty: %t\ngo: %s\n", info.Version, info.Commit, info.Dirty, info.GoVersion)
	return exitOK
}

func discoverCommand(args []string) int {
	flags := flag.NewFlagSet("discover", flag.ContinueOnError)
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil {
		return exitInternal
	}
	root := "."
	if flags.NArg() == 1 {
		root = flags.Arg(0)
	} else if flags.NArg() > 1 {
		fmt.Fprintln(os.Stderr, "discover accepts at most one root path")
		return exitInternal
	}
	projects, err := discovery.Discover(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitInternal
	}
	if *jsonOutput {
		return printJSON(projects)
	}
	for _, project := range projects {
		fmt.Printf("%s\t%s\n", project.Name, technologyNames(project))
	}
	return exitOK
}

func verifyCommand(args []string) int {
	startedAt := time.Now().UTC()
	agentRequested := agentFlagRequested(args)
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	jsonOutput := flags.Bool("json", false, "emit JSON")
	liveOutput := flags.Bool("live", false, "render live verification events to stderr")
	agentOutput := flags.Bool("agent", false, "emit one bounded agent result")
	if agentRequested {
		flags.SetOutput(io.Discard)
	}
	if err := flags.Parse(args); err != nil {
		if agentRequested {
			return printAgentError("invalid_arguments", err.Error(), startedAt)
		}
		return exitInternal
	}
	if *agentOutput {
		if *liveOutput || *jsonOutput {
			return printAgentError("invalid_arguments", "--agent cannot be combined with --live or --json", startedAt)
		}
		if flags.NArg() != 1 {
			return printAgentError("invalid_arguments", "verify --agent requires one project path", startedAt)
		}
		return verifyAgent(flags.Arg(0), startedAt)
	}
	if flags.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "verify requires one project path")
		return exitInternal
	}
	projectPath := flags.Arg(0)
	report, exitCode, executionErr := executeVerification(projectPath, *liveOutput)
	if executionErr != nil {
		fmt.Fprintln(os.Stderr, executionErr)
		return exitCode
	}
	_ = recordVerificationSession(report, projectPath)
	if *jsonOutput {
		if code := printJSON(report); code != exitOK {
			return code
		}
	} else {
		printReport(report)
	}
	return exitCode
}

func workerCommand(args []string) int {
	if len(args) == 0 || args[0] != "verify" {
		fmt.Fprintln(os.Stderr, "worker requires the verify operation")
		return exitInternal
	}
	flags := flag.NewFlagSet("worker verify", flag.ContinueOnError)
	requestPath := flags.String("request", "", "path to a versioned worker request JSON file")
	liveOutput := flags.Bool("live", false, "render deterministic verification events to stderr")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || strings.TrimSpace(*requestPath) == "" {
		fmt.Fprintln(os.Stderr, "worker verify requires --request <path>")
		return exitInternal
	}
	request, err := worker.ReadRequest(*requestPath)
	if err != nil {
		result := worker.RejectedResult("", "verify", "invalid_request", err.Error())
		if printJSON(result) != exitOK {
			return exitInternal
		}
		return result.ExitCode
	}
	projectEntry, resolveErr := registry.Resolve(request.ProjectID)
	if resolveErr != nil {
		code := "project_not_approved"
		if errors.Is(resolveErr, registry.ErrProjectIdentityMismatch) {
			code = "project_identity_mismatch"
		}
		result := worker.RejectedResult(request.RequestID, request.Operation, code, resolveErr.Error())
		if printJSON(result) != exitOK {
			return exitInternal
		}
		return result.ExitCode
	}
	startedAt := time.Now().UTC()
	report, exitCode, executionErr := executeVerification(projectEntry.Path, *liveOutput, request.ProjectID)
	if executionErr != nil {
		code := "verification_unavailable"
		if errors.Is(executionErr, registry.ErrActiveRun) {
			code = "active_run"
		} else if errors.Is(executionErr, registry.ErrProjectIdentityMismatch) {
			code = "project_identity_mismatch"
		}
		result := worker.RejectedResult(request.RequestID, request.Operation, code, executionErr.Error())
		if printJSON(result) != exitOK {
			return exitInternal
		}
		return result.ExitCode
	}
	finishedAt := time.Now().UTC()
	_ = recordVerificationSession(report, projectEntry.Path)
	result := worker.NewVerificationResult(request, report, exitCode, startedAt, finishedAt)
	if printJSON(result) != exitOK {
		return exitInternal
	}
	return exitCode
}

func executeVerification(projectPath string, liveOutput bool, expectedProjectIDs ...string) (model.Report, int, error) {
	return executeVerificationWithOptions(projectPath, verificationExecutionOptions{
		LiveOutput:         liveOutput,
		ExpectedProjectIDs: expectedProjectIDs,
		Diagnostics:        os.Stderr,
	})
}

type verificationExecutionOptions struct {
	LiveOutput         bool
	ExpectedProjectIDs []string
	Diagnostics        io.Writer
	OutputMetrics      *runner.OutputMetrics
}

func executeVerificationWithOptions(projectPath string, options verificationExecutionOptions) (model.Report, int, error) {
	diagnostics := options.Diagnostics
	if diagnostics == nil {
		diagnostics = io.Discard
	}
	projectEntry, registryDetectErr := registry.DetectProject(projectPath)
	if registryDetectErr != nil {
		info, statErr := os.Stat(projectPath)
		if statErr != nil || !info.IsDir() {
			return model.Report{}, exitInternal, fmt.Errorf("project path is unavailable: %s: %w", projectPath, registryDetectErr)
		}
	}
	if len(options.ExpectedProjectIDs) > 0 {
		if registryDetectErr != nil {
			return model.Report{}, exitInternal, registryDetectErr
		}
		if projectEntry.ProjectID != options.ExpectedProjectIDs[0] {
			return model.Report{}, exitInternal, fmt.Errorf("%w: registered %q, current %q", registry.ErrProjectIdentityMismatch, options.ExpectedProjectIDs[0], projectEntry.ProjectID)
		}
	}
	runID := verify.NewRunID()
	registryStarted := false
	if registryDetectErr != nil {
		fmt.Fprintf(diagnostics, "registry unavailable: %v\n", registryDetectErr)
	} else {
		if err := registry.Register(projectEntry); err != nil {
			fmt.Fprintf(diagnostics, "registry update unavailable: %v\n", err)
		}
		if err := registry.Begin(projectEntry, runID, os.Getpid()); err != nil {
			fmt.Fprintf(diagnostics, "registry run state unavailable: %v\n", err)
			if errors.Is(err, registry.ErrActiveRun) {
				return model.Report{}, exitInternal, err
			}
		} else {
			registryStarted = true
		}
	}
	var report model.Report
	if options.LiveOutput {
		recorder, recorderErr := workflow.New(projectPath)
		if recorderErr != nil {
			fmt.Fprintf(diagnostics, "live workflow journal unavailable: %v\n", recorderErr)
		}
		renderer := live.NewRenderer(os.Stderr)
		asyncRenderer := events.NewAsyncSink(renderer, 256)
		subscribers := []events.Sink{asyncRenderer}
		if recorder != nil {
			subscribers = append(subscribers, recorder)
		}
		stream := events.NewStream(subscribers...)
		report = verify.ProjectWithOptions(context.Background(), projectPath, verify.Options{Sink: stream, RunID: runID, OutputMetrics: options.OutputMetrics})
		asyncRenderer.Close()
		if recorder != nil {
			_ = recorder.Close()
		}
	} else {
		report = verify.ProjectWithOptions(context.Background(), projectPath, verify.Options{RunID: runID, OutputMetrics: options.OutputMetrics})
	}
	if registryStarted {
		if err := registry.Finish(projectEntry.ProjectID, runID, string(report.Overall)); err != nil {
			fmt.Fprintf(diagnostics, "registry completion unavailable: %v\n", err)
		}
	}
	return report, verify.ExitCode(report), nil
}

func verifyAgent(projectPath string, startedAt time.Time) int {
	metrics := &runner.OutputMetrics{}
	report, exitCode, executionErr := executeVerificationWithOptions(projectPath, verificationExecutionOptions{
		Diagnostics:   io.Discard,
		OutputMetrics: metrics,
	})
	if executionErr != nil {
		return printAgentError("verification_unavailable", executionErr.Error(), startedAt)
	}
	_ = recordVerificationSession(report, projectPath)
	snapshot := metrics.Snapshot()
	localBytes, localEvidenceErr := evidenceDirectoryBytes(projectPath, report.EvidencePath)
	result := worker.NewAgentVerificationResult(report, exitCode, startedAt, time.Now().UTC(), worker.InformationFlow{
		RawSubprocessBytes:      snapshot.RawBytes,
		RetainedSubprocessBytes: snapshot.RetainedBytes,
		LocalEvidenceBytes:      localBytes,
		LocalEvidenceMeasured:   localEvidenceErr == nil,
		OutputTruncated:         snapshot.Truncated,
	})
	return printAgentResult(result, exitCode)
}

func printAgentError(code, message string, startedAt time.Time) int {
	result := worker.RejectedAgentResult(code, message, startedAt, time.Now().UTC())
	return printAgentResult(result, exitInternal)
}

func printAgentResult(result worker.Result, exitCode int) int {
	data, err := worker.EncodeResult(result)
	if err != nil {
		fallback := worker.RejectedAgentResult("result_encoding_failed", err.Error(), result.StartedAt, time.Now().UTC())
		data, err = worker.EncodeResult(fallback)
		if err != nil {
			return exitInternal
		}
		exitCode = exitInternal
	}
	if _, err := os.Stdout.Write(data); err != nil {
		return exitInternal
	}
	return exitCode
}

func evidenceDirectoryBytes(projectPath, relativeEvidencePath string) (int64, error) {
	if strings.TrimSpace(relativeEvidencePath) == "" {
		return 0, errors.New("verification did not record an evidence path")
	}
	root := filepath.Join(projectPath, filepath.FromSlash(relativeEvidencePath))
	var total int64
	err := filepath.Walk(root, func(_ string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

func agentFlagRequested(args []string) bool {
	requested := false
	for _, argument := range args {
		if argument == "--" || !strings.HasPrefix(argument, "-") {
			break
		}
		if argument == "--agent" {
			requested = true
			continue
		}
		if strings.HasPrefix(argument, "--agent=") {
			enabled, err := strconv.ParseBool(strings.TrimPrefix(argument, "--agent="))
			if err != nil {
				return true
			}
			requested = requested || enabled
		}
	}
	return requested
}

func sessionCommand(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "session requires record, status, or resume")
		return exitInternal
	}
	switch args[0] {
	case "status", "resume":
		state, err := session.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "session unavailable: %v\n", err)
			return exitInternal
		}
		if args[0] == "resume" {
			return printResume(state)
		}
		return printJSON(state)
	case "record":
		flags := flag.NewFlagSet("session record", flag.ContinueOnError)
		project := flags.String("project", "", "project name")
		projectPath := flags.String("path", "", "project path")
		branch := flags.String("branch", "", "Git branch")
		commit := flags.String("commit", "", "last commit")
		task := flags.String("task", "", "current task")
		result := flags.String("result", "", "last result")
		evidence := flags.String("evidence", "", "last evidence path")
		ci := flags.String("ci", "", "CI state")
		decision := flags.String("decision", "", "prompt decision")
		promptDate := flags.String("prompt-date", "", "prompt date in YYYY-MM-DD format")
		if err := flags.Parse(args[1:]); err != nil {
			return exitInternal
		}
		if *project == "" {
			*project = filepath.Base(*projectPath)
		}
		state := model.SessionState{Project: *project, ProjectPath: *projectPath, Branch: *branch, LastCommit: *commit, CurrentTask: *task, LastResult: model.Status(*result), EvidencePath: *evidence, CIState: *ci, PromptDecision: *decision, PromptDate: *promptDate}
		path, err := session.Record(state)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return exitInternal
		}
		fmt.Println(path)
		return exitOK
	default:
		fmt.Fprintf(os.Stderr, "unknown session action: %s\n", args[0])
		return exitInternal
	}
}

func printResume(state model.SessionState) int {
	projectStatus := "available"
	if _, err := os.Stat(state.ProjectPath); err != nil {
		projectStatus = "missing"
	}
	stale := "current"
	if time.Since(state.UpdatedAt) > 30*24*time.Hour {
		stale = "stale"
	}
	fmt.Printf("PROJECT: %s\nPATH: %s\nSTATE: %s\nPROJECT_PATH: %s\nBRANCH: %s\nCOMMIT: %s\nLAST_RESULT: %s\nEVIDENCE: %s\nCI: %s\n", state.Project, state.ProjectPath, stale, projectStatus, state.Branch, state.LastCommit, state.LastResult, state.EvidencePath, state.CIState)
	return exitOK
}

func handoffCommand(args []string) int {
	flags := flag.NewFlagSet("handoff", flag.ContinueOnError)
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil || flags.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "handoff requires one report.json path")
		return exitInternal
	}
	packet, err := handoff.Read(flags.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitInternal
	}
	if *jsonOutput {
		return printJSON(packet)
	}
	fmt.Print(handoff.Text(packet))
	return exitOK
}

func recordVerificationSession(report model.Report, projectPath string) error {
	if report.Project == nil {
		return nil
	}
	branchResult, _ := runner.Run(context.Background(), projectPath, runner.GitBranch)
	commitResult, _ := runner.Run(context.Background(), projectPath, runner.GitCommit)
	state := model.SessionState{Project: report.Project.Name, ProjectPath: projectPath, Branch: strings.TrimSpace(branchResult.Output), LastCommit: strings.TrimSpace(commitResult.Output), LastResult: report.Overall, EvidencePath: report.EvidencePath}
	_, err := session.Record(state)
	return err
}

func printReport(report model.Report) {
	if report.Project != nil {
		fmt.Printf("PROJECT: %s\n", report.Project.Name)
		fmt.Printf("TECHNOLOGY: %s\n", technologyNames(*report.Project))
	}
	for _, check := range report.Checks {
		fmt.Printf("%-24s %s — %s\n", strings.ToUpper(check.ID), check.Status, check.Summary)
		if check.Reason != "" && check.Status == model.Error {
			fmt.Printf("  reason: %s\n", check.Reason)
		}
	}
	fmt.Printf("OVERALL: %s\n", report.Overall)
}

func printJSON(value any) int {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitInternal
	}
	return exitOK
}

func technologyNames(project model.Project) string {
	names := make([]string, 0, len(project.Technologies))
	for _, technology := range project.Technologies {
		names = append(names, technology.ID)
	}
	return strings.Join(names, ", ")
}

func usage() {
	program := filepath.Base(os.Args[0])
	fmt.Printf("Usage: %s version [--json]\n", program)
	fmt.Printf("Usage: %s discover [--json] [root]\n", program)
	fmt.Printf("       %s verify [--json] [--live] [--agent] <project>\n", program)
	fmt.Printf("       %s worker verify [--live] --request <request.json>\n", program)
	fmt.Printf("       %s session record|status|resume ...\n", program)
	fmt.Printf("       %s handoff [--json] <report.json>\n", program)
	fmt.Printf("       %s failures [--json] [--project <path>] [--offset <index>] <run-id>\n", program)
	fmt.Printf("       %s failure [--json] [--project <path>] [--offset <index>] <run-id> <check-id>\n", program)
	fmt.Printf("       %s repair [--json] [--verbose] [--proposal <proposal.json>] [--allow <path,...>] <project>\n", program)
	fmt.Printf("       %s context|status [--json] [project]\n", program)
	fmt.Printf("       %s evidence rebuild|latest [--json] <project>\n", program)
	fmt.Printf("       %s evidence [--json] [--project <path>] [--offset <bytes>] <run-id> <failure-id>\n", program)
	fmt.Printf("       %s history [--json] <project>\n", program)
	fmt.Printf("       %s lessons query|add [--json] <project>\n", program)
	fmt.Printf("       %s fixes record [--json] --input <candidate.json> <project>\n", program)
	fmt.Printf("       %s fixes list [--json] [--limit <count>] <project>\n", program)
	fmt.Printf("       %s fixes show [--json] <project> <fix-id>\n", program)
	fmt.Printf("       %s cache status|inspect|clear [--json] <project>\n", program)
}
