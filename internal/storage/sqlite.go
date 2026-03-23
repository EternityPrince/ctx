package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/vladimirkasterin/ctx/internal/project"
)

type Store struct {
	db     *sql.DB
	dbPath string
}

func Open(dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=on&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open sqlite db: %w", err)
	}

	store := &Store{db: db, dbPath: dbPath}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) EnsureProject(info project.Info) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var exists int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM project_meta`).Scan(&exists); err != nil {
		return fmt.Errorf("check project meta: %w", err)
	}
	if exists == 0 {
		_, err := s.db.Exec(`
			INSERT INTO project_meta (
				project_id, root_path, module_path, go_version, created_at, updated_at, current_snapshot_id
			) VALUES (?, ?, ?, ?, ?, ?, 0)
		`, info.ID, info.Root, info.ModulePath, info.GoVersion, now, now)
		if err != nil {
			return fmt.Errorf("insert project meta: %w", err)
		}
		return nil
	}

	_, err := s.db.Exec(`
		UPDATE project_meta
		SET project_id = ?, root_path = ?, module_path = ?, go_version = ?, updated_at = ?
	`, info.ID, info.Root, info.ModulePath, info.GoVersion, now)
	if err != nil {
		return fmt.Errorf("update project meta: %w", err)
	}
	return nil
}

func (s *Store) ReverseDependencies(snapshotID int64, packages []string) ([]string, error) {
	if snapshotID == 0 || len(packages) == 0 {
		return nil, nil
	}

	args := make([]any, 0, len(packages)+1)
	args = append(args, snapshotID)
	placeholders := make([]string, 0, len(packages))
	for _, pkg := range packages {
		args = append(args, pkg)
		placeholders = append(placeholders, "?")
	}

	query := `
		SELECT DISTINCT from_package_import_path
		FROM package_deps
		WHERE snapshot_id = ?
		AND to_package_import_path IN (` + strings.Join(placeholders, ",") + `)
	`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query reverse deps: %w", err)
	}
	defer rows.Close()

	var reverse []string
	for rows.Next() {
		var pkg string
		if err := rows.Scan(&pkg); err != nil {
			return nil, fmt.Errorf("scan reverse dep: %w", err)
		}
		reverse = append(reverse, pkg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reverse deps: %w", err)
	}
	sort.Strings(reverse)
	return reverse, nil
}
