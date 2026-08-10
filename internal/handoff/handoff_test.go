package handoff

import (
	"strings"
	"testing"

	"devctl/internal/model"
)

func TestFromReportIncludesOnlyFailedOrBlockingChecks(t *testing.T) {
	packet := FromReport(model.Report{RunID: "run", Overall: model.Fail, Project: &model.Project{Name: "demo"}, Checks: []model.CheckResult{
		{ID: "ok", Status: model.Pass, Summary: "passed", RawOutput: "secret raw output"},
		{ID: "failed", Status: model.Fail, Blocking: true, Summary: "failed", Evidence: []model.Evidence{{Path: ".devctl/evidence/run/check.json"}}},
	}})
	if len(packet.Failures) != 1 || packet.Failures[0].CheckID != "failed" {
		t.Fatalf("unexpected packet: %#v", packet)
	}
	if strings.Contains(Text(packet), "secret raw output") {
		t.Fatal("handoff must not include raw output")
	}
}
