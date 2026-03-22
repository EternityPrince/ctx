package app

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/cli"
	"github.com/vladimirkasterin/ctx/internal/clipboard"
	"github.com/vladimirkasterin/ctx/internal/collector"
	"github.com/vladimirkasterin/ctx/internal/render"
	"github.com/vladimirkasterin/ctx/internal/storage"
	"github.com/vladimirkasterin/ctx/internal/tree"
)

func Run(command cli.Command, stdout io.Writer) error {
	switch command.Name {
	case "report":
		return runProjectReport(command, stdout)
	case "legacy-report":
		return runLegacyReport(command, stdout)
	case "index":
		return runIndexLike(command, stdout, true)
	case "update":
		return runIndexLike(command, stdout, false)
	case "shell":
		return runShell(command, stdout)
	case "status":
		return runStatus(command, stdout)
	case "projects":
		return runProjects(command, stdout)
	case "symbol":
		return runSymbol(command, stdout)
	case "impact":
		return runImpact(command, stdout)
	case "diff":
		return runDiff(command, stdout)
	case "snapshots":
		return runSnapshots(command, stdout)
	case "snapshot":
		return runSnapshot(command, stdout)
	default:
		return fmt.Errorf("unsupported command %q", command.Name)
	}
}

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

	view, err := state.Store.LoadReportView(command.Limit)
	if err != nil {
		return err
	}

	switch command.OutputMode {
	case cli.OutputAI:
		return renderAIReport(stdout, state.Info.ModulePath, status, view)
	default:
		return renderHumanReport(stdout, state.Info.Root, state.Info.ModulePath, status, view)
	}
}

func runIndexLike(command cli.Command, stdout io.Writer, forceFull bool) error {
	state, err := openProjectState(command.Root)
	if err != nil {
		return err
	}
	defer state.Close()

	snapshot, committed, err := projectService.ApplySnapshot(state, command.Name, command.Note, forceFull)
	if err != nil {
		return err
	}
	if !committed {
		_, err := fmt.Fprintf(stdout, "No changes detected. current_snapshot=%d\n", snapshot.ID)
		return err
	}

	_, err = fmt.Fprintf(
		stdout,
		"snapshot=%d kind=%s packages=%d files=%d symbols=%d refs=%d calls=%d tests=%d changed_files=%d changed_packages=%d changed_symbols=%d\n",
		snapshot.ID,
		snapshot.Kind,
		snapshot.TotalPackages,
		snapshot.TotalFiles,
		snapshot.TotalSymbols,
		snapshot.TotalRefs,
		snapshot.TotalCalls,
		snapshot.TotalTests,
		snapshot.ChangedFiles,
		snapshot.ChangedPackages,
		snapshot.ChangedSymbols,
	)
	return err
}

func runStatus(command cli.Command, stdout io.Writer) error {
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
		if command.OutputMode == cli.OutputAI {
			_, err := fmt.Fprintf(stdout, "root=%s\nmodule=%s\nsnapshot=none\nchanged_now=%d\n", state.Info.Root, state.Info.ModulePath, status.ChangedNow)
			return err
		}
		_, err := fmt.Fprintf(stdout, "Root: %s\nModule: %s\nSnapshot: none\nChanged now: %d\n", state.Info.Root, state.Info.ModulePath, status.ChangedNow)
		return err
	}

	if command.OutputMode == cli.OutputAI {
		_, err = fmt.Fprintf(
			stdout,
			"root=%s\nmodule=%s\ngo=%s\nsnapshot=%d\nsnapshot_at=%s\npackages=%d\nfiles=%d\nsymbols=%d\nrefs=%d\ncalls=%d\ntests=%d\nchanged_now=%d\ndb=%s\n",
			status.RootPath,
			status.ModulePath,
			status.GoVersion,
			status.Current.ID,
			status.Current.CreatedAt.Format(timeFormat),
			status.Current.TotalPackages,
			status.Current.TotalFiles,
			status.Current.TotalSymbols,
			status.Current.TotalRefs,
			status.Current.TotalCalls,
			status.Current.TotalTests,
			status.ChangedNow,
			status.CurrentDBPath,
		)
		return err
	}

	_, err = fmt.Fprintf(
		stdout,
		"Root: %s\nModule: %s\nGo: %s\nSnapshot: %d (%s)\nInventory: packages=%d files=%d symbols=%d refs=%d calls=%d tests=%d\nChanged now: %d\nDB: %s\n",
		status.RootPath,
		status.ModulePath,
		status.GoVersion,
		status.Current.ID,
		status.Current.CreatedAt.Format(timeFormat),
		status.Current.TotalPackages,
		status.Current.TotalFiles,
		status.Current.TotalSymbols,
		status.Current.TotalRefs,
		status.Current.TotalCalls,
		status.Current.TotalTests,
		status.ChangedNow,
		status.CurrentDBPath,
	)
	return err
}

