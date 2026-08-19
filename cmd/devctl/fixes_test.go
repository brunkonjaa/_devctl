package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devctl/internal/evidence"
	"devctl/internal/fixrecord"
	"devctl/internal/gitstate"
	"devctl/internal/model"
)

func TestFixesCommandRecordsListsAndShowsVerifiedFix(t *testing.T) {
	root, input := writeFixesCLIFixture(t)
	stdout, stderr := captureStreams(t, func() int {
		return fixesCommand([]string{"record", "--json", "--input", input, root})
	})
	if len(stderr) != 0 {
		t.Fatalf("record wrote to stderr: %s", stderr)
	}
	var record fixrecord.Record
	if err := json.Unmarshal(stdout, &record); err != nil {
		t.Fatalf("record output is not JSON: %v\n%s", err, stdout)
	}
	if record.Status != fixrecord.StatusVerified || record.ID != "FIX-CLI-0001" {
		t.Fatalf("unexpected record output: %+v", record)
	}

	stdout, stderr = captureStreams(t, func() int {
		return fixesCommand([]string{"list", "--json", "--limit", "10", root})
	})
	if len(stderr) != 0 {
		t.Fatalf("list wrote to stderr: %s", stderr)
	}
	var summaries []fixrecord.Summary
	if err := json.Unmarshal(stdout, &summaries); err != nil || len(summaries) != 1 || summaries[0].ID != record.ID {
		t.Fatalf("unexpected list result: records=%+v err=%v", summaries, err)
	}

	stdout, stderr = captureStreams(t, func() int {
		return fixesCommand([]string{"show", "--json", root, record.ID})
	})
	if len(stderr) != 0 {
		t.Fatalf("show wrote to stderr: %s", stderr)
	}
	var shown fixrecord.Record
	if err := json.Unmarshal(stdout, &shown); err != nil || shown.RecordHash != record.RecordHash {
		t.Fatalf("unexpected show result: record=%+v err=%v", shown, err)
	}
}

func TestFixesCommandRejectsInvalidActionAndCandidate(t *testing.T) {
	root := t.TempDir()
	for _, args := range [][]string{
		{},
		{"unknown", root},
		{"record", root},
		{"show", root},
		{"list", "--limit", "0", root},
	} {
		_, _ = captureFixesStreams(t, func() int {
			if code := fixesCommand(args); code != exitInternal {
				t.Fatalf("expected exit %d for %v, got %d", exitInternal, args, code)
			}
			return exitInternal
		})
	}
}

func captureFixesStreams(t *testing.T, run func() int) ([]byte, []byte) {
	t.Helper()
	oldStdout, oldStderr := os.Stdout, os.Stderr
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = stdoutWriter, stderrWriter
	stdoutChannel := make(chan []byte, 1)
	stderrChannel := make(chan []byte, 1)
	go func() {
		data, _ := io.ReadAll(stdoutReader)
		stdoutChannel <- data
	}()
	go func() {
		data, _ := io.ReadAll(stderrReader)
		stderrChannel <- data
	}()
	_ = run()
	_ = stdoutWriter.Close()
	_ = stderrWriter.Close()
	os.Stdout, os.Stderr = oldStdout, oldStderr
	stdout, stderr := <-stdoutChannel, <-stderrChannel
	_ = stdoutReader.Close()
	_ = stderrReader.Close()
	return bytes.TrimSpace(stdout), stderr
}

func writeFixesCLIFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module fixture\n\ngo 1.25\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fingerprint, err := gitstate.Fingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	project := model.Project{Name: "fixture", Path: root, Identity: "cli-project", Technologies: []model.Technology{{ID: "go", Confidence: "high"}}}
	base := time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)
	pre := model.Report{SchemaVersion: "1", Command: "verify", RunID: "cli-before", PolicyVersion: "policy-v1", DevctlVersion: "0.1.0", DevctlCommit: "devctl-before", RepositoryRevision: "revision-before", RepositoryFingerprint: strings.Repeat("1", 64), StartedAt: base, FinishedAt: base.Add(time.Minute), Project: &project, Overall: model.Fail, Checks: []model.CheckResult{{ID: "go-test", CheckVersion: "go-v1", Status: model.Fail, Blocking: true, Summary: "failed"}}}
	post := model.Report{SchemaVersion: "1", Command: "verify", RunID: "cli-after", PolicyVersion: "policy-v1", DevctlVersion: "0.1.0", DevctlCommit: "devctl-after", RepositoryRevision: "revision-after", RepositoryFingerprint: fingerprint, StartedAt: base.Add(2 * time.Minute), FinishedAt: base.Add(3 * time.Minute), Project: &project, Overall: model.Pass, Checks: []model.CheckResult{{ID: "go-test", CheckVersion: "go-v1", Status: model.Pass, Summary: "passed"}}}
	if _, err := evidence.Write(root, pre); err != nil {
		t.Fatal(err)
	}
	if _, err := evidence.Write(root, post); err != nil {
		t.Fatal(err)
	}
	candidate := fixrecord.Candidate{SchemaVersion: fixrecord.CandidateSchemaVersion, ID: "FIX-CLI-0001", Title: "Fix Go test failure", ProjectID: project.Identity, Problem: "Go tests failed.", Symptoms: []string{"go-test failed"}, RootCause: "The fixture returned the wrong value.", AffectedFiles: []string{"main.go"}, AffectedComponents: []string{"go tests"}, FinalFix: "Correct the fixture value.", PreRunID: pre.RunID, PostRunID: post.RunID, CheckIDs: []string{"go-test"}, Applicability: "This project state.", Tags: []string{"go"}}
	data, err := json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(t.TempDir(), "candidate.json")
	if err := os.WriteFile(input, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return root, input
}
