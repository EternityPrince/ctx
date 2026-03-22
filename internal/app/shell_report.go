package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/storage"
)

type fileReportEntry struct {
	View   storage.SymbolView
	Score  int
	Impact string
}

func (s *shellSession) showContextReport(args []string) error {
	if len(args) == 0 {
		switch s.currentMode {
		case "symbol":
			return s.showEntityReport("")
		case "walk":
			return s.showEntityReport("")
		case "file":
			return s.showFileReport("")
		default:
			return s.showReport()
		}
	}

	scope := strings.ToLower(args[0])
	rest := strings.TrimSpace(strings.Join(args[1:], " "))
	switch scope {
	case "project", "root":
		return s.showReport()
	case "entity", "symbol":
		return s.showEntityReport(rest)
	case "file":
		return s.showFileReport(rest)
	default:
		return s.showEntityReport(strings.Join(args, " "))
	}
}

func (s *shellSession) showEntityReport(query string) error {
	symbolKey := s.currentKey
	if strings.TrimSpace(query) != "" {
		matches, err := s.store.FindSymbols(query)
		if err != nil {
			return err
		}
		if len(matches) == 0 {
			return s.printShellError(fmt.Errorf("No symbol matches for %q", query))
		}
		if len(matches) > 1 {
			return s.showSymbol(query, false)
		}
		if matches[0].SymbolKey != s.currentKey {
			s.pushHistory(matches[0].SymbolKey)
		}
		symbolKey = matches[0].SymbolKey
	}
	if symbolKey == "" {
		return s.printShellError(fmt.Errorf("No current symbol. Open a symbol first or use `report entity <name>`."))
	}

	view, err := s.store.LoadSymbolView(symbolKey)
	if err != nil {
		return err
	}

	s.currentKey = symbolKey
	s.currentQName = view.Symbol.QName
	s.currentMode = "symbol"
	s.currentFile = view.Symbol.FilePath

	if err := s.beginScreen("Entity Report"); err != nil {
		return err
	}
	if err := s.renderEntityReport(view); err != nil {
		return err
	}
	return s.showAutoDriveForView(view)
}

