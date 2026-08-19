package knowledgevault

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"devctl/internal/fixrecord"
)

const (
	MaxSearchResults = 20
	MaxSearchBytes   = 16 * 1024
	MaxSearchText    = 2048
	MaxSearchTerms   = 64
)

type scoredLesson struct {
	result SearchResult
	order  int
}

func Search(projectRoot, globalRoot string, query SearchQuery) (SearchResponse, error) {
	if len([]rune(query.Text)) > MaxSearchText {
		return SearchResponse{}, fmt.Errorf("knowledge search text exceeds the %d-character limit", MaxSearchText)
	}
	limit := query.Limit
	if limit == 0 {
		limit = 10
	}
	if limit < 1 || limit > MaxSearchResults {
		return SearchResponse{}, fmt.Errorf("knowledge search limit must be between 1 and %d", MaxSearchResults)
	}
	if strings.TrimSpace(projectRoot) == "" {
		return SearchResponse{}, errors.New("knowledge search requires a project root")
	}
	terms := searchTokens(query.Text)
	if strings.TrimSpace(query.Text) != "" && len(terms) == 0 {
		return SearchResponse{}, errors.New("knowledge search text contains no searchable terms")
	}
	if len(terms) > MaxSearchTerms {
		return SearchResponse{}, fmt.Errorf("knowledge search contains more than %d terms", MaxSearchTerms)
	}
	filters := []struct{ name, value string }{
		{"check", query.CheckID}, {"failure", query.FailureID}, {"technology", query.Technology},
		{"version", query.Version}, {"platform", query.Platform}, {"tag", query.Tag},
		{"adapter", query.Adapter}, {"path", query.Path}, {"symptom", query.Symptom},
	}
	for _, filter := range filters {
		if len([]rune(filter.value)) > 512 {
			return SearchResponse{}, fmt.Errorf("knowledge search %s filter exceeds the 512-character limit", filter.name)
		}
	}

	var scored []scoredLesson
	order := 0
	projectLessons, err := searchLessons(projectRoot, ScopeProject, query, terms)
	if err != nil {
		return SearchResponse{}, err
	}
	for _, item := range projectLessons {
		item.order = order
		order++
		scored = append(scored, item)
	}
	if strings.TrimSpace(globalRoot) == "" {
		globalRoot = projectRoot
	}
	globalLessons, err := searchLessons(globalRoot, ScopeGlobal, query, terms)
	if err != nil {
		return SearchResponse{}, err
	}
	for _, item := range globalLessons {
		item.order = order
		order++
		scored = append(scored, item)
	}

	sort.SliceStable(scored, func(i, j int) bool {
		left, right := scored[i].result, scored[j].result
		if left.Score != right.Score {
			return left.Score > right.Score
		}
		if statusRank(left.Status) != statusRank(right.Status) {
			return statusRank(left.Status) < statusRank(right.Status)
		}
		if left.Scope != right.Scope {
			return scopeRank(left.Scope) < scopeRank(right.Scope)
		}
		if left.ID != right.ID {
			return left.ID < right.ID
		}
		if left.Revision != right.Revision {
			return left.Revision > right.Revision
		}
		return scored[i].order < scored[j].order
	})

	results := make([]SearchResult, 0, min(limit, len(scored)))
	for _, item := range scored {
		if len(results) == limit {
			break
		}
		results = append(results, item.result)
	}
	return boundSearchResponse(SearchResponse{Total: len(scored), Results: results}), nil
}

func searchLessons(root, scope string, query SearchQuery, terms []string) ([]scoredLesson, error) {
	lessons, err := ListAll(root, scope)
	if err != nil {
		return nil, err
	}
	if !query.IncludeHistory {
		current, currentErr := List(root, scope)
		if currentErr != nil {
			return nil, currentErr
		}
		lessons = current
	}
	result := make([]scoredLesson, 0, len(lessons))
	for _, lesson := range lessons {
		if !query.IncludeHistory && lesson.Status != StatusVerified {
			continue
		}
		score, reasons, matched := scoreLesson(lesson, query, terms)
		if !matched {
			continue
		}
		result = append(result, scoredLesson{result: searchResult(root, lesson, score, reasons)})
	}
	return result, nil
}

