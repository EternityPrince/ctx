package app

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
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
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, data, parser.ParseComments)
	if err != nil {
		start := max(1, symbol.Line-2)
		end := min(len(lines), symbol.Line+12)
		return path, start, end, lines, nil
	}

	start, end := 0, 0
	ast.Inspect(file, func(node ast.Node) bool {
		if start != 0 || node == nil {
			return start == 0
		}

		switch value := node.(type) {
		case *ast.FuncDecl:
			if value.Name == nil || value.Name.Name != symbol.Name {
				return true
			}
			line := fset.Position(value.Pos()).Line
			if line != symbol.Line {
				return true
			}
			start = nodeStartLine(fset, value.Doc, value.Pos())
			end = fset.Position(value.End()).Line
			return false
		case *ast.TypeSpec:
			if value.Name == nil || value.Name.Name != symbol.Name {
				return true
			}
			line := fset.Position(value.Pos()).Line
			if line != symbol.Line {
				return true
			}
			start = nodeStartLine(fset, value.Doc, value.Pos())
			end = fset.Position(value.End()).Line
			return false
		}
		return true
	})

	if start == 0 || end == 0 || start > len(lines) {
		start = max(1, symbol.Line-2)
		end = min(len(lines), symbol.Line+12)
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

func nodeStartLine(fset *token.FileSet, doc *ast.CommentGroup, pos token.Pos) int {
	if doc != nil {
		if line := fset.Position(doc.Pos()).Line; line > 0 {
			return line
		}
	}
	return fset.Position(pos).Line
}
