package knowledgevault

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSearchRanksProjectAndGlobalVerifiedLessonsDeterministically(t *testing.T) {
	projectRoot := t.TempDir()
	globalRoot := t.TempDir()
	project := writeSearchLesson(t, projectRoot, ScopeProject, "LESSON-PROJECT", StatusVerified, "Kotlin JaCoCo coverage checks", "Serialize the Gradle coverage task.", "")
	_ = writeSearchLesson(t, globalRoot, ScopeGlobal, "LESSON-GLOBAL", StatusVerified, "Kotlin JaCoCo coverage rule", "Keep coverage evidence tied to the authored source.", project.ID)
	_ = writeSearchLesson(t, projectRoot, ScopeProject, "LESSON-REJECTED", StatusRejected, "Kotlin JaCoCo coverage", "Rejected explanation", "")

	query := SearchQuery{Text: "jacoco kotlin", CheckID: "android-coverage", Technology: "Kotlin", Version: "1.9", Limit: 10}
	first, err := Search(projectRoot, globalRoot, query)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Search(projectRoot, globalRoot, query)
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, _ := json.Marshal(first)
	secondJSON, _ := json.Marshal(second)
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("identical search inputs produced different ordering:\n%s\n%s", firstJSON, secondJSON)
	}
	if len(first.Results) != 2 || first.Total != 2 || first.Truncated || first.Results[0].Status != StatusVerified || first.Results[1].Status != StatusVerified {
		t.Fatalf("unexpected trusted search results: %+v", first)
	}
	if first.Results[0].Scope != ScopeProject || first.Results[1].Scope != ScopeGlobal {
		t.Fatalf("expected deterministic project-before-global tie ordering: %+v", first)
	}
	if len(first.Results[0].SourceFixIDs) != 1 || first.Results[0].SourceFixIDs[0] != "FIX-SEARCH" {
		t.Fatalf("Fix Record provenance was not returned: %+v", first.Results[0])
	}

	history, err := Search(projectRoot, globalRoot, SearchQuery{Text: "jacoco", IncludeHistory: true, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Results) != 3 {
		t.Fatalf("explicit history search did not preserve lifecycle records: %+v", history)
	}
	seenRejected := false
	for _, result := range history.Results {
		if result.Status == StatusRejected {
			seenRejected = true
		}
	}
	if !seenRejected {
		t.Fatalf("history search omitted rejected lifecycle status: %+v", history)
	}
}

func TestSearchBoundsOutputAndSupportsDisplayIdentifiers(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < MaxSearchResults; i++ {
		writeSearchLesson(t, root, ScopeProject, "LESSON-BOUNDED-"+string(rune('A'+i)), StatusVerified, "bounded search lesson", strings.Repeat("coverage evidence ", 90)+"coverage evidence", "")
	}
	lesson := writeSearchLesson(t, root, ScopeProject, "LESSON-DISPLAY", StatusVerified, "display identifier lesson", "coverage evidence", "")
	results, err := Search(root, root, SearchQuery{Text: "coverage", Limit: MaxSearchResults})
	if err != nil {
		t.Fatal(err)
	}
	data, err := MarshalSearchJSON(results)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > MaxSearchBytes {
		t.Fatalf("search result exceeded bound: %d", len(data))
	}
	if results.Total != MaxSearchResults+1 || !results.Truncated || results.Returned >= results.Total {
		t.Fatalf("search truncation was not reported: %+v", results)
	}
	var decoded SearchResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Total != results.Total || decoded.Returned != len(decoded.Results) || !decoded.Truncated {
		t.Fatalf("serialized truncation metadata was not truthful: %+v", decoded)
	}
	shown, err := ReadIdentifier(root, ScopeProject, lesson.DisplayID)
	if err != nil || shown.ID != lesson.ID {
		t.Fatalf("display identifier was not readable: %v %+v", err, shown)
	}
}

func TestSearchRequiresTextMatchWhenFiltersAreProvided(t *testing.T) {
	root := t.TempDir()
	writeSearchLesson(t, root, ScopeProject, "LESSON-FILTER-AND", StatusVerified, "Kotlin coverage lesson", "The Kotlin coverage check is recorded.", "")
	result, err := Search(root, "", SearchQuery{Text: "completelyunrelated", Technology: "Kotlin", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 0 || len(result.Results) != 0 {
		t.Fatalf("filter match incorrectly bypassed the text query: %+v", result)
	}
}

func TestSearchUsesExactMetadataFilters(t *testing.T) {
	root := t.TempDir()
	writeSearchLesson(t, root, ScopeProject, "LESSON-EXACT-FILTER", StatusVerified, "Kotlin coverage lesson", "Kotlin coverage", "")
	for _, query := range []SearchQuery{
		{Technology: "Kot", Limit: 10},
		{Technology: "KotlinExtended", Limit: 10},
		{CheckID: "android", Limit: 10},
		{Adapter: "android", Limit: 10},
	} {
		result, err := Search(root, "", query)
		if err != nil {
			t.Fatal(err)
		}
		if result.Total != 0 || len(result.Results) != 0 {
			t.Fatalf("partial metadata filter unexpectedly matched: query=%+v result=%+v", query, result)
		}
	}
	result, err := Search(root, "", SearchQuery{Technology: "Kotlin", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || len(result.Results) != 1 {
		t.Fatalf("exact metadata filter did not match: %+v", result)
	}
}

func writeSearchLesson(t *testing.T, root, scope, displayID, status, title, statement, sourceLessonID string) Lesson {
	t.Helper()
	id, err := newID()
	if err != nil {
		t.Fatal(err)
	}
	lesson := Lesson{
		SchemaVersion: SchemaVersion, ID: id, DisplayID: displayID, Scope: scope, Revision: 1, Status: status,
		Title: title, Statement: statement, Problem: "coverage check failed", RootCause: "tool output was not bound", Correction: "bind the coverage result to the exact check",
		Technologies: []string{"Kotlin", "Gradle"}, RelevantVersions: map[string]string{"kotlin": "1.9", "gradle": "8.5"},
		Platform: "Windows", VerificationScope: "one deterministic verification run", Applicability: "Android coverage checks",
		Limitations: []string{"This is a candidate for retrieval, not proof of a current fix."}, Tags: []string{"jacoco", "coverage"},
		CheckIDs: []string{"android-coverage"}, FailureIDs: []string{"coverage-missing"}, Adapters: []string{"androidgradle"},
		NormalizedErrors: []string{"jacoco coverage below threshold"}, AffectedPaths: []string{"app/build.gradle.kts"}, Symptoms: []string{"coverage check failed"},
		SourceFixIDs: []string{"FIX-SEARCH"}, SourceProjectIDs: []string{"search-project"},
		CreatedAt: time.Now().UTC(),
	}
	lesson.SourceLessonID = sourceLessonID
	if status != StatusVerified {
		lesson.SourceFixIDs = nil
		lesson.SourceProjectIDs = nil
	}
	created, err := appendRevision(root, scope, lesson)
	if err != nil {
		t.Fatal(err)
	}
	return created
}
