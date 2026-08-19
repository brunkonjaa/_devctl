package fixrecord

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"devctl/internal/evidence"
	"devctl/internal/gitstate"
	"devctl/internal/knowledge"
	"devctl/internal/model"
)

func TestCreateClosesExactFixWithoutCreatingLesson(t *testing.T) {
	root, candidate := fixRecordFixture(t, nil)
	candidate.Attempts = []Attempt{{Outcome: "FAILED", Description: "increase the timeout", Reason: "contention remained"}}

	record, err := Create(root, candidate, Options{Now: fixedRecordTime})
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != StatusVerified || record.ClosureRule != ClosureRuleVersion || record.RecordHash == "" {
		t.Fatalf("record did not close as a hashed VERIFIED record: %+v", record)
	}
	if record.ProjectID != candidate.ProjectID || record.PreRun.RunID != candidate.PreRunID || record.PostRun.RunID != candidate.PostRunID {
		t.Fatalf("evidence identity was not derived correctly: %+v", record)
	}
	if record.PreRun.ReportSHA256 == "" || record.PostRun.ReportSHA256 == "" || record.ChangeFingerprint == "" {
		t.Fatalf("exact evidence hashes were not bound into the record: %+v", record)
	}
	if len(record.CheckTransitions) != 1 || record.CheckTransitions[0].BeforeStatus != model.Fail || record.CheckTransitions[0].AfterStatus != model.Pass {
		t.Fatalf("check transition was not preserved: %+v", record.CheckTransitions)
	}
	if len(record.Attempts) != 1 || record.Attempts[0].Outcome != "FAILED" {
		t.Fatalf("failed attempt was not retained: %+v", record.Attempts)
	}
	if _, err := os.Stat(knowledge.Path(root)); !os.IsNotExist(err) {
		t.Fatalf("Fix Record creation changed the lesson store: %v", err)
	}
	info, err := os.Lstat(Path(root, candidate.ID))
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("record is not a normal file: info=%v err=%v", info, err)
	}

	shown, err := Show(root, candidate.ID)
	if err != nil || shown.RecordHash != record.RecordHash {
		t.Fatalf("stored record cannot be read back: record=%+v err=%v", shown, err)
	}
	listed, err := List(root, 10)
	if err != nil || len(listed) != 1 || listed[0].ID != candidate.ID || listed[0].RecordHash != record.RecordHash {
		t.Fatalf("record list is incorrect: records=%+v err=%v", listed, err)
	}
}

func TestCreateIsAppendOnlyAndSupersessionPreservesOldBytes(t *testing.T) {
	root, candidate := fixRecordFixture(t, nil)
	if _, err := Create(root, candidate, Options{Now: fixedRecordTime}); err != nil {
		t.Fatal(err)
	}
	originalPath := Path(root, candidate.ID)
	original, err := os.ReadFile(originalPath)
	if err != nil {
		t.Fatal(err)
	}

	candidate.Title = "attempted overwrite"
	if _, err := Create(root, candidate, Options{Now: fixedRecordTime}); !errors.Is(err, ErrRecordExists) {
		t.Fatalf("expected immutable duplicate rejection, got %v", err)
	}
	afterDuplicate, err := os.ReadFile(originalPath)
	if err != nil || !bytes.Equal(original, afterDuplicate) {
		t.Fatalf("duplicate write changed original bytes: err=%v", err)
	}

	candidate.ID = "FIX-0002"
	candidate.Title = "stronger verified explanation"
	candidate.Supersedes = "FIX-0001"
	second, err := Create(root, candidate, Options{Now: func() time.Time { return fixedRecordTime().Add(time.Minute) }})
	if err != nil {
		t.Fatal(err)
	}
	if second.Supersedes != "FIX-0001" {
		t.Fatalf("supersession missing: %+v", second)
	}
	afterSupersession, err := os.ReadFile(originalPath)
	if err != nil || !bytes.Equal(original, afterSupersession) {
		t.Fatalf("supersession changed original bytes: err=%v", err)
	}
}

