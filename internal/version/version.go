package version

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
)

const Value = "0.1.0"

var Commit = "unknown"

type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Dirty     bool   `json:"dirty"`
	GoVersion string `json:"go_version"`
}

func Current() Info {
	info := Info{Version: Value, Commit: Commit, GoVersion: runtime.Version()}
	if build, ok := debug.ReadBuildInfo(); ok {
		if info.GoVersion == "" {
			info.GoVersion = build.GoVersion
		}
		for _, setting := range build.Settings {
			switch setting.Key {
			case "vcs.revision":
				if info.Commit == "unknown" {
					info.Commit = setting.Value
				}
			case "vcs.modified":
				info.Dirty = setting.Value == "true"
			}
		}
	}
	if info.Commit == "unknown" {
		if commit, dirty, ok := sourceRepositoryProvenance(); ok {
			info.Commit = commit
			info.Dirty = dirty
		}
	}
	return info
}

func sourceRepositoryProvenance() (string, bool, bool) {
	directory, err := os.Getwd()
	if err != nil {
		return "", false, false
	}
	for {
		module, readErr := os.ReadFile(filepath.Join(directory, "go.mod"))
		if readErr == nil && strings.Contains(string(module), "module devctl") {
			if _, statErr := os.Stat(filepath.Join(directory, ".git")); statErr == nil {
				commitCommand := exec.Command("git", "rev-parse", "HEAD")
				commitCommand.Dir = directory
				commitOutput, commitErr := commitCommand.Output()
				if commitErr != nil {
					return "", false, false
				}
				statusCommand := exec.Command("git", "status", "--porcelain")
				statusCommand.Dir = directory
				statusOutput, statusErr := statusCommand.Output()
				if statusErr != nil {
					return "", false, false
				}
				return strings.TrimSpace(string(commitOutput)), len(statusOutput) > 0, true
			}
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", false, false
		}
		directory = parent
	}
}
