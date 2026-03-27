package app

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/storage"
)

func (s *shellSession) renderSymbolJourney(view storage.SymbolView) error {
	if err := s.writeCurrentSymbolSummary(view); err != nil {
		return err
	}
	riskSummary, err := s.symbolJourneyRiskSummary(view)
	if err != nil {
		return err
	}
	testGuidance, err := buildSymbolTestGuidance(s.store, view, 6)
	if err != nil {
		return err
	}
	section := buildSymbolExplain(view, testGuidance)
	section.Facts = append([]explainFact{
		{Key: "Risk", Value: riskSummary},
		{Key: "Area", Value: fmt.Sprintf("local_deps=%d reverse_deps=%d file_symbols=%d", len(view.Package.LocalDeps), len(view.Package.ReverseDeps), len(view.Siblings)+1)},
	}, section.Facts...)
	if err := s.renderShellExplain(section); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(
		s.stdout,
		"%s\n  %s %s\n  %s %s\n  %s use %s to see the file card or %s to start a file walk immediately\n\n",
		s.palette.section("File Journey"),
		s.palette.label("File:"),
		view.Symbol.FilePath,
		s.palette.label("Package:"),
		shortenQName(s.info.ModulePath, view.Symbol.PackageImportPath),
		s.palette.label("Tip:"),
		s.palette.accent("file"),
		s.palette.accent("walk"),
	); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		s.stdout,
		"%s\n  %s use %s for a denser symbol report\n  %s use %s to step back to the project structure or %s for the main menu\n\n",
		s.palette.section("Research Moves"),
		s.palette.label("Deepen:"),
		s.palette.accent("report"),
		s.palette.label("Navigate:"),
		s.palette.accent("tree"),
		s.palette.accent("home"),
	); err != nil {
		return err
	}

	if err := s.renderRelatedPreview("Callers", view.Callers, 3); err != nil {
		return err
	}
	if err := s.renderRelatedPreview("Callees", view.Callees, 4); err != nil {
		return err
	}
	if err := s.renderTestsPreview("Tests To Read Before Change", testGuidance, 3); err != nil {
		return err
	}

	preview, err := renderSymbolSource(s.info.Root, s.batPath, view.Symbol, 18, s.palette.enabled)
	if err != nil {
		return err
	}
	if preview != "" {
		if _, err := fmt.Fprintf(s.stdout, "%s\n%s\n\n", s.palette.section("Body Preview"), preview); err != nil {
			return err
		}
	}
	return s.showAutoDriveForView(view)
}

