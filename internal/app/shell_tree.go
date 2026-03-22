package app

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/filter"
	"github.com/vladimirkasterin/ctx/internal/model"
	"github.com/vladimirkasterin/ctx/internal/storage"
	"github.com/vladimirkasterin/ctx/internal/text"
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
	Active    bool
	IsTest    bool
	FileIndex int
	Scanned   shellScannedFile
	Summary   storage.FileSummary
}

type treeAggregate struct {
	TotalFiles            int
	GoFiles               int
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
	page := s.treePage
	if s.currentMode != "tree" {
		page = 0
	}

	if len(args) > 0 {
		switch strings.ToLower(strings.TrimSpace(args[0])) {
		case "next", "more":
			page++
		case "prev", "back":
			if page > 0 {
				page--
			}
		case "root":
			page = 0
		case "page":
			if len(args) < 2 {
				return s.printShellError(fmt.Errorf("Usage: tree page <n>"))
			}
			value, err := strconv.Atoi(strings.TrimSpace(args[1]))
			if err != nil || value < 1 {
				return s.printShellError(fmt.Errorf("Invalid tree page %q", args[1]))
			}
			page = value - 1
		default:
			value, err := strconv.Atoi(strings.TrimSpace(args[0]))
			if err != nil || value < 1 {
				return s.printShellError(fmt.Errorf("Usage: tree [next|prev|page <n>|root]"))
			}
			page = value - 1
		}
	}
	return s.showTreePage(page)
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
	fileSummaries, err := s.store.LoadFileSummaries()
	if err != nil {
		return err
	}
	allLines := buildShellTreeLines(treeRoot, scannedFiles, fileSummaries, s.currentFile)
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

	report, err := s.store.LoadReportView(6)
	if err != nil {
		return err
	}
	aggregate := aggregateTree(scannedFiles, fileSummaries)

	s.currentMode = "tree"
	s.lastTargets = s.lastTargets[:0]
	s.treeTargets = s.treeTargets[:0]
	for _, line := range allLines {
		if line.IsDir {
			continue
		}
		s.treeTargets = append(s.treeTargets, shellTarget{
			Kind:     "file",
			Label:    line.Path,
			FilePath: line.Path,
			Line:     1,
		})
	}
	s.lastTargets = append(s.lastTargets, s.treeTargets...)

	if err := s.beginScreen("Project Tree"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		s.stdout,
		"%s\n  %s dirs=%d  files=%d  go=%d  test_files=%d  lines=%d  size=%s\n  %s indexed_files=%d  funcs=%d  methods=%d  structs=%d  tests=%d  test-link=%s\n  %s files are page-numbered, %s rows are test files, and the right column is copy-friendly\n\n",
		s.palette.section("Project Structure"),
		s.palette.label("Disk view:"),
		len(directories),
		aggregate.TotalFiles,
		aggregate.GoFiles,
		aggregate.TestFiles,
		aggregate.TotalLines,
		shellHumanSize(aggregate.TotalBytes),
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
			if _, err := fmt.Fprintf(s.stdout, "%s %s\n", prefix, s.palette.section(line.Text)); err != nil {
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

	indexByPath := make(map[string]int, len(allLines))
	for _, line := range allLines {
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
			}
		}
		if _, err := fmt.Fprintln(s.stdout); err != nil {
			return err
		}
	}

	_, err = fmt.Fprintf(
		s.stdout,
		"%s\n  %s use %s or %s to jump into a numbered file\n  %s use %s, %s, or %s to explore the full tree without truncation\n  %s use %s to paste the exact path from the right column\n\n",
		s.palette.section("Workflow"),
		s.palette.label("Open:"),
		s.palette.accent("open <n>"),
		s.palette.accent("file <n>"),
		s.palette.label("Explore:"),
		s.palette.accent("tree next"),
		s.palette.accent("tree prev"),
		s.palette.accent("tree page <n>"),
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
		appendTarget(shellTarget{
			Kind:      "file",
			Label:     s.currentFile,
			FilePath:  s.currentFile,
			Line:      1,
			SymbolKey: s.currentKey,
		})
	}
	for _, target := range s.landingHotFiles(report, 5) {
		appendTarget(target)
	}
	return targets
}

