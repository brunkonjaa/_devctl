package knowledgevault

import (
	"time"
)

const (
	SchemaVersion = "1"

	ScopeProject = "PROJECT"
	ScopeGlobal  = "GLOBAL"

	StatusCandidate      = "CANDIDATE"
	StatusVerified       = "VERIFIED"
	StatusRequiresReview = "REQUIRES_REVIEW"
	StatusSuperseded     = "SUPERSEDED"
	StatusRejected       = "REJECTED"
)

// Lesson is one immutable revision. The JSON file containing it is
// authoritative; indexes contain only disposable copies of these fields.
type Lesson struct {
	SchemaVersion     string            `json:"schema_version"`
	ID                string            `json:"id"`
	DisplayID         string            `json:"display_id"`
	Scope             string            `json:"scope"`
	Revision          int               `json:"revision"`
	Status            string            `json:"status"`
	Title             string            `json:"title"`
	Statement         string            `json:"statement"`
	Problem           string            `json:"problem"`
	RootCause         string            `json:"root_cause"`
	Correction        string            `json:"correction"`
	Technologies      []string          `json:"technologies"`
	RelevantVersions  map[string]string `json:"relevant_versions"`
	Platform          string            `json:"platform"`
	VerificationScope string            `json:"verification_scope"`
	ValidatedAt       time.Time         `json:"validated_at"`
	Applicability     string            `json:"applicability"`
	Limitations       []string          `json:"limitations"`
	Tags              []string          `json:"tags,omitempty"`
	CheckIDs          []string          `json:"check_ids,omitempty"`
	FailureIDs        []string          `json:"failure_ids,omitempty"`
	Adapters          []string          `json:"adapters,omitempty"`
	NormalizedErrors  []string          `json:"normalized_errors,omitempty"`
	AffectedPaths     []string          `json:"affected_paths,omitempty"`
	Symptoms          []string          `json:"symptoms,omitempty"`
	SourceFixIDs      []string          `json:"source_fix_ids"`
	SourceProjectIDs  []string          `json:"source_project_ids"`
	SourceLessonID    string            `json:"source_lesson_id,omitempty"`
	PreviousHash      string            `json:"previous_hash,omitempty"`
	ReviewNote        string            `json:"review_note,omitempty"`
	Reviewer          string            `json:"reviewer,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	ContentHash       string            `json:"content_hash"`
}

// Draft is descriptive input. It cannot select VERIFIED status or provide
// objective verification fields; those come from Fix Records and review.
type Draft struct {
	DisplayID         string            `json:"display_id"`
	Title             string            `json:"title"`
	Statement         string            `json:"statement"`
	Problem           string            `json:"problem"`
	RootCause         string            `json:"root_cause"`
	Correction        string            `json:"correction"`
	Technologies      []string          `json:"technologies"`
	RelevantVersions  map[string]string `json:"relevant_versions"`
	Platform          string            `json:"platform"`
	VerificationScope string            `json:"verification_scope"`
	Applicability     string            `json:"applicability"`
	Limitations       []string          `json:"limitations"`
	Tags              []string          `json:"tags,omitempty"`
	CheckIDs          []string          `json:"check_ids,omitempty"`
	FailureIDs        []string          `json:"failure_ids,omitempty"`
	Adapters          []string          `json:"adapters,omitempty"`
	NormalizedErrors  []string          `json:"normalized_errors,omitempty"`
	AffectedPaths     []string          `json:"affected_paths,omitempty"`
	Symptoms          []string          `json:"symptoms,omitempty"`
	SourceFixIDs      []string          `json:"source_fix_ids"`
}

type Review struct {
	Reviewer string
	Approve  bool
	Note     string
}

type PromotionApproval struct {
	Reviewer string
	Approve  bool
	Note     string
}

type Index struct {
	SchemaVersion string        `json:"schema_version"`
	BuiltAt       time.Time     `json:"built_at"`
	Scope         string        `json:"scope"`
	Lessons       []IndexLesson `json:"lessons"`
}

type IndexLesson struct {
	ID               string            `json:"id"`
	DisplayID        string            `json:"display_id"`
	Revision         int               `json:"revision"`
	Status           string            `json:"status"`
	Title            string            `json:"title"`
	Statement        string            `json:"statement"`
	Technologies     []string          `json:"technologies"`
	RelevantVersions map[string]string `json:"relevant_versions"`
	Platform         string            `json:"platform"`
	Applicability    string            `json:"applicability"`
	Limitations      []string          `json:"limitations"`
	ValidatedAt      time.Time         `json:"validated_at"`
	Tags             []string          `json:"tags,omitempty"`
	CheckIDs         []string          `json:"check_ids,omitempty"`
	FailureIDs       []string          `json:"failure_ids,omitempty"`
	Adapters         []string          `json:"adapters,omitempty"`
	NormalizedErrors []string          `json:"normalized_errors,omitempty"`
	AffectedPaths    []string          `json:"affected_paths,omitempty"`
	Symptoms         []string          `json:"symptoms,omitempty"`
}

type SearchQuery struct {
	Text           string
	CheckID        string
	FailureID      string
	Technology     string
	Version        string
	Platform       string
	Tag            string
	Adapter        string
	Path           string
	Symptom        string
	IncludeHistory bool
	Limit          int
}

type SearchResult struct {
	ID               string            `json:"id"`
	DisplayID        string            `json:"display_id"`
	Scope            string            `json:"scope"`
	Revision         int               `json:"revision"`
	Status           string            `json:"status"`
	Score            int               `json:"score"`
	MatchReasons     []string          `json:"match_reasons"`
	Title            string            `json:"title"`
	Statement        string            `json:"statement"`
	Technologies     []string          `json:"technologies,omitempty"`
	RelevantVersions map[string]string `json:"relevant_versions,omitempty"`
	Tags             []string          `json:"tags,omitempty"`
	CheckIDs         []string          `json:"check_ids,omitempty"`
	FailureIDs       []string          `json:"failure_ids,omitempty"`
	Adapters         []string          `json:"adapters,omitempty"`
	AffectedPaths    []string          `json:"affected_paths,omitempty"`
	Applicability    string            `json:"applicability"`
	Limitations      []string          `json:"limitations,omitempty"`
	SourceLessonID   string            `json:"source_lesson_id,omitempty"`
	SourceFixIDs     []string          `json:"source_fix_ids,omitempty"`
	SourceEvidence   []string          `json:"source_evidence,omitempty"`
}

type SearchResponse struct {
	Total     int            `json:"total"`
	Returned  int            `json:"returned"`
	Truncated bool           `json:"truncated"`
	Results   []SearchResult `json:"results"`
}
