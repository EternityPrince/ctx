package storage

import (
	"database/sql"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/vladimirkasterin/ctx/internal/codebase"
)

type SnapshotInfo struct {
	ID                int64
	ParentID          sql.NullInt64
	Kind              string
	Note              string
	CreatedAt         time.Time
	ChangedFiles      int
	ChangedPackages   int
	ChangedSymbols    int
	ScannedFiles      int
	ScanDurationMs    int
	AnalyzeDurationMs int
	WriteDurationMs   int
	IncrementalMode   string
	DirectPackages    int
	ExpandedPackages  int
	ReusedPackages    int
	ReusePercent      int
	PlanCacheHit      bool
	TotalPackages     int
	TotalFiles        int
	TotalSymbols      int
	TotalCalls        int
	TotalRefs         int
	TotalTests        int
}

type StorageStatus struct {
	CurrentDBPath        string
	SnapshotCount        int
	SnapshotLimit        int
	TotalSizeBytes       int64
	AvgSnapshotSizeBytes int64
}

type ProjectStatus struct {
	RootPath    string
	ModulePath  string
	GoVersion   string
	Current     SnapshotInfo
	HasSnapshot bool
	ChangedNow  int
	Storage     StorageStatus
}

func snapshotFromRow(snapshot SnapshotInfo, createdAt string) (SnapshotInfo, error) {
	parsed, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return SnapshotInfo{}, fmt.Errorf("parse snapshot time: %w", err)
	}
	snapshot.CreatedAt = parsed
	return snapshot, nil
}

func newBoolScanDest(target *bool) any {
	return sqlScannerFunc(func(src any) error {
		switch value := src.(type) {
		case int64:
			*target = value != 0
			return nil
		case int:
			*target = value != 0
			return nil
		case []byte:
			*target = string(value) != "0" && string(value) != ""
			return nil
		default:
			return fmt.Errorf("unsupported bool scan type %T", src)
		}
	})
}

type sqlScannerFunc func(src any) error

func (fn sqlScannerFunc) Scan(src any) error {
	return fn(src)
}

func (s SnapshotInfo) TotalDurationMs() int {
	return s.ScanDurationMs + s.AnalyzeDurationMs + s.WriteDurationMs
}

func (s SnapshotInfo) TimingBottleneck() string {
	if s.TotalDurationMs() == 0 {
		return "none"
	}
	stage := "scan"
	maxValue := s.ScanDurationMs
	if s.AnalyzeDurationMs > maxValue {
		stage = "analyze"
		maxValue = s.AnalyzeDurationMs
	}
	if s.WriteDurationMs > maxValue {
		stage = "write"
	}
	return stage
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
			s.scanned_files,
			s.scan_ms,
			s.analyze_ms,
			s.write_ms,
			s.incremental_mode,
			s.direct_packages,
			s.expanded_packages,
			s.reused_packages,
			s.reuse_percent,
			s.plan_cache_hit,
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
		&snapshot.ScannedFiles,
		&snapshot.ScanDurationMs,
		&snapshot.AnalyzeDurationMs,
		&snapshot.WriteDurationMs,
		&snapshot.IncrementalMode,
		&snapshot.DirectPackages,
		&snapshot.ExpandedPackages,
		&snapshot.ReusedPackages,
		&snapshot.ReusePercent,
		newBoolScanDest(&snapshot.PlanCacheHit),
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
	snapshot, err = snapshotFromRow(snapshot, createdAt)
	if err != nil {
		return SnapshotInfo{}, false, err
	}
	return snapshot, true, nil
}

