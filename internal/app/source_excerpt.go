package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func readSourceExcerpt(projectRoot, relPath string, focusLine, before, after int) (string, error) {
	path := filepath.Join(projectRoot, filepath.FromSlash(relPath))
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if focusLine <= 0 || focusLine > len(lines) {
		return "", nil
	}

	start := max(1, focusLine-before)
	end := min(len(lines), focusLine+after)
	var builder strings.Builder
	for line := start; line <= end; line++ {
		marker := " "
		if line == focusLine {
			marker = ">"
		}
		builder.WriteString(fmt.Sprintf("  %s %4d | %s\n", marker, line, lines[line-1]))
	}
	return strings.TrimRight(builder.String(), "\n"), nil
}

func sourceLineSnippet(projectRoot, relPath string, line int) string {
	excerpt, err := readSourceExcerpt(projectRoot, relPath, line, 0, 0)
	if err != nil || excerpt == "" {
		return ""
	}
	parts := strings.Split(strings.TrimSpace(excerpt), "|")
	if len(parts) == 0 {
		return strings.TrimSpace(excerpt)
	}
	return strings.TrimSpace(parts[len(parts)-1])
}
