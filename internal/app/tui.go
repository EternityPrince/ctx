package app

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/vladimirkasterin/ctx/internal/cli"
	"github.com/vladimirkasterin/ctx/internal/clipboard"
	"github.com/vladimirkasterin/ctx/internal/project"
	"github.com/vladimirkasterin/ctx/internal/storage"
)

const (
	tuiModeLanding = "landing"
	tuiModeSearch  = "search"
	tuiModeSymbol  = "symbol"
	tuiModeFile    = "file"
)

type tuiHistoryEntry struct {
	Mode      string
	SymbolKey string
	FilePath  string
	Line      int
}

type tuiSection struct {
	Title string
	Items []tuiItem
	Empty string
}

type tuiItem struct {
	Kind        string
	Title       string
	Subtitle    string
	Detail      string
	Preview     string
	SymbolKey   string
	Symbol      storage.SymbolMatch
	FilePath    string
	Line        int
	CopyText    string
	Relation    string
	Importance  string
	Score       int
	OpenInFile  bool
	PackageName string
}

type tuiModel struct {
	info         project.Info
	store        *storage.Store
	stdout       *os.File
	palette      palette
	changedNow   int
	status       storage.ProjectStatus
	report       storage.ReportView
	initialQuery string
	mode         string
	current      storage.SymbolView
	currentFile  string
	currentLine  int
	fileSymbols  []storage.SymbolMatch
	searchQuery  string
	searchItems  []storage.SymbolMatch
	sections     []tuiSection
	sectionIndex int
	itemIndex    int
	history      []tuiHistoryEntry
	historyIndex int
	showSource   bool
	message      string
	inputMode    string
	inputValue   string
}

func runShell(command cli.Command, stdout io.Writer) error {
	if os.Getenv("CTX_EXPERIMENTAL_TUI") != "1" {
		return runShellREPL(command, stdout)
	}
	if !shouldUseTUI(stdout) {
		tuiDebugf("tui disabled: stdout is not an interactive terminal")
		return runShellREPL(command, stdout)
	}

	state, err := openPreparedProjectState(command)
	if err != nil {
		return err
	}
	defer state.Close()

	changedNow := projectService.ChangedNow(state)
	status, err := state.Store.Status(changedNow)
	if err != nil {
		return err
	}
	if !status.HasSnapshot {
		_, err := fmt.Fprintf(stdout, "No index snapshot for %s. Run `ctx index %s` first.\n", state.Info.ModulePath, state.Info.Root)
		return err
	}

	report, err := state.Store.LoadReportView(8)
	if err != nil {
		return err
	}

	file := stdout.(*os.File)
	model := &tuiModel{
		info:         state.Info,
		store:        state.Store,
		stdout:       file,
		palette:      newPalette(),
		changedNow:   changedNow,
		status:       status,
		report:       report,
		initialQuery: command.Query,
		mode:         tuiModeLanding,
		historyIndex: -1,
	}
	if err := model.loadLanding(); err != nil {
		return err
	}
	if strings.TrimSpace(command.Query) != "" {
		if err := model.search(strings.TrimSpace(command.Query), true); err != nil {
			return err
		}
	}

	if err := model.run(); err != nil {
		return err
	}
	return nil
}

func (m *tuiModel) run() error {
	term, err := enterTerminal(m.stdout)
	if err != nil {
		tuiDebugf("enter terminal failed: %v", err)
		return runShellREPL(cli.Command{
			Name:  "shell",
			Root:  m.info.Root,
			Query: m.initialQuery,
		}, m.stdout)
	}
	defer term.Restore()

	reader := bufio.NewReader(os.Stdin)
	for {
		if err := m.draw(); err != nil {
			return err
		}

		key, err := readTUIKey(reader)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		stop, err := m.handleKey(key)
		if err != nil {
			return err
		}
		if stop {
			return nil
		}
	}
}

func (m *tuiModel) handleKey(key tuiKey) (bool, error) {
	if m.inputMode != "" {
		return m.handleInputKey(key)
	}

	switch key.Name {
	case "quit":
		return true, nil
	case "up":
		m.moveItem(-1)
	case "down":
		m.moveItem(1)
	case "left":
		m.moveSection(-1)
	case "right", "tab":
		m.moveSection(1)
	case "enter":
		return false, m.openSelected(true)
	case "backspace":
		return false, nil
	case "rune":
		return m.handleRune(key.Rune)
	}
	return false, nil
}

func (m *tuiModel) handleInputKey(key tuiKey) (bool, error) {
	switch key.Name {
	case "escape":
		m.inputMode = ""
		m.inputValue = ""
		m.message = "Search cancelled"
		return false, nil
	case "enter":
		query := strings.TrimSpace(m.inputValue)
		m.inputMode = ""
		m.inputValue = ""
		if query == "" {
			m.message = "Type a symbol name to search"
			return false, nil
		}
		return false, m.search(query, true)
	case "backspace":
		if len(m.inputValue) > 0 {
			m.inputValue = m.inputValue[:len(m.inputValue)-1]
		}
		return false, nil
	case "rune":
		if key.Rune >= 32 && key.Rune <= 126 {
			m.inputValue += string(key.Rune)
		}
		return false, nil
	default:
		return false, nil
	}
}