func (s *Store) Status(changedNow int) (ProjectStatus, error) {
	status := ProjectStatus{
		ChangedNow: changedNow,
		Storage: StorageStatus{
			CurrentDBPath: s.dbPath,
		},
	}

	if stat, err := os.Stat(s.dbPath); err == nil {
		status.Storage.TotalSizeBytes = stat.Size()
	} else if !os.IsNotExist(err) {
		return ProjectStatus{}, fmt.Errorf("stat db: %w", err)
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

	if err := s.db.QueryRow(`SELECT COUNT(*) FROM snapshots`).Scan(&status.Storage.SnapshotCount); err != nil {
		return ProjectStatus{}, fmt.Errorf("count snapshots: %w", err)
	}
	if err := s.db.QueryRow(`SELECT snapshot_limit FROM project_meta LIMIT 1`).Scan(&status.Storage.SnapshotLimit); err != nil {
		return ProjectStatus{}, fmt.Errorf("load snapshot limit: %w", err)
	}
	if status.Storage.SnapshotCount > 0 {
		status.Storage.AvgSnapshotSizeBytes = status.Storage.TotalSizeBytes / int64(status.Storage.SnapshotCount)
	}
	return status, nil
}

func (s *Store) CurrentFiles() (map[string]codebase.PreviousFile, error) {
	snapshot, ok, err := s.CurrentSnapshot()
	if err != nil || !ok {
		return map[string]codebase.PreviousFile{}, err
	}

	rows, err := s.db.Query(`
		SELECT rel_path, package_import_path, identity, semantic_meta, content_hash, is_test
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
		if err := rows.Scan(&record.RelPath, &record.PackageImportPath, &record.Identity, &record.SemanticMeta, &record.Hash, &isTest); err != nil {
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
		       scanned_files, scan_ms, analyze_ms, write_ms,
		       incremental_mode, direct_packages, expanded_packages, reused_packages, reuse_percent, plan_cache_hit,
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
		&snapshot.ScannedFiles,
		&snapshot.ScanDurationMs,
		&snapshot.AnalyzeDurationMs,
		&snapshot.WriteDurationMs,
		&snapshot.IncrementalMode,
		&snapshot.DirectPackages,
		&snapshot.ExpandedPackages,
		&snapshot.ReusedPackages,
		&snapshot.ReusePercent,
		newBoolScanDest(&snapshot.PlanCacheHit),
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
	snapshot, err = snapshotFromRow(snapshot, createdAt)
	if err != nil {
		return SnapshotInfo{}, err
	}
	return snapshot, nil
}

func (s *Store) ListSnapshots() ([]SnapshotInfo, error) {
	rows, err := s.db.Query(`
		SELECT id, parent_id, kind, note, created_at, changed_files, changed_packages, changed_symbols,
		       scanned_files, scan_ms, analyze_ms, write_ms,
		       incremental_mode, direct_packages, expanded_packages, reused_packages, reuse_percent, plan_cache_hit,
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
			&snapshot.ScannedFiles,
			&snapshot.ScanDurationMs,
			&snapshot.AnalyzeDurationMs,
			&snapshot.WriteDurationMs,
			&snapshot.IncrementalMode,
			&snapshot.DirectPackages,
			&snapshot.ExpandedPackages,
			&snapshot.ReusedPackages,
			&snapshot.ReusePercent,
			newBoolScanDest(&snapshot.PlanCacheHit),
			&snapshot.TotalPackages,
			&snapshot.TotalFiles,
			&snapshot.TotalSymbols,
			&snapshot.TotalCalls,
			&snapshot.TotalRefs,
			&snapshot.TotalTests,
		); err != nil {
			return nil, fmt.Errorf("scan snapshot: %w", err)
		}
		snapshot, err = snapshotFromRow(snapshot, createdAt)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate snapshots: %w", err)
	}
	return snapshots, nil
}

func (s *Store) SetSnapshotLimit(limit int) error {
	if limit < 0 {
		return fmt.Errorf("snapshot limit cannot be negative")
	}
	if _, err := s.db.Exec(`UPDATE project_meta SET snapshot_limit = ?, updated_at = ?`, limit, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("set snapshot limit: %w", err)
	}
	return s.EnforceSnapshotLimit()
}

func (s *Store) EnforceSnapshotLimit() error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := enforceSnapshotLimitTx(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit snapshot prune: %w", err)
	}
	return vacuumDB(s.db)
}

