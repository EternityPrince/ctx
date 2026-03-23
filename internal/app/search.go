package app

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/vladimirkasterin/ctx/internal/codebase"
	"github.com/vladimirkasterin/ctx/internal/storage"
)

const (
	projectSearchModeAll    = "all"
	projectSearchModeSymbol = "symbol"
	projectSearchModeText   = "text"
	projectSearchModeRegex  = "regex"
)

const projectSearchLimit = 16

type projectTextMatch struct {
	FilePath          string
	PackageImportPath string
	Line              int
	Column            int
	Preview           string
	MatchKind         string
	SearchText        string
}

type projectTextFileGroup struct {
	FilePath              string
	PackageImportPath     string
	Matches               []projectTextMatch
	TotalMatches          int
	RelevantSymbolCount   int
	RelatedTestCount      int
	TestLinkedSymbolCount int
	PackageImportance     int
	ReverseDepCount       int
	Score                 int
	Why                   string
}

type projectTextPackageGroup struct {
	PackageImportPath string
	Files             []projectTextFileGroup
	TotalMatches      int
	PackageImportance int
	ReverseDepCount   int
	Score             int
	Why               string
}

type projectSearchResults struct {
	Query        string
	Mode         string
	Symbols      []storage.SymbolMatch
	Text         []projectTextMatch
	TextPackages []projectTextPackageGroup
}

func loadProjectSearchResults(root string, store *storage.Store, mode, query string, limit int) (projectSearchResults, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return projectSearchResults{}, fmt.Errorf("search query cannot be empty")
	}
	if limit <= 0 {
		limit = projectSearchLimit
	}

	results := projectSearchResults{
		Query: query,
		Mode:  normalizeProjectSearchMode(mode),
	}

	if results.Mode == projectSearchModeAll || results.Mode == projectSearchModeSymbol {
		symbols, err := store.FindSymbols(query)
		if err != nil {
			return projectSearchResults{}, err
		}
		if len(symbols) > limit {
			symbols = symbols[:limit]
		}
		results.Symbols = symbols
	}

	if results.Mode == projectSearchModeAll || results.Mode == projectSearchModeText || results.Mode == projectSearchModeRegex {
		files, err := store.CurrentFiles()
		if err != nil {
			return projectSearchResults{}, err
		}
		fileSummaries, err := store.LoadFileSummaries()
		if err != nil {
			return projectSearchResults{}, err
		}
		packageMetrics, err := store.LoadSearchPackageMetrics()
		if err != nil {
			return projectSearchResults{}, err
		}
		text, grouped, err := findProjectTextMatches(
			root,
			files,
			fileSummaries,
			packageMetrics,
			query,
			results.Mode == projectSearchModeRegex,
			limit,
		)
		if err != nil {
			return projectSearchResults{}, err
		}
		results.Text = text
		results.TextPackages = grouped
	}

	return results, nil
}

func normalizeProjectSearchMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case projectSearchModeSymbol, "sym":
		return projectSearchModeSymbol
	case projectSearchModeText, "literal":
		return projectSearchModeText
	case projectSearchModeRegex, "re":
		return projectSearchModeRegex
	default:
		return projectSearchModeAll
	}
}

func parseProjectSearchArgs(args []string, defaultMode string) (string, string, error) {
	mode := normalizeProjectSearchMode(defaultMode)
	if len(args) == 0 {
		return "", "", fmt.Errorf("Usage: search [symbol|text|regex] <query>")
	}

	first := strings.ToLower(strings.TrimSpace(args[0]))
	switch first {
	case projectSearchModeSymbol, "sym", projectSearchModeText, "literal", projectSearchModeRegex, "re", projectSearchModeAll:
		mode = normalizeProjectSearchMode(first)
		args = args[1:]
	}
	query := strings.TrimSpace(strings.Join(args, " "))
	if query == "" {
		return "", "", fmt.Errorf("Usage: search [symbol|text|regex] <query>")
	}
	return mode, query, nil
}