func (s *shellSession) renderRelatedPreview(title string, values []storage.RelatedSymbolView, limit int) error {
	if _, err := fmt.Fprintf(s.stdout, "%s (%d)\n", s.palette.section(title), len(values)); err != nil {
		return err
	}
	if len(values) == 0 {
		_, err := fmt.Fprintf(s.stdout, "  %s\n\n", s.palette.muted("none yet"))
		return err
	}
	for _, value := range values[:min(limit, len(values))] {
		if _, err := fmt.Fprintf(
			s.stdout,
			"  -> %s %s\n     %s\n     use %s:%d\n",
			s.palette.kindBadge(value.Symbol.Kind),
			shortenQName(s.info.ModulePath, value.Symbol.QName),
			styleHumanSignature(s.palette, displaySignature(value.Symbol)),
			value.UseFilePath,
			value.UseLine,
		); err != nil {
			return err
		}
		if snippet := s.previewLine(value.UseFilePath, value.UseLine); snippet != "" {
			if _, err := fmt.Fprintf(s.stdout, "     %s\n", snippet); err != nil {
				return err
			}
		}
	}
	if len(values) > limit {
		if _, err := fmt.Fprintf(s.stdout, "  %s and %d more\n", s.palette.muted("..."), len(values)-limit); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(s.stdout)
	return err
}

func (s *shellSession) renderTestsPreview(title string, guidance symbolTestGuidance, limit int) error {
	tests := guidance.ReadBefore
	if _, err := fmt.Fprintf(s.stdout, "%s (%d)\n", s.palette.section(title), len(tests)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.stdout, "  %s %s\n", s.palette.label("signal:"), guidance.Signal); err != nil {
		return err
	}
	if guidance.ImportantWarning != "" {
		if _, err := fmt.Fprintf(s.stdout, "  %s %s\n", s.palette.label("warning:"), guidance.ImportantWarning); err != nil {
			return err
		}
	}
	if len(tests) == 0 {
		_, err := fmt.Fprintf(s.stdout, "  %s\n\n", s.palette.muted("coverage unavailable, no strong direct or nearby tests indexed"))
		return err
	}
	for _, test := range tests[:min(limit, len(tests))] {
		if _, err := fmt.Fprintf(s.stdout, "  -> %s %s  %s\n", s.palette.kindBadge(test.Kind), test.Name, formatTestRelationLabel(test)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(s.stdout, "     %s:%d\n", test.FilePath, test.Line); err != nil {
			return err
		}
		if test.Why != "" {
			if _, err := fmt.Fprintf(s.stdout, "     %s %s\n", s.palette.label("why:"), test.Why); err != nil {
				return err
			}
		}
	}
	if len(tests) > limit {
		if _, err := fmt.Fprintf(s.stdout, "  %s and %d more\n", s.palette.muted("..."), len(tests)-limit); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(s.stdout)
	return err
}

func (s *shellSession) renderFileJourney(filePath, focusSymbolKey string, symbols []storage.SymbolMatch, summary storage.FileSummary, focusView *storage.SymbolView) error {
	if err := s.renderFileJourneyOverview(filePath, focusSymbolKey, symbols, summary, focusView); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(
		s.stdout,
		"%s\n  %s %s\n  %s %d symbol(s) indexed here\n  %s linked=%d  coverage=%s\n\n",
		s.palette.section("File Journey"),
		s.palette.label("File:"),
		filePath,
		s.palette.label("Inventory:"),
		len(symbols),
		s.palette.label("Tests:"),
		summary.RelatedTestCount,
		s.coverageBadge(coveragePercent(summary.TestLinkedSymbolCount, summary.RelevantSymbolCount)),
	); err != nil {
		return err
	}

	if len(symbols) == 0 {
		_, err := fmt.Fprintf(s.stdout, "  %s\n\n", s.palette.muted("No indexed symbols in this file"))
		return err
	}

	for idx, symbol := range symbols {
		marker := "  "
		if symbol.SymbolKey == focusSymbolKey {
			marker = "=>"
		}
		if _, err := fmt.Fprintf(
			s.stdout,
			"%s [%d] %s %s\n     %s\n     %s %s\n",
			marker,
			idx+1,
			s.palette.kindBadge(symbol.Kind),
			symbol.Name,
			styleHumanSignature(s.palette, displaySignature(symbol)),
			s.palette.label("declared at"),
			symbolRangeWithCountDisplay(s.info.Root, symbol),
		); err != nil {
			return err
		}
		if doc := oneLine(symbol.Doc); doc != "" {
			if _, err := fmt.Fprintf(s.stdout, "     %s %s\n", s.palette.label("doc:"), doc); err != nil {
				return err
			}
		}
		if idx+1 < len(symbols) {
			if _, err := fmt.Fprintln(s.stdout); err != nil {
				return err
			}
		}
	}
	if _, err := fmt.Fprintf(
		s.stdout,
		"\n%s\n  %s use %s to jump into a symbol\n  %s use %s to inspect the body without leaving the file screen\n  %s use %s to walk entity-by-entity through this file\n  %s use %s for the full current entity body or plain %s for the whole file\n  %s use %s for a ranked file report\n  %s use %s to copy the current symbol/file context\n  %s use %s or %s to move up a level\n\n",
		s.palette.section("Workflow"),
		s.palette.label("Next:"),
		s.palette.accent("open <n>"),
		s.palette.label("Peek:"),
		s.palette.accent("source <n>"),
		s.palette.label("Walk:"),
		s.palette.accent("walk"),
		s.palette.label("Deep body:"),
		s.palette.accent("full <n>"),
		s.palette.accent("full"),
		s.palette.label("Report:"),
		s.palette.accent("report"),
		s.palette.label("Copy:"),
		s.palette.accent("copy"),
		s.palette.label("Navigate:"),
		s.palette.accent("tree"),
		s.palette.accent("home"),
	); err != nil {
		return err
	}

	if focusView == nil {
		return nil
	}
	preview, err := renderSymbolSource(s.info.Root, s.batPath, focusView.Symbol, 20, s.palette.enabled)
	if err != nil {
		return err
	}
	if preview != "" {
		if _, err := fmt.Fprintf(s.stdout, "%s\n%s\n\n", s.palette.section("Focused Body"), preview); err != nil {
			return err
		}
	}
	return nil
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func (s *shellSession) renderFileJourneyOverview(filePath, focusSymbolKey string, symbols []storage.SymbolMatch, summary storage.FileSummary, focusView *storage.SymbolView) error {
	packageName := shortenQName(s.info.ModulePath, summary.PackageImportPath)
	if packageName == "" && focusView != nil {
		packageName = shortenQName(s.info.ModulePath, focusView.Symbol.PackageImportPath)
	}
	if packageName == "" {
		packageName = "unknown"
	}

	totalCallers := 0
	totalRefsIn := 0
	totalRefsOut := 0
	hotspotsLabel := "none yet"
	riskSummary := "contained"
	if len(symbols) > 0 {
		var parts []string
		for _, symbol := range symbols[:min(3, len(symbols))] {
			parts = append(parts, fmt.Sprintf("%s[%s]", symbol.Name, symbol.Kind))
		}
		hotspotsLabel = strings.Join(parts, ", ")
	}

	focus := "first symbol in file"
	if focusView != nil {
		totalCallers = len(focusView.Callers)
		totalRefsIn = len(focusView.ReferencesIn)
		totalRefsOut = len(focusView.ReferencesOut)
		focus = shortenQName(s.info.ModulePath, focusView.Symbol.QName)
	} else if focusSymbolKey != "" {
		for _, symbol := range symbols {
			if symbol.SymbolKey == focusSymbolKey {
				focus = shortenQName(s.info.ModulePath, symbol.QName)
				break
			}
		}
	}
	if hotScore, recentChanged, err := s.fileRiskSignals(filePath, 0); err == nil {
		riskSummary = fileRiskSummary(summary, hotScore, recentChanged)
	}
	section := s.buildShellFileExplain(summary, focusView, riskSummary, []string{hotspotsLabel}, focus)
	section.Facts = append(section.Facts[:1], append([]explainFact{
		{Key: "Signals", Value: fmt.Sprintf("callers=%d refs_in=%d refs_out=%d", totalCallers, totalRefsIn, totalRefsOut)},
		{Key: "Explore", Value: "walk / source <n> / full <n> / full"},
	}, section.Facts[1:]...)...)
	if packageName != "" {
		section.Facts[1].Value = packageName + " " + strings.TrimSpace(stripANSICodes(s.fileBadge(filePath, summary.IsTest)))
	}
	return s.renderShellExplain(section)
}

func averageLineLabel(total, count int) string {
	if count == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%dL", total/count)
}

func stripANSICodes(value string) string {
	return ansiPattern.ReplaceAllString(value, "")
}

func (s *shellSession) landingHotFiles(view storage.ReportView, limit int) []shellTarget {
	type scoredFile struct {
		Path   string
		Score  int
		Symbol storage.SymbolMatch
	}
	byFile := map[string]*scoredFile{}
	appendSymbol := func(item storage.RankedSymbol) {
		entry, ok := byFile[item.Symbol.FilePath]
		if !ok {
			entry = &scoredFile{Path: item.Symbol.FilePath, Symbol: item.Symbol}
			byFile[item.Symbol.FilePath] = entry
		}
		entry.Score += item.Score
		if entry.Symbol.Line == 0 || item.Symbol.Line < entry.Symbol.Line {
			entry.Symbol = item.Symbol
		}
	}
	for _, item := range view.TopFunctions {
		appendSymbol(item)
	}
	for _, item := range view.TopTypes {
		appendSymbol(item)
	}

	values := make([]*scoredFile, 0, len(byFile))
	for _, value := range byFile {
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].Score != values[j].Score {
			return values[i].Score > values[j].Score
		}
		return values[i].Path < values[j].Path
	})

	targets := make([]shellTarget, 0, min(limit, len(values)))
	for _, value := range values[:min(limit, len(values))] {
		targets = append(targets, shellTarget{
			Kind:      "file",
			Label:     value.Path,
			FilePath:  value.Path,
			Line:      value.Symbol.Line,
			SymbolKey: value.Symbol.SymbolKey,
		})
	}
	return targets
}

func shortenFile(path string) string {
	return filepath.ToSlash(path)
}

func compactSignature(value string) string {
	return strings.ReplaceAll(value, "\n", " ")
}
