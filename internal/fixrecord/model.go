package fixrecord

import (
	"io"
	"time"

	"devctl/internal/model"
)

const (
	CandidateSchemaVersion = "1"
	RecordSchemaVersion    = "1"
	ClosureRuleVersion     = "fix-closure-v1"
	StatusVerified         = "VERIFIED"
	MaxCandidateBytes      = 64 * 1024
	MaxRecordBytes         = 128 * 1024
	MaxPatchBytes          = 2 * 1024 * 1024
)

type Attempt struct {
	Outcome     string `json:"outcome"`
	Description string `json:"description"`
	Reason      string `json:"reason"`
}

type Candidate struct {
	SchemaVersion      string            `json:"schema_version"`
	ID                 string            `json:"id"`
	Title              string            `json:"title"`
	ProjectID          string            `json:"project_id"`
	Problem            string            `json:"problem"`
	Symptoms           []string          `json:"symptoms"`
	RootCause          string            `json:"root_cause"`
	AffectedComponents []string          `json:"affected_components,omitempty"`
	AffectedFiles      []string          `json:"affected_files,omitempty"`
	Attempts           []Attempt         `json:"attempts,omitempty"`
	FinalFix           string            `json:"final_fix"`
	PreRunID           string            `json:"pre_run_id"`
	PostRunID          string            `json:"post_run_id"`
	CheckIDs           []string          `json:"check_ids"`
	PatchEvidencePath  string            `json:"patch_evidence_path,omitempty"`
	PatchSHA256        string            `json:"patch_sha256,omitempty"`
	KnownLimitations   []string          `json:"known_limitations,omitempty"`
	Applicability      string            `json:"applicability,omitempty"`
	RelevantVersions   map[string]string `json:"relevant_versions,omitempty"`
	RelatedFixIDs      []string          `json:"related_fix_ids,omitempty"`
	Supersedes         string            `json:"supersedes,omitempty"`
	Tags               []string          `json:"tags,omitempty"`
}

type RunReference struct {
	RunID                 string       `json:"run_id"`
	EvidencePath          string       `json:"evidence_path"`
	ReportSHA256          string       `json:"report_sha256"`
	Overall               model.Status `json:"overall"`
	StartedAt             time.Time    `json:"started_at"`
	FinishedAt            time.Time    `json:"finished_at"`
	PolicyVersion         string       `json:"policy_version"`
	DevctlVersion         string       `json:"devctl_version"`
	DevctlCommit          string       `json:"devctl_commit"`
	DevctlDirty           bool         `json:"devctl_dirty"`
	RepositoryRevision    string       `json:"repository_revision"`
	RepositoryDirty       bool         `json:"repository_dirty"`
	RepositoryFingerprint string       `json:"repository_fingerprint"`
}

type CheckTransition struct {
	CheckID        string       `json:"check_id"`
	BeforeVersion  string       `json:"before_version"`
	BeforeStatus   model.Status `json:"before_status"`
	BeforeBlocking bool         `json:"before_blocking"`
	AfterVersion   string       `json:"after_version"`
	AfterStatus    model.Status `json:"after_status"`
	AfterBlocking  bool         `json:"after_blocking"`
}

type Record struct {
	SchemaVersion      string            `json:"schema_version"`
	ID                 string            `json:"id"`
	Status             string            `json:"status"`
	ClosureRule        string            `json:"closure_rule"`
	RecordedAt         time.Time         `json:"recorded_at"`
	Title              string            `json:"title"`
	ProjectID          string            `json:"project_id"`
	ProjectName        string            `json:"project_name"`
	Technologies       []string          `json:"technologies,omitempty"`
	Problem            string            `json:"problem"`
	Symptoms           []string          `json:"symptoms"`
	RootCause          string            `json:"root_cause"`
	AffectedComponents []string          `json:"affected_components,omitempty"`
	AffectedFiles      []string          `json:"affected_files,omitempty"`
	Attempts           []Attempt         `json:"attempts,omitempty"`
	FinalFix           string            `json:"final_fix"`
	PreRun             RunReference      `json:"pre_run"`
	PostRun            RunReference      `json:"post_run"`
	CheckTransitions   []CheckTransition `json:"check_transitions"`
	ChangeFingerprint  string            `json:"change_fingerprint"`
	PatchEvidencePath  string            `json:"patch_evidence_path,omitempty"`
	PatchSHA256        string            `json:"patch_sha256,omitempty"`
	KnownLimitations   []string          `json:"known_limitations,omitempty"`
	Applicability      string            `json:"applicability,omitempty"`
	RelevantVersions   map[string]string `json:"relevant_versions,omitempty"`
	RelatedEvidence    []string          `json:"related_evidence"`
	RelatedFixIDs      []string          `json:"related_fix_ids,omitempty"`
	Supersedes         string            `json:"supersedes,omitempty"`
	Tags               []string          `json:"tags,omitempty"`
	RecordHash         string            `json:"record_hash"`
}

type Summary struct {
	ID         string    `json:"id"`
	Status     string    `json:"status"`
	RecordedAt time.Time `json:"recorded_at"`
	Title      string    `json:"title"`
	ProjectID  string    `json:"project_id"`
	PreRunID   string    `json:"pre_run_id"`
	PostRunID  string    `json:"post_run_id"`
	Supersedes string    `json:"supersedes,omitempty"`
	RecordHash string    `json:"record_hash"`
}

type Options struct {
	Now func() time.Time
}

func DecodeCandidate(reader io.Reader) (Candidate, error) {
	return decodeCandidate(reader)
}
