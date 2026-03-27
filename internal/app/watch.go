package app

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/vladimirkasterin/ctx/internal/cli"
	"github.com/vladimirkasterin/ctx/internal/codebase"
	"github.com/vladimirkasterin/ctx/internal/storage"
)

type watchCycleResult struct {
	Action     string
	Plan       codebase.ChangePlan
	Snapshot   storage.SnapshotInfo
	WakeReason string
}

type watchWake struct {
	Triggered bool
	Reason    string
}

type watchBackend interface {
	Mode() string
	EventDriven() bool
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
	if _, err := fmt.Fprintf(stdout, "Watching %s every %s debounce=%s mode=%s\n", command.Root, interval, command.WatchDebounce, backend.Mode()); err != nil {
		return err
	}

	cycle := 0
	lastCycleAt := time.Time{}
	for {
		shouldRun := cycle == 0
		resultWake := watchWake{Triggered: cycle == 0, Reason: "startup"}
		if !shouldRun {
			wake, err := backend.Wait(interval)
			if err != nil {
				return err
			}
			if wake.Triggered && backend.EventDriven() && command.WatchDebounce > 0 {
				wake, err = coalesceWatchWake(backend, wake, command.WatchDebounce)
				if err != nil {
					return err
				}
			}
			shouldRun = wake.Triggered
			if !shouldRun && !lastCycleAt.IsZero() && time.Since(lastCycleAt) >= resyncEvery {
				shouldRun = true
				wake.Reason = "resync"
			}
			resultWake = wake
		}
		if !shouldRun {
			continue
		}

		cycle++
		result, err := runWatchCycle(command.Root)
		if err != nil {
			return err
		}
		result.WakeReason = resultWake.Reason
		lastCycleAt = time.Now()
		if shouldRenderWatchCycle(command, cycle, result) {
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

func shouldRenderWatchCycle(command cli.Command, cycle int, result watchCycleResult) bool {
	if result.Action != "idle" {
		return true
	}
	if command.WatchQuiet {
		return false
	}
	return cycle == 1 || command.Explain
}

func coalesceWatchWake(backend watchBackend, initial watchWake, debounce time.Duration) (watchWake, error) {
	if !initial.Triggered || debounce <= 0 || !backend.EventDriven() {
		return initial, nil
	}
	count := 1
	reasons := []string{strings.TrimSpace(initial.Reason)}
	deadline := time.Now().Add(debounce)
	for time.Until(deadline) > 0 {
		wake, err := backend.Wait(time.Until(deadline))
		if err != nil {
			return watchWake{}, err
		}
		if !wake.Triggered {
			break
		}
		count++
		if reason := strings.TrimSpace(wake.Reason); reason != "" && !watchReasonsContain(reasons, reason) {
			reasons = append(reasons, reason)
		}
	}
	result := initial
	if count > 1 {
		result.Reason = strings.Join(reasons, "+")
		if result.Reason == "" {
			result.Reason = "event-burst"
		}
		result.Reason += fmt.Sprintf(" x%d", count)
	}
	return result, nil
}

func watchReasonsContain(values []string, needle string) bool {
	return slices.Contains(values, needle)
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
		"cycle=%d action=%s wake=%q snapshot=%d changed_files=%d changed_packages=%d changed_symbols=%d cache_hit=%t reason=%q %s\n",
		cycle,
		result.Action,
		strings.TrimSpace(result.WakeReason),
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
		section := explainSection{
			Title: "Explain",
			Facts: []explainFact{
				{Key: "Cycle", Value: fmt.Sprintf("%d", cycle)},
				{Key: "Action", Value: result.Action},
				{Key: "Wake", Value: blankIf(strings.TrimSpace(result.WakeReason), "periodic scan")},
				{Key: "Strategy", Value: describeChangePlanStrategy(result.Plan)},
				{Key: "Reason", Value: blankIf(strings.TrimSpace(result.Plan.Reason), "none")},
				{Key: "Change cache", Value: fmt.Sprintf("%s (%s)", yesNo(result.Plan.CacheHit), shortFingerprint(result.Plan.Fingerprint))},
				{Key: "Changes", Value: fmt.Sprintf("added=%d changed=%d deleted=%d", len(result.Plan.Changes.Added), len(result.Plan.Changes.Changed), len(result.Plan.Changes.Deleted))},
				{Key: "Package scope", Value: fmt.Sprintf("direct=%d expanded=%d reused=%d (%d%%)", result.Plan.Metrics.DirectPackageCount, result.Plan.Metrics.ExpandedPackageCount, result.Plan.Metrics.ReusedPackageCount, result.Plan.Metrics.ReusePercent)},
			},
			Groups: []explainGroup{
				{
					Title: "Manifest semantics",
					Items: watchManifestExplainItems(result.Plan),
				},
			},
		}
		if err := renderHumanExplainSection(stdout, newPalette(), section); err != nil {
			return err
		}
	}
	return nil
}

func watchManifestExplainItems(plan codebase.ChangePlan) []explainItem {
	items := make([]explainItem, 0, len(plan.ManifestChanges))
	for _, delta := range plan.ManifestChanges {
		details := append([]string{}, delta.Details...)
		if delta.PrevValue != "" || delta.CurValue != "" {
			details = append(details, fmt.Sprintf("prev=%q current=%q", delta.PrevValue, delta.CurValue))
		}
		if len(delta.Packages) > 0 {
			details = append(details, fmt.Sprintf("packages=%s", strings.Join(delta.Packages, ", ")))
		}
		items = append(items, explainItem{
			Label:   fmt.Sprintf("%s [%s/%s]", delta.RelPath, delta.Kind, delta.Impact),
			Details: details,
		})
	}
	return items
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
