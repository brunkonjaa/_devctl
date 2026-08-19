package verify

import (
	"context"
	"fmt"
	"strings"
	"time"

	"devctl/internal/adapters"
	"devctl/internal/discovery"
	"devctl/internal/events"
	"devctl/internal/evidence"
	"devctl/internal/gitstate"
	"devctl/internal/model"
	"devctl/internal/policy"
	"devctl/internal/runner"
	"devctl/internal/scheduler"
	"devctl/internal/version"
)

const coreCheckVersion = "core-v1"

type Options struct {
	Sink          events.Sink
	RunID         string
	OutputMetrics *runner.OutputMetrics
}

func Project(ctx context.Context, path string) (report model.Report) {
	return ProjectWithOptions(ctx, path, Options{})
}

func ProjectWithOptions(ctx context.Context, path string, options Options) (report model.Report) {
	started := time.Now().UTC()
	provenance := version.Current()
	runID := options.RunID
	if runID == "" {
		runID = started.Format("20060102T150405.000000000Z")
	}
	report = model.Report{SchemaVersion: "1", Command: "verify", RunID: runID, StartedAt: started, DevctlVersion: provenance.Version, DevctlCommit: provenance.Commit, DevctlDirty: provenance.Dirty}
	if options.Sink != nil {
		ctx = events.WithSink(ctx, options.Sink)
	}
	ctx = runner.WithOutputMetrics(ctx, options.OutputMetrics)
	ctx = events.WithMetadata(ctx, report.RunID, "")
	defer func() { finalize(&report, path, ctx) }()
	project, err := discovery.Detect(path)
	if err != nil {
		report.Overall = model.Error
		report.FinishedAt = time.Now().UTC()
		report.Checks = []model.CheckResult{{ID: "project-detection", CheckVersion: coreCheckVersion, Status: model.Error, Summary: "project could not be inspected", ErrorDetail: err.Error()}}
		return report
	}
	report.Project = &project
	if len(project.Technologies) == 0 {
		report.Checks = append(report.Checks, model.CheckResult{ID: "technology-detection", CheckVersion: coreCheckVersion, Status: model.InsufficientEvidence, Blocking: true, Summary: "no supported project markers were found"})
		report.Overall = model.InsufficientEvidence
		report.FinishedAt = time.Now().UTC()
		return report
	} else {
		report.Checks = append(report.Checks, model.CheckResult{ID: "technology-detection", CheckVersion: coreCheckVersion, Status: model.Pass, Summary: "supported project technology detected"})
	}

	config, configErr := policy.Load(project.Path)
	if configErr != nil {
		report.Overall = model.Error
		report.Checks = append(report.Checks, model.CheckResult{ID: "policy-loading", CheckVersion: coreCheckVersion, Status: model.Error, Summary: "project policy could not be loaded", Reason: configErr.Error()})
		report.FinishedAt = time.Now().UTC()
		return report
	}
	report.PolicyVersion = config.Version
	if config.ProjectID != "" {
		project.Identity = config.ProjectID
		report.Project = &project
	}
	ctx = events.WithMetadata(ctx, report.RunID, project.Identity)
	events.Emit(ctx, events.Event{EventType: events.VerificationStarted, Message: "verification started"})
	specs := adapters.Checks(project)
	if validationErr := policy.ValidateCheckConfiguration(specs, config); validationErr != nil {
		report.Overall = model.Error
		report.Checks = append(report.Checks, model.CheckResult{ID: "policy-validation", CheckVersion: coreCheckVersion, Status: model.Error, Summary: "project policy is invalid", Reason: validationErr.Error()})
		report.FinishedAt = time.Now().UTC()
		return report
	}
	plan, planErr := scheduler.BuildPlan(policy.FilterChecks(specs, config))
	if planErr != nil {
		report.Overall = model.Error
		report.Checks = append(report.Checks, model.CheckResult{ID: "scheduler-plan", CheckVersion: coreCheckVersion, Status: model.Error, Summary: "check plan could not be built", Reason: planErr.Error()})
		report.FinishedAt = time.Now().UTC()
		return report
	}
	report.Checks = append(report.Checks, scheduler.Run(ctx, plan, 4)...)
	policy.Apply(&report, config)
	for _, spec := range plan.Checks {
		if !spec.DeferFinish {
			continue
		}
		for _, check := range report.Checks {
			if check.ID == spec.ID && check.Status != model.Skip {
				events.Emit(events.WithCheck(ctx, check.ID), events.Event{EventType: events.CheckFinished, Status: string(check.Status), ElapsedMS: check.DurationMS, Message: check.Summary})
				break
			}
		}
	}
	provenanceCtx := events.WithCheck(ctx, "provenance")
	commitResult, _ := runner.Run(provenanceCtx, project.Path, runner.GitCommit)
	statusResult, _ := runner.Run(provenanceCtx, project.Path, runner.GitStatus)
	report.RepositoryRevision = strings.TrimSpace(commitResult.Output)
	for _, line := range strings.Split(statusResult.Output, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "##") {
			report.RepositoryDirty = true
			break
		}
	}
	if fingerprint, fingerprintErr := gitstate.Fingerprint(project.Path); fingerprintErr == nil {
		report.RepositoryFingerprint = fingerprint
	}
	report.Overall = overall(report.Checks)
	report.FinishedAt = time.Now().UTC()
	return report
}

func finalize(report *model.Report, projectPath string, ctx context.Context) {
	if report.FinishedAt.IsZero() {
		report.FinishedAt = time.Now().UTC()
	}
	report.EvidencePath = fmt.Sprintf(".devctl/evidence/%s", report.RunID)
	if _, err := evidence.Write(projectPath, *report); err != nil {
		report.Checks = append(report.Checks, model.CheckResult{ID: "evidence-write", CheckVersion: coreCheckVersion, Status: model.Error, Summary: "verification evidence could not be written", Reason: err.Error()})
		report.Overall = model.Error
	} else {
		events.Emit(ctx, events.Event{EventType: events.EvidenceWritten, Status: string(report.Overall), Message: report.EvidencePath})
	}
	events.Emit(ctx, events.Event{EventType: events.VerificationFinished, Status: string(report.Overall), Message: "verification finished"})
}

func NewRunID() string {
	return time.Now().UTC().Format("20060102T150405.000000000Z")
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
