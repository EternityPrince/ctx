package storage

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

type qualityChangeContext struct {
	changedFiles    map[string]struct{}
	changedDirs     map[string]struct{}
	changedPackages map[string]struct{}
}

func (s *Store) loadQualityContext(current SnapshotInfo) (qualityChangeContext, error) {
	ctx := qualityChangeContext{
		changedFiles:    map[string]struct{}{},
		changedDirs:     map[string]struct{}{},
		changedPackages: map[string]struct{}{},
	}
	if !current.ParentID.Valid {
		return ctx, nil
	}

	diff, err := s.Diff(current.ParentID.Int64, current.ID)
	if err != nil {
		return ctx, err
	}
	for _, filePath := range append(append([]string{}, diff.AddedFiles...), diff.ChangedFiles...) {
		filePath = normalizeReportPath(filePath)
		ctx.changedFiles[filePath] = struct{}{}
		ctx.changedDirs[normalizeReportDir(filePath)] = struct{}{}
	}
	for _, pkg := range diff.ChangedPackages {
		if strings.TrimSpace(pkg.ImportPath) == "" {
			continue
		}
		ctx.changedPackages[strings.TrimSpace(pkg.ImportPath)] = struct{}{}
	}
	return ctx, nil
}

func normalizeReportPath(value string) string {
	value = path.Clean(strings.TrimSpace(strings.ReplaceAll(value, "\\", "/")))
	value = strings.TrimPrefix(value, "./")
	if value == "." {
		return ""
	}
	return value
}

func normalizeReportDir(filePath string) string {
	filePath = normalizeReportPath(filePath)
	if filePath == "" {
		return "."
	}
	dir := path.Dir(filePath)
	if dir == "" {
		return "."
	}
	return dir
}

func entrypointFileSignal(filePath string) (int, string) {
	filePath = normalizeReportPath(filePath)
	switch {
	case strings.HasSuffix(filePath, "/__main__.py") || filePath == "__main__.py":
		return 4, "python entrypoint file"
	case strings.HasSuffix(filePath, "/main.go") || filePath == "main.go" || strings.HasSuffix(filePath, "/main.rs") || filePath == "main.rs":
		return 4, "main entrypoint file"
	case strings.HasPrefix(filePath, "cmd/") || strings.Contains(filePath, "/cmd/"):
		return 3, "cmd entrypoint surface"
	case strings.HasPrefix(filePath, "bin/") || strings.Contains(filePath, "/bin/"):
		return 3, "bin entrypoint surface"
	case strings.HasPrefix(filePath, "cli/") || strings.Contains(filePath, "/cli/"):
		return 3, "cli entrypoint surface"
	case strings.HasPrefix(filePath, "scripts/") || strings.Contains(filePath, "/scripts/"):
		return 2, "script entrypoint surface"
	case strings.HasPrefix(filePath, "examples/") || strings.Contains(filePath, "/examples/") || strings.HasPrefix(filePath, "example/") || strings.Contains(filePath, "/example/"):
		return 2, "example entrypoint surface"
	default:
		return 0, ""
	}
}

func entrypointSymbolSignal(symbol SymbolMatch) (int, string) {
	if strings.EqualFold(strings.TrimSpace(symbol.Name), "main") && symbol.Kind == "func" {
		return 2, "main entrypoint symbol"
	}
	return 0, ""
}

func fileChangeSignal(summary FileSummary, ctx qualityChangeContext) (int, []string, bool, int) {
	filePath := normalizeReportPath(summary.FilePath)
	if _, ok := ctx.changedFiles[filePath]; ok {
		return 8, []string{"recently changed file"}, true, 0
	}
	if summary.PackageImportPath != "" {
		if _, ok := ctx.changedPackages[summary.PackageImportPath]; ok {
			return 5, []string{"near recent changes in same package"}, false, 1
		}
	}
	if _, ok := ctx.changedDirs[normalizeReportDir(filePath)]; ok {
		return 3, []string{"near recent changes in same directory"}, false, 2
	}
	return 0, nil, false, -1
}

func symbolChangeSignal(symbol SymbolMatch, ctx qualityChangeContext) (int, []string) {
	summary := FileSummary{
		FilePath:          symbol.FilePath,
		PackageImportPath: symbol.PackageImportPath,
	}
	score, reasons, _, _ := fileChangeSignal(summary, ctx)
	return score, reasons
}

