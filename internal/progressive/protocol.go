package progressive

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"devctl/internal/model"
)

const SchemaVersion = "1"

// MaxResultBytes includes the terminating newline written to stdout.
const MaxResultBytes = 16 * 1024

const (
	maxText            = 512
	maxFailurePage     = 64
	maxFindingPage     = 64
	maxEvidencePaths   = 16
	maxEvidenceContent = 8 * 1024
)

type Result struct {
	SchemaVersion         string            `json:"schema_version"`
	Operation             string            `json:"operation"`
	Accepted              bool              `json:"accepted"`
	ExitCode              int               `json:"exit_code"`
	RunID                 string            `json:"run_id,omitempty"`
	Overall               model.Status      `json:"overall,omitempty"`
	ProjectID             string            `json:"project_id,omitempty"`
	EvidencePath          string            `json:"evidence_path,omitempty"`
	DevctlVersion         string            `json:"devctl_version,omitempty"`
	DevctlCommit          string            `json:"devctl_commit,omitempty"`
	DevctlDirty           bool              `json:"devctl_dirty,omitempty"`
	PolicyVersion         string            `json:"policy_version,omitempty"`
	RepositoryRevision    string            `json:"repository_revision,omitempty"`
	RepositoryDirty       bool              `json:"repository_dirty,omitempty"`
	RepositoryFingerprint string            `json:"repository_fingerprint,omitempty"`
	FailuresTotal         int               `json:"failures_total"`
	FailuresReturned      int               `json:"failures_returned"`
	ItemsOffset           int64             `json:"items_offset,omitempty"`
	NextItemsOffset       int64             `json:"next_items_offset,omitempty"`
	Truncated             bool              `json:"truncated,omitempty"`
	Next                  string            `json:"next,omitempty"`
	AgentResponseBytes    int64             `json:"agent_response_bytes"`
	Failures              []Failure         `json:"failures,omitempty"`
	Failure               *Failure          `json:"failure,omitempty"`
	Evidence              *EvidenceFragment `json:"evidence,omitempty"`
	Error                 *ProtocolError    `json:"error,omitempty"`
}

type Failure struct {
	FailureID             string          `json:"failure_id"`
	CheckID               string          `json:"check_id"`
	CheckVersion          string          `json:"check_version,omitempty"`
	Status                model.Status    `json:"status"`
	Blocking              bool            `json:"blocking,omitempty"`
	Summary               string          `json:"summary"`
	Reason                string          `json:"reason,omitempty"`
	FindingsTotal         int             `json:"findings_total,omitempty"`
	FindingsReturned      int             `json:"findings_returned,omitempty"`
	Findings              []model.Finding `json:"findings,omitempty"`
	EvidencePathsTotal    int             `json:"evidence_paths_total,omitempty"`
	EvidencePathsReturned int             `json:"evidence_paths_returned,omitempty"`
	EvidencePaths         []string        `json:"evidence_paths,omitempty"`
	EvidenceAvailable     bool            `json:"evidence_available"`
	SourceOutputTruncated bool            `json:"source_output_truncated,omitempty"`
}

type EvidenceFragment struct {
	FailureID             string `json:"failure_id"`
	CheckID               string `json:"check_id"`
	CheckVersion          string `json:"check_version,omitempty"`
	RawBytesTotal         int64  `json:"raw_bytes_total"`
	RawOffset             int64  `json:"raw_offset"`
	RawBytesRead          int64  `json:"raw_bytes_read"`
	NextOffset            int64  `json:"next_offset,omitempty"`
	ContentBytesReturned  int64  `json:"content_bytes_returned"`
	Content               string `json:"content"`
	SourceOutputTruncated bool   `json:"source_output_truncated,omitempty"`
}

type ProtocolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Error struct {
	Code string
	Err  error
}

func (err *Error) Error() string {
	if err == nil || err.Err == nil {
		return "progressive retrieval failed"
	}
	return err.Err.Error()
}

func (err *Error) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}

