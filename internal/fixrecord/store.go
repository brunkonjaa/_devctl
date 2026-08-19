package fixrecord

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"devctl/internal/strictjson"
)

const (
	maxRecords   = 2000
	maxListLimit = 100
)

var (
	ErrRecordExists   = errors.New("Fix Record already exists")
	ErrRecordNotFound = errors.New("Fix Record not found")
)

func Directory(root string) string {
	return filepath.Join(root, ".devctl", "knowledge", "fix-records")
}

func Path(root, id string) string {
	return filepath.Join(Directory(root), id+".json")
}

func ReadCandidateFile(path string) (Candidate, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Candidate{}, fmt.Errorf("inspect Fix Record candidate: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return Candidate{}, errors.New("Fix Record candidate is not a normal file")
	}
	file, err := os.Open(path)
	if err != nil {
		return Candidate{}, fmt.Errorf("open Fix Record candidate: %w", err)
	}
	defer file.Close()
	return DecodeCandidate(file)
}

func Create(root string, candidate Candidate, options Options) (Record, error) {
	canonicalRoot, err := canonicalProject(root)
	if err != nil {
		return Record{}, err
	}
	if err := validateCandidate(candidate); err != nil {
		return Record{}, err
	}
	existingRecords, err := auditRecordStore(canonicalRoot)
	if err != nil {
		return Record{}, err
	}
	if len(existingRecords) >= maxRecords {
		return Record{}, fmt.Errorf("Fix Record store has reached the %d-record limit", maxRecords)
	}
	for _, existing := range existingRecords {
		if existing.ID == candidate.ID {
			return Record{}, ErrRecordExists
		}
	}
	record, err := closeCandidate(canonicalRoot, candidate, options)
	if err != nil {
		return Record{}, err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return Record{}, fmt.Errorf("encode Fix Record: %w", err)
	}
	data = append(data, '\n')
	if len(data) > MaxRecordBytes {
		return Record{}, fmt.Errorf("Fix Record exceeds the %d-byte limit", MaxRecordBytes)
	}
	directory, err := ensureRecordDirectory(canonicalRoot)
	if err != nil {
		return Record{}, err
	}
	path := filepath.Join(directory, record.ID+".json")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return Record{}, ErrRecordExists
		}
		return Record{}, fmt.Errorf("create Fix Record: %w", err)
	}
	removePartial := true
	defer func() {
		if removePartial {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return Record{}, fmt.Errorf("write Fix Record: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return Record{}, fmt.Errorf("sync Fix Record: %w", err)
	}
	if err := file.Close(); err != nil {
		return Record{}, fmt.Errorf("close Fix Record: %w", err)
	}
	removePartial = false
	return record, nil
}

func Show(root, id string) (Record, error) {
	if !validFixID(id) {
		return Record{}, errors.New("Fix Record ID is invalid")
	}
	canonicalRoot, err := canonicalProject(root)
	if err != nil {
		return Record{}, err
	}
	return readRecord(canonicalRoot, id)
}

func List(root string, limit int) ([]Summary, error) {
	if limit < 1 || limit > maxListLimit {
		return nil, fmt.Errorf("Fix Record list limit must be between 1 and %d", maxListLimit)
	}
	canonicalRoot, err := canonicalProject(root)
	if err != nil {
		return nil, err
	}
	records, err := auditRecordStore(canonicalRoot)
	if err != nil {
		return nil, err
	}
	sort.Slice(records, func(left, right int) bool {
		if records[left].RecordedAt.Equal(records[right].RecordedAt) {
			return records[left].ID < records[right].ID
		}
		return records[left].RecordedAt.After(records[right].RecordedAt)
	})
	if len(records) > limit {
		records = records[:limit]
	}
	summaries := make([]Summary, 0, len(records))
	for _, record := range records {
		summaries = append(summaries, Summary{
			ID: record.ID, Status: record.Status, RecordedAt: record.RecordedAt,
			Title: record.Title, ProjectID: record.ProjectID, PreRunID: record.PreRun.RunID,
			PostRunID: record.PostRun.RunID, Supersedes: record.Supersedes, RecordHash: record.RecordHash,
		})
	}
	return summaries, nil
}

func readRecord(canonicalRoot, id string) (Record, error) {
	directory, exists, err := findRecordDirectory(canonicalRoot)
	if err != nil {
		return Record{}, err
	}
	if !exists {
		return Record{}, ErrRecordNotFound
	}
	path := filepath.Join(directory, id+".json")
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Record{}, ErrRecordNotFound
		}
		return Record{}, fmt.Errorf("inspect Fix Record: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return Record{}, errors.New("Fix Record is not a normal file")
	}
	canonicalPath, err := filepath.EvalSymlinks(path)
	if err != nil || !contained(canonicalPath, directory) {
		return Record{}, errors.New("Fix Record escapes the record directory")
	}
	file, err := os.Open(canonicalPath)
	if err != nil {
		return Record{}, fmt.Errorf("open Fix Record: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxRecordBytes+1))
	if err != nil {
		return Record{}, fmt.Errorf("read Fix Record: %w", err)
	}
	if len(data) > MaxRecordBytes {
		return Record{}, fmt.Errorf("Fix Record exceeds the %d-byte limit", MaxRecordBytes)
	}
	var record Record
	if err := strictjson.Decode(data, &record); err != nil {
		return Record{}, fmt.Errorf("parse Fix Record: %w", err)
	}
	if record.ID != id {
		return Record{}, errors.New("Fix Record identity does not match its filename")
	}
	if err := validateStoredRecord(record); err != nil {
		return Record{}, err
	}
	return record, nil
}

func auditRecordStore(canonicalRoot string) ([]Record, error) {
	directory, exists, err := findRecordDirectory(canonicalRoot)
	if err != nil {
		return nil, err
	}
	if !exists {
		return []Record{}, nil
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("read Fix Record directory: %w", err)
	}
	if len(entries) > maxRecords {
		return nil, fmt.Errorf("Fix Record store exceeds the %d-record limit", maxRecords)
	}
	records := make([]Record, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".json" {
			return nil, fmt.Errorf("unexpected entry in Fix Record store: %s", name)
		}
		id := strings.TrimSuffix(name, ".json")
		if !validFixID(id) {
			return nil, fmt.Errorf("invalid Fix Record filename: %s", name)
		}
		record, err := readRecord(canonicalRoot, id)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func findRecordDirectory(canonicalRoot string) (string, bool, error) {
	parts := []string{".devctl", "knowledge", "fix-records"}
	current := canonicalRoot
	for _, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return "", false, nil
		}
		if err != nil {
			return "", false, fmt.Errorf("inspect Fix Record directory: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", false, fmt.Errorf("Fix Record path component is not a normal directory: %s", current)
		}
		canonical, err := filepath.EvalSymlinks(current)
		if err != nil || !contained(canonical, canonicalRoot) {
			return "", false, errors.New("Fix Record directory escapes the selected project")
		}
		current = canonical
	}
	return current, true, nil
}

func ensureRecordDirectory(canonicalRoot string) (string, error) {
	parts := []string{".devctl", "knowledge", "fix-records"}
	current := canonicalRoot
	for _, part := range parts {
		current = filepath.Join(current, part)
		if err := os.Mkdir(current, 0o700); err != nil && !os.IsExist(err) {
			return "", fmt.Errorf("create Fix Record directory: %w", err)
		}
		if err := normalDirectory(current); err != nil {
			return "", fmt.Errorf("Fix Record path component is not a normal directory: %s", current)
		}
		canonical, err := filepath.EvalSymlinks(current)
		if err != nil || !contained(canonical, canonicalRoot) {
			return "", errors.New("Fix Record directory escapes the selected project")
		}
		current = canonical
	}
	return current, nil
}

func canonicalProject(root string) (string, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve project path: %w", err)
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return "", fmt.Errorf("inspect project path: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", errors.New("project path is not a normal directory")
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve canonical project path: %w", err)
	}
	return canonical, nil
}

func normalDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("path is not a normal directory")
	}
	return nil
}

func contained(path, root string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) && !filepath.IsAbs(relative)
}

func samePath(left, right string) bool {
	left, right = filepath.Clean(left), filepath.Clean(right)
	if filepath.Separator == '\\' {
		return strings.EqualFold(left, right)
	}
	return left == right
}