func runProjects(command cli.Command, stdout io.Writer) error {
	switch command.ProjectsVerb {
	case "", "list":
		records, err := storage.ListProjects()
		if err != nil {
			return err
		}
		if len(records) == 0 {
			_, err := fmt.Fprintln(stdout, "No indexed projects")
			return err
		}
		for _, record := range records {
			if _, err := fmt.Fprintf(
				stdout,
				"%s snapshot=%d size=%dB module=%s root=%s updated=%s\n",
				record.ID,
				record.CurrentSnapshotID,
				record.SizeBytes,
				record.ModulePath,
				record.RootPath,
				record.UpdatedAt.Format(timeFormat),
			); err != nil {
				return err
			}
		}
		return nil
	case "rm":
		if err := storage.RemoveProject(command.ProjectArg); err != nil {
			return err
		}
		_, err := fmt.Fprintf(stdout, "Removed project %s\n", command.ProjectArg)
		return err
	case "prune":
		removed, err := storage.PruneProjects()
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(stdout, "Pruned %d project(s)\n", removed)
		return err
	default:
		return fmt.Errorf("unsupported projects subcommand %q", command.ProjectsVerb)
	}
}

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
	switch command.OutputMode {
	case cli.OutputAI:
		return renderAISymbolView(stdout, state.Info.ModulePath, view)
	default:
		return renderHumanSymbolView(stdout, state.Info.Root, state.Info.ModulePath, view)
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
	switch command.OutputMode {
	case cli.OutputAI:
		return renderAIImpactView(stdout, state.Info.ModulePath, view, command.Depth)
	default:
		return renderHumanImpactView(stdout, state.Info.Root, state.Info.ModulePath, view, command.Depth)
	}
}

func runDiff(command cli.Command, stdout io.Writer) error {
	state, err := openPreparedProjectState(command)
	if err != nil {
		return err
	}
	defer state.Close()

	diff, err := state.Store.Diff(command.FromSnapshot, command.ToSnapshot)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(stdout, "Diff %d -> %d\n", diff.FromSnapshotID, diff.ToSnapshotID); err != nil {
		return err
	}
	if err := printStringList(stdout, "Added files", diff.AddedFiles); err != nil {
		return err
	}
	if err := printStringList(stdout, "Changed files", diff.ChangedFiles); err != nil {
		return err
	}
	if err := printStringList(stdout, "Deleted files", diff.DeletedFiles); err != nil {
		return err
	}

	if err := printSymbolList(stdout, "Added symbols", diff.AddedSymbols); err != nil {
		return err
	}
	if err := printSymbolList(stdout, "Removed symbols", diff.RemovedSymbols); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(stdout, "Changed symbols (%d):\n", len(diff.ChangedSymbols)); err != nil {
		return err
	}
	for _, symbol := range diff.ChangedSymbols {
		if _, err := fmt.Fprintf(stdout, "  %s\n    from: %s\n    to:   %s\n", symbol.QName, symbol.FromSignature, symbol.ToSignature); err != nil {
			return err
		}
	}
	return nil
}

func printStringList(stdout io.Writer, title string, values []string) error {
	if _, err := fmt.Fprintf(stdout, "%s (%d):\n", title, len(values)); err != nil {
		return err
	}
	for _, value := range values {
		if _, err := fmt.Fprintf(stdout, "  %s\n", value); err != nil {
			return err
		}
	}
	return nil
}

func printSymbolList(stdout io.Writer, title string, values []storage.SymbolMatch) error {
	if _, err := fmt.Fprintf(stdout, "%s (%d):\n", title, len(values)); err != nil {
		return err
	}
	for _, value := range values {
		if _, err := fmt.Fprintf(stdout, "  %s  %s:%d\n", value.QName, value.FilePath, value.Line); err != nil {
			return err
		}
	}
	return nil
}

func shortenQName(modulePath, qname string) string {
	return strings.TrimPrefix(qname, modulePath+"/")
}

func oneLine(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.Join(strings.Fields(value), " ")
}

const timeFormat = "2006-01-02 15:04:05"
