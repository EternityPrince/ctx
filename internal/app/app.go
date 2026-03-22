package app

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/cli"
	"github.com/vladimirkasterin/ctx/internal/clipboard"
	"github.com/vladimirkasterin/ctx/internal/collector"
	"github.com/vladimirkasterin/ctx/internal/indexer"
	"github.com/vladimirkasterin/ctx/internal/project"
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
	info, store, scanned, previous, err := openPreparedProjectState(command)
	if err != nil {
		return err
	}
	defer store.Close()

	changes := indexer.Diff(scanned, previous)
	status, err := store.Status(len(changes.Added) + len(changes.Changed) + len(changes.Deleted))
	if err != nil {
		return err
	}
	if !status.HasSnapshot {
		_, err := fmt.Fprintf(stdout, "No index snapshot for %s. Run `ctx index %s` first.\n", info.ModulePath, info.Root)
		return err
	}

	view, err := store.LoadReportView(command.Limit)
	if err != nil {
		return err
	}

	switch command.OutputMode {
	case cli.OutputAI:
		return renderAIReport(stdout, info.ModulePath, status, view)
	default:
		return renderHumanReport(stdout, info.Root, info.ModulePath, status, view)
	}
}

func runIndexLike(command cli.Command, stdout io.Writer, forceFull bool) error {
	info, store, scanned, previous, err := openProjectState(command.Root)
	if err != nil {
		return err
	}
	defer store.Close()

	changes, impacted, fullReindex := indexer.DetectImpactedPackages(info.Root, info.ModulePath, scanned, previous)
	if forceFull {
		fullReindex = true
	}

	if !fullReindex && len(changes.Added) == 0 && len(changes.Changed) == 0 && len(changes.Deleted) == 0 {
		current, ok, err := store.CurrentSnapshot()
		if err != nil {
			return err
		}
		if !ok {
			fullReindex = true
		} else {
			_, err = fmt.Fprintf(stdout, "No changes detected. current_snapshot=%d\n", current.ID)
			return err
		}
	}

	current, hasCurrent, err := store.CurrentSnapshot()
	if err != nil {
		return err
	}

	if !fullReindex && hasCurrent && len(impacted) > 0 {
		reverse, err := store.ReverseDependencies(current.ID, impacted)
		if err != nil {
			return err
		}
		impacted = mergeStrings(impacted, reverse)
	}

	patterns := impacted
	if fullReindex {
		patterns = nil
	}

	scanMap := make(map[string]indexer.ScanFile, len(scanned))
	for _, file := range scanned {
		scanMap[file.RelPath] = file
	}

	result, err := indexer.Analyze(info.Root, info.ModulePath, info.GoVersion, scanMap, patterns)
	if err != nil {
		return fmt.Errorf("analyze project: %w", err)
	}

	snapshot, err := store.CommitSnapshot(command.Name, command.Note, scanned, result, changes, fullReindex)
	if err != nil {
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
	info, store, scanned, previous, err := openPreparedProjectState(command)
	if err != nil {
		return err
	}
	defer store.Close()

	changes := indexer.Diff(scanned, previous)
	status, err := store.Status(len(changes.Added) + len(changes.Changed) + len(changes.Deleted))
	if err != nil {
		return err
	}

	if !status.HasSnapshot {
		if command.OutputMode == cli.OutputAI {
			_, err := fmt.Fprintf(stdout, "root=%s\nmodule=%s\nsnapshot=none\nchanged_now=%d\n", info.Root, info.ModulePath, status.ChangedNow)
			return err
		}
		_, err := fmt.Fprintf(stdout, "Root: %s\nModule: %s\nSnapshot: none\nChanged now: %d\n", info.Root, info.ModulePath, status.ChangedNow)
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
	info, store, _, _, err := openPreparedProjectState(command)
	if err != nil {
		return err
	}
	defer store.Close()

	match, found, err := resolveSingleSymbolQuery(stdout, info.ModulePath, store, command.Query)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}

	view, err := store.LoadSymbolView(match.SymbolKey)
	if err != nil {
		return err
	}
	switch command.OutputMode {
	case cli.OutputAI:
		return renderAISymbolView(stdout, info.ModulePath, view)
	default:
		return renderHumanSymbolView(stdout, info.Root, info.ModulePath, view)
	}
}

func runImpact(command cli.Command, stdout io.Writer) error {
	info, store, _, _, err := openPreparedProjectState(command)
	if err != nil {
		return err
	}
	defer store.Close()

	match, found, err := resolveSingleSymbolQuery(stdout, info.ModulePath, store, command.Query)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}

	view, err := store.LoadImpactView(match.SymbolKey, command.Depth)
	if err != nil {
		return err
	}
	switch command.OutputMode {
	case cli.OutputAI:
		return renderAIImpactView(stdout, info.ModulePath, view, command.Depth)
	default:
		return renderHumanImpactView(stdout, info.Root, info.ModulePath, view, command.Depth)
	}
}

