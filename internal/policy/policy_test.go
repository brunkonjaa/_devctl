package policy

import (
	"context"
	"testing"

	"devctl/internal/model"
	"devctl/internal/scheduler"
)

func TestFilterChecksRemovesDisabledDependants(t *testing.T) {
	disabled := false
	checks := []scheduler.CheckSpec{
		{ID: "build", Run: testCheck},
		{ID: "tests", Requires: []string{"build"}, Run: testCheck},
	}
	filtered := FilterChecks(checks, Config{Checks: map[string]CheckPolicy{"build": {Enabled: &disabled}}})
	if len(filtered) != 0 {
		t.Fatalf("expected dependant checks to be removed, got %d", len(filtered))
	}
}

func TestApplyMakesRequiredUnavailableEvidenceBlocking(t *testing.T) {
	required := true
	report := model.Report{Checks: []model.CheckResult{{ID: "android-coverage", Status: model.NotTested}}}
	Apply(&report, Config{Checks: map[string]CheckPolicy{"android-coverage": {Required: &required}}})
	if !report.Checks[0].Blocking {
		t.Fatal("expected required not-tested check to become blocking")
	}
}

func TestApplyKeepsNonBlockingDirtyWorktreeWarning(t *testing.T) {
	notBlocking := false
	report := model.Report{Checks: []model.CheckResult{{ID: "git-status", Status: model.Warn, Blocking: true}}}
	Apply(&report, Config{Checks: map[string]CheckPolicy{"git-status": {Blocking: &notBlocking}}})
	if report.Checks[0].Blocking {
		t.Fatal("expected policy to keep dirty worktree warning non-blocking")
	}
}

func TestApplyUsesConfiguredAndroidCoverageThresholds(t *testing.T) {
	minimum := 70.0
	preferred := 80.0
	for _, test := range []struct {
		name       string
		percentage float64
		status     model.Status
		blocking   bool
	}{
		{name: "below minimum", percentage: 69, status: model.Fail, blocking: true},
		{name: "at minimum", percentage: 70, status: model.Warn},
		{name: "below preferred", percentage: 79.9, status: model.Warn},
		{name: "at preferred", percentage: 80, status: model.Pass},
	} {
		t.Run(test.name, func(t *testing.T) {
			percentage := test.percentage
			report := model.Report{Checks: []model.CheckResult{{
				ID:      "android-coverage",
				Status:  model.Pass,
				Summary: "Coverage evidence collected",
				Evidence: []model.Evidence{{
					Type:     "coverage-report",
					Coverage: &percentage,
				}},
			}}}
			Apply(&report, Config{Checks: map[string]CheckPolicy{"android-coverage": {Minimum: &minimum, Preferred: &preferred}}})
			result := report.Checks[0]
			if result.Status != test.status || result.Blocking != test.blocking {
				t.Fatalf("unexpected result: %#v", result)
			}
		})
	}
}

func TestApplyLeavesCoverageEvidenceWithoutNumericMetricUnchanged(t *testing.T) {
	minimum := 70.0
	report := model.Report{Checks: []model.CheckResult{{ID: "android-coverage", Status: model.NotTested, Summary: "Coverage not tested"}}}
	Apply(&report, Config{Checks: map[string]CheckPolicy{"android-coverage": {Minimum: &minimum}}})
	if report.Checks[0].Status != model.NotTested || report.Checks[0].Summary != "Coverage not tested" {
		t.Fatalf("expected evidence gap to remain truthful, got %#v", report.Checks[0])
	}
}

func testCheck(context.Context) model.CheckResult {
	return model.CheckResult{Status: model.Pass}
}
