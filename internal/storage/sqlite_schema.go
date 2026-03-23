package storage

import (
	"database/sql"
	"fmt"
	"strings"
)

func (s *Store) init() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS project_meta (
			project_id TEXT NOT NULL,
			root_path TEXT NOT NULL,
			module_path TEXT NOT NULL,
			go_version TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			current_snapshot_id INTEGER NOT NULL DEFAULT 0,
			snapshot_limit INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS snapshots (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			parent_id INTEGER,
			kind TEXT NOT NULL,
			created_at TEXT NOT NULL,
			note TEXT NOT NULL DEFAULT '',
			changed_files INTEGER NOT NULL DEFAULT 0,
			changed_packages INTEGER NOT NULL DEFAULT 0,
			changed_symbols INTEGER NOT NULL DEFAULT 0,
			total_packages INTEGER NOT NULL DEFAULT 0,
			total_files INTEGER NOT NULL DEFAULT 0,
			total_symbols INTEGER NOT NULL DEFAULT 0,
			total_calls INTEGER NOT NULL DEFAULT 0,
			total_refs INTEGER NOT NULL DEFAULT 0,
			total_tests INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS packages (
			snapshot_id INTEGER NOT NULL,
			import_path TEXT NOT NULL,
			name TEXT NOT NULL,
			dir_path TEXT NOT NULL,
			file_count INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (snapshot_id, import_path)
		)`,
		`CREATE TABLE IF NOT EXISTS files (
			snapshot_id INTEGER NOT NULL,
			rel_path TEXT NOT NULL,
			package_import_path TEXT NOT NULL,
			content_hash TEXT NOT NULL,
			size_bytes INTEGER NOT NULL,
			is_test INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (snapshot_id, rel_path)
		)`,
		`CREATE TABLE IF NOT EXISTS symbols (
			snapshot_id INTEGER NOT NULL,
			symbol_key TEXT NOT NULL,
			qname TEXT NOT NULL,
			package_import_path TEXT NOT NULL,
			file_path TEXT NOT NULL,
			name TEXT NOT NULL,
			kind TEXT NOT NULL,
			receiver TEXT NOT NULL DEFAULT '',
			signature TEXT NOT NULL DEFAULT '',
			doc TEXT NOT NULL DEFAULT '',
			line INTEGER NOT NULL,
			col INTEGER NOT NULL,
			exported INTEGER NOT NULL DEFAULT 0,
			is_test INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (snapshot_id, symbol_key)
		)`,
		`CREATE TABLE IF NOT EXISTS package_deps (
			snapshot_id INTEGER NOT NULL,
			from_package_import_path TEXT NOT NULL,
			to_package_import_path TEXT NOT NULL,
			is_local INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (snapshot_id, from_package_import_path, to_package_import_path)
		)`,
		`CREATE TABLE IF NOT EXISTS refs (
			snapshot_id INTEGER NOT NULL,
			from_package_import_path TEXT NOT NULL,
			from_symbol_key TEXT NOT NULL DEFAULT '',
			to_symbol_key TEXT NOT NULL,
			file_path TEXT NOT NULL,
			line INTEGER NOT NULL,
			col INTEGER NOT NULL,
			kind TEXT NOT NULL,
			PRIMARY KEY (snapshot_id, from_package_import_path, file_path, line, col, to_symbol_key, from_symbol_key)
		)`,
		`CREATE TABLE IF NOT EXISTS call_edges (
			snapshot_id INTEGER NOT NULL,
			caller_package_import_path TEXT NOT NULL,
			caller_symbol_key TEXT NOT NULL,
			callee_symbol_key TEXT NOT NULL,
			file_path TEXT NOT NULL,
			line INTEGER NOT NULL,
			col INTEGER NOT NULL,
			dispatch TEXT NOT NULL,
			PRIMARY KEY (snapshot_id, caller_symbol_key, callee_symbol_key, file_path, line, col)
		)`,
		`CREATE TABLE IF NOT EXISTS tests (
			snapshot_id INTEGER NOT NULL,
			test_key TEXT NOT NULL,
			package_import_path TEXT NOT NULL,
			file_path TEXT NOT NULL,
			name TEXT NOT NULL,
			kind TEXT NOT NULL,
			line INTEGER NOT NULL,
			PRIMARY KEY (snapshot_id, test_key)
		)`,
		`CREATE TABLE IF NOT EXISTS test_links (
			snapshot_id INTEGER NOT NULL,
			test_package_import_path TEXT NOT NULL,
			test_key TEXT NOT NULL,
			symbol_key TEXT NOT NULL,
			link_kind TEXT NOT NULL,
			confidence TEXT NOT NULL,
			PRIMARY KEY (snapshot_id, test_key, symbol_key, link_kind)
		)`,
		`CREATE INDEX IF NOT EXISTS symbols_name_idx ON symbols (snapshot_id, name)`,
		`CREATE INDEX IF NOT EXISTS symbols_qname_idx ON symbols (snapshot_id, qname)`,
		`CREATE INDEX IF NOT EXISTS files_pkg_idx ON files (snapshot_id, package_import_path)`,
		`CREATE INDEX IF NOT EXISTS calls_caller_idx ON call_edges (snapshot_id, caller_symbol_key)`,
		`CREATE INDEX IF NOT EXISTS calls_callee_idx ON call_edges (snapshot_id, callee_symbol_key)`,
		`CREATE INDEX IF NOT EXISTS refs_target_idx ON refs (snapshot_id, to_symbol_key)`,
		`CREATE INDEX IF NOT EXISTS refs_source_idx ON refs (snapshot_id, from_symbol_key)`,
		`CREATE INDEX IF NOT EXISTS deps_target_idx ON package_deps (snapshot_id, to_package_import_path)`,
		`CREATE INDEX IF NOT EXISTS tests_symbol_idx ON test_links (snapshot_id, symbol_key)`,
	}

	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("init sqlite schema: %w", err)
		}
	}
	if _, err := s.db.Exec(`ALTER TABLE project_meta ADD COLUMN snapshot_limit INTEGER NOT NULL DEFAULT 0`); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
		return fmt.Errorf("add snapshot_limit column: %w", err)
	}
	return nil
}

func (s *Store) copyForward(tx *sql.Tx, fromID, toID int64, impacted []string) error {
	if len(impacted) == 0 {
		tables := []string{"packages", "symbols", "package_deps", "refs", "call_edges", "tests", "test_links"}
		for _, table := range tables {
			if _, err := tx.Exec(`INSERT INTO `+table+` SELECT ?, `+forwardColumns(table)+` FROM `+table+` WHERE snapshot_id = ?`, append([]any{toID}, fromID)...); err != nil {
				return fmt.Errorf("copy table %s: %w", table, err)
			}
		}
		return nil
	}

	if err := copyTableByPackage(tx, "packages", "import_path", fromID, toID, impacted); err != nil {
		return err
	}
	if err := copyTableByPackage(tx, "symbols", "package_import_path", fromID, toID, impacted); err != nil {
		return err
	}
	if err := copyTableByPackage(tx, "package_deps", "from_package_import_path", fromID, toID, impacted); err != nil {
		return err
	}
	if err := copyTableByPackage(tx, "refs", "from_package_import_path", fromID, toID, impacted); err != nil {
		return err
	}
	if err := copyTableByPackage(tx, "call_edges", "caller_package_import_path", fromID, toID, impacted); err != nil {
		return err
	}
	if err := copyTableByPackage(tx, "tests", "package_import_path", fromID, toID, impacted); err != nil {
		return err
	}
	if err := copyTableByPackage(tx, "test_links", "test_package_import_path", fromID, toID, impacted); err != nil {
		return err
	}
	return nil
}

func countChangedSymbols(tx *sql.Tx, fromID, toID int64) (int, error) {
	if fromID == 0 {
		return countTable(tx, "symbols", toID)
	}

	var changed int
	err := tx.QueryRow(`
		SELECT COUNT(*)
		FROM (
			SELECT s2.qname
			FROM symbols s2
			LEFT JOIN symbols s1 ON s1.snapshot_id = ? AND s1.qname = s2.qname
			WHERE s2.snapshot_id = ?
			  AND (s1.qname IS NULL OR s1.signature != s2.signature OR s1.file_path != s2.file_path OR s1.line != s2.line)
			UNION
			SELECT s1.qname
			FROM symbols s1
			LEFT JOIN symbols s2 ON s2.snapshot_id = ? AND s2.qname = s1.qname
			WHERE s1.snapshot_id = ?
			  AND s2.qname IS NULL
		)
	`, fromID, toID, toID, fromID).Scan(&changed)
	if err != nil {
		return 0, fmt.Errorf("count changed symbols: %w", err)
	}
	return changed, nil
}

func countTable(tx *sql.Tx, table string, snapshotID int64) (int, error) {
	var count int
	err := tx.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE snapshot_id = ?`, snapshotID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count %s: %w", table, err)
	}
	return count, nil
}
