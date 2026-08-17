package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"devctl/internal/discovery"
	"devctl/internal/policy"
)

const (
	schemaVersion = "1"
	lockTimeout   = 5 * time.Second
	lockStale     = 30 * time.Second
)

var ErrActiveRun = errors.New("project already has an active run")

type Registry struct {
	SchemaVersion string                  `json:"schema_version"`
	UpdatedAt     time.Time               `json:"updated_at"`
	Projects      map[string]ProjectEntry `json:"projects"`
}

type ProjectEntry struct {
	ProjectID       string    `json:"project_id"`
	Name            string    `json:"name"`
	Path            string    `json:"path"`
	Technologies    []string  `json:"technologies,omitempty"`
	CurrentRunID    string    `json:"current_run_id,omitempty"`
	State           string    `json:"state"`
	Status          string    `json:"status,omitempty"`
	StartedAt       time.Time `json:"started_at,omitempty"`
	UpdatedAt       time.Time `json:"updated_at"`
	FinishedAt      time.Time `json:"finished_at,omitempty"`
	PID             int       `json:"pid,omitempty"`
	ProcessIdentity string    `json:"process_identity,omitempty"`
}

var processLock sync.Mutex

func Path() string {
	if configured := strings.TrimSpace(os.Getenv("DEVCTL_STATE_DIR")); configured != "" {
		return filepath.Join(configured, "registry.json")
	}
	if configDir, err := os.UserConfigDir(); err == nil && configDir != "" {
		return filepath.Join(configDir, "devctl", "registry.json")
	}
	return filepath.Join(".devctl-state", "registry.json")
}

func DetectProject(path string) (ProjectEntry, error) {
	project, err := discovery.Detect(path)
	if err != nil {
		return ProjectEntry{}, err
	}
	canonical, err := canonicalPath(project.Path)
	if err != nil {
		return ProjectEntry{}, err
	}
	project.Path = canonical
	if config, configErr := policy.Load(canonical); configErr == nil && config.ProjectID != "" {
		project.Identity = config.ProjectID
	}
	technologies := make([]string, 0, len(project.Technologies))
	for _, technology := range project.Technologies {
		technologies = append(technologies, technology.ID)
	}
	return ProjectEntry{
		ProjectID:    project.Identity,
		Name:         project.Name,
		Path:         canonical,
		Technologies: technologies,
		State:        "idle",
	}, nil
}

func Register(entry ProjectEntry) error {
	return update(func(registry *Registry) error {
		if err := validateEntry(registry, &entry); err != nil {
			return err
		}
		now := time.Now().UTC()
		if previous, ok := registry.Projects[entry.ProjectID]; ok {
			entry.CurrentRunID = previous.CurrentRunID
			entry.State = previous.State
			entry.Status = previous.Status
			entry.StartedAt = previous.StartedAt
			entry.FinishedAt = previous.FinishedAt
			entry.PID = previous.PID
			entry.ProcessIdentity = previous.ProcessIdentity
		}
		entry.UpdatedAt = now
		registry.Projects[entry.ProjectID] = entry
		return nil
	})
}

func Begin(entry ProjectEntry, runID string, pid int) error {
	if strings.TrimSpace(runID) == "" {
		return errors.New("run_id must not be empty")
	}
	processIdentity, ok := CurrentProcessIdentity(pid)
	if !ok {
		return fmt.Errorf("could not capture process start identity for pid %d", pid)
	}
	return update(func(registry *Registry) error {
		if err := validateEntry(registry, &entry); err != nil {
			return err
		}
		if previous, ok := registry.Projects[entry.ProjectID]; ok && previous.State == "running" && ProcessMatches(previous.PID, previous.ProcessIdentity) {
			return fmt.Errorf("%w: %q run %q", ErrActiveRun, entry.ProjectID, previous.CurrentRunID)
		}
		now := time.Now().UTC()
		entry.State = "running"
		entry.Status = ""
		entry.CurrentRunID = runID
		entry.StartedAt = now
		entry.UpdatedAt = now
		entry.FinishedAt = time.Time{}
		entry.PID = pid
		entry.ProcessIdentity = processIdentity
		registry.Projects[entry.ProjectID] = entry
		return nil
	})
}

