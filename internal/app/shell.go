package app

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/cli"
	"github.com/vladimirkasterin/ctx/internal/clipboard"
	"github.com/vladimirkasterin/ctx/internal/indexer"
	"github.com/vladimirkasterin/ctx/internal/project"
	"github.com/vladimirkasterin/ctx/internal/storage"
)

type shellSession struct {
	info         project.Info
	store        *storage.Store
	stdout       io.Writer
	palette      palette
	changedNow   int
	batPath      string
	currentKey   string
	currentQName string
	currentMode  string
	currentFile  string
	history      []string
	historyIndex int
	lastTargets  []shellTarget
}

type shellTarget struct {
	Kind      string
	Action    string
	SymbolKey string
	Label     string
	FilePath  string
	Line      int
}

const shellListLimit = 12

func runShellREPL(command cli.Command, stdout io.Writer) error {
	info, store, scanned, previous, err := openProjectState(command.Root)
	if err != nil {
		return err
	}
	defer store.Close()

	changes := indexer.Diff(scanned, previous)
	changedNow := len(changes.Added) + len(changes.Changed) + len(changes.Deleted)
	status, err := store.Status(changedNow)
	if err != nil {
		return err
	}
	if !status.HasSnapshot {
		_, err := fmt.Fprintf(stdout, "No index snapshot for %s. Run `ctx index %s` first.\n", info.ModulePath, info.Root)
		return err
	}

	session := &shellSession{
		info:         info,
		store:        store,
		stdout:       stdout,
		palette:      newPalette(),
		changedNow:   changedNow,
		batPath:      detectBatPath(),
		historyIndex: -1,
	}

	return session.run(command.Query)
}

