//go:build !windows

package runner

import (
	"context"
	"testing"
	"time"
)

func TestRunWithOptionsTerminatesUnixProcessGroupOnInactivity(t *testing.T) {
	id := CommandID("test-idle-unix")
	allowed[id] = Spec{ID: id, Program: "sh", Args: []string{"-c", "sleep 20"}}
	defer delete(allowed, id)

	result, err := RunWithOptions(context.Background(), t.TempDir(), id, TimeoutPolicy{Hard: 5 * time.Second, Inactivity: 500 * time.Millisecond})
	if err == nil {
		t.Fatal("expected inactivity timeout")
	}
	if result.TerminationReason != "inactivity_timeout" {
		t.Fatalf("expected inactivity timeout, got %q", result.TerminationReason)
	}
}