func (m *tuiModel) handleRune(r rune) (bool, error) {
	switch r {
	case 'q':
		return true, nil
	case 'k':
		m.moveItem(-1)
	case 'j':
		m.moveItem(1)
	case 'h':
		m.moveSection(-1)
	case 'l':
		m.moveSection(1)
	case '/':
		m.inputMode = "search"
		m.inputValue = ""
		m.message = "Search for a function, method, or type"
	case 'g':
		return false, m.loadLanding()
	case 'b':
		return false, m.back()
	case 'n':
		return false, m.forward()
	case 'v', ' ':
		m.showSource = !m.showSource
		if m.showSource {
			m.message = "Expanded source view"
		} else {
			m.message = "Compact preview view"
		}
	case 'y':
		return false, m.copySelection()
	case 'o':
		return false, m.openSelectionFile()
	case 'f':
		return false, m.openCurrentFile()
	case 'r':
		return false, m.reloadSummary()
	default:
		if r >= '1' && r <= '9' {
			return false, m.openIndex(int(r - '1'))
		}
	}
	return false, nil
}

func (m *tuiModel) reloadSummary() error {
	status, err := m.store.Status(m.changedNow)
	if err != nil {
		return err
	}
	report, err := m.store.LoadReportView(8)
	if err != nil {
		return err
	}
	m.status = status
	m.report = report
	m.message = "Summary reloaded"
	if m.mode == tuiModeLanding {
		return m.loadLanding()
	}
	return nil
}

func (m *tuiModel) search(query string, focusMessage bool) error {
	matches, err := m.store.FindSymbols(query)
	if err != nil {
		return err
	}
	if len(matches) == 0 {
		m.message = fmt.Sprintf("No symbol matches for %q", query)
		return nil
	}
	if len(matches) == 1 {
		return m.openSymbol(matches[0].SymbolKey, true)
	}

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Kind != matches[j].Kind {
			return matches[i].Kind < matches[j].Kind
		}
		return matches[i].QName < matches[j].QName
	})
	m.mode = tuiModeSearch
	m.searchQuery = query
	m.searchItems = matches
	m.sections = []tuiSection{{
		Title: "Matches",
		Items: m.symbolSearchItems(matches),
		Empty: "No matches",
	}}
	m.sectionIndex = 0
	m.itemIndex = 0
	m.showSource = false
	if focusMessage {
		m.message = fmt.Sprintf("%d matches for %q", len(matches), query)
	}
	return nil
}

func (m *tuiModel) loadLanding() error {
	m.mode = tuiModeLanding
	m.showSource = false
	m.searchQuery = ""

	m.sections = []tuiSection{
		{
			Title: "Start Here",
			Items: m.rankedFunctionItems(m.report.TopFunctions),
			Empty: "Index the project to discover entry points",
		},
		{
			Title: "Important Types",
			Items: m.rankedTypeItems(m.report.TopTypes),
			Empty: "No indexed types yet",
		},
		{
			Title: "Hot Files",
			Items: m.hotFileItems(),
			Empty: "No file hints yet",
		},
	}
	m.sectionIndex = 0
	m.itemIndex = 0
	m.message = "Search with /, move with arrows or hjkl, open with Enter, copy with y"
	return nil
}

func (m *tuiModel) openSymbol(symbolKey string, pushHistory bool) error {
	view, err := m.store.LoadSymbolView(symbolKey)
	if err != nil {
		return err
	}
	fileSymbols, err := m.store.LoadFileSymbols(view.Symbol.FilePath)
	if err != nil {
		return err
	}

	m.current = view
	m.currentFile = view.Symbol.FilePath
	m.currentLine = view.Symbol.Line
	m.fileSymbols = fileSymbols
	m.mode = tuiModeSymbol
	m.showSource = false
	m.sections = m.symbolSections(view, fileSymbols)
	m.sectionIndex = 0
	m.itemIndex = m.indexOfFileSymbol(fileSymbols, view.Symbol.SymbolKey)
	if pushHistory {
		m.pushHistory(tuiHistoryEntry{
			Mode:      tuiModeSymbol,
			SymbolKey: symbolKey,
			FilePath:  view.Symbol.FilePath,
			Line:      view.Symbol.Line,
		})
	}
	m.message = fmt.Sprintf("Opened %s", shortenQName(m.info.ModulePath, view.Symbol.QName))
	return nil
}

func (m *tuiModel) openFile(filePath string, line int, pushHistory bool) error {
	symbols, err := m.store.LoadFileSymbols(filePath)
	if err != nil {
		return err
	}
	m.mode = tuiModeFile
	m.currentFile = filePath
	m.currentLine = line
	m.fileSymbols = symbols
	m.sections = []tuiSection{{
		Title: "Symbols",
		Items: m.fileItems(symbols),
		Empty: "No indexed symbols in this file",
	}}
	m.sectionIndex = 0
	m.itemIndex = m.closestLineIndex(symbols, line)
	m.showSource = false
	if pushHistory {
		m.pushHistory(tuiHistoryEntry{
			Mode:     tuiModeFile,
			FilePath: filePath,
			Line:     line,
		})
	}
	m.message = fmt.Sprintf("Opened file %s", filePath)
	return nil
}

