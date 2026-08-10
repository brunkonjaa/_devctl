//go:build windows

package runner

import (
	"context"
	"testing"
	"time"
)

func TestRunWithOptionsTerminatesWindowsProcessTreeOnInactivity(t *testing.T) {
	id := CommandID("test-idle-windows")
	allowed[id] = Spec{ID: id, Program: "cmd.exe", Args: []string{"/d", "/c", "ping 127.0.0.1 -n 20 > nul"}}
	defer delete(allowed, id)

	result, err := RunWithOptions(context.Background(), t.TempDir(), id, TimeoutPolicy{Hard: 5 * time.Second, Inactivity: 500 * time.Millisecond})
	if err == nil {
		t.Fatal("expected inactivity timeout")
	}
	if result.TerminationReason != "inactivity_timeout" {
		t.Fatalf("expected inactivity timeout, got %q", result.TerminationReason)
	}
}
