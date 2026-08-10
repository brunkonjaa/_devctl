package golang

import "testing"

func TestRaceEnvironmentReportsMissingCompilerAsUnavailable(t *testing.T) {
	available, reason := raceEnvironmentFor("windows", "1", func(string) bool { return false })
	if available {
		t.Fatal("expected race environment to be unavailable")
	}
	if reason == "" {
		t.Fatal("expected a reason for unavailable race environment")
	}
}

func TestRaceEnvironmentReportsCGORequirement(t *testing.T) {
	available, reason := raceEnvironmentFor("windows", "0", func(string) bool { return true })
	if available || reason != "CGO_ENABLED=0; the Go race detector requires cgo" {
		t.Fatalf("expected cgo-disabled explanation, got available=%v reason=%q", available, reason)
	}
}
