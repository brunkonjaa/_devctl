package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"devctl/internal/events"
)

const (
	maxEventsBytes      = 1024 * 1024
	retainedEventsBytes = 768 * 1024
)

type Recorder struct {
	mu          sync.Mutex
	file        *os.File
	eventsPath  string
	currentPath string
	last        events.Event
	checks      map[string]events.Event
	eventBytes  int64
	closed      bool
}

func New(projectPath string) (*Recorder, error) {
	root := filepath.Join(projectPath, ".devctl", "workflow")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create workflow directory: %w", err)
	}
	eventsPath := filepath.Join(root, "events.jsonl")
	if err := boundEvents(eventsPath); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(eventsPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open workflow events: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("stat workflow events: %w", err)
	}
	return &Recorder{file: file, eventsPath: eventsPath, currentPath: filepath.Join(root, "current.md"), checks: make(map[string]events.Event), eventBytes: info.Size()}, nil
}

func (recorder *Recorder) Publish(event events.Event) {
	if event.EventType == events.ProcessOutput {
		return
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.closed {
		return
	}
	recorder.last = event
	if event.CheckID != "" && (event.EventType == events.CheckStarted || event.EventType == events.CheckFinished || event.EventType == events.CheckSkipped) {
		recorder.checks[event.CheckID] = event
	}
	if event.EventType != events.ProcessOutput {
		if data, err := json.Marshal(event); err == nil {
			line := append(data, '\n')
			if recorder.eventBytes+int64(len(line)) > maxEventsBytes {
				_ = recorder.rotateLocked()
			}
			if written, err := recorder.file.Write(line); err == nil {
				recorder.eventBytes += int64(written)
			}
		}
	}
	_ = writeCurrent(recorder.currentPath, recorder.last, recorder.checks)
}

func (recorder *Recorder) rotateLocked() error {
	data, err := os.ReadFile(recorder.eventsPath)
	if err != nil {
		return err
	}
	cut := 0
	if len(data) > retainedEventsBytes {
		cut = len(data) - retainedEventsBytes
		if newline := strings.IndexByte(string(data[cut:]), '\n'); newline >= 0 {
			cut += newline + 1
		}
	}
	retained := data[cut:]
	if err := recorder.file.Close(); err != nil {
		return err
	}
	if err := writeAtomic(recorder.eventsPath, retained); err != nil {
		file, reopenErr := os.OpenFile(recorder.eventsPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if reopenErr == nil {
			recorder.file = file
			recorder.eventBytes = int64(len(data))
		}
		return err
	}
	file, err := os.OpenFile(recorder.eventsPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	recorder.file = file
	recorder.eventBytes = int64(len(retained))
	return nil
}

func (recorder *Recorder) Close() error {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.closed {
		return nil
	}
	recorder.closed = true
	return recorder.file.Close()
}

func boundEvents(path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read workflow events: %w", err)
	}
	if len(data) <= maxEventsBytes {
		return nil
	}
	cut := len(data) - retainedEventsBytes
	if newline := strings.IndexByte(string(data[cut:]), '\n'); newline >= 0 {
		cut += newline + 1
	}
	return writeAtomic(path, data[cut:])
}

func writeCurrent(path string, last events.Event, checks map[string]events.Event) error {
	ids := make([]string, 0, len(checks))
	for id := range checks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var builder strings.Builder
	builder.WriteString("# devctl verification\n\n")
	fmt.Fprintf(&builder, "- Run: `%s`\n", last.RunID)
	fmt.Fprintf(&builder, "- Project: `%s`\n", last.ProjectID)
	fmt.Fprintf(&builder, "- Last event: `%s`\n", last.EventType)
	if last.Status != "" {
		fmt.Fprintf(&builder, "- Status: `%s`\n", last.Status)
	}
	builder.WriteString("\n## Checks\n\n| Check | Status | Elapsed (ms) | Message |\n|---|---:|---:|---|\n")
	for _, id := range ids {
		event := checks[id]
		fmt.Fprintf(&builder, "| `%s` | `%s` | %d | %s |\n", id, event.Status, event.ElapsedMS, strings.ReplaceAll(event.Message, "|", "\\|"))
	}
	return writeAtomic(path, []byte(builder.String()))
}

func writeAtomic(path string, data []byte) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".workflow-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
