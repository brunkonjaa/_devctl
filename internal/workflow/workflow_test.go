package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devctl/internal/events"
)

func TestRecorderPersistsLifecycleEventsButNotProcessOutput(t *testing.T) {
	root := t.TempDir()
	recorder, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	recorder.Publish(events.Event{Sequence: 1, RunID: "run-1", ProjectID: "project-1", EventType: events.ProcessOutput, CheckID: "build", Message: "secret child output"})
	recorder.Publish(events.Event{Sequence: 2, RunID: "run-1", ProjectID: "project-1", EventType: events.CheckFinished, CheckID: "build", Status: "PASS", Message: "build passed"})
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(root, ".devctl", "workflow", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret child output") {
		t.Fatalf("process output was persisted in lifecycle journal: %s", data)
	}
	var event events.Event
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &event); err != nil {
		t.Fatal(err)
	}
	if event.EventType != events.CheckFinished || event.Status != "PASS" {
		t.Fatalf("unexpected persisted lifecycle event: %#v", event)
	}
	current, err := os.ReadFile(filepath.Join(root, ".devctl", "workflow", "current.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(current), "build passed") {
		t.Fatalf("current.md did not contain latest check state: %s", current)
	}
}

func TestRecorderEnforcesEventJournalBoundDuringAppend(t *testing.T) {
	root := t.TempDir()
	recorder, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	message := strings.Repeat("x", 2048)
	for index := 0; index < 700; index++ {
		recorder.Publish(events.Event{RunID: "run-bound", ProjectID: "project-bound", EventType: events.CheckFinished, CheckID: "check", Status: "PASS", Message: message})
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(root, ".devctl", "workflow", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > maxEventsBytes {
		t.Fatalf("workflow journal exceeded bound: %d", info.Size())
	}
}
