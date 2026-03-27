package app

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/cli"
	"github.com/vladimirkasterin/ctx/internal/codebase"
	"github.com/vladimirkasterin/ctx/internal/config"
	"github.com/vladimirkasterin/ctx/internal/core"
	"github.com/vladimirkasterin/ctx/internal/project"
	"github.com/vladimirkasterin/ctx/internal/storage"
)

func runDoctor(command cli.Command, stdout io.Writer) error {
	root := command.Root
	cfg, cfgErr := config.LoadProject(root)
	hasConfig := cfgErr == nil && config.HasConfigFile(cfg)

	_, _ = fmt.Fprintf(stdout, "CTX Doctor\nRoot: %s\n", root)
	switch {
	case cfgErr != nil:
		_, _ = fmt.Fprintf(stdout, "Config: fail (%v)\n", cfgErr)
	case hasConfig:
		_, _ = fmt.Fprintf(stdout, "Config: ok (%s)\n", cfg.Path)
	default:
		_, _ = fmt.Fprintln(stdout, "Config: none")
	}

	info, resolveErr := project.Resolve(root)
	if resolveErr != nil {
		_, _ = fmt.Fprintf(stdout, "Project detection: fail (%v)\n", resolveErr)
		return nil
	}
	_, _ = fmt.Fprintf(stdout, "Project detection: ok\nResolved root: %s\nModule: %s\nLanguage: %s\nVersion: %s\n", info.Root, info.ModulePath, displayLanguage(info.Language), strings.TrimSpace(info.GoVersion))

	state, err := openProjectState(root)
	if err != nil {
		_, _ = fmt.Fprintf(stdout, "Open project: fail (%v)\n", err)
		return nil
	}
	defer state.Close()

	composition := summarizeProjectComposition(state.Scanned)
	_, _ = fmt.Fprintf(stdout, "Composition: %s\nCapabilities: %s\n", composition.Display(), composition.Capabilities())

	schemaVersion, schemaErr := state.Store.SchemaVersion()
	expectedSchema := storage.ExpectedSchemaVersion()
	switch {
	case schemaErr != nil:
		_, _ = fmt.Fprintf(stdout, "Schema: fail (%v)\n", schemaErr)
	case schemaVersion != expectedSchema:
		_, _ = fmt.Fprintf(stdout, "Schema: warn (version=%d expected=%d)\n", schemaVersion, expectedSchema)
	default:
		_, _ = fmt.Fprintf(stdout, "Schema: ok (version=%d)\n", schemaVersion)
	}

	quickCheck, quickCheckErr := state.Store.QuickCheck()
	switch {
	case quickCheckErr != nil:
		_, _ = fmt.Fprintf(stdout, "DB quick check: fail (%v)\n", quickCheckErr)
	case strings.EqualFold(strings.TrimSpace(quickCheck), "ok"):
		_, _ = fmt.Fprintln(stdout, "DB quick check: ok")
	default:
		_, _ = fmt.Fprintf(stdout, "DB quick check: warn (%s)\n", strings.TrimSpace(quickCheck))
	}

	status, err := state.Store.Status(projectService.ChangedNow(state))
	if err != nil {
		_, _ = fmt.Fprintf(stdout, "Storage status: fail (%v)\n", err)
		return nil
	}
	_, _ = fmt.Fprintf(stdout, "DB: %s\nSnapshots: %d\n", status.Storage.CurrentDBPath, status.Storage.SnapshotCount)
	if status.HasSnapshot {
		_, _ = fmt.Fprintf(stdout, "Current snapshot: %d (%s)\n", status.Current.ID, status.Current.CreatedAt.Format(timeFormat))
		_, _ = fmt.Fprintf(stdout, "Snapshot telemetry: %s\n", formatSnapshotTelemetry(status.Current))
		_, _ = fmt.Fprintf(stdout, "Snapshot chain: %s\n", doctorSnapshotChain(state.Store, status.Current))
		_, _ = fmt.Fprintf(stdout, "Snapshot inventory: %s\n", doctorInventoryHealth(status.Current, len(state.Previous)))
	} else {
		_, _ = fmt.Fprintln(stdout, "Current snapshot: none")
	}
	if status.ChangedNow == 0 {
		_, _ = fmt.Fprintln(stdout, "Freshness: clean")
	} else {
		_, _ = fmt.Fprintf(stdout, "Freshness: stale (%d local change(s) not reflected in snapshot)\n", status.ChangedNow)
	}

	plan := projectService.Plan(state, false)
	incremental, err := doctorIncrementalDiagnosis(state, status, plan)
	if err != nil {
		return err
	}
	cacheLabel := "miss"
	if plan.CacheHit {
		cacheLabel = "hit"
	}
	_, _ = fmt.Fprintf(stdout, "Incremental: %s\nChange cache: %s (%s)\n", incremental, cacheLabel, shortFingerprint(plan.Fingerprint))

	explained, err := buildChangePlanExplain(state, plan)
	if err != nil {
		return err
	}
	if err := renderHumanExplainSection(stdout, newPalette(), explained); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(stdout, "Ignore files: .gitignore=%t .ctxignore=%t .ctxconfig=%t\n", fileExists(filepath.Join(info.Root, ".gitignore")), fileExists(filepath.Join(info.Root, ".ctxignore")), hasConfig)
	if hasConfig {
		dumpProfile := config.EffectiveProfile(cfg, "dump")
		indexProfile := config.EffectiveProfile(cfg, "index")
		reportProfile := config.EffectiveProfile(cfg, "report")
		_, _ = fmt.Fprintf(stdout, "Config rules: dump include=%d exclude=%d index include=%d exclude=%d report include=%d exclude=%d\n", len(dumpProfile.IncludePaths), len(dumpProfile.ExcludePaths), len(indexProfile.IncludePaths), len(indexProfile.ExcludePaths), len(reportProfile.IncludePaths), len(reportProfile.ExcludePaths))
	}

	if composition.Python > 0 {
		if _, err := exec.LookPath("python3"); err != nil {
			_, _ = fmt.Fprintln(stdout, "Analyzer python: missing python3 in PATH")
		} else {
			_, _ = fmt.Fprintln(stdout, "Analyzer python: ok")
		}
	}
	if composition.Go > 0 {
		_, _ = fmt.Fprintln(stdout, "Analyzer go: ok")
	}
	if composition.Rust > 0 {
		_, _ = fmt.Fprintln(stdout, "Analyzer rust: best-effort")
	}
	return nil
}

