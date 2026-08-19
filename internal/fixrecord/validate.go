package fixrecord

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"devctl/internal/evidence"
	"devctl/internal/strictjson"
)

const (
	maxTitleRunes       = 256
	maxTextRunes        = 4096
	maxListItemRunes    = 1024
	maxListItems        = 64
	maxAttempts         = 32
	maxTags             = 32
	maxTagRunes         = 64
	maxRelevantVersions = 32
)

var (
	fixIDPattern            = regexp.MustCompile(`^FIX-[A-Z0-9][A-Z0-9._-]{0,63}$`)
	sha256Pattern           = regexp.MustCompile(`^[a-f0-9]{64}$`)
	privateKeyPattern       = regexp.MustCompile(`(?i)-----BEGIN (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----`)
	awsAccessKeyPattern     = regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)
	secretAssignmentPattern = regexp.MustCompile(`(?i)\b(?:api[_-]?key|secret|password|token)\s*[:=]\s*["']?[A-Za-z0-9/+=_-]{16,}`)
)

func decodeCandidate(reader io.Reader) (Candidate, error) {
	if reader == nil {
		return Candidate{}, errors.New("candidate input is unavailable")
	}
	data, err := io.ReadAll(io.LimitReader(reader, MaxCandidateBytes+1))
	if err != nil {
		return Candidate{}, fmt.Errorf("read Fix Record candidate: %w", err)
	}
	if len(data) > MaxCandidateBytes {
		return Candidate{}, fmt.Errorf("Fix Record candidate exceeds the %d-byte limit", MaxCandidateBytes)
	}
	var candidate Candidate
	if err := strictjson.Decode(data, &candidate); err != nil {
		return Candidate{}, fmt.Errorf("parse Fix Record candidate: %w", err)
	}
	if err := validateCandidate(candidate); err != nil {
		return Candidate{}, err
	}
	return candidate, nil
}

func validateCandidate(candidate Candidate) error {
	encoded, err := json.Marshal(candidate)
	if err != nil {
		return fmt.Errorf("encode Fix Record candidate: %w", err)
	}
	if len(encoded) > MaxCandidateBytes {
		return fmt.Errorf("Fix Record candidate exceeds the %d-byte limit", MaxCandidateBytes)
	}
	if candidate.SchemaVersion != CandidateSchemaVersion {
		return fmt.Errorf("unsupported Fix Record candidate schema version %q", candidate.SchemaVersion)
	}
	if !validFixID(candidate.ID) {
		return errors.New("Fix Record ID is invalid")
	}
	if err := validateRequiredText("title", candidate.Title, maxTitleRunes); err != nil {
		return err
	}
	if err := validateRequiredText("project_id", candidate.ProjectID, 128); err != nil {
		return err
	}
	for _, field := range []namedText{
		{name: "problem", value: candidate.Problem},
		{name: "root_cause", value: candidate.RootCause},
		{name: "final_fix", value: candidate.FinalFix},
		{name: "applicability", value: candidate.Applicability},
	} {
		if err := validateRequiredText(field.name, field.value, maxTextRunes); err != nil {
			return err
		}
	}
	if err := validateTextList("symptoms", candidate.Symptoms, true, maxListItems, maxListItemRunes); err != nil {
		return err
	}
	if err := validateTextList("affected_components", candidate.AffectedComponents, false, maxListItems, maxListItemRunes); err != nil {
		return err
	}
	if err := validateTextList("affected_files", candidate.AffectedFiles, false, maxListItems, maxListItemRunes); err != nil {
		return err
	}
	if err := validateTextList("known_limitations", candidate.KnownLimitations, false, maxListItems, maxListItemRunes); err != nil {
		return err
	}
	if len(candidate.Attempts) > maxAttempts {
		return fmt.Errorf("attempts exceeds the %d-item limit", maxAttempts)
	}
	for index, attempt := range candidate.Attempts {
		if attempt.Outcome != "FAILED" && attempt.Outcome != "INCONCLUSIVE" {
			return fmt.Errorf("attempts[%d].outcome must be FAILED or INCONCLUSIVE", index)
		}
		if err := validateRequiredText(fmt.Sprintf("attempts[%d].description", index), attempt.Description, maxListItemRunes); err != nil {
			return err
		}
		if err := validateRequiredText(fmt.Sprintf("attempts[%d].reason", index), attempt.Reason, maxListItemRunes); err != nil {
			return err
		}
	}
	if !evidence.ValidIdentifier(candidate.PreRunID) || !evidence.ValidIdentifier(candidate.PostRunID) {
		return errors.New("pre_run_id and post_run_id must be valid evidence identifiers")
	}
	if candidate.PreRunID == candidate.PostRunID {
		return errors.New("pre-fix and post-fix runs must be different")
	}
	if err := validateIdentifiers("check_ids", candidate.CheckIDs, true, evidence.ValidIdentifier); err != nil {
		return err
	}
	if (candidate.PatchEvidencePath == "") != (candidate.PatchSHA256 == "") {
		return errors.New("patch_evidence_path and patch_sha256 must be supplied together")
	}
	if candidate.PatchEvidencePath != "" {
		normalized := filepath.ToSlash(filepath.Clean(filepath.FromSlash(candidate.PatchEvidencePath)))
		if normalized != candidate.PatchEvidencePath || filepath.IsAbs(filepath.FromSlash(candidate.PatchEvidencePath)) || !strings.HasPrefix(normalized, ".devctl/evidence/") {
			return errors.New("patch_evidence_path must be a canonical relative path under .devctl/evidence")
		}
	}
	if candidate.PatchSHA256 != "" && !sha256Pattern.MatchString(candidate.PatchSHA256) {
		return errors.New("patch_sha256 must be a lowercase SHA-256 value")
	}
	if len(candidate.RelevantVersions) > maxRelevantVersions {
		return fmt.Errorf("relevant_versions exceeds the %d-item limit", maxRelevantVersions)
	}
	versionNames := make([]string, 0, len(candidate.RelevantVersions))
	for name := range candidate.RelevantVersions {
		versionNames = append(versionNames, name)
	}
	sort.Strings(versionNames)
	for _, name := range versionNames {
		version := candidate.RelevantVersions[name]
		if err := validateRequiredText("relevant_versions key", name, 128); err != nil {
			return err
		}
		if err := validateRequiredText("relevant_versions value", version, 256); err != nil {
			return err
		}
	}
	if err := validateIdentifiers("related_fix_ids", candidate.RelatedFixIDs, false, validFixID); err != nil {
		return err
	}
	for _, id := range candidate.RelatedFixIDs {
		if id == candidate.ID {
			return errors.New("a Fix Record cannot relate to itself")
		}
	}
	if candidate.Supersedes != "" {
		if !validFixID(candidate.Supersedes) {
			return errors.New("supersedes is not a valid Fix Record ID")
		}
		if candidate.Supersedes == candidate.ID {
			return errors.New("a Fix Record cannot supersede itself")
		}
	}
	if err := validateTextList("tags", candidate.Tags, false, maxTags, maxTagRunes); err != nil {
		return err
	}
	return nil
}

