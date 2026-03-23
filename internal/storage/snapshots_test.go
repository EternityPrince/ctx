package storage

import (
	"path/filepath"
	"testing"
	"time"

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
		DBPath:     dbPath,
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