func (s *Store) DeleteSnapshots(ids []int64) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	removed, err := deleteSnapshotsTx(tx, ids, false)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit snapshot delete: %w", err)
	}
	if removed > 0 {
		if err := vacuumDB(s.db); err != nil {
			return removed, err
		}
	}
	return removed, nil
}

func (s *Store) DeleteAllSnapshots() (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.Query(`SELECT id FROM snapshots ORDER BY id`)
	if err != nil {
		return 0, fmt.Errorf("query snapshots: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return 0, fmt.Errorf("scan snapshot id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate snapshots: %w", err)
	}

	removed, err := deleteSnapshotsTx(tx, ids, true)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit snapshot delete all: %w", err)
	}
	if removed > 0 {
		if err := vacuumDB(s.db); err != nil {
			return removed, err
		}
	}
	return removed, nil
}

func deleteSnapshotsTx(tx *sql.Tx, ids []int64, allowDeleteCurrent bool) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	currentID, err := currentSnapshotIDTx(tx)
	if err != nil {
		return 0, err
	}

	seen := make(map[int64]struct{}, len(ids))
	normalized := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if !allowDeleteCurrent && currentID != 0 && id == currentID {
			return 0, fmt.Errorf("cannot delete current snapshot %d", id)
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		normalized = append(normalized, id)
	}
	if len(normalized) == 0 {
		return 0, nil
	}
	slices.Sort(normalized)

	tables := []string{"packages", "files", "symbols", "package_deps", "refs", "call_edges", "flow_edges", "tests", "test_links", "change_cache", "snapshots"}
	removed := 0
	for _, id := range normalized {
		exists, err := snapshotExistsTx(tx, id)
		if err != nil {
			return removed, err
		}
		if !exists {
			continue
		}
		for _, table := range tables {
			column := "snapshot_id"
			if table == "snapshots" {
				column = "id"
			}
			if _, err := tx.Exec(`DELETE FROM `+table+` WHERE `+column+` = ?`, id); err != nil {
				return removed, fmt.Errorf("delete snapshot %d from %s: %w", id, table, err)
			}
		}
		removed++
	}

	nextCurrent := currentID
	if allowDeleteCurrent {
		if exists, err := snapshotExistsTx(tx, currentID); err != nil {
			return removed, err
		} else if !exists {
			nextCurrent, err = latestSnapshotIDTx(tx)
			if err != nil {
				return removed, err
			}
		}
	}
	if _, err := tx.Exec(`UPDATE project_meta SET current_snapshot_id = ?, updated_at = ?`, nextCurrent, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return removed, fmt.Errorf("update current snapshot after delete: %w", err)
	}
	return removed, nil
}

func enforceSnapshotLimitTx(tx *sql.Tx) (int, error) {
	limit, err := snapshotLimitTx(tx)
	if err != nil {
		return 0, err
	}
	if limit <= 0 {
		return 0, nil
	}

	currentID, err := currentSnapshotIDTx(tx)
	if err != nil {
		return 0, err
	}

	rows, err := tx.Query(`SELECT id FROM snapshots ORDER BY id DESC`)
	if err != nil {
		return 0, fmt.Errorf("query snapshots for limit: %w", err)
	}
	defer rows.Close()

	keep := make([]int64, 0, limit)
	var deleteIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return 0, fmt.Errorf("scan snapshot id: %w", err)
		}
		if id == currentID {
			keep = append(keep, id)
			continue
		}
		if len(keep) < limit {
			keep = append(keep, id)
			continue
		}
		deleteIDs = append(deleteIDs, id)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate snapshots for limit: %w", err)
	}
	return deleteSnapshotsTx(tx, deleteIDs, false)
}

func currentSnapshotIDTx(tx *sql.Tx) (int64, error) {
	var currentID int64
	if err := tx.QueryRow(`SELECT current_snapshot_id FROM project_meta LIMIT 1`).Scan(&currentID); err != nil {
		return 0, fmt.Errorf("load current snapshot id: %w", err)
	}
	return currentID, nil
}

