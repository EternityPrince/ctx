package storage

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/vladimirkasterin/ctx/internal/codebase"
)

type SnapshotInfo struct {
	ID              int64
	ParentID        sql.NullInt64
	Kind            string
	Note            string
	CreatedAt       time.Time
	ChangedFiles    int
	ChangedPackages int
	ChangedSymbols  int
	TotalPackages   int
	TotalFiles      int
	TotalSymbols    int
	TotalCalls      int
	TotalRefs       int
	TotalTests      int
}

type ProjectStatus struct {
	RootPath      string
	ModulePath    string
	GoVersion     string
	Current       SnapshotInfo
	HasSnapshot   bool
	ChangedNow    int
	CurrentDBPath string
}

func (s *Store) CurrentSnapshot() (SnapshotInfo, bool, error) {
	const query = `
		SELECT
			s.id,
			s.parent_id,
			s.kind,
			s.note,
			s.created_at,
			s.changed_files,
			s.changed_packages,
			s.changed_symbols,
			s.total_packages,
			s.total_files,
			s.total_symbols,
			s.total_calls,
			s.total_refs,
			s.total_tests
		FROM snapshots s
		JOIN project_meta p ON p.current_snapshot_id = s.id
		LIMIT 1
	`

	var snapshot SnapshotInfo
	var createdAt string
	err := s.db.QueryRow(query).Scan(
		&snapshot.ID,
		&snapshot.ParentID,
		&snapshot.Kind,
		&snapshot.Note,
		&createdAt,
		&snapshot.ChangedFiles,
		&snapshot.ChangedPackages,
		&snapshot.ChangedSymbols,
		&snapshot.TotalPackages,
		&snapshot.TotalFiles,
		&snapshot.TotalSymbols,
		&snapshot.TotalCalls,
		&snapshot.TotalRefs,
		&snapshot.TotalTests,
	)
	if err == sql.ErrNoRows {
		return SnapshotInfo{}, false, nil
	}
	if err != nil {
		return SnapshotInfo{}, false, fmt.Errorf("load current snapshot: %w", err)
	}

	snapshot.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return SnapshotInfo{}, false, fmt.Errorf("parse snapshot time: %w", err)
	}
	return snapshot, true, nil
}

func (s *Store) Status(changedNow int) (ProjectStatus, error) {
	status := ProjectStatus{
		ChangedNow:    changedNow,
		CurrentDBPath: s.dbPath,
	}

	err := s.db.QueryRow(`
		SELECT root_path, module_path, go_version
		FROM project_meta
		LIMIT 1
	`).Scan(&status.RootPath, &status.ModulePath, &status.GoVersion)
	if err == sql.ErrNoRows {
		return status, nil
	}
	if err != nil {
		return ProjectStatus{}, fmt.Errorf("load project meta: %w", err)
	}

	current, ok, err := s.CurrentSnapshot()
	if err != nil {
		return ProjectStatus{}, err
	}
	status.HasSnapshot = ok
	status.Current = current
	return status, nil
}

func (s *Store) CurrentFiles() (map[string]codebase.PreviousFile, error) {
	snapshot, ok, err := s.CurrentSnapshot()
	if err != nil || !ok {
		return map[string]codebase.PreviousFile{}, err
	}

	rows, err := s.db.Query(`
		SELECT rel_path, package_import_path, content_hash, is_test
		FROM files
		WHERE snapshot_id = ?
	`, snapshot.ID)
	if err != nil {
		return nil, fmt.Errorf("query current files: %w", err)
	}
	defer rows.Close()

	files := make(map[string]codebase.PreviousFile)
	for rows.Next() {
		var record codebase.PreviousFile
		var isTest int
		if err := rows.Scan(&record.RelPath, &record.PackageImportPath, &record.Hash, &isTest); err != nil {
			return nil, fmt.Errorf("scan current file: %w", err)
		}
		record.IsTest = isTest == 1
		files[record.RelPath] = record
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate current files: %w", err)
	}
	return files, nil
}

func (s *Store) SnapshotByID(id int64) (SnapshotInfo, error) {
	var snapshot SnapshotInfo
	var createdAt string
	err := s.db.QueryRow(`
		SELECT id, parent_id, kind, note, created_at, changed_files, changed_packages, changed_symbols,
		       total_packages, total_files, total_symbols, total_calls, total_refs, total_tests
		FROM snapshots
		WHERE id = ?
	`, id).Scan(
		&snapshot.ID,
		&snapshot.ParentID,
		&snapshot.Kind,
		&snapshot.Note,
		&createdAt,
		&snapshot.ChangedFiles,
		&snapshot.ChangedPackages,
		&snapshot.ChangedSymbols,
		&snapshot.TotalPackages,
		&snapshot.TotalFiles,
		&snapshot.TotalSymbols,
		&snapshot.TotalCalls,
		&snapshot.TotalRefs,
		&snapshot.TotalTests,
	)
	if err != nil {
		return SnapshotInfo{}, fmt.Errorf("load snapshot %d: %w", id, err)
	}
	snapshot.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return SnapshotInfo{}, fmt.Errorf("parse snapshot time: %w", err)
	}
	return snapshot, nil
}

