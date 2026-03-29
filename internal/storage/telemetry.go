package storage

import (
	"database/sql"
	"fmt"
	"time"
)

const sqliteSchemaVersion = 7

type SnapshotCommitTelemetry struct {
	ScannedFiles     int
	ScanDuration     time.Duration
	AnalyzeDuration  time.Duration
	PlanMode         string
	DirectPackages   int
	ExpandedPackages int
	ReusedPackages   int
	ReusePercent     int
	PlanCacheHit     bool
}

func ExpectedSchemaVersion() int {
	return sqliteSchemaVersion
}

func (s *Store) SchemaVersion() (int, error) {
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return 0, fmt.Errorf("read sqlite schema version: %w", err)
	}
	return version, nil
}

func (s *Store) QuickCheck() (string, error) {
	var result string
	if err := s.db.QueryRow(`PRAGMA quick_check(1)`).Scan(&result); err != nil {
		return "", fmt.Errorf("run sqlite quick_check: %w", err)
	}
	return result, nil
}

func durationMillis(value time.Duration) int {
	if value <= 0 {
		return 0
	}
	return int(value / time.Millisecond)
}

func nullableSnapshotID(value int64) sql.NullInt64 {
	if value <= 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: value, Valid: true}
}