func (s *shellSession) run(initialQuery string) error {
	if initialQuery != "" {
		if err := s.showSymbol(strings.TrimSpace(initialQuery), true); err != nil {
			return err
		}
	} else {
		if err := s.showLanding(); err != nil {
			return err
		}
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		if _, err := fmt.Fprint(s.stdout, s.prompt()); err != nil {
			return err
		}

		line, err := reader.ReadString('\n')
		if err != nil && len(line) == 0 {
			if err == io.EOF {
				_, writeErr := fmt.Fprintln(s.stdout)
				return writeErr
			}
			return err
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if err == io.EOF {
			if err := s.handle(line); err != nil {
				return err
			}
			_, writeErr := fmt.Fprintln(s.stdout)
			return writeErr
		}

		stop, err := s.handleWithStop(line)
		if err != nil {
			return err
		}
		if stop {
			return nil
		}
	}
}

func (s *shellSession) prompt() string {
	if s.currentMode == "file" && s.currentFile != "" {
		return fmt.Sprintf("ctx:file:%s> ", shortenQName(s.info.ModulePath, s.currentFile))
	}
	if s.currentQName != "" {
		return fmt.Sprintf("ctx:%s> ", shortenQName(s.info.ModulePath, s.currentQName))
	}
	return "ctx> "
}

func (s *shellSession) beginScreen(title string) error {
	if _, err := fmt.Fprint(s.stdout, "\x1b[2J\x1b[H"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.stdout, "%s\n%s\n", s.palette.rule("CTX Shell"), s.palette.title("CTX Shell · "+title)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.stdout, "%s %s\n%s %s\n", s.palette.label("Project:"), s.info.Root, s.palette.label("Module:"), s.info.ModulePath); err != nil {
		return err
	}
	if s.currentMode == "symbol" && s.currentQName != "" {
		if _, err := fmt.Fprintf(s.stdout, "%s %s\n", s.palette.label("Focus:"), shortenQName(s.info.ModulePath, s.currentQName)); err != nil {
			return err
		}
	}
	if s.currentMode == "file" && s.currentFile != "" {
		if _, err := fmt.Fprintf(s.stdout, "%s %s\n", s.palette.label("File:"), s.currentFile); err != nil {
			return err
		}
	}
	if s.changedNow > 0 {
		if _, err := fmt.Fprintf(s.stdout, "%s %d file(s) changed since the last snapshot. Use `ctx update %s` when ready.\n", s.palette.label("Index note:"), s.changedNow, s.info.Root); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(s.stdout, "%s symbol <name>, file [path], callers, callees, refs, tests, related, impact, source, copy, back, forward, report, status, quit\n\n", s.palette.label("Flow:"))
	return err
}

func (s *shellSession) writeCurrentSymbolSummary(view storage.SymbolView) error {
	impact := impactLabel(len(view.Callers), len(view.ReferencesIn), len(view.Tests), len(view.Package.ReverseDeps))
	_, err := fmt.Fprintf(
		s.stdout,
		"%s %s %s\n%s\n%s %s:%d\n\n",
		s.palette.kindBadge(view.Symbol.Kind),
		s.palette.accent(shortenQName(s.info.ModulePath, view.Symbol.QName)),
		s.palette.badge(impact),
		styleHumanSignature(s.palette, displaySignature(view.Symbol)),
		s.palette.label("Declared:"),
		view.Symbol.FilePath,
		view.Symbol.Line,
	)
	return err
}

func (s *shellSession) handle(line string) error {
	_, err := s.handleWithStop(line)
	return err
}

func (s *shellSession) handleWithStop(line string) (bool, error) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return false, nil
	}

	cmd := strings.ToLower(fields[0])
	args := fields[1:]

	switch cmd {
	case "help", "?":
		return false, s.printHelp()
	case "quit", "exit", "q":
		return true, nil
	case "clear":
		if s.currentMode == "file" && s.currentFile != "" {
			return false, s.showFileJourney(s.currentFile)
		}
		if s.currentKey != "" {
			return false, s.openSymbolKey(s.currentKey, false)
		}
		return false, s.showLanding()
	case "symbol", "s":
		if len(args) == 0 {
			if s.currentKey == "" {
				_, err := fmt.Fprintln(s.stdout, "No current symbol. Type a symbol name first.")
				return false, err
			}
			return false, s.openSymbolKey(s.currentKey, false)
		}
		return false, s.showSymbol(strings.Join(args, " "), true)
	case "impact", "i":
		query := strings.Join(args, " ")
		if query == "" {
			return false, s.showImpact("")
		}
		return false, s.showImpact(query)
	case "callers":
		return false, s.listCallers()
	case "callees":
		return false, s.listCallees()
	case "refs":
		mode := ""
		if len(args) > 0 {
			mode = strings.ToLower(args[0])
		}
		return false, s.listRefs(mode)
	case "tests":
		return false, s.listTests()
	case "related", "siblings":
		return false, s.listRelated()
	case "source", "src", "body":
		if len(args) == 1 {
			return false, s.showSourceTarget(args[0])
		}
		return false, s.showSource()
	case "file", "files":
		query := ""
		if len(args) > 0 {
			query = strings.Join(args, " ")
		}
		return false, s.showFileJourney(query)
	case "copy", "y":
		arg := ""
		if len(args) > 0 {
			arg = args[0]
		}
		return false, s.copyCurrent(arg)
	case "open", "o":
		if len(args) != 1 {
			_, err := fmt.Fprintln(s.stdout, "Usage: open <n>")
			return false, err
		}
		return false, s.openIndex(args[0])
	case "back", "b":
		return false, s.back()
	case "forward", "f":
		return false, s.forward()
	case "report":
		return false, s.showReport()
	case "status":
		return false, s.showStatus()
	case "menu", "m":
		return false, s.showAutoDrive()
	case "auto", "autodrive":
		return false, s.showAutoDrive()
	default:
		if _, err := strconv.Atoi(cmd); err == nil && len(args) == 0 {
			return false, s.openIndex(cmd)
		}
		return false, s.showSymbol(line, true)
	}
}

