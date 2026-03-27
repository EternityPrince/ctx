package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/vladimirkasterin/ctx/internal/project"
)

type ProjectRecord struct {
	ID                string
	RootPath          string
	ModulePath        string
	GoVersion         string
	CurrentSnapshotID int64
	SnapshotCount     int
	SnapshotLimit     int
	UpdatedAt         time.Time
	DBPath            string
	SizeBytes         int64
}

type ProjectResetStats struct {
	ProjectsRemoved  int
	SnapshotsRemoved int
	BytesFreed       int64
}

func ResolveProject(projectArg string) (ProjectRecord, error) {
	records, err := ListProjects()
	if err != nil {
		return ProjectRecord{}, err
	}

	var matches []ProjectRecord
	for _, record := range records {
		if record.ID == projectArg || record.RootPath == projectArg || record.ModulePath == projectArg {
			return record, nil
		}
		if strings.HasPrefix(record.ID, projectArg) || strings.HasPrefix(record.RootPath, projectArg) || strings.HasPrefix(record.ModulePath, projectArg) {
			matches = append(matches, record)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return ProjectRecord{}, fmt.Errorf("project %q is ambiguous", projectArg)
	}
	return ProjectRecord{}, fmt.Errorf("project %q not found", projectArg)
}

func loadProjectRecord(dbPath string) (ProjectRecord, error) {
	store, err := Open(dbPath)
	if err != nil {
		return ProjectRecord{}, err
	}
	defer store.Close()

	record := ProjectRecord{DBPath: dbPath}
	var updatedAt string
	err = store.db.QueryRow(`
		SELECT project_id, root_path, module_path, go_version, updated_at, current_snapshot_id, snapshot_limit
		FROM project_meta
		LIMIT 1
	`).Scan(
		&record.ID,
		&record.RootPath,
		&record.ModulePath,
		&record.GoVersion,
		&updatedAt,
		&record.CurrentSnapshotID,
		&record.SnapshotLimit,
	)
	if err != nil {
		return ProjectRecord{}, err
	}
	record.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return ProjectRecord{}, fmt.Errorf("parse project updated time: %w", err)
	}

	stat, err := os.Stat(dbPath)
	if err == nil {
		record.SizeBytes = stat.Size()
	}
	_ = store.db.QueryRow(`SELECT COUNT(*) FROM snapshots`).Scan(&record.SnapshotCount)
	return record, nil
}

func ListProjects() ([]ProjectRecord, error) {
	dbPaths, err := project.ListDBPaths()
	if err != nil {
		return nil, err
	}

	records := make([]ProjectRecord, 0, len(dbPaths))
	for _, dbPath := range dbPaths {
		record, err := loadProjectRecord(dbPath)
		if err != nil {
			continue
		}
		records = append(records, record)
	}

	sort.Slice(records, func(i, j int) bool {
		return records[i].UpdatedAt.After(records[j].UpdatedAt)
	})
	return records, nil
}

func RemoveProject(projectArg string) error {
	record, err := ResolveProject(projectArg)
	if err != nil {
		return err
	}

	dir, err := project.ProjectDir(record.ID)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

func PruneProjects() (int, error) {
	records, err := ListProjects()
	if err != nil {
		return 0, err
	}

	removed := 0
	for _, record := range records {
		if _, err := os.Stat(record.RootPath); err == nil {
			continue
		}
		dir, err := project.ProjectDir(record.ID)
		if err != nil {
			return removed, err
		}
		if err := os.RemoveAll(dir); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

func DeleteAllProjects() (ProjectResetStats, error) {
	records, err := ListProjects()
	if err != nil {
		return ProjectResetStats{}, err
	}

	projectsRoot, err := project.ProjectsRoot()
	if err != nil {
		return ProjectResetStats{}, err
	}
	entries, err := os.ReadDir(projectsRoot)
	if err != nil {
		return ProjectResetStats{}, fmt.Errorf("read projects root: %w", err)
	}

	known := make(map[string]ProjectRecord, len(records))
	for _, record := range records {
		known[record.ID] = record
	}

	stats := ProjectResetStats{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirPath := filepath.Join(projectsRoot, entry.Name())
		size, err := dirSize(dirPath)
		if err != nil {
			return stats, err
		}
		stats.BytesFreed += size
		stats.ProjectsRemoved++
		if record, ok := known[entry.Name()]; ok {
			stats.SnapshotsRemoved += record.SnapshotCount
		}
		if err := os.RemoveAll(dirPath); err != nil {
			return stats, err
		}
	}
	return stats, nil
}

func dirSize(root string) (int64, error) {
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	if !info.IsDir() {
		return info.Size(), nil
	}

	var total int64
	err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info == nil || info.IsDir() {
			return nil
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		return 0, err
	}
	return total, nil
}
