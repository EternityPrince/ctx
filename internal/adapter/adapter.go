package adapter

import (
	golangadapter "github.com/vladimirkasterin/ctx/internal/adapter/golang"
	pythonadapter "github.com/vladimirkasterin/ctx/internal/adapter/python"
)

type Adapter struct {
	goAdapter     *golangadapter.Adapter
	pythonAdapter *pythonadapter.Adapter
}

func NewAdapter() *Adapter {
	return &Adapter{
		goAdapter:     golangadapter.NewAdapter(),
		pythonAdapter: pythonadapter.NewAdapter(),
	}
}
