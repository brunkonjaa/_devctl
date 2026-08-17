package policy

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"devctl/internal/model"
	"devctl/internal/scheduler"
)

func TestLoadValidatesVersionedAutomationConfiguration(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "devctl.json"), []byte(`{
  "version": "1",
  "automation": {
    "control_documents": ["docs/README.md"],
    "verification_profile": "go",
    "max_agent_attempts": 3
  }
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if config.Automation.VerificationProfile != "go" || config.Automation.MaxAgentAttempts != 3 {
		t.Fatalf("unexpected automation configuration: %#v", config.Automation)
	}
}

func TestLoadPreservesCommittedProjectIdentity(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "devctl.json"), []byte(`{"version":"1","project_id":"project-1234"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := Load(root)
	if err != nil || config.ProjectID != "project-1234" {
		t.Fatalf("expected committed project identity, got %#v, %v", config, err)
	}
}

func TestLoadRejectsUnsupportedConfigurationVersion(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "devctl.json"), []byte(`{"version":"2"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil {
		t.Fatal("expected unsupported configuration version to be rejected")
	}
}

func TestLoadRejectsEscapingControlDocument(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "devctl.json"), []byte(`{
  "version": "1",
  "automation": {"control_documents": ["../outside.md"]}
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil {
		t.Fatal("expected escaping control document to be rejected")
	}
}

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

func TestValidateRejectsRequiredDisabledCheck(t *testing.T) {
	disabled := false
	required := true
	checks := []scheduler.CheckSpec{{ID: "go-test", Run: testCheck}}
	if err := ValidateCheckConfiguration(checks, Config{Checks: map[string]CheckPolicy{"go-test": {Enabled: &disabled, Required: &required}}}); err == nil {
		t.Fatal("expected required disabled check to be rejected")
	}
}

func TestValidateRejectsInvalidThresholds(t *testing.T) {
	minimum := 90.0
	preferred := 80.0
	checks := []scheduler.CheckSpec{{ID: "android-coverage", Run: testCheck}}
	if err := ValidateCheckConfiguration(checks, Config{Checks: map[string]CheckPolicy{"android-coverage": {Minimum: &minimum, Preferred: &preferred}}}); err == nil {
		t.Fatal("expected inverted coverage thresholds to be rejected")
	}
}

func TestValidateRejectsUnknownProjectCheck(t *testing.T) {
	checks := []scheduler.CheckSpec{{ID: "go-test", Run: testCheck}}
	if err := ValidateCheckConfiguration(checks, Config{Checks: map[string]CheckPolicy{"made-up-check": {Enabled: boolPointer(true)}}}); err == nil {
		t.Fatal("expected unknown configured check to be rejected")
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

func boolPointer(value bool) *bool {
	return &value
}