func scoreLesson(lesson Lesson, query SearchQuery, terms []string) (int, []string, bool) {
	score := 0
	filterMatched := false
	textMatched := false
	reasons := make([]string, 0, 12)
	addFilter := func(label, value string, values []string, weight int, contains bool) bool {
		if strings.TrimSpace(value) == "" {
			return true
		}
		matched := matchExactAny
		if contains {
			matched = matchContainsAny
		}
		if !matched(value, values) {
			return false
		}
		score += weight
		filterMatched = true
		reasons = append(reasons, label+":"+normalizeForSearch(value))
		return true
	}
	if !addFilter("check", query.CheckID, lesson.CheckIDs, 16, false) ||
		!addFilter("failure", query.FailureID, lesson.FailureIDs, 16, false) ||
		!addFilter("technology", query.Technology, lesson.Technologies, 12, false) ||
		!addFilter("platform", query.Platform, []string{lesson.Platform}, 10, false) ||
		!addFilter("tag", query.Tag, lesson.Tags, 12, false) ||
		!addFilter("adapter", query.Adapter, lesson.Adapters, 12, false) ||
		!addFilter("symptom", query.Symptom, lesson.Symptoms, 10, true) ||
		!addFilter("path", query.Path, lesson.AffectedPaths, 10, true) {
		return 0, nil, false
	}
	if strings.TrimSpace(query.Version) != "" {
		versionValues := make([]string, 0, len(lesson.RelevantVersions)*2)
		keys := make([]string, 0, len(lesson.RelevantVersions))
		for key := range lesson.RelevantVersions {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			versionValues = append(versionValues, key, lesson.RelevantVersions[key])
		}
		if !matchExactAny(query.Version, versionValues) {
			return 0, nil, false
		}
		score += 12
		filterMatched = true
		reasons = append(reasons, "version:"+normalizeForSearch(query.Version))
	}
	fields := []struct {
		label  string
		value  string
		weight int
	}{
		{"title", lesson.Title, 6}, {"statement", lesson.Statement, 4}, {"problem", lesson.Problem, 4},
		{"root_cause", lesson.RootCause, 4}, {"correction", lesson.Correction, 3}, {"platform", lesson.Platform, 3},
		{"technology", strings.Join(lesson.Technologies, " "), 5}, {"tag", strings.Join(lesson.Tags, " "), 5},
		{"check", strings.Join(lesson.CheckIDs, " "), 5}, {"failure", strings.Join(lesson.FailureIDs, " "), 5},
		{"adapter", strings.Join(lesson.Adapters, " "), 5}, {"normalized_error", strings.Join(lesson.NormalizedErrors, " "), 5},
		{"path", strings.Join(lesson.AffectedPaths, " "), 4}, {"symptom", strings.Join(lesson.Symptoms, " "), 4},
	}
	for _, term := range terms {
		for _, field := range fields {
			if containsSearchTerm(field.value, term) {
				score += field.weight
				textMatched = true
				reasons = append(reasons, "query:"+field.label+":"+term)
				break
			}
		}
	}
	if len(terms) > 0 && !textMatched {
		return 0, nil, false
	}
	if lesson.Status == StatusVerified {
		score++
	}
	return score, reasons, textMatched || filterMatched || len(terms) == 0
}

