package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"devctl/internal/cache"
	"devctl/internal/evidence"
	"devctl/internal/insight"
	"devctl/internal/knowledge"
	"devctl/internal/repaircli"
)

func contextCommand(args []string) int { return contextLike(args, false) }
func statusCommand(args []string) int  { return contextLike(args, true) }
func contextLike(args []string, status bool) int {
	flags := flag.NewFlagSet("context", flag.ContinueOnError)
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil {
		return exitInternal
	}
	root := "."
	if flags.NArg() == 1 {
		root = flags.Arg(0)
	} else if flags.NArg() > 1 {
		fmt.Fprintln(os.Stderr, "context accepts at most one project path")
		return exitInternal
	}
	if *jsonOutput {
		data, err := insight.JSON(root)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return exitInternal
		}
		if _, err := os.Stdout.Write(data); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return exitInternal
		}
		return exitOK
	}
	value, err := insight.Build(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitInternal
	}
	if status {
		fmt.Printf("PROJECT: %s\nHEAD: %s\nBRANCH: %s\nDIRTY: %t\nLATEST_EVIDENCE: %s\nCACHE: %s (%d entries)\n", value.Project.Name, value.Repository.Head, value.Repository.Branch, value.Repository.Dirty, value.LatestEvidence, value.Cache.Validity, value.Cache.Entries)
	} else {
		fmt.Printf("PROJECT: %s\nPATH: %s\nHEAD: %s\nBRANCH: %s\nDIRTY: %t\n", value.Project.Name, value.Project.Path, value.Repository.Head, value.Repository.Branch, value.Repository.Dirty)
		for _, failure := range value.CurrentFailures {
			fmt.Printf("FAILURE: %s %s %s\n", failure.CheckID, failure.Status, failure.EvidencePath)
		}
		fmt.Printf("LESSONS: %d\nSUGGESTED_CHECKS: %s\n", len(value.RelevantLessons), strings.Join(value.SuggestedChecks, ", "))
	}
	return exitOK
}

func evidenceCommand(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "evidence requires rebuild or latest")
		return exitInternal
	}
	action := args[0]
	if action != "rebuild" && action != "latest" {
		fmt.Fprintln(os.Stderr, "evidence requires rebuild or latest")
		return exitInternal
	}
	flags := flag.NewFlagSet("evidence", flag.ContinueOnError)
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "evidence requires a project path")
		return exitInternal
	}
	root := flags.Arg(0)
	index, err := evidence.Rebuild(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitInternal
	}
	if *jsonOutput {
		return printJSON(index)
	}
	if latest := evidence.Latest(index); latest != nil {
		fmt.Printf("LATEST: %s %s %s\n", latest.RunID, latest.Overall, latest.Path)
	} else {
		fmt.Println("NO EVIDENCE")
	}
	return exitOK
}
func historyCommand(args []string) int {
	flags := flag.NewFlagSet("history", flag.ContinueOnError)
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil || flags.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "history requires a project path")
		return exitInternal
	}
	index, err := evidence.Rebuild(flags.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitInternal
	}
	if *jsonOutput {
		return printJSON(index.Runs)
	}
	for _, run := range index.Runs {
		fmt.Printf("%s %-5s %s\n", run.RunID, run.Overall, run.Path)
	}
	return exitOK
}

