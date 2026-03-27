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
	plan := projectService.Plan(state, forceFull)

	snapshot, committed, err := projectService.ApplySnapshot(state, command.Name, command.Note, forceFull)
	if err != nil {
		return err
	}
	if !committed {
		_, err := fmt.Fprintf(stdout, "No changes detected. current_snapshot=%d mode=%s direct_pkgs=%d expanded_pkgs=%d reused_pkgs=%d reuse=%d%% cache_hit=%t reason=%q\n", snapshot.ID, plan.Metrics.Mode, plan.Metrics.DirectPackageCount, plan.Metrics.ExpandedPackageCount, plan.Metrics.ReusedPackageCount, plan.Metrics.ReusePercent, plan.CacheHit, strings.TrimSpace(plan.Reason))
		return err
	}

	_, err = fmt.Fprintf(
		stdout,
		"snapshot=%d kind=%s packages=%d files=%d symbols=%d refs=%d calls=%d tests=%d changed_files=%d changed_packages=%d changed_symbols=%d scan_ms=%d analyze_ms=%d write_ms=%d scanned_files=%d mode=%s direct_pkgs=%d expanded_pkgs=%d reused_pkgs=%d reuse=%d%% cache_hit=%t reason=%q\n",
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
		snapshot.ScanDurationMs,
		snapshot.AnalyzeDurationMs,
		snapshot.WriteDurationMs,
		snapshot.ScannedFiles,
		snapshot.IncrementalMode,
		snapshot.DirectPackages,
		snapshot.ExpandedPackages,
		snapshot.ReusedPackages,
		snapshot.ReusePercent,
		plan.CacheHit,
		strings.TrimSpace(plan.Reason),
	)
	if err != nil {
		return err
	}
	if command.Explain {
		explained, err := buildChangePlanExplain(state, plan)
		if err != nil {
			return err
		}
		err = renderHumanExplainSection(stdout, newPalette(), explained)
	}
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
	plan := projectService.Plan(state, false)
	language := displayLanguage(state.Info.Language)
	version := strings.TrimSpace(state.Info.GoVersion)
	composition := summarizeProjectComposition(state.Scanned)
	capabilities := composition.Capabilities()

	if !status.HasSnapshot {
		if command.OutputMode == cli.OutputAI {
			_, err := fmt.Fprintf(stdout, "root=%s\nmodule=%s\nlanguage=%s\nversion=%s\ncomposition=%s\ncapabilities=%s\nsnapshot=none\nchanged_now=%d\nstorage_snapshots=%d\nstorage_limit=%d\nstorage_total_bytes=%d\nstorage_avg_snapshot_bytes=%d\ndb=%s\n", state.Info.Root, state.Info.ModulePath, state.Info.Language, version, composition.Display(), capabilities, status.ChangedNow, status.Storage.SnapshotCount, status.Storage.SnapshotLimit, status.Storage.TotalSizeBytes, status.Storage.AvgSnapshotSizeBytes, status.Storage.CurrentDBPath)
			if err != nil {
				return err
			}
		} else {
			_, err := fmt.Fprintf(stdout, "Root: %s\nModule: %s\nLanguage: %s\nComposition: %s\nCapabilities: %s\nSnapshot: none\nChanged now: %d\nStorage: snapshots=%d limit=%s total=%s avg=%s\nDB: %s\n", state.Info.Root, state.Info.ModulePath, language, composition.Display(), capabilities, status.ChangedNow, status.Storage.SnapshotCount, formatSnapshotLimit(status.Storage.SnapshotLimit), shellHumanSize(status.Storage.TotalSizeBytes), shellHumanSize(status.Storage.AvgSnapshotSizeBytes), status.Storage.CurrentDBPath)
			if err != nil {
				return err
			}
		}
		if command.Explain {
			explained, err := buildChangePlanExplain(state, plan)
			if err != nil {
				return err
			}
			switch command.OutputMode {
			case cli.OutputAI:
				return renderAIExplainSection(stdout, "explain", explained)
			default:
				return renderHumanExplainSection(stdout, newPalette(), explained)
			}
		}
		return nil
	}

	if command.OutputMode == cli.OutputAI {
		_, err = fmt.Fprintf(
			stdout,
			"root=%s\nmodule=%s\nlanguage=%s\nversion=%s\ncomposition=%s\ncapabilities=%s\nsnapshot=%d\nsnapshot_at=%s\npackages=%d\nfiles=%d\nsymbols=%d\nrefs=%d\ncalls=%d\ntests=%d\nscan_ms=%d\nanalyze_ms=%d\nwrite_ms=%d\nscanned_files=%d\nmode=%s\ndirect_packages=%d\nexpanded_packages=%d\nreused_packages=%d\nreuse_percent=%d\nchanged_now=%d\nstorage_snapshots=%d\nstorage_limit=%d\nstorage_total_bytes=%d\nstorage_avg_snapshot_bytes=%d\ndb=%s\n",
			status.RootPath,
			status.ModulePath,
			state.Info.Language,
			version,
			composition.Display(),
			capabilities,
			status.Current.ID,
			status.Current.CreatedAt.Format(timeFormat),
			status.Current.TotalPackages,
			status.Current.TotalFiles,
			status.Current.TotalSymbols,
			status.Current.TotalRefs,
			status.Current.TotalCalls,
			status.Current.TotalTests,
			status.Current.ScanDurationMs,
			status.Current.AnalyzeDurationMs,
			status.Current.WriteDurationMs,
			status.Current.ScannedFiles,
			status.Current.IncrementalMode,
			status.Current.DirectPackages,
			status.Current.ExpandedPackages,
			status.Current.ReusedPackages,
			status.Current.ReusePercent,
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
		"Root: %s\nModule: %s\nLanguage: %s\nVersion: %s\nComposition: %s\nCapabilities: %s\nSnapshot: %d (%s)\nInventory: packages=%d files=%d symbols=%d refs=%d calls=%d tests=%d\nTimings: %s\nChanged now: %d\nStorage: snapshots=%d limit=%s total=%s avg=%s\nDB: %s\n",
		status.RootPath,
		status.ModulePath,
		language,
		version,
		composition.Display(),
		capabilities,
		status.Current.ID,
		status.Current.CreatedAt.Format(timeFormat),
		status.Current.TotalPackages,
		status.Current.TotalFiles,
		status.Current.TotalSymbols,
		status.Current.TotalRefs,
		status.Current.TotalCalls,
		status.Current.TotalTests,
		formatSnapshotTelemetry(status.Current),
		status.ChangedNow,
		status.Storage.SnapshotCount,
		formatSnapshotLimit(status.Storage.SnapshotLimit),
		shellHumanSize(status.Storage.TotalSizeBytes),
		shellHumanSize(status.Storage.AvgSnapshotSizeBytes),
		status.Storage.CurrentDBPath,
	)
	if err != nil {
		return err
	}
	if command.Explain {
		explained, err := buildChangePlanExplain(state, plan)
		if err != nil {
			return err
		}
		switch command.OutputMode {
		case cli.OutputAI:
			err = renderAIExplainSection(stdout, "explain", explained)
		default:
			err = renderHumanExplainSection(stdout, newPalette(), explained)
		}
	}
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
	case "dev-reset":
		stats, err := storage.DeleteAllProjects()
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(stdout, "Dev reset complete: projects=%d snapshots=%d freed=%s\n", stats.ProjectsRemoved, stats.SnapshotsRemoved, shellHumanSize(stats.BytesFreed))
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