func latestSnapshotIDTx(tx *sql.Tx) (int64, error) {
	var next sql.NullInt64
	if err := tx.QueryRow(`SELECT MAX(id) FROM snapshots`).Scan(&next); err != nil {
		return 0, fmt.Errorf("load latest snapshot id: %w", err)
	}
	if !next.Valid {
		return 0, nil
	}
	return next.Int64, nil
}

func snapshotExistsTx(tx *sql.Tx, id int64) (bool, error) {
	if id == 0 {
		return false, nil
	}
	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM snapshots WHERE id = ?`, id).Scan(&count); err != nil {
		return false, fmt.Errorf("check snapshot %d: %w", id, err)
	}
	return count > 0, nil
}

func snapshotLimitTx(tx *sql.Tx) (int, error) {
	var limit int
	if err := tx.QueryRow(`SELECT snapshot_limit FROM project_meta LIMIT 1`).Scan(&limit); err != nil {
		return 0, fmt.Errorf("load snapshot limit: %w", err)
	}
	return limit, nil
}

func vacuumDB(db *sql.DB) error {
	if _, err := db.Exec(`VACUUM`); err != nil {
		return fmt.Errorf("vacuum sqlite db: %w", err)
	}
	return nil
}

func (s *Store) CommitSnapshot(kind, note string, scanned []codebase.ScanFile, result *codebase.Result, changes codebase.ChangeSet, full bool) (SnapshotInfo, error) {
	return s.CommitSnapshotWithTelemetry(kind, note, scanned, result, changes, full, SnapshotCommitTelemetry{
		ScannedFiles: len(scanned),
	})
}

func (s *Store) CommitSnapshotWithTelemetry(kind, note string, scanned []codebase.ScanFile, result *codebase.Result, changes codebase.ChangeSet, full bool, telemetry SnapshotCommitTelemetry) (SnapshotInfo, error) {
	current, ok, err := s.CurrentSnapshot()
	if err != nil {
		return SnapshotInfo{}, err
	}
	writeStarted := time.Now()

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
			scanned_files, scan_ms, analyze_ms, write_ms,
			incremental_mode, direct_packages, expanded_packages, reused_packages, reuse_percent, plan_cache_hit,
			total_packages, total_files, total_symbols, total_calls, total_refs, total_tests
		) VALUES (?, ?, ?, ?, 0, 0, 0, 0, 0, 0, 0, '', 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
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
	if err := insertFlows(tx, snapshotID, result.Flows); err != nil {
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

	scannedFiles := telemetry.ScannedFiles
	if scannedFiles <= 0 {
		scannedFiles = len(scanned)
	}
	writeDurationMs := durationMillis(time.Since(writeStarted))
	_, err = tx.Exec(`
		UPDATE snapshots
		SET changed_files = ?, changed_packages = ?, changed_symbols = ?,
		    scanned_files = ?, scan_ms = ?, analyze_ms = ?, write_ms = ?,
		    incremental_mode = ?, direct_packages = ?, expanded_packages = ?, reused_packages = ?, reuse_percent = ?, plan_cache_hit = ?,
		    total_packages = ?, total_files = ?, total_symbols = ?, total_calls = ?, total_refs = ?, total_tests = ?
		WHERE id = ?
	`,
		changes.Count(),
		len(result.ImpactedPackage),
		changedSymbols,
		scannedFiles,
		durationMillis(telemetry.ScanDuration),
		durationMillis(telemetry.AnalyzeDuration),
		writeDurationMs,
		telemetry.PlanMode,
		telemetry.DirectPackages,
		telemetry.ExpandedPackages,
		telemetry.ReusedPackages,
		telemetry.ReusePercent,
		boolInt(telemetry.PlanCacheHit),
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
	pruned, err := enforceSnapshotLimitTx(tx)
	if err != nil {
		return SnapshotInfo{}, err
	}

	if err := tx.Commit(); err != nil {
		return SnapshotInfo{}, fmt.Errorf("commit snapshot: %w", err)
	}
	if pruned > 0 {
		if err := vacuumDB(s.db); err != nil {
			return SnapshotInfo{}, err
		}
	}

	return s.SnapshotByID(snapshotID)
}