func (m *tuiModel) symbolSections(view storage.SymbolView, fileSymbols []storage.SymbolMatch) []tuiSection {
	return []tuiSection{
		{Title: "File Map", Items: m.fileItems(fileSymbols), Empty: "No file symbols"},
		{Title: "Callers", Items: m.relatedItems(view.Callers), Empty: "No direct callers"},
		{Title: "Callees", Items: m.relatedItems(view.Callees), Empty: "No direct callees"},
		{Title: "Refs In", Items: m.refItems(view.ReferencesIn), Empty: "No inbound refs"},
		{Title: "Refs Out", Items: m.refItems(view.ReferencesOut), Empty: "No outbound refs"},
		{Title: "Tests", Items: m.testItems(view.Tests), Empty: "No related tests"},
		{Title: "Related", Items: m.siblingItems(view.Siblings), Empty: "No nearby symbols"},
	}
}

func (m *tuiModel) moveSection(delta int) {
	if len(m.sections) == 0 {
		return
	}
	m.sectionIndex = (m.sectionIndex + delta + len(m.sections)) % len(m.sections)
	items := m.sections[m.sectionIndex].Items
	if len(items) == 0 {
		m.itemIndex = 0
		return
	}
	if m.itemIndex >= len(items) {
		m.itemIndex = len(items) - 1
	}
	if m.itemIndex < 0 {
		m.itemIndex = 0
	}
}

func (m *tuiModel) moveItem(delta int) {
	section, ok := m.currentSection()
	if !ok || len(section.Items) == 0 {
		m.itemIndex = 0
		return
	}
	m.itemIndex += delta
	if m.itemIndex < 0 {
		m.itemIndex = 0
	}
	if m.itemIndex >= len(section.Items) {
		m.itemIndex = len(section.Items) - 1
	}
}

func (m *tuiModel) openIndex(index int) error {
	section, ok := m.currentSection()
	if !ok {
		return nil
	}
	if index < 0 || index >= len(section.Items) {
		m.message = fmt.Sprintf("No item %d in %s", index+1, section.Title)
		return nil
	}
	m.itemIndex = index
	return m.openSelected(true)
}

func (m *tuiModel) openSelected(pushHistory bool) error {
	item, ok := m.selectedItem()
	if !ok {
		return nil
	}
	switch item.Kind {
	case "symbol":
		return m.openSymbol(item.SymbolKey, pushHistory)
	case "file":
		return m.openFile(item.FilePath, item.Line, pushHistory)
	case "location":
		return m.openFile(item.FilePath, item.Line, pushHistory)
	default:
		return nil
	}
}

func (m *tuiModel) openSelectionFile() error {
	item, ok := m.selectedItem()
	if ok && item.FilePath != "" {
		return m.openFile(item.FilePath, item.Line, true)
	}
	return m.openCurrentFile()
}

func (m *tuiModel) openCurrentFile() error {
	switch m.mode {
	case tuiModeSymbol:
		return m.openFile(m.current.Symbol.FilePath, m.current.Symbol.Line, true)
	case tuiModeFile:
		return nil
	default:
		item, ok := m.selectedItem()
		if ok && item.FilePath != "" {
			return m.openFile(item.FilePath, item.Line, true)
		}
	}
	m.message = "No file is selected"
	return nil
}

func (m *tuiModel) back() error {
	if m.historyIndex <= 0 {
		m.message = "No previous view"
		return nil
	}
	m.historyIndex--
	return m.restoreHistory(m.history[m.historyIndex])
}

func (m *tuiModel) forward() error {
	if m.historyIndex < 0 || m.historyIndex+1 >= len(m.history) {
		m.message = "No next view"
		return nil
	}
	m.historyIndex++
	return m.restoreHistory(m.history[m.historyIndex])
}

func (m *tuiModel) restoreHistory(entry tuiHistoryEntry) error {
	switch entry.Mode {
	case tuiModeSymbol:
		return m.openSymbol(entry.SymbolKey, false)
	case tuiModeFile:
		return m.openFile(entry.FilePath, entry.Line, false)
	default:
		return m.loadLanding()
	}
}

func (m *tuiModel) pushHistory(entry tuiHistoryEntry) {
	if entry.Mode == "" {
		return
	}
	if m.historyIndex >= 0 && m.historyIndex < len(m.history) {
		current := m.history[m.historyIndex]
		if current == entry {
			return
		}
	}
	if m.historyIndex+1 < len(m.history) {
		m.history = append([]tuiHistoryEntry{}, m.history[:m.historyIndex+1]...)
	}
	m.history = append(m.history, entry)
	m.historyIndex = len(m.history) - 1
}

func (m *tuiModel) copySelection() error {
	text := ""
	item, ok := m.selectedItem()
	if ok && item.CopyText != "" {
		text = item.CopyText
	}
	if text == "" && m.mode == tuiModeSymbol {
		text = fmt.Sprintf("%s\n%s:%d", displaySignature(m.current.Symbol), m.current.Symbol.FilePath, m.current.Symbol.Line)
	}
	if text == "" && m.mode == tuiModeFile {
		text = m.currentFile
	}
	if text == "" {
		m.message = "Nothing to copy"
		return nil
	}
	if err := clipboard.Copy(text); err != nil {
		m.message = fmt.Sprintf("Copy failed: %v", err)
		return nil
	}
	m.message = "Copied current selection to clipboard"
	return nil
}

func (m *tuiModel) currentSection() (tuiSection, bool) {
	if len(m.sections) == 0 || m.sectionIndex < 0 || m.sectionIndex >= len(m.sections) {
		return tuiSection{}, false
	}
	return m.sections[m.sectionIndex], true
}

