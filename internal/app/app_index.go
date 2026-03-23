package app

import (
	"fmt"
	"io"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/cli"
	"github.com/vladimirkasterin/ctx/internal/storage"
)

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
	language := displayLanguage(state.Info.Language)
	version := strings.TrimSpace(state.Info.GoVersion)

	if !status.HasSnapshot {
		if command.OutputMode == cli.OutputAI {
			_, err := fmt.Fprintf(stdout, "root=%s\nmodule=%s\nlanguage=%s\nversion=%s\nsnapshot=none\nchanged_now=%d\nstorage_snapshots=%d\nstorage_limit=%d\nstorage_total_bytes=%d\nstorage_avg_snapshot_bytes=%d\ndb=%s\n", state.Info.Root, state.Info.ModulePath, state.Info.Language, version, status.ChangedNow, status.Storage.SnapshotCount, status.Storage.SnapshotLimit, status.Storage.TotalSizeBytes, status.Storage.AvgSnapshotSizeBytes, status.Storage.CurrentDBPath)
			return err
		}
		_, err := fmt.Fprintf(stdout, "Root: %s\nModule: %s\nLanguage: %s\nSnapshot: none\nChanged now: %d\nStorage: snapshots=%d limit=%s total=%s avg=%s\nDB: %s\n", state.Info.Root, state.Info.ModulePath, language, status.ChangedNow, status.Storage.SnapshotCount, formatSnapshotLimit(status.Storage.SnapshotLimit), shellHumanSize(status.Storage.TotalSizeBytes), shellHumanSize(status.Storage.AvgSnapshotSizeBytes), status.Storage.CurrentDBPath)
		return err
	}

	if command.OutputMode == cli.OutputAI {
		_, err = fmt.Fprintf(
			stdout,
			"root=%s\nmodule=%s\nlanguage=%s\nversion=%s\nsnapshot=%d\nsnapshot_at=%s\npackages=%d\nfiles=%d\nsymbols=%d\nrefs=%d\ncalls=%d\ntests=%d\nchanged_now=%d\nstorage_snapshots=%d\nstorage_limit=%d\nstorage_total_bytes=%d\nstorage_avg_snapshot_bytes=%d\ndb=%s\n",
			status.RootPath,
			status.ModulePath,
			state.Info.Language,
			version,
			status.Current.ID,
			status.Current.CreatedAt.Format(timeFormat),
			status.Current.TotalPackages,
			status.Current.TotalFiles,
			status.Current.TotalSymbols,
			status.Current.TotalRefs,
			status.Current.TotalCalls,
			status.Current.TotalTests,
			status.ChangedNow,
			status.Storage.SnapshotCount,
			status.Storage.SnapshotLimit,
			status.Storage.TotalSizeBytes,
			status.Storage.AvgSnapshotSizeBytes,
			status.Storage.CurrentDBPath,
		)
		return err
	}

	_, err = fmt.Fprintf(
		stdout,
		"Root: %s\nModule: %s\nLanguage: %s\nVersion: %s\nSnapshot: %d (%s)\nInventory: packages=%d files=%d symbols=%d refs=%d calls=%d tests=%d\nChanged now: %d\nStorage: snapshots=%d limit=%s total=%s avg=%s\nDB: %s\n",
		status.RootPath,
		status.ModulePath,
		language,
		version,
		status.Current.ID,
		status.Current.CreatedAt.Format(timeFormat),
		status.Current.TotalPackages,
		status.Current.TotalFiles,
		status.Current.TotalSymbols,
		status.Current.TotalRefs,
		status.Current.TotalCalls,
		status.Current.TotalTests,
		status.ChangedNow,
		status.Storage.SnapshotCount,
		formatSnapshotLimit(status.Storage.SnapshotLimit),
		shellHumanSize(status.Storage.TotalSizeBytes),
		shellHumanSize(status.Storage.AvgSnapshotSizeBytes),
		status.Storage.CurrentDBPath,
	)
	return err
}

func displayLanguage(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	if len(value) == 1 {
		return strings.ToUpper(value)
	}
	return strings.ToUpper(value[:1]) + value[1:]
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
				"%s snapshot=%d snapshots=%d limit=%s size=%dB module=%s root=%s updated=%s\n",
				record.ID,
				record.CurrentSnapshotID,
				record.SnapshotCount,
				formatSnapshotLimit(record.SnapshotLimit),
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
	case "status":
		record, err := storage.ResolveProject(command.ProjectArg)
		if err != nil {
			return err
		}
		return runStatus(cli.Command{
			Name:       "status",
			Root:       record.RootPath,
			ProjectArg: record.ID,
			OutputMode: cli.OutputHuman,
		}, stdout)
	default:
		return fmt.Errorf("unsupported projects subcommand %q", command.ProjectsVerb)
	}
}

func formatSnapshotLimit(limit int) string {
	if limit <= 0 {
		return "unlimited"
	}
	return fmt.Sprintf("%d", limit)
}
