package androidgradle

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"devctl/internal/model"
	"devctl/internal/runner"
	"devctl/internal/scheduler"
)

const dependencyCheckVersion = "android-dependency-pack-v3"

func AdditionalChecks(project model.Project) []scheduler.CheckSpec {
	return []scheduler.CheckSpec{
		dependencyVulnerabilityCheck(project),
		coverageCheck(project),
	}
}

func dependencyVulnerabilityCheck(project model.Project) scheduler.CheckSpec {
	return scheduler.CheckSpec{
		ID: "dependency-vulnerability-scan", Version: dependencyCheckVersion,
		Run: func(ctx context.Context) model.CheckResult {
			if !hasSupportedDependencyManifest(project.Path) {
				return model.CheckResult{ID: "dependency-vulnerability-scan", Status: model.NotTested, Summary: "Dependency vulnerability scan not tested", Reason: "No supported dependency lockfile or verification metadata was found"}
			}
			if runner.Available(runner.OsvScanner) {
				return runOSVScanner(ctx, project)
			}
			return runGradleOSVQuery(ctx, project)
		},
	}
}

func runOSVScanner(ctx context.Context, project model.Project) model.CheckResult {
	result, err := runner.Run(ctx, project.Path, runner.OsvScanner)
	toolVersion := "unknown"
	if versionResult, versionErr := runner.Run(ctx, project.Path, runner.OsvScannerVersion); versionErr == nil {
		toolVersion = strings.TrimSpace(versionResult.Output)
	}
	check := model.CheckResult{ID: "dependency-vulnerability-scan", RawOutput: result.Output, OutputTruncated: result.OutputTruncated, Executable: result.Executable, Arguments: result.Arguments, EnvironmentProfile: result.EnvironmentProfile, EnvironmentKeys: result.EnvironmentKeys, Executions: executionList(result), Evidence: []model.Evidence{{Type: "osv-json", Detail: "OSV-Scanner JSON output"}}}
	findings, parseErr := parseOSVFindings(result.Output)
	if parseErr != nil {
		check.Status = model.Error
		check.Summary = "Dependency vulnerability results could not be parsed"
		check.Reason = parseErr.Error()
		return check
	}
	check.Findings = findings
	for index := range check.Findings {
		check.Findings[index].ToolVersion = toolVersion
		check.Findings[index].Project = project.Name
	}
	scanReportedFindings := result.Started && result.TerminationReason == "completed" && result.ExitCode == 1 && len(findings) > 0
	if err != nil && !scanReportedFindings {
		check.Status = model.Error
		check.Summary = "Dependency vulnerability scan did not complete"
		check.Reason = err.Error()
		return check
	}
	return dependencyResult(check, len(findings), "OSV-Scanner")
}

type gradleDependency struct {
	Name    string
	Version string
}

var gradleDependencyLine = regexp.MustCompile(`^\s*(?:\|\s{4})*(?:\+---|\\---)\s+([^:\s]+):([^:\s]+)(?::\s*([^\s(]+))(?:\s*->\s*([^\s(]+))?`)

func runGradleOSVQuery(ctx context.Context, project model.Project) model.CheckResult {
	result, err := runner.RunGradleDependencyGraph(ctx, project.Path)
	check := model.CheckResult{ID: "dependency-vulnerability-scan", RawOutput: result.Output, OutputTruncated: result.OutputTruncated, Executable: result.Executable, Arguments: result.Arguments, EnvironmentProfile: result.EnvironmentProfile, EnvironmentKeys: result.EnvironmentKeys, Executions: executionList(result), Evidence: []model.Evidence{{Type: "gradle-dependency-graph", Detail: "Resolved Maven release/debug runtime dependency graph"}}}
	if err != nil {
		check.Status = model.NotTested
		check.Summary = "Dependency vulnerability scan not tested"
		check.Reason = fmt.Sprintf("Gradle dependency graph could not be resolved: %v", err)
		return check
	}
	dependencies := parseGradleDependencies(result.Output)
	if len(dependencies) == 0 {
		check.Status = model.NotTested
		check.Summary = "Dependency vulnerability scan not tested"
		check.Reason = "Gradle produced no parseable Maven runtime dependencies"
		return check
	}
	queries := make([]osvQuery, 0, len(dependencies))
	for _, dependency := range dependencies {
		queries = append(queries, osvQuery{Package: osvPackage{Name: dependency.Name, Ecosystem: "Maven"}, Version: dependency.Version})
	}
	response, raw, queryErr := queryOSV(ctx, queries)
	if queryErr != nil {
		check.Status = model.NotTested
		check.Summary = "Dependency vulnerability scan not tested"
		check.Reason = fmt.Sprintf("OSV query service unavailable: %v", queryErr)
		return check
	}
	check.RawOutput = raw
	check.Evidence = append(check.Evidence, model.Evidence{Type: "osv-querybatch", Detail: fmt.Sprintf("OSV API querybatch for %d resolved Maven components", len(queries)), Source: "https://api.osv.dev/v1/querybatch"})
	check.Findings = normalizeOSVQueryFindings(project, dependencies, response)
	return dependencyResult(check, len(check.Findings), "OSV API")
}