func (m *tuiModel) selectedItem() (tuiItem, bool) {
	section, ok := m.currentSection()
	if !ok || len(section.Items) == 0 {
		return tuiItem{}, false
	}
	index := m.itemIndex
	if index < 0 {
		index = 0
	}
	if index >= len(section.Items) {
		index = len(section.Items) - 1
	}
	return section.Items[index], true
}

func (m *tuiModel) rankedFunctionItems(values []storage.RankedSymbol) []tuiItem {
	items := make([]tuiItem, 0, len(values))
	for _, value := range values {
		items = append(items, tuiItem{
			Kind:       "symbol",
			Title:      shortenQName(m.info.ModulePath, value.Symbol.QName),
			Subtitle:   displaySignature(value.Symbol),
			Detail:     fmt.Sprintf("%s | callers=%d refs=%d tests=%d rdeps=%d score=%d", reportImportance(value.Score), value.CallerCount, value.ReferenceCount, value.TestCount, value.ReversePackageDeps, value.Score),
			Preview:    oneLine(value.Symbol.Doc),
			SymbolKey:  value.Symbol.SymbolKey,
			Symbol:     value.Symbol,
			FilePath:   value.Symbol.FilePath,
			Line:       value.Symbol.Line,
			CopyText:   fmt.Sprintf("%s\n%s:%d", displaySignature(value.Symbol), value.Symbol.FilePath, value.Symbol.Line),
			Importance: reportImportance(value.Score),
			Score:      value.Score,
		})
	}
	return items
}

func (m *tuiModel) rankedTypeItems(values []storage.RankedSymbol) []tuiItem {
	items := make([]tuiItem, 0, len(values))
	for _, value := range values {
		items = append(items, tuiItem{
			Kind:       "symbol",
			Title:      shortenQName(m.info.ModulePath, value.Symbol.QName),
			Subtitle:   displaySignature(value.Symbol),
			Detail:     fmt.Sprintf("%s | refs=%d tests=%d methods=%d rdeps=%d score=%d", reportImportance(value.Score), value.ReferenceCount, value.TestCount, value.MethodCount, value.ReversePackageDeps, value.Score),
			Preview:    oneLine(value.Symbol.Doc),
			SymbolKey:  value.Symbol.SymbolKey,
			Symbol:     value.Symbol,
			FilePath:   value.Symbol.FilePath,
			Line:       value.Symbol.Line,
			CopyText:   fmt.Sprintf("%s\n%s:%d", displaySignature(value.Symbol), value.Symbol.FilePath, value.Symbol.Line),
			Importance: reportImportance(value.Score),
			Score:      value.Score,
		})
	}
	return items
}

func (m *tuiModel) hotFileItems() []tuiItem {
	type fileScore struct {
		Path    string
		Score   int
		Symbols []string
		Line    int
	}
	byFile := map[string]*fileScore{}
	appendValue := func(value storage.RankedSymbol) {
		item, ok := byFile[value.Symbol.FilePath]
		if !ok {
			item = &fileScore{
				Path: value.Symbol.FilePath,
				Line: value.Symbol.Line,
			}
			byFile[value.Symbol.FilePath] = item
		}
		item.Score += value.Score
		if len(item.Symbols) < 4 {
			item.Symbols = append(item.Symbols, value.Symbol.Name)
		}
		if item.Line == 0 || value.Symbol.Line < item.Line {
			item.Line = value.Symbol.Line
		}
	}
	for _, value := range m.report.TopFunctions {
		appendValue(value)
	}
	for _, value := range m.report.TopTypes {
		appendValue(value)
	}

	keys := make([]string, 0, len(byFile))
	for key := range byFile {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left := byFile[keys[i]]
		right := byFile[keys[j]]
		if left.Score != right.Score {
			return left.Score > right.Score
		}
		return left.Path < right.Path
	})

	items := make([]tuiItem, 0, len(keys))
	for _, key := range keys {
		value := byFile[key]
		items = append(items, tuiItem{
			Kind:     "file",
			Title:    value.Path,
			Subtitle: fmt.Sprintf("score=%d | symbols=%s", value.Score, strings.Join(value.Symbols, ", ")),
			Detail:   "Open file travel mode",
			Preview:  fmt.Sprintf("Highlighted symbols: %s", strings.Join(value.Symbols, ", ")),
			FilePath: value.Path,
			Line:     value.Line,
			CopyText: value.Path,
		})
	}
	return items
}

func (m *tuiModel) symbolSearchItems(values []storage.SymbolMatch) []tuiItem {
	items := make([]tuiItem, 0, len(values))
	for _, symbol := range values {
		items = append(items, tuiItem{
			Kind:      "symbol",
			Title:     shortenQName(m.info.ModulePath, symbol.QName),
			Subtitle:  displaySignature(symbol),
			Detail:    fmt.Sprintf("%s:%d", symbol.FilePath, symbol.Line),
			Preview:   oneLine(symbol.Doc),
			SymbolKey: symbol.SymbolKey,
			Symbol:    symbol,
			FilePath:  symbol.FilePath,
			Line:      symbol.Line,
			CopyText:  fmt.Sprintf("%s\n%s:%d", displaySignature(symbol), symbol.FilePath, symbol.Line),
		})
	}
	return items
}