func doctorIncrementalDiagnosis(state core.ProjectState, status storage.ProjectStatus, plan codebase.ChangePlan) (string, error) {
	switch {
	case !status.HasSnapshot:
		return "bootstrap required (no snapshot yet)", nil
	case plan.FullReindex:
		return fmt.Sprintf("full reindex required (changed=%d)", plan.Changes.Count()), nil
	case plan.Changes.Count() == 0:
		return "no-op ready (working tree already matches current snapshot)", nil
	default:
		reverseCount := 0
		if len(plan.ImpactedPackages) > 0 {
			reverse, err := state.Store.ReverseDependencies(status.Current.ID, plan.ImpactedPackages)
			if err != nil {
				return "", err
			}
			reverseCount = len(reverse)
		}
		return fmt.Sprintf("partial update ready (changed=%d direct_packages=%d reverse_packages=%d expanded_packages=%d reused=%d%%)", plan.Changes.Count(), len(plan.ImpactedPackages), reverseCount, plan.Metrics.ExpandedPackageCount, plan.Metrics.ReusePercent), nil
	}
}

func doctorSnapshotChain(store *storage.Store, snapshot storage.SnapshotInfo) string {
	if snapshot.ID == 0 {
		return "n/a"
	}
	if !snapshot.ParentID.Valid {
		return "ok (root snapshot)"
	}
	parent, err := store.SnapshotByID(snapshot.ParentID.Int64)
	if err != nil {
		return fmt.Sprintf("warn (missing parent snapshot %d)", snapshot.ParentID.Int64)
	}
	if parent.ID >= snapshot.ID {
		return fmt.Sprintf("warn (parent=%d is not older than current=%d)", parent.ID, snapshot.ID)
	}
	return fmt.Sprintf("ok (parent=%d)", parent.ID)
}

func doctorInventoryHealth(snapshot storage.SnapshotInfo, indexedFiles int) string {
	switch {
	case snapshot.ID == 0:
		return "n/a"
	case indexedFiles == 0 && snapshot.TotalFiles == 0:
		return "ok (no indexed files)"
	case snapshot.TotalFiles != indexedFiles:
		return fmt.Sprintf("warn (snapshot files=%d current file records=%d)", snapshot.TotalFiles, indexedFiles)
	default:
		return fmt.Sprintf("ok (current file records=%d)", indexedFiles)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
