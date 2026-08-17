package policy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

type AutomationConfig struct {
	ControlDocuments    []string `json:"control_documents,omitempty"`
	VerificationProfile string   `json:"verification_profile,omitempty"`
	Profiles            []string `json:"profiles,omitempty"`
	MaxAgentAttempts    int      `json:"max_agent_attempts,omitempty"`
}

type Config struct {
	Project          string                 `json:"project,omitempty"`
	ProjectID        string                 `json:"project_id,omitempty"`
	Version          string                 `json:"version,omitempty"`
	Automation       AutomationConfig       `json:"automation,omitempty"`
	Checks           map[string]CheckPolicy `json:"checks,omitempty"`
	DeclaredCheckIDs map[string]bool        `json:"-"`
}

func Load(projectPath string) (Config, error) {
	config := Config{Version: "1", Checks: make(map[string]CheckPolicy), DeclaredCheckIDs: make(map[string]bool)}
	centralPath := filepath.Join(filepath.Dir(projectPath), "_devctl", "config", "defaults.json")
	if err := mergeFile(&config, centralPath, true, false); err != nil {
		return Config{}, err
	}
	projectConfigPath := filepath.Join(projectPath, "devctl.json")
	if err := mergeFile(&config, projectConfigPath, false, true); err != nil {
		return Config{}, err
	}
	if config.Project == "" {
		config.Project = filepath.Base(projectPath)
	}
	if err := validate(config, filepath.Join(projectPath, "devctl.json")); err != nil {
		return Config{}, err
	}
	return config, nil
}

func mergeFile(config *Config, path string, optional, trackDeclaredChecks bool) error {
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
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&incoming); err != nil {
		return fmt.Errorf("parse policy %s: %w", path, err)
	}
	if err := validate(incoming, path); err != nil {
		return err
	}
	if incoming.Project != "" {
		config.Project = incoming.Project
	}
	if incoming.ProjectID != "" {
		config.ProjectID = incoming.ProjectID
	}
	if incoming.Version != "" {
		config.Version = incoming.Version
	}
	if incoming.Automation.ControlDocuments != nil {
		config.Automation.ControlDocuments = append([]string(nil), incoming.Automation.ControlDocuments...)
	}
	if incoming.Automation.VerificationProfile != "" {
		config.Automation.VerificationProfile = incoming.Automation.VerificationProfile
	}
	if incoming.Automation.Profiles != nil {
		config.Automation.Profiles = append([]string(nil), incoming.Automation.Profiles...)
	}
	if incoming.Automation.MaxAgentAttempts != 0 {
		config.Automation.MaxAgentAttempts = incoming.Automation.MaxAgentAttempts
	}
	for id, incomingPolicy := range incoming.Checks {
		if trackDeclaredChecks {
			if config.DeclaredCheckIDs == nil {
				config.DeclaredCheckIDs = make(map[string]bool)
			}
			config.DeclaredCheckIDs[id] = true
		}
		current := config.Checks[id]
		mergeCheckPolicy(&current, incomingPolicy)
		config.Checks[id] = current
	}
	return nil
}

