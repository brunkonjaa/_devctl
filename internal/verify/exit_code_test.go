package verify

import (
	"testing"

	"devctl/internal/model"
	"devctl/internal/policy"
)

func TestExitCodeDoesNotBlockOnNotTestedEvidence(t *testing.T) {
	report := model.Report{Overall: model.NotTested, Checks: []model.CheckResult{{Status: model.NotTested}}}
	if got := ExitCode(report); got != 0 {
		t.Fatalf("expected not-tested evidence to be non-blocking by default, got %d", got)
	}
}

func TestRequiredNotTestedEvidenceBlocks(t *testing.T) {
	report := model.Report{Overall: model.NotTested, Checks: []model.CheckResult{{ID: "go-test-race", Status: model.NotTested}}}
	config := policy.Config{Checks: map[string]policy.CheckPolicy{"go-test-race": {Required: boolPtr(true)}}}
	policy.Apply(&report, config)
	if !report.Checks[0].Blocking || ExitCode(report) != 1 {
		t.Fatalf("expected required missing evidence to block: %#v", report.Checks[0])
	}
}

func boolPtr(value bool) *bool { return &value }

func TestExitCodeDoesNotBlockOnWarning(t *testing.T) {
	report := model.Report{Overall: model.Warn, Checks: []model.CheckResult{{Status: model.Warn}}}
	if got := ExitCode(report); got != 0 {
		t.Fatalf("expected warning to be non-blocking, got exit code %d", got)
	}
}

func TestExitCodeBlocksOnExplicitBlockingWarning(t *testing.T) {
	report := model.Report{Overall: model.Warn, Checks: []model.CheckResult{{Status: model.Warn, Blocking: true}}}
	if got := ExitCode(report); got != 1 {
		t.Fatalf("expected blocking warning to fail, got exit code %d", got)
	}
}

func TestExitCodeUsesFrameworkErrorCode(t *testing.T) {
	report := model.Report{Overall: model.Error}
	if got := ExitCode(report); got != 2 {
		t.Fatalf("expected framework error code 2, got %d", got)
	}
}

func TestExitCodeDoesNotBlockOnSuccessfulCheckWithBlockingPolicy(t *testing.T) {
	report := model.Report{Overall: model.Pass, Checks: []model.CheckResult{{Status: model.Pass, Blocking: true}}}
	if got := ExitCode(report); got != 0 {
		t.Fatalf("expected successful check to remain non-blocking, got exit code %d", got)
	}
}
