package app

import (
	"io"

	"github.com/vladimirkasterin/ctx/internal/cli"
)

func runSymbol(command cli.Command, stdout io.Writer) error {
	state, err := openPreparedProjectState(command)
	if err != nil {
		return err
	}
	defer state.Close()

	match, found, err := resolveSingleSymbolQuery(stdout, state.Info.ModulePath, state.Store, command.Query)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}

	view, err := state.Store.LoadSymbolView(match.SymbolKey)
	if err != nil {
		return err
	}
	guidance, err := buildSymbolTestGuidance(state.Store, view, 8)
	if err != nil {
		return err
	}
	switch command.OutputMode {
	case cli.OutputAI:
		return renderAISymbolView(stdout, state.Info.ModulePath, view, guidance)
	default:
		return renderHumanSymbolView(stdout, state.Info.Root, state.Info.ModulePath, view, guidance)
	}
}

func runImpact(command cli.Command, stdout io.Writer) error {
	state, err := openPreparedProjectState(command)
	if err != nil {
		return err
	}
	defer state.Close()

	match, found, err := resolveSingleSymbolQuery(stdout, state.Info.ModulePath, state.Store, command.Query)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}

	view, err := state.Store.LoadImpactView(match.SymbolKey, command.Depth)
	if err != nil {
		return err
	}
	symbolView, err := state.Store.LoadSymbolView(match.SymbolKey)
	if err != nil {
		return err
	}
	guidance, err := buildSymbolTestGuidance(state.Store, symbolView, 8)
	if err != nil {
		return err
	}
	switch command.OutputMode {
	case cli.OutputAI:
		return renderAIImpactView(stdout, state.Info.ModulePath, view, guidance, command.Depth)
	default:
		return renderHumanImpactView(stdout, state.Info.Root, state.Info.ModulePath, view, guidance, command.Depth)
	}
}
