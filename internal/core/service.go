package core

import (
	"fmt"
	"strings"
	"time"

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
	Plan(state ProjectState, forceFull bool) codebase.ChangePlan
	ApplySnapshot(state ProjectState, kind, note string, forceFull bool) (storage.SnapshotInfo, bool, error)
	ChangedNow(state ProjectState) int
}

type ProjectState struct {
	Info               project.Info
	Store              *storage.Store
	Scanned            []codebase.ScanFile
	Previous           map[string]codebase.PreviousFile
	ScanDuration       time.Duration
	ChangeFingerprint  string
	CurrentSnapshot    storage.SnapshotInfo
	HasCurrentSnapshot bool
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

	dbPath, err := project.DBPath(info.ID)
	if err != nil {
		return ProjectState{}, err
	}

	store, err := storage.Open(dbPath)
	if err != nil {
		return ProjectState{}, err
	}

	if err := store.EnsureProject(info); err != nil {
		_ = store.Close()
		return ProjectState{}, err
	}

	scanStarted := time.Now()
	scanned, err := s.adapter.Scan(info.Root)
	if err != nil {
		_ = store.Close()
		return ProjectState{}, fmt.Errorf("scan project files: %w", err)
	}
	scanDuration := time.Since(scanStarted)

	previous, err := store.CurrentFiles()
	if err != nil {
		_ = store.Close()
		return ProjectState{}, err
	}
	current, hasCurrent, err := store.CurrentSnapshot()
	if err != nil {
		_ = store.Close()
		return ProjectState{}, err
	}

	return ProjectState{
		Info:               info,
		Store:              store,
		Scanned:            scanned,
		Previous:           previous,
		ScanDuration:       scanDuration,
		ChangeFingerprint:  codebase.ScanFingerprint(scanned),
		CurrentSnapshot:    current,
		HasCurrentSnapshot: hasCurrent,
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
	current, hasCurrent, err := state.Store.CurrentSnapshot()
	if err != nil {
		_ = state.Close()
		return ProjectState{}, err
	}
	state.Previous = previous
	state.CurrentSnapshot = current
	state.HasCurrentSnapshot = hasCurrent
	return state, nil
}

func (s *service) ApplySnapshot(state ProjectState, kind, note string, forceFull bool) (storage.SnapshotInfo, bool, error) {
	return s.applySnapshot(state, kind, note, forceFull)
}

func (s *service) Plan(state ProjectState, forceFull bool) codebase.ChangePlan {
	fingerprint := state.ChangeFingerprint
	if strings.TrimSpace(fingerprint) == "" {
		fingerprint = codebase.ScanFingerprint(state.Scanned)
	}
	if state.HasCurrentSnapshot {
		if cached, ok, err := state.Store.LoadChangePlan(state.CurrentSnapshot.ID, fingerprint); err == nil && ok {
			return finalizePlan(cached, fingerprint, true, forceFull)
		}
	}

	plan := s.adapter.DetectChanges(state.Info, state.Scanned, state.Previous)
	if state.HasCurrentSnapshot {
		_ = state.Store.SaveChangePlan(state.CurrentSnapshot.ID, fingerprint, plan)
	}
	return finalizePlan(plan, fingerprint, false, forceFull)
}

func (s *service) ChangedNow(state ProjectState) int {
	return codebase.Diff(state.Scanned, state.Previous).Count()
}

func (s *service) maybeAutoRefresh(state ProjectState, purpose string) (bool, error) {
	if !shouldAutoRefresh(purpose) {
		return false, nil
	}

	plan := s.Plan(state, false)
	if plan.Changes.Count() == 0 {
		return false, nil
	}

	if !state.HasCurrentSnapshot {
		return false, nil
	}

	if _, err := s.commitSnapshot(state, state.CurrentSnapshot, state.HasCurrentSnapshot, plan, "update", "auto-refresh before "+purpose); err != nil {
		return false, fmt.Errorf("auto-refresh index: %w", err)
	}
	return true, nil
}

func (s *service) applySnapshot(state ProjectState, kind, note string, forceFull bool) (storage.SnapshotInfo, bool, error) {
	plan := s.Plan(state, forceFull)

	if !plan.FullReindex && plan.Changes.Count() == 0 {
		if !state.HasCurrentSnapshot {
			plan.FullReindex = true
			if strings.TrimSpace(plan.Reason) == "" {
				plan.Reason = "bootstrap full reindex"
			}
		} else {
			return state.CurrentSnapshot, false, nil
		}
	}

	snapshot, err := s.commitSnapshot(state, state.CurrentSnapshot, state.HasCurrentSnapshot, plan, kind, note)
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

	analyzeStarted := time.Now()
	result, err := s.adapter.Analyze(state.Info, codebase.ScanMap(state.Scanned), patterns)
	if err != nil {
		return storage.SnapshotInfo{}, fmt.Errorf("analyze project: %w", err)
	}
	analyzeDuration := time.Since(analyzeStarted)

	snapshot, err := state.Store.CommitSnapshotWithTelemetry(kind, note, state.Scanned, result, plan.Changes, plan.FullReindex, storage.SnapshotCommitTelemetry{
		ScannedFiles:    len(state.Scanned),
		ScanDuration:    state.ScanDuration,
		AnalyzeDuration: analyzeDuration,
	})
	if err != nil {
		return storage.SnapshotInfo{}, err
	}
	if strings.TrimSpace(state.ChangeFingerprint) != "" {
		_ = state.Store.SaveChangePlan(snapshot.ID, state.ChangeFingerprint, codebase.ChangePlan{
			Reason: "no indexed file changes",
		})
	}
	return snapshot, nil
}

func finalizePlan(plan codebase.ChangePlan, fingerprint string, cacheHit, forceFull bool) codebase.ChangePlan {
	plan.Fingerprint = fingerprint
	plan.CacheHit = cacheHit
	if forceFull {
		if !plan.FullReindex {
			if strings.TrimSpace(plan.Reason) == "" {
				plan.Reason = "forced by command"
			} else {
				plan.Reason += "; forced by command"
			}
		}
		plan.FullReindex = true
	}
	if strings.TrimSpace(plan.Reason) == "" {
		switch {
		case plan.FullReindex:
			plan.Reason = "full reindex required"
		case plan.Changes.Count() > 0:
			plan.Reason = "package-scoped changes"
		default:
			plan.Reason = "no indexed file changes"
		}
	}
	return plan
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
