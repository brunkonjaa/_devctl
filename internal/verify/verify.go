package verify

import (
	"context"
	"fmt"
	"time"

	"devctl/internal/adapters"
	"devctl/internal/discovery"
	"devctl/internal/evidence"
	"devctl/internal/model"
	"devctl/internal/policy"
	"devctl/internal/scheduler"
	"devctl/internal/version"
)

func Project(ctx context.Context, path string) (report model.Report) {
	started := time.Now().UTC()
	report = model.Report{SchemaVersion: "1", Command: "verify", RunID: started.Format("20060102T150405.000000000Z"), StartedAt: started, DevctlVersion: version.Value, DevctlCommit: version.Commit}
	defer finalize(&report, path)
	project, err := discovery.Detect(path)
	if err != nil {
		report.Overall = model.Error
		report.FinishedAt = time.Now().UTC()
		report.Checks = []model.CheckResult{{ID: "project-detection", Status: model.Error, Summary: "project could not be inspected", ErrorDetail: err.Error()}}
		return report
	}
	report.Project = &project
	if len(project.Technologies) == 0 {
		report.Checks = append(report.Checks, model.CheckResult{ID: "technology-detection", Status: model.InsufficientEvidence, Blocking: true, Summary: "no supported project markers were found"})
		report.Overall = model.InsufficientEvidence
		report.FinishedAt = time.Now().UTC()
		return report
	} else {
		report.Checks = append(report.Checks, model.CheckResult{ID: "technology-detection", Status: model.Pass, Summary: "supported project technology detected"})
	}

	config, configErr := policy.Load(project.Path)
	if configErr != nil {
		report.Overall = model.Error
		report.Checks = append(report.Checks, model.CheckResult{ID: "policy-loading", Status: model.Error, Summary: "project policy could not be loaded", Reason: configErr.Error()})
		report.FinishedAt = time.Now().UTC()
		return report
	}
	report.PolicyVersion = config.Version
	plan, planErr := scheduler.BuildPlan(policy.FilterChecks(adapters.Checks(project), config))
	if planErr != nil {
		report.Overall = model.Error
		report.Checks = append(report.Checks, model.CheckResult{ID: "scheduler-plan", Status: model.Error, Summary: "check plan could not be built", Reason: planErr.Error()})
		report.FinishedAt = time.Now().UTC()
		return report
	}
	report.Checks = append(report.Checks, scheduler.Run(ctx, plan, 4)...)
	policy.Apply(&report, config)
	report.Overall = overall(report.Checks)
	report.FinishedAt = time.Now().UTC()
	return report
}

func finalize(report *model.Report, projectPath string) {
	if report.FinishedAt.IsZero() {
		report.FinishedAt = time.Now().UTC()
	}
	report.EvidencePath = fmt.Sprintf(".devctl/evidence/%s", report.RunID)
	if _, err := evidence.Write(projectPath, *report); err != nil {
		report.Checks = append(report.Checks, model.CheckResult{ID: "evidence-write", Status: model.Error, Summary: "verification evidence could not be written", Reason: err.Error()})
		report.Overall = model.Error
	}
}

func overall(checks []model.CheckResult) model.Status {
	worst := model.Pass
	priority := map[model.Status]int{model.Pass: 0, model.NotApplicable: 0, model.Skip: 0, model.Warn: 1, model.NotTested: 2, model.InsufficientEvidence: 2, model.RequiresReview: 2, model.Fail: 3, model.Error: 4}
	for _, check := range checks {
		if priority[check.Status] > priority[worst] {
			worst = check.Status
		}
	}
	return worst
}

// ExitCode separates a finding's severity from its pipeline consequence.
// Warnings remain visible but are non-blocking unless a policy marks them as blocking.
func ExitCode(report model.Report) int {
	if report.Overall == model.Error {
		return 2
	}
	for _, check := range report.Checks {
		if check.Status == model.Fail || (check.Blocking && check.Status != model.Pass && check.Status != model.NotApplicable) {
			return 1
		}
	}
	return 0
}