func validateStoredRecord(record Record) error {
	if record.SchemaVersion != RecordSchemaVersion || record.Status != StatusVerified || record.ClosureRule != ClosureRuleVersion {
		return errors.New("stored Fix Record has an unsupported trust state")
	}
	if !validFixID(record.ID) || record.RecordedAt.IsZero() || record.RecordedAt.Location() != timeUTC() {
		return errors.New("stored Fix Record identity or time is invalid")
	}
	candidate := Candidate{
		SchemaVersion:      CandidateSchemaVersion,
		ID:                 record.ID,
		Title:              record.Title,
		ProjectID:          record.ProjectID,
		Problem:            record.Problem,
		Symptoms:           record.Symptoms,
		RootCause:          record.RootCause,
		AffectedComponents: record.AffectedComponents,
		AffectedFiles:      record.AffectedFiles,
		Attempts:           record.Attempts,
		FinalFix:           record.FinalFix,
		PreRunID:           record.PreRun.RunID,
		PostRunID:          record.PostRun.RunID,
		PatchEvidencePath:  record.PatchEvidencePath,
		PatchSHA256:        record.PatchSHA256,
		KnownLimitations:   record.KnownLimitations,
		Applicability:      record.Applicability,
		RelevantVersions:   record.RelevantVersions,
		RelatedFixIDs:      record.RelatedFixIDs,
		Supersedes:         record.Supersedes,
		Tags:               record.Tags,
	}
	for _, transition := range record.CheckTransitions {
		candidate.CheckIDs = append(candidate.CheckIDs, transition.CheckID)
	}
	if err := validateCandidate(candidate); err != nil {
		return fmt.Errorf("stored Fix Record content is invalid: %w", err)
	}
	if err := validateRequiredText("project name", record.ProjectName, 256); err != nil {
		return fmt.Errorf("stored Fix Record project metadata is invalid: %w", err)
	}
	if err := validateTextList("technologies", record.Technologies, true, maxListItems, 128); err != nil {
		return fmt.Errorf("stored Fix Record project metadata is invalid: %w", err)
	}
	if len(record.CheckTransitions) != len(candidate.CheckIDs) || len(record.CheckTransitions) == 0 {
		return errors.New("stored Fix Record check transitions are invalid")
	}
	for _, transition := range record.CheckTransitions {
		if !evidence.ValidIdentifier(transition.CheckID) {
			return errors.New("stored Fix Record contains an invalid check transition")
		}
		if err := validateRequiredText("before check version", transition.BeforeVersion, 128); err != nil {
			return errors.New("stored Fix Record contains an invalid check transition")
		}
		if err := validateRequiredText("after check version", transition.AfterVersion, 128); err != nil {
			return errors.New("stored Fix Record contains an invalid check transition")
		}
		if !validStatus(transition.BeforeStatus) || transition.BeforeStatus == "PASS" || transition.BeforeStatus == "SKIP" || transition.BeforeStatus == "NOT_APPLICABLE" || transition.AfterStatus != "PASS" {
			return errors.New("stored Fix Record contains an unverified check transition")
		}
	}
	if err := validateRunReference(record.PreRun, record.ProjectID); err != nil {
		return fmt.Errorf("stored pre-fix run is invalid: %w", err)
	}
	if err := validateRunReference(record.PostRun, record.ProjectID); err != nil {
		return fmt.Errorf("stored post-fix run is invalid: %w", err)
	}
	if !record.PostRun.StartedAt.Before(record.PreRun.FinishedAt) && record.PostRun.FinishedAt.After(record.PreRun.FinishedAt) {
		// Ordered as required.
	} else {
		return errors.New("stored Fix Record run order is invalid")
	}
	if !sha256Pattern.MatchString(record.ChangeFingerprint) {
		return errors.New("stored Fix Record change fingerprint is invalid")
	}
	expectedChange := sha256.Sum256([]byte(record.PreRun.RepositoryFingerprint + "\x00" + record.PostRun.RepositoryFingerprint))
	if hex.EncodeToString(expectedChange[:]) != record.ChangeFingerprint {
		return errors.New("stored Fix Record change fingerprint does not match its run provenance")
	}
	expectedEvidence := []string{record.PreRun.EvidencePath, record.PostRun.EvidencePath}
	if record.PatchEvidencePath != "" {
		expectedEvidence = append(expectedEvidence, record.PatchEvidencePath)
	}
	if !equalStrings(record.RelatedEvidence, expectedEvidence) {
		return errors.New("stored Fix Record evidence references are invalid")
	}
	if !sha256Pattern.MatchString(record.RecordHash) {
		return errors.New("stored Fix Record hash is invalid")
	}
	expectedHash, err := recordHash(record)
	if err != nil || expectedHash != record.RecordHash {
		return errors.New("stored Fix Record hash does not match its content")
	}
	return nil
}

