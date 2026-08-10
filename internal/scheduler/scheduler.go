package scheduler

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"devctl/internal/model"
)

type CheckFunc func(context.Context) model.CheckResult

type TimeoutPolicy struct {
	Hard       time.Duration
	Inactivity time.Duration
}

type CheckSpec struct {
	ID        string
	Version   string
	Requires  []string
	Resources []string
	Timeout   TimeoutPolicy
	Run       CheckFunc
}

type Plan struct {
	Checks map[string]CheckSpec
	Order  []string
}

func BuildPlan(specs []CheckSpec) (Plan, error) {
	checks := make(map[string]CheckSpec, len(specs))
	for _, spec := range specs {
		if strings.TrimSpace(spec.ID) == "" {
			return Plan{}, fmt.Errorf("check ID cannot be empty")
		}
		if spec.Run == nil {
			return Plan{}, fmt.Errorf("check %q has no runner", spec.ID)
		}
		if _, exists := checks[spec.ID]; exists {
			return Plan{}, fmt.Errorf("duplicate check ID %q", spec.ID)
		}
		checks[spec.ID] = spec
	}
	for _, spec := range specs {
		for _, dependency := range spec.Requires {
			if _, exists := checks[dependency]; !exists {
				return Plan{}, fmt.Errorf("check %q requires unknown check %q", spec.ID, dependency)
			}
		}
	}

	state := make(map[string]uint8, len(checks))
	order := make([]string, 0, len(checks))
	var visit func(string) error
	visit = func(id string) error {
		switch state[id] {
		case 1:
			return fmt.Errorf("dependency cycle detected at %q", id)
		case 2:
			return nil
		}
		state[id] = 1
		dependencies := append([]string(nil), checks[id].Requires...)
		sort.Strings(dependencies)
		for _, dependency := range dependencies {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		state[id] = 2
		order = append(order, id)
		return nil
	}
	ids := make([]string, 0, len(checks))
	for id := range checks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := visit(id); err != nil {
			return Plan{}, err
		}
	}
	return Plan{Checks: checks, Order: order}, nil
}

func Run(ctx context.Context, plan Plan, maxConcurrent int) []model.CheckResult {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	pending := make(map[string]bool, len(plan.Checks))
	for id := range plan.Checks {
		pending[id] = true
	}
	results := make(map[string]model.CheckResult, len(plan.Checks))

	for len(pending) > 0 {
		if ctx.Err() != nil {
			for id := range pending {
				results[id] = skipped(id, "scheduler context was cancelled")
				delete(pending, id)
			}
			break
		}

		ready := readyChecks(plan, pending, results)
		if len(ready) == 0 {
			for id := range pending {
				results[id] = model.CheckResult{ID: id, Status: model.Error, Summary: "scheduler could not make progress", Reason: "unresolved dependency state"}
				delete(pending, id)
			}
			break
		}
		for _, id := range ready {
			if blockedByDependency(plan.Checks[id], results) {
				results[id] = skipped(id, "dependency did not complete successfully")
				delete(pending, id)
			}
		}
		ready = readyChecks(plan, pending, results)
		if len(ready) == 0 {
			continue
		}
		batch := lockCompatibleBatch(plan, ready)
		batchResults := runBatch(ctx, plan, batch, maxConcurrent)
		for id, result := range batchResults {
			results[id] = result
			delete(pending, id)
		}
	}

	ordered := make([]model.CheckResult, 0, len(plan.Order))
	for _, id := range plan.Order {
		if result, ok := results[id]; ok {
			ordered = append(ordered, result)
		}
	}
	return ordered
}

func readyChecks(plan Plan, pending map[string]bool, results map[string]model.CheckResult) []string {
	ready := make([]string, 0)
	for id := range pending {
		readyNow := true
		for _, dependency := range plan.Checks[id].Requires {
			if _, completed := results[dependency]; !completed {
				readyNow = false
				break
			}
		}
		if readyNow {
			ready = append(ready, id)
		}
	}
	sort.Strings(ready)
	return ready
}

func lockCompatibleBatch(plan Plan, ready []string) []string {
	used := make(map[string]bool)
	batch := make([]string, 0, len(ready))
	for _, id := range ready {
		compatible := true
		for _, resource := range plan.Checks[id].Resources {
			if used[resource] {
				compatible = false
				break
			}
		}
		if !compatible {
			continue
		}
		for _, resource := range plan.Checks[id].Resources {
			used[resource] = true
		}
		batch = append(batch, id)
	}
	return batch
}

func runBatch(ctx context.Context, plan Plan, ids []string, maxConcurrent int) map[string]model.CheckResult {
	results := make(map[string]model.CheckResult, len(ids))
	var mu sync.Mutex
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, maxConcurrent)
	for _, id := range ids {
		id := id
		wg.Add(1)
		go func() {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			result := runOne(ctx, plan.Checks[id])
			mu.Lock()
			results[id] = result
			mu.Unlock()
		}()
	}
	wg.Wait()
	return results
}

func runOne(ctx context.Context, spec CheckSpec) model.CheckResult {
	checkContext := ctx
	cancel := func() {}
	if spec.Timeout.Hard > 0 {
		checkContext, cancel = context.WithTimeout(ctx, spec.Timeout.Hard)
	}
	defer cancel()
	started := time.Now()
	result := spec.Run(checkContext)
	result.ID = spec.ID
	result.CheckVersion = spec.Version
	result.DurationMS = time.Since(started).Milliseconds()
	if checkContext.Err() == context.DeadlineExceeded {
		result.Status = model.Error
		result.Summary = "check timed out"
		result.Reason = "hard timeout exceeded"
	}
	if checkContext.Err() == context.Canceled {
		result.Status = model.Error
		result.Summary = "check cancelled"
		result.Reason = "scheduler context was cancelled while the check was running"
	}
	return result
}

func skipped(id, reason string) model.CheckResult {
	return model.CheckResult{ID: id, Status: model.Skip, Summary: "check was not run", Reason: reason}
}

func dependencyBlocks(result model.CheckResult) bool {
	if result.Blocking {
		return true
	}
	switch result.Status {
	case model.Fail, model.Error, model.Skip, model.NotTested, model.InsufficientEvidence, model.RequiresReview:
		return true
	default:
		return false
	}
}

func blockedByDependency(spec CheckSpec, results map[string]model.CheckResult) bool {
	for _, dependency := range spec.Requires {
		if dependencyBlocks(results[dependency]) {
			return true
		}
	}
	return false
}
