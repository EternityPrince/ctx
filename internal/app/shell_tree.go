package app

import (
	"fmt"
	"path/filepath"

	"github.com/vladimirkasterin/ctx/internal/model"
	"github.com/vladimirkasterin/ctx/internal/storage"
	projecttree "github.com/vladimirkasterin/ctx/internal/tree"
)

const shellTreePageSize = 28

type shellScannedFile struct {
	Path      string
	SizeBytes int64
	LineCount int
	IsTest    bool
}

type shellTreeLine struct {
	Text      string
	Path      string
	IsDir     bool
	DirLines  int
	Active    bool
	IsTest    bool
	FileIndex int
	Scanned   shellScannedFile
	Summary   storage.FileSummary
}

type treeAggregate struct {
	TotalFiles            int
	SourceFiles           int
	TestFiles             int
	IndexedFiles          int
	TotalLines            int
	TotalBytes            int64
	FuncCount             int
	MethodCount           int
	StructCount           int
	DeclaredTestCount     int
	RelatedTestCount      int
	RelevantSymbolCount   int
	TestLinkedSymbolCount int
}

func (s *shellSession) showTree() error {
	return s.showTreeCommand(nil)
}

func (s *shellSession) showTreeCommand(args []string) error {
	mode, scope, page, err := s.parseTreeCommand(args)
	if err != nil {
		return s.printShellError(err)
	}

	s.currentMode = "tree"
	s.treeMode = mode
	s.treeScope = scope
	s.treePage = page

	switch mode {
	case shellTreeModeDirs:
		return s.showTreeDirsPage(page)
	case shellTreeModeHot:
		return s.showTreeHotPage(page)
	default:
		return s.showTreePage(page)
	}
}

