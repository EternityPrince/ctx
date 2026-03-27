package app

import (
	"fmt"
	"strconv"
	"strings"
)

func (m *tuiModel) draw() error {
	width, height := terminalSize()
	if width < 80 || height < 20 {
		_, err := fmt.Fprintf(m.stdout, "\x1b[H\x1b[2J%s\nResize the terminal to at least 80x20.\n", m.palette.title("CTX TUI"))
		return err
	}

	leftWidth := max(width/3, 34)
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

	for idx := range bodyHeight {
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
	lines = append(lines, wrapLines(width, "This shell is for flowing through a codebase: start from important functions, jump into a file, inspect contracts, then follow callers, callees, refs, tests, and neighboring symbols.")...)
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
	lines = append(lines, truncateText("Symbols use exact/prefix/contains/fuzzy ranking; text search uses smart-case substring across indexed files.", width))
	lines = append(lines, "")
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
