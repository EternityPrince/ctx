package app

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/storage"
)

func (s *shellSession) showSymbol(query string, pushHistory bool) error {
	matches, err := s.store.FindSymbols(query)
	if err != nil {
		return err
	}
	if len(matches) == 0 {
		if err := s.beginScreen("No Match"); err != nil {
			return err
		}
		_, err := fmt.Fprintf(s.stdout, "No symbol matches for %q\n\n", query)
		return err
	}
	if len(matches) == 1 {
		return s.openSymbolKey(matches[0].SymbolKey, pushHistory)
	}

	if err := s.beginScreen("Matches"); err != nil {
		return err
	}
	s.lastTargets = make([]shellTarget, 0, len(matches))
	if _, err := fmt.Fprintf(s.stdout, "%s (%d)\n", s.palette.section("Matches"), len(matches)); err != nil {
		return err
	}
	for idx, match := range matches {
		s.lastTargets = append(s.lastTargets, shellTarget{
			Kind:      "symbol",
			SymbolKey: match.SymbolKey,
			Label:     shortenQName(s.info.ModulePath, match.QName),
			FilePath:  match.FilePath,
			Line:      match.Line,
		})
		if _, err := fmt.Fprintf(
			s.stdout,
			"  [%d] %s %s\n      %s\n      %s\n      %s %s\n      %s %s\n",
			idx+1,
			s.palette.kindBadge(match.Kind),
			shortenQName(s.info.ModulePath, match.QName),
			styleHumanSignature(s.palette, displaySignature(match)),
			symbolRangeDisplay(s.info.Root, match),
			s.palette.label("next:"),
			strings.Join(searchResultNextCommands(match, idx+1), " | "),
			s.palette.label("lenses:"),
			strings.Join(searchResultLensCommands(match, idx+1), " | "),
		); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(s.stdout)
	return err
}

func (s *shellSession) openSymbolKey(symbolKey string, pushHistory bool) error {
	view, err := s.store.LoadSymbolView(symbolKey)
	if err != nil {
		return err
	}

	s.currentKey = symbolKey
	s.currentQName = view.Symbol.QName
	s.currentMode = "symbol"
	s.currentFile = view.Symbol.FilePath
	if pushHistory {
		s.pushHistory(symbolKey)
	}

	if err := s.beginScreen("Symbol Journey"); err != nil {
		return err
	}
	if err := s.renderSymbolJourney(view); err != nil {
		return err
	}
	return nil
}

func (s *shellSession) pushHistory(symbolKey string) {
	if s.historyIndex >= 0 && s.historyIndex < len(s.history) && s.history[s.historyIndex] == symbolKey {
		return
	}
	if s.historyIndex+1 < len(s.history) {
		s.history = append([]string{}, s.history[:s.historyIndex+1]...)
	}
	s.history = append(s.history, symbolKey)
	s.historyIndex = len(s.history) - 1
}

func (s *shellSession) back() error {
	if s.historyIndex <= 0 {
		_, err := fmt.Fprintln(s.stdout, "No previous symbol")
		return err
	}
	s.historyIndex--
	return s.openSymbolKey(s.history[s.historyIndex], false)
}

func (s *shellSession) forward() error {
	if s.historyIndex < 0 || s.historyIndex+1 >= len(s.history) {
		_, err := fmt.Fprintln(s.stdout, "No next symbol")
		return err
	}
	s.historyIndex++
	return s.openSymbolKey(s.history[s.historyIndex], false)
}

func (s *shellSession) openIndex(raw string) error {
	index, err := strconv.Atoi(raw)
	if err != nil || index < 1 || index > len(s.lastTargets) {
		_, writeErr := fmt.Fprintf(s.stdout, "No item %q in the current list\n\n", raw)
		if writeErr != nil {
			return writeErr
		}
		return nil
	}

	target := s.lastTargets[index-1]
	switch target.Kind {
	case "action":
		return s.runAction(target.Action)
	case "dir":
		s.treeMode = shellTreeModeFiles
		s.treeScope = normalizeTreeScope(target.FilePath)
		s.treePage = 0
		return s.showTreeCommand(nil)
	case "file":
		return s.showFileJourney(target.FilePath)
	case "location":
		return s.showLocation(target.Label, target.FilePath, target.Line)
	default:
		return s.openSymbolKey(target.SymbolKey, true)
	}
}

func (s *shellSession) currentView() (storage.SymbolView, error) {
	if s.currentKey == "" {
		return storage.SymbolView{}, fmt.Errorf("No current symbol. Type a symbol name first.")
	}
	return s.store.LoadSymbolView(s.currentKey)
}

func (s *shellSession) targetSymbolView(arg string, pushHistory bool) (storage.SymbolView, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return s.currentView()
	}

	target, ok := s.targetFromArg(arg)
	if !ok {
		return storage.SymbolView{}, fmt.Errorf("No list item %q to inspect", arg)
	}
	if target.Kind != "symbol" {
		return storage.SymbolView{}, fmt.Errorf("List item %q is not a symbol", arg)
	}

	view, err := s.store.LoadSymbolView(target.SymbolKey)
	if err != nil {
		return storage.SymbolView{}, err
	}
	if pushHistory && target.SymbolKey != "" && target.SymbolKey != s.currentKey {
		s.pushHistory(target.SymbolKey)
	}
	s.currentKey = view.Symbol.SymbolKey
	s.currentQName = view.Symbol.QName
	s.currentFile = view.Symbol.FilePath
	s.currentMode = "symbol"
	return view, nil
}

