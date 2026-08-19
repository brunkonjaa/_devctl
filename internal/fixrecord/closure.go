package fixrecord

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"devctl/internal/evidence"
	"devctl/internal/gitstate"
	"devctl/internal/model"
	"devctl/internal/verify"
)

func closeCandidate(root string, candidate Candidate, options Options) (Record, error) {
	if err := validateCandidate(candidate); err != nil {
		return Record{}, err
	}
	preLoaded, err := evidence.ReadRun(root, candidate.PreRunID)
	if err != nil {
		return Record{}, fmt.Errorf("read pre-fix evidence: %w", err)
	}
	postLoaded, err := evidence.ReadRun(root, candidate.PostRunID)
	if err != nil {
		return Record{}, fmt.Errorf("read post-fix evidence: %w", err)
	}
	pre, post := preLoaded.Report, postLoaded.Report
	if err := validateClosureReports(root, candidate, pre, post); err != nil {
		return Record{}, err
	}
	currentFingerprint, err := gitstate.Fingerprint(root)
	if err != nil {
		return Record{}, fmt.Errorf("fingerprint current project: %w", err)
	}
	if currentFingerprint != post.RepositoryFingerprint {
		return Record{}, errors.New("post-fix evidence is stale for the current project fingerprint")
	}
	patchPath, patchHash, err := validatePatchArtifact(root, candidate.PatchEvidencePath, candidate.PatchSHA256)
	if err != nil {
		return Record{}, err
	}
	for _, relatedID := range candidate.RelatedFixIDs {
		if _, err := Show(root, relatedID); err != nil {
			return Record{}, fmt.Errorf("related Fix Record %q is unavailable or invalid: %w", relatedID, err)
		}
	}
	if candidate.Supersedes != "" {
		if _, err := Show(root, candidate.Supersedes); err != nil {
			return Record{}, fmt.Errorf("superseded Fix Record %q is unavailable or invalid: %w", candidate.Supersedes, err)
		}
	}
	now := time.Now
	if options.Now != nil {
		now = options.Now
	}
	recordedAt := now().UTC()
	if recordedAt.IsZero() || recordedAt.Before(post.FinishedAt) {
		return Record{}, errors.New("Fix Record time must not precede the post-fix run")
	}
	transitions, err := deriveTransitions(candidate.CheckIDs, pre, post)
	if err != nil {
		return Record{}, err
	}
	technologies := technologyIDs(*post.Project)
	relatedEvidence := []string{pre.EvidencePath, post.EvidencePath}
	if patchPath != "" {
		relatedEvidence = append(relatedEvidence, patchPath)
	}
	record := Record{
		SchemaVersion:      RecordSchemaVersion,
		ID:                 candidate.ID,
		Status:             StatusVerified,
		ClosureRule:        ClosureRuleVersion,
		RecordedAt:         recordedAt,
		Title:              candidate.Title,
		ProjectID:          candidate.ProjectID,
		ProjectName:        post.Project.Name,
		Technologies:       technologies,
		Problem:            candidate.Problem,
		Symptoms:           cloneStrings(candidate.Symptoms),
		RootCause:          candidate.RootCause,
		AffectedComponents: cloneStrings(candidate.AffectedComponents),
		AffectedFiles:      cloneStrings(candidate.AffectedFiles),
		Attempts:           append([]Attempt(nil), candidate.Attempts...),
		FinalFix:           candidate.FinalFix,
		PreRun:             runReference(pre, preLoaded.ReportSHA256),
		PostRun:            runReference(post, postLoaded.ReportSHA256),
		CheckTransitions:   transitions,
		ChangeFingerprint:  changeFingerprint(pre, post),
		PatchEvidencePath:  patchPath,
		PatchSHA256:        patchHash,
		KnownLimitations:   cloneStrings(candidate.KnownLimitations),
		Applicability:      candidate.Applicability,
		RelevantVersions:   cloneMap(candidate.RelevantVersions),
		RelatedEvidence:    relatedEvidence,
		RelatedFixIDs:      sortedCopy(candidate.RelatedFixIDs),
		Supersedes:         candidate.Supersedes,
		Tags:               sortedCopy(candidate.Tags),
	}
	record.RecordHash, err = recordHash(record)
	if err != nil {
		return Record{}, fmt.Errorf("hash Fix Record: %w", err)
	}
	if err := validateStoredRecord(record); err != nil {
		return Record{}, fmt.Errorf("derive valid Fix Record: %w", err)
	}
	return record, nil
}

