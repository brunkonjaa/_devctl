package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const schemaVersion = "1"

type Fingerprint struct {
	ProjectID string            `json:"project_id"`
	Head      string            `json:"head"`
	Dirty     bool              `json:"dirty"`
	Files     map[string]string `json:"files,omitempty"`
	Config    string            `json:"config_hash,omitempty"`
	Policy    string            `json:"policy_hash,omitempty"`
	Check     string            `json:"check_version,omitempty"`
	Devctl    string            `json:"devctl_version,omitempty"`
}

type Entry struct {
	SchemaVersion string          `json:"schema_version"`
	Key           string          `json:"key"`
	Kind          string          `json:"kind"`
	CreatedAt     time.Time       `json:"created_at"`
	Fingerprint   Fingerprint     `json:"fingerprint"`
	Payload       json.RawMessage `json:"payload"`
}

func Directory(root string) string { return filepath.Join(root, ".devctl", "cache", "entries") }

func Key(kind string, fingerprint Fingerprint) string {
	data, _ := json.Marshal(fingerprint)
	sum := sha256.Sum256(append([]byte(kind+"\x00"), data...))
	return hex.EncodeToString(sum[:])[:32]
}

func Put(root, kind string, fingerprint Fingerprint, payload any) (Entry, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return Entry{}, err
	}
	entry := Entry{SchemaVersion: schemaVersion, Key: Key(kind, fingerprint), Kind: kind, CreatedAt: time.Now().UTC(), Fingerprint: fingerprint, Payload: data}
	if strings.TrimSpace(kind) == "" {
		return Entry{}, errors.New("cache kind must not be empty")
	}
	if err := writeAtomic(filepath.Join(Directory(root), entry.Key+".json"), entry); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

func Get(root, kind string, current Fingerprint) (Entry, bool, error) {
	key := Key(kind, current)
	data, err := os.ReadFile(filepath.Join(Directory(root), key+".json"))
	if os.IsNotExist(err) {
		return Entry{}, false, nil
	}
	if err != nil {
		return Entry{}, false, err
	}
	var entry Entry
	if err := json.Unmarshal(data, &entry); err != nil {
		return Entry{}, false, fmt.Errorf("parse cache entry: %w", err)
	}
	if entry.SchemaVersion != schemaVersion || entry.Key != key || !sameFingerprint(entry.Fingerprint, current) {
		return Entry{}, false, nil
	}
	if !json.Valid(entry.Payload) {
		return Entry{}, false, errors.New("cache payload is invalid JSON")
	}
	return entry, true, nil
}

func List(root string) ([]Entry, error) {
	entries := []Entry{}
	files, err := os.ReadDir(Directory(root))
	if os.IsNotExist(err) {
		return entries, nil
	}
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if file.IsDir() || filepath.Ext(file.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(Directory(root), file.Name()))
		if err != nil {
			return nil, err
		}
		var entry Entry
		if err := json.Unmarshal(data, &entry); err != nil {
			continue
		}
		if entry.SchemaVersion == schemaVersion {
			entries = append(entries, entry)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].CreatedAt.After(entries[j].CreatedAt) })
	return entries, nil
}

func Clear(root string) error { return os.RemoveAll(Directory(root)) }

func sameFingerprint(a, b Fingerprint) bool {
	if a.ProjectID != b.ProjectID || a.Head != b.Head || a.Dirty != b.Dirty || a.Config != b.Config || a.Policy != b.Policy || a.Check != b.Check || a.Devctl != b.Devctl || len(a.Files) != len(b.Files) {
		return false
	}
	for path, hash := range a.Files {
		if b.Files[path] != hash {
			return false
		}
	}
	return true
}

func HashFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func writeAtomic(path string, value Entry) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".cache-*.tmp")
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
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
