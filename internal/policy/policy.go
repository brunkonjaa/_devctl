package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"devctl/internal/model"
	"devctl/internal/scheduler"
)

type CheckPolicy struct {
	Enabled   *bool    `json:"enabled,omitempty"`
	Required  *bool    `json:"required,omitempty"`
	Blocking  *bool    `json:"blocking,omitempty"`
	Preferred *float64 `json:"preferred,omitempty"`
	Minimum   *float64 `json:"minimum,omitempty"`
}

type Config struct {
	Project string                 `json:"project,omitempty"`
	Version string                 `json:"version,omitempty"`
	Checks  map[string]CheckPolicy `json:"checks,omitempty"`
}

func Load(projectPath string) (Config, error) {
	config := Config{Checks: make(map[string]CheckPolicy)}
	centralPath := filepath.Join(filepath.Dir(projectPath), "_devctl", "config", "defaults.json")
	if err := mergeFile(&config, centralPath, true); err != nil {
		return Config{}, err
	}
	projectConfigPath := filepath.Join(projectPath, "devctl.json")
	if err := mergeFile(&config, projectConfigPath, false); err != nil {
		return Config{}, err
	}
	if config.Project == "" {
		config.Project = filepath.Base(projectPath)
	}
	return config, nil
}

func mergeFile(config *Config, path string, optional bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if optional && os.IsNotExist(err) {
			return nil
		}
		if os.IsNotExist(err) && !optional {
			return nil
		}
		return err
	}
	var incoming Config
	if err := json.Unmarshal(data, &incoming); err != nil {
		return fmt.Errorf("parse policy %s: %w", path, err)
	}
	if incoming.Project != "" {
		config.Project = incoming.Project
	}
	if incoming.Version != "" {
		config.Version = incoming.Version
	}
	for id, incomingPolicy := range incoming.Checks {
		current := config.Checks[id]
		mergeCheckPolicy(&current, incomingPolicy)
		config.Checks[id] = current
	}
	return nil
}

func mergeCheckPolicy(target *CheckPolicy, source CheckPolicy) {
	if source.Enabled != nil {
		target.Enabled = source.Enabled
	}
	if source.Required != nil {
		target.Required = source.Required
	}
	if source.Blocking != nil {
		target.Blocking = source.Blocking
	}
	if source.Preferred != nil {
		target.Preferred = source.Preferred
	}
	if source.Minimum != nil {
		target.Minimum = source.Minimum
	}
}

func FilterChecks(specs []scheduler.CheckSpec, config Config) []scheduler.CheckSpec {
	selected := make(map[string]scheduler.CheckSpec, len(specs))
	for _, spec := range specs {
		if check, ok := config.Checks[spec.ID]; ok && check.Enabled != nil && !*check.Enabled {
			continue
		}
		selected[spec.ID] = spec
	}
	changed := true
	for changed {
		changed = false
		for id, spec := range selected {
			for _, dependency := range spec.Requires {
				if _, exists := selected[dependency]; !exists {
					delete(selected, id)
					changed = true
					break
				}
			}
		}
	}
	filtered := make([]scheduler.CheckSpec, 0, len(specs))
	for _, spec := range specs {
		if _, exists := selected[spec.ID]; exists {
			filtered = append(filtered, spec)
		}
	}
	return filtered
}

func Apply(report *model.Report, config Config) {
	for index := range report.Checks {
		check := &report.Checks[index]
		checkPolicy, ok := config.Checks[check.ID]
		if !ok {
			continue
		}
		if checkPolicy.Blocking != nil && check.Status != model.Pass {
			check.Blocking = *checkPolicy.Blocking
		}
		if checkPolicy.Required != nil && *checkPolicy.Required && unavailable(check.Status) {
			check.Blocking = true
		}
	}
}

func unavailable(status model.Status) bool {
	switch status {
	case model.NotTested, model.InsufficientEvidence, model.RequiresReview, model.NotApplicable, model.Skip:
		return true
	default:
		return false
	}
}
