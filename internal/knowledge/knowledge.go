package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const schemaVersion = "1"
const maxLessons = 2000

type Lesson struct {
	SchemaVersion string     `json:"schema_version"`
	ID            string     `json:"id"`
	Project       string     `json:"project,omitempty"`
	Adapter       string     `json:"adapter,omitempty"`
	Revision      string     `json:"revision,omitempty"`
	Branch        string     `json:"branch,omitempty"`
	RecordedAt    time.Time  `json:"recorded_at"`
	Check         string     `json:"check,omitempty"`
	Problem       string     `json:"problem"`
	Signature     string     `json:"signature"`
	Normalized    string     `json:"normalized_error,omitempty"`
	Files         []string   `json:"files,omitempty"`
	RootCause     string     `json:"root_cause,omitempty"`
	Attempt       string     `json:"attempt,omitempty"`
	Success       bool       `json:"success"`
	Solution      string     `json:"solution,omitempty"`
	Commands      []string   `json:"commands,omitempty"`
	Evidence      []string   `json:"evidence,omitempty"`
	Status        string     `json:"status,omitempty"`
	Tool          string     `json:"tool,omitempty"`
	Confidence    string     `json:"confidence,omitempty"`
	InvalidatedAt *time.Time `json:"invalidated_at,omitempty"`
}

type Store struct {
	SchemaVersion string   `json:"schema_version"`
	Lessons       []Lesson `json:"lessons"`
}

type Query struct {
	Project   string
	Check     string
	Signature string
	Path      string
	Adapter   string
	Tool      string
	Limit     int
}

func Path(root string) string { return filepath.Join(root, ".devctl", "knowledge", "lessons.json") }

func Normalize(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(value, " ")
	return strings.Join(strings.Fields(value), " ")
}

func Signature(problem, rootCause string) string {
	sum := sha256.Sum256([]byte(Normalize(problem) + "\x00" + Normalize(rootCause)))
	return hex.EncodeToString(sum[:])[:24]
}

func Read(root string) (Store, error) {
	data, err := os.ReadFile(Path(root))
	if os.IsNotExist(err) {
		return Store{SchemaVersion: schemaVersion, Lessons: []Lesson{}}, nil
	}
	if err != nil {
		return Store{}, err
	}
	var store Store
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&store); err != nil {
		return Store{}, fmt.Errorf("parse lessons: %w", err)
	}
	if store.SchemaVersion != schemaVersion {
		return Store{}, fmt.Errorf("unsupported lessons schema version %q", store.SchemaVersion)
	}
	if len(store.Lessons) > maxLessons {
		return Store{}, errors.New("lessons store exceeds bounded record limit")
	}
	for _, lesson := range store.Lessons {
		if err := validate(lesson); err != nil {
			return Store{}, err
		}
	}
	return store, nil
}

func Write(root string, lesson Lesson) (Lesson, error) {
	if err := validate(lesson); err != nil {
		return Lesson{}, err
	}
	store, err := Read(root)
	if err != nil {
		return Lesson{}, err
	}
	lesson.SchemaVersion, lesson.Signature = schemaVersion, Signature(lesson.Problem, lesson.RootCause)
	lesson.Normalized = Normalize(lesson.Problem)
	if lesson.RecordedAt.IsZero() {
		lesson.RecordedAt = time.Now().UTC()
	}
	if lesson.ID == "" {
		outcome := "failed"
		if lesson.Success {
			outcome = "success"
		}
		lesson.ID = lesson.Signature + "-" + outcome
	}
	updated := false
	for i := range store.Lessons {
		if store.Lessons[i].ID == lesson.ID || (store.Lessons[i].Signature == lesson.Signature && store.Lessons[i].Success == lesson.Success) {
			store.Lessons[i] = lesson
			updated = true
			break
		}
	}
	if !updated {
		store.Lessons = append(store.Lessons, lesson)
	}
	if len(store.Lessons) > maxLessons {
		return Lesson{}, errors.New("lessons store exceeds bounded record limit")
	}
	sort.SliceStable(store.Lessons, func(i, j int) bool { return store.Lessons[i].RecordedAt.After(store.Lessons[j].RecordedAt) })
	store.SchemaVersion = schemaVersion
	if err := writeAtomic(Path(root), store); err != nil {
		return Lesson{}, err
	}
	return lesson, nil
}

func QueryLessons(root string, query Query) ([]Lesson, error) {
	store, err := Read(root)
	if err != nil {
		return nil, err
	}
	limit := query.Limit
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	type ranked struct {
		lesson Lesson
		score  int
	}
	rows := make([]ranked, 0)
	for _, lesson := range store.Lessons {
		if lesson.InvalidatedAt != nil {
			continue
		}
		score := 0
		if query.Project != "" {
			if lesson.Project != query.Project {
				continue
			}
			score += 8
		}
		if query.Check != "" {
			if !strings.EqualFold(lesson.Check, query.Check) {
				continue
			}
			score += 6
		}
		if query.Adapter != "" {
			if !strings.EqualFold(lesson.Adapter, query.Adapter) {
				continue
			}
			score += 4
		}
		if query.Tool != "" {
			if !strings.EqualFold(lesson.Tool, query.Tool) {
				continue
			}
			score += 3
		}
		if query.Signature != "" {
			if lesson.Signature != query.Signature && Normalize(lesson.Normalized) != Normalize(query.Signature) {
				continue
			}
			score += 10
		}
		if query.Path != "" {
			found := false
			for _, path := range lesson.Files {
				if strings.Contains(filepath.ToSlash(path), filepath.ToSlash(query.Path)) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
			score += 5
		}
		if lesson.Success {
			score++
		}
		rows = append(rows, ranked{lesson, score})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].score != rows[j].score {
			return rows[i].score > rows[j].score
		}
		return rows[i].lesson.RecordedAt.After(rows[j].lesson.RecordedAt)
	})
	if len(rows) > limit {
		rows = rows[:limit]
	}
	result := make([]Lesson, len(rows))
	for i := range rows {
		result[i] = rows[i].lesson
	}
	return result, nil
}

func validate(lesson Lesson) error {
	if strings.TrimSpace(lesson.Problem) == "" {
		return errors.New("lesson problem must not be empty")
	}
	if len(lesson.Problem) > 4096 || len(lesson.RootCause) > 4096 || len(lesson.Solution) > 4096 {
		return errors.New("lesson text exceeds bounded length")
	}
	if lesson.ID != "" && (strings.ContainsAny(lesson.ID, `/\\`) || len(lesson.ID) > 128) {
		return errors.New("lesson id is invalid")
	}
	return nil
}

func writeAtomic(path string, value Store) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".lessons-*.tmp")
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
