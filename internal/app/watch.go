package app

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/vladimirkasterin/ctx/internal/cli"
	"github.com/vladimirkasterin/ctx/internal/codebase"
	"github.com/vladimirkasterin/ctx/internal/storage"
)

type watchCycleResult struct {
	Action   string
	Plan     codebase.ChangePlan
	Snapshot storage.SnapshotInfo
}

type watchWake struct {
	Triggered bool
	Reason    string
}

type watchBackend interface {
	Mode() string
	Wait(timeout time.Duration) (watchWake, error)
	Close() error
}

var watchBackendFactory = newWatchBackend

func runWatch(command cli.Command, stdout io.Writer) error {
	backend, err := watchBackendFactory(command.Root)
	if err != nil {
		return err
	}
	defer backend.Close()
	return runWatchLoop(command, stdout, backend)
}

func runWatchLoop(command cli.Command, stdout io.Writer, backend watchBackend) error {
	interval := command.WatchInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	resyncEvery := watchResyncInterval(interval)
	if _, err := fmt.Fprintf(stdout, "Watching %s every %s mode=%s\n", command.Root, interval, backend.Mode()); err != nil {
		return err
	}

	cycle := 0
	lastCycleAt := time.Time{}
	for {
		shouldRun := cycle == 0
		if !shouldRun {
			wake, err := backend.Wait(interval)
			if err != nil {
				return err
			}
			shouldRun = wake.Triggered
			if !shouldRun && !lastCycleAt.IsZero() && time.Since(lastCycleAt) >= resyncEvery {
				shouldRun = true
			}
		}
		if !shouldRun {
			continue
		}

		cycle++
		result, err := runWatchCycle(command.Root)
		if err != nil {
			return err
		}
		lastCycleAt = time.Now()
		if result.Action != "idle" || cycle == 1 || command.Explain {
			if err := renderWatchCycle(stdout, cycle, result, command.Explain); err != nil {
				return err
			}
		}
		if command.WatchCycles > 0 && cycle >= command.WatchCycles {
			_, err := fmt.Fprintf(stdout, "Watch complete: cycles=%d\n", cycle)
			return err
		}
	}
}

func runWatchCycle(root string) (watchCycleResult, error) {
	state, err := openProjectState(root)
	if err != nil {
		return watchCycleResult{}, err
	}
	defer state.Close()

	plan := projectService.Plan(state, false)
	if !state.HasCurrentSnapshot {
		snapshot, _, err := projectService.ApplySnapshot(state, "index", "watch bootstrap", false)
		if err != nil {
			return watchCycleResult{}, err
		}
		return watchCycleResult{Action: "bootstrap", Plan: plan, Snapshot: snapshot}, nil
	}
	if !plan.FullReindex && plan.Changes.Count() == 0 {
		return watchCycleResult{Action: "idle", Plan: plan, Snapshot: state.CurrentSnapshot}, nil
	}

	kind := "update"
	if plan.FullReindex {
		kind = "index"
	}
	note := "watch refresh"
	if plan.FullReindex {
		note = "watch full refresh"
	}
	snapshot, _, err := projectService.ApplySnapshot(state, kind, note, false)
	if err != nil {
		return watchCycleResult{}, err
	}
	action := "update"
	if plan.FullReindex {
		action = "reindex"
	}
	return watchCycleResult{Action: action, Plan: plan, Snapshot: snapshot}, nil
}

func renderWatchCycle(stdout io.Writer, cycle int, result watchCycleResult, explain bool) error {
	if _, err := fmt.Fprintf(
		stdout,
		"cycle=%d action=%s snapshot=%d changed_files=%d changed_packages=%d changed_symbols=%d cache_hit=%t reason=%q %s\n",
		cycle,
		result.Action,
		result.Snapshot.ID,
		result.Snapshot.ChangedFiles,
		result.Snapshot.ChangedPackages,
		result.Snapshot.ChangedSymbols,
		result.Plan.CacheHit,
		strings.TrimSpace(result.Plan.Reason),
		formatSnapshotTelemetry(result.Snapshot),
	); err != nil {
		return err
	}
	if explain && result.Action != "idle" {
		if _, err := fmt.Fprintf(
			stdout,
			"  explain: full_reindex=%t added=%d changed=%d deleted=%d impacted=%d fingerprint=%s\n",
			result.Plan.FullReindex,
			len(result.Plan.Changes.Added),
			len(result.Plan.Changes.Changed),
			len(result.Plan.Changes.Deleted),
			len(result.Plan.ImpactedPackages),
			shortFingerprint(result.Plan.Fingerprint),
		); err != nil {
			return err
		}
	}
	return nil
}

func shortFingerprint(value string) string {
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}

func watchResyncInterval(interval time.Duration) time.Duration {
	if interval <= 0 {
		return 30 * time.Second
	}
	value := interval * 15
	if value < 30*time.Second {
		return 30 * time.Second
	}
	return value
}