func (m *tuiModel) fileItems(values []storage.SymbolMatch) []tuiItem {
	items := make([]tuiItem, 0, len(values))
	for _, symbol := range values {
		items = append(items, tuiItem{
			Kind:      "symbol",
			Title:     symbol.Name,
			Subtitle:  displaySignature(symbol),
			Detail:    fmt.Sprintf("%s | %s:%d", symbol.Kind, symbol.FilePath, symbol.Line),
			Preview:   oneLine(symbol.Doc),
			SymbolKey: symbol.SymbolKey,
			Symbol:    symbol,
			FilePath:  symbol.FilePath,
			Line:      symbol.Line,
			CopyText:  fmt.Sprintf("%s\n%s:%d", displaySignature(symbol), symbol.FilePath, symbol.Line),
		})
	}
	return items
}

func (m *tuiModel) relatedItems(values []storage.RelatedSymbolView) []tuiItem {
	items := make([]tuiItem, 0, len(values))
	for _, value := range values {
		detail := fmt.Sprintf("decl %s:%d | use %s:%d", value.Symbol.FilePath, value.Symbol.Line, value.UseFilePath, value.UseLine)
		if value.Relation != "" {
			detail += " | " + value.Relation
		}
		items = append(items, tuiItem{
			Kind:       "symbol",
			Title:      shortenQName(m.info.ModulePath, value.Symbol.QName),
			Subtitle:   displaySignature(value.Symbol),
			Detail:     detail,
			Preview:    sourceLineSnippet(m.info.Root, value.UseFilePath, value.UseLine),
			SymbolKey:  value.Symbol.SymbolKey,
			Symbol:     value.Symbol,
			FilePath:   value.UseFilePath,
			Line:       value.UseLine,
			CopyText:   fmt.Sprintf("%s\nuse %s:%d", displaySignature(value.Symbol), value.UseFilePath, value.UseLine),
			Relation:   value.Relation,
			OpenInFile: true,
		})
	}
	return items
}

func (m *tuiModel) refItems(values []storage.RefView) []tuiItem {
	items := make([]tuiItem, 0, len(values))
	for _, value := range values {
		items = append(items, tuiItem{
			Kind:       "symbol",
			Title:      shortenQName(m.info.ModulePath, value.Symbol.QName),
			Subtitle:   displaySignature(value.Symbol),
			Detail:     fmt.Sprintf("decl %s:%d | ref %s:%d | %s", value.Symbol.FilePath, value.Symbol.Line, value.UseFilePath, value.UseLine, value.Kind),
			Preview:    sourceLineSnippet(m.info.Root, value.UseFilePath, value.UseLine),
			SymbolKey:  value.Symbol.SymbolKey,
			Symbol:     value.Symbol,
			FilePath:   value.UseFilePath,
			Line:       value.UseLine,
			CopyText:   fmt.Sprintf("%s\nref %s:%d", displaySignature(value.Symbol), value.UseFilePath, value.UseLine),
			Relation:   value.Kind,
			OpenInFile: true,
		})
	}
	return items
}

func (m *tuiModel) testItems(values []storage.TestView) []tuiItem {
	items := make([]tuiItem, 0, len(values))
	for _, value := range values {
		items = append(items, tuiItem{
			Kind:       "location",
			Title:      value.Name,
			Subtitle:   fmt.Sprintf("%s [%s/%s]", value.FilePath, value.LinkKind, value.Confidence),
			Detail:     fmt.Sprintf("%s:%d", value.FilePath, value.Line),
			Preview:    sourceLineSnippet(m.info.Root, value.FilePath, value.Line),
			FilePath:   value.FilePath,
			Line:       value.Line,
			CopyText:   fmt.Sprintf("%s\n%s:%d", value.Name, value.FilePath, value.Line),
			Relation:   value.LinkKind,
			OpenInFile: true,
		})
	}
	return items
}

func (m *tuiModel) siblingItems(values []storage.SymbolMatch) []tuiItem {
	items := make([]tuiItem, 0, len(values))
	for _, symbol := range values {
		items = append(items, tuiItem{
			Kind:      "symbol",
			Title:     shortenQName(m.info.ModulePath, symbol.QName),
			Subtitle:  displaySignature(symbol),
			Detail:    fmt.Sprintf("%s:%d", symbol.FilePath, symbol.Line),
			Preview:   oneLine(symbol.Doc),
			SymbolKey: symbol.SymbolKey,
			Symbol:    symbol,
			FilePath:  symbol.FilePath,
			Line:      symbol.Line,
			CopyText:  fmt.Sprintf("%s\n%s:%d", displaySignature(symbol), symbol.FilePath, symbol.Line),
		})
	}
	return items
}

func (m *tuiModel) indexOfFileSymbol(values []storage.SymbolMatch, symbolKey string) int {
	for idx, value := range values {
		if value.SymbolKey == symbolKey {
			return idx
		}
	}
	return 0
}

func (m *tuiModel) closestLineIndex(values []storage.SymbolMatch, line int) int {
	if len(values) == 0 {
		return 0
	}
	best := 0
	bestDelta := abs(values[0].Line - line)
	for idx := 1; idx < len(values); idx++ {
		delta := abs(values[idx].Line - line)
		if delta < bestDelta {
			best = idx
			bestDelta = delta
		}
	}
	return best
}

