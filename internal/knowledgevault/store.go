package knowledgevault

import (
	"crypto/rand"
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
	"unicode"

	"devctl/internal/fixrecord"
	"devctl/internal/strictjson"
)

const maxLessons = 2000
const MaxDraftBytes = 64 * 1024

var (
	uuidPattern             = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	displayIDPattern        = regexp.MustCompile(`^LESSON-[A-Z0-9][A-Z0-9._-]{3,63}$`)
	sha256Pattern           = regexp.MustCompile(`^[a-f0-9]{64}$`)
	privateKeyPattern       = regexp.MustCompile(`(?i)-----BEGIN (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----`)
	awsAccessKeyPattern     = regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)
	secretAssignmentPattern = regexp.MustCompile(`(?i)\b(?:api[_-]?key|secret|password|token)\s*[:=]\s*["']?[A-Za-z0-9/+=_-]{16,}`)
	privatePathPattern      = regexp.MustCompile(`(?i)(?:[a-z]:[\\/]|\\\\|/(?:users|home|private|var/log)/|\.devctl/evidence/)`)
	rawLogPattern           = regexp.MustCompile(`(?i)(?:raw_output|stdout|stderr|stack trace|panic:|traceback \(|BEGIN LOG)`)
)

var (
	ErrLessonExists   = errors.New("lesson revision already exists")
	ErrLessonNotFound = errors.New("lesson not found")
)

func LocalDirectory(root string) string {
	return filepath.Join(root, ".devctl", "knowledge", "authoritative-lessons")
}

func GlobalDirectory(root string) string {
	return filepath.Join(root, "knowledge", "authoritative-lessons")
}

func IndexPath(root, scope string) string {
	if scope == ScopeGlobal {
		return filepath.Join(root, "knowledge", "lesson-index.json")
	}
	return filepath.Join(root, ".devctl", "knowledge", "lesson-index.json")
}

// CreateCandidate stores a local, non-trusted revision. A draft without
// objective Fix Record evidence is retained as REQUIRES_REVIEW, never VERIFIED.
func CreateCandidate(root string, draft Draft) (Lesson, error) {
	if err := validateDraft(draft); err != nil {
		return Lesson{}, err
	}
	id, err := newID()
	if err != nil {
		return Lesson{}, err
	}
	now := time.Now().UTC()
	lesson := Lesson{
		SchemaVersion: SchemaVersion, ID: id, DisplayID: draft.DisplayID, Scope: ScopeProject,
		Revision: 1, Status: StatusCandidate, Title: draft.Title, Statement: draft.Statement,
		Problem: draft.Problem, RootCause: draft.RootCause, Correction: draft.Correction, Technologies: cloneStrings(draft.Technologies),
		RelevantVersions: cloneMap(draft.RelevantVersions), Platform: draft.Platform,
		VerificationScope: draft.VerificationScope, Applicability: draft.Applicability,
		Limitations: cloneStrings(draft.Limitations), SourceFixIDs: cloneStrings(draft.SourceFixIDs),
		Tags: cloneStrings(draft.Tags), CheckIDs: cloneStrings(draft.CheckIDs), FailureIDs: cloneStrings(draft.FailureIDs),
		Adapters: cloneStrings(draft.Adapters), NormalizedErrors: cloneStrings(draft.NormalizedErrors),
		AffectedPaths: cloneStrings(draft.AffectedPaths), Symptoms: cloneStrings(draft.Symptoms),
		CreatedAt: now,
	}
	if len(draft.SourceFixIDs) == 0 {
		lesson.Status = StatusRequiresReview
		lesson.ReviewNote = "No verified Fix Record evidence was supplied."
	} else {
		projectIDs, sourceErr := validateAndBindFixSources(root, draft.SourceFixIDs)
		lesson.SourceProjectIDs = projectIDs
		if sourceErr != nil {
			lesson.Status = StatusRequiresReview
			lesson.ReviewNote = sourceErr.Error()
		}
	}
	return appendRevision(root, ScopeProject, lesson)
}

