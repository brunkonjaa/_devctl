package evidence

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"devctl/internal/model"
)

type Index struct {
	SchemaVersion string          `json:"schema_version"`
	BuiltAt       time.Time       `json:"built_at"`
	Runs          []RunIndexEntry `json:"runs"`
}

type RunIndexEntry struct {
	RunID       string       `json:"run_id"`
	Path        string       `json:"path"`
	Project     string       `json:"project,omitempty"`
	Revision    string       `json:"revision,omitempty"`
	Fingerprint string       `json:"fingerprint,omitempty"`
	Dirty       bool         `json:"dirty"`
	Overall     model.Status `json:"overall"`
	StartedAt   time.Time    `json:"started_at"`
	FinishedAt  time.Time    `json:"finished_at"`
	Checks      []CheckIndex `json:"checks,omitempty"`
}

type CheckIndex struct {
	ID           string       `json:"check_id"`
	Status       model.Status `json:"status"`
	Summary      string       `json:"summary,omitempty"`
	EvidencePath string       `json:"evidence_path,omitempty"`
}

func IndexPath(root string) string { return filepath.Join(root, ".devctl", "evidence", "index.json") }

func Rebuild(root string) (Index, error) {
	directory := filepath.Join(root, ".devctl", "evidence")
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return Index{SchemaVersion: "1", BuiltAt: time.Now().UTC(), Runs: []RunIndexEntry{}}, nil
	}
	if err != nil {
		return Index{}, err
	}
	index := Index{SchemaVersion: "1", BuiltAt: time.Now().UTC(), Runs: []RunIndexEntry{}}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "repair" || entry.Name() == "index.json" {
			continue
		}
		path := filepath.Join(directory, entry.Name(), "report.json")
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		var report model.Report
		if json.Unmarshal(data, &report) != nil || report.RunID == "" {
			continue
		}
		row := RunIndexEntry{RunID: report.RunID, Path: filepath.ToSlash(filepath.Join(".devctl", "evidence", entry.Name())), Overall: report.Overall, StartedAt: report.StartedAt, FinishedAt: report.FinishedAt}
		row.Revision = report.RepositoryRevision
		row.Fingerprint = report.RepositoryFingerprint
		row.Dirty = report.RepositoryDirty
		if report.Project != nil {
			row.Project = report.Project.Name
		}
		for _, check := range report.Checks {
			row.Checks = append(row.Checks, CheckIndex{ID: check.ID, Status: check.Status, Summary: check.Summary, EvidencePath: filepath.ToSlash(filepath.Join(row.Path, "checks", safeName(check.ID)+".json"))})
		}
		index.Runs = append(index.Runs, row)
	}
	sort.Slice(index.Runs, func(i, j int) bool { return runTime(index.Runs[i]).After(runTime(index.Runs[j])) })
	if err := writeIndex(IndexPath(root), index); err != nil {
		return Index{}, err
	}
	return index, nil
}

func Load(root string) (Index, error) {
	data, err := os.ReadFile(IndexPath(root))
	if err != nil {
		if os.IsNotExist(err) {
			return Rebuild(root)
		}
		return Index{}, err
	}
	var index Index
	if err := json.Unmarshal(data, &index); err != nil {
		return Index{}, fmt.Errorf("parse evidence index: %w", err)
	}
	if index.SchemaVersion != "1" {
		return Index{}, fmt.Errorf("unsupported evidence index schema %q", index.SchemaVersion)
	}
	return index, nil
}

func Latest(index Index) *RunIndexEntry {
	if len(index.Runs) == 0 {
		return nil
	}
	return &index.Runs[0]
}

func LatestSuccessful(index Index) *RunIndexEntry {
	for i := range index.Runs {
		if index.Runs[i].Overall == model.Pass || index.Runs[i].Overall == model.Warn {
			return &index.Runs[i]
		}
	}
	return nil
}

func FindCheck(index Index, checkID string) *CheckIndex {
	for _, run := range index.Runs {
		for i := range run.Checks {
			if strings.EqualFold(run.Checks[i].ID, checkID) {
				result := run.Checks[i]
				return &result
			}
		}
	}
	return nil
}

func runTime(entry RunIndexEntry) time.Time {
	if !entry.FinishedAt.IsZero() {
		return entry.FinishedAt
	}
	return entry.StartedAt
}

func writeIndex(path string, index Index) error {
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".evidence-index-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
