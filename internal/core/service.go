package core

import (
	"fmt"

	"github.com/vladimirkasterin/ctx/internal/codebase"
	"github.com/vladimirkasterin/ctx/internal/project"
	"github.com/vladimirkasterin/ctx/internal/storage"
)

type Adapter interface {
	Scan(root string) ([]codebase.ScanFile, error)
	DetectChanges(info project.Info, scanned []codebase.ScanFile, previous map[string]codebase.PreviousFile) codebase.ChangePlan
	Analyze(info project.Info, scanned map[string]codebase.ScanFile, patterns []string) (*codebase.Result, error)
}

type ProjectService interface {
	OpenProject(path string) (ProjectState, error)
	PrepareProject(path, purpose string) (ProjectState, error)
	ApplySnapshot(state ProjectState, kind, note string, forceFull bool) (storage.SnapshotInfo, bool, error)
	ChangedNow(state ProjectState) int
}

type ProjectState struct {
	Info     project.Info
	Store    *storage.Store
	Scanned  []codebase.ScanFile
	Previous map[string]codebase.PreviousFile
}

func (s ProjectState) Close() error {
	if s.Store == nil {
		return nil
	}
	return s.Store.Close()
}

type service struct {
	adapter Adapter
}

func NewService(adapter Adapter) ProjectService {
	return &service{adapter: adapter}
}

func (s *service) OpenProject(path string) (ProjectState, error) {
	info, err := project.Resolve(path)
	if err != nil {
		return ProjectState{}, err
	}

	store, err := storage.Open(info.DBPath)
	if err != nil {
		return ProjectState{}, err
	}

	if err := store.EnsureProject(info); err != nil {
		_ = store.Close()
		return ProjectState{}, err
	}

	scanned, err := s.adapter.Scan(info.Root)
	if err != nil {
		_ = store.Close()
		return ProjectState{}, fmt.Errorf("scan project files: %w", err)
	}

	previous, err := store.CurrentFiles()
	if err != nil {
		_ = store.Close()
		return ProjectState{}, err
	}

	return ProjectState{
		Info:     info,
		Store:    store,
		Scanned:  scanned,
		Previous: previous,
	}, nil
}

func (s *service) PrepareProject(path, purpose string) (ProjectState, error) {
	state, err := s.OpenProject(path)
	if err != nil {
		return ProjectState{}, err
	}

	refreshed, err := s.maybeAutoRefresh(state, purpose)
	if err != nil {
		_ = state.Close()
		return ProjectState{}, err
	}
	if !refreshed {
		return state, nil
	}

	previous, err := state.Store.CurrentFiles()
	if err != nil {
		_ = state.Close()
		return ProjectState{}, err
	}
	state.Previous = previous
	return state, nil
}

func (s *service) ApplySnapshot(state ProjectState, kind, note string, forceFull bool) (storage.SnapshotInfo, bool, error) {
	return s.applySnapshot(state, kind, note, forceFull)
}

func (s *service) ChangedNow(state ProjectState) int {
	return codebase.Diff(state.Scanned, state.Previous).Count()
}

func (s *service) maybeAutoRefresh(state ProjectState, purpose string) (bool, error) {
	if !shouldAutoRefresh(purpose) {
		return false, nil
	}

	plan := s.adapter.DetectChanges(state.Info, state.Scanned, state.Previous)
	if plan.Changes.Count() == 0 {
		return false, nil
	}

	current, hasCurrent, err := state.Store.CurrentSnapshot()
	if err != nil {
		return false, err
	}
	if !hasCurrent {
		return false, nil
	}

	if _, err := s.commitSnapshot(state, current, hasCurrent, plan, "update", "auto-refresh before "+purpose); err != nil {
		return false, fmt.Errorf("auto-refresh index: %w", err)
	}
	return true, nil
}

func (s *service) applySnapshot(state ProjectState, kind, note string, forceFull bool) (storage.SnapshotInfo, bool, error) {
	plan := s.adapter.DetectChanges(state.Info, state.Scanned, state.Previous)
	if forceFull {
		plan.FullReindex = true
	}

	if !plan.FullReindex && plan.Changes.Count() == 0 {
		current, ok, err := state.Store.CurrentSnapshot()
		if err != nil {
			return storage.SnapshotInfo{}, false, err
		}
		if !ok {
			plan.FullReindex = true
		} else {
			return current, false, nil
		}
	}

	current, hasCurrent, err := state.Store.CurrentSnapshot()
	if err != nil {
		return storage.SnapshotInfo{}, false, err
	}

	snapshot, err := s.commitSnapshot(state, current, hasCurrent, plan, kind, note)
	if err != nil {
		return storage.SnapshotInfo{}, false, err
	}
	return snapshot, true, nil
}

func (s *service) commitSnapshot(state ProjectState, current storage.SnapshotInfo, hasCurrent bool, plan codebase.ChangePlan, kind, note string) (storage.SnapshotInfo, error) {
	patterns := plan.ImpactedPackages
	if !plan.FullReindex && hasCurrent && len(patterns) > 0 {
		reverse, err := state.Store.ReverseDependencies(current.ID, patterns)
		if err != nil {
			return storage.SnapshotInfo{}, err
		}
		patterns = mergeStringLists(patterns, reverse)
	}
	if plan.FullReindex {
		patterns = nil
	}

	result, err := s.adapter.Analyze(state.Info, codebase.ScanMap(state.Scanned), patterns)
	if err != nil {
		return storage.SnapshotInfo{}, fmt.Errorf("analyze project: %w", err)
	}

	snapshot, err := state.Store.CommitSnapshot(kind, note, state.Scanned, result, plan.Changes, plan.FullReindex)
	if err != nil {
		return storage.SnapshotInfo{}, err
	}
	return snapshot, nil
}

func shouldAutoRefresh(commandName string) bool {
	switch commandName {
	case "report", "shell", "status", "symbol", "impact", "diff", "snapshot", "snapshots":
		return true
	default:
		return false
	}
}

func mergeStringLists(parts ...[]string) []string {
	seen := make(map[string]struct{})
	merged := make([]string, 0)
	for _, values := range parts {
		for _, value := range values {
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			merged = append(merged, value)
		}
	}
	return merged
}
