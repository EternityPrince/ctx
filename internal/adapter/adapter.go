package adapter

import (
	golangadapter "github.com/vladimirkasterin/ctx/internal/adapter/golang"
	pythonadapter "github.com/vladimirkasterin/ctx/internal/adapter/python"
	rustadapter "github.com/vladimirkasterin/ctx/internal/adapter/rust"
)

type Adapter struct {
	goAdapter     *golangadapter.Adapter
	pythonAdapter *pythonadapter.Adapter
	rustAdapter   *rustadapter.Adapter
}

func NewAdapter() *Adapter {
	return &Adapter{
		goAdapter:     golangadapter.NewAdapter(),
		pythonAdapter: pythonadapter.NewAdapter(),
		rustAdapter:   rustadapter.NewAdapter(),
	}
}