func DecodeDraft(data []byte) (Draft, error) {
	if len(data) > MaxDraftBytes {
		return Draft{}, fmt.Errorf("lesson draft exceeds the %d-byte limit", MaxDraftBytes)
	}
	var draft Draft
	if err := strictjson.Decode(data, &draft); err != nil {
		return Draft{}, fmt.Errorf("parse lesson draft: %w", err)
	}
	if err := validateDraft(draft); err != nil {
		return Draft{}, err
	}
	return draft, nil
}

// Review validates the immutable source Fix Records again and appends a new
// revision. Only an explicit approval can move a candidate to VERIFIED.
func ReviewLesson(root, id string, review Review) (Lesson, error) {
	current, err := Read(root, ScopeProject, id)
	if err != nil {
		return Lesson{}, err
	}
	if current.Status != StatusCandidate && current.Status != StatusRequiresReview {
		return Lesson{}, fmt.Errorf("lesson %s is not reviewable from %s", id, current.Status)
	}
	if strings.TrimSpace(review.Reviewer) == "" {
		return Lesson{}, errors.New("lesson review requires a reviewer")
	}
	next := current
	next.Revision++
	next.PreviousHash = current.ContentHash
	next.Reviewer = review.Reviewer
	next.ReviewNote = review.Note
	next.CreatedAt = time.Now().UTC()
	if !review.Approve {
		next.Status = StatusRejected
	} else if len(current.SourceFixIDs) == 0 {
		next.Status = StatusRequiresReview
		next.ReviewNote = "Approval cannot replace objective Fix Record evidence."
	} else {
		projectIDs, sourceErr := validateAndBindFixSources(root, current.SourceFixIDs)
		next.SourceProjectIDs = projectIDs
		if sourceErr != nil {
			next.Status = StatusRequiresReview
			next.ReviewNote = sourceErr.Error()
			next.ValidatedAt = time.Time{}
		} else {
			next.Status = StatusVerified
			next.ValidatedAt = next.CreatedAt
		}
	}
	if err := validateLesson(next); err != nil {
		return Lesson{}, err
	}
	return appendRevision(root, ScopeProject, next)
}

// Supersede appends a historical lifecycle revision. It never rewrites the
// earlier VERIFIED bytes, so a later correction can be linked to this exact
// revision instead of changing history in place.
func Supersede(root, id, reviewer, note string) (Lesson, error) {
	if strings.TrimSpace(reviewer) == "" {
		return Lesson{}, errors.New("supersession requires a reviewer")
	}
	current, err := Read(root, ScopeProject, id)
	if err != nil {
		return Lesson{}, err
	}
	if current.Status != StatusVerified {
		return Lesson{}, fmt.Errorf("only VERIFIED lessons can be superseded, got %s", current.Status)
	}
	next := current
	next.Revision++
	next.Status = StatusSuperseded
	next.PreviousHash = current.ContentHash
	next.Reviewer = reviewer
	next.ReviewNote = note
	next.CreatedAt = time.Now().UTC()
	return appendRevision(root, ScopeProject, next)
}

// Correct appends a new candidate revision linked to the current revision.
// The previous revision remains untouched and can be inspected as history.
func Correct(root, id string, draft Draft) (Lesson, error) {
	if err := validateDraft(draft); err != nil {
		return Lesson{}, err
	}
	current, err := Read(root, ScopeProject, id)
	if err != nil {
		return Lesson{}, err
	}
	next := Lesson{
		SchemaVersion: SchemaVersion, ID: current.ID, DisplayID: draft.DisplayID, Scope: ScopeProject,
		Revision: current.Revision + 1, Status: StatusCandidate, Title: draft.Title, Statement: draft.Statement,
		Problem: draft.Problem, RootCause: draft.RootCause, Correction: draft.Correction,
		Technologies: cloneStrings(draft.Technologies), RelevantVersions: cloneMap(draft.RelevantVersions),
		Platform: draft.Platform, VerificationScope: draft.VerificationScope, Applicability: draft.Applicability,
		Limitations: cloneStrings(draft.Limitations), SourceFixIDs: cloneStrings(draft.SourceFixIDs),
		Tags: cloneStrings(draft.Tags), CheckIDs: cloneStrings(draft.CheckIDs), FailureIDs: cloneStrings(draft.FailureIDs),
		Adapters: cloneStrings(draft.Adapters), NormalizedErrors: cloneStrings(draft.NormalizedErrors),
		AffectedPaths: cloneStrings(draft.AffectedPaths), Symptoms: cloneStrings(draft.Symptoms),
		PreviousHash: current.ContentHash, CreatedAt: time.Now().UTC(),
	}
	if len(next.SourceFixIDs) == 0 {
		next.Status = StatusRequiresReview
		next.ReviewNote = "No verified Fix Record evidence was supplied."
	} else {
		projectIDs, sourceErr := validateAndBindFixSources(root, next.SourceFixIDs)
		next.SourceProjectIDs = projectIDs
		if sourceErr != nil {
			next.Status = StatusRequiresReview
			next.ReviewNote = sourceErr.Error()
		}
	}
	return appendRevision(root, ScopeProject, next)
}

