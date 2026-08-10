package model

import "time"

type Status string

const (
	Pass                 Status = "PASS"
	Warn                 Status = "WARN"
	Fail                 Status = "FAIL"
	Error                Status = "ERROR"
	Skip                 Status = "SKIP"
	NotApplicable        Status = "NOT_APPLICABLE"
	NotTested            Status = "NOT_TESTED"
	InsufficientEvidence Status = "INSUFFICIENT_EVIDENCE"
	RequiresReview       Status = "REQUIRES_REVIEW"
)

type Technology struct {
	ID         string   `json:"id"`
	Confidence string   `json:"confidence"`
	Markers    []string `json:"markers"`
}

type Project struct {
	Name         string       `json:"name"`
	Path         string       `json:"path"`
	Technologies []Technology `json:"technologies"`
}

type Evidence struct {
	Type   string `json:"type"`
	Path   string `json:"path,omitempty"`
	Detail string `json:"detail,omitempty"`
}

type Finding struct {
	FindingID    string `json:"finding_id"`
	Severity     string `json:"severity"`
	Component    string `json:"component,omitempty"`
	Version      string `json:"version,omitempty"`
	Issue        string `json:"issue"`
	Action       string `json:"action,omitempty"`
	Path         string `json:"path,omitempty"`
	EvidencePath string `json:"evidence_path,omitempty"`
	Source       string `json:"source,omitempty"`
	ToolVersion  string `json:"tool_version,omitempty"`
	Project      string `json:"project,omitempty"`
}

type CheckResult struct {
	ID           string     `json:"check_id"`
	CheckVersion string     `json:"check_version,omitempty"`
	Status       Status     `json:"status"`
	Blocking     bool       `json:"blocking,omitempty"`
	Summary      string     `json:"summary"`
	Reason       string     `json:"reason,omitempty"`
	DurationMS   int64      `json:"duration_ms"`
	ExitCode     *int       `json:"exit_code,omitempty"`
	Evidence     []Evidence `json:"evidence,omitempty"`
	Findings     []Finding  `json:"findings,omitempty"`
	RawOutput    string     `json:"raw_output,omitempty"`
	ErrorDetail  string     `json:"error,omitempty"`
}

type Report struct {
	SchemaVersion string        `json:"schema_version"`
	Command       string        `json:"command"`
	RunID         string        `json:"run_id"`
	PolicyVersion string        `json:"policy_version,omitempty"`
	DevctlVersion string        `json:"devctl_version,omitempty"`
	DevctlCommit  string        `json:"devctl_commit,omitempty"`
	EvidencePath  string        `json:"evidence_path,omitempty"`
	StartedAt     time.Time     `json:"started_at"`
	FinishedAt    time.Time     `json:"finished_at"`
	Project       *Project      `json:"project,omitempty"`
	Projects      []Project     `json:"projects,omitempty"`
	Checks        []CheckResult `json:"checks,omitempty"`
	Overall       Status        `json:"overall"`
}

// SessionState is deliberately small and contains no credentials or raw logs.
// It describes where work stopped; it is not verification evidence.
type SessionState struct {
	SchemaVersion string    `json:"schema_version"`
	Project       string    `json:"project"`
	ProjectPath   string    `json:"project_path"`
	Branch        string    `json:"branch,omitempty"`
	LastCommit    string    `json:"last_commit,omitempty"`
	CurrentTask   string    `json:"current_task,omitempty"`
	LastResult    Status    `json:"last_result,omitempty"`
	EvidencePath  string    `json:"evidence_path,omitempty"`
	CIState       string    `json:"ci_state,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type FailurePacket struct {
	SchemaVersion string        `json:"schema_version"`
	RunID         string        `json:"run_id"`
	Project       string        `json:"project,omitempty"`
	Overall       Status        `json:"overall"`
	DevctlVersion string        `json:"devctl_version,omitempty"`
	DevctlCommit  string        `json:"devctl_commit,omitempty"`
	EvidencePath  string        `json:"evidence_path,omitempty"`
	Failures      []FailureItem `json:"failures"`
	NextAction    string        `json:"next_action"`
}

type FailureItem struct {
	CheckID       string    `json:"check_id"`
	Status        Status    `json:"status"`
	Blocking      bool      `json:"blocking"`
	Summary       string    `json:"summary"`
	Reason        string    `json:"reason,omitempty"`
	EvidencePaths []string  `json:"evidence_paths,omitempty"`
	Findings      []Finding `json:"findings,omitempty"`
}
