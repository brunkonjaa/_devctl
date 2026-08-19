package progressive

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"devctl/internal/evidence"
	"devctl/internal/model"
)

const (
	maxRawEvidenceBytes = 2 * 1024 * 1024
	maxRawChunkBytes    = 2 * 1024
)

type runEvidence struct {
	report        model.Report
	canonicalRoot string
}

func Failures(root, runID string, offset int64) (Result, error) {
	run, err := loadRun(root, runID)
	if err != nil {
		return Result{}, err
	}
	checks := failedChecks(run.report)
	if err := validateOffset(offset, len(checks)); err != nil {
		return Result{}, err
	}
	result := baseResult("failures", run.report)
	result.FailuresTotal = len(checks)
	result.ItemsOffset = offset
	end := len(checks)
	if maximum := int(offset) + maxFailurePage; end > maximum {
		end = maximum
	}
	for _, check := range checks[int(offset):end] {
		result.Failures = append(result.Failures, failureFromCheck(check, false, nil))
	}
	result.FailuresReturned = len(result.Failures)
	if end < len(checks) {
		result.Truncated = true
		result.NextItemsOffset = int64(end)
		result.Next = fmt.Sprintf("devctl failures %s --offset %d --json", runID, end)
	}
	return result, nil
}

func FailureDetail(root, runID, failureID string, offset int64) (Result, error) {
	run, err := loadRun(root, runID)
	if err != nil {
		return Result{}, err
	}
	check, err := findFailure(run.report, failureID)
	if err != nil {
		return Result{}, err
	}
	if err := validateOffset(offset, len(check.Findings)); err != nil {
		return Result{}, err
	}
	available, err := rawEvidenceAvailable(run, check.ID)
	if err != nil {
		return Result{}, err
	}
	result := baseResult("failure", run.report)
	result.FailuresTotal = 1
	result.FailuresReturned = 1
	result.ItemsOffset = offset
	end := len(check.Findings)
	if maximum := int(offset) + maxFindingPage; end > maximum {
		end = maximum
	}
	detail := failureFromCheck(check, true, check.Findings[int(offset):end])
	detail.EvidenceAvailable = available
	result.Failure = &detail
	if detail.EvidencePathsReturned < detail.EvidencePathsTotal {
		result.Truncated = true
		if available {
			result.Next = fmt.Sprintf("devctl evidence %s %s --json", runID, failureID)
		}
	}
	if end < len(check.Findings) {
		result.Truncated = true
		result.NextItemsOffset = int64(end)
		result.Next = fmt.Sprintf("devctl failure %s %s --offset %d --json", runID, failureID, end)
	}
	return result, nil
}

func SelectedEvidence(root, runID, failureID string, offset int64) (Result, error) {
	run, err := loadRun(root, runID)
	if err != nil {
		return Result{}, err
	}
	check, err := findFailure(run.report, failureID)
	if err != nil {
		return Result{}, err
	}
	fragment, more, err := readRawFragment(run, check, offset)
	if err != nil {
		return Result{}, err
	}
	result := baseResult("evidence", run.report)
	result.FailuresTotal = 1
	result.FailuresReturned = 1
	result.Evidence = &fragment
	result.Truncated = more || check.OutputTruncated
	if more {
		result.Next = fmt.Sprintf("devctl evidence %s %s --offset %d --json", runID, failureID, fragment.NextOffset)
	}
	return result, nil
}

func baseResult(operation string, report model.Report) Result {
	result := Result{
		SchemaVersion:         SchemaVersion,
		Operation:             operation,
		Accepted:              true,
		ExitCode:              0,
		RunID:                 report.RunID,
		Overall:               report.Overall,
		EvidencePath:          report.EvidencePath,
		DevctlVersion:         report.DevctlVersion,
		DevctlCommit:          report.DevctlCommit,
		DevctlDirty:           report.DevctlDirty,
		PolicyVersion:         report.PolicyVersion,
		RepositoryRevision:    report.RepositoryRevision,
		RepositoryDirty:       report.RepositoryDirty,
		RepositoryFingerprint: report.RepositoryFingerprint,
	}
	if report.Project != nil {
		result.ProjectID = report.Project.Identity
	}
	return result
}

func failedChecks(report model.Report) []model.CheckResult {
	checks := make([]model.CheckResult, 0)
	for _, check := range report.Checks {
		if check.Status == model.Pass || check.Status == model.Warn && !check.Blocking {
			continue
		}
		checks = append(checks, check)
	}
	return checks
}

