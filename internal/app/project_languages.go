package app

import (
	"strconv"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/codebase"
)

type projectComposition struct {
	Go     int
	Python int
	Rust   int
}

func summarizeProjectComposition(scanned []codebase.ScanFile) projectComposition {
	var summary projectComposition
	for _, file := range scanned {
		switch {
		case codebase.IsGoFile(file.RelPath):
			summary.Go++
		case codebase.IsPythonFile(file.RelPath):
			summary.Python++
		case codebase.IsRustFile(file.RelPath):
			summary.Rust++
		}
	}
	return summary
}

func (c projectComposition) Display() string {
	parts := make([]string, 0, 3)
	if c.Go > 0 {
		parts = append(parts, "go="+itoa(c.Go))
	}
	if c.Python > 0 {
		parts = append(parts, "py="+itoa(c.Python))
	}
	if c.Rust > 0 {
		parts = append(parts, "rs="+itoa(c.Rust))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, " ")
}

func (c projectComposition) Capabilities() string {
	parts := make([]string, 0, 3)
	if c.Go > 0 {
		parts = append(parts, "Go=typed")
	}
	if c.Python > 0 {
		parts = append(parts, "Python=heuristic")
	}
	if c.Rust > 0 {
		parts = append(parts, "Rust=best-effort")
	}
	if len(parts) == 0 {
		return "n/a"
	}
	return strings.Join(parts, "  ")
}

func itoa(value int) string {
	return strconv.Itoa(value)
}
