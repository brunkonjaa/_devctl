//go:build windows

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"time"
)

type processRecord struct {
	StartedAt string `json:"started_at"`
	RunnerPID int    `json:"runner_pid"`
	ChildPID  int    `json:"child_pid"`
}

type resultRecord struct {
	FinishedAt    string `json:"finished_at"`
	RunnerPID     int    `json:"runner_pid"`
	ChildPID      int    `json:"child_pid"`
	ChildExitCode int    `json:"child_exit_code"`
	WaitError     string `json:"wait_error,omitempty"`
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".visible-runner-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func main() {
	// The visible child must receive Ctrl+C. The sidecar must survive it long
	// enough to wait for the child and write the real child exit code.
	signal.Ignore(os.Interrupt)

	stdoutPath := flag.String("stdout", "", "capture child stdout here while relaying it to the terminal")
	stderrPath := flag.String("stderr", "", "capture child stderr here while relaying it to the terminal")
	runRecordPath := flag.String("run-record", "", "write child PID and exit evidence here")
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 || *stdoutPath == "" || *stderrPath == "" || *runRecordPath == "" {
		fmt.Fprintln(os.Stderr, "usage: visible-runner --stdout FILE --stderr FILE --run-record FILE -- EXECUTABLE [ARGS ...]")
		os.Exit(125)
	}

	stdoutFile, err := os.Create(*stdoutPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "visible runner: create stdout capture: %v\n", err)
		os.Exit(125)
	}
	defer stdoutFile.Close()
	stderrFile, err := os.Create(*stderrPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "visible runner: create stderr capture: %v\n", err)
		os.Exit(125)
	}
	defer stderrFile.Close()

	child := exec.Command(args[0], args[1:]...)
	// Keep the real visible terminal input. The runner never writes to stdin.
	child.Stdin = os.Stdin
	child.Stdout = io.MultiWriter(os.Stdout, stdoutFile)
	child.Stderr = io.MultiWriter(os.Stderr, stderrFile)
	if err := child.Start(); err != nil {
		result := resultRecord{
			FinishedAt:    time.Now().UTC().Format(time.RFC3339Nano),
			RunnerPID:     os.Getpid(),
			ChildExitCode: 125,
			WaitError:     err.Error(),
		}
		_ = writeJSON(*runRecordPath, result)
		fmt.Fprintf(os.Stderr, "visible runner: start child: %v\n", err)
		os.Exit(125)
	}

	if err := writeJSON(*runRecordPath, processRecord{
		StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
		RunnerPID: os.Getpid(),
		ChildPID:  child.Process.Pid,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "visible runner: write start record: %v\n", err)
		_ = child.Process.Kill()
		_ = child.Wait()
		os.Exit(125)
	}

	waitErr := child.Wait()
	code := 0
	if waitErr != nil {
		code = child.ProcessState.ExitCode()
		if code < 0 {
			code = 125
		}
	}
	result := resultRecord{
		FinishedAt:    time.Now().UTC().Format(time.RFC3339Nano),
		RunnerPID:     os.Getpid(),
		ChildPID:      child.Process.Pid,
		ChildExitCode: code,
	}
	if waitErr != nil {
		result.WaitError = waitErr.Error()
	}
	if err := writeJSON(*runRecordPath, result); err != nil {
		fmt.Fprintf(os.Stderr, "visible runner: write result record: %v\n", err)
		os.Exit(125)
	}
	os.Exit(code)
}