func (s *shellSession) beginCurrentSymbolScreen(title string) (storage.SymbolView, error) {
	view, err := s.currentView()
	if err != nil {
		return storage.SymbolView{}, err
	}
	if err := s.beginScreen(title); err != nil {
		return storage.SymbolView{}, err
	}
	if err := s.writeCurrentSymbolSummary(view); err != nil {
		return storage.SymbolView{}, err
	}
	return view, nil
}

func (s *shellSession) beginTargetSymbolScreen(title, arg string) (storage.SymbolView, error) {
	view, err := s.targetSymbolView(arg, true)
	if err != nil {
		return storage.SymbolView{}, err
	}
	if err := s.beginScreen(title); err != nil {
		return storage.SymbolView{}, err
	}
	if err := s.writeCurrentSymbolSummary(view); err != nil {
		return storage.SymbolView{}, err
	}
	return view, nil
}

func (s *shellSession) showImpact(query string) error {
	query = strings.TrimSpace(query)
	symbolKey := s.currentKey
	if query != "" {
		if view, err := s.targetSymbolView(query, true); err == nil {
			symbolKey = view.Symbol.SymbolKey
		} else {
			matches, err := s.store.FindSymbols(query)
			if err != nil {
				return err
			}
			if len(matches) == 0 {
				_, err := fmt.Fprintf(s.stdout, "No symbol matches for %q\n\n", query)
				return err
			}
			if len(matches) > 1 {
				return s.showSymbol(query, false)
			}
			symbolKey = matches[0].SymbolKey
		}
	}
	if symbolKey == "" {
		_, err := fmt.Fprintln(s.stdout, "No current symbol. Type a symbol name first.")
		return err
	}

	s.currentKey = symbolKey
	view, err := s.store.LoadImpactView(symbolKey, 3)
	if err != nil {
		return err
	}
	symbolView, err := s.store.LoadSymbolView(symbolKey)
	if err != nil {
		return err
	}
	guidance, err := buildSymbolTestGuidance(s.store, symbolView, 8)
	if err != nil {
		return err
	}
	s.currentQName = view.Target.QName
	s.currentMode = "symbol"
	s.currentFile = view.Target.FilePath
	if err := s.beginScreen("Impact"); err != nil {
		return err
	}
	if err := renderHumanImpactView(s.stdout, s.info.Root, s.info.ModulePath, view, guidance, 3, false); err != nil {
		return err
	}
	currentView, err := s.store.LoadSymbolView(symbolKey)
	if err != nil {
		return err
	}
	return s.showAutoDriveForView(currentView)
}

func (s *shellSession) showSource() error {
	view, err := s.beginCurrentSymbolScreen("Source")
	if err != nil {
		return s.printShellError(err)
	}

	excerpt, err := renderSymbolSource(s.info.Root, s.batPath, view.Symbol, 40, s.palette.enabled)
	if err != nil {
		return err
	}
	if excerpt == "" {
		_, err := fmt.Fprintln(s.stdout, "No source excerpt available")
		return err
	}
	_, err = fmt.Fprintf(s.stdout, "%s\n%s\n\n", s.palette.section("Source"), excerpt)
	return err
}

