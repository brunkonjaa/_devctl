package knowledge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteQueryAndDeduplicatesLessons(t *testing.T) {
	root := t.TempDir()
	first := Lesson{Project: "project-a", Check: "go-test", Problem: "CGO disabled / gcc missing", RootCause: "compiler unavailable", Solution: "Use controlled CGO environment", Success: true}
	if _, err := Write(root, first); err != nil {
		t.Fatal(err)
	}
	first.Solution = "same normalized failure, verified fix"
	if _, err := Write(root, first); err != nil {
		t.Fatal(err)
	}
	results, err := QueryLessons(root, Query{Project: "project-a", Check: "go-test", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].Success || results[0].Signature == "" {
		t.Fatalf("unexpected lessons: %+v", results)
	}
}

func TestFailedAttemptIsRetainedSeparately(t *testing.T) {
	root := t.TempDir()
	if _, err := Write(root, Lesson{Project: "p", Problem: "build fails", RootCause: "bad flag", Attempt: "remove unrelated flag", Success: false}); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(root, Lesson{Project: "p", Problem: "build fails", RootCause: "bad flag", Attempt: "use the supported flag", Solution: "supported flag", Success: true}); err != nil {
		t.Fatal(err)
	}
	results, err := QueryLessons(root, Query{Project: "p", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected failed and successful records, got %d", len(results))
	}
}

func TestMalformedLessonRejected(t *testing.T) {
	root := t.TempDir()
	path := Path(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"schema_version":"1","lessons":[{"problem":"ok","unknown":true}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(root); err == nil || !strings.Contains(err.Error(), "parse lessons") {
		t.Fatalf("expected malformed record rejection, got %v", err)
	}
}
