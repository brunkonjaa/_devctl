package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"devctl/internal/discovery"
	"devctl/internal/model"
	"devctl/internal/verify"
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
	case "discover":
		exitCode = discoverCommand(os.Args[2:])
	case "verify":
		exitCode = verifyCommand(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
		exitCode = exitInternal
	}
	os.Exit(exitCode)
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
	if err := flags.Parse(args); err != nil {
		return exitInternal
	}
	if flags.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "verify requires one project path")
		return exitInternal
	}
	report := verify.Project(context.Background(), flags.Arg(0))
	if *jsonOutput {
		if code := printJSON(report); code != exitOK {
			return code
		}
	} else {
		printReport(report)
	}
	return verify.ExitCode(report)
}

func printReport(report model.Report) {
	if report.Project != nil {
		fmt.Printf("PROJECT: %s\n", report.Project.Name)
		fmt.Printf("TECHNOLOGY: %s\n", technologyNames(*report.Project))
	}
	for _, check := range report.Checks {
		fmt.Printf("%-24s %s — %s\n", strings.ToUpper(check.ID), check.Status, check.Summary)
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
	fmt.Printf("Usage: %s discover [--json] [root]\n", program)
	fmt.Printf("       %s verify [--json] <project>\n", program)
}