func searchResult(root string, lesson Lesson, score int, reasons []string) SearchResult {
	result := SearchResult{
		ID: lesson.ID, DisplayID: lesson.DisplayID, Scope: lesson.Scope, Revision: lesson.Revision,
		Status: lesson.Status, Score: score, MatchReasons: uniqueSorted(reasons), Title: boundText(lesson.Title, 256),
		Statement: boundText(lesson.Statement, 768), Technologies: boundedStrings(lesson.Technologies, 16, 128),
		RelevantVersions: boundedMap(lesson.RelevantVersions, 16, 128, 256), Applicability: boundText(lesson.Applicability, 512),
		Tags: boundedStrings(lesson.Tags, 16, 128), CheckIDs: boundedStrings(lesson.CheckIDs, 16, 128),
		FailureIDs: boundedStrings(lesson.FailureIDs, 16, 128), Adapters: boundedStrings(lesson.Adapters, 16, 128),
		AffectedPaths: boundedStrings(lesson.AffectedPaths, 16, 256),
		Limitations:   boundedStrings(lesson.Limitations, 8, 256), SourceLessonID: lesson.SourceLessonID,
		SourceFixIDs: boundedStrings(lesson.SourceFixIDs, 16, 128),
	}
	if lesson.Scope == ScopeProject {
		for _, fixID := range lesson.SourceFixIDs {
			record, err := fixrecord.Show(root, fixID)
			if err != nil {
				continue
			}
			result.SourceEvidence = append(result.SourceEvidence, boundedStrings(record.RelatedEvidence, 8, 256)...)
		}
		result.SourceEvidence = uniqueSorted(result.SourceEvidence)
	}
	return result
}

func matchExactAny(query string, values []string) bool {
	wanted := normalizeForSearch(query)
	for _, value := range values {
		if normalizeForSearch(value) == wanted {
			return true
		}
	}
	return false
}

func matchContainsAny(query string, values []string) bool {
	wanted := normalizeForSearch(query)
	for _, value := range values {
		candidate := normalizeForSearch(value)
		if candidate == wanted || strings.Contains(candidate, wanted) || strings.Contains(wanted, candidate) {
			return true
		}
	}
	return false
}

func containsSearchTerm(value, term string) bool {
	for _, candidate := range searchTokens(value) {
		if candidate == term || strings.Contains(candidate, term) || strings.Contains(term, candidate) {
			return true
		}
	}
	return false
}

func searchTokens(value string) []string {
	return uniqueSorted(strings.FieldsFunc(strings.ToLower(value), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }))
}

func normalizeForSearch(value string) string { return strings.Join(searchTokens(value), " ") }

func statusRank(status string) int {
	switch status {
	case StatusVerified:
		return 0
	case StatusCandidate:
		return 1
	case StatusRequiresReview:
		return 2
	case StatusSuperseded:
		return 3
	case StatusRejected:
		return 4
	default:
		return 5
	}
}

func scopeRank(scope string) int {
	if scope == ScopeProject {
		return 0
	}
	return 1
}

func MarshalSearchJSON(response SearchResponse) ([]byte, error) {
	response = boundSearchResponse(response)
	data, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func boundSearchResponse(response SearchResponse) SearchResponse {
	for {
		response.Returned = len(response.Results)
		response.Truncated = response.Returned < response.Total
		data, err := json.MarshalIndent(response, "", "  ")
		if err == nil && len(data)+1 <= MaxSearchBytes {
			return response
		}
		if len(response.Results) == 0 {
			return response
		}
		response.Results = response.Results[:len(response.Results)-1]
	}
}

func boundText(value string, maximum int) string {
	runes := []rune(value)
	if len(runes) > maximum {
		return string(runes[:maximum])
	}
	return value
}

func boundedStrings(values []string, maximumItems, maximumRunes int) []string {
	result := make([]string, 0, min(maximumItems, len(values)))
	for _, value := range values {
		if len(result) == maximumItems {
			break
		}
		result = append(result, boundText(value, maximumRunes))
	}
	return result
}

func boundedMap(values map[string]string, maximumItems, maximumKeyRunes, maximumValueRunes int) map[string]string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make(map[string]string, min(maximumItems, len(keys)))
	for _, key := range keys[:min(maximumItems, len(keys))] {
		result[boundText(key, maximumKeyRunes)] = boundText(values[key], maximumValueRunes)
	}
	return result
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}
