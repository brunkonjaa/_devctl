package insight

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"devctl/internal/cache"
	"devctl/internal/evidence"
	"devctl/internal/gitstate"
	"devctl/internal/knowledge"
	"devctl/internal/model"
	"devctl/internal/policy"
	"devctl/internal/registry"
)

const maxContextBytes = 128 * 1024

type Context struct {
	SchemaVersion   string                  `json:"schema_version"`
	GeneratedAt     time.Time               `json:"generated_at"`
	Project         model.Project           `json:"project"`
	Repository      RepositoryState         `json:"repository"`
	CurrentTask     string                  `json:"current_task,omitempty"`
	Constraints     Constraints             `json:"constraints"`
	ChangedFiles    []string                `json:"changed_files,omitempty"`
	CurrentFailures []FailureSummary        `json:"current_failures,omitempty"`
	RelevantLessons []knowledge.Lesson      `json:"relevant_lessons,omitempty"`
	LastSuccessful  *evidence.RunIndexEntry `json:"last_successful_verification,omitempty"`
	LatestEvidence  string                  `json:"latest_evidence,omitempty"`
	SuggestedChecks []string                `json:"suggested_checks,omitempty"`
	Cache           CacheSummary            `json:"cache"`
	Authority       string                  `json:"authority"`
}

type RepositoryState struct {
	Head            string `json:"head,omitempty"`
	Branch          string `json:"branch,omitempty"`
	Dirty           bool   `json:"dirty"`
	EvidenceCurrent bool   `json:"evidence_current"`
}
type Constraints struct {
	ConfigPath           string   `json:"config_path,omitempty"`
	PolicyVersion        string   `json:"policy_version,omitempty"`
	RepairAllowed        bool     `json:"repair_allowed"`
	ForbiddenRepairAreas []string `json:"forbidden_repair_areas,omitempty"`
}
type FailureSummary struct {
	CheckID      string       `json:"check_id"`
	Status       model.Status `json:"status"`
	Summary      string       `json:"summary"`
	EvidencePath string       `json:"evidence_path,omitempty"`
}
type CacheSummary struct {
	Entries  int    `json:"entries"`
	Validity string `json:"validity"`
	Note     string `json:"note,omitempty"`
}

func Build(root string) (Context, error) {
	project, err := registry.DetectProject(root)
	if err != nil {
		return Context{}, err
	}
	detected := model.Project{Name: project.Name, Path: project.Path, Identity: project.ProjectID}
	for _, id := range project.Technologies {
		detected.Technologies = append(detected.Technologies, model.Technology{ID: id, Confidence: "registry"})
	}
	head := git(root, "rev-parse", "HEAD")
	branch := git(root, "branch", "--show-current")
	status := git(root, "status", "--porcelain=v1", "--untracked-files=all")
	index, err := evidence.Rebuild(root)
	if err != nil {
		return Context{}, err
	}
	result := Context{SchemaVersion: "1", GeneratedAt: time.Now().UTC(), Project: detected, Repository: RepositoryState{Head: head, Branch: branch, Dirty: strings.TrimSpace(status) != ""}, Constraints: Constraints{ConfigPath: filepath.ToSlash(filepath.Join(project.Path, "devctl.json")), RepairAllowed: true, ForbiddenRepairAreas: []string{"tests-and-fixtures", "devctl-policy-and-configuration", "git-metadata", "generated-evidence-and-journals", "build-outputs-and-dependency-caches"}}, Authority: "Current repository evidence and deterministic verification override historical lessons and cache entries."}
	if config, configErr := policy.Load(project.Path); configErr == nil {
		result.Constraints.PolicyVersion = config.Version
	}
	result.ChangedFiles = changedFiles(status)
	if len(index.Runs) > 0 {
		result.LatestEvidence = index.Runs[0].Path
		result.LastSuccessful = evidence.LatestSuccessful(index)
		for _, check := range index.Runs[0].Checks {
			if check.Status == model.Fail || check.Status == model.Error || check.Status == model.NotTested || check.Status == model.InsufficientEvidence {
				result.CurrentFailures = append(result.CurrentFailures, FailureSummary{CheckID: check.ID, Status: check.Status, Summary: check.Summary, EvidencePath: check.EvidencePath})
			}
		}
	}
	for _, failure := range result.CurrentFailures {
		lessons, queryErr := knowledge.QueryLessons(root, knowledge.Query{Project: project.ProjectID, Check: failure.CheckID, Limit: 3})
		if queryErr == nil {
			result.RelevantLessons = append(result.RelevantLessons, lessons...)
		}
		result.SuggestedChecks = append(result.SuggestedChecks, failure.CheckID)
	}
	result.SuggestedChecks = unique(result.SuggestedChecks)
	sort.Strings(result.SuggestedChecks)
	entries, cacheErr := cache.List(root)
	if cacheErr == nil {
		result.Cache.Entries = len(entries)
		if len(entries) == 0 {
			result.Cache.Validity = "MISS"
		} else {
			result.Cache.Validity = "REVALIDATE"
			result.Cache.Note = "Entries are advisory and must match current repository inputs before reuse."
		}
	} else {
		result.Cache.Validity = "UNAVAILABLE"
		result.Cache.Note = cacheErr.Error()
	}
	if latest := evidence.Latest(index); latest != nil {
		currentFingerprint, fingerprintErr := gitstate.Fingerprint(project.Path)
		result.Repository.EvidenceCurrent = fingerprintErr == nil && latest.Fingerprint != "" && latest.Fingerprint == currentFingerprint
	}
	if data, readErr := osRead(filepath.Join(root, "devctl.json")); readErr == nil {
		var raw map[string]any
		if json.Unmarshal(data, &raw) == nil {
			if v, ok := raw["current_task"].(string); ok {
				result.CurrentTask = v
			}
		}
	}
	return result, nil
}

func git(root string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	data, err := command.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
func osRead(path string) ([]byte, error) { return os.ReadFile(path) }
func unique(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, v := range values {
		if v != "" && !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}

func changedFiles(status string) []string {
	var result []string
	for _, line := range strings.Split(status, "\n") {
		line = strings.TrimSpace(line)
		if len(line) < 3 {
			continue
		}
		path := strings.TrimSpace(line[2:])
		if strings.Contains(path, " -> ") {
			path = strings.TrimSpace(strings.Split(path, " -> ")[1])
		}
		if path != "" {
			result = append(result, filepath.ToSlash(path))
		}
	}
	return unique(result)
}
func JSON(root string) ([]byte, error) {
	value, err := Build(root)
	if err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	if len(data) > maxContextBytes {
		return nil, fmt.Errorf("context package exceeds %d bytes", maxContextBytes)
	}
	return append(data, '\n'), nil
}
