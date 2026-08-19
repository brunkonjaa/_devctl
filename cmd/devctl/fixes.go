package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"devctl/internal/fixrecord"
)

func fixesCommand(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "fixes requires record, list, or show")
		return exitInternal
	}
	switch args[0] {
	case "record":
		return fixesRecordCommand(args[1:])
	case "list":
		return fixesListCommand(args[1:])
	case "show":
		return fixesShowCommand(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown fixes action: %s\n", args[0])
		return exitInternal
	}
}

func fixesRecordCommand(args []string) int {
	flags := flag.NewFlagSet("fixes record", flag.ContinueOnError)
	jsonOutput := flags.Bool("json", false, "emit JSON")
	input := flags.String("input", "", "Fix Record candidate JSON")
	if err := flags.Parse(args); err != nil || flags.NArg() != 1 || strings.TrimSpace(*input) == "" {
		fmt.Fprintln(os.Stderr, "fixes record requires --input <candidate.json> and one project path")
		return exitInternal
	}
	project := flags.Arg(0)
	candidate, err := fixrecord.ReadCandidateFile(*input)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitInternal
	}
	record, err := fixrecord.Create(project, candidate, fixrecord.Options{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitInternal
	}
	if *jsonOutput {
		return printJSON(record)
	}
	fmt.Printf("FIX RECORD: %s\nSTATUS: %s\nPROJECT: %s\nPRE-FIX RUN: %s\nPOST-FIX RUN: %s\nPATH: %s\n", record.ID, record.Status, record.ProjectID, record.PreRun.RunID, record.PostRun.RunID, fixrecord.Path(project, record.ID))
	return exitOK
}

func fixesListCommand(args []string) int {
	flags := flag.NewFlagSet("fixes list", flag.ContinueOnError)
	jsonOutput := flags.Bool("json", false, "emit JSON")
	limit := flags.Int("limit", 20, "maximum records to return")
	if err := flags.Parse(args); err != nil || flags.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "fixes list requires one project path")
		return exitInternal
	}
	records, err := fixrecord.List(flags.Arg(0), *limit)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitInternal
	}
	if *jsonOutput {
		return printJSON(records)
	}
	if len(records) == 0 {
		fmt.Println("No Fix Records found.")
		return exitOK
	}
	for _, record := range records {
		fmt.Printf("%s\t%s\t%s\t%s\n", record.ID, record.Status, record.RecordedAt.Format(time.RFC3339Nano), record.Title)
	}
	return exitOK
}

func fixesShowCommand(args []string) int {
	flags := flag.NewFlagSet("fixes show", flag.ContinueOnError)
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil || flags.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "fixes show requires one project path and one Fix Record ID")
		return exitInternal
	}
	record, err := fixrecord.Show(flags.Arg(0), flags.Arg(1))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitInternal
	}
	if *jsonOutput {
		return printJSON(record)
	}
	fmt.Printf("FIX RECORD: %s\nSTATUS: %s\nRECORDED: %s\nPROJECT: %s\nTITLE: %s\nPROBLEM: %s\nROOT CAUSE: %s\nFINAL FIX: %s\nPRE-FIX RUN: %s\nPOST-FIX RUN: %s\nHASH: %s\n", record.ID, record.Status, record.RecordedAt.Format(time.RFC3339Nano), record.ProjectID, record.Title, record.Problem, record.RootCause, record.FinalFix, record.PreRun.RunID, record.PostRun.RunID, record.RecordHash)
	return exitOK
}