func findProjectTextMatches(
	root string,
	files map[string]codebase.PreviousFile,
	fileSummaries map[string]storage.FileSummary,
	packageMetrics map[string]storage.SearchPackageMetrics,
	query string,
	useRegex bool,
	limit int,
) ([]projectTextMatch, []projectTextPackageGroup, error) {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	if limit <= 0 {
		limit = projectSearchLimit
	}

	var compiled *regexp.Regexp
	var err error
	if useRegex {
		compiled, err = regexp.Compile(query)
		if err != nil {
			return nil, nil, fmt.Errorf("compile regex: %w", err)
		}
	}

	smartCase := hasUppercase(query)
	lowerQuery := strings.ToLower(query)
	fileGroups := make(map[string]*projectTextFileGroup)
	for _, relPath := range paths {
		absPath := filepath.Join(root, filepath.FromSlash(relPath))
		stat, err := os.Stat(absPath)
		if err != nil || stat.IsDir() || stat.Size() > 1<<20 {
			continue
		}

		data, err := os.ReadFile(absPath)
		if err != nil || bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
			continue
		}

		lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
		for idx, line := range lines {
			column := matchSearchLine(line, query, lowerQuery, compiled, useRegex, smartCase)
			if column == 0 {
				continue
			}

			preview := strings.TrimSpace(line)
			if preview == "" {
				preview = line
			}

			group, ok := fileGroups[relPath]
			if !ok {
				group = &projectTextFileGroup{
					FilePath:          relPath,
					PackageImportPath: files[relPath].PackageImportPath,
				}
				if group.PackageImportPath == "" {
					group.PackageImportPath = fileSummaries[relPath].PackageImportPath
				}
				fileGroups[relPath] = group
			}
			group.Matches = append(group.Matches, projectTextMatch{
				FilePath:          relPath,
				PackageImportPath: group.PackageImportPath,
				Line:              idx + 1,
				Column:            column,
				Preview:           preview,
				MatchKind:         searchTextKind(useRegex),
				SearchText:        query,
			})
		}
	}

	displayed, grouped := rankProjectTextMatches(fileGroups, fileSummaries, packageMetrics, limit)
	return displayed, grouped, nil
}

func rankProjectTextMatches(
	fileGroups map[string]*projectTextFileGroup,
	fileSummaries map[string]storage.FileSummary,
	packageMetrics map[string]storage.SearchPackageMetrics,
	limit int,
) ([]projectTextMatch, []projectTextPackageGroup) {
	packages := make(map[string]*projectTextPackageGroup)
	for _, group := range fileGroups {
		sort.Slice(group.Matches, func(i, j int) bool {
			if group.Matches[i].Line != group.Matches[j].Line {
				return group.Matches[i].Line < group.Matches[j].Line
			}
			return group.Matches[i].Column < group.Matches[j].Column
		})
		group.TotalMatches = len(group.Matches)

		summary := fileSummaries[group.FilePath]
		pkg := packageMetrics[group.PackageImportPath]
		group.RelevantSymbolCount = summary.RelevantSymbolCount
		group.RelatedTestCount = summary.RelatedTestCount
		group.TestLinkedSymbolCount = summary.TestLinkedSymbolCount
		group.PackageImportance = pkg.ImportanceScore
		group.ReverseDepCount = pkg.ReverseDepCount
		group.Score = group.TotalMatches*100 +
			group.RelevantSymbolCount*6 +
			group.TestLinkedSymbolCount*8 +
			group.RelatedTestCount*4 +
			group.ReverseDepCount*10 +
			group.PackageImportance*2
		group.Why = describeProjectTextFileWhy(*group)

		packageGroup, ok := packages[group.PackageImportPath]
		if !ok {
			packageGroup = &projectTextPackageGroup{
				PackageImportPath: group.PackageImportPath,
				PackageImportance: pkg.ImportanceScore,
				ReverseDepCount:   pkg.ReverseDepCount,
			}
			packages[group.PackageImportPath] = packageGroup
		}
		packageGroup.Files = append(packageGroup.Files, *group)
		packageGroup.TotalMatches += group.TotalMatches
	}

	grouped := make([]projectTextPackageGroup, 0, len(packages))
	for _, pkg := range packages {
		sort.Slice(pkg.Files, func(i, j int) bool {
			left := pkg.Files[i]
			right := pkg.Files[j]
			if left.Score != right.Score {
				return left.Score > right.Score
			}
			if left.TotalMatches != right.TotalMatches {
				return left.TotalMatches > right.TotalMatches
			}
			return left.FilePath < right.FilePath
		})
		fileScore := 0
		for _, file := range pkg.Files {
			fileScore += file.Score / 4
		}
		pkg.Score = pkg.TotalMatches*60 + pkg.PackageImportance*3 + pkg.ReverseDepCount*10 + fileScore
		pkg.Why = describeProjectTextPackageWhy(*pkg)
		grouped = append(grouped, *pkg)
	}

	sort.Slice(grouped, func(i, j int) bool {
		if grouped[i].Score != grouped[j].Score {
			return grouped[i].Score > grouped[j].Score
		}
		if grouped[i].TotalMatches != grouped[j].TotalMatches {
			return grouped[i].TotalMatches > grouped[j].TotalMatches
		}
		return grouped[i].PackageImportPath < grouped[j].PackageImportPath
	})

	displayed := make([]projectTextMatch, 0, limit)
	displayedGroups := make([]projectTextPackageGroup, 0, len(grouped))
	remaining := limit
	for _, pkg := range grouped {
		if remaining == 0 {
			break
		}
		nextPackage := projectTextPackageGroup{
			PackageImportPath: pkg.PackageImportPath,
			TotalMatches:      pkg.TotalMatches,
			PackageImportance: pkg.PackageImportance,
			ReverseDepCount:   pkg.ReverseDepCount,
			Score:             pkg.Score,
			Why:               pkg.Why,
		}
		for _, file := range pkg.Files {
			if remaining == 0 {
				break
			}
			take := min(len(file.Matches), remaining)
			if take == 0 {
				continue
			}
			nextFile := file
			nextFile.Matches = append([]projectTextMatch(nil), file.Matches[:take]...)
			nextPackage.Files = append(nextPackage.Files, nextFile)
			displayed = append(displayed, nextFile.Matches...)
			remaining -= take
		}
		if len(nextPackage.Files) > 0 {
			displayedGroups = append(displayedGroups, nextPackage)
		}
	}

	return displayed, displayedGroups
}

