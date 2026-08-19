package main

import (
	"encoding/json"
	"testing"

	"devctl/internal/fixrecord"
	"devctl/internal/knowledgevault"
)

func TestKnowledgeVaultRequiresFixEvidenceAndAllowsExplicitPromotion(t *testing.T) {
	root, input := writeFixesCLIFixture(t)
	stdout, stderr := captureStreams(t, func() int {
		return fixesCommand([]string{"record", "--json", "--input", input, root})
	})
	if len(stderr) != 0 {
		t.Fatalf("Fix Record creation wrote to stderr: %s", stderr)
	}
	var record fixrecord.Record
	if err := json.Unmarshal(stdout, &record); err != nil {
		t.Fatal(err)
	}
	draft := validKnowledgeDraft(record.ID)
	candidate, err := knowledgevault.CreateCandidate(root, draft)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Status != knowledgevault.StatusCandidate {
		t.Fatalf("verified Fix Record did not produce a candidate: %s", candidate.Status)
	}
	verified, err := knowledgevault.ReviewLesson(root, candidate.ID, knowledgevault.Review{Reviewer: "human-reviewer", Approve: true})
	if err != nil {
		t.Fatal(err)
	}
	if verified.Status != knowledgevault.StatusVerified {
		t.Fatalf("explicit objective review did not verify lesson: %s", verified.Status)
	}
	if _, err := knowledgevault.Promote(root, t.TempDir(), verified.ID, knowledgevault.PromotionApproval{Reviewer: "publisher", Approve: false}); err == nil {
		t.Fatal("promotion without explicit approval succeeded")
	}
	globalRoot := t.TempDir()
	global, err := knowledgevault.Promote(root, globalRoot, verified.ID, knowledgevault.PromotionApproval{Reviewer: "publisher", Approve: true})
	if err != nil {
		t.Fatal(err)
	}
	if global.Status != knowledgevault.StatusVerified || global.Scope != knowledgevault.ScopeGlobal {
		t.Fatalf("unexpected promoted lesson: %+v", global)
	}
	stdout, stderr = captureStreams(t, func() int {
		return knowledgeCommand([]string{"search", "--json", "--project-root", root, "--global-root", globalRoot, "verified"})
	})
	if len(stderr) != 0 {
		t.Fatalf("knowledge search wrote to stderr: %s", stderr)
	}
	var results knowledgevault.SearchResponse
	if err := json.Unmarshal(stdout, &results); err != nil || len(results.Results) != 2 || results.Total != 2 || results.Truncated {
		t.Fatalf("unexpected deterministic search result: %v %+v", err, results)
	}
	stdout, stderr = captureStreams(t, func() int {
		return knowledgeCommand([]string{"show", "--json", root, candidate.DisplayID})
	})
	if len(stderr) != 0 {
		t.Fatalf("knowledge display show wrote to stderr: %s", stderr)
	}
	var shown knowledgevault.Lesson
	if err := json.Unmarshal(stdout, &shown); err != nil || shown.ID != candidate.ID {
		t.Fatalf("display ID show failed: %v %+v", err, shown)
	}
}

func TestKnowledgeCorrectionRebindsFixSourcesAndMissingSourcesRequireReview(t *testing.T) {
	root, input := writeFixesCLIFixture(t)
	stdout, stderr := captureStreams(t, func() int {
		return fixesCommand([]string{"record", "--json", "--input", input, root})
	})
	if len(stderr) != 0 {
		t.Fatalf("Fix Record creation wrote to stderr: %s", stderr)
	}
	var record fixrecord.Record
	if err := json.Unmarshal(stdout, &record); err != nil {
		t.Fatal(err)
	}
	candidate, err := knowledgevault.CreateCandidate(root, validKnowledgeDraft(record.ID))
	if err != nil {
		t.Fatal(err)
	}
	verified, err := knowledgevault.ReviewLesson(root, candidate.ID, knowledgevault.Review{Reviewer: "reviewer", Approve: true})
	if err != nil {
		t.Fatal(err)
	}
	corrected, err := knowledgevault.Correct(root, verified.ID, validKnowledgeDraft(record.ID))
	if err != nil {
		t.Fatal(err)
	}
	if corrected.Status != knowledgevault.StatusCandidate || len(corrected.SourceProjectIDs) != 1 || corrected.SourceProjectIDs[0] != record.ProjectID {
		t.Fatalf("correction lost bound Fix Record provenance: %+v", corrected)
	}
	correctedVerified, err := knowledgevault.ReviewLesson(root, corrected.ID, knowledgevault.Review{Reviewer: "reviewer", Approve: true})
	if err != nil {
		t.Fatal(err)
	}
	if correctedVerified.Status != knowledgevault.StatusVerified || len(correctedVerified.SourceProjectIDs) != 1 || correctedVerified.SourceProjectIDs[0] != record.ProjectID {
		t.Fatalf("review lost corrected Fix Record provenance: %+v", correctedVerified)
	}
	missing := validKnowledgeDraft("FIX-DOES-NOT-EXIST")
	missingCorrection, err := knowledgevault.Correct(root, verified.ID, missing)
	if err != nil {
		t.Fatal(err)
	}
	if missingCorrection.Status != knowledgevault.StatusRequiresReview {
		t.Fatalf("missing correction evidence was trusted: %s", missingCorrection.Status)
	}
}

func TestKnowledgePromotionSanitizesVersionMetadata(t *testing.T) {
	root, input := writeFixesCLIFixture(t)
	stdout, stderr := captureStreams(t, func() int {
		return fixesCommand([]string{"record", "--json", "--input", input, root})
	})
	if len(stderr) != 0 {
		t.Fatalf("Fix Record creation wrote to stderr: %s", stderr)
	}
	var record fixrecord.Record
	if err := json.Unmarshal(stdout, &record); err != nil {
		t.Fatal(err)
	}
	draft := validKnowledgeDraft(record.ID)
	draft.RelevantVersions = map[string]string{"workspace": `C:\Users\private\repo`}
	candidate, err := knowledgevault.CreateCandidate(root, draft)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := knowledgevault.ReviewLesson(root, candidate.ID, knowledgevault.Review{Reviewer: "reviewer", Approve: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := knowledgevault.Promote(root, t.TempDir(), verified.ID, knowledgevault.PromotionApproval{Reviewer: "publisher", Approve: true}); err == nil {
		t.Fatal("private path in version metadata was promoted")
	}
}

func validKnowledgeDraft(sourceID string) knowledgevault.Draft {
	return knowledgevault.Draft{
		DisplayID:         "LESSON-PROMOTION",
		Title:             "Keep verified status tied to evidence",
		Statement:         "A reusable rule must keep its objective source visible.",
		Problem:           "A repair explanation was treated as proof by itself.",
		RootCause:         "The knowledge boundary did not separate descriptive text from evidence.",
		Correction:        "Require a verified Fix Record before human review can verify the lesson.",
		Technologies:      []string{"Go"},
		RelevantVersions:  map[string]string{"go": "1.25"},
		Platform:          "Windows",
		VerificationScope: "The named Fix Record pre and post verification pair.",
		Applicability:     "Deterministic project verification controllers.",
		Limitations:       []string{"The source Fix Record remains project-specific evidence."},
		SourceFixIDs:      []string{sourceID},
	}
}
