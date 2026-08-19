package knowledgevault

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCandidateWithoutObjectiveEvidenceRequiresReview(t *testing.T) {
	root := t.TempDir()
	lesson, err := CreateCandidate(root, Draft{
		DisplayID:         "LESSON-7FB1",
		Title:             "Keep verification status deterministic",
		Statement:         "Verification state comes from the check result.",
		Problem:           "A descriptive explanation was treated as proof.",
		RootCause:         "The input boundary accepted trust state from prose.",
		Correction:        "Derive the status from exact verification evidence.",
		Platform:          "Windows",
		VerificationScope: "One local verification run and its report.",
		Applicability:     "Deterministic verification controllers.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if lesson.Status != StatusRequiresReview {
		t.Fatalf("expected missing evidence to require review, got %s", lesson.Status)
	}
	if _, err := ReviewLesson(root, lesson.ID, Review{Reviewer: "reviewer", Approve: true}); err != nil {
		t.Fatal(err)
	}
	current, err := Read(root, ScopeProject, lesson.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != StatusRequiresReview {
		t.Fatalf("AI or reviewer prose promoted a lesson without evidence: %s", current.Status)
	}
}

func TestAuthoritativeLessonSurvivesIndexDeletionAndRebuild(t *testing.T) {
	root := t.TempDir()
	lesson, err := CreateCandidate(root, validDraft("LESSON-INDEX"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RebuildIndex(root, ScopeProject); err != nil {
		t.Fatal(err)
	}
	indexPath := IndexPath(root, ScopeProject)
	if err := os.Remove(indexPath); err != nil {
		t.Fatal(err)
	}
	read, err := Read(root, ScopeProject, lesson.ID)
	if err != nil || read.ID != lesson.ID {
		t.Fatalf("authoritative lesson depended on generated index: %v", err)
	}
	rebuilt, err := RebuildIndex(root, ScopeProject)
	if err != nil || len(rebuilt.Lessons) != 1 {
		t.Fatalf("index did not rebuild from authoritative source: %v %+v", err, rebuilt)
	}
	if err := os.WriteFile(indexPath, []byte(`{"schema_version":"1","unknown":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadIndex(root, ScopeProject); err == nil {
		t.Fatal("malformed generated index was accepted")
	}
	if _, err := RebuildIndex(root, ScopeProject); err != nil {
		t.Fatalf("malformed index prevented source rebuild: %v", err)
	}
}

func TestDisplayIDsAreNotMachineIdentity(t *testing.T) {
	root := t.TempDir()
	first, err := CreateCandidate(root, validDraft("LESSON-SAME"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := CreateCandidate(root, validDraft("LESSON-SAME"))
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || first.DisplayID != second.DisplayID {
		t.Fatalf("display ID was treated as machine identity: first=%+v second=%+v", first, second)
	}
}

func TestMalformedOrTamperedAuthoritativeLessonFailsClosed(t *testing.T) {
	root := t.TempDir()
	lesson, err := CreateCandidate(root, validDraft("LESSON-INTEGRITY"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(LocalDirectory(root), lesson.ID+"-r0001.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "Keep verification status deterministic", "Changed after approval", 1))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(root, ScopeProject, lesson.ID); err == nil || !strings.Contains(err.Error(), "content hash") {
		t.Fatalf("expected tamper detection, got %v", err)
	}
}

func TestCorrectionUsesAnAppendOnlyLinkedRevision(t *testing.T) {
	root := t.TempDir()
	first, err := CreateCandidate(root, validDraft("LESSON-REVISION"))
	if err != nil {
		t.Fatal(err)
	}
	trusted := first
	trusted.Revision = 2
	trusted.Status = StatusVerified
	trusted.SourceFixIDs = []string{"FIX-TEST"}
	trusted.SourceProjectIDs = []string{"test-project"}
	trusted.PreviousHash = first.ContentHash
	trusted, err = appendRevision(root, ScopeProject, trusted)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Supersede(root, first.ID, "reviewer", "The first wording was too narrow.")
	if err != nil {
		t.Fatal(err)
	}
	if second.Revision != 3 || second.PreviousHash != trusted.ContentHash || second.Status != StatusSuperseded {
		t.Fatalf("revision was not linked: %+v", second)
	}
	old, err := os.ReadFile(filepath.Join(LocalDirectory(root), first.ID+"-r0001.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(old) == "" {
		t.Fatal("original revision disappeared")
	}
	current, err := Read(root, ScopeProject, first.ID)
	if err != nil || current.Revision != 3 || current.Status != StatusSuperseded {
		t.Fatalf("latest revision was not selected: %v %+v", err, current)
	}
	corrected, err := Correct(root, first.ID, validDraft("LESSON-REVISION"))
	if err != nil {
		t.Fatal(err)
	}
	if corrected.Revision != 4 || corrected.PreviousHash != second.ContentHash || corrected.Status != StatusRequiresReview {
		t.Fatalf("correction did not append a linked review candidate: %+v", corrected)
	}
}

func TestSupersedeRequiresVerifiedCurrentRevision(t *testing.T) {
	root := t.TempDir()
	lesson, err := CreateCandidate(root, validDraft("LESSON-TRANSITION"))
	if err != nil {
		t.Fatal(err)
	}
	if lesson.Status != StatusRequiresReview {
		t.Fatalf("expected an evidence-free candidate to require review, got %s", lesson.Status)
	}
	if _, err := Supersede(root, lesson.ID, "reviewer", "invalid transition"); err == nil {
		t.Fatal("REQUIRES_REVIEW lesson was superseded")
	}
	rejected, err := ReviewLesson(root, lesson.ID, Review{Reviewer: "reviewer", Approve: false})
	if err != nil {
		t.Fatal(err)
	}
	if rejected.Status != StatusRejected {
		t.Fatalf("expected rejected lesson, got %s", rejected.Status)
	}
	if _, err := Supersede(root, lesson.ID, "reviewer", "invalid transition"); err == nil {
		t.Fatal("REJECTED lesson was superseded")
	}
}

func validDraft(displayID string) Draft {
	return Draft{
		DisplayID:         displayID,
		Title:             "Keep verification status deterministic",
		Statement:         "Verification state comes from the check result.",
		Problem:           "A descriptive explanation was treated as proof.",
		RootCause:         "The input boundary accepted trust state from prose.",
		Correction:        "Derive the status from exact verification evidence.",
		Technologies:      []string{"Go"},
		RelevantVersions:  map[string]string{"go": "1.22"},
		Platform:          "Windows",
		VerificationScope: "One local verification run and its report.",
		Applicability:     "Deterministic verification controllers.",
		Limitations:       []string{"This candidate is still awaiting review."},
	}
}
