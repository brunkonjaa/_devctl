package worker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"

	"devctl/internal/handoff"
	"devctl/internal/model"
)

const ProtocolVersion = "1"
const AgentResultSchemaVersion = "1"

// MaxAgentResultBytes includes the terminating newline written to stdout.
const MaxAgentResultBytes = 16 * 1024

const (
	maxRequestBytes     = 64 * 1024
	maxIdentifierLength = 128
	maxChecks           = 256
	maxText             = 512
	maxFailureItems     = 16
	maxFailureFindings  = 8
	maxFailureEvidence  = 16
)

type Request struct {
	SchemaVersion string `json:"schema_version"`
	RequestID     string `json:"request_id"`
	Operation     string `json:"operation"`
	ProjectID     string `json:"project_id"`
}

type Result struct {
	SchemaVersion         string               `json:"schema_version"`
	RequestID             string               `json:"request_id,omitempty"`
	Operation             string               `json:"operation,omitempty"`
	VerificationClass     string               `json:"verification_class,omitempty"`
	Accepted              bool                 `json:"accepted"`
	ExitCode              int                  `json:"exit_code"`
	StartedAt             time.Time            `json:"started_at"`
	FinishedAt            time.Time            `json:"finished_at"`
	ProjectID             string               `json:"project_id,omitempty"`
	RunID                 string               `json:"run_id,omitempty"`
	Overall               model.Status         `json:"overall,omitempty"`
	EvidencePath          string               `json:"evidence_path,omitempty"`
	DevctlVersion         string               `json:"devctl_version,omitempty"`
	DevctlCommit          string               `json:"devctl_commit,omitempty"`
	DevctlDirty           bool                 `json:"devctl_dirty,omitempty"`
	PolicyVersion         string               `json:"policy_version,omitempty"`
	RepositoryRevision    string               `json:"repository_revision,omitempty"`
	RepositoryDirty       bool                 `json:"repository_dirty,omitempty"`
	RepositoryFingerprint string               `json:"repository_fingerprint,omitempty"`
	ChecksTotal           int                  `json:"checks_total,omitempty"`
	ChecksReturned        int                  `json:"checks_returned,omitempty"`
	FailuresTotal         int                  `json:"failures_total,omitempty"`
	FailuresReturned      int                  `json:"failures_returned,omitempty"`
	Truncated             bool                 `json:"truncated,omitempty"`
	Next                  string               `json:"next,omitempty"`
	InformationFlow       *InformationFlow     `json:"information_flow,omitempty"`
	Checks                []CheckSummary       `json:"checks,omitempty"`
	FailurePacket         *model.FailurePacket `json:"failure_packet,omitempty"`
	Error                 *ProtocolError       `json:"error,omitempty"`
}

type CheckSummary struct {
	ID           string       `json:"check_id"`
	CheckVersion string       `json:"check_version,omitempty"`
	Status       model.Status `json:"status"`
	Blocking     bool         `json:"blocking,omitempty"`
	Summary      string       `json:"summary"`
	Reason       string       `json:"reason,omitempty"`
	DurationMS   int64        `json:"duration_ms"`
}

type InformationFlow struct {
	RawSubprocessBytes      int64 `json:"raw_subprocess_bytes"`
	RetainedSubprocessBytes int64 `json:"retained_subprocess_bytes"`
	LocalEvidenceBytes      int64 `json:"local_evidence_bytes"`
	LocalEvidenceMeasured   bool  `json:"local_evidence_measured"`
	AgentResponseBytes      int64 `json:"agent_response_bytes"`
	OutputTruncated         bool  `json:"output_truncated"`
}

type ProtocolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func ReadRequest(path string) (Request, error) {
	file, err := os.Open(path)
	if err != nil {
		return Request{}, fmt.Errorf("open worker request: %w", err)
	}
	defer file.Close()
	return DecodeRequest(file)
}