func TestCreateRejectsUnverifiedClosureWithoutWriting(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*model.Report, *model.Report, *Candidate)
	}{
		{name: "same run", mutate: func(_ *model.Report, _ *model.Report, candidate *Candidate) { candidate.PostRunID = candidate.PreRunID }},
		{name: "missing pre run", mutate: func(_ *model.Report, _ *model.Report, candidate *Candidate) { candidate.PreRunID = "missing-run" }},
		{name: "candidate project mismatch", mutate: func(_ *model.Report, _ *model.Report, candidate *Candidate) {
			candidate.ProjectID = "different-project"
		}},
		{name: "report project mismatch", mutate: func(_ *model.Report, post *model.Report, _ *Candidate) { post.Project.Identity = "different-project" }},
		{name: "pre check passed", mutate: func(pre *model.Report, _ *model.Report, _ *Candidate) { pre.Checks[0].Status = model.Pass }},
		{name: "pre check skipped", mutate: func(pre *model.Report, _ *model.Report, _ *Candidate) { pre.Checks[0].Status = model.Skip }},
		{name: "pre check not applicable", mutate: func(pre *model.Report, _ *model.Report, _ *Candidate) { pre.Checks[0].Status = model.NotApplicable }},
		{name: "post check failed", mutate: func(_ *model.Report, post *model.Report, _ *Candidate) {
			post.Checks[0].Status = model.Fail
			post.Overall = model.Fail
		}},
		{name: "post run still blocks", mutate: func(_ *model.Report, post *model.Report, _ *Candidate) {
			post.Checks = append(post.Checks, model.CheckResult{ID: "secret-scan", CheckVersion: "secret-v1", Status: model.NotTested, Blocking: true, Summary: "not tested"})
			post.Overall = model.NotTested
		}},
		{name: "stale fingerprint", mutate: func(_ *model.Report, post *model.Report, _ *Candidate) {
			post.RepositoryFingerprint = strings.Repeat("0", 64)
		}},
		{name: "reversed run order", mutate: func(pre *model.Report, post *model.Report, _ *Candidate) {
			post.FinishedAt = pre.FinishedAt.Add(-time.Second)
		}},
		{name: "missing provenance", mutate: func(_ *model.Report, post *model.Report, _ *Candidate) { post.PolicyVersion = "" }},
		{name: "wrong command", mutate: func(_ *model.Report, post *model.Report, _ *Candidate) { post.Command = "discover" }},
		{name: "wrong project path", mutate: func(_ *model.Report, post *model.Report, _ *Candidate) {
			post.Project.Path = filepath.Dir(post.Project.Path)
		}},
		{name: "missing check version", mutate: func(_ *model.Report, post *model.Report, _ *Candidate) { post.Checks[0].CheckVersion = "" }},
		{name: "missing technology provenance", mutate: func(_ *model.Report, post *model.Report, _ *Candidate) { post.Project.Technologies = nil }},
		{name: "missing target check", mutate: func(_ *model.Report, post *model.Report, _ *Candidate) { post.Checks = nil }},
		{name: "unknown related fix", mutate: func(_ *model.Report, _ *model.Report, candidate *Candidate) {
			candidate.RelatedFixIDs = []string{"FIX-9999"}
		}},
		{name: "unknown superseded fix", mutate: func(_ *model.Report, _ *model.Report, candidate *Candidate) { candidate.Supersedes = "FIX-9999" }},
		{name: "traversal id", mutate: func(_ *model.Report, _ *model.Report, candidate *Candidate) { candidate.ID = "../FIX-0001" }},
		{name: "secret text", mutate: func(_ *model.Report, _ *model.Report, candidate *Candidate) {
			candidate.FinalFix = "token=" + strings.Repeat("a", 16)
		}},
		{name: "control text", mutate: func(_ *model.Report, _ *model.Report, candidate *Candidate) { candidate.Problem = "bad\x1b[31mproblem" }},
		{name: "patch traversal", mutate: func(_ *model.Report, _ *model.Report, candidate *Candidate) {
			candidate.PatchEvidencePath = "../change.patch"
			candidate.PatchSHA256 = strings.Repeat("0", 64)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, candidate := fixRecordFixture(t, test.mutate)
			if _, err := Create(root, candidate, Options{Now: fixedRecordTime}); err == nil {
				t.Fatal("expected closure rejection")
			}
			entries, err := os.ReadDir(Directory(root))
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("rejected closure wrote records: %v", entries)
			}
		})
	}
}

func TestShowAndListDoNotReverifyCurrentProject(t *testing.T) {
	root, candidate := fixRecordFixture(t, nil)
	record, err := Create(root, candidate, Options{Now: fixedRecordTime})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	shown, err := Show(root, record.ID)
	if err != nil || shown.RecordHash != record.RecordHash {
		t.Fatalf("show unexpectedly reverified the changed project: record=%+v err=%v", shown, err)
	}
	listed, err := List(root, 10)
	if err != nil || len(listed) != 1 || listed[0].ID != record.ID {
		t.Fatalf("list unexpectedly reverified the changed project: records=%+v err=%v", listed, err)
	}
}

