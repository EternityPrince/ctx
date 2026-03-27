package app

import (
	"database/sql"
	"fmt"
	"io"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/cli"
	"github.com/vladimirkasterin/ctx/internal/storage"
)

func runSnapshots(command cli.Command, stdout io.Writer) error {
	switch command.SnapshotsVerb {
	case "", "list":
		return runSnapshotsList(command, stdout)
	case "rm":
		return runSnapshotsRemove(command, stdout)
	case "limit":
		return runSnapshotsLimit(command, stdout)
	default:
		return fmt.Errorf("unsupported snapshots subcommand %q", command.SnapshotsVerb)
	}
}

func runSnapshotsList(command cli.Command, stdout io.Writer) error {
	state, err := openPreparedProjectState(command)
	if err != nil {
		return err
	}
	defer state.Close()

	current, hasCurrent, err := state.Store.CurrentSnapshot()
	if err != nil {
		return err
	}
	if !hasCurrent {
		_, err := fmt.Fprintf(stdout, "No index snapshot for %s. Run `ctx index %s` first.\n", state.Info.ModulePath, state.Info.Root)
		return err
	}

	snapshots, err := state.Store.ListSnapshots()
	if err != nil {
		return err
	}

	switch command.OutputMode {
	case cli.OutputAI:
		return renderAISnapshots(stdout, state.Info.Root, state.Info.ModulePath, current.ID, snapshots)
	default:
		return renderHumanSnapshots(stdout, state.Info.Root, state.Info.ModulePath, current.ID, snapshots)
	}
}

func runSnapshotsRemove(command cli.Command, stdout io.Writer) error {
	state, err := openPreparedProjectState(command)
	if err != nil {
		return err
	}
	defer state.Close()

	if command.Query == "all" {
		removed, err := state.Store.DeleteAllSnapshots()
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(stdout, "Removed %d snapshot(s)\n", removed)
		return err
	}

	removed, err := state.Store.DeleteSnapshots(command.SnapshotIDs)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "Removed %d snapshot(s)\n", removed)
	return err
}

func runSnapshotsLimit(command cli.Command, stdout io.Writer) error {
	state, err := openPreparedProjectState(command)
	if err != nil {
		return err
	}
	defer state.Close()

	if err := state.Store.SetSnapshotLimit(command.SnapshotLimit); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "Snapshot limit set to %s\n", formatSnapshotLimit(command.SnapshotLimit))
	return err
}

func runSnapshot(command cli.Command, stdout io.Writer) error {
	state, err := openPreparedProjectState(command)
	if err != nil {
		return err
	}
	defer state.Close()

	current, hasCurrent, err := state.Store.CurrentSnapshot()
	if err != nil {
		return err
	}
	if !hasCurrent {
		_, err := fmt.Fprintf(stdout, "No index snapshot for %s. Run `ctx index %s` first.\n", state.Info.ModulePath, state.Info.Root)
		return err
	}

	snapshotID := command.SnapshotID
	if snapshotID == 0 {
		snapshotID = current.ID
	}

	snapshot, err := state.Store.SnapshotByID(snapshotID)
	if err != nil {
		return err
	}

	switch command.OutputMode {
	case cli.OutputAI:
		return renderAISnapshot(stdout, state.Info.Root, state.Info.ModulePath, snapshot, current.ID == snapshot.ID)
	default:
		return renderHumanSnapshot(stdout, state.Info.Root, state.Info.ModulePath, snapshot, current.ID == snapshot.ID)
	}
}

func renderHumanSnapshots(stdout io.Writer, root, modulePath string, currentID int64, snapshots []storage.SnapshotInfo) error {
	if _, err := fmt.Fprintf(stdout, "Root: %s\nModule: %s\nCurrent snapshot: %d\nSnapshots: %d\n\n", root, modulePath, currentID, len(snapshots)); err != nil {
		return err
	}
	for _, snapshot := range snapshots {
		marker := " "
		if snapshot.ID == currentID {
			marker = "*"
		}
		if _, err := fmt.Fprintf(
			stdout,
			"%s %d kind=%s at=%s changed_files=%d changed_packages=%d changed_symbols=%d packages=%d files=%d symbols=%d refs=%d calls=%d tests=%d %s%s\n",
			marker,
			snapshot.ID,
			snapshot.Kind,
			snapshot.CreatedAt.Format(timeFormat),
			snapshot.ChangedFiles,
			snapshot.ChangedPackages,
			snapshot.ChangedSymbols,
			snapshot.TotalPackages,
			snapshot.TotalFiles,
			snapshot.TotalSymbols,
			snapshot.TotalRefs,
			snapshot.TotalCalls,
			snapshot.TotalTests,
			formatSnapshotTelemetry(snapshot),
			formatSnapshotNote(snapshot.Note),
		); err != nil {
			return err
		}
	}
	return nil
}