func (s *Store) ListSnapshots() ([]SnapshotInfo, error) {
	rows, err := s.db.Query(`
		SELECT id, parent_id, kind, note, created_at, changed_files, changed_packages, changed_symbols,
		       total_packages, total_files, total_symbols, total_calls, total_refs, total_tests
		FROM snapshots
		ORDER BY id DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query snapshots: %w", err)
	}
	defer rows.Close()

	snapshots := make([]SnapshotInfo, 0, 8)
	for rows.Next() {
		var snapshot SnapshotInfo
		var createdAt string
		if err := rows.Scan(
			&snapshot.ID,
			&snapshot.ParentID,
			&snapshot.Kind,
			&snapshot.Note,
			&createdAt,
			&snapshot.ChangedFiles,
			&snapshot.ChangedPackages,
			&snapshot.ChangedSymbols,
			&snapshot.TotalPackages,
			&snapshot.TotalFiles,
			&snapshot.TotalSymbols,
			&snapshot.TotalCalls,
			&snapshot.TotalRefs,
			&snapshot.TotalTests,
		); err != nil {
			return nil, fmt.Errorf("scan snapshot: %w", err)
		}
		snapshot.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse snapshot time: %w", err)
		}
		snapshots = append(snapshots, snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate snapshots: %w", err)
	}
	return snapshots, nil
}

func (s *Store) CommitSnapshot(kind, note string, scanned []codebase.ScanFile, result *codebase.Result, changes codebase.ChangeSet, full bool) (SnapshotInfo, error) {
	current, ok, err := s.CurrentSnapshot()
	if err != nil {
		return SnapshotInfo{}, err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return SnapshotInfo{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var parentID any
	if ok {
		parentID = current.ID
	}

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := tx.Exec(`
		INSERT INTO snapshots (
			parent_id, kind, created_at, note,
			changed_files, changed_packages, changed_symbols,
			total_packages, total_files, total_symbols, total_calls, total_refs, total_tests
		) VALUES (?, ?, ?, ?, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	`, parentID, kind, now, note)
	if err != nil {
		return SnapshotInfo{}, fmt.Errorf("insert snapshot: %w", err)
	}

	snapshotID, err := res.LastInsertId()
	if err != nil {
		return SnapshotInfo{}, fmt.Errorf("read snapshot id: %w", err)
	}

	if ok && !full {
		impacted := packageList(result.ImpactedPackage)
		if err := s.copyForward(tx, current.ID, snapshotID, impacted); err != nil {
			return SnapshotInfo{}, err
		}
	}

	if err := insertFiles(tx, snapshotID, result.ModulePath, result.Root, scanned); err != nil {
		return SnapshotInfo{}, err
	}
	if err := insertPackages(tx, snapshotID, result.Packages); err != nil {
		return SnapshotInfo{}, err
	}
	if err := insertSymbols(tx, snapshotID, result.Symbols); err != nil {
		return SnapshotInfo{}, err
	}
	if err := insertDependencies(tx, snapshotID, result.Dependencies); err != nil {
		return SnapshotInfo{}, err
	}
	if err := insertReferences(tx, snapshotID, result.References); err != nil {
		return SnapshotInfo{}, err
	}
	if err := insertCalls(tx, snapshotID, result.Calls); err != nil {
		return SnapshotInfo{}, err
	}
	if err := insertTests(tx, snapshotID, result.Tests); err != nil {
		return SnapshotInfo{}, err
	}
	if err := insertTestLinks(tx, snapshotID, result.TestLinks); err != nil {
		return SnapshotInfo{}, err
	}

	changedSymbols, err := countChangedSymbols(tx, currentID(ok, current.ID), snapshotID)
	if err != nil {
		return SnapshotInfo{}, err
	}

	totalPackages, err := countTable(tx, "packages", snapshotID)
	if err != nil {
		return SnapshotInfo{}, err
	}
	totalFiles, err := countTable(tx, "files", snapshotID)
	if err != nil {
		return SnapshotInfo{}, err
	}
	totalSymbols, err := countTable(tx, "symbols", snapshotID)
	if err != nil {
		return SnapshotInfo{}, err
	}
	totalCalls, err := countTable(tx, "call_edges", snapshotID)
	if err != nil {
		return SnapshotInfo{}, err
	}
	totalRefs, err := countTable(tx, "refs", snapshotID)
	if err != nil {
		return SnapshotInfo{}, err
	}
	totalTests, err := countTable(tx, "tests", snapshotID)
	if err != nil {
		return SnapshotInfo{}, err
	}

	_, err = tx.Exec(`
		UPDATE snapshots
		SET changed_files = ?, changed_packages = ?, changed_symbols = ?,
		    total_packages = ?, total_files = ?, total_symbols = ?, total_calls = ?, total_refs = ?, total_tests = ?
		WHERE id = ?
	`,
		changes.Count(),
		len(result.ImpactedPackage),
		changedSymbols,
		totalPackages,
		totalFiles,
		totalSymbols,
		totalCalls,
		totalRefs,
		totalTests,
		snapshotID,
	)
	if err != nil {
		return SnapshotInfo{}, fmt.Errorf("update snapshot totals: %w", err)
	}

	_, err = tx.Exec(`UPDATE project_meta SET current_snapshot_id = ?, updated_at = ?`, snapshotID, now)
	if err != nil {
		return SnapshotInfo{}, fmt.Errorf("update project meta: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return SnapshotInfo{}, fmt.Errorf("commit snapshot: %w", err)
	}

	return s.SnapshotByID(snapshotID)
}
