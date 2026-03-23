package app

import (
	"github.com/vladimirkasterin/ctx/internal/adapter"
	"github.com/vladimirkasterin/ctx/internal/cli"
	"github.com/vladimirkasterin/ctx/internal/core"
	"github.com/vladimirkasterin/ctx/internal/storage"
)

var projectService = core.NewService(adapter.NewAdapter())

func openProjectState(path string) (core.ProjectState, error) {
	return projectService.OpenProject(path)
}

func openPreparedProjectState(command cli.Command) (core.ProjectState, error) {
	path := command.Root
	if command.ProjectArg != "" {
		record, err := storage.ResolveProject(command.ProjectArg)
		if err != nil {
			return core.ProjectState{}, err
		}
		path = record.RootPath
	}
	return projectService.PrepareProject(path, command.Name)
}
