package androidgradle

import (
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
