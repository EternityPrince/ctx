package rust

import (
	"github.com/vladimirkasterin/ctx/internal/codebase"
	"github.com/vladimirkasterin/ctx/internal/project"
)

type Adapter struct{}

func NewAdapter() *Adapter {
	return &Adapter{}
}

func (a *Adapter) DetectChanges(info project.Info, scanned []codebase.ScanFile, previous map[string]codebase.PreviousFile) codebase.ChangePlan {
	return codebase.DetectRustChanges(info.ModulePath, scanned, previous)
}
