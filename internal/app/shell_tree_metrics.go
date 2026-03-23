package app

import (
	"fmt"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/codebase"
	"github.com/vladimirkasterin/ctx/internal/storage"
)

func aggregateTree(scannedFiles []shellScannedFile, summaries map[string]storage.FileSummary) treeAggregate {
	var aggregate treeAggregate
	aggregate.TotalFiles = len(scannedFiles)
	for _, file := range scannedFiles {
		aggregate.TotalBytes += file.SizeBytes
		aggregate.TotalLines += file.LineCount
		if codebase.IsIndexedSourceFile(file.Path) {
			aggregate.SourceFiles++
		}
		if file.IsTest {
			aggregate.TestFiles++
		}
		if summary, ok := summaries[file.Path]; ok {
			aggregate.IndexedFiles++
			aggregate.FuncCount += summary.FuncCount
			aggregate.MethodCount += summary.MethodCount
			aggregate.StructCount += summary.StructCount
			aggregate.DeclaredTestCount += summary.DeclaredTestCount
			aggregate.RelatedTestCount += summary.RelatedTestCount
			aggregate.RelevantSymbolCount += summary.RelevantSymbolCount
			aggregate.TestLinkedSymbolCount += summary.TestLinkedSymbolCount
		}
	}
	return aggregate
}

func treeColumnWidths(lines []shellTreeLine) (int, int) {
	hierWidth := 0
	pathWidth := 0
	for _, line := range lines {
		if line.IsDir {
			continue
		}
		hierWidth = max(hierWidth, len(line.Text))
		pathWidth = max(pathWidth, len(line.Path))
	}
	return hierWidth, pathWidth
}

func (s *shellSession) fileBadge(path string, isTest bool) string {
	switch {
	case isTest:
		return s.palette.kindBadge("test")
	case codebase.IsGoFile(path):
		return "[" + s.palette.wrap("1;36", "GO") + "]"
	case codebase.IsPythonFile(path):
		return "[" + s.palette.wrap("1;34", "PY") + "]"
	default:
		return "[" + s.palette.wrap("1;37", "FILE") + "]"
	}
}

func (s *shellSession) renderTreeFileMetrics(file shellScannedFile, summary storage.FileSummary) string {
	parts := []string{
		fmt.Sprintf("%dL", file.LineCount),
		shellHumanSize(file.SizeBytes),
	}
	if summary.SymbolCount > 0 || summary.IsTest {
		parts = append(parts,
			fmt.Sprintf("fn=%d", summary.FuncCount),
			fmt.Sprintf("m=%d", summary.MethodCount),
			fmt.Sprintf("type=%d", summary.StructCount),
		)
		if summary.IsTest {
			parts = append(parts, fmt.Sprintf("tests=%d", summary.DeclaredTestCount))
		} else if summary.RelatedTestCount > 0 || summary.RelevantSymbolCount > 0 {
			parts = append(parts,
				fmt.Sprintf("tests=%d", summary.RelatedTestCount),
				"link="+s.coverageBadge(coveragePercent(summary.TestLinkedSymbolCount, summary.RelevantSymbolCount)),
			)
		}
	}
	return strings.Join(parts, "  ")
}

func shellHumanSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%dB", size)
	}

	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	suffixes := []string{"KB", "MB", "GB", "TB"}
	return fmt.Sprintf("%.1f%s", float64(size)/float64(div), suffixes[exp])
}

func coveragePercent(linked, relevant int) int {
	if relevant <= 0 {
		return -1
	}
	return (linked * 100) / relevant
}

func (s *shellSession) coverageBadge(pct int) string {
	if pct < 0 {
		return s.palette.muted("n/a")
	}
	switch {
	case pct >= 70:
		return s.palette.wrap("1;32", fmt.Sprintf("%d%%", pct))
	case pct >= 35:
		return s.palette.wrap("1;33", fmt.Sprintf("%d%%", pct))
	default:
		return s.palette.wrap("1;31", fmt.Sprintf("%d%%", pct))
	}
}