func NewError(code, message string) error {
	return &Error{Code: code, Err: errors.New(message)}
}

func ErrorCode(err error) string {
	var retrievalError *Error
	if errors.As(err, &retrievalError) && retrievalError.Code != "" {
		return retrievalError.Code
	}
	return "retrieval_unavailable"
}

func Rejected(operation, code, message string) Result {
	return Result{
		SchemaVersion: SchemaVersion,
		Operation:     operation,
		Accepted:      false,
		ExitCode:      2,
		Error:         &ProtocolError{Code: code, Message: message},
	}
}

func Encode(result Result) ([]byte, error) {
	result = sanitizeResult(result)
	for {
		encoded, err := encodeWithResponseSize(result)
		if err != nil {
			return nil, err
		}
		if len(encoded) <= MaxResultBytes {
			return encoded, nil
		}
		result.Truncated = true
		switch {
		case len(result.Failures) > 0:
			result.Failures = result.Failures[:len(result.Failures)-1]
			result.FailuresReturned = len(result.Failures)
			refreshNext(&result)
		case result.Failure != nil && len(result.Failure.Findings) > 0:
			result.Failure.Findings = result.Failure.Findings[:len(result.Failure.Findings)-1]
			result.Failure.FindingsReturned = len(result.Failure.Findings)
			refreshNext(&result)
		case result.Failure != nil && len(result.Failure.EvidencePaths) > 0:
			result.Failure.EvidencePaths = result.Failure.EvidencePaths[:len(result.Failure.EvidencePaths)-1]
			result.Failure.EvidencePathsReturned = len(result.Failure.EvidencePaths)
		default:
			return nil, fmt.Errorf("progressive result cannot be represented within %d bytes", MaxResultBytes)
		}
	}
}

func refreshNext(result *Result) {
	switch result.Operation {
	case "failures":
		result.NextItemsOffset = result.ItemsOffset + int64(result.FailuresReturned)
		result.Next = fmt.Sprintf("devctl failures %s --offset %d --json", result.RunID, result.NextItemsOffset)
	case "failure":
		if result.Failure == nil {
			return
		}
		result.NextItemsOffset = result.ItemsOffset + int64(result.Failure.FindingsReturned)
		result.Next = fmt.Sprintf("devctl failure %s %s --offset %d --json", result.RunID, result.Failure.FailureID, result.NextItemsOffset)
	}
}

func encodeWithResponseSize(result Result) ([]byte, error) {
	for attempts := 0; attempts < 8; attempts++ {
		data, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}
		size := int64(len(data) + 1)
		if result.AgentResponseBytes == size {
			return append(data, '\n'), nil
		}
		result.AgentResponseBytes = size
	}
	return nil, errors.New("progressive response size did not stabilize")
}

var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
var awsAccessKeyPattern = regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)
var privateKeyMarkerPattern = regexp.MustCompile(`-----BEGIN (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----`)
var privateKeyEndMarkerPattern = regexp.MustCompile(`-----END (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----`)
var secretAssignmentPattern = regexp.MustCompile(`(?i)\b(api[_-]?key|secret|password|token)\s*[:=]\s*["']?[A-Za-z0-9/+=_-]{16,}`)

func sanitizeResult(result Result) Result {
	result.SchemaVersion = boundedText(result.SchemaVersion)
	result.Operation = boundedText(result.Operation)
	result.RunID = boundedText(result.RunID)
	result.ProjectID = boundedText(result.ProjectID)
	result.EvidencePath = boundedText(result.EvidencePath)
	result.DevctlVersion = boundedText(result.DevctlVersion)
	result.DevctlCommit = boundedText(result.DevctlCommit)
	result.PolicyVersion = boundedText(result.PolicyVersion)
	result.RepositoryRevision = boundedText(result.RepositoryRevision)
	result.RepositoryFingerprint = boundedText(result.RepositoryFingerprint)
	result.Next = boundedText(result.Next)
	result.Failures = append([]Failure(nil), result.Failures...)
	for index := range result.Failures {
		result.Failures[index] = sanitizeFailure(result.Failures[index])
	}
	if result.Failure != nil {
		failure := sanitizeFailure(*result.Failure)
		result.Failure = &failure
	}
	if result.Evidence != nil {
		fragment := *result.Evidence
		fragment.FailureID = boundedText(fragment.FailureID)
		fragment.CheckID = boundedText(fragment.CheckID)
		fragment.CheckVersion = boundedText(fragment.CheckVersion)
		fragment.Content = sanitizeEvidenceText(fragment.Content)
		fragment.ContentBytesReturned = int64(len(fragment.Content))
		result.Evidence = &fragment
	}
	if result.Error != nil {
		protocolError := *result.Error
		protocolError.Code = boundedText(protocolError.Code)
		protocolError.Message = boundedText(protocolError.Message)
		result.Error = &protocolError
	}
	return result
}