func scanProjectTree(root string) ([]string, []shellScannedFile, error) {
	directories := make([]string, 0, 64)
	files := make([]shellScannedFile, 0, 128)

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("make relative path: %w", err)
		}
		if relPath == "." {
			return nil
		}

		relPath = filepath.ToSlash(relPath)
		name := entry.Name()
		if entry.IsDir() {
			switch name {
			case ".git", "vendor", "node_modules":
				return filepath.SkipDir
			}
			if strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			directories = append(directories, relPath)
			return nil
		}

		if strings.HasPrefix(name, ".") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("read file info: %w", err)
		}

		lineCount := 0
		data, err := os.ReadFile(path)
		if err == nil && !filter.IsLikelyBinary(data) {
			lineCount, _ = text.CountLines(text.NormalizeNewlines(string(data)))
		}

		files = append(files, shellScannedFile{
			Path:      relPath,
			SizeBytes: info.Size(),
			LineCount: lineCount,
			IsTest:    strings.HasSuffix(name, "_test.go"),
		})
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return directories, files, nil
}

func buildShellTreeLines(root *projecttree.Node, scannedFiles []shellScannedFile, summaries map[string]storage.FileSummary, currentFile string) []shellTreeLine {
	fileByPath := make(map[string]shellScannedFile, len(scannedFiles))
	for _, file := range scannedFiles {
		fileByPath[file.Path] = file
	}

	lines := []shellTreeLine{{
		Text:  root.Name + "/",
		Path:  "",
		IsDir: true,
	}}
	fileIndex := 0
	for idx, child := range root.Children {
		appendShellTreeNode(&lines, child, "", idx == len(root.Children)-1, child.Name, currentFile, fileByPath, summaries, &fileIndex)
	}
	return lines
}

func appendShellTreeNode(lines *[]shellTreeLine, node *projecttree.Node, prefix string, last bool, relPath, currentFile string, fileByPath map[string]shellScannedFile, summaries map[string]storage.FileSummary, fileIndex *int) {
	branch := "|-- "
	nextPrefix := prefix + "|   "
	if last {
		branch = "`-- "
		nextPrefix = prefix + "    "
	}

	line := shellTreeLine{
		Text:   prefix + branch + node.Name,
		Path:   filepath.ToSlash(relPath),
		IsDir:  node.IsDir,
		Active: filepath.ToSlash(relPath) == filepath.ToSlash(currentFile),
	}
	if node.IsDir {
		line.Text += "/"
	} else {
		line.Scanned = fileByPath[line.Path]
		line.Summary = summaries[line.Path]
		line.IsTest = line.Scanned.IsTest || line.Summary.IsTest
		*fileIndex = *fileIndex + 1
		line.FileIndex = *fileIndex
	}
	*lines = append(*lines, line)

	for idx, child := range node.Children {
		childPath := filepath.ToSlash(filepath.Join(relPath, child.Name))
		appendShellTreeNode(lines, child, nextPrefix, idx == len(node.Children)-1, childPath, currentFile, fileByPath, summaries, fileIndex)
	}
}

func aggregateTree(scannedFiles []shellScannedFile, summaries map[string]storage.FileSummary) treeAggregate {
	var aggregate treeAggregate
	aggregate.TotalFiles = len(scannedFiles)
	for _, file := range scannedFiles {
		aggregate.TotalBytes += file.SizeBytes
		aggregate.TotalLines += file.LineCount
		if strings.HasSuffix(file.Path, ".go") {
			aggregate.GoFiles++
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
	case strings.HasSuffix(path, ".go"):
		return "[" + s.palette.wrap("1;36", "GO") + "]"
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
			fmt.Sprintf("st=%d", summary.StructCount),
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