func lessonsCommand(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "lessons requires query or add")
		return exitInternal
	}
	action := args[0]
	flags := flag.NewFlagSet("lessons", flag.ContinueOnError)
	jsonOutput := flags.Bool("json", false, "emit JSON")
	project := flags.String("project", "", "project identity")
	check := flags.String("check", "", "check id")
	signature := flags.String("signature", "", "normalized error or signature")
	path := flags.String("path", "", "relevant path")
	adapter := flags.String("adapter", "", "adapter")
	tool := flags.String("tool", "", "tool")
	limit := flags.Int("limit", 10, "maximum records")
	problem := flags.String("problem", "", "problem text")
	rootCause := flags.String("root-cause", "", "root cause")
	solution := flags.String("solution", "", "successful or attempted solution")
	success := flags.Bool("success", false, "mark the record successful")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "lessons requires a project path")
		return exitInternal
	}
	root := flags.Arg(0)
	if action == "query" {
		records, err := knowledge.QueryLessons(root, knowledge.Query{Project: *project, Check: *check, Signature: *signature, Path: *path, Adapter: *adapter, Tool: *tool, Limit: *limit})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return exitInternal
		}
		if *jsonOutput {
			return printJSON(records)
		}
		for _, record := range records {
			fmt.Printf("%s %s %s success=%t\n", record.ID, record.Check, record.Problem, record.Success)
		}
		return exitOK
	}
	if action != "add" || strings.TrimSpace(*problem) == "" {
		fmt.Fprintln(os.Stderr, "lessons add requires --problem")
		return exitInternal
	}
	record, err := knowledge.Write(root, knowledge.Lesson{Project: *project, Check: *check, Adapter: *adapter, Tool: *tool, Problem: *problem, RootCause: *rootCause, Solution: *solution, Success: *success, Status: "RECORDED"})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitInternal
	}
	if *jsonOutput {
		return printJSON(record)
	}
	fmt.Println(record.ID)
	return exitOK
}

func cacheCommand(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "cache requires status, inspect, or clear")
		return exitInternal
	}
	action := args[0]
	if action != "status" && action != "inspect" && action != "clear" {
		fmt.Fprintln(os.Stderr, "cache requires status, inspect, or clear")
		return exitInternal
	}
	flags := flag.NewFlagSet("cache", flag.ContinueOnError)
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "cache requires a project path")
		return exitInternal
	}
	root := flags.Arg(0)
	if action == "clear" {
		if err := cache.Clear(root); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return exitInternal
		}
		if *jsonOutput {
			return printJSON(map[string]any{"status": "CLEARED"})
		}
		fmt.Println("cache cleared")
		return exitOK
	}
	entries, err := cache.List(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitInternal
	}
	if *jsonOutput {
		return printJSON(entries)
	}
	for _, entry := range entries {
		fmt.Printf("%s %s %s\n", entry.Key, entry.Kind, entry.CreatedAt.Format("2006-01-02T15:04:05Z"))
	}
	return exitOK
}

func repairCommand(args []string) int {
	flags := flag.NewFlagSet("repair", flag.ContinueOnError)
	jsonOutput := flags.Bool("json", false, "emit JSON")
	verbose := flags.Bool("verbose", false, "show technical progress")
	proposal := flags.String("proposal", "", "controlled proposal JSON path")
	worker := flags.String("worker", "controlled-cli", "proposal worker identity")
	task := flags.String("task", "repair-cli-001", "repair task id")
	allowed := flags.String("allow", "", "comma-separated allowed source paths")
	if err := flags.Parse(args); err != nil || flags.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "repair requires one project path")
		return exitInternal
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	input := io.Reader(os.Stdin)
	interactive := isInteractive(os.Stdin)
	allow := []string{}
	if strings.TrimSpace(*allowed) != "" {
		for _, value := range strings.Split(*allowed, ",") {
			if strings.TrimSpace(value) != "" {
				allow = append(allow, strings.TrimSpace(value))
			}
		}
	}
	if len(allow) == 0 && strings.TrimSpace(*proposal) == "" {
		// No source can be modified through this placeholder. It only lets a
		// PASS/WARN baseline stop before proposal or approval is needed.
		allow = []string{"__devctl_no_repair_path__"}
	}
	output, err := repaircli.Run(ctx, repaircli.Options{ProjectPath: flags.Arg(0), ProposalPath: *proposal, TaskID: *task, Worker: *worker, AllowedPaths: allow, Input: input, Output: os.Stdout, Diagnostics: os.Stderr, Interactive: interactive, Verbose: *verbose, JSON: *jsonOutput})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitInternal
	}
	return output.ExitCode
}
func isInteractive(file *os.File) bool {
	if file == nil {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