func findFailure(report model.Report, failureID string) (model.CheckResult, error) {
	if !evidence.ValidIdentifier(failureID) {
		return model.CheckResult{}, NewError("invalid_failure_id", "failure ID is invalid")
	}
	for _, check := range failedChecks(report) {
		if check.ID == failureID {
			return check, nil
		}
	}
	return model.CheckResult{}, NewError("failure_not_found", "failure was not found in the requested run")
}

func failureFromCheck(check model.CheckResult, detail bool, findings []model.Finding) Failure {
	failure := Failure{
		FailureID:             check.ID,
		CheckID:               check.ID,
		CheckVersion:          check.CheckVersion,
		Status:                check.Status,
		Blocking:              check.Blocking,
		Summary:               check.Summary,
		SourceOutputTruncated: check.OutputTruncated,
	}
	if !detail {
		return failure
	}
	failure.Reason = check.Reason
	failure.FindingsTotal = len(check.Findings)
	failure.Findings = append([]model.Finding(nil), findings...)
	failure.FindingsReturned = len(failure.Findings)
	for _, item := range check.Evidence {
		if strings.TrimSpace(item.Path) != "" {
			failure.EvidencePaths = append(failure.EvidencePaths, item.Path)
		}
	}
	failure.EvidencePathsTotal = len(failure.EvidencePaths)
	if len(failure.EvidencePaths) > maxEvidencePaths {
		failure.EvidencePaths = failure.EvidencePaths[:maxEvidencePaths]
	}
	failure.EvidencePathsReturned = len(failure.EvidencePaths)
	return failure
}

func validateOffset(offset int64, total int) error {
	if offset < 0 || offset > int64(total) {
		return NewError("invalid_offset", "offset is outside the available result range")
	}
	return nil
}

func loadRun(root, runID string) (runEvidence, error) {
	loaded, err := evidence.ReadRun(root, runID)
	if err != nil {
		return runEvidence{}, NewError(evidence.ErrorCode(err), err.Error())
	}
	return runEvidence{report: loaded.Report, canonicalRoot: loaded.CanonicalRoot}, nil
}

func rawEvidenceAvailable(run runEvidence, checkID string) (bool, error) {
	_, _, err := rawEvidenceFile(run, checkID)
	if err == nil {
		return true, nil
	}
	if ErrorCode(err) == "evidence_unavailable" {
		return false, nil
	}
	return false, err
}

func readRawFragment(run runEvidence, check model.CheckResult, offset int64) (EvidenceFragment, bool, error) {
	path, size, err := rawEvidenceFile(run, check.ID)
	if err != nil {
		return EvidenceFragment{}, false, err
	}
	if size > maxRawEvidenceBytes {
		return EvidenceFragment{}, false, NewError("evidence_too_large", "raw evidence exceeds the retrieval limit")
	}
	if offset < 0 || offset > size {
		return EvidenceFragment{}, false, NewError("invalid_offset", "offset is outside the raw evidence range")
	}
	file, err := os.Open(path)
	if err != nil {
		return EvidenceFragment{}, false, &Error{Code: "retrieval_unavailable", Err: err}
	}
	defer file.Close()
	privateKeyOpen, continuation, linePrefix, err := evidenceStateAt(file, offset)
	if err != nil {
		return EvidenceFragment{}, false, &Error{Code: "retrieval_unavailable", Err: err}
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return EvidenceFragment{}, false, &Error{Code: "retrieval_unavailable", Err: err}
	}
	remaining := size - offset
	readSize := int64(maxRawChunkBytes)
	if remaining < readSize {
		readSize = remaining
	}
	raw := make([]byte, int(readSize))
	read, err := io.ReadFull(file, raw)
	if err != nil && !errorsIsEOF(err) {
		return EvidenceFragment{}, false, &Error{Code: "retrieval_unavailable", Err: err}
	}
	raw = raw[:read]
	nextOffset := offset + int64(read)
	more := nextOffset < size
	content := sanitizeRawLines(raw, privateKeyOpen, continuation, linePrefix, more)
	if !more {
		nextOffset = 0
	}
	fragment := EvidenceFragment{
		FailureID:             check.ID,
		CheckID:               check.ID,
		CheckVersion:          check.CheckVersion,
		RawBytesTotal:         size,
		RawOffset:             offset,
		RawBytesRead:          int64(read),
		NextOffset:            nextOffset,
		Content:               content,
		ContentBytesReturned:  int64(len(content)),
		SourceOutputTruncated: check.OutputTruncated,
	}
	return fragment, more, nil
}

