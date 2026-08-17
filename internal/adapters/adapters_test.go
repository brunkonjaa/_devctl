package adapters

import (
	"testing"

	"devctl/internal/model"
)

func TestChecksComposeAllApplicableAdapters(t *testing.T) {
	specs := Checks(model.Project{Path: t.TempDir(), Technologies: []model.Technology{
		{ID: "android-gradle"},
		{ID: "go"},
	}})
	seen := make(map[string]bool, len(specs))
	for _, spec := range specs {
		seen[spec.ID] = true
	}
	if !seen["android-build"] || !seen["go-test"] {
		t.Fatalf("expected Android and Go checks, got %#v", seen)
	}
}

func TestChecksRejectUnsupportedTechnologyWithBlockingCheck(t *testing.T) {
	specs := Checks(model.Project{Path: t.TempDir(), Technologies: []model.Technology{{ID: "node"}}})
	for _, spec := range specs {
		if spec.ID != "adapter-support" {
			continue
		}
		result := spec.Run(nil)
		if result.Status != model.NotTested || !result.Blocking {
			t.Fatalf("expected blocking unsupported-adapter result, got %#v", result)
		}
		return
	}
	t.Fatal("expected adapter-support check")
}