// Promote copies a verified local lesson into the global authoritative store.
// It creates a new machine ID so the project history and global publication
// remain separate append-only histories.
func Promote(localRoot, globalRoot, id string, approval PromotionApproval) (Lesson, error) {
	if strings.TrimSpace(approval.Reviewer) == "" || !approval.Approve {
		return Lesson{}, errors.New("global promotion requires explicit reviewer approval")
	}
	local, err := Read(localRoot, ScopeProject, id)
	if err != nil {
		return Lesson{}, err
	}
	if local.Status != StatusVerified {
		return Lesson{}, fmt.Errorf("only VERIFIED lessons can be promoted, got %s", local.Status)
	}
	if err := validatePromotable(local); err != nil {
		return Lesson{}, err
	}
	for _, fixID := range local.SourceFixIDs {
		if _, showErr := fixrecord.Show(localRoot, fixID); showErr != nil {
			return Lesson{}, fmt.Errorf("promotion source Fix Record %q is unavailable or invalid: %w", fixID, showErr)
		}
	}
	newID, err := newID()
	if err != nil {
		return Lesson{}, err
	}
	now := time.Now().UTC()
	global := local
	global.ID = newID
	global.Scope = ScopeGlobal
	global.Revision = 1
	global.Status = StatusVerified
	global.SourceLessonID = local.ID
	global.PreviousHash = ""
	global.Reviewer = approval.Reviewer
	global.ReviewNote = approval.Note
	global.CreatedAt = now
	global.ValidatedAt = now
	return appendRevision(globalRoot, ScopeGlobal, global)
}

// Read returns the newest authoritative revision for one machine ID. It does
// not read or rebuild a generated index.
func Read(root, scope, id string) (Lesson, error) {
	if !uuidPattern.MatchString(id) {
		return Lesson{}, errors.New("lesson machine ID is invalid")
	}
	lessons, err := readAuthoritative(root, scope)
	if err != nil {
		return Lesson{}, err
	}
	var found *Lesson
	for i := range lessons {
		if lessons[i].ID == id && (found == nil || lessons[i].Revision > found.Revision) {
			copy := lessons[i]
			found = &copy
		}
	}
	if found == nil {
		return Lesson{}, ErrLessonNotFound
	}
	return *found, nil
}