func uniqueReasons(parts []string) []string {
	if len(parts) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(parts))
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		result = append(result, part)
	}
	return result
}

func applyRankedPackageQuality(items []RankedPackage, ctx qualityChangeContext) {
	for idx := range items {
		graphScore := items[idx].ReverseDepCount*4 + items[idx].Summary.SymbolCount + items[idx].Summary.TestCount*2
		items[idx].GraphScore = graphScore
		items[idx].Score = graphScore
		reasons := []string{}
		if items[idx].ReverseDepCount > 0 {
			reasons = append(reasons, fmt.Sprintf("%d reverse package deps", items[idx].ReverseDepCount))
		}
		if items[idx].Summary.SymbolCount > 0 {
			reasons = append(reasons, fmt.Sprintf("%d indexed symbols", items[idx].Summary.SymbolCount))
		}
		if items[idx].Summary.TestCount > 0 {
			reasons = append(reasons, fmt.Sprintf("%d related tests", items[idx].Summary.TestCount))
		}
		if _, ok := ctx.changedPackages[items[idx].Summary.ImportPath]; ok {
			items[idx].Score += 5
			reasons = append(reasons, "recently changed package")
		}
		items[idx].QualityWhy = uniqueReasons(reasons)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Score != items[j].Score {
			return items[i].Score > items[j].Score
		}
		return items[i].Summary.ImportPath < items[j].Summary.ImportPath
	})
}

func rankedFunctionGraphScore(item RankedSymbol) int {
	return item.CallerCount*5 + item.CalleeCount + item.ReferenceCount*2 + item.TestCount*3 + item.ReversePackageDeps*2
}

func rankedTypeGraphScore(item RankedSymbol) int {
	return item.ReferenceCount*4 + item.TestCount*3 + item.MethodCount*2 + item.ReversePackageDeps*2
}

func applyRankedSymbolQuality(items []RankedSymbol, kind string, ctx qualityChangeContext) {
	for idx := range items {
		items[idx] = scoredRankedSymbol(items[idx], kind, ctx)
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Score != items[j].Score {
			return items[i].Score > items[j].Score
		}
		return items[i].Symbol.QName < items[j].Symbol.QName
	})
}

func scoredRankedSymbol(item RankedSymbol, kind string, ctx qualityChangeContext) RankedSymbol {
	graphScore := rankedFunctionGraphScore(item)
	if kind == "type" {
		graphScore = rankedTypeGraphScore(item)
	}
	item.GraphScore = graphScore
	item.Score = graphScore
	reasons := []string{}
	if item.CallerCount > 0 {
		reasons = append(reasons, fmt.Sprintf("%d callers", item.CallerCount))
	}
	if item.ReferenceCount > 0 {
		reasons = append(reasons, fmt.Sprintf("%d inbound refs", item.ReferenceCount))
	}
	if item.TestCount > 0 {
		reasons = append(reasons, fmt.Sprintf("%d direct tests", item.TestCount))
	}
	if item.ReversePackageDeps > 0 {
		reasons = append(reasons, fmt.Sprintf("%d reverse package deps", item.ReversePackageDeps))
	}
	if item.MethodCount > 0 {
		reasons = append(reasons, fmt.Sprintf("%d attached methods", item.MethodCount))
	}

	boost, why := entrypointFileSignal(item.Symbol.FilePath)
	if boost > 0 {
		item.Score += boost
		reasons = append(reasons, why)
	}
	boost, why = entrypointSymbolSignal(item.Symbol)
	if boost > 0 {
		item.Score += boost
		reasons = append(reasons, why)
	}
	boost, whyParts := symbolChangeSignal(item.Symbol, ctx)
	if boost > 0 {
		item.Score += boost
		reasons = append(reasons, whyParts...)
	}
	item.QualityWhy = uniqueReasons(reasons)
	return item
}

