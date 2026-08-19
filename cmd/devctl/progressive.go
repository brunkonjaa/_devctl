package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"devctl/internal/progressive"
)

type progressiveArguments struct {
	JSON        bool
	Project     string
	Offset      int64
	Positionals []string
}

func failuresCommand(args []string) int {
	return progressiveCommand("failures", args, 1, func(arguments progressiveArguments) (progressive.Result, error) {
		return progressive.Failures(arguments.Project, arguments.Positionals[0], arguments.Offset)
	})
}

func failureCommand(args []string) int {
	return progressiveCommand("failure", args, 2, func(arguments progressiveArguments) (progressive.Result, error) {
		return progressive.FailureDetail(arguments.Project, arguments.Positionals[0], arguments.Positionals[1], arguments.Offset)
	})
}

func progressiveEvidenceCommand(args []string) int {
	return progressiveCommand("evidence", args, 2, func(arguments progressiveArguments) (progressive.Result, error) {
		return progressive.SelectedEvidence(arguments.Project, arguments.Positionals[0], arguments.Positionals[1], arguments.Offset)
	})
}

func progressiveCommand(operation string, args []string, positionalCount int, run func(progressiveArguments) (progressive.Result, error)) int {
	jsonRequested := progressiveJSONRequested(args)
	arguments, err := parseProgressiveArguments(args)
	if err != nil {
		return printProgressiveError(operation, jsonRequested, "invalid_arguments", err.Error())
	}
	if len(arguments.Positionals) != positionalCount {
		return printProgressiveError(operation, arguments.JSON, "invalid_arguments", progressiveUsage(operation))
	}
	result, err := run(arguments)
	if err != nil {
		return printProgressiveError(operation, arguments.JSON, progressive.ErrorCode(err), err.Error())
	}
	if arguments.JSON {
		return printProgressiveJSON(result, result.ExitCode)
	}
	printProgressiveText(result)
	return result.ExitCode
}

func parseProgressiveArguments(args []string) (progressiveArguments, error) {
	result := progressiveArguments{Project: "."}
	projectSet := false
	offsetSet := false
	for index := 0; index < len(args); index++ {
		argument := args[index]
		switch {
		case argument == "--":
			result.Positionals = append(result.Positionals, args[index+1:]...)
			return result, nil
		case argument == "--json":
			result.JSON = true
		case strings.HasPrefix(argument, "--json="):
			value, err := strconv.ParseBool(strings.TrimPrefix(argument, "--json="))
			if err != nil {
				return progressiveArguments{}, fmt.Errorf("invalid --json value")
			}
			result.JSON = value
		case argument == "--project":
			if projectSet || index+1 >= len(args) {
				return progressiveArguments{}, fmt.Errorf("--project requires one value")
			}
			index++
			result.Project = args[index]
			projectSet = true
		case strings.HasPrefix(argument, "--project="):
			if projectSet {
				return progressiveArguments{}, fmt.Errorf("--project may be provided once")
			}
			result.Project = strings.TrimPrefix(argument, "--project=")
			projectSet = true
		case argument == "--offset":
			if offsetSet || index+1 >= len(args) {
				return progressiveArguments{}, fmt.Errorf("--offset requires one value")
			}
			index++
			value, err := strconv.ParseInt(args[index], 10, 64)
			if err != nil {
				return progressiveArguments{}, fmt.Errorf("invalid --offset value")
			}
			result.Offset = value
			offsetSet = true
		case strings.HasPrefix(argument, "--offset="):
			if offsetSet {
				return progressiveArguments{}, fmt.Errorf("--offset may be provided once")
			}
			value, err := strconv.ParseInt(strings.TrimPrefix(argument, "--offset="), 10, 64)
			if err != nil {
				return progressiveArguments{}, fmt.Errorf("invalid --offset value")
			}
			result.Offset = value
			offsetSet = true
		case strings.HasPrefix(argument, "-"):
			return progressiveArguments{}, fmt.Errorf("unknown option %s", argument)
		default:
			result.Positionals = append(result.Positionals, argument)
		}
	}
	if strings.TrimSpace(result.Project) == "" {
		return progressiveArguments{}, fmt.Errorf("--project must not be empty")
	}
	return result, nil
}

func progressiveJSONRequested(args []string) bool {
	for _, argument := range args {
		if argument == "--json" {
			return true
		}
		if strings.HasPrefix(argument, "--json=") {
			value, err := strconv.ParseBool(strings.TrimPrefix(argument, "--json="))
			return err != nil || value
		}
	}
	return false
}

func printProgressiveError(operation string, jsonOutput bool, code, message string) int {
	if !jsonOutput {
		fmt.Fprintln(os.Stderr, message)
		return exitInternal
	}
	return printProgressiveJSON(progressive.Rejected(operation, code, message), exitInternal)
}

func printProgressiveJSON(result progressive.Result, exitCode int) int {
	data, err := progressive.Encode(result)
	if err != nil {
		fallback := progressive.Rejected(result.Operation, "result_encoding_failed", err.Error())
		data, err = progressive.Encode(fallback)
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

func printProgressiveText(result progressive.Result) {
	result = progressive.Sanitize(result)
	switch result.Operation {
	case "failures":
		fmt.Printf("RUN: %s\nOVERALL: %s\n", result.RunID, result.Overall)
		for _, failure := range result.Failures {
			fmt.Printf("%s\t%s\t%s\n", failure.CheckID, failure.Status, failure.Summary)
		}
		if len(result.Failures) == 0 {
			fmt.Println("NO FAILURES")
		}
	case "failure":
		failure := result.Failure
		if failure == nil {
			return
		}
		fmt.Printf("RUN: %s\nCHECK: %s\nSTATUS: %s\nSUMMARY: %s\n", result.RunID, failure.CheckID, failure.Status, failure.Summary)
		if failure.Reason != "" {
			fmt.Printf("REASON: %s\n", failure.Reason)
		}
		for _, finding := range failure.Findings {
			fmt.Printf("FINDING: %s %s %s\n", finding.FindingID, finding.Severity, finding.Issue)
		}
	case "evidence":
		if result.Evidence != nil {
			fmt.Print(result.Evidence.Content)
		}
	}
}

func progressiveUsage(operation string) string {
	switch operation {
	case "failures":
		return "failures requires one run ID"
	case "failure":
		return "failure requires one run ID and check ID"
	default:
		return "evidence retrieval requires one run ID and failure ID"
	}
}