func List(root, scope string) ([]Lesson, error) {
	all, err := ListAll(root, scope)
	if err != nil {
		return nil, err
	}
	current := map[string]Lesson{}
	for _, lesson := range all {
		if previous, exists := current[lesson.ID]; !exists || lesson.Revision > previous.Revision {
			current[lesson.ID] = lesson
		}
	}
	result := make([]Lesson, 0, len(current))
	for _, lesson := range current {
		result = append(result, lesson)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func ListAll(root, scope string) ([]Lesson, error) {
	lessons, err := readAuthoritative(root, scope)
	if err != nil {
		return nil, err
	}
	sort.Slice(lessons, func(i, j int) bool {
		if lessons[i].ID != lessons[j].ID {
			return lessons[i].ID < lessons[j].ID
		}
		return lessons[i].Revision < lessons[j].Revision
	})
	return lessons, nil
}

func ReadIdentifier(root, scope, identifier string) (Lesson, error) {
	if uuidPattern.MatchString(identifier) {
		return Read(root, scope, identifier)
	}
	if !displayIDPattern.MatchString(identifier) {
		return Lesson{}, errors.New("lesson identifier is invalid")
	}
	lessons, err := List(root, scope)
	if err != nil {
		return Lesson{}, err
	}
	var found *Lesson
	for i := range lessons {
		if lessons[i].DisplayID != identifier {
			continue
		}
		if found != nil {
			return Lesson{}, fmt.Errorf("lesson display ID is ambiguous: %s", identifier)
		}
		copy := lessons[i]
		found = &copy
	}
	if found == nil {
		return Lesson{}, ErrLessonNotFound
	}
	return *found, nil
}

// RebuildIndex reads only authoritative files and recreates a disposable
// index. Deleting or corrupting the index therefore cannot delete knowledge.
func RebuildIndex(root, scope string) (Index, error) {
	if _, _, err := resolveAuthoritativeDirectory(root, scope, true); err != nil {
		return Index{}, err
	}
	lessons, err := List(root, scope)
	if err != nil {
		return Index{}, err
	}
	index := Index{SchemaVersion: SchemaVersion, BuiltAt: time.Now().UTC(), Scope: scope, Lessons: make([]IndexLesson, 0, len(lessons))}
	for _, lesson := range lessons {
		index.Lessons = append(index.Lessons, IndexLesson{ID: lesson.ID, DisplayID: lesson.DisplayID, Revision: lesson.Revision, Status: lesson.Status, Title: lesson.Title, Statement: lesson.Statement, Technologies: cloneStrings(lesson.Technologies), RelevantVersions: cloneMap(lesson.RelevantVersions), Platform: lesson.Platform, Applicability: lesson.Applicability, Limitations: cloneStrings(lesson.Limitations), ValidatedAt: lesson.ValidatedAt, Tags: cloneStrings(lesson.Tags), CheckIDs: cloneStrings(lesson.CheckIDs), FailureIDs: cloneStrings(lesson.FailureIDs), Adapters: cloneStrings(lesson.Adapters), NormalizedErrors: cloneStrings(lesson.NormalizedErrors), AffectedPaths: cloneStrings(lesson.AffectedPaths), Symptoms: cloneStrings(lesson.Symptoms)})
	}
	if err := writeJSONAtomic(IndexPath(root, scope), index); err != nil {
		return Index{}, err
	}
	return index, nil
}

func LoadIndex(root, scope string) (Index, error) {
	data, err := os.ReadFile(IndexPath(root, scope))
	if err != nil {
		return Index{}, err
	}
	var index Index
	if err := strictjson.Decode(data, &index); err != nil {
		return Index{}, fmt.Errorf("parse lesson index: %w", err)
	}
	if index.SchemaVersion != SchemaVersion || index.Scope != scope {
		return Index{}, errors.New("lesson index identity is invalid")
	}
	return index, nil
}

func appendRevision(root, scope string, lesson Lesson) (Lesson, error) {
	if lesson.Scope != scope {
		return Lesson{}, errors.New("lesson scope does not match destination")
	}
	all, err := readAuthoritative(root, scope)
	if err != nil {
		return Lesson{}, err
	}
	for _, existing := range all {
		if existing.ID == lesson.ID && existing.Revision == lesson.Revision {
			return Lesson{}, ErrLessonExists
		}
	}
	if len(all) >= maxLessons {
		return Lesson{}, errors.New("authoritative lesson store exceeds bounded record limit")
	}
	if lesson.Revision == 1 {
		for _, existing := range all {
			if existing.ID == lesson.ID {
				return Lesson{}, errors.New("lesson machine ID already exists")
			}
		}
	} else if lesson.PreviousHash == "" {
		return Lesson{}, errors.New("lesson revision must link its previous content hash")
	}
	lesson.ContentHash = ""
	data, err := json.Marshal(lesson)
	if err != nil {
		return Lesson{}, fmt.Errorf("encode lesson: %w", err)
	}
	digest := sha256.Sum256(data)
	lesson.ContentHash = hex.EncodeToString(digest[:])
	if err := validateLesson(lesson); err != nil {
		return Lesson{}, err
	}
	data, err = json.MarshalIndent(lesson, "", "  ")
	if err != nil {
		return Lesson{}, fmt.Errorf("encode lesson: %w", err)
	}
	data = append(data, '\n')
	directory, _, err := resolveAuthoritativeDirectory(root, scope, true)
	if err != nil {
		return Lesson{}, err
	}
	path := filepath.Join(directory, fmt.Sprintf("%s-r%04d.json", lesson.ID, lesson.Revision))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return Lesson{}, ErrLessonExists
		}
		return Lesson{}, fmt.Errorf("create authoritative lesson: %w", err)
	}
	remove := true
	defer func() {
		if remove {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return Lesson{}, fmt.Errorf("write authoritative lesson: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return Lesson{}, fmt.Errorf("sync authoritative lesson: %w", err)
	}
	if err := file.Close(); err != nil {
		return Lesson{}, fmt.Errorf("close authoritative lesson: %w", err)
	}
	remove = false
	return lesson, nil
}

func readAuthoritative(root, scope string) ([]Lesson, error) {
	if scope != ScopeProject && scope != ScopeGlobal {
		return nil, errors.New("lesson scope is invalid")
	}
	directory, exists, err := resolveAuthoritativeDirectory(root, scope, false)
	if err != nil {
		return nil, err
	}
	if !exists {
		return []Lesson{}, nil
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("read authoritative lesson directory: %w", err)
	}
	if len(entries) > maxLessons {
		return nil, errors.New("authoritative lesson store exceeds bounded record limit")
	}
	lessons := make([]Lesson, 0, len(entries))
	seen := map[string]struct{}{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			return nil, fmt.Errorf("unexpected entry in authoritative lesson store: %s", entry.Name())
		}
		path := filepath.Join(directory, entry.Name())
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("authoritative lesson is not a normal file: %s", entry.Name())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read authoritative lesson: %w", err)
		}
		var lesson Lesson
		if err := strictjson.Decode(data, &lesson); err != nil {
			return nil, fmt.Errorf("parse authoritative lesson %s: %w", entry.Name(), err)
		}
		if err := validateLesson(lesson); err != nil {
			return nil, fmt.Errorf("validate authoritative lesson %s: %w", entry.Name(), err)
		}
		storedHash := lesson.ContentHash
		lesson.ContentHash = ""
		canonical, hashErr := json.Marshal(lesson)
		if hashErr != nil {
			return nil, fmt.Errorf("hash authoritative lesson %s: %w", entry.Name(), hashErr)
		}
		digest := sha256.Sum256(canonical)
		if storedHash != hex.EncodeToString(digest[:]) {
			return nil, fmt.Errorf("authoritative lesson %s content hash does not match", entry.Name())
		}
		lesson.ContentHash = storedHash
		if lesson.Scope != scope {
			return nil, fmt.Errorf("authoritative lesson %s has the wrong scope", entry.Name())
		}
		expectedName := fmt.Sprintf("%s-r%04d.json", lesson.ID, lesson.Revision)
		if entry.Name() != expectedName {
			return nil, fmt.Errorf("authoritative lesson filename does not match its identity: %s", entry.Name())
		}
		key := fmt.Sprintf("%s/%d", lesson.ID, lesson.Revision)
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("duplicate authoritative lesson revision: %s", key)
		}
		seen[key] = struct{}{}
		lessons = append(lessons, lesson)
	}
	byRevision := make(map[string]Lesson, len(lessons))
	for _, lesson := range lessons {
		byRevision[fmt.Sprintf("%s/%d", lesson.ID, lesson.Revision)] = lesson
	}
	for _, lesson := range lessons {
		if lesson.Revision == 1 {
			continue
		}
		previous, exists := byRevision[fmt.Sprintf("%s/%d", lesson.ID, lesson.Revision-1)]
		if !exists || lesson.PreviousHash != previous.ContentHash {
			return nil, fmt.Errorf("authoritative lesson %s has a broken revision chain", lesson.ID)
		}
	}
	return lessons, nil
}

