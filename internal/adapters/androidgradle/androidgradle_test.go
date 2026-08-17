package androidgradle

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"devctl/internal/model"
)

func TestChecksDeclareGradleExecutionRelationships(t *testing.T) {
	project := model.Project{Name: "HearthLink", Path: `C:\Projects\HearthLink`}
	checks := Checks(project)
	byID := make(map[string]struct {
		requires  []string
		resources []string
	}, len(checks))
	for _, check := range checks {
		byID[check.ID] = struct {
			requires  []string
			resources []string
		}{check.Requires, check.Resources}
	}

	assertRequires(t, byID, "android-build", "android-java-environment", "android-gradle-wrapper", "android-project-structure")
	assertRequires(t, byID, "android-unit-tests", "android-build")
	assertRequires(t, byID, "android-lint", "android-java-environment", "android-gradle-wrapper", "android-project-structure")
	assertResource(t, byID, "android-build", "gradle")
	assertResource(t, byID, "android-unit-tests", "gradle")
	assertResource(t, byID, "android-lint", "gradle")
}

func TestGradleWrapperRequiresPlatformAppropriateScript(t *testing.T) {
	root := t.TempDir()
	wrapper := "gradlew"
	if runtime.GOOS == "windows" {
		wrapper = "gradlew.bat"
	}
	if err := os.WriteFile(filepath.Join(root, wrapper), []byte("wrapper"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := fileCheck(root, "android-gradle-wrapper", []string{wrapper}, "Gradle wrapper is present")
	if result.Status != model.Pass {
		t.Fatalf("expected platform wrapper %q to pass, got %#v", wrapper, result)
	}
}

func assertRequires(t *testing.T, checks map[string]struct {
	requires  []string
	resources []string
}, id string, expected ...string) {
	t.Helper()
	check, ok := checks[id]
	if !ok {
		t.Fatalf("missing check %q", id)
	}
	for _, dependency := range expected {
		found := false
		for _, actual := range check.requires {
			if actual == dependency {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("check %q missing dependency %q", id, dependency)
		}
	}
}

func assertResource(t *testing.T, checks map[string]struct {
	requires  []string
	resources []string
}, id, expected string) {
	t.Helper()
	check, ok := checks[id]
	if !ok {
		t.Fatalf("missing check %q", id)
	}
	for _, resource := range check.resources {
		if resource == expected {
			return
		}
	}
	t.Errorf("check %q missing resource %q", id, expected)
}
