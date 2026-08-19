package live

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"devctl/internal/events"
)

func TestRendererLabelsPolicyEvidenceAndProvenance(t *testing.T) {
	var output bytes.Buffer
	renderer := NewRenderer(&output)
	stamp := time.Date(2026, time.August, 18, 17, 0, 0, 0, time.UTC)
	renderer.Publish(events.Event{Timestamp: stamp, ProjectID: "project", EventType: events.VerificationStarted, Message: "verification started"})
	renderer.Publish(events.Event{Timestamp: stamp.Add(time.Second), CheckID: "android-coverage", EventType: events.CheckEvidence, Status: "INFO", Message: "Coverage evidence collected: 39.7% line coverage"})
	renderer.Publish(events.Event{Timestamp: stamp.Add(2 * time.Second), CheckID: "android-coverage", EventType: events.CheckFinished, Status: "FAIL", Message: "Coverage 39.7% is below blocking threshold 70%"})
	renderer.Publish(events.Event{Timestamp: stamp.Add(3 * time.Second), CheckID: "provenance", EventType: events.ProcessStarted, Executable: "git", Arguments: []string{"rev-parse", "HEAD"}})
	renderer.Publish(events.Event{Timestamp: stamp.Add(3 * time.Second), CheckID: "provenance", EventType: events.ProcessOutput, Message: "a4308fe\n"})

	text := output.String()
	for _, expected := range []string{
		"INFO      android-coverage   Coverage evidence collected: 39.7% line coverage",
		"FAIL      android-coverage   Coverage 39.7% is below blocking threshold 70%",
		"PROCESS   provenance         git rev-parse HEAD",
		"[provenance] a4308fe",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("renderer output missing %q:\n%s", expected, text)
		}
	}
}
