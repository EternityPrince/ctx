package app

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

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

	if _, err := fmt.Fprintf(
		s.stdout,
		"%s\n  %s callers=%d  refs_in=%d  refs_out=%d  tests=%d  coverage=%s\n  %s %s\n  %s %s\n  %s local_deps=%d  reverse_deps=%d  file_symbols=%d\n\n",
		s.palette.section("Why It Matters"),
		s.palette.label("Signals:"),
		len(view.Callers),
		len(view.ReferencesIn),
		len(view.ReferencesOut),
		len(view.Tests),
		s.palette.muted("n/a"),
		s.palette.label("Risk:"),
		riskSummary,
		s.palette.label("Tests:"),
		testGuidance.Signal,
		s.palette.label("Area:"),
		len(view.Package.LocalDeps),
		len(view.Package.ReverseDeps),
		len(view.Siblings)+1,
	); err != nil {
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
		packageName = s.palette.muted("unknown")
	}

	fileImportance := "low"
	totalCallers := 0
	totalRefsIn := 0
	totalRefsOut := 0
	packageLocalDeps := 0
	packageReverseDeps := 0
	hotspots := s.palette.muted("none yet")
	riskSummary := "contained"
	if len(symbols) > 0 {
		var parts []string
		for _, symbol := range symbols[:min(3, len(symbols))] {
			parts = append(parts, fmt.Sprintf("%s[%s]", symbol.Name, symbol.Kind))
		}
		hotspots = s.palette.accent(strings.Join(parts, ", "))
	}

	focus := s.palette.muted("first symbol in file")
	if focusView != nil {
		fileImportance = impactLabel(len(focusView.Callers), len(focusView.ReferencesIn), len(focusView.Tests), len(focusView.Package.ReverseDeps))
		totalCallers = len(focusView.Callers)
		totalRefsIn = len(focusView.ReferencesIn)
		totalRefsOut = len(focusView.ReferencesOut)
		packageLocalDeps = len(focusView.Package.LocalDeps)
		packageReverseDeps = len(focusView.Package.ReverseDeps)
		focus = s.palette.accent(shortenQName(s.info.ModulePath, focusView.Symbol.QName))
	} else if focusSymbolKey != "" {
		for _, symbol := range symbols {
			if symbol.SymbolKey == focusSymbolKey {
				focus = s.palette.accent(shortenQName(s.info.ModulePath, symbol.QName))
				break
			}
		}
	}
	if hotScore, recentChanged, err := s.fileRiskSignals(filePath, 0); err == nil {
		riskSummary = fileRiskSummary(summary, hotScore, recentChanged)
	}

	if _, err := fmt.Fprintf(s.stdout, "%s\n", s.palette.section("Why It Matters")); err != nil {
		return err
	}
	if err := s.writeFileJourneyRow(
		fmt.Sprintf("%s %s", s.palette.label("File:"), filePath),
		fmt.Sprintf("%s %s %s", s.palette.label("Package:"), packageName, s.fileBadge(filePath, summary.IsTest)),
	); err != nil {
		return err
	}
	if err := s.writeFileJourneyRow(
		fmt.Sprintf("%s symbols=%d  fn=%d  methods=%d  types=%d", s.palette.label("Shape:"), summary.SymbolCount, summary.FuncCount, summary.MethodCount, summary.StructCount),
		fmt.Sprintf("%s tests=%d  link=%s", s.palette.label("Test map:"), max(summary.DeclaredTestCount, summary.RelatedTestCount), s.coverageBadge(coveragePercent(summary.TestLinkedSymbolCount, summary.RelevantSymbolCount))),
	); err != nil {
		return err
	}
	if err := s.writeFileJourneyRow(
		fmt.Sprintf("%s %s", s.palette.label("Importance:"), s.palette.badge(fileImportance)),
		fmt.Sprintf("%s callers=%d  refs_in=%d  refs_out=%d", s.palette.label("Signals:"), totalCallers, totalRefsIn, totalRefsOut),
	); err != nil {
		return err
	}
	if err := s.writeFileJourneyRow(
		fmt.Sprintf("%s local_deps=%d  reverse_deps=%d", s.palette.label("Reach:"), packageLocalDeps, packageReverseDeps),
		fmt.Sprintf("%s %s", s.palette.label("Risk:"), riskSummary),
	); err != nil {
		return err
	}
	if err := s.writeFileJourneyRow(
		fmt.Sprintf("%s %s", s.palette.label("Hotspots:"), hotspots),
		fmt.Sprintf("%s %s", s.palette.label("Focus:"), focus),
	); err != nil {
		return err
	}
	if err := s.writeFileJourneyRow(
		fmt.Sprintf("%s size=%s", s.palette.label("Footprint:"), shellHumanSize(summary.SizeBytes)),
		fmt.Sprintf("%s %s / %s / %s / %s", s.palette.label("Explore:"), s.palette.accent("walk"), s.palette.accent("source <n>"), s.palette.accent("full <n>"), s.palette.accent("full")),
	); err != nil {
		return err
	}
	previewState := s.palette.muted("open a symbol to preview")
	if focusView != nil {
		previewState = s.palette.accent("focused body ready below")
	}
	if err := s.writeFileJourneyRow(
		fmt.Sprintf("%s %s", s.palette.label("Preview:"), previewState),
		"",
	); err != nil {
		return err
	}
	_, err := fmt.Fprintln(s.stdout)
	return err
}

func (s *shellSession) writeFileJourneyRow(left, right string) error {
	termWidth, _ := terminalSize()
	if termWidth <= 0 {
		termWidth = 120
	}
	usableWidth := max(72, termWidth-6)
	leftWidth := (usableWidth - 4) / 2
	rightWidth := usableWidth - 4 - leftWidth
	left = fitShellColumn(left, leftWidth)
	right = fitShellColumn(right, rightWidth)
	_, err := fmt.Fprintf(s.stdout, "  %s  ||  %s\n", left, right)
	return err
}

func fitShellColumn(value string, width int) string {
	if width <= 0 {
		return value
	}
	visible := stripANSICodes(value)
	runes := []rune(visible)
	if len(runes) > width {
		if width <= 1 {
			return string(runes[:width])
		}
		return string(runes[:width-1]) + "…"
	}
	return value + strings.Repeat(" ", width-utf8.RuneCountInString(visible))
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
