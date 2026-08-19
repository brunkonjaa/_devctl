package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"devctl/internal/model"
	"devctl/internal/strictjson"
)

const MaxReportBytes = 64 * 1024 * 1024

var identifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type LoadedRun struct {
	Report        model.Report
	CanonicalRoot string
	ReportSHA256  string
}

type ReadError struct {
	Code string
	Err  error
}

func (err *ReadError) Error() string {
	if err == nil || err.Err == nil {
		return "evidence read failed"
	}
	return err.Err.Error()
}

func (err *ReadError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}

func ErrorCode(err error) string {
	var readError *ReadError
	if errors.As(err, &readError) && readError.Code != "" {
		return readError.Code
	}
	return "retrieval_unavailable"
}

func ValidIdentifier(value string) bool {
	return identifierPattern.MatchString(value)
}

func ReadRun(root, runID string) (LoadedRun, error) {
	if !ValidIdentifier(runID) {
		return LoadedRun{}, readError("invalid_run_id", "run ID is invalid")
	}
	absoluteProject, err := filepath.Abs(root)
	if err != nil {
		return LoadedRun{}, readError("invalid_project", "project path is unavailable")
	}
	projectInfo, err := os.Lstat(absoluteProject)
	if err != nil || projectInfo.Mode()&os.ModeSymlink != 0 || !projectInfo.IsDir() {
		return LoadedRun{}, readError("invalid_project", "project path is not a normal directory")
	}
	canonicalProject, err := filepath.EvalSymlinks(absoluteProject)
	if err != nil {
		return LoadedRun{}, readError("invalid_project", "project path is unavailable")
	}
	projectInfo, err = os.Stat(canonicalProject)
	if err != nil || !projectInfo.IsDir() {
		return LoadedRun{}, readError("invalid_project", "project path is not a directory")
	}
	devctlRoot := filepath.Join(canonicalProject, ".devctl")
	if err := requireEvidenceDirectory(devctlRoot); err != nil {
		return LoadedRun{}, err
	}
	evidenceRoot := filepath.Join(devctlRoot, "evidence")
	if err := requireEvidenceDirectory(evidenceRoot); err != nil {
		return LoadedRun{}, err
	}
	canonicalEvidence, err := filepath.EvalSymlinks(evidenceRoot)
	if err != nil {
		return LoadedRun{}, readError("run_not_found", "requested evidence run was not found")
	}
	if !contained(canonicalEvidence, canonicalProject) {
		return LoadedRun{}, readError("evidence_boundary", "evidence root escapes the selected project")
	}
	runPath := filepath.Join(evidenceRoot, runID)
	runInfo, err := os.Lstat(runPath)
	if err != nil {
		if os.IsNotExist(err) {
			return LoadedRun{}, readError("run_not_found", "requested evidence run was not found")
		}
		return LoadedRun{}, &ReadError{Code: "retrieval_unavailable", Err: err}
	}
	if runInfo.Mode()&os.ModeSymlink != 0 || !runInfo.IsDir() {
		return LoadedRun{}, readError("evidence_boundary", "evidence run is not a normal directory")
	}
	canonicalRun, err := filepath.EvalSymlinks(runPath)
	if err != nil || !contained(canonicalRun, canonicalEvidence) {
		return LoadedRun{}, readError("evidence_boundary", "evidence run escapes the evidence root")
	}
	reportPath := filepath.Join(canonicalRun, "report.json")
	reportInfo, err := os.Lstat(reportPath)
	if err != nil {
		if os.IsNotExist(err) {
			return LoadedRun{}, readError("run_not_found", "requested evidence report was not found")
		}
		return LoadedRun{}, &ReadError{Code: "retrieval_unavailable", Err: err}
	}
	if reportInfo.Mode()&os.ModeSymlink != 0 || !reportInfo.Mode().IsRegular() {
		return LoadedRun{}, readError("evidence_boundary", "evidence report is not a normal file")
	}
	canonicalReport, err := filepath.EvalSymlinks(reportPath)
	if err != nil || !contained(canonicalReport, canonicalRun) {
		return LoadedRun{}, readError("evidence_boundary", "evidence report escapes the requested run")
	}
	data, err := readReportFile(canonicalReport)
	if err != nil {
		return LoadedRun{}, err
	}
	var report model.Report
	if err := strictjson.Decode(data, &report); err != nil {
		return LoadedRun{}, readError("evidence_invalid", "evidence report is malformed")
	}
	expectedEvidencePath := filepath.ToSlash(filepath.Join(".devctl", "evidence", runID))
	if report.SchemaVersion != "1" || report.RunID != runID || filepath.ToSlash(report.EvidencePath) != expectedEvidencePath {
		return LoadedRun{}, readError("evidence_invalid", "evidence report identity does not match the requested run")
	}
	seenChecks := make(map[string]struct{}, len(report.Checks))
	for _, check := range report.Checks {
		if !ValidIdentifier(check.ID) {
			return LoadedRun{}, readError("evidence_invalid", "evidence report contains an invalid check ID")
		}
		if _, exists := seenChecks[check.ID]; exists {
			return LoadedRun{}, readError("evidence_invalid", "evidence report contains duplicate check IDs")
		}
		seenChecks[check.ID] = struct{}{}
	}
	digest := sha256.Sum256(data)
	return LoadedRun{Report: report, CanonicalRoot: canonicalRun, ReportSHA256: hex.EncodeToString(digest[:])}, nil
}

func requireEvidenceDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return readError("run_not_found", "requested evidence run was not found")
		}
		return &ReadError{Code: "retrieval_unavailable", Err: err}
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return readError("evidence_boundary", "evidence path is not a normal directory")
	}
	return nil
}

func readReportFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, &ReadError{Code: "retrieval_unavailable", Err: err}
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxReportBytes+1))
	if err != nil {
		return nil, &ReadError{Code: "retrieval_unavailable", Err: err}
	}
	if int64(len(data)) > MaxReportBytes {
		return nil, readError("evidence_too_large", fmt.Sprintf("evidence report exceeds the %d-byte retrieval limit", MaxReportBytes))
	}
	return data, nil
}

func readError(code, message string) error {
	return &ReadError{Code: code, Err: errors.New(message)}
}