func validateDraft(draft Draft) error {
	fields := []struct {
		name  string
		value string
	}{
		{"display_id", draft.DisplayID}, {"title", draft.Title}, {"statement", draft.Statement},
		{"problem", draft.Problem}, {"root_cause", draft.RootCause}, {"correction", draft.Correction},
		{"platform", draft.Platform}, {"verification_scope", draft.VerificationScope}, {"applicability", draft.Applicability},
	}
	for _, field := range fields {
		if err := validateText(field.name, field.value, 4096, true); err != nil {
			return err
		}
	}
	if !displayIDPattern.MatchString(draft.DisplayID) {
		return errors.New("lesson display ID is invalid")
	}
	if len(draft.Technologies) > 32 || len(draft.RelevantVersions) > 32 || len(draft.Limitations) > 64 || len(draft.SourceFixIDs) > 64 {
		return errors.New("lesson metadata exceeds its bounded limit")
	}
	for _, value := range draft.Technologies {
		if err := validateText("technology", value, 128, true); err != nil {
			return err
		}
	}
	for key, value := range draft.RelevantVersions {
		if err := validateText("relevant version key", key, 128, true); err != nil {
			return err
		}
		if err := validateText("relevant version", value, 256, true); err != nil {
			return err
		}
	}
	for _, value := range draft.Limitations {
		if err := validateText("limitation", value, 1024, true); err != nil {
			return err
		}
	}
	if err := validateSearchMetadata(draft.Tags, draft.CheckIDs, draft.FailureIDs, draft.Adapters, draft.NormalizedErrors, draft.AffectedPaths, draft.Symptoms); err != nil {
		return err
	}
	for _, value := range draft.SourceFixIDs {
		if !strings.HasPrefix(value, "FIX-") || strings.ContainsAny(value, `/\\`) {
			return errors.New("source Fix Record ID is invalid")
		}
	}
	return nil
}