func (s *shellSession) showReport() error {
	return s.showReportScope("project")
}

func (s *shellSession) showReportScope(scope string) error {
	status, err := s.store.Status(s.changedNow)
	if err != nil {
		return err
	}
	normalized := normalizeReportSliceScope(scope)
	loadLimit := 6
	if normalized != "project" {
		loadLimit = 24
	}
	view, err := s.store.LoadReportView(loadLimit)
	if err != nil {
		return err
	}
	watch, err := buildReportTestWatch(s.store, view)
	if err != nil {
		return err
	}
	s.currentMode = "report"
	screenTitle := "Project Report"
	if normalized != "project" {
		screenTitle = "Report · " + reportSliceTitle(normalized)
	}
	if err := s.beginScreen(screenTitle); err != nil {
		return err
	}
	if normalized == "project" {
		if err := renderHumanReport(s.stdout, s.info.Root, s.info.ModulePath, status, view, watch, s.composition, false); err != nil {
			return err
		}
	} else {
		slice, err := buildReportSlice(normalized, s.store, view, watch, 6)
		if err != nil {
			return err
		}
		if err := renderHumanReportSlice(s.stdout, s.info.Root, s.info.ModulePath, status, view, slice, 6, false); err != nil {
			return err
		}
	}
	s.lastTargets = nil
	return nil
}

func (s *shellSession) showAutoDrive() error {
	view, err := s.beginCurrentSymbolScreen("Next Steps")
	if err != nil {
		return s.printShellError(err)
	}
	return s.showAutoDriveForView(view)
}

func (s *shellSession) showAutoDriveForView(view storage.SymbolView) error {
	steps := suggestedStepsForView(view)
	lenses := builtinLenses(view)
	s.lastTargets = s.lastTargets[:0]

	if _, err := fmt.Fprintf(s.stdout, "%s\n", s.palette.section("Adjacent Routes")); err != nil {
		return err
	}
	for _, step := range steps[:min(6, len(steps))] {
		s.lastTargets = append(s.lastTargets, step.Target)
		if _, err := fmt.Fprintf(
			s.stdout,
			"  [%d] %-18s %s\n",
			len(s.lastTargets),
			step.Label,
			step.Summary,
		); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(s.stdout); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(s.stdout, "%s\n", s.palette.section("Saved Views / Named Lenses")); err != nil {
		return err
	}
	for _, lens := range lenses {
		target := shellTarget{Kind: "action", Action: "lens:" + lens.Name}
		s.lastTargets = append(s.lastTargets, target)
		badge := ""
		if lens.Recommended {
			badge = "  [" + s.palette.badge("recommended") + "]"
		}
		if _, err := fmt.Fprintf(
			s.stdout,
			"  [%d] %-18s %s%s\n",
			len(s.lastTargets),
			lens.Label,
			lens.Summary,
			badge,
		); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(
		s.stdout,
		"\n%s use %s, %s, %s, %s, or %s from search results too\n%s type a number, or run file/callers/callees/tests/impact/source/report/lens <name> directly\n\n",
		s.palette.label("Quick jumps:"),
		s.palette.accent("file <n>"),
		s.palette.accent("callers <n>"),
		s.palette.accent("callees <n>"),
		s.palette.accent("tests <n>"),
		s.palette.accent("impact <n>"),
		s.palette.label("Flow:"),
	); err != nil {
		return err
	}
	return nil
}

func (s *shellSession) showLocation(label, relPath string, line int) error {
	excerpt, err := renderLocationSource(s.info.Root, s.batPath, relPath, line, 2, 6, s.palette.enabled)
	if err != nil {
		return err
	}
	if err := s.beginScreen("Location"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.stdout, "%s\n  %s\n  %s:%d\n", s.palette.section("Location"), label, relPath, line); err != nil {
		return err
	}
	if excerpt != "" {
		if _, err := fmt.Fprintf(s.stdout, "%s\n\n", excerpt); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(s.stdout)
	return err
}

func (s *shellSession) printShellError(err error) error {
	if screenErr := s.beginScreen("Error"); screenErr != nil {
		return screenErr
	}
	_, writeErr := fmt.Fprintf(s.stdout, "%v\n\n", err)
	if writeErr != nil {
		return writeErr
	}
	return nil
}
