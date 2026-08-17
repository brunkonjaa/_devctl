package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
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
	case "session":
		exitCode = sessionCommand(os.Args[2:])
	case "handoff":
		exitCode = handoffCommand(os.Args[2:])
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
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	jsonOutput := flags.Bool("json", false, "emit JSON")
	liveOutput := flags.Bool("live", false, "render live verification events to stderr")
	if err := flags.Parse(args); err != nil {
		return exitInternal
	}
	if flags.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "verify requires one project path")
		return exitInternal
	}
	projectPath := flags.Arg(0)
	projectEntry, registryDetectErr := registry.DetectProject(projectPath)
	runID := verify.NewRunID()
	registryStarted := false
	if registryDetectErr != nil {
		fmt.Fprintf(os.Stderr, "registry unavailable: %v\n", registryDetectErr)
	} else {
		if err := registry.Register(projectEntry); err != nil {
			fmt.Fprintf(os.Stderr, "registry update unavailable: %v\n", err)
		}
		if err := registry.Begin(projectEntry, runID, os.Getpid()); err != nil {
			fmt.Fprintf(os.Stderr, "registry run state unavailable: %v\n", err)
			if errors.Is(err, registry.ErrActiveRun) {
				return exitInternal
			}
		} else {
			registryStarted = true
		}
	}
	var report model.Report
	if *liveOutput {
		recorder, recorderErr := workflow.New(projectPath)
		if recorderErr != nil {
			fmt.Fprintf(os.Stderr, "live workflow journal unavailable: %v\n", recorderErr)
		}
		renderer := live.NewRenderer(os.Stderr)
		asyncRenderer := events.NewAsyncSink(renderer, 256)
		subscribers := []events.Sink{asyncRenderer}
		if recorder != nil {
			subscribers = append(subscribers, recorder)
		}
		stream := events.NewStream(subscribers...)
		report = verify.ProjectWithOptions(context.Background(), projectPath, verify.Options{Sink: stream, RunID: runID})
		asyncRenderer.Close()
		if recorder != nil {
			_ = recorder.Close()
		}
	} else {
		report = verify.ProjectWithOptions(context.Background(), projectPath, verify.Options{RunID: runID})
	}
	if registryStarted {
		if err := registry.Finish(projectEntry.ProjectID, runID, string(report.Overall)); err != nil {
			fmt.Fprintf(os.Stderr, "registry completion unavailable: %v\n", err)
		}
	}
	_ = recordVerificationSession(report, projectPath)
	if *jsonOutput {
		if code := printJSON(report); code != exitOK {
			return code
		}
	} else {
		printReport(report)
	}
	return verify.ExitCode(report)
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
	fmt.Printf("       %s verify [--json] [--live] <project>\n", program)
	fmt.Printf("       %s session record|status|resume ...\n", program)
	fmt.Printf("       %s handoff [--json] <report.json>\n", program)
}