func validate(config Config, path string) error {
	if config.Version != "" && config.Version != "1" {
		return fmt.Errorf("unsupported policy schema version %q in %s", config.Version, path)
	}
	if config.ProjectID != "" {
		trimmedID := strings.TrimSpace(config.ProjectID)
		if trimmedID != config.ProjectID || len(trimmedID) < 8 || len(trimmedID) > 128 {
			return fmt.Errorf("project_id must be a stable non-empty identifier in %s", path)
		}
	}
	if config.Automation.MaxAgentAttempts < 0 {
		return fmt.Errorf("automation.max_agent_attempts must not be negative in %s", path)
	}
	for _, document := range append(append([]string(nil), config.Automation.ControlDocuments...), config.Automation.Profiles...) {
		if strings.TrimSpace(document) == "" {
			return fmt.Errorf("automation paths must not be empty in %s", path)
		}
		if filepath.IsAbs(document) || filepath.Clean(document) == ".." || strings.HasPrefix(filepath.Clean(document), ".."+string(filepath.Separator)) {
			return fmt.Errorf("automation path must stay relative to the project in %s: %q", path, document)
		}
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

func ValidateCheckConfiguration(specs []scheduler.CheckSpec, config Config) error {
	known := make(map[string]bool, len(specs))
	for _, spec := range specs {
		known[spec.ID] = true
	}
	declared := config.DeclaredCheckIDs
	if declared == nil {
		declared = make(map[string]bool, len(config.Checks))
		for id := range config.Checks {
			declared[id] = true
		}
	}
	for id := range declared {
		if !known[id] {
			return fmt.Errorf("project policy configures unknown check %q", id)
		}
	}
	for _, spec := range specs {
		checkPolicy, ok := config.Checks[spec.ID]
		if !ok {
			continue
		}
		if checkPolicy.Required != nil && *checkPolicy.Required && checkPolicy.Enabled != nil && !*checkPolicy.Enabled {
			return fmt.Errorf("check %q cannot be both required and disabled", spec.ID)
		}
		if checkPolicy.Minimum != nil && (*checkPolicy.Minimum < 0 || *checkPolicy.Minimum > 100) {
			return fmt.Errorf("check %q minimum threshold must be between 0 and 100", spec.ID)
		}
		if checkPolicy.Preferred != nil && (*checkPolicy.Preferred < 0 || *checkPolicy.Preferred > 100) {
			return fmt.Errorf("check %q preferred threshold must be between 0 and 100", spec.ID)
		}
		if checkPolicy.Minimum != nil && checkPolicy.Preferred != nil && *checkPolicy.Minimum > *checkPolicy.Preferred {
			return fmt.Errorf("check %q minimum threshold cannot exceed preferred threshold", spec.ID)
		}
	}
	return nil
}

func Apply(report *model.Report, config Config) {
	for index := range report.Checks {
		check := &report.Checks[index]
		checkPolicy, ok := config.Checks[check.ID]
		if !ok {
			continue
		}
		applyCoverageThreshold(check, checkPolicy)
		if checkPolicy.Blocking != nil && check.Status != model.Pass {
			check.Blocking = *checkPolicy.Blocking
		}
		if checkPolicy.Required != nil && *checkPolicy.Required && unavailable(check.Status) {
			check.Blocking = true
		}
	}
}

func applyCoverageThreshold(check *model.CheckResult, checkPolicy CheckPolicy) {
	if check.ID != "android-coverage" || checkPolicy.Minimum == nil && checkPolicy.Preferred == nil {
		return
	}
	percentage, ok := coveragePercentage(check.Evidence)
	if !ok {
		return
	}

	check.Blocking = false
	switch {
	case checkPolicy.Minimum != nil && percentage < *checkPolicy.Minimum:
		check.Status = model.Fail
		check.Blocking = true
		check.Summary = fmt.Sprintf("Coverage %.1f%% is below blocking threshold %g%%", percentage, *checkPolicy.Minimum)
	case checkPolicy.Preferred != nil && percentage < *checkPolicy.Preferred:
		check.Status = model.Warn
		check.Summary = fmt.Sprintf("Coverage %.1f%% is below preferred target %g%%", percentage, *checkPolicy.Preferred)
	case checkPolicy.Preferred != nil:
		check.Status = model.Pass
		check.Summary = fmt.Sprintf("Coverage %.1f%% meets preferred target %g%%", percentage, *checkPolicy.Preferred)
	case checkPolicy.Minimum != nil:
		check.Status = model.Pass
		check.Summary = fmt.Sprintf("Coverage %.1f%% meets minimum threshold %g%%", percentage, *checkPolicy.Minimum)
	}
}

func coveragePercentage(evidence []model.Evidence) (float64, bool) {
	for _, item := range evidence {
		if item.Type == "coverage-report" && item.Coverage != nil {
			return *item.Coverage, true
		}
	}
	return 0, false
}

func unavailable(status model.Status) bool {
	switch status {
	case model.NotTested, model.InsufficientEvidence, model.RequiresReview, model.NotApplicable, model.Skip:
		return true
	default:
		return false
	}
}