func dependencyResult(check model.CheckResult, findingCount int, source string) model.CheckResult {
	if findingCount > 0 {
		check.Status = model.Warn
		check.Summary = fmt.Sprintf("%d known dependency vulnerabilities found", findingCount)
		check.Reason = fmt.Sprintf("Findings came from %s and remain advisory until project policy assigns blocking thresholds", source)
		return check
	}
	check.Status = model.Pass
	check.Summary = fmt.Sprintf("No known vulnerabilities found in %s dependency evidence", source)
	check.Reason = "Database-backed scan only; this is not proof that all dependencies are safe"
	return check
}

func parseGradleDependencies(output string) []gradleDependency {
	seen := make(map[string]bool)
	dependencies := make([]gradleDependency, 0)
	for _, line := range strings.Split(output, "\n") {
		match := gradleDependencyLine.FindStringSubmatch(line)
		if len(match) != 5 {
			continue
		}
		version := match[3]
		if match[4] != "" {
			version = match[4]
		}
		if version == "" || strings.Contains(version, "+") || strings.Contains(version, "(") {
			continue
		}
		dependency := gradleDependency{Name: match[1] + ":" + match[2], Version: version}
		key := dependency.Name + "@" + dependency.Version
		if !seen[key] {
			seen[key] = true
			dependencies = append(dependencies, dependency)
		}
	}
	return dependencies
}

type osvPackage struct {
	Name      string `json:"name"`
	Ecosystem string `json:"ecosystem"`
}

type osvQuery struct {
	Package osvPackage `json:"package"`
	Version string     `json:"version"`
}

type osvBatchRequest struct {
	Queries []osvQuery `json:"queries"`
}

type osvBatchResponse struct {
	Results []struct {
		Vulnerabilities []struct {
			ID               string `json:"id"`
			Summary          string `json:"summary"`
			DatabaseSpecific struct {
				Severity string `json:"severity"`
			} `json:"database_specific"`
		} `json:"vulns"`
		NextPageToken string `json:"next_page_token"`
	} `json:"results"`
}

var osvQueryClient = func(ctx context.Context, payload []byte) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.osv.dev/v1/querybatch", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 55 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 4*1024*1024))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("OSV API returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func queryOSV(ctx context.Context, queries []osvQuery) (osvBatchResponse, string, error) {
	payload, err := json.Marshal(osvBatchRequest{Queries: queries})
	if err != nil {
		return osvBatchResponse{}, "", err
	}
	raw, err := osvQueryClient(ctx, payload)
	if err != nil {
		return osvBatchResponse{}, "", err
	}
	var response osvBatchResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return osvBatchResponse{}, string(raw), fmt.Errorf("parse OSV querybatch response: %w", err)
	}
	if len(response.Results) != len(queries) {
		return osvBatchResponse{}, string(raw), fmt.Errorf("OSV querybatch returned %d results for %d queries", len(response.Results), len(queries))
	}
	for _, result := range response.Results {
		if result.NextPageToken != "" {
			return osvBatchResponse{}, string(raw), errors.New("OSV querybatch returned a pagination token")
		}
	}
	return response, string(raw), nil
}

func normalizeOSVQueryFindings(project model.Project, dependencies []gradleDependency, response osvBatchResponse) []model.Finding {
	findings := make([]model.Finding, 0)
	for index, result := range response.Results {
		for _, vulnerability := range result.Vulnerabilities {
			severity := vulnerability.DatabaseSpecific.Severity
			if severity == "" {
				severity = "unknown"
			}
			issue := vulnerability.ID
			if vulnerability.Summary != "" {
				issue += ": " + vulnerability.Summary
			}
			findings = append(findings, model.Finding{FindingID: vulnerability.ID, Severity: severity, Component: dependencies[index].Name, Version: dependencies[index].Version, Issue: issue, Action: "review the OSV advisory and upgrade where a fixed version exists", Path: "gradle runtime dependency graph", EvidencePath: "gradle/verification-metadata.xml", Source: "osv-api", ToolVersion: "v1/querybatch", Project: project.Name})
		}
	}
	return findings
}

func coverageCheck(project model.Project) scheduler.CheckSpec {
	return scheduler.CheckSpec{
		ID:          "android-coverage",
		Requires:    []string{"android-unit-tests"},
		Resources:   []string{"gradle"},
		DeferFinish: true,
		Timeout:     gradleTimeout,
		Run: func(ctx context.Context) model.CheckResult {
			return collectCoverage(ctx, project)
		},
	}
}

