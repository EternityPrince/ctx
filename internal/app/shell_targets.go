package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/clipboard"
	"github.com/vladimirkasterin/ctx/internal/storage"
)

func (s *shellSession) showFileJourney(query string) error {
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
		return s.printShellError(fmt.Errorf("%s is a directory. Use `tree` to explore directories and `file <path>` for files.", relPath))
	}

	symbols, summary, selectedSymbolKey, focusView, err := s.loadFileJourneyState(relPath, focusSymbolKey)
	if err != nil {
		return err
	}

	s.currentMode = "file"
	s.currentFile = relPath
	if focusSymbolKey == "" {
		s.currentQName = ""
	}
	if err := s.beginScreen("File Journey"); err != nil {
		return err
	}

	s.lastTargets = s.lastTargets[:0]
	for _, symbol := range symbols {
		s.lastTargets = append(s.lastTargets, shellTarget{
			Kind:      "symbol",
			SymbolKey: symbol.SymbolKey,
			Label:     symbol.Name,
			FilePath:  symbol.FilePath,
			Line:      symbol.Line,
		})
	}
	return s.renderFileJourney(relPath, selectedSymbolKey, symbols, summary, focusView)
}

func (s *shellSession) loadFileJourneyState(relPath, focusSymbolKey string) ([]storage.SymbolMatch, storage.FileSummary, string, *storage.SymbolView, error) {
	symbols, err := s.store.LoadFileSymbols(relPath)
	if err != nil {
		return nil, storage.FileSummary{}, "", nil, err
	}
	summary, err := s.store.LoadFileSummary(relPath)
	if err != nil {
		return nil, storage.FileSummary{}, "", nil, err
	}

	selectedSymbolKey := strings.TrimSpace(focusSymbolKey)
	if selectedSymbolKey == "" && len(symbols) > 0 {
		selectedSymbolKey = symbols[0].SymbolKey
	}
	if selectedSymbolKey == "" {
		return symbols, summary, "", nil, nil
	}

	view, err := s.store.LoadSymbolView(selectedSymbolKey)
	if err != nil {
		return nil, storage.FileSummary{}, "", nil, err
	}
	return symbols, summary, selectedSymbolKey, &view, nil
}

func (s *shellSession) resolveFileQuery(query string) (string, string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		if s.currentFile != "" {
			return s.currentFile, s.currentKey, nil
		}
		view, err := s.currentView()
		if err != nil {
			return "", "", fmt.Errorf("No current file. Use `file <path>` or open a symbol first.")
		}
		return view.Symbol.FilePath, view.Symbol.SymbolKey, nil
	}

	if target, ok := s.targetFromArg(query); ok && target.FilePath != "" {
		return target.FilePath, target.SymbolKey, nil
	}

	candidate := filepath.Clean(query)
	if filepath.IsAbs(candidate) {
		rel, err := filepath.Rel(s.info.Root, candidate)
		if err != nil {
			return "", "", fmt.Errorf("resolve file path: %w", err)
		}
		candidate = rel
	}
	candidate = filepath.ToSlash(candidate)
	return candidate, "", nil
}

func (s *shellSession) isDirectoryQuery(relPath string) (bool, error) {
	candidate := relPath
	if candidate == "" {
		candidate = "."
	}
	path := candidate
	if !filepath.IsAbs(path) {
		path = filepath.Join(s.info.Root, filepath.FromSlash(candidate))
	}
	stat, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("inspect path %s: %w", candidate, err)
	}
	return stat.IsDir(), nil
}

func (s *shellSession) showSourceTarget(arg string) error {
	target, ok := s.targetFromArg(arg)
	if !ok {
		return s.printShellError(fmt.Errorf("No list item %q to preview", arg))
	}
	switch target.Kind {
	case "symbol":
		match := storage.SymbolMatch{SymbolKey: target.SymbolKey, FilePath: target.FilePath, Line: target.Line}
		view, err := s.store.LoadSymbolView(target.SymbolKey)
		if err == nil {
			match = view.Symbol
			s.currentKey = view.Symbol.SymbolKey
			s.currentQName = view.Symbol.QName
			s.currentFile = view.Symbol.FilePath
			s.currentMode = "symbol"
		}
		if err := s.beginScreen("Body Preview"); err != nil {
			return err
		}
		if view.Symbol.SymbolKey != "" {
			if err := s.writeCurrentSymbolSummary(view); err != nil {
				return err
			}
		}
		source, err := renderSymbolSource(s.info.Root, s.batPath, match, 40, s.palette.enabled)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(s.stdout, "%s\n%s\n\n", s.palette.section("Body Preview"), source)
		return err
	case "file", "location":
		return s.showLocation(target.Label, target.FilePath, target.Line)
	default:
		return s.printShellError(fmt.Errorf("Target %q cannot be previewed", arg))
	}
}

func (s *shellSession) showFullTarget(arg string) error {
	target, ok := s.targetFromArg(arg)
	if !ok {
		return s.printShellError(fmt.Errorf("No list item %q to open fully", arg))
	}
	if target.Kind != "symbol" {
		return s.printShellError(fmt.Errorf("Target %q is not a symbol body", arg))
	}

	view, err := s.store.LoadSymbolView(target.SymbolKey)
	if err != nil {
		return err
	}
	s.currentKey = view.Symbol.SymbolKey
	s.currentQName = view.Symbol.QName
	s.currentFile = view.Symbol.FilePath
	s.currentMode = "symbol"
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

func (s *shellSession) copyCurrent(arg string) error {
	var text string
	if strings.TrimSpace(arg) != "" {
		target, ok := s.targetFromArg(arg)
		if !ok {
			return s.printShellError(fmt.Errorf("No list item %q to copy", arg))
		}
		switch target.Kind {
		case "symbol":
			view, err := s.store.LoadSymbolView(target.SymbolKey)
			if err != nil {
				return err
			}
			text = fmt.Sprintf("%s\n%s", displaySignature(view.Symbol), symbolRangeDisplay(s.info.Root, view.Symbol))
		case "file":
			text = target.FilePath
		default:
			text = fmt.Sprintf("%s\n%s:%d", target.Label, target.FilePath, target.Line)
		}
	} else if s.currentMode == "file" && s.currentFile != "" {
		text = s.currentFile
	} else if s.currentKey != "" {
		view, err := s.currentView()
		if err != nil {
			return err
		}
		text = fmt.Sprintf("%s\n%s", displaySignature(view.Symbol), symbolRangeDisplay(s.info.Root, view.Symbol))
	}
	if text == "" {
		return s.printShellError(fmt.Errorf("Nothing to copy yet. Open a symbol or file first."))
	}
	if err := clipboard.Copy(text); err != nil {
		return s.printShellError(fmt.Errorf("copy to clipboard failed: %w", err))
	}
	if err := s.beginScreen("Copied"); err != nil {
		return err
	}
	_, err := fmt.Fprintf(s.stdout, "%s\n  %s\n\n", s.palette.section("Clipboard"), text)
	return err
}

func (s *shellSession) targetFromArg(raw string) (shellTarget, bool) {
	index, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || index < 1 || index > len(s.lastTargets) {
		return shellTarget{}, false
	}
	return s.lastTargets[index-1], true
}

func (s *shellSession) previewLine(relPath string, line int) string {
	return sourceLineSnippet(s.info.Root, relPath, line)
}
