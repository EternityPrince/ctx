package app

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/codebase"
	"github.com/vladimirkasterin/ctx/internal/storage"
	projecttree "github.com/vladimirkasterin/ctx/internal/tree"
)

type shellDirectorySummary struct {
	Path           string
	Directories    int
	Files          int
	SourceFiles    int
	TestFiles      int
	IndexedFiles   int
	TotalLines     int
	TotalBytes     int64
	ExtensionCount map[string]int
}

type shellDirectoryLine struct {
	Text     string
	Path     string
	Active   bool
	DirIndex int
	Summary  shellDirectorySummary
}

func (s *shellSession) showTreeDirsPage(page int) error {
	directories, scannedFiles, err := scanProjectTree(s.info.Root)
	if err != nil {
		return err
	}

	treeRoot := projecttree.Build(s.info.Root, directories, nil)
	scopeNode, err := treeNodeForScope(treeRoot, s.treeScope)
	if err != nil {
		return s.printShellError(err)
	}

	fileSummaries, err := s.store.LoadFileSummaries()
	if err != nil {
		return err
	}
	dirSummaries := buildDirectorySummaries(directories, scannedFiles, fileSummaries)
	allLines := buildShellDirectoryLines(scopeNode, normalizeTreeScope(s.treeScope), dirSummaries)
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

	s.currentMode = "tree"
	s.lastTargets = s.lastTargets[:0]
	for idx := range visible {
		if visible[idx].Path == normalizeTreeScope(s.treeScope) {
			continue
		}
		visible[idx].DirIndex = len(s.lastTargets) + 1
		s.lastTargets = append(s.lastTargets, shellTarget{
			Kind:     "dir",
			Label:    visible[idx].Path,
			FilePath: visible[idx].Path,
		})
	}

	if err := s.beginScreen("Directory Overview"); err != nil {
		return err
	}

	scopeSummary := dirSummaries[normalizeTreeScope(s.treeScope)]
	if _, err := fmt.Fprintf(
		s.stdout,
		"%s\n  %s %s  %s dirs=%d  files=%d  source=%d  test_files=%d\n  %s indexed_files=%d  total_lines=%d  footprint=%s\n  %s %s\n\n",
		s.palette.section("Directory Map"),
		s.palette.label("Scope:"),
		treeScopeLabel(s.treeScope),
		s.palette.label("Inventory:"),
		scopeSummary.Directories,
		scopeSummary.Files,
		scopeSummary.SourceFiles,
		scopeSummary.TestFiles,
		s.palette.label("Code map:"),
		scopeSummary.IndexedFiles,
		scopeSummary.TotalLines,
		shellHumanSize(scopeSummary.TotalBytes),
		s.palette.label("Extensions:"),
		extensionSummary(scopeSummary.ExtensionCount, 5),
	); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(
		s.stdout,
		"%s\n  %s page=%d/%d  lines=%d-%d/%d\n\n",
		s.palette.section("Directories"),
		s.palette.label("Window:"),
		page+1,
		totalPages,
		start+1,
		end,
		len(allLines),
	); err != nil {
		return err
	}

	hierWidth, pathWidth := treeDirectoryColumnWidths(visible)
	for _, line := range visible {
		prefix := "  "
		if line.Active {
			prefix = "=>"
		}

		hierarchy := fmt.Sprintf("%-*s", hierWidth, line.Text)
		if line.Active {
			hierarchy = s.palette.accent(hierarchy)
		}

		label := "     "
		if line.DirIndex > 0 {
			label = fmt.Sprintf("[%3d]", line.DirIndex)
		}
		pathText := fmt.Sprintf("%-*s", pathWidth, treeScopeLabel(line.Path))
		metrics := fmt.Sprintf(
			"dirs=%d  files=%d  src=%d  tests=%d  %dL  %s  ||  %s",
			line.Summary.Directories,
			line.Summary.Files,
			line.Summary.SourceFiles,
			line.Summary.TestFiles,
			line.Summary.TotalLines,
			shellHumanSize(line.Summary.TotalBytes),
			extensionSummary(line.Summary.ExtensionCount, 4),
		)
		if _, err := fmt.Fprintf(s.stdout, "%s %s  %s  ||  %s  ||  %s\n", prefix, hierarchy, label, pathText, metrics); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(s.stdout); err != nil {
		return err
	}

	_, err = fmt.Fprintf(
		s.stdout,
		"%s\n  %s use %s to zoom into a directory\n  %s use %s, %s, %s, or %s to move through directory scopes\n  %s use %s to switch back to files or %s for ranked files\n\n",
		s.palette.section("Workflow"),
		s.palette.label("Zoom:"),
		s.palette.accent("open <n>"),
		s.palette.label("Scope:"),
		s.palette.accent("tree up"),
		s.palette.accent("tree root"),
		s.palette.accent("tree next"),
		s.palette.accent("tree prev"),
		s.palette.label("Switch:"),
		s.palette.accent("tree"),
		s.palette.accent("tree hot"),
	)
	return err
}

func buildDirectorySummaries(directories []string, scannedFiles []shellScannedFile, fileSummaries map[string]storage.FileSummary) map[string]shellDirectorySummary {
	summaries := make(map[string]shellDirectorySummary)
	ensure := func(path string) shellDirectorySummary {
		path = normalizeTreeScope(path)
		if summary, ok := summaries[path]; ok {
			return summary
		}
		summary := shellDirectorySummary{
			Path:           path,
			ExtensionCount: make(map[string]int),
		}
		summaries[path] = summary
		return summary
	}
	store := func(summary shellDirectorySummary) {
		summaries[summary.Path] = summary
	}

	store(ensure(""))
	for _, dir := range directories {
		store(ensure(dir))
		parent := treeParentScope(dir)
		for {
			summary := ensure(parent)
			summary.Directories++
			store(summary)
			if parent == "" {
				break
			}
			parent = treeParentScope(parent)
		}
	}

	for _, file := range scannedFiles {
		dir := normalizeTreeScope(filepath.ToSlash(filepath.Dir(file.Path)))
		if dir == "." {
			dir = ""
		}
		ext := extensionLabel(file.Path)
		indexed := codebase.IsIndexedSourceFile(file.Path)
		if _, ok := fileSummaries[file.Path]; ok {
			indexed = true
		}
		for {
			summary := ensure(dir)
			summary.Files++
			summary.TotalLines += file.LineCount
			summary.TotalBytes += file.SizeBytes
			if codebase.IsIndexedSourceFile(file.Path) {
				summary.SourceFiles++
			}
			if file.IsTest {
				summary.TestFiles++
			}
			if indexed {
				summary.IndexedFiles++
			}
			summary.ExtensionCount[ext]++
			store(summary)
			if dir == "" {
				break
			}
			dir = treeParentScope(dir)
		}
	}

	return summaries
}

func buildShellDirectoryLines(root *projecttree.Node, scope string, summaries map[string]shellDirectorySummary) []shellDirectoryLine {
	lines := []shellDirectoryLine{{
		Text:    root.Name + "/",
		Path:    normalizeTreeScope(scope),
		Active:  true,
		Summary: summaries[normalizeTreeScope(scope)],
	}}
	for idx, child := range root.Children {
		if !child.IsDir {
			continue
		}
		appendShellDirectoryNode(&lines, child, "", idx == len(root.Children)-1, joinTreePath(scope, child.Name), summaries)
	}
	return lines
}

func appendShellDirectoryNode(lines *[]shellDirectoryLine, node *projecttree.Node, prefix string, last bool, relPath string, summaries map[string]shellDirectorySummary) {
	branch := "|-- "
	nextPrefix := prefix + "|   "
	if last {
		branch = "`-- "
		nextPrefix = prefix + "    "
	}

	line := shellDirectoryLine{
		Text:    prefix + branch + node.Name + "/",
		Path:    normalizeTreeScope(relPath),
		Summary: summaries[normalizeTreeScope(relPath)],
	}
	*lines = append(*lines, line)

	for idx, child := range node.Children {
		if !child.IsDir {
			continue
		}
		appendShellDirectoryNode(lines, child, nextPrefix, idx == len(node.Children)-1, joinTreePath(relPath, child.Name), summaries)
	}
}

func treeDirectoryColumnWidths(lines []shellDirectoryLine) (int, int) {
	hierWidth := 0
	pathWidth := 0
	for _, line := range lines {
		hierWidth = max(hierWidth, len(line.Text))
		pathWidth = max(pathWidth, len(treeScopeLabel(line.Path)))
	}
	return hierWidth, pathWidth
}

func extensionLabel(path string) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	if ext == "" {
		return "none"
	}
	return ext
}

func extensionSummary(values map[string]int, limit int) string {
	if len(values) == 0 {
		return "empty"
	}
	type item struct {
		Name  string
		Count int
	}
	items := make([]item, 0, len(values))
	for name, count := range values {
		items = append(items, item{Name: name, Count: count})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count != items[j].Count {
			return items[i].Count > items[j].Count
		}
		return items[i].Name < items[j].Name
	})

	if limit <= 0 || limit > len(items) {
		limit = len(items)
	}
	parts := make([]string, 0, limit+1)
	for _, item := range items[:limit] {
		parts = append(parts, fmt.Sprintf("%s=%d", item.Name, item.Count))
	}
	if extra := len(items) - limit; extra > 0 {
		parts = append(parts, fmt.Sprintf("+%d more", extra))
	}
	return strings.Join(parts, "  ")
}
