package registry

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

func TestRegisterPersistsProjectMetadata(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("DEVCTL_STATE_DIR", stateDir)
	projectDir := makeProject(t, "alpha", "project-alpha")

	entry, err := DetectProject(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := Register(entry); err != nil {
		t.Fatal(err)
	}

	registry, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	got, ok := registry.Projects["project-alpha"]
	if !ok {
		t.Fatal("registered project is missing")
	}
	if got.Path != mustCanonical(t, projectDir) || got.Name != "alpha" || got.State != "idle" {
		t.Fatalf("unexpected entry: %+v", got)
	}
	if len(got.Technologies) != 1 || got.Technologies[0] != "go" {
		t.Fatalf("unexpected technologies: %#v", got.Technologies)
	}
}

func TestConcurrentWritersPreserveBothProjectsAndValidJSON(t *testing.T) {
	t.Setenv("DEVCTL_STATE_DIR", t.TempDir())
	projects := []ProjectEntry{
		{ProjectID: "project-alpha", Name: "alpha", Path: makeProject(t, "alpha", "")},
		{ProjectID: "project-beta", Name: "beta", Path: makeProject(t, "beta", "")},
	}
	var wait sync.WaitGroup
	for _, entry := range projects {
		entry := entry
		wait.Add(1)
		go func() {
			defer wait.Done()
			if err := Register(entry); err != nil {
				t.Errorf("register %s: %v", entry.ProjectID, err)
			}
		}()
	}
	wait.Wait()

	data, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	var decoded Registry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("registry is not valid JSON: %v", err)
	}
	if len(decoded.Projects) != 2 {
		t.Fatalf("expected two projects, got %d", len(decoded.Projects))
	}
}

func TestConcurrentProcessWritersPreserveBothProjects(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("DEVCTL_STATE_DIR", stateDir)
	first := makeProject(t, "process-first", "")
	second := makeProject(t, "process-second", "")
	commands := make([]*exec.Cmd, 0, 2)
	for _, project := range []struct {
		id   string
		path string
	}{
		{id: "process-alpha", path: first},
		{id: "process-beta", path: second},
	} {
		command := exec.Command(os.Args[0], "-test.run=TestRegistryHelperProcess")
		command.Env = append(os.Environ(),
			"DEVCTL_STATE_DIR="+stateDir,
			"DEVCTL_REGISTRY_HELPER=1",
			"DEVCTL_REGISTRY_PROJECT_ID="+project.id,
			"DEVCTL_REGISTRY_PROJECT_PATH="+project.path,
		)
		commands = append(commands, command)
	}
	for _, command := range commands {
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
	}
	for _, command := range commands {
		if err := command.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	registry, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Projects) != 2 {
		t.Fatalf("expected two projects after concurrent processes, got %d", len(registry.Projects))
	}
}

func TestRegistryHelperProcess(t *testing.T) {
	if os.Getenv("DEVCTL_REGISTRY_HELPER") != "1" {
		return
	}
	entry := ProjectEntry{
		ProjectID: os.Getenv("DEVCTL_REGISTRY_PROJECT_ID"),
		Name:      filepath.Base(os.Getenv("DEVCTL_REGISTRY_PROJECT_PATH")),
		Path:      os.Getenv("DEVCTL_REGISTRY_PROJECT_PATH"),
	}
	if err := Register(entry); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

func TestMovedPathRetainsCommittedProjectID(t *testing.T) {
	t.Setenv("DEVCTL_STATE_DIR", t.TempDir())
	oldPath := makeProject(t, "moved", "project-moved")
	entry, err := DetectProject(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := Register(entry); err != nil {
		t.Fatal(err)
	}

	newPath := filepath.Join(t.TempDir(), "moved-new")
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatal(err)
	}
	movedEntry, err := DetectProject(newPath)
	if err != nil {
		t.Fatal(err)
	}
	if movedEntry.ProjectID != "project-moved" {
		t.Fatalf("committed identity was not retained: %q", movedEntry.ProjectID)
	}
	if err := Register(movedEntry); err != nil {
		t.Fatal(err)
	}

	registry, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Projects) != 1 || registry.Projects["project-moved"].Path != mustCanonical(t, newPath) {
		t.Fatalf("moved project was not updated in place: %+v", registry.Projects)
	}
}

func TestIdentityCollisionIsRejectedWhileOriginalPathExists(t *testing.T) {
	t.Setenv("DEVCTL_STATE_DIR", t.TempDir())
	first := makeProject(t, "first", "project-collision")
	second := makeProject(t, "second", "project-collision")
	firstEntry, err := DetectProject(first)
	if err != nil {
		t.Fatal(err)
	}
	if err := Register(firstEntry); err != nil {
		t.Fatal(err)
	}
	secondEntry, err := DetectProject(second)
	if err != nil {
		t.Fatal(err)
	}
	if err := Register(secondEntry); err == nil {
		t.Fatal("expected project identity collision")
	}
}

func TestStaleRunningStateIsNormalizedOnLoad(t *testing.T) {
	t.Setenv("DEVCTL_STATE_DIR", t.TempDir())
	projectDir := makeProject(t, "stale", "project-stale")
	entry, err := DetectProject(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := Register(entry); err != nil {
		t.Fatal(err)
	}
	if err := update(func(registry *Registry) error {
		stale := registry.Projects[entry.ProjectID]
		stale.State = "running"
		stale.CurrentRunID = "run-stale"
		stale.PID = os.Getpid()
		stale.ProcessIdentity = "pid:invalid/start:0"
		registry.Projects[entry.ProjectID] = stale
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	registry, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	got := registry.Projects["project-stale"]
	if got.State != "stale" || got.Status != "STALE" || got.PID != 0 {
		t.Fatalf("stale state was not normalized: %+v", got)
	}
}

func TestBeginStoresAndValidatesProcessIdentity(t *testing.T) {
	t.Setenv("DEVCTL_STATE_DIR", t.TempDir())
	projectDir := makeProject(t, "identity", "project-identity")
	entry, err := DetectProject(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := Begin(entry, "run-identity", os.Getpid()); err != nil {
		t.Fatal(err)
	}
	registry, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	stored := registry.Projects[entry.ProjectID]
	if stored.ProcessIdentity == "" {
		t.Fatal("process identity was not stored")
	}
	if !ProcessMatches(os.Getpid(), stored.ProcessIdentity) {
		t.Fatalf("stored process identity does not match current process: %q", stored.ProcessIdentity)
	}
	if ProcessMatches(os.Getpid(), stored.ProcessIdentity+"/different") {
		t.Fatal("different process identity was accepted")
	}
}

func TestActiveRunCannotBeReplaced(t *testing.T) {
	t.Setenv("DEVCTL_STATE_DIR", t.TempDir())
	projectDir := makeProject(t, "active", "project-active")
	entry, err := DetectProject(projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := Begin(entry, "run-one", os.Getpid()); err != nil {
		t.Fatal(err)
	}
	if err := Begin(entry, "run-two", os.Getpid()); err == nil {
		t.Fatal("expected active run rejection")
	}
	if err := Finish(entry.ProjectID, "run-one", "PASS"); err != nil {
		t.Fatal(err)
	}
}

func makeProject(t *testing.T, name, projectID string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "go.mod"), []byte("module example.com/"+name+"\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if projectID != "" {
		data := []byte("{\n  \"version\": \"1\",\n  \"project_id\": \"" + projectID + "\"\n}\n")
		if err := os.WriteFile(filepath.Join(path, "devctl.json"), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func mustCanonical(t *testing.T, path string) string {
	t.Helper()
	canonical, err := canonicalPath(path)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}
