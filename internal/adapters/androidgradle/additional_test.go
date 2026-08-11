package androidgradle

import (
	"errors"
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

	result, err := findCoverageReport(root)
	if err != nil {
		t.Fatal(err)
	}
	if result.Path != report || result.Percentage != 70 || result.Source != "gradle-jvm-unit" {
		t.Fatalf("unexpected coverage result: %#v", result)
	}
}

func TestFindCoverageReportPrefersFocusedAndroidReport(t *testing.T) {
	root := t.TempDir()
	unitReport := filepath.Join(root, "app", "build", "reports", "jacoco", "jacocoTestReport", "jacocoTestReport.xml")
	focusedReport := filepath.Join(root, "app", "build", "reports", "jacoco", "jacocoFocusedAndroidTestReport", "jacocoFocusedAndroidTestReport.xml")
	for _, report := range []string{unitReport, focusedReport} {
		if err := os.MkdirAll(filepath.Dir(report), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(unitReport, []byte(`<report><counter type="LINE" missed="80" covered="20"/></report>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(focusedReport, []byte(`<report><counter type="LINE" missed="10" covered="90"/></report>`), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := findCoverageReport(root)
	if err != nil {
		t.Fatal(err)
	}
	if result.Path != focusedReport || result.Percentage != 90 || result.Source != "gradle-focused-android" {
		t.Fatalf("expected focused Android report, got %#v", result)
	}
}

func TestFindCoverageReportPrefersAGPAndroidReport(t *testing.T) {
	root := t.TempDir()
	agpReport := filepath.Join(root, "app", "build", "reports", "coverage", "androidTest", "debug", "connected", "report.xml")
	focusedReport := filepath.Join(root, "app", "build", "reports", "jacoco", "jacocoFocusedAndroidTestReport", "jacocoFocusedAndroidTestReport.xml")
	unitReport := filepath.Join(root, "app", "build", "reports", "jacoco", "jacocoTestReport", "jacocoTestReport.xml")
	for path, contents := range map[string]string{
		agpReport:     `<report><counter type="LINE" missed="20" covered="80"/></report>`,
		focusedReport: `<report><counter type="LINE" missed="10" covered="90"/></report>`,
		unitReport:    `<report><counter type="LINE" missed="5" covered="95"/></report>`,
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := findCoverageReport(root)
	if err != nil {
		t.Fatal(err)
	}
	if result.Path != agpReport || result.Percentage != 80 || result.Source != "agp-android-instrumented" {
		t.Fatalf("expected native AGP Android report, got %#v", result)
	}
	coverage := result.Percentage
	check := coverageResult(model.Project{Name: "HearthLink", Path: root}, result, "")
	if len(check.Evidence) != 1 || check.Evidence[0].Coverage == nil || *check.Evidence[0].Coverage != coverage || check.Evidence[0].Source != result.Source {
		t.Fatalf("expected coverage provenance in evidence, got %#v", check.Evidence)
	}
	if check.Findings[0].Source != result.Source {
		t.Fatalf("expected coverage provenance in finding, got %#v", check.Findings[0])
	}
	evidenceFirst := collectCoverage(nil, model.Project{Name: "HearthLink", Path: root})
	if evidenceFirst.Status != model.Pass || evidenceFirst.Evidence[0].Source != "agp-android-instrumented" {
		t.Fatalf("expected existing AGP evidence to avoid the Gradle fallback, got %#v", evidenceFirst)
	}
}

func TestReadCoverageReportExcludesGeneratedAndroidClasses(t *testing.T) {
	root := t.TempDir()
	reportPath := filepath.Join(root, "report.xml")
	report := `<report>
  <package name="com/brunk/hearthlink">
    <class name="com/brunk/hearthlink/MessageRepository"><counter type="LINE" missed="10" covered="90"/></class>
    <class name="com/brunk/hearthlink/HearthLinkDatabase_Impl"><counter type="LINE" missed="90" covered="10"/></class>
    <class name="com/brunk/hearthlink/HearthLinkDatabase_Impl$1"><counter type="LINE" missed="50" covered="50"/></class>
    <class name="com/brunk/hearthlink/R$drawable"><counter type="LINE" missed="100" covered="0"/></class>
    <class name="com/brunk/hearthlink/BuildConfig"><counter type="LINE" missed="100" covered="0"/></class>
    <class name="com/brunk/hearthlink/Manifest$permission"><counter type="LINE" missed="100" covered="0"/></class>
  </package>
  <counter type="LINE" missed="450" covered="150"/>
</report>`
	if err := os.WriteFile(reportPath, []byte(report), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := readCoverageReport(reportPath, "agp-android-instrumented")
	if err != nil {
		t.Fatal(err)
	}
	if result.Percentage != 90 {
		t.Fatalf("expected generated classes to be excluded, got %.1f%%", result.Percentage)
	}
}

func TestFindCoverageReportRejectsMalformedAuthoritativeReport(t *testing.T) {
	root := t.TempDir()
	report := filepath.Join(root, "app", "build", "reports", "coverage", "androidTest", "debug", "connected", "report.xml")
	if err := os.MkdirAll(filepath.Dir(report), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(report, []byte(`<report><counter`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := findCoverageReport(root); err == nil || errors.Is(err, errCoverageReportNotFound) {
		t.Fatalf("expected malformed authoritative report error, got %v", err)
	}
	result := collectCoverage(nil, model.Project{Name: "HearthLink", Path: root})
	if result.Status != model.Error {
		t.Fatalf("expected corrupt evidence to be an error, got %#v", result)
	}
}

func TestFindCoverageReportReportsMissingEvidence(t *testing.T) {
	if _, err := findCoverageReport(t.TempDir()); !errors.Is(err, errCoverageReportNotFound) {
		t.Fatalf("expected missing report sentinel, got %v", err)
	}
}