func validateLesson(lesson Lesson) error {
	if lesson.SchemaVersion != SchemaVersion || !uuidPattern.MatchString(lesson.ID) || !displayIDPattern.MatchString(lesson.DisplayID) {
		return errors.New("lesson identity is invalid")
	}
	if lesson.Scope != ScopeProject && lesson.Scope != ScopeGlobal {
		return errors.New("lesson scope is invalid")
	}
	if lesson.Revision < 1 || lesson.Status != StatusCandidate && lesson.Status != StatusVerified && lesson.Status != StatusRequiresReview && lesson.Status != StatusSuperseded && lesson.Status != StatusRejected {
		return errors.New("lesson revision or lifecycle status is invalid")
	}
	if lesson.CreatedAt.IsZero() || lesson.CreatedAt.Location() != time.UTC {
		return errors.New("lesson creation time is invalid")
	}
	if err := validateText("title", lesson.Title, 4096, true); err != nil {
		return err
	}
	fields := []struct {
		name  string
		value string
	}{
		{"statement", lesson.Statement}, {"problem", lesson.Problem}, {"root_cause", lesson.RootCause},
		{"correction", lesson.Correction}, {"platform", lesson.Platform},
		{"verification_scope", lesson.VerificationScope}, {"applicability", lesson.Applicability},
	}
	for _, field := range fields {
		if err := validateText(field.name, field.value, 4096, true); err != nil {
			return err
		}
	}
	if len(lesson.SourceFixIDs) > 64 || len(lesson.SourceProjectIDs) > 64 {
		return errors.New("lesson source list exceeds its bounded limit")
	}
	if len(lesson.Technologies) > 32 || len(lesson.RelevantVersions) > 32 || len(lesson.Limitations) > 64 {
		return errors.New("lesson metadata exceeds its bounded limit")
	}
	for _, value := range lesson.Technologies {
		if err := validateText("technology", value, 128, true); err != nil {
			return err
		}
	}
	for key, value := range lesson.RelevantVersions {
		if err := validateText("relevant version key", key, 128, true); err != nil {
			return err
		}
		if err := validateText("relevant version", value, 256, true); err != nil {
			return err
		}
	}
	for _, value := range lesson.Limitations {
		if err := validateText("limitation", value, 1024, true); err != nil {
			return err
		}
	}
	if err := validateSearchMetadata(lesson.Tags, lesson.CheckIDs, lesson.FailureIDs, lesson.Adapters, lesson.NormalizedErrors, lesson.AffectedPaths, lesson.Symptoms); err != nil {
		return err
	}
	for _, id := range lesson.SourceFixIDs {
		if !strings.HasPrefix(id, "FIX-") || strings.ContainsAny(id, `/\\`) {
			return errors.New("lesson contains an invalid Fix Record ID")
		}
	}
	if lesson.Status == StatusVerified && len(lesson.SourceFixIDs) == 0 {
		return errors.New("VERIFIED lesson requires Fix Record evidence")
	}
	if lesson.Scope == ScopeGlobal && lesson.Status == StatusVerified && lesson.SourceLessonID == "" {
		return errors.New("global VERIFIED lesson requires a source lesson")
	}
	if !sha256Pattern.MatchString(lesson.ContentHash) {
		return errors.New("lesson content hash is invalid")
	}
	return nil
}