func renderAISnapshots(stdout io.Writer, root, modulePath string, currentID int64, snapshots []storage.SnapshotInfo) error {
	if _, err := fmt.Fprintf(stdout, "root=%s\nmodule=%s\ncurrent_snapshot=%d\nsnapshot_count=%d\n", root, modulePath, currentID, len(snapshots)); err != nil {
		return err
	}
	for _, snapshot := range snapshots {
		if _, err := fmt.Fprintf(
			stdout,
			"snapshot=%d kind=%s at=%s parent=%s changed_files=%d changed_packages=%d changed_symbols=%d packages=%d files=%d symbols=%d refs=%d calls=%d tests=%d scan_ms=%d analyze_ms=%d write_ms=%d scanned_files=%d current=%t note=%q\n",
			snapshot.ID,
			snapshot.Kind,
			snapshot.CreatedAt.Format(timeFormat),
			parentSnapshotValue(snapshot.ParentID),
			snapshot.ChangedFiles,
			snapshot.ChangedPackages,
			snapshot.ChangedSymbols,
			snapshot.TotalPackages,
			snapshot.TotalFiles,
			snapshot.TotalSymbols,
			snapshot.TotalRefs,
			snapshot.TotalCalls,
			snapshot.TotalTests,
			snapshot.ScanDurationMs,
			snapshot.AnalyzeDurationMs,
			snapshot.WriteDurationMs,
			snapshot.ScannedFiles,
			snapshot.ID == currentID,
			snapshot.Note,
		); err != nil {
			return err
		}
	}
	return nil
}

func renderHumanSnapshot(stdout io.Writer, root, modulePath string, snapshot storage.SnapshotInfo, isCurrent bool) error {
	currentLabel := "no"
	if isCurrent {
		currentLabel = "yes"
	}
	if _, err := fmt.Fprintf(
		stdout,
		"Snapshot: %d\nRoot: %s\nModule: %s\nKind: %s\nCreated: %s\nParent: %s\nCurrent: %s\nChanged: files=%d packages=%d symbols=%d\nInventory: packages=%d files=%d symbols=%d refs=%d calls=%d tests=%d\nTimings: %s\n",
		snapshot.ID,
		root,
		modulePath,
		snapshot.Kind,
		snapshot.CreatedAt.Format(timeFormat),
		parentSnapshotValue(snapshot.ParentID),
		currentLabel,
		snapshot.ChangedFiles,
		snapshot.ChangedPackages,
		snapshot.ChangedSymbols,
		snapshot.TotalPackages,
		snapshot.TotalFiles,
		snapshot.TotalSymbols,
		snapshot.TotalRefs,
		snapshot.TotalCalls,
		snapshot.TotalTests,
		formatSnapshotTelemetry(snapshot),
	); err != nil {
		return err
	}
	if note := strings.TrimSpace(snapshot.Note); note != "" {
		_, err := fmt.Fprintf(stdout, "Note: %s\n", note)
		return err
	}
	return nil
}

func renderAISnapshot(stdout io.Writer, root, modulePath string, snapshot storage.SnapshotInfo, isCurrent bool) error {
	_, err := fmt.Fprintf(
		stdout,
		"root=%s\nmodule=%s\nsnapshot=%d\nkind=%s\ncreated_at=%s\nparent=%s\ncurrent=%t\nchanged_files=%d\nchanged_packages=%d\nchanged_symbols=%d\npackages=%d\nfiles=%d\nsymbols=%d\nrefs=%d\ncalls=%d\ntests=%d\nscan_ms=%d\nanalyze_ms=%d\nwrite_ms=%d\nscanned_files=%d\nnote=%q\n",
		root,
		modulePath,
		snapshot.ID,
		snapshot.Kind,
		snapshot.CreatedAt.Format(timeFormat),
		parentSnapshotValue(snapshot.ParentID),
		isCurrent,
		snapshot.ChangedFiles,
		snapshot.ChangedPackages,
		snapshot.ChangedSymbols,
		snapshot.TotalPackages,
		snapshot.TotalFiles,
		snapshot.TotalSymbols,
		snapshot.TotalRefs,
		snapshot.TotalCalls,
		snapshot.TotalTests,
		snapshot.ScanDurationMs,
		snapshot.AnalyzeDurationMs,
		snapshot.WriteDurationMs,
		snapshot.ScannedFiles,
		snapshot.Note,
	)
	return err
}

func formatSnapshotNote(note string) string {
	note = strings.TrimSpace(note)
	if note == "" {
		return ""
	}
	return fmt.Sprintf(" note=%q", note)
}

func parentSnapshotValue(parentID sql.NullInt64) string {
	if !parentID.Valid {
		return "none"
	}
	return fmt.Sprintf("%d", parentID.Int64)
}