func (s *shellSession) showTreePage(page int) error {
	directories, scannedFiles, err := scanProjectTree(s.info.Root)
	if err != nil {
		return err
	}

	fileNodes := make([]model.File, 0, len(scannedFiles))
	for _, file := range scannedFiles {
		fileNodes = append(fileNodes, model.File{
			Name:         filepath.Base(file.Path),
			RelativePath: file.Path,
			SizeBytes:    file.SizeBytes,
			LineCount:    file.LineCount,
		})
	}

	treeRoot := projecttree.Build(s.info.Root, directories, fileNodes)
	scopeNode, err := treeNodeForScope(treeRoot, s.treeScope)
	if err != nil {
		return s.printShellError(err)
	}
	fileSummaries, err := s.store.LoadFileSummaries()
	if err != nil {
		return err
	}
	dirLines := directoryLineTotals(scannedFiles)
	allLines := buildShellTreeLines(scopeNode, normalizeTreeScope(s.treeScope), scannedFiles, fileSummaries, dirLines, s.currentFile)
	totalPages := max(1, (len(allLines)+shellTreePageSize-1)/shellTreePageSize)
	if page < 0 {
		page = 0
	}
	if page >= totalPages {
		page = totalPages - 1
	}
	s.treePage = page

	start := page * shellTreePageSize
	end := min(len(allLines), start+shellTreePageSize)
	visible := allLines[start:end]

	report, err := s.store.LoadReportView(12)
	if err != nil {
		return err
	}
	aggregate := aggregateTree(filterTreeFilesByScope(scannedFiles, s.treeScope), fileSummaries)

	s.lastTargets = s.lastTargets[:0]
	for idx := range visible {
		if visible[idx].IsDir {
			continue
		}
		visible[idx].FileIndex = len(s.lastTargets) + 1
		s.lastTargets = append(s.lastTargets, shellTarget{
			Kind:     "file",
			Label:    visible[idx].Path,
			FilePath: visible[idx].Path,
			Line:     1,
		})
	}

	if err := s.beginScreen("Project Tree"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		s.stdout,
		"%s\n  %s %s  %s %d  %s %s\n  %s dirs=%d  files=%d  source=%d  test_files=%d\n  %s indexed_files=%d  funcs=%d  methods=%d  types=%d  tests=%d  test-link=%s\n  %s numbering is local to the current window, %s rows are test files, and the right column is copy-friendly\n\n",
		s.palette.section("Project Structure"),
		s.palette.label("Scope:"),
		treeScopeLabel(s.treeScope),
		s.palette.label("Total lines:"),
		aggregate.TotalLines,
		s.palette.label("Footprint:"),
		shellHumanSize(aggregate.TotalBytes),
		s.palette.label("Disk view:"),
		len(directories),
		aggregate.TotalFiles,
		aggregate.SourceFiles,
		aggregate.TestFiles,
		s.palette.label("Code map:"),
		aggregate.IndexedFiles,
		aggregate.FuncCount,
		aggregate.MethodCount,
		aggregate.StructCount,
		aggregate.DeclaredTestCount,
		s.coverageBadge(coveragePercent(aggregate.TestLinkedSymbolCount, aggregate.RelevantSymbolCount)),
		s.palette.label("Read:"),
		s.palette.kindBadge("test"),
	); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(
		s.stdout,
		"%s\n  %s page=%d/%d  lines=%d-%d/%d\n\n",
		s.palette.section("Tree"),
		s.palette.label("Window:"),
		page+1,
		totalPages,
		start+1,
		end,
		len(allLines),
	); err != nil {
		return err
	}

	hierWidth, pathWidth := treeColumnWidths(visible)
	for _, line := range visible {
		prefix := "  "
		if line.Active {
			prefix = "=>"
		}

		if line.IsDir {
			dirText := s.palette.section(line.Text)
			if line.DirLines > 0 {
				dirText = fmt.Sprintf("%s %s", dirText, s.palette.muted(fmt.Sprintf("(%dL)", line.DirLines)))
			}
			if _, err := fmt.Fprintf(s.stdout, "%s %s\n", prefix, dirText); err != nil {
				return err
			}
			continue
		}

		hierarchy := fmt.Sprintf("%-*s", hierWidth, line.Text)
		if line.Active {
			hierarchy = s.palette.accent(hierarchy)
		}
		label := fmt.Sprintf("[%3d] %s", line.FileIndex, s.fileBadge(line.Path, line.IsTest))
		pathText := fmt.Sprintf("%-*s", pathWidth, line.Path)
		metrics := s.renderTreeFileMetrics(line.Scanned, line.Summary)
		if _, err := fmt.Fprintf(s.stdout, "%s %s  %s  ||  %s  ||  %s\n", prefix, hierarchy, label, pathText, metrics); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(s.stdout); err != nil {
		return err
	}

	indexByPath := make(map[string]int, len(visible))
	for _, line := range visible {
		if line.IsDir {
			continue
		}
		indexByPath[line.Path] = line.FileIndex
	}
	recommended := s.treeQuickTargets(report)
	if len(recommended) > 0 {
		if _, err := fmt.Fprintf(s.stdout, "%s\n", s.palette.section("Recommended Hops")); err != nil {
			return err
		}
		for _, target := range recommended {
			if number, ok := indexByPath[target.FilePath]; ok {
				if _, err := fmt.Fprintf(s.stdout, "  [%d] %s\n", number, target.FilePath); err != nil {
					return err
				}
				continue
			}
			if _, err := fmt.Fprintf(s.stdout, "  file %s\n", target.FilePath); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(s.stdout); err != nil {
			return err
		}
	}

	_, err = fmt.Fprintf(
		s.stdout,
		"%s\n  %s use %s or %s to jump into a numbered file\n  %s use %s to choose a directory first or %s to see ranked files\n  %s use %s, %s, %s, or %s to move through the current scope\n  %s use %s to paste the exact path from the right column\n\n",
		s.palette.section("Workflow"),
		s.palette.label("Open:"),
		s.palette.accent("open <n>"),
		s.palette.accent("file <n>"),
		s.palette.label("Navigate:"),
		s.palette.accent("tree dirs"),
		s.palette.accent("tree hot"),
		s.palette.label("Explore:"),
		s.palette.accent("tree next"),
		s.palette.accent("tree prev"),
		s.palette.accent("tree page <n>"),
		s.palette.accent("tree up"),
		s.palette.label("Direct jump:"),
		s.palette.accent("file <path>"),
	)
	return err
}

func (s *shellSession) treeQuickTargets(report storage.ReportView) []shellTarget {
	seen := make(map[string]struct{})
	targets := make([]shellTarget, 0, 6)
	appendTarget := func(target shellTarget) {
		if target.FilePath == "" {
			return
		}
		if _, ok := seen[target.FilePath]; ok {
			return
		}
		seen[target.FilePath] = struct{}{}
		targets = append(targets, target)
	}

	if s.currentFile != "" {
		if pathInTreeScope(s.currentFile, s.treeScope) {
			appendTarget(shellTarget{
				Kind:      "file",
				Label:     s.currentFile,
				FilePath:  s.currentFile,
				Line:      1,
				SymbolKey: s.currentKey,
			})
		}
	}
	for _, target := range s.landingHotFiles(report, 5) {
		if pathInTreeScope(target.FilePath, s.treeScope) {
			appendTarget(target)
		}
	}
	return targets
}
