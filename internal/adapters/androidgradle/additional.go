package androidgradle

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"devctl/internal/model"
	"devctl/internal/runner"
	"devctl/internal/scheduler"
)

const dependencyCheckVersion = "android-dependency-pack-v2"

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
			if !runner.Available(runner.OsvScanner) {
				return model.CheckResult{ID: "dependency-vulnerability-scan", Status: model.NotTested, Summary: "Dependency vulnerability scan not tested", Reason: "osv-scanner is not installed"}
			}
			result, err := runner.Run(ctx, project.Path, runner.OsvScanner)
			toolVersion := "unknown"
			if versionResult, versionErr := runner.Run(ctx, project.Path, runner.OsvScannerVersion); versionErr == nil {
				toolVersion = strings.TrimSpace(versionResult.Output)
			}
			check := model.CheckResult{ID: "dependency-vulnerability-scan", RawOutput: result.Output, Evidence: []model.Evidence{{Type: "osv-json", Detail: "OSV-Scanner JSON output"}}}
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
			if err != nil {
				check.Status = model.Error
				check.Summary = "Dependency vulnerability scan did not complete"
				check.Reason = err.Error()
				return check
			}
			if len(findings) > 0 {
				check.Status = model.Warn
				check.Summary = fmt.Sprintf("%d known dependency vulnerabilities found", len(findings))
				check.Reason = "Findings are advisory until project policy assigns blocking thresholds"
				return check
			}
			check.Status = model.Pass
			check.Summary = "No known vulnerabilities found in supported manifests"
			check.Reason = "Database-backed scan only; this is not proof that all dependencies are safe"
			return check
		},
	}
}

func coverageCheck(project model.Project) scheduler.CheckSpec {
	return scheduler.CheckSpec{
		ID:        "android-coverage",
		Requires:  []string{"android-unit-tests"},
		Resources: []string{"gradle"},
		Timeout:   gradleTimeout,
		Run: func(ctx context.Context) model.CheckResult {
			return collectCoverage(ctx, project)
		},
	}
}

func collectCoverage(ctx context.Context, project model.Project) model.CheckResult {
	if !coverageConfigured(project.Path) {
		return model.CheckResult{ID: "android-coverage", Status: model.NotTested, Summary: "Coverage collection not tested", Reason: "JaCoCo configuration or a coverage report was not found"}
	}
	result := commandCheck(ctx, project, runner.GradleCoverage, "Coverage report generated")
	if result.Status != model.Pass {
		return result
	}
	reportPath, percentage, err := findCoverageReport(project.Path)
	if err != nil {
		return model.CheckResult{ID: "android-coverage", Status: model.NotTested, Summary: "Coverage collection not tested", Reason: err.Error(), RawOutput: result.RawOutput}
	}
	result.Evidence = append(result.Evidence, model.Evidence{Type: "coverage-report", Path: reportPath, Detail: fmt.Sprintf("line coverage %.1f%%", percentage)})
	result.Findings = []model.Finding{{FindingID: "COV-LINE", Severity: "info", Issue: fmt.Sprintf("line coverage %.1f%%", percentage), Action: "maintain or improve test coverage", EvidencePath: reportPath, Source: "jacoco", ToolVersion: "unknown", Project: project.Name}}
	applyCoverageThreshold(&result, percentage)
	return result
}

func applyCoverageThreshold(result *model.CheckResult, percentage float64) {
	switch {
	case percentage < 70:
		result.Status = model.Fail
		result.Blocking = true
		result.Summary = fmt.Sprintf("Coverage %.1f%% is below blocking threshold 70%%", percentage)
	case percentage < 80:
		result.Status = model.Warn
		result.Summary = fmt.Sprintf("Coverage %.1f%% is below preferred target 80%%", percentage)
	default:
		result.Status = model.Pass
		result.Summary = fmt.Sprintf("Coverage %.1f%% meets preferred target 80%%", percentage)
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

func findCoverageReport(root string) (string, float64, error) {
	var preferredPath, fallbackPath string
	var preferredMissed, preferredCovered, fallbackMissed, fallbackCovered int
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() || !strings.HasSuffix(strings.ToLower(path), ".xml") || !strings.Contains(strings.ToLower(path), "jacoco") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		var report struct {
			Counters []jacocoCounter `xml:"counter"`
		}
		if decodeErr := xml.Unmarshal(data, &report); decodeErr != nil {
			return nil
		}
		for _, counter := range report.Counters {
			if counter.Type == "LINE" {
				if strings.Contains(strings.ToLower(filepath.Base(path)), "jacocofocusedandroidtestreport") {
					preferredPath = path
					preferredMissed = counter.Missed
					preferredCovered = counter.Covered
				} else if fallbackPath == "" {
					fallbackPath = path
					fallbackMissed = counter.Missed
					fallbackCovered = counter.Covered
				}
				break
			}
		}
		return nil
	})
	if err != nil {
		return "", 0, err
	}
	reportPath := preferredPath
	totalMissed, totalCovered := preferredMissed, preferredCovered
	if reportPath == "" {
		reportPath = fallbackPath
		totalMissed, totalCovered = fallbackMissed, fallbackCovered
	}
	if reportPath == "" || totalMissed+totalCovered == 0 {
		return "", 0, fmt.Errorf("JaCoCo XML report was not found")
	}
	return reportPath, float64(totalCovered) / float64(totalMissed+totalCovered) * 100, nil
}