func DecodeRequest(reader io.Reader) (Request, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxRequestBytes+1))
	if err != nil {
		return Request{}, fmt.Errorf("read worker request: %w", err)
	}
	if len(data) > maxRequestBytes {
		return Request{}, fmt.Errorf("worker request exceeds %d bytes", maxRequestBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var request Request
	if err := decoder.Decode(&request); err != nil {
		return Request{}, fmt.Errorf("parse worker request: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Request{}, errors.New("worker request must contain one JSON object")
		}
		return Request{}, fmt.Errorf("read worker request: %w", err)
	}
	if err := ValidateRequest(request); err != nil {
		return Request{}, err
	}
	return request, nil
}

func ValidateRequest(request Request) error {
	if request.SchemaVersion != ProtocolVersion {
		return fmt.Errorf("unsupported worker request schema version %q", request.SchemaVersion)
	}
	if err := validateIdentifier(request.RequestID, "request_id"); err != nil {
		return err
	}
	if request.Operation != "verify" {
		return fmt.Errorf("unsupported worker operation %q", request.Operation)
	}
	if err := validateIdentifier(request.ProjectID, "project_id"); err != nil {
		return err
	}
	return nil
}

func validateIdentifier(value, name string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("worker %s must not be empty", name)
	}
	if len([]rune(trimmed)) > maxIdentifierLength {
		return fmt.Errorf("worker %s is too long", name)
	}
	if strings.IndexFunc(trimmed, unicode.IsControl) >= 0 {
		return fmt.Errorf("worker %s contains a control character", name)
	}
	return nil
}

func NewVerificationResult(request Request, report model.Report, exitCode int, startedAt, finishedAt time.Time) Result {
	result := newVerificationResult(report, exitCode, startedAt, finishedAt, nil)
	result.RequestID = boundedText(request.RequestID)
	result.Operation = boundedText(request.Operation)
	result.ProjectID = boundedText(request.ProjectID)
	return result
}

func NewAgentVerificationResult(report model.Report, exitCode int, startedAt, finishedAt time.Time, informationFlow InformationFlow) Result {
	result := newVerificationResult(report, exitCode, startedAt, finishedAt, &informationFlow)
	result.SchemaVersion = AgentResultSchemaVersion
	result.Operation = "verify"
	result.VerificationClass = "local-full"
	result.PolicyVersion = boundedText(report.PolicyVersion)
	result.RepositoryRevision = boundedText(report.RepositoryRevision)
	result.RepositoryDirty = report.RepositoryDirty
	result.RepositoryFingerprint = boundedText(report.RepositoryFingerprint)
	result.ChecksTotal = len(report.Checks)
	result.ChecksReturned = len(result.Checks)
	for index := range result.Checks {
		if index < len(report.Checks) {
			result.Checks[index].CheckVersion = boundedText(report.Checks[index].CheckVersion)
		}
	}
	if result.ChecksReturned < result.ChecksTotal {
		result.Truncated = true
	}
	packet := handoff.FromReport(report)
	result.FailuresTotal = len(packet.Failures)
	if result.FailurePacket != nil {
		result.FailuresReturned = len(result.FailurePacket.Failures)
	}
	if result.FailuresReturned < result.FailuresTotal {
		result.Truncated = true
	}
	if result.Truncated {
		result.Next = "request bounded failure details from local evidence"
	}
	if report.Project != nil {
		result.ProjectID = boundedText(report.Project.Identity)
	}
	return result
}

