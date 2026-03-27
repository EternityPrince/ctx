package storage

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/vladimirkasterin/ctx/internal/codebase"
	"github.com/vladimirkasterin/ctx/internal/project"
)

func TestSetSnapshotLimitPrunesOldestSnapshots(t *testing.T) {
	store := openTestStore(t)

	insertTestSnapshot(t, store, 1, 0)
	insertTestSnapshot(t, store, 2, 0)
	insertTestSnapshot(t, store, 3, 3)

	if err := store.SetSnapshotLimit(2); err != nil {
		t.Fatalf("SetSnapshotLimit returned error: %v", err)
	}

	snapshots, err := store.ListSnapshots()
	if err != nil {
		t.Fatalf("ListSnapshots returned error: %v", err)
	}
	if len(snapshots) != 2 {
		t.Fatalf("expected 2 snapshots after prune, got %d", len(snapshots))
	}
	if snapshots[0].ID != 3 || snapshots[1].ID != 2 {
		t.Fatalf("unexpected snapshots after prune: %+v", snapshots)
	}
}

func TestDeleteAllSnapshotsClearsCurrent(t *testing.T) {
	store := openTestStore(t)

	insertTestSnapshot(t, store, 1, 0)
	insertTestSnapshot(t, store, 2, 2)

	removed, err := store.DeleteAllSnapshots()
	if err != nil {
		t.Fatalf("DeleteAllSnapshots returned error: %v", err)
	}
	if removed != 2 {
		t.Fatalf("expected 2 removed snapshots, got %d", removed)
	}

	current, ok, err := store.CurrentSnapshot()
	if err != nil {
		t.Fatalf("CurrentSnapshot returned error: %v", err)
	}
	if ok || current.ID != 0 {
		t.Fatalf("expected no current snapshot after delete all, got ok=%v current=%+v", ok, current)
	}
}

func TestCommitSnapshotStoresTelemetryAndSchemaVersion(t *testing.T) {
	store := openTestStore(t)

	version, err := store.SchemaVersion()
	if err != nil {
		t.Fatalf("SchemaVersion returned error: %v", err)
	}
	if version != ExpectedSchemaVersion() {
		t.Fatalf("unexpected schema version: got %d want %d", version, ExpectedSchemaVersion())
	}

	snapshot, err := store.CommitSnapshotWithTelemetry(
		"index",
		"telemetry test",
		[]codebase.ScanFile{
			{
				RelPath:   "go.mod",
				Identity:  "example.com/project",
				Hash:      "mod123",
				SizeBytes: 32,
				IsModule:  true,
			},
			{
				RelPath:           "main.go",
				PackageImportPath: "example.com/project",
				Hash:              "abc123",
				SizeBytes:         42,
				IsGo:              true,
			},
		},
		&codebase.Result{
			Root:            "/tmp/project",
			ModulePath:      "example.com/project",
			GoVersion:       "1.26",
			ImpactedPackage: map[string]struct{}{"example.com/project": {}},
			Packages: []codebase.PackageFact{
				{ImportPath: "example.com/project", Name: "main", DirPath: ".", FileCount: 1},
			},
			Symbols: []codebase.SymbolFact{
				{
					SymbolKey:         "example.com/project.main",
					QName:             "example.com/project.main",
					PackageImportPath: "example.com/project",
					FilePath:          "main.go",
					Name:              "main",
					Kind:              "func",
					Signature:         "func main()",
					Line:              1,
					Column:            1,
				},
			},
		},
		codebase.ChangeSet{Added: []string{"go.mod", "main.go"}},
		true,
		SnapshotCommitTelemetry{
			ScannedFiles:    7,
			ScanDuration:    15 * time.Millisecond,
			AnalyzeDuration: 25 * time.Millisecond,
		},
	)
	if err != nil {
		t.Fatalf("CommitSnapshotWithTelemetry returned error: %v", err)
	}
	if snapshot.ScannedFiles != 7 {
		t.Fatalf("expected scanned files=7, got %+v", snapshot)
	}
	if snapshot.ScanDurationMs != 15 || snapshot.AnalyzeDurationMs != 25 {
		t.Fatalf("expected scan/analyze telemetry, got %+v", snapshot)
	}
	if snapshot.WriteDurationMs < 0 {
		t.Fatalf("expected non-negative write duration, got %+v", snapshot)
	}
	if snapshot.TotalDurationMs() < 40 {
		t.Fatalf("expected total duration to include scan/analyze timings, got %+v", snapshot)
	}

	files, err := store.CurrentFiles()
	if err != nil {
		t.Fatalf("CurrentFiles returned error: %v", err)
	}
	if files["go.mod"].Identity != "example.com/project" {
		t.Fatalf("expected module file identity to roundtrip, got %+v", files["go.mod"])
	}
	if files["main.go"].Identity != "" {
		t.Fatalf("did not expect plain source file identity to be populated, got %+v", files["main.go"])
	}
}

func TestChangeCacheLifecycleFollowsSnapshot(t *testing.T) {
	store := openTestStore(t)

	insertTestSnapshot(t, store, 11, 0)
	plan := codebase.ChangePlan{
		Changes:          codebase.ChangeSet{Changed: []string{"pkg/service.go"}},
		ImpactedPackages: []string{"example.com/project/pkg"},
		Reason:           "package-scoped changes",
	}
	if err := store.SaveChangePlan(11, "fp-1", plan); err != nil {
		t.Fatalf("SaveChangePlan returned error: %v", err)
	}

	loaded, ok, err := store.LoadChangePlan(11, "fp-1")
	if err != nil {
		t.Fatalf("LoadChangePlan returned error: %v", err)
	}
	if !ok || !loaded.CacheHit || loaded.Reason != plan.Reason {
		t.Fatalf("expected cached plan roundtrip, got ok=%v plan=%+v", ok, loaded)
	}

	removed, err := store.DeleteSnapshots([]int64{11})
	if err != nil {
		t.Fatalf("DeleteSnapshots returned error: %v", err)
	}
	if removed != 1 {
		t.Fatalf("expected one snapshot to be removed, got %d", removed)
	}

	_, ok, err = store.LoadChangePlan(11, "fp-1")
	if err != nil {
		t.Fatalf("LoadChangePlan after delete returned error: %v", err)
	}
	if ok {
		t.Fatal("expected cached change plan to be removed with its snapshot")
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "index.sqlite")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.EnsureProject(project.Info{
		ID:         "test",
		Root:       "/tmp/project",
		ModulePath: "example.com/project",
		GoVersion:  "1.26",
	}); err != nil {
		t.Fatalf("EnsureProject returned error: %v", err)
	}
	return store
}

func insertTestSnapshot(t *testing.T, store *Store, id int64, currentID int64) {
	t.Helper()

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := store.db.Exec(`
		INSERT INTO snapshots (id, kind, created_at, note)
		VALUES (?, 'index', ?, '')
	`, id, now); err != nil {
		t.Fatalf("insert snapshot %d: %v", id, err)
	}
	if currentID != 0 {
		if _, err := store.db.Exec(`UPDATE project_meta SET current_snapshot_id = ?`, currentID); err != nil {
			t.Fatalf("set current snapshot %d: %v", currentID, err)
		}
	}
}