func validateRunReference(run RunReference, projectID string) error {
	if !evidence.ValidIdentifier(run.RunID) || run.EvidencePath != evidencePath(run.RunID) || !sha256Pattern.MatchString(run.ReportSHA256) {
		return errors.New("run evidence identity is invalid")
	}
	if run.StartedAt.IsZero() || run.FinishedAt.IsZero() || run.StartedAt.After(run.FinishedAt) {
		return errors.New("run timestamps are invalid")
	}
	for _, field := range []namedText{
		{name: "policy_version", value: run.PolicyVersion},
		{name: "devctl_version", value: run.DevctlVersion},
		{name: "devctl_commit", value: run.DevctlCommit},
		{name: "repository_revision", value: run.RepositoryRevision},
		{name: "repository_fingerprint", value: run.RepositoryFingerprint},
	} {
		if err := validateRequiredText(field.name, field.value, 256); err != nil {
			return err
		}
	}
	if !sha256Pattern.MatchString(run.RepositoryFingerprint) {
		return errors.New("run repository fingerprint is invalid")
	}
	if !validStatus(run.Overall) || projectID == "" {
		return errors.New("run provenance is incomplete")
	}
	return nil
}

func validateRequiredText(name, value string, maximum int) error {
	if value == "" || strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must be non-empty and have no surrounding whitespace", name)
	}
	if len([]rune(value)) > maximum {
		return fmt.Errorf("%s exceeds the %d-character limit", name, maximum)
	}
	if unsafeText(value) {
		return fmt.Errorf("%s contains control or secret-like content", name)
	}
	return nil
}

func validateTextList(name string, values []string, required bool, maximumItems, maximumRunes int) error {
	if required && len(values) == 0 {
		return fmt.Errorf("%s must contain at least one item", name)
	}
	if len(values) > maximumItems {
		return fmt.Errorf("%s exceeds the %d-item limit", name, maximumItems)
	}
	seen := make(map[string]struct{}, len(values))
	for index, value := range values {
		if err := validateRequiredText(fmt.Sprintf("%s[%d]", name, index), value, maximumRunes); err != nil {
			return err
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("%s contains a duplicate item", name)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validateIdentifiers(name string, values []string, required bool, valid func(string) bool) error {
	if required && len(values) == 0 {
		return fmt.Errorf("%s must contain at least one item", name)
	}
	if len(values) > maxListItems {
		return fmt.Errorf("%s exceeds the %d-item limit", name, maxListItems)
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !valid(value) {
			return fmt.Errorf("%s contains an invalid identifier", name)
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("%s contains a duplicate identifier", name)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func unsafeText(value string) bool {
	if strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return true
	}
	return privateKeyPattern.MatchString(value) || awsAccessKeyPattern.MatchString(value) || secretAssignmentPattern.MatchString(value)
}

func validFixID(value string) bool {
	return fixIDPattern.MatchString(value)
}

func recordHash(record Record) (string, error) {
	record.RecordHash = ""
	data, err := json.Marshal(record)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func evidencePath(runID string) string {
	return ".devctl/evidence/" + runID
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func sortedCopy(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

type namedText struct {
	name  string
	value string
}

// timeUTC is kept behind a helper so stored timestamps are checked for the
// exact canonical UTC location rather than only a zero offset.
func timeUTC() *time.Location {
	return time.UTC
}
