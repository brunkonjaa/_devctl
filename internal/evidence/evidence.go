package evidence

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"devctl/internal/model"
)

func Write(projectPath string, report model.Report) (string, error) {
	relativeRoot := filepath.Join(".devctl", "evidence", report.RunID)
	root := filepath.Join(projectPath, relativeRoot)
	if err := os.MkdirAll(filepath.Join(root, "checks"), 0o700); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(root, "raw"), 0o700); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(root, "artifacts"), 0o700); err != nil {
		return "", err
	}

	report.EvidencePath = filepath.ToSlash(relativeRoot)
	if err := writeJSON(filepath.Join(root, "report.json"), report); err != nil {
		return "", err
	}
	projectName := "unknown"
	if report.Project != nil {
		projectName = report.Project.Name
	}
	var summary strings.Builder
	fmt.Fprintf(&summary, "PROJECT: %s\n\n", projectName)
	for _, check := range report.Checks {
		fmt.Fprintf(&summary, "%-32s %s — %s\n", strings.ToUpper(check.ID), check.Status, check.Summary)
	}
	fmt.Fprintf(&summary, "\nOVERALL: %s\n", report.Overall)
	if err := os.WriteFile(filepath.Join(root, "summary.txt"), []byte(summary.String()), 0o600); err != nil {
		return "", err
	}
	for _, check := range report.Checks {
		name := safeName(check.ID)
		if err := writeJSON(filepath.Join(root, "checks", name+".json"), check); err != nil {
			return "", err
		}
		if check.RawOutput != "" {
			if err := os.WriteFile(filepath.Join(root, "raw", name+".log"), []byte(check.RawOutput), 0o600); err != nil {
				return "", err
			}
		}
		for _, item := range check.Evidence {
			if item.Type != "coverage-report" || item.Path == "" {
				continue
			}
			source := item.Path
			if !filepath.IsAbs(source) {
				source = filepath.Join(projectPath, source)
			}
			data, err := os.ReadFile(source)
			if err != nil {
				return "", fmt.Errorf("copy coverage evidence %q: %w", item.Path, err)
			}
			if err := os.WriteFile(filepath.Join(root, "artifacts", name+".xml"), data, 0o600); err != nil {
				return "", err
			}
		}
	}
	return filepath.ToSlash(relativeRoot), nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func safeName(value string) string {
	var builder strings.Builder
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '-' || character == '_' {
			builder.WriteRune(character)
		} else {
			builder.WriteByte('_')
		}
	}
	return builder.String()
}
