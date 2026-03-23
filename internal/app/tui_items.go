package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/storage"
)

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
		detail := fmt.Sprintf("%s:%d", symbol.FilePath, symbol.Line)
		if symbol.SearchKind != "" {
			detail = fmt.Sprintf("%s | %s | %s", symbol.SearchKind, describeSymbolSearchWhy(symbol), detail)
		}
		items = append(items, tuiItem{
			Kind:      "symbol",
			Title:     shortenQName(m.info.ModulePath, symbol.QName),
			Subtitle:  displaySignature(symbol),
			Detail:    detail,
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

func (m *tuiModel) textSearchItems(values []projectTextMatch, group projectTextFileGroup) []tuiItem {
	items := make([]tuiItem, 0, len(values))
	for _, value := range values {
		items = append(items, tuiItem{
			Kind:     "location",
			Title:    fmt.Sprintf("L%d:C%d", value.Line, value.Column),
			Subtitle: fmt.Sprintf("%s match", strings.ToUpper(value.MatchKind)),
			Detail:   fmt.Sprintf("%s | %s", group.Why, value.Preview),
			Preview:  sourceLineSnippet(m.info.Root, value.FilePath, value.Line),
			FilePath: value.FilePath,
			Line:     value.Line,
			CopyText: fmt.Sprintf("%s:%d:%d\n%s", value.FilePath, value.Line, value.Column, value.Preview),
			Relation: value.MatchKind,
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
		detail := fmt.Sprintf("%s:%d", value.FilePath, value.Line)
		if value.Why != "" {
			detail += " | " + value.Why
		}
		items = append(items, tuiItem{
			Kind:       "location",
			Title:      value.Name,
			Subtitle:   fmt.Sprintf("%s %s", value.FilePath, formatTestRelationLabel(value)),
			Detail:     detail,
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
