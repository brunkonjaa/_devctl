package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"devctl/internal/model"
)

const schemaVersion = "1"

func StatePath() string {
	if configured := os.Getenv("DEVCTL_STATE_DIR"); configured != "" {
		return filepath.Join(configured, "session.json")
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(".", ".devctl-state", "session.json")
	}
	return filepath.Join(base, "devctl", "session.json")
}

func Record(state model.SessionState) (string, error) {
	if state.Project == "" || state.ProjectPath == "" {
		return "", errors.New("project and project_path are required")
	}
	if state.CurrentTask == "" {
		if previous, err := Load(); err == nil {
			state.CurrentTask = previous.CurrentTask
		}
	}
	abs, err := filepath.Abs(state.ProjectPath)
	if err != nil {
		return "", fmt.Errorf("resolve project path: %w", err)
	}
	state.ProjectPath = abs
	state.SchemaVersion = schemaVersion
	state.UpdatedAt = time.Now().UTC()
	path := StatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".session-*.tmp")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return "", err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return "", err
	}
	return path, nil
}

func Load() (model.SessionState, error) {
	data, err := os.ReadFile(StatePath())
	if err != nil {
		return model.SessionState{}, err
	}
	var state model.SessionState
	if err := json.Unmarshal(data, &state); err != nil {
		return model.SessionState{}, fmt.Errorf("parse session state: %w", err)
	}
	if state.SchemaVersion != schemaVersion {
		return model.SessionState{}, fmt.Errorf("unsupported session schema %q", state.SchemaVersion)
	}
	if state.Project == "" || state.ProjectPath == "" || state.UpdatedAt.IsZero() {
		return model.SessionState{}, errors.New("session state is incomplete")
	}
	return state, nil
}
