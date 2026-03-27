package app

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

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
	start := max(index-height/3, 0)
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