func applyFileSummaryQuality(summary FileSummary, ctx qualityChangeContext) FileSummary {
	graphScore := summary.InboundCallCount*5 +
		summary.InboundReferenceCount*3 +
		summary.RelatedTestCount*3 +
		summary.ReversePackageDeps*2 +
		summary.RelevantSymbolCount +
		summary.DeclaredTestCount*2
	summary.GraphScore = graphScore
	summary.QualityScore = graphScore
	summary.ChangeDistance = -1

	reasons := []string{}
	if summary.InboundCallCount > 0 {
		reasons = append(reasons, fmt.Sprintf("%d inbound calls", summary.InboundCallCount))
	}
	if summary.InboundReferenceCount > 0 {
		reasons = append(reasons, fmt.Sprintf("%d inbound refs", summary.InboundReferenceCount))
	}
	if summary.RelatedTestCount > 0 {
		reasons = append(reasons, fmt.Sprintf("%d related tests", summary.RelatedTestCount))
	}
	if summary.ReversePackageDeps > 0 {
		reasons = append(reasons, fmt.Sprintf("%d reverse package deps", summary.ReversePackageDeps))
	}
	if summary.RelevantSymbolCount > 0 {
		reasons = append(reasons, fmt.Sprintf("%d relevant symbols", summary.RelevantSymbolCount))
	}

	boost, why := entrypointFileSignal(summary.FilePath)
	if boost > 0 {
		summary.QualityScore += boost
		summary.IsEntrypoint = true
		reasons = append(reasons, why)
	}
	boost, whyParts, changedRecently, distance := fileChangeSignal(summary, ctx)
	if boost > 0 {
		summary.QualityScore += boost
		summary.ChangedRecently = changedRecently
		summary.ChangeDistance = distance
		reasons = append(reasons, whyParts...)
	}
	summary.QualityWhy = uniqueReasons(reasons)
	return summary
}

func buildRankedFiles(summaries map[string]FileSummary, limit int) []RankedFile {
	if limit <= 0 {
		limit = 8
	}

	files := make([]RankedFile, 0, len(summaries))
	for _, summary := range summaries {
		if summary.FilePath == "" {
			continue
		}
		files = append(files, RankedFile{
			Summary:    summary,
			GraphScore: summary.GraphScore,
			Score:      summary.QualityScore,
			QualityWhy: append([]string(nil), summary.QualityWhy...),
		})
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].Score != files[j].Score {
			return files[i].Score > files[j].Score
		}
		return files[i].Summary.FilePath < files[j].Summary.FilePath
	})
	if len(files) > limit {
		files = files[:limit]
	}
	return files
}

func (s *Store) attachRankedFileSymbols(snapshotID int64, files []RankedFile) error {
	for idx := range files {
		rows, err := s.db.Query(`
			SELECT symbol_key, name, line
			FROM symbols
			WHERE snapshot_id = ? AND file_path = ?
			ORDER BY line, col, qname
			LIMIT 4
		`, snapshotID, files[idx].Summary.FilePath)
		if err != nil {
			return fmt.Errorf("query ranked file symbols: %w", err)
		}

		var primaryKey string
		var primaryLine int
		var names []string
		for rows.Next() {
			var symbolKey string
			var name string
			var line int
			if err := rows.Scan(&symbolKey, &name, &line); err != nil {
				rows.Close()
				return fmt.Errorf("scan ranked file symbol: %w", err)
			}
			if primaryKey == "" {
				primaryKey = symbolKey
				primaryLine = line
			}
			if strings.TrimSpace(name) != "" {
				names = append(names, name)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("iterate ranked file symbols: %w", err)
		}
		rows.Close()

		files[idx].PrimarySymbolKey = primaryKey
		files[idx].PrimaryLine = primaryLine
		files[idx].TopSymbols = names
	}
	return nil
}

func qualityForSymbolView(view SymbolView, ctx qualityChangeContext) (int, []string) {
	item := RankedSymbol{
		Symbol:             view.Symbol,
		CallerCount:        len(view.Callers),
		CalleeCount:        len(view.Callees),
		ReferenceCount:     len(view.ReferencesIn),
		TestCount:          len(view.Tests),
		ReversePackageDeps: len(view.Package.ReverseDeps),
	}
	kind := "function"
	if view.Symbol.Kind != "method" && view.Symbol.Kind != "func" {
		for _, sibling := range view.Siblings {
			if sibling.Kind == "method" {
				item.MethodCount++
			}
		}
		kind = "type"
	}
	item = scoredRankedSymbol(item, kind, ctx)
	return item.Score, item.QualityWhy
}