func validateSearchMetadata(tags, checks, failures, adapters, normalizedErrors, paths, symptoms []string) error {
	fields := []struct {
		name   string
		values []string
		limit  int
		max    int
	}{
		{"tag", tags, 64, 128}, {"check ID", checks, 64, 128}, {"failure ID", failures, 64, 128},
		{"adapter", adapters, 64, 128}, {"normalized error", normalizedErrors, 64, 1024},
		{"affected path", paths, 64, 512}, {"symptom", symptoms, 64, 1024},
	}
	for _, field := range fields {
		if len(field.values) > field.limit {
			return fmt.Errorf("%s metadata exceeds the %d-item limit", field.name, field.limit)
		}
		seen := make(map[string]struct{}, len(field.values))
		for _, value := range field.values {
			if err := validateText(field.name, value, field.max, true); err != nil {
				return err
			}
			if _, exists := seen[value]; exists {
				return fmt.Errorf("%s metadata contains a duplicate", field.name)
			}
			seen[value] = struct{}{}
		}
	}
	return nil
}

func validatePromotable(lesson Lesson) error {
	values := []string{lesson.DisplayID, lesson.Title, lesson.Statement, lesson.Problem, lesson.RootCause, lesson.Correction, lesson.Platform, lesson.VerificationScope, lesson.Applicability, lesson.ReviewNote, lesson.Reviewer, lesson.SourceLessonID}
	for _, value := range values {
		if promotableTextUnsafe(value) {
			return errors.New("lesson promotion contains secret, raw-log, or private-path material")
		}
	}
	for _, value := range lesson.Technologies {
		if promotableTextUnsafe(value) {
			return errors.New("lesson promotion contains secret, raw-log, or private-path material")
		}
	}
	for _, value := range lesson.SourceFixIDs {
		if promotableTextUnsafe(value) {
			return errors.New("lesson promotion contains secret, raw-log, or private-path material")
		}
	}
	for _, value := range lesson.SourceProjectIDs {
		if promotableTextUnsafe(value) {
			return errors.New("lesson promotion contains secret, raw-log, or private-path material")
		}
	}
	for _, value := range lesson.Tags {
		if promotableTextUnsafe(value) {
			return errors.New("lesson promotion contains secret, raw-log, or private-path material")
		}
	}
	for _, value := range lesson.CheckIDs {
		if promotableTextUnsafe(value) {
			return errors.New("lesson promotion contains secret, raw-log, or private-path material")
		}
	}
	for _, value := range lesson.FailureIDs {
		if promotableTextUnsafe(value) {
			return errors.New("lesson promotion contains secret, raw-log, or private-path material")
		}
	}
	for _, value := range lesson.Adapters {
		if promotableTextUnsafe(value) {
			return errors.New("lesson promotion contains secret, raw-log, or private-path material")
		}
	}
	for _, value := range lesson.NormalizedErrors {
		if promotableTextUnsafe(value) {
			return errors.New("lesson promotion contains secret, raw-log, or private-path material")
		}
	}
	for _, value := range lesson.AffectedPaths {
		if promotableTextUnsafe(value) {
			return errors.New("lesson promotion contains secret, raw-log, or private-path material")
		}
	}
	for _, value := range lesson.Symptoms {
		if promotableTextUnsafe(value) {
			return errors.New("lesson promotion contains secret, raw-log, or private-path material")
		}
	}
	for _, value := range lesson.Limitations {
		if promotableTextUnsafe(value) {
			return errors.New("lesson promotion contains secret, raw-log, or private-path material")
		}
	}
	versionKeys := make([]string, 0, len(lesson.RelevantVersions))
	for key := range lesson.RelevantVersions {
		versionKeys = append(versionKeys, key)
	}
	sort.Strings(versionKeys)
	for _, key := range versionKeys {
		if promotableTextUnsafe(key) || promotableTextUnsafe(lesson.RelevantVersions[key]) {
			return errors.New("lesson promotion contains secret, raw-log, or private-path material")
		}
	}
	return nil
}

