package app

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/storage"
)

func detectBatPath() string {
	for _, candidate := range []string{"bat", "batcat"} {
		if path, err := exec.LookPath(candidate); err == nil {
			return path
		}
	}
	return ""
}

func renderLocationSource(projectRoot, batPath, relPath string, focusLine, before, after int, color bool) (string, error) {
	if batPath == "" {
		return readSourceExcerpt(projectRoot, relPath, focusLine, before, after)
	}

	start := max(1, focusLine-before)
	end := focusLine + after
	output, err := renderWithBat(batPath, projectRoot, relPath, start, end, focusLine, color)
	if err != nil {
		return readSourceExcerpt(projectRoot, relPath, focusLine, before, after)
	}
	return output, nil
}

func renderSymbolSource(projectRoot, batPath string, symbol storage.SymbolMatch, maxLines int, color bool) (string, error) {
	if batPath == "" {
		return readSymbolBlock(projectRoot, symbol, maxLines)
	}

	path, start, end, _, err := symbolBlockBounds(projectRoot, symbol)
	if err != nil {
		return "", err
	}
	if maxLines > 0 && end-start+1 > maxLines {
		end = start + maxLines - 1
	}
	output, err := renderWithBatAbsolute(batPath, path, start, end, symbol.Line, color)
	if err != nil {
		return readSymbolBlock(projectRoot, symbol, maxLines)
	}
	return output, nil
}

func renderWithBat(batPath, projectRoot, relPath string, start, end, focusLine int, color bool) (string, error) {
	absPath := filepath.Join(projectRoot, filepath.FromSlash(relPath))
	return renderWithBatAbsolute(batPath, absPath, start, end, focusLine, color)
}

func renderWithBatAbsolute(batPath, path string, start, end, focusLine int, color bool) (string, error) {
	args := []string{
		"--paging=never",
		"--style=numbers",
		"--line-range", strconv.Itoa(start) + ":" + strconv.Itoa(end),
		"--highlight-line", strconv.Itoa(focusLine),
	}
	if color {
		args = append(args, "--color=always")
	} else {
		args = append(args, "--color=never")
	}
	args = append(args, path)

	out, err := exec.Command(batPath, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("run bat: %w", err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}