func (m *tuiModel) draw() error {
	width, height := terminalSize()
	if width < 80 || height < 20 {
		_, err := fmt.Fprintf(m.stdout, "\x1b[H\x1b[2J%s\nResize the terminal to at least 80x20.\n", m.palette.title("CTX TUI"))
		return err
	}

	leftWidth := width / 3
	if leftWidth < 34 {
		leftWidth = 34
	}
	if leftWidth > 48 {
		leftWidth = 48
	}
	rightWidth := width - leftWidth - 3
	bodyHeight := height - 6

	left := m.renderLeftPane(leftWidth, bodyHeight)
	right := m.renderRightPane(rightWidth, bodyHeight)

	var builder strings.Builder
	builder.WriteString("\x1b[H\x1b[2J")
	builder.WriteString(screenLine(m.headerLine(width), width))
	builder.WriteByte('\n')
	builder.WriteString(screenLine(m.subHeaderLine(width), width))
	builder.WriteByte('\n')
	builder.WriteString(screenLine(m.palette.rule(""), width))
	builder.WriteByte('\n')

	for idx := 0; idx < bodyHeight; idx++ {
		leftLine := ""
		if idx < len(left) {
			leftLine = left[idx]
		}
		rightLine := ""
		if idx < len(right) {
			rightLine = right[idx]
		}
		bodyLine := padANSI(leftLine, leftWidth) + " " + m.palette.muted("|") + " " + truncateText(rightLine, rightWidth)
		builder.WriteString(screenLine(bodyLine, width))
		builder.WriteByte('\n')
	}

	builder.WriteString(screenLine(m.palette.rule(""), width))
	builder.WriteByte('\n')
	builder.WriteString(screenLine(m.footerLine(width), width))
	builder.WriteByte('\n')
	builder.WriteString(screenLine(m.messageLine(width), width))
	builder.WriteString("\x1b[J")

	_, err := fmt.Fprint(m.stdout, builder.String())
	return err
}

func (m *tuiModel) headerLine(width int) string {
	mode := strings.ToUpper(m.mode)
	left := fmt.Sprintf("%s  %s", m.palette.title("CTX TUI"), m.palette.label(m.info.ModulePath))
	right := fmt.Sprintf("%s %d  %s %s", m.palette.label("snapshot"), m.status.Current.ID, m.palette.label("mode"), m.palette.badge(strings.ToLower(mode)))
	return alignLine(left, right, width)
}

func (m *tuiModel) subHeaderLine(width int) string {
	changed := fmt.Sprintf("%s %d", m.palette.label("changed"), m.changedNow)
	inventory := fmt.Sprintf("%s p=%d f=%d s=%d refs=%d calls=%d tests=%d", m.palette.label("inventory"), m.status.Current.TotalPackages, m.status.Current.TotalFiles, m.status.Current.TotalSymbols, m.status.Current.TotalRefs, m.status.Current.TotalCalls, m.status.Current.TotalTests)
	return alignLine(changed, inventory, width)
}

func (m *tuiModel) footerLine(width int) string {
	if m.inputMode == "search" {
		return fmt.Sprintf("%s /%s", m.palette.accent("Search"), m.inputValue)
	}
	base := "hjkl/arrows move  Enter open  1..9 quick-open  / search  f file  o use-site  v source  y copy  b/n history  g home  q quit"
	return truncateText(m.palette.label(base), width)
}

func (m *tuiModel) messageLine(width int) string {
	if m.message == "" {
		return m.palette.muted("Explore symbols, files, callers, callees, refs, and tests without leaving the terminal.")
	}
	return truncateText(m.palette.accent(m.message), width)
}

func (m *tuiModel) renderLeftPane(width, height int) []string {
	lines := make([]string, 0, height)
	title := "Landing"
	switch m.mode {
	case tuiModeSearch:
		title = fmt.Sprintf("Search: %q", m.searchQuery)
	case tuiModeSymbol:
		title = "Journey"
	case tuiModeFile:
		title = "File Travel"
	}
	lines = append(lines, m.palette.section(title))
	lines = append(lines, m.renderSectionTabs(width))
	lines = append(lines, "")

	section, ok := m.currentSection()
	if !ok {
		return fillLines(lines, height)
	}
	lines = append(lines, m.palette.label(fmt.Sprintf("%s (%d)", section.Title, len(section.Items))))
	if len(section.Items) == 0 {
		lines = append(lines, m.palette.muted(section.Empty))
		return fillLines(lines, height)
	}

	listHeight := height - len(lines)
	visibleItems := max(1, listHeight/3)
	start, end := windowForIndex(len(section.Items), m.itemIndex, visibleItems)
	for idx := start; idx < end; idx++ {
		item := section.Items[idx]
		prefix := "  "
		if idx == m.itemIndex {
			prefix = m.palette.accent("> ")
		}
		number := "  "
		if idx < 9 {
			number = strconv.Itoa(idx+1) + "."
		}
		label := item.Title
		if item.Kind == "symbol" && item.Symbol.Kind != "" {
			label = fmt.Sprintf("%s %s", m.palette.kindBadge(item.Symbol.Kind), item.Title)
		} else if item.Kind == "location" {
			label = fmt.Sprintf("%s %s", m.palette.kindBadge("test"), item.Title)
		} else if item.Kind == "file" {
			label = fmt.Sprintf("%s %s", m.palette.badge("low"), item.Title)
		}
		lines = append(lines, truncateText(prefix+number+" "+label, width))
		if item.Subtitle != "" {
			lines = append(lines, truncateText("     "+styleHumanSignature(m.palette, item.Subtitle), width))
		}
		if item.Detail != "" {
			lines = append(lines, truncateText("     "+m.palette.muted(item.Detail), width))
		}
	}

	return fillLines(lines, height)
}