// Sanitize applies the same untrusted-text boundary used by JSON encoding to
// the human-readable presentation path.
func Sanitize(result Result) Result {
	return sanitizeResult(result)
}

func sanitizeFailure(failure Failure) Failure {
	failure.FailureID = boundedText(failure.FailureID)
	failure.CheckID = boundedText(failure.CheckID)
	failure.CheckVersion = boundedText(failure.CheckVersion)
	failure.Summary = boundedText(failure.Summary)
	failure.Reason = boundedText(failure.Reason)
	failure.Findings = append([]model.Finding(nil), failure.Findings...)
	for index := range failure.Findings {
		finding := &failure.Findings[index]
		finding.FindingID = boundedText(finding.FindingID)
		finding.Severity = boundedText(finding.Severity)
		finding.Component = boundedText(finding.Component)
		finding.Version = boundedText(finding.Version)
		finding.Issue = boundedText(finding.Issue)
		finding.Action = boundedText(finding.Action)
		finding.Path = boundedText(finding.Path)
		finding.EvidencePath = boundedText(finding.EvidencePath)
		finding.Source = boundedText(finding.Source)
		finding.ToolVersion = boundedText(finding.ToolVersion)
		finding.Project = boundedText(finding.Project)
	}
	failure.EvidencePaths = append([]string(nil), failure.EvidencePaths...)
	if len(failure.EvidencePaths) > maxEvidencePaths {
		failure.EvidencePaths = failure.EvidencePaths[:maxEvidencePaths]
	}
	for index := range failure.EvidencePaths {
		failure.EvidencePaths[index] = boundedText(failure.EvidencePaths[index])
	}
	failure.EvidencePathsReturned = len(failure.EvidencePaths)
	return failure
}

func boundedText(value string) string {
	value = ansiEscapePattern.ReplaceAllString(value, "")
	value = awsAccessKeyPattern.ReplaceAllString(value, "[REDACTED_AWS_ACCESS_KEY]")
	if privateKeyMarkerPattern.MatchString(value) {
		return "[REDACTED_PRIVATE_KEY]"
	}
	value = secretAssignmentPattern.ReplaceAllString(value, "$1=[REDACTED]")
	value = strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return ' '
		}
		return character
	}, value)
	runes := []rune(strings.Join(strings.Fields(value), " "))
	if len(runes) <= maxText {
		return string(runes)
	}
	return string(runes[:maxText-3]) + "..."
}

func sanitizeEvidenceText(value string) string {
	value = strings.ToValidUTF8(value, "?")
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	value = ansiEscapePattern.ReplaceAllString(value, "")
	value = awsAccessKeyPattern.ReplaceAllString(value, "[REDACTED_AWS_ACCESS_KEY]")
	value = secretAssignmentPattern.ReplaceAllString(value, "$1=[REDACTED]")
	value = strings.Map(func(character rune) rune {
		if character == '\n' || character == '\t' {
			return character
		}
		if unicode.IsControl(character) {
			return ' '
		}
		return character
	}, value)
	runes := []rune(value)
	if len(runes) > maxEvidenceContent {
		return string(runes[:maxEvidenceContent])
	}
	return string(runes)
}
