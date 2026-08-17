package worker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode"

	"devctl/internal/handoff"
	"devctl/internal/model"
)

const ProtocolVersion = "1"

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
	SchemaVersion string               `json:"schema_version"`
	RequestID     string               `json:"request_id,omitempty"`
	Operation     string               `json:"operation,omitempty"`
	Accepted      bool                 `json:"accepted"`
	ExitCode      int                  `json:"exit_code"`
	StartedAt     time.Time            `json:"started_at"`
	FinishedAt    time.Time            `json:"finished_at"`
	ProjectID     string               `json:"project_id,omitempty"`
	RunID         string               `json:"run_id,omitempty"`
	Overall       model.Status         `json:"overall,omitempty"`
	EvidencePath  string               `json:"evidence_path,omitempty"`
	DevctlVersion string               `json:"devctl_version,omitempty"`
	DevctlCommit  string               `json:"devctl_commit,omitempty"`
	DevctlDirty   bool                 `json:"devctl_dirty,omitempty"`
	Checks        []CheckSummary       `json:"checks,omitempty"`
	FailurePacket *model.FailurePacket `json:"failure_packet,omitempty"`
	Error         *ProtocolError       `json:"error,omitempty"`
}

type CheckSummary struct {
	ID         string       `json:"check_id"`
	Status     model.Status `json:"status"`
	Blocking   bool         `json:"blocking,omitempty"`
	Summary    string       `json:"summary"`
	Reason     string       `json:"reason,omitempty"`
	DurationMS int64        `json:"duration_ms"`
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
	result := Result{
		SchemaVersion: ProtocolVersion,
		RequestID:     boundedText(request.RequestID),
		Operation:     boundedText(request.Operation),
		Accepted:      true,
		ExitCode:      exitCode,
		StartedAt:     startedAt,
		FinishedAt:    finishedAt,
		ProjectID:     boundedText(request.ProjectID),
		RunID:         boundedText(report.RunID),
		Overall:       report.Overall,
		EvidencePath:  boundedText(report.EvidencePath),
		DevctlVersion: boundedText(report.DevctlVersion),
		DevctlCommit:  boundedText(report.DevctlCommit),
		DevctlDirty:   report.DevctlDirty,
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
	packet := sanitizeFailurePacket(handoff.FromReport(report))
	if len(packet.Failures) > 0 {
		result.FailurePacket = &packet
	}
	return result
}

func boundedText(value string) string {
	runes := []rune(strings.TrimSpace(value))
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
