package app

import (
	"fmt"
	"io"
	"os"

	"github.com/vladimirkasterin/ctx/internal/cli"
	"github.com/vladimirkasterin/ctx/internal/clipboard"
	"github.com/vladimirkasterin/ctx/internal/collector"
	"github.com/vladimirkasterin/ctx/internal/render"
	"github.com/vladimirkasterin/ctx/internal/tree"
)

func runLegacyReport(command cli.Command, stdout io.Writer) error {
	snapshot, err := collector.Collect(command.Report)
	if err != nil {
		return fmt.Errorf("collect project context: %w", err)
	}

	projectTree := tree.Build(snapshot.Root, snapshot.Directories, snapshot.Files)
	output := render.Report(snapshot, projectTree, command.Report)

	if command.Report.CopyToClipboard {
		if err := clipboard.Copy(output); err != nil {
			return fmt.Errorf("copy report to clipboard: %w", err)
		}
		_, err := fmt.Fprintln(stdout, "Report copied to clipboard")
		return err
	}

	if command.Report.OutputPath == "" {
		_, err := io.WriteString(stdout, output)
		return err
	}

	if err := os.WriteFile(command.Report.OutputPath, []byte(output), 0o644); err != nil {
		return fmt.Errorf("write output file: %w", err)
	}

	_, err = fmt.Fprintf(stdout, "Report written to %s\n", command.Report.OutputPath)
	return err
}

func runProjectReport(command cli.Command, stdout io.Writer) error {
	state, err := openPreparedProjectState(command)
	if err != nil {
		return err
	}
	defer state.Close()

	status, err := state.Store.Status(projectService.ChangedNow(state))
	if err != nil {
		return err
	}
	if !status.HasSnapshot {
		_, err := fmt.Fprintf(stdout, "No index snapshot for %s. Run `ctx index %s` first.\n", state.Info.ModulePath, state.Info.Root)
		return err
	}
	composition := summarizeProjectComposition(state.Scanned)

	scope := normalizeReportSliceScope(command.Scope)
	loadLimit := command.Limit
	if scope != "project" {
		loadLimit = max(command.Limit*4, 24)
	}

	view, err := state.Store.LoadReportView(loadLimit)
	if err != nil {
		return err
	}
	if command.Explain {
		if err := state.Store.ExplainReportView(&view); err != nil {
			return err
		}
	}
	watch, err := buildReportTestWatch(state.Store, view)
	if err != nil {
		return err
	}

	if scope != "project" {
		slice, err := buildReportSlice(scope, state.Store, view, watch, command.Limit)
		if err != nil {
			return err
		}
		switch command.OutputMode {
		case cli.OutputAI:
			return renderAIReportSlice(stdout, state.Info.ModulePath, status, view, slice, command.Limit, command.Explain)
		default:
			return renderHumanReportSlice(stdout, state.Info.Root, state.Info.ModulePath, status, view, slice, command.Limit, command.Explain)
		}
	}

	switch command.OutputMode {
	case cli.OutputAI:
		return renderAIReport(stdout, state.Info.ModulePath, status, view, watch, composition, command.Explain)
	default:
		return renderHumanReport(stdout, state.Info.Root, state.Info.ModulePath, status, view, watch, composition, command.Explain)
	}
}
