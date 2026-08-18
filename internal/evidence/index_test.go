package evidence

import (
	"devctl/internal/model"
	"testing"
	"time"
)

func timeValue(second int) time.Time { return time.Unix(int64(second), 0).UTC() }

func TestRebuildIndexesLatestSuccessfulAndCheck(t *testing.T) {
	root := t.TempDir()
	if _, err := Write(root, model.Report{RunID: "run-1", StartedAt: timeValue(1), FinishedAt: timeValue(2), Project: &model.Project{Name: "p"}, Checks: []model.CheckResult{{ID: "go-test", Status: model.Fail, Summary: "failed"}}, Overall: model.Fail}); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(root, model.Report{RunID: "run-2", StartedAt: timeValue(3), FinishedAt: timeValue(4), Project: &model.Project{Name: "p"}, Checks: []model.CheckResult{{ID: "go-test", Status: model.Pass, Summary: "passed"}}, Overall: model.Pass}); err != nil {
		t.Fatal(err)
	}
	index, err := Rebuild(root)
	if err != nil {
		t.Fatal(err)
	}
	if Latest(index) == nil || Latest(index).RunID != "run-2" {
		t.Fatalf("unexpected latest: %+v", index)
	}
	if LatestSuccessful(index) == nil || LatestSuccessful(index).RunID != "run-2" {
		t.Fatal("missing latest successful")
	}
	if check := FindCheck(index, "go-test"); check == nil || check.Status != model.Pass {
		t.Fatalf("missing check index: %+v", check)
	}
}