func newVerificationResult(report model.Report, exitCode int, startedAt, finishedAt time.Time, informationFlow *InformationFlow) Result {
	result := Result{
		SchemaVersion:   ProtocolVersion,
		Accepted:        true,
		ExitCode:        exitCode,
		StartedAt:       startedAt,
		FinishedAt:      finishedAt,
		RunID:           boundedText(report.RunID),
		Overall:         report.Overall,
		EvidencePath:    boundedText(report.EvidencePath),
		DevctlVersion:   boundedText(report.DevctlVersion),
		DevctlCommit:    boundedText(report.DevctlCommit),
		DevctlDirty:     report.DevctlDirty,
		InformationFlow: informationFlow,
	}
	for index, check := range report.Checks {
		if index >= maxChecks {
			break
		}
		result.Checks = append(result.Checks, CheckSummary{
			ID:         boundedText(check.ID),
			Status:     check.Status,
			Blocking:   check.Blocking,
			Summary:    boundedText(check.Summary),
			Reason:     boundedText(check.Reason),
			DurationMS: check.DurationMS,
		})
	}
	packet := handoff.FromReport(report)
	packet = sanitizeFailurePacket(packet)
	if len(packet.Failures) > 0 {
		result.FailurePacket = &packet
	}
	return result
}

var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
var awsAccessKeyPattern = regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)
var privateKeyMarkerPattern = regexp.MustCompile(`-----BEGIN (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----`)
var secretAssignmentPattern = regexp.MustCompile(`(?i)\b(api[_-]?key|secret|password|token)\s*[:=]\s*["']?[A-Za-z0-9/+=_-]{16,}`)

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

func RejectedResult(requestID, operation, code, message string) Result {
	return Result{
		SchemaVersion: ProtocolVersion,
		RequestID:     boundedText(requestID),
		Operation:     boundedText(operation),
		Accepted:      false,
		ExitCode:      2,
		Error:         &ProtocolError{Code: boundedText(code), Message: boundedText(message)},
	}
}

func RejectedAgentResult(code, message string, startedAt, finishedAt time.Time) Result {
	informationFlow := InformationFlow{}
	return Result{
		SchemaVersion:     AgentResultSchemaVersion,
		Operation:         "verify",
		VerificationClass: "local-full",
		Accepted:          false,
		ExitCode:          2,
		StartedAt:         startedAt,
		FinishedAt:        finishedAt,
		InformationFlow:   &informationFlow,
		Error:             &ProtocolError{Code: boundedText(code), Message: boundedText(message)},
	}
}

// EncodeResult produces the one bounded JSON object used by agent-facing
// callers. It never includes raw subprocess output.
func EncodeResult(result Result) ([]byte, error) {
	result = sanitizeResult(result)
	encoded, err := encodeWithResponseSize(result)
	if err != nil {
		return nil, err
	}
	if len(encoded) <= MaxAgentResultBytes {
		return encoded, nil
	}

	result.Truncated = true
	if result.Next == "" {
		result.Next = "request bounded failure details from local evidence"
	}
	for result.FailurePacket != nil && len(result.FailurePacket.Failures) > 0 {
		result.FailurePacket.Failures = result.FailurePacket.Failures[:len(result.FailurePacket.Failures)-1]
		result.FailuresReturned = len(result.FailurePacket.Failures)
		if result.FailuresReturned == 0 {
			result.FailurePacket = nil
		}
		encoded, err = encodeWithResponseSize(result)
		if err != nil {
			return nil, err
		}
		if len(encoded) <= MaxAgentResultBytes {
			return encoded, nil
		}
	}
	for index := range result.Checks {
		result.Checks[index].Summary = ""
		result.Checks[index].Reason = ""
	}
	encoded, err = encodeWithResponseSize(result)
	if err != nil {
		return nil, err
	}
	for len(encoded) > MaxAgentResultBytes && len(result.Checks) > 0 {
		result.Checks = result.Checks[:len(result.Checks)-1]
		result.ChecksReturned = len(result.Checks)
		encoded, err = encodeWithResponseSize(result)
		if err != nil {
			return nil, err
		}
	}
	if len(encoded) > MaxAgentResultBytes {
		return nil, fmt.Errorf("agent result cannot be represented within %d bytes", MaxAgentResultBytes)
	}
	return encoded, nil
}