func collectCoverage(ctx context.Context, project model.Project) model.CheckResult {
	report, err := findCoverageReport(project.Path)
	if err == nil {
		return coverageResult(project, report, "")
	}
	if !errors.Is(err, errCoverageReportNotFound) {
		return model.CheckResult{ID: "android-coverage", Status: model.Error, Summary: "Coverage evidence could not be read", Reason: err.Error()}
	}
	if !coverageConfigured(project.Path) {
		return model.CheckResult{ID: "android-coverage", Status: model.NotTested, Summary: "Coverage collection not tested", Reason: "JaCoCo configuration or a coverage report was not found"}
	}

	result := commandCheck(ctx, project, runner.GradleCoverage, "Coverage report generated")
	if result.Status != model.Pass {
		result.ID = "android-coverage"
		return result
	}
	report, err = findCoverageReport(project.Path)
	if err != nil {
		if errors.Is(err, errCoverageReportNotFound) {
			return model.CheckResult{ID: "android-coverage", Status: model.NotTested, Summary: "Coverage collection not tested", Reason: err.Error(), RawOutput: result.RawOutput}
		}
		return model.CheckResult{ID: "android-coverage", Status: model.Error, Summary: "Coverage evidence could not be read", Reason: err.Error(), RawOutput: result.RawOutput}
	}
	return coverageResult(project, report, result.RawOutput)
}

func coverageResult(project model.Project, report coverageReport, rawOutput string) model.CheckResult {
	percentage := report.Percentage
	return model.CheckResult{
		ID:        "android-coverage",
		Status:    model.Pass,
		Summary:   fmt.Sprintf("Coverage evidence collected: %.1f%% line coverage", percentage),
		RawOutput: rawOutput,
		Evidence: []model.Evidence{{
			Type:     "coverage-report",
			Path:     report.Path,
			Detail:   fmt.Sprintf("line coverage %.1f%%", percentage),
			Source:   report.Source,
			Metric:   "line",
			Coverage: &percentage,
		}},
		Findings: []model.Finding{{FindingID: "COV-LINE", Severity: "info", Issue: fmt.Sprintf("line coverage %.1f%%", percentage), Action: "maintain or improve test coverage", EvidencePath: report.Path, Source: report.Source, ToolVersion: "unknown", Project: project.Name}},
	}
}

func hasSupportedDependencyManifest(root string) bool {
	for _, path := range []string{"gradle.lockfile", "buildscript-gradle.lockfile", filepath.Join("gradle", "verification-metadata.xml"), "pom.xml"} {
		if _, err := os.Stat(filepath.Join(root, path)); err == nil {
			return true
		}
	}
	return false
}

func coverageConfigured(root string) bool {
	buildFile, err := os.ReadFile(filepath.Join(root, "app", "build.gradle.kts"))
	if err == nil && strings.Contains(strings.ToLower(string(buildFile)), "jacoco") {
		return true
	}
	_, err = os.Stat(filepath.Join(root, "app", "build", "reports", "jacoco"))
	if err == nil {
		return true
	}
	_, err = os.Stat(filepath.Join(root, "app", "build", "reports", "coverage", "androidTest"))
	return err == nil
}

type osvReport struct {
	Results []struct {
		Source struct {
			Path string `json:"path"`
		} `json:"source"`
		Packages []struct {
			Package struct {
				Name      string `json:"name"`
				Version   string `json:"version"`
				Ecosystem string `json:"ecosystem"`
			} `json:"package"`
			Vulnerabilities []struct {
				ID               string `json:"id"`
				Summary          string `json:"summary"`
				DatabaseSpecific struct {
					Severity string `json:"severity"`
				} `json:"database_specific"`
			} `json:"vulnerabilities"`
		} `json:"packages"`
	} `json:"results"`
}

func parseOSVFindings(output string) ([]model.Finding, error) {
	var report osvReport
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		return nil, err
	}
	findings := make([]model.Finding, 0)
	for _, result := range report.Results {
		for _, packageResult := range result.Packages {
			for _, vulnerability := range packageResult.Vulnerabilities {
				severity := vulnerability.DatabaseSpecific.Severity
				if severity == "" {
					severity = "unknown"
				}
				issue := vulnerability.ID
				if vulnerability.Summary != "" {
					issue += ": " + vulnerability.Summary
				}
				findings = append(findings, model.Finding{FindingID: vulnerability.ID, Severity: severity, Component: packageResult.Package.Name, Version: packageResult.Package.Version, Issue: issue, Action: "review the OSV advisory and upgrade where a fixed version exists", Path: result.Source.Path, EvidencePath: result.Source.Path, Source: "osv-scanner", ToolVersion: "unknown"})
			}
		}
	}
	return findings, nil
}