func TestShowAndListRejectNonNormalOrUnexpectedStoredEntries(t *testing.T) {
	root, candidate := fixRecordFixture(t, nil)
	if _, err := Create(root, candidate, Options{Now: fixedRecordTime}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(Directory(root), "unexpected.txt"), []byte("not a record"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := List(root, 10); err == nil {
		t.Fatal("expected list to fail closed on an unexpected store entry")
	}
	if err := os.Remove(Path(root, candidate.ID)); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(Path(root, candidate.ID), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Show(root, candidate.ID); err == nil {
		t.Fatal("expected show to reject a directory in place of a record")
	}
}

func TestCreateValidatesContainedPatchArtifact(t *testing.T) {
	root, candidate := fixRecordFixture(t, nil)
	patchPath := filepath.Join(root, ".devctl", "evidence", "repair", "task-1", "change.patch")
	if err := os.MkdirAll(filepath.Dir(patchPath), 0o700); err != nil {
		t.Fatal(err)
	}
	patch := []byte("diff --git a/main.go b/main.go\n")
	if err := os.WriteFile(patchPath, patch, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(patch)
	candidate.PatchEvidencePath = filepath.ToSlash(filepath.Join(".devctl", "evidence", "repair", "task-1", "change.patch"))
	candidate.PatchSHA256 = hex.EncodeToString(sum[:])
	record, err := Create(root, candidate, Options{Now: fixedRecordTime})
	if err != nil {
		t.Fatal(err)
	}
	if record.PatchSHA256 != candidate.PatchSHA256 || record.PatchEvidencePath != candidate.PatchEvidencePath {
		t.Fatalf("patch evidence was not bound: %+v", record)
	}

	root, candidate = fixRecordFixture(t, nil)
	candidate.PatchEvidencePath = filepath.ToSlash(filepath.Join(".devctl", "evidence", "repair", "missing.patch"))
	candidate.PatchSHA256 = strings.Repeat("0", 64)
	if _, err := Create(root, candidate, Options{Now: fixedRecordTime}); err == nil {
		t.Fatal("expected missing/mismatched patch evidence rejection")
	}
}

func TestStoredRecordTamperingFailsClosed(t *testing.T) {
	root, candidate := fixRecordFixture(t, nil)
	if _, err := Create(root, candidate, Options{Now: fixedRecordTime}); err != nil {
		t.Fatal(err)
	}
	path := Path(root, candidate.ID)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte(candidate.Title), []byte("tampered title"), 1)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Show(root, candidate.ID); err == nil {
		t.Fatal("expected tampered record to fail hash validation")
	}
	if _, err := List(root, 10); err == nil {
		t.Fatal("expected list to fail closed on tampered record")
	}
	candidate.ID = "FIX-0002"
	if _, err := Create(root, candidate, Options{Now: fixedRecordTime}); err == nil {
		t.Fatal("expected append to fail closed while the existing store is tampered")
	}
	if _, err := os.Stat(Path(root, candidate.ID)); !os.IsNotExist(err) {
		t.Fatalf("tampered store accepted a new record: %v", err)
	}
}

func TestConcurrentCreateAllowsExactlyOneRecord(t *testing.T) {
	root, candidate := fixRecordFixture(t, nil)
	const writers = 8
	var wait sync.WaitGroup
	results := make(chan error, writers)
	for index := 0; index < writers; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := Create(root, candidate, Options{Now: fixedRecordTime})
			results <- err
		}()
	}
	wait.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
			continue
		}
		if !errors.Is(err, ErrRecordExists) {
			t.Fatalf("unexpected concurrent error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("expected one successful create, got %d", successes)
	}
}

func TestDecodeCandidateIsBoundedAndStrict(t *testing.T) {
	_, candidate := fixRecordFixture(t, nil)
	data, err := json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeCandidate(bytes.NewReader(data))
	if err != nil || decoded.ID != candidate.ID {
		t.Fatalf("valid candidate was rejected: %+v err=%v", decoded, err)
	}
	unknown := append(append([]byte(nil), data[:len(data)-1]...), []byte(`,"unknown":true}`)...)
	if _, err := DecodeCandidate(bytes.NewReader(unknown)); err == nil {
		t.Fatal("expected unknown field rejection")
	}
	duplicate := append(append([]byte(nil), data[:len(data)-1]...), []byte(`,"id":"FIX-0002"}`)...)
	if _, err := DecodeCandidate(bytes.NewReader(duplicate)); err == nil {
		t.Fatal("expected duplicate field rejection")
	}
	if _, err := DecodeCandidate(strings.NewReader(string(data) + `{}`)); err == nil {
		t.Fatal("expected multiple-object rejection")
	}
	if _, err := DecodeCandidate(strings.NewReader(strings.Repeat("x", MaxCandidateBytes+1))); err == nil {
		t.Fatal("expected oversized candidate rejection")
	}
}

func TestCandidateValidationErrorOrderIsDeterministic(t *testing.T) {
	_, candidate := fixRecordFixture(t, nil)
	candidate.Problem = ""
	candidate.RootCause = ""
	candidate.FinalFix = ""
	candidate.Applicability = ""
	for attempt := 0; attempt < 100; attempt++ {
		err := validateCandidate(candidate)
		if err == nil || !strings.Contains(err.Error(), "problem") {
			t.Fatalf("validation did not return the first ordered field: %v", err)
		}
	}
}

func fixRecordFixture(t *testing.T, mutate func(*model.Report, *model.Report, *Candidate)) (string, Candidate) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fingerprint, err := gitstate.Fingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	project := model.Project{Name: "fixture", Path: root, Identity: "project-fixture", Technologies: []model.Technology{{ID: "go", Confidence: "high"}}}
	pre := model.Report{
		SchemaVersion: "1", Command: "verify", RunID: "run-before", PolicyVersion: "policy-v1", DevctlVersion: "0.1.0", DevctlCommit: "devctl-before",
		RepositoryRevision: "revision-before", RepositoryFingerprint: strings.Repeat("1", 64), StartedAt: time.Date(2026, 8, 19, 8, 0, 0, 0, time.UTC), FinishedAt: time.Date(2026, 8, 19, 8, 1, 0, 0, time.UTC),
		Project: &project, Overall: model.Fail, Checks: []model.CheckResult{{ID: "go-test", CheckVersion: "go-pack-v1", Status: model.Fail, Blocking: true, Summary: "tests failed"}},
	}
	post := model.Report{
		SchemaVersion: "1", Command: "verify", RunID: "run-after", PolicyVersion: "policy-v1", DevctlVersion: "0.1.0", DevctlCommit: "devctl-after",
		RepositoryRevision: "revision-after", RepositoryFingerprint: fingerprint, StartedAt: time.Date(2026, 8, 19, 8, 2, 0, 0, time.UTC), FinishedAt: time.Date(2026, 8, 19, 8, 3, 0, 0, time.UTC),
		Project: &project, Overall: model.Pass, Checks: []model.CheckResult{{ID: "go-test", CheckVersion: "go-pack-v1", Status: model.Pass, Summary: "tests passed"}},
	}
	candidate := Candidate{
		SchemaVersion:      CandidateSchemaVersion,
		ID:                 "FIX-0001",
		Title:              "Go tests failed under shared cache contention",
		ProjectID:          project.Identity,
		Problem:            "The race test timed out while another Go check used the shared cache.",
		Symptoms:           []string{"go-test-race ended with inactivity_timeout"},
		RootCause:          "Independent checks contended for the same Go build cache.",
		AffectedComponents: []string{"scheduler", "go adapter"},
		AffectedFiles:      []string{"internal/adapters/golang/golang.go"},
		FinalFix:           "Serialize Go toolchain checks through one scheduler resource.",
		PreRunID:           pre.RunID,
		PostRunID:          post.RunID,
		CheckIDs:           []string{"go-test"},
		KnownLimitations:   []string{"The record does not establish cross-project applicability."},
		Applicability:      "This exact project and evidence pair.",
		RelevantVersions:   map[string]string{"go": "1.25"},
		Tags:               []string{"go", "scheduler"},
	}
	if mutate != nil {
		mutate(&pre, &post, &candidate)
	}
	if _, err := evidence.Write(root, pre); err != nil {
		t.Fatal(err)
	}
	if post.RunID != pre.RunID {
		if _, err := evidence.Write(root, post); err != nil {
			t.Fatal(err)
		}
	}
	return root, candidate
}

func fixedRecordTime() time.Time {
	return time.Date(2026, 8, 19, 9, 0, 0, 0, time.UTC)
}
