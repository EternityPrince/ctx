package storage

import (
	"fmt"
	"os"
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
	UpdatedAt         time.Time
	DBPath            string
	SizeBytes         int64
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
		SELECT project_id, root_path, module_path, go_version, updated_at, current_snapshot_id
		FROM project_meta
		LIMIT 1
	`).Scan(
		&record.ID,
		&record.RootPath,
		&record.ModulePath,
		&record.GoVersion,
		&updatedAt,
		&record.CurrentSnapshotID,
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
	records, err := ListProjects()
	if err != nil {
		return err
	}
	for _, record := range records {
		if record.ID == projectArg || record.RootPath == projectArg || strings.HasPrefix(record.ID, projectArg) {
			dir, err := project.ProjectDir(record.ID)
			if err != nil {
				return err
			}
			return os.RemoveAll(dir)
		}
	}
	return fmt.Errorf("project %q not found", projectArg)
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