func Finish(projectID, runID, status string) error {
	return update(func(registry *Registry) error {
		entry, ok := registry.Projects[projectID]
		if !ok {
			return fmt.Errorf("project %q is not registered", projectID)
		}
		if entry.CurrentRunID != runID {
			return fmt.Errorf("project %q run mismatch: current %q, received %q", projectID, entry.CurrentRunID, runID)
		}
		now := time.Now().UTC()
		entry.State = "finished"
		entry.Status = status
		entry.UpdatedAt = now
		entry.FinishedAt = now
		entry.PID = 0
		entry.ProcessIdentity = ""
		registry.Projects[projectID] = entry
		return nil
	})
}

func Load() (Registry, error) {
	var result Registry
	err := withLock(func() error {
		registry, err := loadLocked()
		if err != nil {
			return err
		}
		if normalizeStale(&registry) {
			if err := writeLocked(registry); err != nil {
				return err
			}
		}
		result = registry
		return nil
	})
	return result, err
}

func update(mutator func(*Registry) error) error {
	return withLock(func() error {
		registry, err := loadLocked()
		if err != nil {
			return err
		}
		if normalizeStale(&registry) {
			// The eventual write below persists the stale transition together with the update.
		}
		if err := mutator(&registry); err != nil {
			return err
		}
		return writeLocked(registry)
	})
}

func loadLocked() (Registry, error) {
	data, err := os.ReadFile(Path())
	if os.IsNotExist(err) {
		return Registry{SchemaVersion: schemaVersion, Projects: make(map[string]ProjectEntry)}, nil
	}
	if err != nil {
		return Registry{}, err
	}
	var registry Registry
	if err := json.Unmarshal(data, &registry); err != nil {
		return Registry{}, fmt.Errorf("parse registry %s: %w", Path(), err)
	}
	if registry.SchemaVersion != "" && registry.SchemaVersion != schemaVersion {
		return Registry{}, fmt.Errorf("unsupported registry schema version %q", registry.SchemaVersion)
	}
	if registry.Projects == nil {
		registry.Projects = make(map[string]ProjectEntry)
	}
	registry.SchemaVersion = schemaVersion
	return registry, nil
}

func writeLocked(registry Registry) error {
	registry.SchemaVersion = schemaVersion
	registry.UpdatedAt = time.Now().UTC()
	if registry.Projects == nil {
		registry.Projects = make(map[string]ProjectEntry)
	}
	path := Path()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temp, err := os.CreateTemp(filepath.Dir(path), ".registry-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	return nil
}

func validateEntry(registry *Registry, entry *ProjectEntry) error {
	entry.ProjectID = strings.TrimSpace(entry.ProjectID)
	if entry.ProjectID == "" {
		return errors.New("project_id must not be empty")
	}
	canonical, err := canonicalPath(entry.Path)
	if err != nil {
		return err
	}
	entry.Path = canonical
	if entry.Name == "" {
		entry.Name = filepath.Base(canonical)
	}
	for id, previous := range registry.Projects {
		if id == entry.ProjectID {
			if previous.Path != "" && previous.Path != canonical {
				if _, statErr := os.Stat(previous.Path); statErr == nil {
					return fmt.Errorf("project_id %q is already registered at %s", entry.ProjectID, previous.Path)
				}
			}
			continue
		}
		if previous.Path == canonical {
			return fmt.Errorf("path %s is already registered as project_id %q", canonical, id)
		}
	}
	return nil
}

func canonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}

func normalizeStale(registry *Registry) bool {
	changed := false
	for id, entry := range registry.Projects {
		if entry.State != "running" || ProcessMatches(entry.PID, entry.ProcessIdentity) {
			continue
		}
		entry.State = "stale"
		entry.Status = "STALE"
		entry.UpdatedAt = time.Now().UTC()
		entry.PID = 0
		entry.ProcessIdentity = ""
		registry.Projects[id] = entry
		changed = true
	}
	return changed
}

func withLock(work func() error) error {
	processLock.Lock()
	defer processLock.Unlock()
	lockPath := Path() + ".lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return err
	}
	deadline := time.Now().Add(lockTimeout)
	for {
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(file, "pid=%d\ncreated=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339Nano))
			_ = file.Close()
			defer os.Remove(lockPath)
			return work()
		}
		if !os.IsExist(err) {
			return err
		}
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > lockStale {
			if removeErr := os.Remove(lockPath); removeErr == nil {
				continue
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for registry lock %s", lockPath)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