func sanitizeResult(result Result) Result {
	if result.InformationFlow != nil {
		informationFlow := *result.InformationFlow
		result.InformationFlow = &informationFlow
	}
	result.Checks = append([]CheckSummary(nil), result.Checks...)
	result.SchemaVersion = boundedText(result.SchemaVersion)
	result.RequestID = boundedText(result.RequestID)
	result.Operation = boundedText(result.Operation)
	result.VerificationClass = boundedText(result.VerificationClass)
	result.ProjectID = boundedText(result.ProjectID)
	result.RunID = boundedText(result.RunID)
	result.EvidencePath = boundedText(result.EvidencePath)
	result.DevctlVersion = boundedText(result.DevctlVersion)
	result.DevctlCommit = boundedText(result.DevctlCommit)
	result.PolicyVersion = boundedText(result.PolicyVersion)
	result.RepositoryRevision = boundedText(result.RepositoryRevision)
	result.RepositoryFingerprint = boundedText(result.RepositoryFingerprint)
	result.Next = boundedText(result.Next)
	for index := range result.Checks {
		result.Checks[index].ID = boundedText(result.Checks[index].ID)
		result.Checks[index].CheckVersion = boundedText(result.Checks[index].CheckVersion)
		result.Checks[index].Summary = boundedText(result.Checks[index].Summary)
		result.Checks[index].Reason = boundedText(result.Checks[index].Reason)
	}
	if result.FailurePacket != nil {
		packet := sanitizeFailurePacket(cloneFailurePacket(*result.FailurePacket))
		result.FailurePacket = &packet
	}
	if result.Error != nil {
		errorValue := *result.Error
		errorValue.Code = boundedText(errorValue.Code)
		errorValue.Message = boundedText(errorValue.Message)
		result.Error = &errorValue
	}
	return result
}

func cloneFailurePacket(packet model.FailurePacket) model.FailurePacket {
	packet.Failures = append([]model.FailureItem(nil), packet.Failures...)
	for index := range packet.Failures {
		packet.Failures[index].EvidencePaths = append([]string(nil), packet.Failures[index].EvidencePaths...)
		packet.Failures[index].Findings = append([]model.Finding(nil), packet.Failures[index].Findings...)
	}
	return packet
}

func encodeWithResponseSize(result Result) ([]byte, error) {
	if result.InformationFlow == nil {
		result.InformationFlow = &InformationFlow{}
	}
	for attempts := 0; attempts < 8; attempts++ {
		data, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}
		size := int64(len(data) + 1)
		if result.InformationFlow.AgentResponseBytes == size {
			return append(data, '\n'), nil
		}
		result.InformationFlow.AgentResponseBytes = size
	}
	return nil, errors.New("agent response size did not stabilize")
}

func sanitizeFailurePacket(packet model.FailurePacket) model.FailurePacket {
	packet.SchemaVersion = boundedText(packet.SchemaVersion)
	packet.RunID = boundedText(packet.RunID)
	packet.Project = boundedText(packet.Project)
	packet.DevctlVersion = boundedText(packet.DevctlVersion)
	packet.DevctlCommit = boundedText(packet.DevctlCommit)
	packet.EvidencePath = boundedText(packet.EvidencePath)
	packet.NextAction = boundedText(packet.NextAction)
	if len(packet.Failures) > maxFailureItems {
		packet.Failures = packet.Failures[:maxFailureItems]
	}
	for index := range packet.Failures {
		failure := &packet.Failures[index]
		failure.CheckID = boundedText(failure.CheckID)
		failure.Summary = boundedText(failure.Summary)
		failure.Reason = boundedText(failure.Reason)
		if len(failure.EvidencePaths) > maxFailureEvidence {
			failure.EvidencePaths = failure.EvidencePaths[:maxFailureEvidence]
		}
		for pathIndex := range failure.EvidencePaths {
			failure.EvidencePaths[pathIndex] = boundedText(failure.EvidencePaths[pathIndex])
		}
		if len(failure.Findings) > maxFailureFindings {
			failure.Findings = failure.Findings[:maxFailureFindings]
		}
		for findingIndex := range failure.Findings {
			finding := &failure.Findings[findingIndex]
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
	}
	return packet
}
