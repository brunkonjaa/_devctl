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
	canonicalProject, err := filepath.EvalSymlinks(projectPath)
	if err != nil {
		return "", fmt.Errorf("resolve project path: %w", err)
	}
	projectInfo, err := os.Stat(canonicalProject)
	if err != nil || !projectInfo.IsDir() {
		return "", fmt.Errorf("project path is not a directory")
	}
	relativeRoot := filepath.Join(".devctl", "evidence", report.RunID)
	if report.RunID == "" || filepath.Base(report.RunID) != report.RunID || strings.ContainsAny(report.RunID, `\\/:`) {
		return "", fmt.Errorf("invalid evidence run id")
	}
	root := filepath.Join(canonicalProject, relativeRoot)
	if err := ensureContainedDirectory(filepath.Join(canonicalProject, ".devctl"), canonicalProject); err != nil {
		return "", err
	}
	if err := ensureContainedDirectory(filepath.Join(canonicalProject, ".devctl", "evidence"), canonicalProject); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(root, "checks"), 0o700); err != nil {
		return "", fmt.Errorf("create checks evidence directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "raw"), 0o700); err != nil {
		return "", fmt.Errorf("create raw evidence directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "artifacts"), 0o700); err != nil {
		return "", fmt.Errorf("create artifact evidence directory: %w", err)
	}

	report.EvidencePath = filepath.ToSlash(relativeRoot)
	if err := writeJSON(filepath.Join(root, "report.json"), report); err != nil {
		return "", fmt.Errorf("write report: %w", err)
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
		return "", fmt.Errorf("write summary: %w", err)
	}
	for _, check := range report.Checks {
		name := safeName(check.ID)
		if err := writeJSON(filepath.Join(root, "checks", name+".json"), check); err != nil {
			return "", fmt.Errorf("write check %s: %w", check.ID, err)
		}
		if check.RawOutput != "" {
			if err := os.WriteFile(filepath.Join(root, "raw", name+".log"), []byte(check.RawOutput), 0o600); err != nil {
				return "", fmt.Errorf("write raw output %s: %w", check.ID, err)
			}
		}
		for _, item := range check.Evidence {
			if item.Type != "coverage-report" || item.Path == "" {
				continue
			}
			source := item.Path
			if !filepath.IsAbs(source) {
				source = filepath.Join(canonicalProject, source)
			}
			if err := ensureContainedFile(source, canonicalProject); err != nil {
				marker := filepath.Join(root, "artifacts", name+"-not-copied.txt")
				if writeErr := os.WriteFile(marker, []byte("Coverage artifact was not copied because its canonical target was outside the permitted project boundary.\n"), 0o600); writeErr != nil {
					return "", fmt.Errorf("write coverage boundary marker: %w", writeErr)
				}
				continue
			}
			data, err := os.ReadFile(source)
			if err != nil {
				return "", fmt.Errorf("copy coverage evidence %q: %w", item.Path, err)
			}
			if err := os.WriteFile(filepath.Join(root, "artifacts", name+".xml"), data, 0o600); err != nil {
				return "", fmt.Errorf("write artifact %s: %w", check.ID, err)
			}
		}
	}
	return filepath.ToSlash(relativeRoot), nil
}

func ensureContainedDirectory(path, root string) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink == 0 && !info.IsDir() {
			return fmt.Errorf("evidence directory is not a normal directory: %s", path)
		}
	} else if os.IsNotExist(err) {
		if err := os.Mkdir(path, 0o700); err != nil && !os.IsExist(err) {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("evidence directory could not be contained: %s", path)
		}
	} else {
		return err
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	if !contained(canonical, canonicalRoot) {
		return fmt.Errorf("evidence path escapes project: %s", path)
	}
	canonicalInfo, err := os.Stat(canonical)
	if err != nil || !canonicalInfo.IsDir() {
		return fmt.Errorf("evidence path is not a directory: %s", path)
	}
	return nil
}

func ensureContainedFile(path, root string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("evidence source is not a regular file: %s", path)
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	if !contained(canonical, canonicalRoot) {
		return fmt.Errorf("evidence source escapes project: %s", path)
	}
	canonicalInfo, err := os.Stat(canonical)
	if err != nil || !canonicalInfo.Mode().IsRegular() {
		return fmt.Errorf("evidence source is not a regular file: %s", path)
	}
	return nil
}

func contained(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
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
