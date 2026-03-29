package app

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/core"
	"github.com/vladimirkasterin/ctx/internal/storage"
)

type workflowRiskContext struct {
	hotScores   map[string]int
	recentFiles map[string]struct{}
}

func ensureIndexedSnapshot(stdout io.Writer, state core.ProjectState) (storage.ProjectStatus, bool, error) {
	status, err := state.Store.Status(projectService.ChangedNow(state))
	if err != nil {
		return storage.ProjectStatus{}, false, err
	}
	if !status.HasSnapshot {
		_, err := fmt.Fprintf(stdout, "No index snapshot for %s. Run `ctx index %s` first.\n", state.Info.ModulePath, state.Info.Root)
		return status, false, err
	}
	return status, true, nil
}

func loadWorkflowRiskContext(store *storage.Store) (workflowRiskContext, error) {
	report, err := store.LoadReportView(64)
	if err != nil {
		return workflowRiskContext{}, err
	}
	hotScores := make(map[string]int, len(report.TopFiles))
	for _, item := range rankShellHotFiles(report, "") {
		hotScores[item.Path] = item.Score
	}

	recentFiles, err := loadRecentChangedFileSet(store)
	if err != nil {
		return workflowRiskContext{}, err
	}
	return workflowRiskContext{
		hotScores:   hotScores,
		recentFiles: recentFiles,
	}, nil
}

func (ctx workflowRiskContext) hotScore(filePath string) int {
	if ctx.hotScores == nil {
		return 0
	}
	return ctx.hotScores[filePath]
}

func (ctx workflowRiskContext) recentChanged(filePath string) bool {
	if ctx.recentFiles == nil {
		return false
	}
	_, ok := ctx.recentFiles[filePath]
	return ok
}

func resolveIndexedFileQuery(root, query string, summaries map[string]storage.FileSummary) (string, bool, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", false, nil
	}

	candidate := filepath.Clean(query)
	if filepath.IsAbs(candidate) {
		rel, err := filepath.Rel(root, candidate)
		if err != nil {
			return "", false, fmt.Errorf("resolve file path: %w", err)
		}
		candidate = rel
	}
	candidate = filepath.ToSlash(candidate)
	candidate = strings.TrimPrefix(candidate, "./")
	if _, ok := summaries[candidate]; ok {
		return candidate, true, nil
	}

	findMatches := func(match func(string) bool) []string {
		matches := make([]string, 0)
		for relPath := range summaries {
			if match(relPath) {
				matches = append(matches, relPath)
			}
		}
		sort.Strings(matches)
		return matches
	}

	if strings.Contains(candidate, "/") {
		matches := findMatches(func(relPath string) bool {
			return strings.HasSuffix(relPath, "/"+candidate) || relPath == candidate
		})
		switch len(matches) {
		case 0:
		case 1:
			return matches[0], true, nil
		default:
			return "", false, fmt.Errorf("ambiguous file query %q: %s", query, strings.Join(matches[:min(6, len(matches))], ", "))
		}
	}

	base := filepath.Base(candidate)
	matches := findMatches(func(relPath string) bool {
		return filepath.Base(relPath) == base
	})
	switch len(matches) {
	case 0:
		return "", false, nil
	case 1:
		return matches[0], true, nil
	default:
		return "", false, fmt.Errorf("ambiguous file query %q: %s", query, strings.Join(matches[:min(6, len(matches))], ", "))
	}
}

func topFileSummaries(summaries map[string]storage.FileSummary, include func(storage.FileSummary) bool, limit int) []storage.FileSummary {
	values := make([]storage.FileSummary, 0, len(summaries))
	for _, summary := range summaries {
		if include != nil && !include(summary) {
			continue
		}
		values = append(values, summary)
	}
	sort.Slice(values, func(i, j int) bool {
		left := values[i]
		right := values[j]
		if left.QualityScore != right.QualityScore {
			return left.QualityScore > right.QualityScore
		}
		if left.GraphScore != right.GraphScore {
			return left.GraphScore > right.GraphScore
		}
		if left.RelevantSymbolCount != right.RelevantSymbolCount {
			return left.RelevantSymbolCount > right.RelevantSymbolCount
		}
		if left.RelatedTestCount != right.RelatedTestCount {
			return left.RelatedTestCount > right.RelatedTestCount
		}
		if left.ChangedRecently != right.ChangedRecently {
			return left.ChangedRecently
		}
		return left.FilePath < right.FilePath
	})
	if limit > 0 && len(values) > limit {
		values = values[:limit]
	}
	return values
}

func dedupeTestsByBestScore(values []storage.TestView, limit int) []storage.TestView {
	byKey := make(map[string]storage.TestView, len(values))
	ordered := make([]storage.TestView, 0, len(values))
	for _, value := range values {
		key := strings.TrimSpace(value.TestKey)
		if key == "" {
			key = fmt.Sprintf("%s:%d:%s", value.FilePath, value.Line, value.Name)
		}
		current, ok := byKey[key]
		switch {
		case !ok:
			byKey[key] = value
			ordered = append(ordered, value)
		case value.Score > current.Score:
			if current.Why != "" && current.Why != value.Why {
				value.Why = mergeWhyParts(value.Why, current.Why)
			}
			byKey[key] = value
		default:
			current.Why = mergeWhyParts(current.Why, value.Why)
			byKey[key] = current
		}
	}

	tests := make([]storage.TestView, 0, len(byKey))
	for _, original := range ordered {
		key := strings.TrimSpace(original.TestKey)
		if key == "" {
			key = fmt.Sprintf("%s:%d:%s", original.FilePath, original.Line, original.Name)
		}
		value, ok := byKey[key]
		if !ok {
			continue
		}
		tests = append(tests, value)
		delete(byKey, key)
	}
	sort.Slice(tests, func(i, j int) bool {
		if tests[i].Score != tests[j].Score {
			return tests[i].Score > tests[j].Score
		}
		if tests[i].FilePath != tests[j].FilePath {
			return tests[i].FilePath < tests[j].FilePath
		}
		if tests[i].Line != tests[j].Line {
			return tests[i].Line < tests[j].Line
		}
		return tests[i].Name < tests[j].Name
	})
	if limit > 0 && len(tests) > limit {
		tests = tests[:limit]
	}
	return tests
}

func renderHumanChecklist(stdout io.Writer, p palette, title string, items []string, limit int) error {
	if _, err := fmt.Fprintf(stdout, "%s (%d)\n", p.section(title), len(items)); err != nil {
		return err
	}
	if len(items) == 0 {
		return renderHumanEmpty(stdout, p)
	}
	if limit <= 0 || limit > len(items) {
		limit = len(items)
	}
	for idx, item := range items[:limit] {
		if _, err := fmt.Fprintf(stdout, "  %d. %s\n", idx+1, item); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(stdout); err != nil {
		return err
	}
	return renderMoreLine(stdout, len(items), limit)
}

func renderAIChecklist(stdout io.Writer, key string, items []string, limit int) error {
	if _, err := fmt.Fprintf(stdout, "%s=%d\n", key, len(items)); err != nil {
		return err
	}
	if limit <= 0 || limit > len(items) {
		limit = len(items)
	}
	for _, item := range items[:limit] {
		if _, err := fmt.Fprintf(stdout, "- %s\n", item); err != nil {
			return err
		}
	}
	return renderMoreLine(stdout, len(items), limit)
}
