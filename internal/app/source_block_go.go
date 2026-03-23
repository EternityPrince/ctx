package app

import (
	"go/ast"
	"go/parser"
	"go/token"

	"github.com/vladimirkasterin/ctx/internal/codebase"
	"github.com/vladimirkasterin/ctx/internal/storage"
)

func locateSymbolRange(path string, data []byte, symbol storage.SymbolMatch) (int, int) {
	if codebase.IsPythonFile(path) {
		return locatePythonSymbolRange(path, symbol)
	}
	return locateGoSymbolRange(path, data, symbol)
}

func locateGoSymbolRange(path string, data []byte, symbol storage.SymbolMatch) (int, int) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, data, parser.ParseComments)
	if err != nil {
		return 0, 0
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

	return start, end
}

func nodeStartLine(fset *token.FileSet, doc *ast.CommentGroup, pos token.Pos) int {
	if doc != nil {
		if line := fset.Position(doc.Pos()).Line; line > 0 {
			return line
		}
	}
	return fset.Position(pos).Line
}
