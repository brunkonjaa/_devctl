package runner

import (
	"context"
	"testing"
)

func TestOutputMetricsCountObservedAndRetainedBytes(t *testing.T) {
	metrics := &OutputMetrics{}
	result, err := Run(WithOutputMetrics(context.Background(), metrics), t.TempDir(), GoVersion)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := metrics.Snapshot()
	if snapshot.RawBytes == 0 || snapshot.RawBytes != int64(len(result.Output)) {
		t.Fatalf("unexpected raw byte count: snapshot=%+v output=%d", snapshot, len(result.Output))
	}
	if snapshot.RetainedBytes != int64(len(result.Output)) || snapshot.Truncated {
		t.Fatalf("unexpected retained byte count: %+v", snapshot)
	}
}