func validateClosureReports(root string, candidate Candidate, pre, post model.Report) error {
	canonicalRoot, err := canonicalProject(root)
	if err != nil {
		return err
	}
	if err := validateClosureReport(canonicalRoot, pre); err != nil {
		return fmt.Errorf("pre-fix report is not eligible: %w", err)
	}
	if err := validateClosureReport(canonicalRoot, post); err != nil {
		return fmt.Errorf("post-fix report is not eligible: %w", err)
	}
	if pre.RunID == post.RunID {
		return errors.New("pre-fix and post-fix runs must be different")
	}
	if pre.Project.Identity != post.Project.Identity || candidate.ProjectID != pre.Project.Identity {
		return errors.New("candidate and evidence project identities do not match")
	}
	if pre.Project.Name != post.Project.Name || !equalStrings(technologyIDs(*pre.Project), technologyIDs(*post.Project)) {
		return errors.New("pre-fix and post-fix project metadata do not match")
	}
	if post.StartedAt.Before(pre.FinishedAt) || !post.FinishedAt.After(pre.FinishedAt) {
		return errors.New("post-fix run must begin no earlier than the completed pre-fix run")
	}
	if verify.ExitCode(post) != 0 {
		return errors.New("complete post-fix verification still has a blocking result")
	}
	_, err = deriveTransitions(candidate.CheckIDs, pre, post)
	return err
}

func validateClosureReport(canonicalRoot string, report model.Report) error {
	if report.SchemaVersion != "1" || report.Command != "verify" {
		return errors.New("report is not a supported verification report")
	}
	if report.Project == nil {
		return errors.New("report project identity is missing")
	}
	if err := validateRequiredText("report project identity", report.Project.Identity, 128); err != nil {
		return err
	}
	if err := validateRequiredText("report project name", report.Project.Name, 256); err != nil {
		return err
	}
	if err := validateRequiredText("report project path", report.Project.Path, maxTextRunes); err != nil {
		return err
	}
	reportedRoot, err := filepath.Abs(report.Project.Path)
	if err != nil {
		return errors.New("report project path is invalid")
	}
	reportedRoot, err = filepath.EvalSymlinks(reportedRoot)
	if err != nil || !samePath(reportedRoot, canonicalRoot) {
		return errors.New("report project path does not match the selected project")
	}
	if report.StartedAt.IsZero() || report.FinishedAt.IsZero() || report.StartedAt.After(report.FinishedAt) {
		return errors.New("report timestamps are incomplete or reversed")
	}
	for _, field := range []namedText{
		{name: "policy_version", value: report.PolicyVersion},
		{name: "devctl_version", value: report.DevctlVersion},
		{name: "devctl_commit", value: report.DevctlCommit},
		{name: "repository_revision", value: report.RepositoryRevision},
		{name: "repository_fingerprint", value: report.RepositoryFingerprint},
	} {
		if err := validateRequiredText(field.name, field.value, 256); err != nil {
			return err
		}
	}
	if !sha256Pattern.MatchString(report.RepositoryFingerprint) {
		return errors.New("repository fingerprint is not a SHA-256 value")
	}
	if len(report.Checks) == 0 {
		return errors.New("verification report contains no checks")
	}
	if len(report.Project.Technologies) == 0 || len(report.Project.Technologies) > maxListItems {
		return errors.New("report technology provenance is incomplete")
	}
	seenTechnologies := make(map[string]struct{}, len(report.Project.Technologies))
	for _, technology := range report.Project.Technologies {
		if err := validateRequiredText("report technology ID", technology.ID, 128); err != nil {
			return err
		}
		if err := validateRequiredText("report technology confidence", technology.Confidence, 64); err != nil {
			return err
		}
		if _, exists := seenTechnologies[technology.ID]; exists {
			return errors.New("report contains duplicate technology IDs")
		}
		seenTechnologies[technology.ID] = struct{}{}
	}
	for _, check := range report.Checks {
		if !validStatus(check.Status) {
			return fmt.Errorf("check %q has an invalid status", check.ID)
		}
		if err := validateRequiredText("check version", check.CheckVersion, 128); err != nil {
			return fmt.Errorf("check %q has incomplete version provenance: %w", check.ID, err)
		}
	}
	if report.Overall != computedOverall(report.Checks) {
		return errors.New("report overall status does not match its check results")
	}
	return nil
}

func deriveTransitions(checkIDs []string, pre, post model.Report) ([]CheckTransition, error) {
	preChecks := checksByID(pre.Checks)
	postChecks := checksByID(post.Checks)
	ids := sortedCopy(checkIDs)
	transitions := make([]CheckTransition, 0, len(ids))
	for _, id := range ids {
		before, beforeFound := preChecks[id]
		after, afterFound := postChecks[id]
		if !beforeFound || !afterFound {
			return nil, fmt.Errorf("target check %q is missing from pre-fix or post-fix evidence", id)
		}
		if before.Status == model.Pass || before.Status == model.Skip || before.Status == model.NotApplicable {
			return nil, fmt.Errorf("target check %q was not unresolved before the fix", id)
		}
		if after.Status != model.Pass {
			return nil, fmt.Errorf("target check %q did not PASS after the fix", id)
		}
		transitions = append(transitions, CheckTransition{
			CheckID:        id,
			BeforeVersion:  before.CheckVersion,
			BeforeStatus:   before.Status,
			BeforeBlocking: before.Blocking,
			AfterVersion:   after.CheckVersion,
			AfterStatus:    after.Status,
			AfterBlocking:  after.Blocking,
		})
	}
	return transitions, nil
}

