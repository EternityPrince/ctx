package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/storage"
)

func readSymbolBlock(projectRoot string, symbol storage.SymbolMatch, maxLines int) (string, error) {
	_, start, end, lines, err := symbolBlockBounds(projectRoot, symbol)
	if err != nil {
		return "", err
	}
	if maxLines > 0 && end-start+1 > maxLines {
		end = start + maxLines - 1
	}

	var builder strings.Builder
	for line := start; line <= end; line++ {
		marker := " "
		if line == symbol.Line {
			marker = ">"
		}
		builder.WriteString(fmt.Sprintf("  %s %4d | %s\n", marker, line, lines[line-1]))
	}
	if maxLines > 0 && end < len(lines) && end-start+1 == maxLines {
		builder.WriteString("  ...\n")
	}
	return strings.TrimRight(builder.String(), "\n"), nil
}

func symbolBlockBounds(projectRoot string, symbol storage.SymbolMatch) (string, int, int, []string, error) {
	path := filepath.Join(projectRoot, filepath.FromSlash(symbol.FilePath))
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, 0, nil, err
	}

	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	start, end := locateSymbolRange(path, data, symbol)
	if start == 0 || end == 0 || start > len(lines) {
		start, end = fallbackSymbolRange(len(lines), symbol.Line)
	}
	end = min(end, len(lines))
	return path, start, end, lines, nil
}

func symbolRange(projectRoot string, symbol storage.SymbolMatch) (int, int, error) {
	_, start, end, _, err := symbolBlockBounds(projectRoot, symbol)
	if err != nil {
		return 0, 0, err
	}
	return start, end, nil
}

func symbolRangeDisplay(projectRoot string, symbol storage.SymbolMatch) string {
	start, end, err := symbolRange(projectRoot, symbol)
	if err != nil || start == 0 {
		return fmt.Sprintf("%s:%d", symbol.FilePath, symbol.Line)
	}
	if end <= start {
		return fmt.Sprintf("%s:%d", symbol.FilePath, start)
	}
	return fmt.Sprintf("%s:%d->%d", symbol.FilePath, start, end)
}

func symbolLineCount(projectRoot string, symbol storage.SymbolMatch) int {
	start, end, err := symbolRange(projectRoot, symbol)
	if err != nil || start == 0 {
		return 0
	}
	if end < start {
		return 1
	}
	return end - start + 1
}

func symbolRangeWithCountDisplay(projectRoot string, symbol storage.SymbolMatch) string {
	base := symbolRangeDisplay(projectRoot, symbol)
	lineCount := symbolLineCount(projectRoot, symbol)
	if lineCount <= 0 {
		return base
	}
	return fmt.Sprintf("%s (%dL)", base, lineCount)
}

func fallbackSymbolRange(totalLines, focusLine int) (int, int) {
	return max(1, focusLine-2), min(totalLines, focusLine+12)
}
