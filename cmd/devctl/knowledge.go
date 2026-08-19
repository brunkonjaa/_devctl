package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"devctl/internal/knowledgevault"
)

func knowledgeCommand(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "knowledge requires candidate, review, supersede, promote, rebuild, search, list, or show")
		return exitInternal
	}
	switch args[0] {
	case "candidate":
		return knowledgeCandidateCommand(args[1:])
	case "review":
		return knowledgeReviewCommand(args[1:])
	case "supersede":
		return knowledgeSupersedeCommand(args[1:])
	case "promote":
		return knowledgePromoteCommand(args[1:])
	case "rebuild":
		return knowledgeRebuildCommand(args[1:])
	case "search":
		return knowledgeSearchCommand(args[1:])
	case "list":
		return knowledgeListCommand(args[1:])
	case "show":
		return knowledgeShowCommand(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown knowledge action: %s\n", args[0])
		return exitInternal
	}
}

func knowledgeCandidateCommand(args []string) int {
	flags := flag.NewFlagSet("knowledge candidate", flag.ContinueOnError)
	input := flags.String("input", "", "lesson draft JSON path")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil || flags.NArg() != 1 || *input == "" {
		fmt.Fprintln(os.Stderr, "knowledge candidate requires --input <draft.json> <project>")
		return exitInternal
	}
	data, err := readBoundedNormalFile(*input, knowledgevault.MaxDraftBytes)
	if err != nil {
		return knowledgeError(err)
	}
	draft, err := knowledgevault.DecodeDraft(data)
	if err != nil {
		return knowledgeError(err)
	}
	lesson, err := knowledgevault.CreateCandidate(flags.Arg(0), draft)
	if err != nil {
		return knowledgeError(err)
	}
	return knowledgeResult(lesson, *jsonOutput, "created lesson %s (%s)\n", lesson.ID, lesson.Status)
}

func knowledgeReviewCommand(args []string) int {
	flags := flag.NewFlagSet("knowledge review", flag.ContinueOnError)
	id := flags.String("id", "", "lesson machine ID")
	reviewer := flags.String("reviewer", "", "reviewer identity")
	note := flags.String("note", "", "review note")
	approve := flags.Bool("approve", false, "approve objective evidence")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil || flags.NArg() != 1 || *id == "" || *reviewer == "" {
		fmt.Fprintln(os.Stderr, "knowledge review requires --id <id> --reviewer <name> [--approve] <project>")
		return exitInternal
	}
	lesson, err := knowledgevault.ReviewLesson(flags.Arg(0), *id, knowledgevault.Review{Reviewer: *reviewer, Approve: *approve, Note: *note})
	if err != nil {
		return knowledgeError(err)
	}
	return knowledgeResult(lesson, *jsonOutput, "reviewed lesson %s (%s)\n", lesson.ID, lesson.Status)
}

func knowledgeSupersedeCommand(args []string) int {
	flags := flag.NewFlagSet("knowledge supersede", flag.ContinueOnError)
	id := flags.String("id", "", "lesson machine ID")
	reviewer := flags.String("reviewer", "", "reviewer identity")
	note := flags.String("note", "", "supersession note")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil || flags.NArg() != 1 || *id == "" || *reviewer == "" {
		fmt.Fprintln(os.Stderr, "knowledge supersede requires --id <id> --reviewer <name> <project>")
		return exitInternal
	}
	lesson, err := knowledgevault.Supersede(flags.Arg(0), *id, *reviewer, *note)
	if err != nil {
		return knowledgeError(err)
	}
	return knowledgeResult(lesson, *jsonOutput, "superseded lesson %s\n", lesson.ID)
}

func knowledgePromoteCommand(args []string) int {
	flags := flag.NewFlagSet("knowledge promote", flag.ContinueOnError)
	id := flags.String("id", "", "local lesson machine ID")
	globalRoot := flags.String("global-root", "", "global knowledge repository root")
	reviewer := flags.String("reviewer", "", "reviewer identity")
	note := flags.String("note", "", "promotion note")
	approve := flags.Bool("approve", false, "approve explicit global promotion")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil || flags.NArg() != 1 || *id == "" || *globalRoot == "" || *reviewer == "" || !*approve {
		fmt.Fprintln(os.Stderr, "knowledge promote requires --id <id> --global-root <root> --reviewer <name> --approve <project>")
		return exitInternal
	}
	lesson, err := knowledgevault.Promote(flags.Arg(0), *globalRoot, *id, knowledgevault.PromotionApproval{Reviewer: *reviewer, Approve: true, Note: *note})
	if err != nil {
		return knowledgeError(err)
	}
	return knowledgeResult(lesson, *jsonOutput, "promoted lesson %s\n", lesson.ID)
}

