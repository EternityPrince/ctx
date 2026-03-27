package sourcerange

import (
	"go/ast"
	"go/parser"
	"go/token"

	pythonadapter "github.com/vladimirkasterin/ctx/internal/adapter/python"
	rustadapter "github.com/vladimirkasterin/ctx/internal/adapter/rust"
	"github.com/vladimirkasterin/ctx/internal/codebase"
)

type Symbol struct {
	Name     string
	Kind     string
	Receiver string
	Line     int
}

func Locate(path string, data []byte, symbol Symbol) (int, int) {
	if codebase.IsPythonFile(path) {
		return locatePython(path, symbol)
	}
	if codebase.IsRustFile(path) {
		return locateRust(data, symbol)
	}
	return locateGo(path, data, symbol)
}

func locatePython(path string, symbol Symbol) (int, int) {
	start, end, err := pythonadapter.LocateSymbolBlock(path, symbol.Name, symbol.Kind, symbol.Receiver, symbol.Line)
	if err != nil {
		return 0, 0
	}
	return start, end
}

func locateRust(data []byte, symbol Symbol) (int, int) {
	start, end, err := rustadapter.LocateSymbolBlock(data, symbol.Name, symbol.Kind, symbol.Receiver, symbol.Line)
	if err != nil {
		return 0, 0
	}
	return start, end
}

func locateGo(path string, data []byte, symbol Symbol) (int, int) {
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
