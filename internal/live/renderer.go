package live

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"devctl/internal/events"
)

type Renderer struct {
	mu    sync.Mutex
	out   io.Writer
	start time.Time
}

func NewRenderer(out io.Writer) *Renderer {
	return &Renderer{out: out}
}

func (renderer *Renderer) Publish(event events.Event) {
	renderer.mu.Lock()
	defer renderer.mu.Unlock()
	if renderer.start.IsZero() {
		renderer.start = event.Timestamp
		fmt.Fprintf(renderer.out, "DEVCTL VERIFY — %s\n\n", event.ProjectID)
	}
	elapsed := event.Timestamp.Sub(renderer.start)
	if elapsed < 0 {
		elapsed = 0
	}
	stamp := elapsed.Round(time.Millisecond)
	switch event.EventType {
	case events.VerificationStarted:
		fmt.Fprintf(renderer.out, "%s  START     verification       %s\n", formatDuration(stamp), event.Message)
	case events.CheckStarted:
		fmt.Fprintf(renderer.out, "%s  START     %-18s %s\n", formatDuration(stamp), event.CheckID, event.Message)
	case events.ProcessStarted:
		fmt.Fprintf(renderer.out, "%s  PROCESS   %-18s %s %s\n", formatDuration(stamp), event.CheckID, event.Executable, strings.Join(event.Arguments, " "))
	case events.ProcessOutput:
		for _, line := range strings.Split(strings.TrimSuffix(event.Message, "\n"), "\n") {
			fmt.Fprintf(renderer.out, "%s  [%s] %s\n", formatDuration(stamp), event.CheckID, line)
		}
	case events.ProcessFinished:
		fmt.Fprintf(renderer.out, "%s  PROCESS   %-18s %s\n", formatDuration(stamp), event.CheckID, event.Status)
	case events.CheckSkipped:
		fmt.Fprintf(renderer.out, "%s  %-9s %-18s %s\n", formatDuration(stamp), event.Status, event.CheckID, event.Message)
	case events.CheckFinished:
		fmt.Fprintf(renderer.out, "%s  %-9s %-18s %s\n", formatDuration(stamp), event.Status, event.CheckID, event.Message)
	case events.CheckEvidence:
		fmt.Fprintf(renderer.out, "%s  INFO      %-18s %s\n", formatDuration(stamp), event.CheckID, event.Message)
	case events.EvidenceWritten:
		fmt.Fprintf(renderer.out, "%s  EVIDENCE  %s\n", formatDuration(stamp), event.Message)
	case events.VerificationFinished:
		fmt.Fprintf(renderer.out, "\nRESULT: %s\n", event.Status)
	}
}

func formatDuration(duration time.Duration) string {
	return fmt.Sprintf("%02d:%06.3f", int(duration/time.Minute), float64(duration%time.Minute)/float64(time.Second))
}
