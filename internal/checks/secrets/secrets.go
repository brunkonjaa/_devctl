package secrets

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"devctl/internal/model"
	"devctl/internal/scheduler"
)

var patterns = []struct {
	name     string
	severity string
	pattern  *regexp.Regexp
}{
	{"private-key", "high", regexp.MustCompile(`-----BEGIN (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----`)},
	{"aws-access-key", "high", regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
	{"generic-secret-assignment", "medium", regexp.MustCompile(`(?i)\b(?:api[_-]?key|secret|password|token)\s*[:=]\s*["']?[A-Za-z0-9/+=_-]{16,}`)},
}

func Check(project model.Project) scheduler.CheckSpec {
	return scheduler.CheckSpec{
		ID: "secret-scan",
		Run: func(context.Context) model.CheckResult {
			return scan(project.Path)
		},
	}
}

func scan(root string) model.CheckResult {
	result := model.CheckResult{ID: "secret-scan"}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && ignoredDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if shouldSkipFile(path) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() > 1<<20 {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.IndexByte(data, 0) >= 0 {
			return nil
		}
		for _, candidate := range patterns {
			if candidate.pattern.Match(data) {
				result.Findings = append(result.Findings, model.Finding{
					FindingID:    findingID(path, candidate.name),
					Severity:     candidate.severity,
					Issue:        "possible " + candidate.name,
					Action:       "inspect the match and remove or rotate any real secret",
					Path:         path,
					EvidencePath: path,
					Source:       "devctl-secret-patterns",
					ToolVersion:  "patterns-v1",
				})
			}
		}
		return nil
	})
	if err != nil {
		result.Status = model.Error
		result.Summary = "secret scan could not inspect the project"
		result.Reason = err.Error()
		return result
	}
	if len(result.Findings) > 0 {
		result.Status = model.Fail
		result.Blocking = true
		result.Summary = fmt.Sprintf("%d possible secret pattern matches found", len(result.Findings))
		return result
	}
	result.Status = model.Pass
	result.Summary = "No configured secret patterns matched"
	result.Reason = "Pattern scan only; this is not proof that the project contains no secrets"
	return result
}

func findingID(path, pattern string) string {
	digest := sha256.Sum256([]byte(path + "\x00" + pattern))
	return fmt.Sprintf("SEC-%x", digest[:6])
}

func ignoredDirectory(name string) bool {
	switch name {
	case ".git", ".gradle", ".devctl", "build", ".codex", "node_modules", "target":
		return true
	default:
		return false
	}
}

func shouldSkipFile(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	for _, suffix := range []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".jar", ".class", ".zip", ".exe", ".dll", ".so"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}
