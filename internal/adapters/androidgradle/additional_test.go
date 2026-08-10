package androidgradle

import (
	"os"
	"path/filepath"
	"testing"

	"devctl/internal/model"
)

func TestParseOSVFindingsNormalizesPackageEvidence(t *testing.T) {
	findings, err := parseOSVFindings(`{
      "results": [{
        "source": {"path": "gradle.lockfile"},
        "packages": [{
          "package": {"name": "example-library", "version": "1.2.3", "ecosystem": "Maven"},
          "vulnerabilities": [{"id": "OSV-123", "summary": "example issue", "database_specific": {"severity": "MEDIUM"}}]
        }]
      }]
    }`)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected one finding, got %#v", findings)
	}
	if findings[0].Component != "example-library" || findings[0].Version != "1.2.3" || findings[0].Severity != "MEDIUM" {
		t.Fatalf("unexpected normalized finding: %#v", findings[0])
	}
}

func TestCoverageCheckReportsMissingConfigurationAsNotTested(t *testing.T) {
	project := model.Project{Path: t.TempDir()}
	result := collectCoverage(nil, project)
	if result.Status != model.NotTested || result.Reason == "" {
		t.Fatalf("expected coverage evidence gap, got %#v", result)
	}
}

func TestFindCoverageReportReadsJaCoCoLineCounter(t *testing.T) {
	root := t.TempDir()
	report := filepath.Join(root, "app", "build", "reports", "jacoco", "jacocoTestReport", "jacocoTestReport.xml")
	if err := os.MkdirAll(filepath.Dir(report), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(report, []byte(`<report name="HearthLink"><counter type="LINE" missed="30" covered="70"/></report>`), 0o644); err != nil {
		t.Fatal(err)
	}

	path, percentage, err := findCoverageReport(root)
	if err != nil {
		t.Fatal(err)
	}
	if path != report || percentage != 70 {
		t.Fatalf("unexpected coverage result: path=%q percentage=%v", path, percentage)
	}
}

func TestCoverageCheckAppliesThresholds(t *testing.T) {
	for _, test := range []struct {
		name     string
		covered  string
		status   model.Status
		blocking bool
	}{
		{name: "below minimum", covered: `missed="31" covered="69"`, status: model.Fail, blocking: true},
		{name: "below preferred", covered: `missed="25" covered="75"`, status: model.Warn},
		{name: "preferred", covered: `missed="15" covered="85"`, status: model.Pass},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := model.CheckResult{Status: model.Pass}
			applyCoverageThreshold(&result, map[string]float64{"below minimum": 69, "below preferred": 75, "preferred": 85}[test.name])
			if result.Status != test.status || result.Blocking != test.blocking {
				t.Fatalf("unexpected result: %#v", result)
			}
		})
	}
}
