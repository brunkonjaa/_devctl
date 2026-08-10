package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"devctl/internal/model"
)

func TestBuildPlanOrdersDependencies(t *testing.T) {
	plan, err := BuildPlan([]CheckSpec{
		{ID: "coverage", Requires: []string{"tests"}, Run: passCheck},
		{ID: "tests", Requires: []string{"build"}, Run: passCheck},
		{ID: "build", Run: passCheck},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"build", "tests", "coverage"}
	for index, id := range want {
		if plan.Order[index] != id {
			t.Fatalf("expected order %v, got %v", want, plan.Order)
		}
	}
}

func TestBuildPlanRejectsCycles(t *testing.T) {
	_, err := BuildPlan([]CheckSpec{
		{ID: "a", Requires: []string{"b"}, Run: passCheck},
		{ID: "b", Requires: []string{"a"}, Run: passCheck},
	})
	if err == nil {
		t.Fatal("expected dependency cycle to be rejected")
	}
}

func TestRunSkipsDependantsAfterFailure(t *testing.T) {
	var dependentRan atomic.Bool
	plan, err := BuildPlan([]CheckSpec{
		{ID: "build", Run: func(context.Context) model.CheckResult {
			return model.CheckResult{Status: model.Fail, Summary: "build failed"}
		}},
		{ID: "tests", Requires: []string{"build"}, Run: func(context.Context) model.CheckResult {
			dependentRan.Store(true)
			return model.CheckResult{Status: model.Pass}
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	results := Run(context.Background(), plan, 2)
	if dependentRan.Load() {
		t.Fatal("dependent check should not have run")
	}
	if results[1].Status != model.Skip || results[1].Reason == "" {
		t.Fatalf("expected dependency skip with reason, got %#v", results[1])
	}
}

func TestRunAllowsWarningsToContinue(t *testing.T) {
	plan, err := BuildPlan([]CheckSpec{
		{ID: "first", Run: func(context.Context) model.CheckResult { return model.CheckResult{Status: model.Warn} }},
		{ID: "second", Requires: []string{"first"}, Run: passCheck},
	})
	if err != nil {
		t.Fatal(err)
	}
	results := Run(context.Background(), plan, 1)
	if results[1].Status != model.Pass {
		t.Fatalf("warning should not block dependency, got %#v", results[1])
	}
}

func TestRunPersistsCheckVersionForCompletedAndSkippedChecks(t *testing.T) {
	plan, err := BuildPlan([]CheckSpec{
		{ID: "build", Version: "build-v2", Run: func(context.Context) model.CheckResult { return model.CheckResult{Status: model.Fail} }},
		{ID: "tests", Requires: []string{"build"}, Run: passCheck},
	})
	if err != nil {
		t.Fatal(err)
	}
	results := Run(context.Background(), plan, 1)
	if results[0].CheckVersion != "build-v2" {
		t.Fatalf("expected explicit check version, got %#v", results[0])
	}
	if results[1].CheckVersion != "1" {
		t.Fatalf("expected default check version on skipped result, got %#v", results[1])
	}
}

func TestRunSerializesSharedResources(t *testing.T) {
	var active atomic.Int32
	var maxActive atomic.Int32
	resourceCheck := func(context.Context) model.CheckResult {
		current := active.Add(1)
		for {
			previous := maxActive.Load()
			if current <= previous || maxActive.CompareAndSwap(previous, current) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		active.Add(-1)
		return model.CheckResult{Status: model.Pass}
	}
	plan, err := BuildPlan([]CheckSpec{
		{ID: "one", Resources: []string{"gradle"}, Run: resourceCheck},
		{ID: "two", Resources: []string{"gradle"}, Run: resourceCheck},
	})
	if err != nil {
		t.Fatal(err)
	}
	Run(context.Background(), plan, 2)
	if got := maxActive.Load(); got != 1 {
		t.Fatalf("expected shared resource to serialize checks, max concurrency was %d", got)
	}
}

func TestRunTimesOutCheck(t *testing.T) {
	plan, err := BuildPlan([]CheckSpec{{
		ID:      "slow",
		Timeout: TimeoutPolicy{Hard: 10 * time.Millisecond},
		Run: func(ctx context.Context) model.CheckResult {
			<-ctx.Done()
			return model.CheckResult{Status: model.Pass}
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	results := Run(context.Background(), plan, 1)
	if results[0].Status != model.Error || results[0].Reason == "" {
		t.Fatalf("expected timeout error with reason, got %#v", results[0])
	}
}

func TestRunCancelsPendingChecks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	plan, err := BuildPlan([]CheckSpec{
		{ID: "one", Run: passCheck},
		{ID: "two", Requires: []string{"one"}, Run: passCheck},
	})
	if err != nil {
		t.Fatal(err)
	}
	results := Run(ctx, plan, 1)
	for _, result := range results {
		if result.Status != model.Skip {
			t.Fatalf("expected cancelled check to be skipped, got %#v", result)
		}
	}
}

func passCheck(context.Context) model.CheckResult {
	return model.CheckResult{Status: model.Pass, Summary: "passed"}
}