func runDiff(command cli.Command, stdout io.Writer) error {
	_, store, _, _, err := openPreparedProjectState(command)
	if err != nil {
		return err
	}
	defer store.Close()

	diff, err := store.Diff(command.FromSnapshot, command.ToSnapshot)
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

func openProjectState(path string) (project.Info, *storage.Store, []indexer.ScanFile, map[string]indexer.PreviousFile, error) {
	info, err := project.Resolve(path)
	if err != nil {
		return project.Info{}, nil, nil, nil, err
	}

	store, err := storage.Open(info.DBPath)
	if err != nil {
		return project.Info{}, nil, nil, nil, err
	}

	if err := store.EnsureProject(info); err != nil {
		store.Close()
		return project.Info{}, nil, nil, nil, err
	}

	scanned, err := indexer.Scan(info.Root)
	if err != nil {
		store.Close()
		return project.Info{}, nil, nil, nil, fmt.Errorf("scan project files: %w", err)
	}

	previous, err := store.CurrentFiles()
	if err != nil {
		store.Close()
		return project.Info{}, nil, nil, nil, err
	}

	return info, store, scanned, previous, nil
}

func openPreparedProjectState(command cli.Command) (project.Info, *storage.Store, []indexer.ScanFile, map[string]indexer.PreviousFile, error) {
	info, store, scanned, previous, err := openProjectState(command.Root)
	if err != nil {
		return project.Info{}, nil, nil, nil, err
	}

	refreshed, err := maybeAutoRefreshProject(command, info, store, scanned, previous)
	if err != nil {
		store.Close()
		return project.Info{}, nil, nil, nil, err
	}
	if refreshed {
		previous, err = store.CurrentFiles()
		if err != nil {
			store.Close()
			return project.Info{}, nil, nil, nil, err
		}
	}
	return info, store, scanned, previous, nil
}

func maybeAutoRefreshProject(command cli.Command, info project.Info, store *storage.Store, scanned []indexer.ScanFile, previous map[string]indexer.PreviousFile) (bool, error) {
	if !shouldAutoRefresh(command.Name) {
		return false, nil
	}

	changes, impacted, fullReindex := indexer.DetectImpactedPackages(info.Root, info.ModulePath, scanned, previous)
	if len(changes.Added) == 0 && len(changes.Changed) == 0 && len(changes.Deleted) == 0 {
		return false, nil
	}

	current, hasCurrent, err := store.CurrentSnapshot()
	if err != nil {
		return false, err
	}
	if !hasCurrent {
		return false, nil
	}

	if !fullReindex && len(impacted) > 0 {
		reverse, err := store.ReverseDependencies(current.ID, impacted)
		if err != nil {
			return false, err
		}
		impacted = mergeStrings(impacted, reverse)
	}

	patterns := impacted
	if fullReindex {
		patterns = nil
	}

	scanMap := make(map[string]indexer.ScanFile, len(scanned))
	for _, file := range scanned {
		scanMap[file.RelPath] = file
	}

	result, err := indexer.Analyze(info.Root, info.ModulePath, info.GoVersion, scanMap, patterns)
	if err != nil {
		return false, fmt.Errorf("auto-refresh index: analyze project: %w", err)
	}

	if _, err := store.CommitSnapshot("update", "auto-refresh before "+command.Name, scanned, result, changes, fullReindex); err != nil {
		return false, fmt.Errorf("auto-refresh index: %w", err)
	}
	return true, nil
}

func shouldAutoRefresh(commandName string) bool {
	switch commandName {
	case "report", "shell", "status", "symbol", "impact", "diff":
		return true
	default:
		return false
	}
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

func mergeStrings(parts ...[]string) []string {
	seen := make(map[string]struct{})
	for _, part := range parts {
		for _, value := range part {
			if value == "" {
				continue
			}
			seen[value] = struct{}{}
		}
	}

	merged := make([]string, 0, len(seen))
	for value := range seen {
		merged = append(merged, value)
	}
	sort.Strings(merged)
	return merged
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