func (s *shellSession) renderEntityReport(view storage.SymbolView) error {
	if err := s.writeCurrentSymbolSummary(view); err != nil {
		return err
	}

	localDeps := strings.Join(shortenValues(s.info.ModulePath, limitStrings(view.Package.LocalDeps, 5)), ", ")
	if localDeps == "" {
		localDeps = s.palette.muted("none indexed")
	}
	reverseDeps := strings.Join(shortenValues(s.info.ModulePath, limitStrings(view.Package.ReverseDeps, 5)), ", ")
	if reverseDeps == "" {
		reverseDeps = s.palette.muted("none indexed")
	}

	if _, err := fmt.Fprintf(
		s.stdout,
		"%s\n  %s %s\n  %s %s\n  %s %s\n  %s callers=%d  callees=%d  refs_in=%d  refs_out=%d  tests=%d  coverage=%s\n  %s local_deps=%d  reverse_deps=%d\n",
		s.palette.section("Contract"),
		s.palette.label("Package:"),
		shortenQName(s.info.ModulePath, view.Symbol.PackageImportPath),
		s.palette.label("Declared:"),
		symbolRangeDisplay(s.info.Root, view.Symbol),
		s.palette.label("Kind / Receiver:"),
		s.describeSymbolKind(view.Symbol),
		s.palette.label("Signals:"),
		len(view.Callers),
		len(view.Callees),
		len(view.ReferencesIn),
		len(view.ReferencesOut),
		len(view.Tests),
		s.palette.muted("n/a"),
		s.palette.label("Reach:"),
		len(view.Package.LocalDeps),
		len(view.Package.ReverseDeps),
	); err != nil {
		return err
	}
	if doc := oneLine(view.Symbol.Doc); doc != "" {
		if _, err := fmt.Fprintf(s.stdout, "  %s %s\n", s.palette.label("Doc:"), doc); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(
		s.stdout,
		"  %s %s\n  %s %s\n\n",
		s.palette.label("Local deps:"),
		localDeps,
		s.palette.label("Reverse deps:"),
		reverseDeps,
	); err != nil {
		return err
	}

	if err := s.renderRelatedPreview("Top Callers", view.Callers, 3); err != nil {
		return err
	}
	if err := s.renderRelatedPreview("Top Callees", view.Callees, 4); err != nil {
		return err
	}
	if err := s.renderTestsPreview("Top Tests", view.Tests, 3); err != nil {
		return err
	}
	if err := s.renderSiblingPreview("In This File", view.Siblings, 4); err != nil {
		return err
	}

	preview, err := renderSymbolSource(s.info.Root, s.batPath, view.Symbol, 16, s.palette.enabled)
	if err != nil {
		return err
	}
	if preview != "" {
		if _, err := fmt.Fprintf(s.stdout, "%s\n%s\n\n", s.palette.section("Contract Body"), preview); err != nil {
			return err
		}
	}
	return nil
}

func (s *shellSession) showFileReport(query string) error {
	relPath, focusSymbolKey, err := s.resolveFileQuery(query)
	if err != nil {
		return s.printShellError(err)
	}
	isDir, err := s.isDirectoryQuery(relPath)
	if err != nil {
		return err
	}
	if isDir {
		if relPath == "." || relPath == "" {
			return s.showTree()
		}
		return s.printShellError(fmt.Errorf("%s is a directory. Use `tree` to explore directories and `report file <path>` for files.", relPath))
	}

	entries, err := s.loadFileReportEntries(relPath)
	if err != nil {
		return err
	}
	summaries, err := s.store.LoadFileSummaries()
	if err != nil {
		return err
	}
	summary := summaries[relPath]

	s.currentMode = "file"
	s.currentFile = relPath
	if focusSymbolKey == "" {
		focusSymbolKey = s.currentKey
	}

	if err := s.beginScreen("File Report"); err != nil {
		return err
	}
	return s.renderFileReport(relPath, focusSymbolKey, entries, summary)
}

func (s *shellSession) loadFileReportEntries(relPath string) ([]fileReportEntry, error) {
	symbols, err := s.store.LoadFileSymbols(relPath)
	if err != nil {
		return nil, err
	}

	entries := make([]fileReportEntry, 0, len(symbols))
	for _, symbol := range symbols {
		view, err := s.store.LoadSymbolView(symbol.SymbolKey)
		if err != nil {
			return nil, err
		}
		score := len(view.Callers)*5 + len(view.Callees) + len(view.ReferencesIn)*2 + len(view.Tests)*3 + len(view.Package.ReverseDeps)*2
		entries = append(entries, fileReportEntry{
			View:   view,
			Score:  score,
			Impact: impactLabel(len(view.Callers), len(view.ReferencesIn), len(view.Tests), len(view.Package.ReverseDeps)),
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Score != entries[j].Score {
			return entries[i].Score > entries[j].Score
		}
		if entries[i].View.Symbol.Line != entries[j].View.Symbol.Line {
			return entries[i].View.Symbol.Line < entries[j].View.Symbol.Line
		}
		return entries[i].View.Symbol.QName < entries[j].View.Symbol.QName
	})
	return entries, nil
}

func (s *shellSession) renderFileReport(relPath, focusSymbolKey string, entries []fileReportEntry, summary storage.FileSummary) error {
	s.lastTargets = s.lastTargets[:0]
	for _, entry := range entries {
		s.lastTargets = append(s.lastTargets, shellTarget{
			Kind:      "symbol",
			SymbolKey: entry.View.Symbol.SymbolKey,
			Label:     shortenQName(s.info.ModulePath, entry.View.Symbol.QName),
			FilePath:  entry.View.Symbol.FilePath,
			Line:      entry.View.Symbol.Line,
		})
	}

	functions, methods, types, tests := 0, 0, 0, 0
	for _, entry := range entries {
		switch entry.View.Symbol.Kind {
		case "func":
			functions++
		case "method":
			methods++
		case "test", "benchmark", "fuzz":
			tests++
		default:
			types++
		}
	}

	if _, err := fmt.Fprintf(
		s.stdout,
		"%s\n  %s %s %s\n  %s symbols=%d  funcs=%d  methods=%d  types=%d  tests=%d  test-link=%s\n  %s ranked by callers + refs + tests + reverse deps\n\n",
		s.palette.section("File Report"),
		s.palette.label("File:"),
		relPath,
		s.fileBadge(relPath, summary.IsTest),
		s.palette.label("Inventory:"),
		len(entries),
		functions,
		methods,
		types,
		max(tests, summary.DeclaredTestCount),
		s.coverageBadge(coveragePercent(summary.TestLinkedSymbolCount, summary.RelevantSymbolCount)),
		s.palette.label("Importance:"),
	); err != nil {
		return err
	}

	if len(entries) == 0 {
		if _, err := fmt.Fprintf(s.stdout, "%s\n  %s\n\n", s.palette.section("Ranked Symbols"), s.palette.muted("No indexed symbols in this file")); err != nil {
			return err
		}
		return nil
	}

	if _, err := fmt.Fprintf(s.stdout, "%s (%d)\n", s.palette.section("Ranked Symbols"), len(entries)); err != nil {
		return err
	}
	for idx, entry := range entries[:min(shellListLimit, len(entries))] {
		marker := "  "
		if focusSymbolKey != "" && entry.View.Symbol.SymbolKey == focusSymbolKey {
			marker = "=>"
		}
		if _, err := fmt.Fprintf(
			s.stdout,
			"%s [%d] %s %s %s\n     %s\n     %s %s\n     %s score=%d  callers=%d  callees=%d  refs_in=%d  tests=%d  rdeps=%d\n",
			marker,
			idx+1,
			s.palette.kindBadge(entry.View.Symbol.Kind),
			entry.View.Symbol.Name,
			s.palette.badge(reportImportance(entry.Score)),
			styleHumanSignature(s.palette, displaySignature(entry.View.Symbol)),
			s.palette.label("declared:"),
			symbolRangeWithCountDisplay(s.info.Root, entry.View.Symbol),
			s.palette.label("signals:"),
			entry.Score,
			len(entry.View.Callers),
			len(entry.View.Callees),
			len(entry.View.ReferencesIn),
			len(entry.View.Tests),
			len(entry.View.Package.ReverseDeps),
		); err != nil {
			return err
		}
		if doc := oneLine(entry.View.Symbol.Doc); doc != "" {
			if _, err := fmt.Fprintf(s.stdout, "     %s %s\n", s.palette.label("doc:"), doc); err != nil {
				return err
			}
		}
		if idx+1 < min(shellListLimit, len(entries)) {
			if _, err := fmt.Fprintln(s.stdout); err != nil {
				return err
			}
		}
	}
	if len(entries) > shellListLimit {
		if _, err := fmt.Fprintf(s.stdout, "  %s and %d more\n", s.palette.muted("..."), len(entries)-shellListLimit); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(s.stdout); err != nil {
		return err
	}

	focus := entries[0]
	for _, entry := range entries {
		if focusSymbolKey != "" && entry.View.Symbol.SymbolKey == focusSymbolKey {
			focus = entry
			break
		}
	}

	preview, err := renderSymbolSource(s.info.Root, s.batPath, focus.View.Symbol, 18, s.palette.enabled)
	if err != nil {
		return err
	}
	if preview != "" {
		if _, err := fmt.Fprintf(s.stdout, "%s\n%s\n\n", s.palette.section("Focused Body"), preview); err != nil {
			return err
		}
	}

	_, err = fmt.Fprintf(
		s.stdout,
		"%s\n  %s use %s to jump into a symbol journey\n  %s use %s to peek a body from this list\n  %s use %s for the full body of the focused entity\n  %s use %s for the full project map or %s for the main menu\n\n",
		s.palette.section("Workflow"),
		s.palette.label("Next:"),
		s.palette.accent("open <n>"),
		s.palette.label("Peek:"),
		s.palette.accent("source <n>"),
		s.palette.label("Deep body:"),
		s.palette.accent("full <n>"),
		s.palette.label("Navigate:"),
		s.palette.accent("tree"),
		s.palette.accent("home"),
	)
	return err
}

func (s *shellSession) renderSiblingPreview(title string, symbols []storage.SymbolMatch, limit int) error {
	if _, err := fmt.Fprintf(s.stdout, "%s (%d)\n", s.palette.section(title), len(symbols)); err != nil {
		return err
	}
	if len(symbols) == 0 {
		_, err := fmt.Fprintf(s.stdout, "  %s\n\n", s.palette.muted("nothing nearby yet"))
		return err
	}
	for _, symbol := range symbols[:min(limit, len(symbols))] {
		if _, err := fmt.Fprintf(
			s.stdout,
			"  -> %s %s\n     %s\n     %s\n",
			s.palette.kindBadge(symbol.Kind),
			shortenQName(s.info.ModulePath, symbol.QName),
			styleHumanSignature(s.palette, displaySignature(symbol)),
			symbolRangeDisplay(s.info.Root, symbol),
		); err != nil {
			return err
		}
	}
	if len(symbols) > limit {
		if _, err := fmt.Fprintf(s.stdout, "  %s and %d more\n", s.palette.muted("..."), len(symbols)-limit); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(s.stdout)
	return err
}

func (s *shellSession) describeSymbolKind(symbol storage.SymbolMatch) string {
	if symbol.Receiver == "" {
		return symbol.Kind
	}
	return fmt.Sprintf("%s on %s", symbol.Kind, symbol.Receiver)
}
