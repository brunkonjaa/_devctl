package discovery

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"devctl/internal/model"
)

var markerTechnologies = []struct {
	id      string
	markers []string
}{
	{"android-gradle", []string{"settings.gradle", "settings.gradle.kts", "build.gradle", "build.gradle.kts", "gradlew"}},
	{"node", []string{"package.json"}},
	{"python", []string{"pyproject.toml", "setup.py", "requirements.txt"}},
	{"rust", []string{"Cargo.toml"}},
	{"java-maven", []string{"pom.xml"}},
	{"dotnet", []string{"*.sln", "*.slnx"}},
	{"go", []string{"go.mod"}},
}

func Detect(path string) (model.Project, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return model.Project{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return model.Project{}, err
	}
	if !info.IsDir() {
		return model.Project{}, os.ErrInvalid
	}

	project := model.Project{Name: filepath.Base(abs), Path: abs, Identity: identity(abs)}
	for _, candidate := range markerTechnologies {
		found := make([]string, 0, len(candidate.markers))
		for _, marker := range candidate.markers {
			matches, matchErr := filepath.Glob(filepath.Join(abs, marker))
			if matchErr != nil {
				return model.Project{}, matchErr
			}
			for _, match := range matches {
				if fileInfo, statErr := os.Stat(match); statErr == nil && !fileInfo.IsDir() {
					found = append(found, filepath.Base(match))
				}
			}
		}
		if len(found) > 0 {
			project.Technologies = append(project.Technologies, model.Technology{
				ID:         candidate.id,
				Confidence: "marker",
				Markers:    unique(found),
			})
		}
	}
	return project, nil
}

func identity(path string) string {
	canonical := filepath.Clean(path)
	if runtime.GOOS == "windows" {
		canonical = strings.ToLower(canonical)
	}
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])[:16]
}

func Discover(root string) ([]model.Project, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(rootAbs)
	if err != nil {
		return nil, err
	}
	projects := make([]model.Project, 0)
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "_devctl" || entry.Name()[0] == '.' {
			continue
		}
		project, detectErr := Detect(filepath.Join(rootAbs, entry.Name()))
		if detectErr != nil {
			return nil, detectErr
		}
		if len(project.Technologies) > 0 {
			projects = append(projects, project)
		}
	}
	return projects, nil
}

func unique(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}