func validatePatchArtifact(root, relativePath, expectedHash string) (string, string, error) {
	if relativePath == "" && expectedHash == "" {
		return "", "", nil
	}
	if relativePath == "" || expectedHash == "" || !sha256Pattern.MatchString(expectedHash) {
		return "", "", errors.New("patch artifact path and lowercase SHA-256 must be supplied together")
	}
	normalized := filepath.ToSlash(filepath.Clean(filepath.FromSlash(relativePath)))
	if normalized != relativePath || filepath.IsAbs(filepath.FromSlash(relativePath)) || !strings.HasPrefix(normalized, ".devctl/evidence/") {
		return "", "", errors.New("patch artifact must be a canonical relative path under .devctl/evidence")
	}
	canonicalRoot, err := canonicalProject(root)
	if err != nil {
		return "", "", err
	}
	evidenceRoot := filepath.Join(canonicalRoot, ".devctl", "evidence")
	if err := normalDirectory(evidenceRoot); err != nil {
		return "", "", errors.New("patch evidence root is not a normal directory")
	}
	canonicalEvidence, err := filepath.EvalSymlinks(evidenceRoot)
	if err != nil || !contained(canonicalEvidence, canonicalRoot) {
		return "", "", errors.New("patch evidence root escapes the project")
	}
	path := filepath.Join(canonicalRoot, filepath.FromSlash(normalized))
	info, err := os.Lstat(path)
	if err != nil {
		return "", "", fmt.Errorf("read patch artifact: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", "", errors.New("patch artifact is not a normal file")
	}
	canonicalPath, err := filepath.EvalSymlinks(path)
	if err != nil || !contained(canonicalPath, canonicalEvidence) {
		return "", "", errors.New("patch artifact escapes .devctl/evidence")
	}
	file, err := os.Open(canonicalPath)
	if err != nil {
		return "", "", fmt.Errorf("open patch artifact: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxPatchBytes+1))
	if err != nil {
		return "", "", fmt.Errorf("read patch artifact: %w", err)
	}
	if len(data) > MaxPatchBytes {
		return "", "", fmt.Errorf("patch artifact exceeds the %d-byte limit", MaxPatchBytes)
	}
	digest := sha256.Sum256(data)
	actualHash := hex.EncodeToString(digest[:])
	if actualHash != expectedHash {
		return "", "", errors.New("patch artifact SHA-256 does not match the candidate")
	}
	return normalized, actualHash, nil
}

func runReference(report model.Report, reportHash string) RunReference {
	return RunReference{
		RunID:                 report.RunID,
		EvidencePath:          report.EvidencePath,
		ReportSHA256:          reportHash,
		Overall:               report.Overall,
		StartedAt:             report.StartedAt,
		FinishedAt:            report.FinishedAt,
		PolicyVersion:         report.PolicyVersion,
		DevctlVersion:         report.DevctlVersion,
		DevctlCommit:          report.DevctlCommit,
		DevctlDirty:           report.DevctlDirty,
		RepositoryRevision:    report.RepositoryRevision,
		RepositoryDirty:       report.RepositoryDirty,
		RepositoryFingerprint: report.RepositoryFingerprint,
	}
}

func changeFingerprint(pre, post model.Report) string {
	digest := sha256.Sum256([]byte(pre.RepositoryFingerprint + "\x00" + post.RepositoryFingerprint))
	return hex.EncodeToString(digest[:])
}

func technologyIDs(project model.Project) []string {
	ids := make([]string, 0, len(project.Technologies))
	seen := make(map[string]struct{}, len(project.Technologies))
	for _, technology := range project.Technologies {
		id := strings.TrimSpace(technology.ID)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func checksByID(checks []model.CheckResult) map[string]model.CheckResult {
	result := make(map[string]model.CheckResult, len(checks))
	for _, check := range checks {
		result[check.ID] = check
	}
	return result
}

func computedOverall(checks []model.CheckResult) model.Status {
	priority := map[model.Status]int{
		model.Pass: 0, model.NotApplicable: 0, model.Skip: 0, model.Warn: 1,
		model.NotTested: 2, model.InsufficientEvidence: 2, model.RequiresReview: 2,
		model.Fail: 3, model.Error: 4,
	}
	worst := model.Pass
	for _, check := range checks {
		if priority[check.Status] > priority[worst] {
			worst = check.Status
		}
	}
	return worst
}

func validStatus(status model.Status) bool {
	switch status {
	case model.Pass, model.Warn, model.Fail, model.Error, model.Skip, model.NotApplicable, model.NotTested, model.InsufficientEvidence, model.RequiresReview:
		return true
	default:
		return false
	}
}

func cloneStrings(values []string) []string {
	return append([]string(nil), values...)
}

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
