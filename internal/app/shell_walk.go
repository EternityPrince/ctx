package app

import (
	"fmt"
	"strconv"
	"strings"
)

func (s *shellSession) showWalk(args []string) error {
	if len(args) == 0 {
		if s.walkActive {
			return s.renderWalk()
		}
		return s.enterWalk("")
	}

	command := strings.ToLower(strings.TrimSpace(args[0]))
	if !s.walkActive {
		switch command {
		case "exit", "leave", "off":
			return s.printShellError(fmt.Errorf("Walk mode is not active yet. Enter a file and type `walk`."))
		default:
			return s.enterWalk(strings.Join(args, " "))
		}
	}

	switch command {
	case "next":
		return s.walkMove(1)
	case "prev":
		return s.walkMove(-1)
	case "open":
		return s.openWalkCurrent()
	case "full":
		return s.showFullCurrentEntity()
	case "exit", "leave", "off":
		return s.exitWalk()
	default:
		if index, err := strconv.Atoi(strings.TrimSpace(args[0])); err == nil {
			return s.walkJump(index)
		}
		return s.enterWalk(strings.Join(args, " "))
	}
}

func (s *shellSession) enterWalk(query string) error {
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
		return s.printShellError(fmt.Errorf("%s is a directory. Use `tree` to explore directories and `walk <file>` for files.", relPath))
	}

	symbols, err := s.store.LoadFileSymbols(relPath)
	if err != nil {
		return err
	}
	if len(symbols) == 0 {
		return s.printShellError(fmt.Errorf("No indexed symbols in %s to walk through.", relPath))
	}

	s.walkActive = true
	s.walkSymbols = symbols
	s.walkIndex = 0
	s.currentMode = "walk"
	s.currentFile = relPath

	if focusSymbolKey == "" {
		focusSymbolKey = s.currentKey
	}
	for idx, symbol := range symbols {
		if symbol.SymbolKey == focusSymbolKey {
			s.walkIndex = idx
			break
		}
	}
	s.syncWalkCurrent()
	return s.renderWalk()
}

func (s *shellSession) walkMove(delta int) error {
	if !s.walkActive || len(s.walkSymbols) == 0 {
		return s.printShellError(fmt.Errorf("Walk mode is not active. Enter a file and type `walk`."))
	}
	s.walkIndex = (s.walkIndex + delta + len(s.walkSymbols)) % len(s.walkSymbols)
	s.syncWalkCurrent()
	return s.renderWalk()
}

func (s *shellSession) walkJump(index int) error {
	if !s.walkActive || len(s.walkSymbols) == 0 {
		return s.printShellError(fmt.Errorf("Walk mode is not active. Enter a file and type `walk`."))
	}
	if index < 1 || index > len(s.walkSymbols) {
		return s.printShellError(fmt.Errorf("No symbol %d in the current walk.", index))
	}
	s.walkIndex = index - 1
	s.syncWalkCurrent()
	return s.renderWalk()
}

func (s *shellSession) openWalkCurrent() error {
	if !s.walkActive || len(s.walkSymbols) == 0 {
		return s.printShellError(fmt.Errorf("Walk mode is not active."))
	}
	current := s.walkSymbols[s.walkIndex]
	s.walkActive = false
	return s.openSymbolKey(current.SymbolKey, true)
}

func (s *shellSession) exitWalk() error {
	if !s.walkActive {
		return s.showFileJourney("")
	}
	s.walkActive = false
	return s.showFileJourney(s.currentFile)
}

func (s *shellSession) syncWalkCurrent() {
	if !s.walkActive || len(s.walkSymbols) == 0 || s.walkIndex < 0 || s.walkIndex >= len(s.walkSymbols) {
		return
	}
	current := s.walkSymbols[s.walkIndex]
	s.currentKey = current.SymbolKey
	s.currentQName = current.QName
	s.currentFile = current.FilePath
	s.currentMode = "walk"
}