func (s *shellSession) printHelp() error {
	if err := s.beginScreen("Help"); err != nil {
		return err
	}
	_, err := fmt.Fprintf(
		s.stdout,
		"%s\n  symbol <query> | s <query>   open a symbol journey card\n  file [path]                travel through symbols in a file\n  menu | m                   show numbered next-step actions for current symbol\n  callers                    show direct callers and let you open them\n  callees                    show direct callees and let you open them\n  refs [in|out]              show references with use-site snippets\n  tests                      show related tests\n  related                    show sibling/nearby symbols\n  impact [query]             show impact summary for current or named symbol\n  source [n] | body [n]      show source/body for the current or listed target\n  copy [n]                   copy the current or listed target context\n  report                     project summary\n  status                     snapshot status\n  open <n>                   open item from the last numbered list\n  back / forward             navigate symbol history\n  clear                      redraw the current screen cleanly\n  quit                       exit the shell\n\n%s\n  after a symbol card, type 1..N to open the suggested next step\n  after a list, type a number to open that item directly\n  after a file journey, use source <n> to peek a body before opening it\n\n",
		s.palette.section("Shell Help"),
		s.palette.section("Number Flow"),
	)
	return err
}

func (s *shellSession) showLanding() error {
	s.currentMode = "landing"
	s.currentFile = ""
	if err := s.beginScreen("Project Entry"); err != nil {
		return err
	}
	status, err := s.store.Status(s.changedNow)
	if err != nil {
		return err
	}
	view, err := s.store.LoadReportView(4)
	if err != nil {
		return err
	}
	s.lastTargets = s.lastTargets[:0]
	if _, err := fmt.Fprintf(
		s.stdout,
		"%s\n  %s %d (%s)\n  %s packages=%d files=%d symbols=%d refs=%d calls=%d tests=%d\n  %s %d\n\n",
		s.palette.section("Snapshot"),
		s.palette.label("Current:"),
		status.Current.ID,
		status.Current.CreatedAt.Format(timeFormat),
		s.palette.label("Inventory:"),
		status.Current.TotalPackages,
		status.Current.TotalFiles,
		status.Current.TotalSymbols,
		status.Current.TotalRefs,
		status.Current.TotalCalls,
		status.Current.TotalTests,
		s.palette.label("Changed now:"),
		status.ChangedNow,
	); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		s.stdout,
		"%s\n  1. type a symbol name like %s\n  2. use %s after opening a symbol\n  3. type %s in lists to open things directly\n  4. use %s to redraw the current screen cleanly\n\n",
		s.palette.section("Quick Start"),
		s.palette.accent("Parse"),
		s.palette.accent("1..N"),
		s.palette.accent("1..N"),
		s.palette.accent("clear"),
	); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.stdout, "%s\n", s.palette.section("Project Entry")); err != nil {
		return err
	}
	if len(view.TopPackages) > 0 {
		if _, err := fmt.Fprintf(s.stdout, "  %s ", s.palette.label("Top packages:")); err != nil {
			return err
		}
		for idx, item := range view.TopPackages {
			if idx > 0 {
				if _, err := fmt.Fprint(s.stdout, ", "); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprint(s.stdout, shortenQName(s.info.ModulePath, item.Summary.ImportPath)); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(s.stdout); err != nil {
			return err
		}
	}
	if len(view.TopFunctions) > 0 {
		if _, err := fmt.Fprintf(s.stdout, "  %s\n", s.palette.label("Try one of these symbols:")); err != nil {
			return err
		}
		for idx, item := range view.TopFunctions {
			s.lastTargets = append(s.lastTargets, shellTarget{
				Kind:      "symbol",
				SymbolKey: item.Symbol.SymbolKey,
				Label:     shortenQName(s.info.ModulePath, item.Symbol.QName),
				FilePath:  item.Symbol.FilePath,
				Line:      item.Symbol.Line,
			})
			if _, err := fmt.Fprintf(s.stdout, "    %d. %s %s\n", idx+1, s.palette.kindBadge(item.Symbol.Kind), shortenQName(s.info.ModulePath, item.Symbol.QName)); err != nil {
				return err
			}
		}
	}
	hotFiles := s.landingHotFiles(view, 3)
	if len(hotFiles) > 0 {
		if _, err := fmt.Fprintf(s.stdout, "\n  %s\n", s.palette.label("Or travel by file:")); err != nil {
			return err
		}
		for _, target := range hotFiles {
			s.lastTargets = append(s.lastTargets, target)
			if _, err := fmt.Fprintf(s.stdout, "    %d. %s %s\n", len(s.lastTargets), s.palette.section("FILE"), target.FilePath); err != nil {
				return err
			}
		}
	}
	_, err = fmt.Fprintf(s.stdout, "\n%s type a symbol name, `file`, `file <path>`, or just press 1..%d to jump in.\n\n", s.palette.label("Start:"), len(s.lastTargets))
	return err
}

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
			"  [%d] %s %s\n      %s\n      %s:%d\n",
			idx+1,
			s.palette.kindBadge(match.Kind),
			shortenQName(s.info.ModulePath, match.QName),
			styleHumanSignature(s.palette, displaySignature(match)),
			match.FilePath,
			match.Line,
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

func (s *shellSession) showFileJourney(query string) error {
	relPath, focusSymbolKey, err := s.resolveFileQuery(query)
	if err != nil {
		return s.printShellError(err)
	}

	symbols, err := s.store.LoadFileSymbols(relPath)
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
	return s.renderFileJourney(relPath, focusSymbolKey, symbols)
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

	if target, ok := s.targetFromArg(query); ok {
		if target.FilePath != "" {
			return target.FilePath, target.SymbolKey, nil
		}
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
			text = fmt.Sprintf("%s\n%s:%d", displaySignature(view.Symbol), view.Symbol.FilePath, view.Symbol.Line)
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
		text = fmt.Sprintf("%s\n%s:%d", displaySignature(view.Symbol), view.Symbol.FilePath, view.Symbol.Line)
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

func (s *shellSession) showImpact(query string) error {
	symbolKey := s.currentKey
	if strings.TrimSpace(query) != "" {
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
	if symbolKey == "" {
		_, err := fmt.Fprintln(s.stdout, "No current symbol. Type a symbol name first.")
		return err
	}

	s.currentKey = symbolKey
	view, err := s.store.LoadImpactView(symbolKey, 3)
	if err != nil {
		return err
	}
	s.currentQName = view.Target.QName
	s.currentMode = "symbol"
	s.currentFile = view.Target.FilePath
	if err := s.beginScreen("Impact"); err != nil {
		return err
	}
	if err := renderHumanImpactView(s.stdout, s.info.Root, s.info.ModulePath, view, 3); err != nil {
		return err
	}
	currentView, err := s.store.LoadSymbolView(symbolKey)
	if err != nil {
		return err
	}
	return s.showAutoDriveForView(currentView)
}

func (s *shellSession) listCallers() error {
	view, err := s.currentView()
	if err != nil {
		return s.printShellError(err)
	}
	return s.renderRelatedList("Direct Callers", view.Callers)
}

func (s *shellSession) listCallees() error {
	view, err := s.currentView()
	if err != nil {
		return s.printShellError(err)
	}
	return s.renderRelatedList("Direct Callees", view.Callees)
}

func (s *shellSession) listRefs(mode string) error {
	view, err := s.currentView()
	if err != nil {
		return s.printShellError(err)
	}

	switch mode {
	case "", "in":
		return s.renderRefList("References In", view.ReferencesIn)
	case "out":
		return s.renderRefList("References Out", view.ReferencesOut)
	default:
		_, err := fmt.Fprintln(s.stdout, "Usage: refs [in|out]")
		return err
	}
}

func (s *shellSession) listTests() error {
	view, err := s.currentView()
	if err != nil {
		return s.printShellError(err)
	}
	if err := s.beginScreen("Related Tests"); err != nil {
		return err
	}
	if err := s.writeCurrentSymbolSummary(view); err != nil {
		return err
	}

	s.lastTargets = s.lastTargets[:0]
	if _, err := fmt.Fprintf(s.stdout, "%s (%d)\n", s.palette.section("Related Tests"), len(view.Tests)); err != nil {
		return err
	}
	if len(view.Tests) == 0 {
		_, err := fmt.Fprintln(s.stdout, "  none")
		return err
	}
	for idx, test := range view.Tests[:min(shellListLimit, len(view.Tests))] {
		s.lastTargets = append(s.lastTargets, shellTarget{
			Kind:     "location",
			Label:    test.Name,
			FilePath: test.FilePath,
			Line:     test.Line,
		})
		if _, err := fmt.Fprintf(
			s.stdout,
			"  [%d] %s %s\n      %s:%d [%s/%s]\n",
			idx+1,
			s.palette.kindBadge(test.Kind),
			test.Name,
			test.FilePath,
			test.Line,
			test.LinkKind,
			test.Confidence,
		); err != nil {
			return err
		}
		if snippet := s.previewLine(test.FilePath, test.Line); snippet != "" {
			if _, err := fmt.Fprintf(s.stdout, "      %s\n", snippet); err != nil {
				return err
			}
		}
	}
	if len(view.Tests) > shellListLimit {
		if _, err := fmt.Fprintf(s.stdout, "  %s and %d more\n", s.palette.muted("..."), len(view.Tests)-shellListLimit); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(s.stdout)
	return err
}

func (s *shellSession) listRelated() error {
	view, err := s.currentView()
	if err != nil {
		return s.printShellError(err)
	}
	if err := s.beginScreen("Related Symbols"); err != nil {
		return err
	}
	if err := s.writeCurrentSymbolSummary(view); err != nil {
		return err
	}

	s.lastTargets = s.lastTargets[:0]
	if _, err := fmt.Fprintf(s.stdout, "%s (%d)\n", s.palette.section("Related Symbols"), len(view.Siblings)); err != nil {
		return err
	}
	if len(view.Siblings) == 0 {
		_, err := fmt.Fprintln(s.stdout, "  none")
		return err
	}
	for idx, symbol := range view.Siblings[:min(shellListLimit, len(view.Siblings))] {
		s.lastTargets = append(s.lastTargets, shellTarget{
			Kind:      "symbol",
			SymbolKey: symbol.SymbolKey,
			Label:     shortenQName(s.info.ModulePath, symbol.QName),
			FilePath:  symbol.FilePath,
			Line:      symbol.Line,
		})
		if _, err := fmt.Fprintf(
			s.stdout,
			"  [%d] %s %s\n      %s\n      %s:%d\n",
			idx+1,
			s.palette.kindBadge(symbol.Kind),
			shortenQName(s.info.ModulePath, symbol.QName),
			styleHumanSignature(s.palette, displaySignature(symbol)),
			symbol.FilePath,
			symbol.Line,
		); err != nil {
			return err
		}
	}
	if len(view.Siblings) > shellListLimit {
		if _, err := fmt.Fprintf(s.stdout, "  %s and %d more\n", s.palette.muted("..."), len(view.Siblings)-shellListLimit); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(s.stdout)
	return err
}

func (s *shellSession) showSource() error {
	view, err := s.currentView()
	if err != nil {
		return s.printShellError(err)
	}

	if err := s.beginScreen("Source"); err != nil {
		return err
	}
	if err := s.writeCurrentSymbolSummary(view); err != nil {
		return err
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
	status, err := s.store.Status(s.changedNow)
	if err != nil {
		return err
	}
	view, err := s.store.LoadReportView(6)
	if err != nil {
		return err
	}
	s.currentMode = "landing"
	s.currentFile = ""
	if err := s.beginScreen("Project Report"); err != nil {
		return err
	}
	if err := renderHumanReport(s.stdout, s.info.Root, s.info.ModulePath, status, view); err != nil {
		return err
	}
	s.lastTargets = nil
	return nil
}

func (s *shellSession) showStatus() error {
	status, err := s.store.Status(s.changedNow)
	if err != nil {
		return err
	}
	s.currentMode = "landing"
	s.currentFile = ""
	if err := s.beginScreen("Status"); err != nil {
		return err
	}
	if !status.HasSnapshot {
		_, err := fmt.Fprintf(s.stdout, "Root: %s\nModule: %s\nSnapshot: none\nChanged now: %d\n\n", s.info.Root, s.info.ModulePath, s.changedNow)
		return err
	}
	_, err = fmt.Fprintf(
		s.stdout,
		"%s\n  Root: %s\n  Module: %s\n  Snapshot: %d (%s)\n  Inventory: packages=%d files=%d symbols=%d refs=%d calls=%d tests=%d\n  Changed now: %d\n\n",
		s.palette.section("Status"),
		status.RootPath,
		status.ModulePath,
		status.Current.ID,
		status.Current.CreatedAt.Format(timeFormat),
		status.Current.TotalPackages,
		status.Current.TotalFiles,
		status.Current.TotalSymbols,
		status.Current.TotalRefs,
		status.Current.TotalCalls,
		status.Current.TotalTests,
		status.ChangedNow,
	)
	s.lastTargets = nil
	return err
}

func (s *shellSession) showAutoDrive() error {
	view, err := s.currentView()
	if err != nil {
		return s.printShellError(err)
	}
	if err := s.beginScreen("Next Steps"); err != nil {
		return err
	}
	if err := s.writeCurrentSymbolSummary(view); err != nil {
		return err
	}
	return s.showAutoDriveForView(view)
}

func (s *shellSession) showAutoDriveForView(view storage.SymbolView) error {
	s.lastTargets = []shellTarget{
		{Kind: "action", Action: "file"},
		{Kind: "action", Action: "callers"},
		{Kind: "action", Action: "callees"},
		{Kind: "action", Action: "refs_in"},
		{Kind: "action", Action: "refs_out"},
		{Kind: "action", Action: "tests"},
		{Kind: "action", Action: "related"},
		{Kind: "action", Action: "impact"},
		{Kind: "action", Action: "source"},
		{Kind: "action", Action: "copy"},
	}

	if _, err := fmt.Fprintf(
		s.stdout,
		"%s\n  [1] File Journey        walk the current file\n  [2] Callers (%d)        follow who reaches this symbol\n  [3] Callees (%d)        follow what this symbol uses\n  [4] Refs In (%d)        inspect incoming references\n  [5] Refs Out (%d)       inspect outgoing references\n  [6] Tests (%d)          jump to related tests\n  [7] Related (%d)        nearby symbols in the same area\n  [8] Impact              show broader blast radius\n  [9] Source / Body       open a wider source excerpt\n  [10] Copy               copy signature + location\n\n%s type a number, or use file/callers/callees/refs/tests/related/impact/source/copy directly\n\n",
		s.palette.section("Next Steps"),
		len(view.Callers),
		len(view.Callees),
		len(view.ReferencesIn),
		len(view.ReferencesOut),
		len(view.Tests),
		len(view.Siblings),
		s.palette.label("Flow:"),
	); err != nil {
		return err
	}
	return nil
}

func (s *shellSession) runAction(action string) error {
	switch action {
	case "file":
		return s.showFileJourney("")
	case "callers":
		return s.listCallers()
	case "callees":
		return s.listCallees()
	case "refs_in":
		return s.listRefs("in")
	case "refs_out":
		return s.listRefs("out")
	case "tests":
		return s.listTests()
	case "related":
		return s.listRelated()
	case "impact":
		return s.showImpact("")
	case "source":
		return s.showSource()
	case "copy":
		return s.copyCurrent("")
	default:
		_, err := fmt.Fprintf(s.stdout, "Unknown action %q\n\n", action)
		return err
	}
}

func (s *shellSession) renderRelatedList(title string, values []storage.RelatedSymbolView) error {
	view, err := s.currentView()
	if err == nil {
		if err := s.beginScreen(title); err != nil {
			return err
		}
		if err := s.writeCurrentSymbolSummary(view); err != nil {
			return err
		}
	}
	s.lastTargets = s.lastTargets[:0]
	if _, err := fmt.Fprintf(s.stdout, "%s (%d)\n", s.palette.section(title), len(values)); err != nil {
		return err
	}
	if len(values) == 0 {
		_, err := fmt.Fprintln(s.stdout, "  none")
		return err
	}
	for idx, value := range values[:min(shellListLimit, len(values))] {
		s.lastTargets = append(s.lastTargets, shellTarget{
			Kind:      "symbol",
			SymbolKey: value.Symbol.SymbolKey,
			Label:     shortenQName(s.info.ModulePath, value.Symbol.QName),
			FilePath:  value.UseFilePath,
			Line:      value.UseLine,
		})
		if _, err := fmt.Fprintf(
			s.stdout,
			"  [%d] %s %s\n      %s\n      declared: %s:%d\n      use: %s:%d",
			idx+1,
			s.palette.kindBadge(value.Symbol.Kind),
			shortenQName(s.info.ModulePath, value.Symbol.QName),
			styleHumanSignature(s.palette, displaySignature(value.Symbol)),
			value.Symbol.FilePath,
			value.Symbol.Line,
			value.UseFilePath,
			value.UseLine,
		); err != nil {
			return err
		}
		if value.Relation != "" {
			if _, err := fmt.Fprintf(s.stdout, " [%s]", value.Relation); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(s.stdout); err != nil {
			return err
		}
		if snippet := s.previewLine(value.UseFilePath, value.UseLine); snippet != "" {
			if _, err := fmt.Fprintf(s.stdout, "      %s\n", snippet); err != nil {
				return err
			}
		}
	}
	if len(values) > shellListLimit {
		if _, err := fmt.Fprintf(s.stdout, "  %s and %d more\n", s.palette.muted("..."), len(values)-shellListLimit); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(s.stdout)
	return err
}

func (s *shellSession) renderRefList(title string, values []storage.RefView) error {
	view, err := s.currentView()
	if err == nil {
		if err := s.beginScreen(title); err != nil {
			return err
		}
		if err := s.writeCurrentSymbolSummary(view); err != nil {
			return err
		}
	}
	s.lastTargets = s.lastTargets[:0]
	if _, err := fmt.Fprintf(s.stdout, "%s (%d)\n", s.palette.section(title), len(values)); err != nil {
		return err
	}
	if len(values) == 0 {
		_, err := fmt.Fprintln(s.stdout, "  none")
		return err
	}
	for idx, value := range values[:min(shellListLimit, len(values))] {
		s.lastTargets = append(s.lastTargets, shellTarget{
			Kind:      "symbol",
			SymbolKey: value.Symbol.SymbolKey,
			Label:     shortenQName(s.info.ModulePath, value.Symbol.QName),
			FilePath:  value.UseFilePath,
			Line:      value.UseLine,
		})
		if _, err := fmt.Fprintf(
			s.stdout,
			"  [%d] %s %s\n      %s\n      declared: %s:%d\n      ref: %s:%d [%s]\n",
			idx+1,
			s.palette.kindBadge(value.Symbol.Kind),
			shortenQName(s.info.ModulePath, value.Symbol.QName),
			styleHumanSignature(s.palette, displaySignature(value.Symbol)),
			value.Symbol.FilePath,
			value.Symbol.Line,
			value.UseFilePath,
			value.UseLine,
			value.Kind,
		); err != nil {
			return err
		}
		if snippet := s.previewLine(value.UseFilePath, value.UseLine); snippet != "" {
			if _, err := fmt.Fprintf(s.stdout, "      %s\n", snippet); err != nil {
				return err
			}
		}
	}
	if len(values) > shellListLimit {
		if _, err := fmt.Fprintf(s.stdout, "  %s and %d more\n", s.palette.muted("..."), len(values)-shellListLimit); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(s.stdout)
	return err
}

func (s *shellSession) previewLine(relPath string, line int) string {
	excerpt, err := readSourceExcerpt(s.info.Root, relPath, line, 0, 0)
	if err != nil || excerpt == "" {
		return ""
	}
	parts := strings.Split(strings.TrimSpace(excerpt), "|")
	if len(parts) < 2 {
		return strings.TrimSpace(excerpt)
	}
	return strings.TrimSpace(parts[len(parts)-1])
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
