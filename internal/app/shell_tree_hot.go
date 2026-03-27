package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/storage"
)

type shellHotFile struct {
	Path      string
	Line      int
	Score     int
	SymbolKey string
	Symbols   []string
}

func (s *shellSession) showTreeHotPage(page int) error {
	report, err := s.store.LoadReportView(32)
	if err != nil {
		return err
	}
	hotFiles := rankShellHotFiles(report, s.treeScope)
	totalPages := max(1, (len(hotFiles)+shellTreePageSize-1)/shellTreePageSize)
	if page < 0 {
		page = 0
	}
	if page >= totalPages {
		page = totalPages - 1
	}
	s.treePage = page

	start := page * shellTreePageSize
	end := min(len(hotFiles), start+shellTreePageSize)
	visible := hotFiles[start:end]

	directories, scannedFiles, err := scanProjectTree(s.info.Root)
	if err != nil {
		return err
	}
	_ = directories
	fileByPath := make(map[string]shellScannedFile, len(scannedFiles))
	for _, file := range scannedFiles {
		fileByPath[file.Path] = file
	}
	fileSummaries, err := s.store.LoadFileSummaries()
	if err != nil {
		return err
	}

	s.currentMode = "tree"
	s.lastTargets = s.lastTargets[:0]
	for _, item := range visible {
		s.lastTargets = append(s.lastTargets, shellTarget{
			Kind:      "file",
			Label:     item.Path,
			FilePath:  item.Path,
			Line:      item.Line,
			SymbolKey: item.SymbolKey,
		})
	}

	if err := s.beginScreen("Hot Files"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		s.stdout,
		"%s\n  %s %s  %s files=%d\n  %s rank is built from graph signals, recent change proximity, and entrypoint heuristics\n  %s compact risk flags surface hotspots and recent weakly linked areas\n\n",
		s.palette.section("Hot Paths"),
		s.palette.label("Scope:"),
		treeScopeLabel(s.treeScope),
		s.palette.label("Candidates:"),
		len(hotFiles),
		s.palette.label("Signal:"),
		s.palette.label("Risk:"),
	); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		s.stdout,
		"%s\n  %s page=%d/%d  lines=%d-%d/%d\n\n",
		s.palette.section("Hot Window"),
		s.palette.label("Window:"),
		page+1,
		totalPages,
		start+1,
		end,
		len(hotFiles),
	); err != nil {
		return err
	}

	pathWidth := 0
	for _, item := range visible {
		pathWidth = max(pathWidth, len(item.Path))
	}
	for idx, item := range visible {
		file := fileByPath[item.Path]
		summary := fileSummaries[item.Path]
		label := fmt.Sprintf("[%3d] %s", idx+1, s.fileBadge(item.Path, file.IsTest || summary.IsTest))
		pathText := fmt.Sprintf("%-*s", pathWidth, item.Path)
		hotScore, recentChanged, err := s.fileRiskSignals(item.Path, item.Score)
		if err != nil {
			return err
		}
		detail := fmt.Sprintf("score=%d  symbols=%s  risk=%s", item.Score, strings.Join(item.Symbols, ", "), fileRiskSummary(summary, hotScore, recentChanged))
		metrics := s.renderTreeFileMetrics(file, summary)
		if _, err := fmt.Fprintf(s.stdout, "  %s  %s  ||  %s  ||  %s\n", label, pathText, detail, metrics); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(s.stdout); err != nil {
		return err
	}

	_, err = fmt.Fprintf(
		s.stdout,
		"%s\n  %s use %s or %s to open a ranked file\n  %s use %s to switch back to the full tree or %s for directory-first navigation\n\n",
		s.palette.section("Workflow"),
		s.palette.label("Open:"),
		s.palette.accent("open <n>"),
		s.palette.accent("file <n>"),
		s.palette.label("Switch:"),
		s.palette.accent("tree"),
		s.palette.accent("tree dirs"),
	)
	return err
}

func rankShellHotFiles(view storage.ReportView, scope string) []shellHotFile {
	files := make([]shellHotFile, 0, len(view.TopFiles))
	for _, item := range view.TopFiles {
		if !pathInTreeScope(item.Summary.FilePath, scope) {
			continue
		}
		files = append(files, shellHotFile{
			Path:      item.Summary.FilePath,
			Line:      item.PrimaryLine,
			Score:     item.Score,
			SymbolKey: item.PrimarySymbolKey,
			Symbols:   append([]string(nil), item.TopSymbols...),
		})
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].Score != files[j].Score {
			return files[i].Score > files[j].Score
		}
		return files[i].Path < files[j].Path
	})
	return files
}
