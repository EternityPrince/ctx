package app

import (
	"fmt"

	"github.com/vladimirkasterin/ctx/internal/storage"
)

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
	results, err := loadProjectSearchResults(m.info.Root, m.store, projectSearchModeAll, query, projectSearchLimit)
	if err != nil {
		return err
	}
	m.mode = tuiModeSearch
	m.searchQuery = results.Query
	m.searchItems = results.Symbols
	m.sections = buildTUISearchSections(m, results)
	m.sectionIndex = 0
	m.itemIndex = 0
	m.showSource = false
	if focusMessage {
		m.message = fmt.Sprintf("%d symbol matches, %d text matches for %q", len(results.Symbols), len(results.Text), results.Query)
	}
	return nil
}

func buildTUISearchSections(m *tuiModel, results projectSearchResults) []tuiSection {
	sections := make([]tuiSection, 0, 1+projectTextDisplayedFileCount(results.TextPackages))
	sections = append(sections, tuiSection{
		Title: "Symbols",
		Items: m.symbolSearchItems(results.Symbols),
		Empty: "No symbol matches",
	})
	if len(results.TextPackages) == 0 {
		sections = append(sections, tuiSection{
			Title: "Text",
			Items: nil,
			Empty: "No text matches",
		})
		return sections
	}
	for _, pkg := range results.TextPackages {
		pkgTitle := pkg.PackageImportPath
		if pkgTitle == "" {
			pkgTitle = "(root)"
		} else {
			pkgTitle = shortenQName(m.info.ModulePath, pkg.PackageImportPath)
		}
		for _, file := range pkg.Files {
			sections = append(sections, tuiSection{
				Title: fmt.Sprintf("Text / %s / %s", pkgTitle, file.FilePath),
				Items: m.textSearchItems(file.Matches, file),
				Empty: "No text matches",
			})
		}
	}
	return sections
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
	guidance, err := buildSymbolTestGuidance(m.store, view, 8)
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
	m.sections = m.symbolSections(view, fileSymbols, guidance.ReadBefore)
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

func (m *tuiModel) symbolSections(view storage.SymbolView, fileSymbols []storage.SymbolMatch, tests []storage.TestView) []tuiSection {
	return []tuiSection{
		{Title: "File Map", Items: m.fileItems(fileSymbols), Empty: "No file symbols"},
		{Title: "Callers", Items: m.relatedItems(view.Callers), Empty: "No direct callers"},
		{Title: "Callees", Items: m.relatedItems(view.Callees), Empty: "No direct callees"},
		{Title: "Refs In", Items: m.refItems(view.ReferencesIn), Empty: "No inbound refs"},
		{Title: "Refs Out", Items: m.refItems(view.ReferencesOut), Empty: "No outbound refs"},
		{Title: "Tests To Read", Items: m.testItems(tests), Empty: "No recommended tests"},
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
