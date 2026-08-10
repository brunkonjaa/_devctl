package discovery

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectFindsMultipleTechnologies(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"package.json", "pyproject.toml", "Cargo.toml"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	project, err := Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(project.Technologies) != 3 {
		t.Fatalf("expected 3 technologies, got %d", len(project.Technologies))
	}
}

func TestDetectFindsGoModule(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	project, err := Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(project.Technologies) != 1 || project.Technologies[0].ID != "go" {
		t.Fatalf("expected Go technology, got %#v", project.Technologies)
	}
}

func TestDiscoverSkipsDevctlAndUnknownDirectories(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"_devctl", "unknown", "HearthLink"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "HearthLink", "settings.gradle.kts"), []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}

	projects, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].Name != "HearthLink" {
		t.Fatalf("unexpected projects: %#v", projects)
	}
}