type jacocoCounter struct {
	Type    string `xml:"type,attr"`
	Missed  int    `xml:"missed,attr"`
	Covered int    `xml:"covered,attr"`
}

type coverageReport struct {
	Path       string
	Source     string
	Percentage float64
}

type coverageCandidate struct {
	Path   string
	Source string
}

type jacocoClass struct {
	Name     string          `xml:"name,attr"`
	Counters []jacocoCounter `xml:"counter"`
}

type jacocoPackage struct {
	Classes []jacocoClass `xml:"class"`
}

type jacocoReportXML struct {
	XMLName  xml.Name        `xml:"report"`
	Packages []jacocoPackage `xml:"package"`
	Counters []jacocoCounter `xml:"counter"`
}

var errCoverageReportNotFound = errors.New("JaCoCo XML report was not found")

func findCoverageReport(root string) (coverageReport, error) {
	groups := []struct {
		Pattern string
		Source  string
	}{
		{Pattern: filepath.Join(root, "app", "build", "reports", "coverage", "androidTest", "*", "connected", "report.xml"), Source: "agp-android-instrumented"},
		{Pattern: filepath.Join(root, "app", "build", "reports", "jacoco", "jacocoFocusedAndroidTestReport", "jacocoFocusedAndroidTestReport.xml"), Source: "gradle-focused-android"},
		{Pattern: filepath.Join(root, "app", "build", "reports", "jacoco", "jacocoTestReport", "jacocoTestReport.xml"), Source: "gradle-jvm-unit"},
	}

	for _, group := range groups {
		matches, err := filepath.Glob(group.Pattern)
		if err != nil {
			return coverageReport{}, err
		}
		for _, path := range matches {
			if err := validateCoveragePath(path, root); err != nil {
				return coverageReport{}, err
			}
			return readCoverageReport(path, group.Source)
		}
	}

	var legacyCandidates []coverageCandidate
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() || !strings.HasSuffix(strings.ToLower(path), ".xml") || !strings.Contains(strings.ToLower(path), "jacoco") {
			return nil
		}
		legacyCandidates = append(legacyCandidates, coverageCandidate{Path: path, Source: "gradle-jacoco-legacy"})
		return nil
	})
	if err != nil {
		return coverageReport{}, err
	}
	for _, candidate := range legacyCandidates {
		if err := validateCoveragePath(candidate.Path, root); err != nil {
			return coverageReport{}, err
		}
		report, readErr := readCoverageReport(candidate.Path, candidate.Source)
		if readErr == nil {
			return report, nil
		}
	}
	return coverageReport{}, errCoverageReportNotFound
}

func validateCoveragePath(path, root string) error {
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("resolve coverage project boundary: %w", err)
	}
	canonicalPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("resolve coverage report %s: %w", path, err)
	}
	if !containedPath(canonicalPath, canonicalRoot) {
		return fmt.Errorf("coverage report escapes project boundary: %s", path)
	}
	info, err := os.Stat(canonicalPath)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("coverage report is not a regular file: %s", path)
	}
	return nil
}

func containedPath(path, root string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) && !filepath.IsAbs(relative)
}

func readCoverageReport(path, source string) (coverageReport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return coverageReport{}, fmt.Errorf("read coverage report %s: %w", path, err)
	}
	var report jacocoReportXML
	if err := xml.Unmarshal(data, &report); err != nil {
		return coverageReport{}, fmt.Errorf("parse coverage report %s: %w", path, err)
	}

	missed, covered, classLines := focusedClassLineTotals(report.Packages)
	if !classLines {
		missed, covered = rootLineTotals(report.Counters)
	}
	if missed+covered == 0 {
		return coverageReport{}, fmt.Errorf("coverage report %s has no countable LINE evidence", path)
	}
	return coverageReport{Path: path, Source: source, Percentage: float64(covered) / float64(missed+covered) * 100}, nil
}

func focusedClassLineTotals(packages []jacocoPackage) (missed, covered int, found bool) {
	for _, pkg := range packages {
		for _, class := range pkg.Classes {
			for _, counter := range class.Counters {
				if counter.Type != "LINE" {
					continue
				}
				found = true
				if generatedAndroidClass(class.Name) {
					continue
				}
				missed += counter.Missed
				covered += counter.Covered
			}
		}
	}
	return missed, covered, found
}

func rootLineTotals(counters []jacocoCounter) (missed, covered int) {
	for _, counter := range counters {
		if counter.Type == "LINE" {
			return counter.Missed, counter.Covered
		}
	}
	return 0, 0
}

func generatedAndroidClass(name string) bool {
	if slash := strings.LastIndex(name, "/"); slash >= 0 {
		name = name[slash+1:]
	}
	outerName := strings.SplitN(name, "$", 2)[0]
	return outerName == "R" || outerName == "BuildConfig" || outerName == "Manifest" || strings.Contains(outerName, "_Impl")
}