func (s *shellSession) renderWalk() error {
	if !s.walkActive || len(s.walkSymbols) == 0 {
		return s.printShellError(fmt.Errorf("Walk mode is not active."))
	}
	s.syncWalkCurrent()
	current := s.walkSymbols[s.walkIndex]

	view, err := s.store.LoadSymbolView(current.SymbolKey)
	if err != nil {
		return err
	}

	if err := s.beginScreen("Walk"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		s.stdout,
		"%s\n  %s %s\n  %s %d/%d\n  %s %s\n  %s %s\n\n",
		s.palette.section("Walk State"),
		s.palette.label("File:"),
		current.FilePath,
		s.palette.label("Step:"),
		s.walkIndex+1,
		len(s.walkSymbols),
		s.palette.label("Current:"),
		shortenQName(s.info.ModulePath, current.QName),
		s.palette.label("Declared:"),
		symbolRangeDisplay(s.info.Root, current),
	); err != nil {
		return err
	}
	if err := s.writeCurrentSymbolSummary(view); err != nil {
		return err
	}

	s.lastTargets = s.lastTargets[:0]
	if _, err := fmt.Fprintf(s.stdout, "%s (%d)\n", s.palette.section("File Symbols"), len(s.walkSymbols)); err != nil {
		return err
	}
	start := max(0, s.walkIndex-3)
	end := min(len(s.walkSymbols), s.walkIndex+4)
	for idx := start; idx < end; idx++ {
		symbol := s.walkSymbols[idx]
		s.lastTargets = append(s.lastTargets, shellTarget{
			Kind:      "symbol",
			SymbolKey: symbol.SymbolKey,
			Label:     shortenQName(s.info.ModulePath, symbol.QName),
			FilePath:  symbol.FilePath,
			Line:      symbol.Line,
		})
		marker := "  "
		if idx == s.walkIndex {
			marker = "=>"
		}
		if _, err := fmt.Fprintf(
			s.stdout,
			"%s [%d] %s %s\n     %s\n     %s\n",
			marker,
			idx+1,
			s.palette.kindBadge(symbol.Kind),
			symbol.Name,
			styleHumanSignature(s.palette, displaySignature(symbol)),
			symbolRangeDisplay(s.info.Root, symbol),
		); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(s.stdout); err != nil {
		return err
	}

	preview, err := renderSymbolSource(s.info.Root, s.batPath, current, 18, s.palette.enabled)
	if err != nil {
		return err
	}
	if preview != "" {
		if _, err := fmt.Fprintf(s.stdout, "%s\n%s\n\n", s.palette.section("Body Preview"), preview); err != nil {
			return err
		}
	}

	_, err = fmt.Fprintf(
		s.stdout,
		"%s\n  %s use %s / %s to move through the file\n  %s use %s to show the full entity body\n  %s use %s or %s to open another entity directly\n  %s use %s to jump into the regular symbol journey\n  %s use %s to leave walk mode, but normal commands still work from here\n\n",
		s.palette.section("Walk Flow"),
		s.palette.label("Move:"),
		s.palette.accent("next"),
		s.palette.accent("prev"),
		s.palette.label("Body:"),
		s.palette.accent("full"),
		s.palette.label("Jump:"),
		s.palette.accent("walk <n>"),
		s.palette.accent("open <n>"),
		s.palette.label("Open:"),
		s.palette.accent("open-current"),
		s.palette.label("Exit:"),
		s.palette.accent("leave"),
	)
	return err
}

func (s *shellSession) showFullCurrentEntity() error {
	if s.currentKey == "" {
		return s.printShellError(fmt.Errorf("No current entity. Open or walk a symbol first."))
	}
	view, err := s.currentView()
	if err != nil {
		return s.printShellError(err)
	}
	if err := s.beginScreen("Full Body"); err != nil {
		return err
	}
	if err := s.writeCurrentSymbolSummary(view); err != nil {
		return err
	}
	source, err := renderSymbolSource(s.info.Root, s.batPath, view.Symbol, 0, s.palette.enabled)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(s.stdout, "%s\n%s\n\n", s.palette.section("Full Entity Body"), source)
	return err
}