func (m *tuiModel) renderSectionTabs(width int) string {
	if len(m.sections) == 0 {
		return ""
	}
	parts := make([]string, 0, len(m.sections))
	for idx, section := range m.sections {
		label := section.Title
		if count := len(section.Items); count > 0 {
			label = fmt.Sprintf("%s(%d)", label, count)
		}
		if idx == m.sectionIndex {
			parts = append(parts, "["+m.palette.accent(label)+"]")
		} else {
			parts = append(parts, label)
		}
	}
	return truncateText(strings.Join(parts, "  "), width)
}

func (m *tuiModel) renderRightPane(width, height int) []string {
	switch m.mode {
	case tuiModeLanding:
		return fillLines(m.renderLandingPane(width), height)
	case tuiModeSearch:
		return fillLines(m.renderSearchPane(width), height)
	case tuiModeFile:
		return fillLines(m.renderFilePane(width), height)
	case tuiModeSymbol:
		return fillLines(m.renderSymbolPane(width), height)
	default:
		return fillLines([]string{m.palette.muted("No view")}, height)
	}
}

func (m *tuiModel) renderLandingPane(width int) []string {
	lines := []string{
		m.palette.title("Welcome to CTX"),
		"",
	}
	lines = append(lines, wrapLines(width, fmt.Sprintf("This shell is for flowing through a Go codebase: start from important functions, jump into a file, inspect contracts, then follow callers, callees, refs, tests, and neighboring symbols."))...)
	lines = append(lines, "")
	lines = append(lines, truncateText(fmt.Sprintf("%s top packages=%d  top functions=%d  top types=%d", m.palette.label("Snapshot summary:"), len(m.report.TopPackages), len(m.report.TopFunctions), len(m.report.TopTypes)), width))
	lines = append(lines, truncateText(fmt.Sprintf("%s coverage=n/a  changed_now=%d", m.palette.label("Research posture:"), m.changedNow), width))
	lines = append(lines, "")

	item, ok := m.selectedItem()
	if !ok {
		return lines
	}
	lines = append(lines, m.palette.section("Selected Entry"))
	lines = append(lines, m.previewItem(width, item, false)...)
	return lines
}

func (m *tuiModel) renderSearchPane(width int) []string {
	lines := []string{
		m.palette.title(fmt.Sprintf("Matches for %q", m.searchQuery)),
		"",
	}
	item, ok := m.selectedItem()
	if !ok {
		return lines
	}
	lines = append(lines, m.previewItem(width, item, m.showSource)...)
	return lines
}

func (m *tuiModel) renderFilePane(width int) []string {
	lines := []string{
		fmt.Sprintf("%s %s", m.palette.title("File"), m.palette.accent(m.currentFile)),
		"",
	}
	lines = append(lines, truncateText(fmt.Sprintf("%s symbols=%d", m.palette.label("Inventory:"), len(m.fileSymbols)), width))
	lines = append(lines, truncateText(fmt.Sprintf("%s select a symbol and press Enter to open its graph slice", m.palette.label("Flow:")), width))
	lines = append(lines, "")

	item, ok := m.selectedItem()
	if !ok {
		excerpt, _ := readSourceExcerpt(m.info.Root, m.currentFile, m.currentLine, 2, 12)
		if excerpt != "" {
			lines = append(lines, m.palette.section("Source Preview"))
			lines = append(lines, wrapPreservingLines(width, excerpt)...)
		}
		return lines
	}
	lines = append(lines, m.previewItem(width, item, m.showSource)...)
	return lines
}

func (m *tuiModel) renderSymbolPane(width int) []string {
	view := m.current
	impact := impactLabel(len(view.Callers), len(view.ReferencesIn), len(view.Tests), len(view.Package.ReverseDeps))
	lines := []string{
		fmt.Sprintf("%s %s %s", m.palette.kindBadge(view.Symbol.Kind), m.palette.title(shortenQName(m.info.ModulePath, view.Symbol.QName)), m.palette.badge(impact)),
		"",
		truncateText(fmt.Sprintf("%s %s", m.palette.label("Contract:"), styleHumanSignature(m.palette, displaySignature(view.Symbol))), width),
		truncateText(fmt.Sprintf("%s %s", m.palette.label("Package:"), shortenQName(m.info.ModulePath, view.Symbol.PackageImportPath)), width),
		truncateText(fmt.Sprintf("%s %s:%d", m.palette.label("Declared:"), view.Symbol.FilePath, view.Symbol.Line), width),
	}
	if view.Symbol.Receiver != "" {
		lines = append(lines, truncateText(fmt.Sprintf("%s %s", m.palette.label("Receiver:"), view.Symbol.Receiver), width))
	}
	lines = append(lines,
		truncateText(fmt.Sprintf("%s callers=%d callees=%d refs_in=%d refs_out=%d tests=%d coverage=n/a", m.palette.label("Metrics:"), len(view.Callers), len(view.Callees), len(view.ReferencesIn), len(view.ReferencesOut), len(view.Tests)), width),
		truncateText(fmt.Sprintf("%s local_deps=%d reverse_deps=%d file_symbols=%d", m.palette.label("Package area:"), len(view.Package.LocalDeps), len(view.Package.ReverseDeps), len(m.fileSymbols)), width),
	)
	if doc := oneLine(view.Symbol.Doc); doc != "" {
		lines = append(lines, truncateText(fmt.Sprintf("%s %s", m.palette.label("Doc:"), doc), width))
	}
	lines = append(lines, "")

	item, ok := m.selectedItem()
	if !ok {
		lines = append(lines, m.renderCurrentSymbolSource(width, m.showSource)...)
		return lines
	}

	lines = append(lines, m.palette.section(fmt.Sprintf("Selected in %s", m.sections[m.sectionIndex].Title)))
	lines = append(lines, m.previewItem(width, item, m.showSource)...)
	return lines
}