func rawEvidenceFile(run runEvidence, checkID string) (string, int64, error) {
	rawRoot := filepath.Join(run.canonicalRoot, "raw")
	rawInfo, err := os.Lstat(rawRoot)
	if err != nil || rawInfo.Mode()&os.ModeSymlink != 0 || !rawInfo.IsDir() {
		if err != nil && !os.IsNotExist(err) {
			return "", 0, &Error{Code: "retrieval_unavailable", Err: err}
		}
		return "", 0, NewError("evidence_unavailable", "selected failure has no generated raw evidence")
	}
	canonicalRaw, err := filepath.EvalSymlinks(rawRoot)
	if err != nil || !contained(canonicalRaw, run.canonicalRoot) {
		return "", 0, NewError("evidence_boundary", "raw evidence directory escapes the requested run")
	}
	path := filepath.Join(canonicalRaw, safeName(checkID)+".log")
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", 0, NewError("evidence_unavailable", "selected failure has no generated raw evidence")
		}
		return "", 0, &Error{Code: "retrieval_unavailable", Err: err}
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", 0, NewError("evidence_boundary", "raw evidence is not a normal file")
	}
	canonicalPath, err := filepath.EvalSymlinks(path)
	if err != nil || !contained(canonicalPath, canonicalRaw) {
		return "", 0, NewError("evidence_boundary", "raw evidence escapes the requested run")
	}
	return canonicalPath, info.Size(), nil
}

func evidenceStateAt(file *os.File, offset int64) (privateKeyOpen, continuation bool, linePrefix string, err error) {
	if offset == 0 {
		return false, false, "", nil
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return false, false, "", err
	}
	prefix := make([]byte, int(offset))
	if _, err := io.ReadFull(file, prefix); err != nil {
		return false, false, "", err
	}
	continuation = len(prefix) > 0 && prefix[len(prefix)-1] != '\n'
	privateKeyOpen = privateKeyState(string(prefix))
	if continuation {
		start := bytes.LastIndexByte(prefix, '\n') + 1
		linePrefix = string(prefix[start:])
	}
	return privateKeyOpen, continuation, linePrefix, nil
}

func privateKeyState(value string) bool {
	begin := -1
	for _, marker := range privateKeyMarkers("BEGIN") {
		if index := strings.LastIndex(value, marker); index > begin {
			begin = index
		}
	}
	end := -1
	for _, marker := range privateKeyMarkers("END") {
		if index := strings.LastIndex(value, marker); index > end {
			end = index
		}
	}
	return begin >= 0 && begin > end
}

func sanitizeRawLines(raw []byte, privateKeyOpen, continuation bool, linePrefix string, more bool) string {
	lines := bytes.SplitAfter(raw, []byte{'\n'})
	var output strings.Builder
	for index, line := range lines {
		if len(line) == 0 {
			continue
		}
		lineText := string(line)
		markerText := lineText
		if continuation && index == 0 {
			markerText = linePrefix + lineText
		}
		begin := privateKeyMarkerPattern.MatchString(markerText)
		end := privateKeyEndMarkerPattern.MatchString(markerText)
		partialTail := more && index == len(lines)-1 && !bytes.HasSuffix(line, []byte{'\n'})
		if continuation && index == 0 {
			output.WriteString("[REDACTED_CONTINUED_LINE]")
			if bytes.HasSuffix(line, []byte{'\n'}) {
				output.WriteByte('\n')
			}
		} else if privateKeyOpen || begin {
			output.WriteString("[REDACTED_PRIVATE_KEY]")
			if bytes.HasSuffix(line, []byte{'\n'}) {
				output.WriteByte('\n')
			}
		} else if partialTail {
			output.WriteString("[REDACTED_PARTIAL_LINE]")
		} else {
			output.WriteString(sanitizeEvidenceText(lineText))
		}
		if begin {
			privateKeyOpen = true
		}
		if end {
			privateKeyOpen = false
		}
	}
	return output.String()
}

func privateKeyMarkers(action string) []string {
	markers := make([]string, 0, 5)
	for _, kind := range []string{"", "RSA", "EC", "OPENSSH", "DSA"} {
		parts := []string{"-----" + action}
		if kind != "" {
			parts = append(parts, kind)
		}
		parts = append(parts, "PRIVATE", "KEY-----")
		markers = append(markers, strings.Join(parts, " "))
	}
	return markers
}

func errorsIsEOF(err error) bool {
	return err == io.EOF || err == io.ErrUnexpectedEOF
}

func safeName(value string) string {
	var builder strings.Builder
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_' {
			builder.WriteRune(character)
		} else {
			builder.WriteByte('_')
		}
	}
	return builder.String()
}

func contained(path, root string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) && !filepath.IsAbs(relative)
}
