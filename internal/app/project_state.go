package app

import (
	golangadapter "github.com/vladimirkasterin/ctx/internal/adapter/golang"
	"github.com/vladimirkasterin/ctx/internal/cli"
	"github.com/vladimirkasterin/ctx/internal/core"
)

var projectService = core.NewService(golangadapter.NewAdapter())

func openProjectState(path string) (core.ProjectState, error) {
	return projectService.OpenProject(path)
}

func openPreparedProjectState(command cli.Command) (core.ProjectState, error) {
	return projectService.PrepareProject(command.Root, command.Name)
}