func (m *tuiModel) renderCurrentSymbolSource(width int, expanded bool) []string {
	lines := []string{m.palette.section("Current Symbol")}
	excerpt := ""
	if expanded {
		excerpt, _ = readSymbolBlock(m.info.Root, m.current.Symbol, 28)
	} else {
		excerpt, _ = readSourceExcerpt(m.info.Root, m.current.Symbol.FilePath, m.current.Symbol.Line, 2, 8)
	}
	if excerpt == "" {
		lines = append(lines, m.palette.muted("No source preview available"))
		return lines
	}
	lines = append(lines, wrapPreservingLines(width, excerpt)...)
	return lines
}

func (m *tuiModel) previewItem(width int, item tuiItem, expanded bool) []string {
	lines := make([]string, 0, 24)
	title := item.Title
	if item.Kind == "symbol" && item.Symbol.Kind != "" {
		title = fmt.Sprintf("%s %s", m.palette.kindBadge(item.Symbol.Kind), title)
	} else if item.Kind == "file" {
		title = fmt.Sprintf("%s %s", m.palette.section("FILE"), title)
	}
	lines = append(lines, title)
	if item.Subtitle != "" {
		lines = append(lines, truncateText(styleHumanSignature(m.palette, item.Subtitle), width))
	}
	if item.Detail != "" {
		lines = append(lines, truncateText(fmt.Sprintf("%s %s", m.palette.label("Detail:"), item.Detail), width))
	}
	if item.Preview != "" {
		lines = append(lines, "")
		lines = append(lines, wrapLines(width, item.Preview)...)
	}
	lines = append(lines, "")
	lines = append(lines, m.palette.section("Source / Context"))

	source := ""
	switch item.Kind {
	case "symbol":
		if expanded {
			source, _ = readSymbolBlock(m.info.Root, item.Symbol, 24)
		} else {
			source, _ = readSourceExcerpt(m.info.Root, item.Symbol.FilePath, item.Symbol.Line, 2, 8)
		}
	case "file", "location":
		source, _ = readSourceExcerpt(m.info.Root, item.FilePath, item.Line, 2, 10)
	}
	if source == "" {
		lines = append(lines, m.palette.muted("No source context available"))
	} else {
		lines = append(lines, wrapPreservingLines(width, source)...)
	}
	return lines
}

func alignLine(left, right string, width int) string {
	leftWidth := visibleWidth(left)
	rightWidth := visibleWidth(right)
	if leftWidth+rightWidth+1 >= width {
		return truncateText(left+" "+right, width)
	}
	return left + strings.Repeat(" ", width-leftWidth-rightWidth) + right
}

func fillLines(lines []string, height int) []string {
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		return lines[:height]
	}
	return lines
}

func windowForIndex(total, index, height int) (int, int) {
	if total <= height {
		return 0, total
	}
	start := index - height/3
	if start < 0 {
		start = 0
	}
	end := start + height
	if end > total {
		end = total
		start = end - height
	}
	return start, end
}

func wrapLines(width int, text string) []string {
	if width <= 0 || strings.TrimSpace(text) == "" {
		return nil
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}
	lines := []string{words[0]}
	for _, word := range words[1:] {
		last := lines[len(lines)-1]
		if visibleWidth(last)+1+visibleWidth(word) <= width {
			lines[len(lines)-1] = last + " " + word
			continue
		}
		lines = append(lines, word)
	}
	return lines
}

func wrapPreservingLines(width int, text string) []string {
	rawLines := strings.Split(text, "\n")
	lines := make([]string, 0, len(rawLines))
	for _, raw := range rawLines {
		if raw == "" {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, truncateText(raw, width))
	}
	return lines
}

func truncateText(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if visibleWidth(text) <= width {
		return text
	}

	plain := stripANSI(text)
	if utf8.RuneCountInString(plain) <= width {
		return plain
	}
	runes := []rune(plain)
	if width <= 1 {
		return string(runes[:width])
	}
	return string(runes[:width-1]) + "…"
}

func padANSI(text string, width int) string {
	if width <= 0 {
		return ""
	}
	plainWidth := visibleWidth(text)
	if plainWidth >= width {
		return truncateText(text, width)
	}
	return text + strings.Repeat(" ", width-plainWidth)
}

func visibleWidth(text string) int {
	return utf8.RuneCountInString(stripANSI(text))
}

func screenLine(text string, width int) string {
	return padANSI(truncateText(text, width), width) + "\x1b[K"
}

func stripANSI(text string) string {
	var builder strings.Builder
	inEscape := false
	for idx := 0; idx < len(text); idx++ {
		ch := text[idx]
		if inEscape {
			if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') {
				inEscape = false
			}
			continue
		}
		if ch == 0x1b {
			inEscape = true
			continue
		}
		builder.WriteByte(ch)
	}
	return builder.String()
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func tuiDebugf(format string, args ...any) {
	if os.Getenv("CTX_TUI_DEBUG") == "" {
		return
	}
	_, _ = fmt.Fprintf(os.Stderr, "ctx-tui-debug: "+format+"\n", args...)
}