func knowledgeRebuildCommand(args []string) int {
	flags := flag.NewFlagSet("knowledge rebuild", flag.ContinueOnError)
	global := flags.Bool("global", false, "rebuild the global index")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil || flags.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "knowledge rebuild requires [--global] <root>")
		return exitInternal
	}
	scope := knowledgevault.ScopeProject
	if *global {
		scope = knowledgevault.ScopeGlobal
	}
	index, err := knowledgevault.RebuildIndex(flags.Arg(0), scope)
	if err != nil {
		return knowledgeError(err)
	}
	return knowledgeResult(index, *jsonOutput, "rebuilt %s lesson index (%d entries)\n", scope, len(index.Lessons))
}

func knowledgeListCommand(args []string) int {
	flags := flag.NewFlagSet("knowledge list", flag.ContinueOnError)
	global := flags.Bool("global", false, "list global lessons")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil || flags.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "knowledge list requires [--global] <root>")
		return exitInternal
	}
	scope := knowledgevault.ScopeProject
	if *global {
		scope = knowledgevault.ScopeGlobal
	}
	lessons, err := knowledgevault.List(flags.Arg(0), scope)
	if err != nil {
		return knowledgeError(err)
	}
	if *jsonOutput {
		return printJSON(lessons)
	}
	for _, lesson := range lessons {
		fmt.Printf("%s %s %s\n", lesson.ID, lesson.Status, lesson.Title)
	}
	return exitOK
}

func knowledgeSearchCommand(args []string) int {
	flags := flag.NewFlagSet("knowledge search", flag.ContinueOnError)
	projectRoot := flags.String("project-root", ".", "project root whose local lessons are searched")
	globalRoot := flags.String("global-root", "", "global knowledge root; defaults to project root")
	check := flags.String("check", "", "check ID filter")
	failure := flags.String("failure", "", "failure ID filter")
	technology := flags.String("technology", "", "technology filter")
	version := flags.String("version", "", "version filter")
	platform := flags.String("platform", "", "platform filter")
	tag := flags.String("tag", "", "tag filter")
	adapter := flags.String("adapter", "", "adapter filter")
	path := flags.String("path", "", "affected path filter")
	symptom := flags.String("symptom", "", "symptom filter")
	history := flags.Bool("include-history", false, "include non-current and non-VERIFIED lifecycle records")
	limit := flags.Int("limit", 10, "maximum results")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil || flags.NArg() > 1 {
		fmt.Fprintln(os.Stderr, "knowledge search accepts [flags] [query]")
		return exitInternal
	}
	queryText := ""
	if flags.NArg() == 1 {
		queryText = flags.Arg(0)
	}
	results, err := knowledgevault.Search(*projectRoot, *globalRoot, knowledgevault.SearchQuery{
		Text: queryText, CheckID: *check, FailureID: *failure, Technology: *technology,
		Version: *version, Platform: *platform, Tag: *tag, Adapter: *adapter, Path: *path,
		Symptom: *symptom, IncludeHistory: *history, Limit: *limit,
	})
	if err != nil {
		return knowledgeError(err)
	}
	if *jsonOutput {
		data, err := knowledgevault.MarshalSearchJSON(results)
		if err != nil {
			return knowledgeError(err)
		}
		if _, err := os.Stdout.Write(data); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return exitInternal
		}
		return exitOK
	}
	for _, result := range results.Results {
		fmt.Printf("%s %s %s score=%d %s\n", result.ID, result.DisplayID, result.Status, result.Score, result.Title)
	}
	return exitOK
}

func knowledgeShowCommand(args []string) int {
	flags := flag.NewFlagSet("knowledge show", flag.ContinueOnError)
	global := flags.Bool("global", false, "show a global lesson")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil || flags.NArg() < 1 || flags.NArg() > 2 {
		fmt.Fprintln(os.Stderr, "knowledge show requires [--global] [<root>] <lesson-id>")
		return exitInternal
	}
	root := "."
	identifier := flags.Arg(0)
	if flags.NArg() == 2 {
		root = flags.Arg(0)
		identifier = flags.Arg(1)
	}
	scope := knowledgevault.ScopeProject
	if *global {
		scope = knowledgevault.ScopeGlobal
	}
	lesson, err := knowledgevault.ReadIdentifier(root, scope, identifier)
	if err != nil {
		return knowledgeError(err)
	}
	if *jsonOutput {
		return printJSON(lesson)
	}
	fmt.Printf("%s %s %s\n", lesson.ID, lesson.Status, lesson.Title)
	return exitOK
}

func knowledgeResult(value any, jsonOutput bool, format string, args ...any) int {
	if jsonOutput {
		return printJSON(value)
	}
	fmt.Printf(format, args...)
	return exitOK
}

func knowledgeError(err error) int {
	if err == nil {
		err = errors.New("knowledge operation failed")
	}
	fmt.Fprintln(os.Stderr, err)
	return exitInternal
}

func readBoundedNormalFile(path string, maximum int) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("input is not a normal file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, int64(maximum)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maximum {
		return nil, fmt.Errorf("input exceeds the %d-byte limit", maximum)
	}
	return data, nil
}