func promotableTextUnsafe(value string) bool {
	return privateKeyPattern.MatchString(value) || awsAccessKeyPattern.MatchString(value) || secretAssignmentPattern.MatchString(value) || privatePathPattern.MatchString(filepath.ToSlash(value)) || rawLogPattern.MatchString(value)
}

func validateAndBindFixSources(root string, sourceFixIDs []string) ([]string, error) {
	projectIDs := make([]string, 0, len(sourceFixIDs))
	for _, fixID := range sourceFixIDs {
		record, err := fixrecord.Show(root, fixID)
		if err != nil {
			return projectIDs, fmt.Errorf("objective Fix Record evidence is unavailable: %s", fixID)
		}
		projectIDs = appendUnique(projectIDs, record.ProjectID)
	}
	return projectIDs, nil
}

func validateText(name, value string, maximum int, required bool) error {
	if required && (value == "" || strings.TrimSpace(value) != value) {
		return fmt.Errorf("%s must be non-empty and have no surrounding whitespace", name)
	}
	if len([]rune(value)) > maximum || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return fmt.Errorf("%s is invalid or exceeds its limit", name)
	}
	if privateKeyPattern.MatchString(value) || awsAccessKeyPattern.MatchString(value) || secretAssignmentPattern.MatchString(value) {
		return fmt.Errorf("%s contains secret-like content", name)
	}
	return nil
}

func authoritativeDirectory(root, scope string) string {
	if scope == ScopeGlobal {
		return GlobalDirectory(root)
	}
	return LocalDirectory(root)
}

func resolveAuthoritativeDirectory(root, scope string, create bool) (string, bool, error) {
	if scope != ScopeProject && scope != ScopeGlobal {
		return "", false, errors.New("lesson scope is invalid")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", false, fmt.Errorf("resolve lesson root: %w", err)
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return "", false, fmt.Errorf("inspect lesson root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", false, errors.New("lesson root is not a normal directory")
	}
	canonicalRoot, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", false, fmt.Errorf("resolve lesson root: %w", err)
	}
	parts := []string{"knowledge", "authoritative-lessons"}
	if scope == ScopeProject {
		parts = []string{".devctl", "knowledge", "authoritative-lessons"}
	}
	current := canonicalRoot
	for _, part := range parts {
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			if !create {
				return "", false, nil
			}
			if err := os.Mkdir(current, 0o700); err != nil && !os.IsExist(err) {
				return "", false, fmt.Errorf("create authoritative lesson directory: %w", err)
			}
			info, statErr = os.Lstat(current)
		}
		if statErr != nil {
			return "", false, fmt.Errorf("inspect authoritative lesson directory: %w", statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", false, fmt.Errorf("authoritative lesson path component is not a normal directory: %s", current)
		}
		canonical, evalErr := filepath.EvalSymlinks(current)
		if evalErr != nil || !containedPath(canonical, canonicalRoot) {
			return "", false, errors.New("authoritative lesson directory escapes the selected root")
		}
		current = canonical
	}
	return current, true, nil
}

func containedPath(path, root string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) && !filepath.IsAbs(relative)
}

func writeJSONAtomic(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".lesson-index-*.tmp")
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

func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate lesson ID: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s", hex.EncodeToString(b[0:4]), hex.EncodeToString(b[4:6]), hex.EncodeToString(b[6:8]), hex.EncodeToString(b[8:10]), hex.EncodeToString(b[10:16])), nil
}

func cloneStrings(values []string) []string { return append([]string(nil), values...) }
func cloneMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}
func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