func matchSearchLine(line, query, lowerQuery string, compiled *regexp.Regexp, useRegex, smartCase bool) int {
	if useRegex {
		loc := compiled.FindStringIndex(line)
		if loc == nil {
			return 0
		}
		return loc[0] + 1
	}

	haystack := line
	needle := query
	if !smartCase {
		haystack = strings.ToLower(line)
		needle = lowerQuery
	}
	idx := strings.Index(haystack, needle)
	if idx < 0 {
		return 0
	}
	return idx + 1
}

func searchTextKind(useRegex bool) string {
	if useRegex {
		return "regex"
	}
	return "text"
}

func hasUppercase(value string) bool {
	for _, r := range value {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

func describeSymbolSearchWhy(symbol storage.SymbolMatch) string {
	parts := []string{symbolSearchKindReason(symbol.SearchKind)}
	if symbol.CallerCount > 0 {
		parts = append(parts, fmt.Sprintf("callers=%d", symbol.CallerCount))
	}
	if symbol.ReferenceCount > 0 {
		parts = append(parts, fmt.Sprintf("refs=%d", symbol.ReferenceCount))
	}
	if symbol.TestCount > 0 {
		parts = append(parts, fmt.Sprintf("tests=%d", symbol.TestCount))
	}
	if symbol.ReversePackageDeps > 0 {
		parts = append(parts, fmt.Sprintf("rdeps=%d", symbol.ReversePackageDeps))
	}
	if symbol.PackageImportance > 0 {
		parts = append(parts, fmt.Sprintf("pkg=%d", symbol.PackageImportance))
	}
	return strings.Join(parts, " | ")
}

func symbolSearchKindReason(kind string) string {
	switch kind {
	case "exact":
		return "exact symbol match"
	case "name":
		return "exact entity name"
	case "prefix":
		return "prefix match"
	case "contains":
		return "contains match"
	case "shape":
		return "normalized shape match"
	case "fuzzy":
		return "fuzzy typo match"
	default:
		return "search match"
	}
}

func describeProjectTextFileWhy(group projectTextFileGroup) string {
	parts := []string{fmt.Sprintf("matches=%d", group.TotalMatches)}
	if group.RelevantSymbolCount > 0 {
		parts = append(parts, fmt.Sprintf("symbols=%d", group.RelevantSymbolCount))
	}
	if group.TestLinkedSymbolCount > 0 {
		parts = append(parts, fmt.Sprintf("linked_tests=%d", group.TestLinkedSymbolCount))
	}
	if group.RelatedTestCount > 0 {
		parts = append(parts, fmt.Sprintf("nearby_tests=%d", group.RelatedTestCount))
	}
	if group.ReverseDepCount > 0 {
		parts = append(parts, fmt.Sprintf("rdeps=%d", group.ReverseDepCount))
	}
	if group.PackageImportance > 0 {
		parts = append(parts, fmt.Sprintf("pkg=%d", group.PackageImportance))
	}
	return strings.Join(parts, " | ")
}

func describeProjectTextPackageWhy(group projectTextPackageGroup) string {
	parts := []string{fmt.Sprintf("matches=%d", group.TotalMatches)}
	if len(group.Files) > 0 {
		parts = append(parts, fmt.Sprintf("files=%d", len(group.Files)))
	}
	if group.ReverseDepCount > 0 {
		parts = append(parts, fmt.Sprintf("rdeps=%d", group.ReverseDepCount))
	}
	if group.PackageImportance > 0 {
		parts = append(parts, fmt.Sprintf("pkg=%d", group.PackageImportance))
	}
	return strings.Join(parts, " | ")
}

func projectTextDisplayedFileCount(groups []projectTextPackageGroup) int {
	count := 0
	for _, group := range groups {
		count += len(group.Files)
	}
	return count
}
