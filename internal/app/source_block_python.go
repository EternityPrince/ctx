package app

import (
	pythonadapter "github.com/vladimirkasterin/ctx/internal/adapter/python"
	"github.com/vladimirkasterin/ctx/internal/storage"
)

func locatePythonSymbolRange(path string, symbol storage.SymbolMatch) (int, int) {
	start, end, err := pythonadapter.LocateSymbolBlock(path, symbol.Name, symbol.Kind, symbol.Receiver, symbol.Line)
	if err != nil {
		return 0, 0
	}
	return start, end
}
