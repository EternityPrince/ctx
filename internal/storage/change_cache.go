package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/vladimirkasterin/ctx/internal/codebase"
)

func (s *Store) LoadChangePlan(snapshotID int64, fingerprint string) (codebase.ChangePlan, bool, error) {
	if snapshotID <= 0 || strings.TrimSpace(fingerprint) == "" {
		return codebase.ChangePlan{}, false, nil
	}

	var payload string
	err := s.db.QueryRow(`
		SELECT plan_json
		FROM change_cache
		WHERE snapshot_id = ? AND scan_fingerprint = ?
		LIMIT 1
	`, snapshotID, fingerprint).Scan(&payload)
	if err != nil {
		if err == sql.ErrNoRows {
			return codebase.ChangePlan{}, false, nil
		}
		return codebase.ChangePlan{}, false, fmt.Errorf("load change cache: %w", err)
	}

	var plan codebase.ChangePlan
	if err := json.Unmarshal([]byte(payload), &plan); err != nil {
		return codebase.ChangePlan{}, false, fmt.Errorf("decode change cache: %w", err)
	}
	plan.CacheHit = true
	plan.Fingerprint = fingerprint
	return plan, true, nil
}

func (s *Store) SaveChangePlan(snapshotID int64, fingerprint string, plan codebase.ChangePlan) error {
	if snapshotID <= 0 || strings.TrimSpace(fingerprint) == "" {
		return nil
	}

	plan.CacheHit = false
	plan.Fingerprint = ""
	payload, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("encode change cache: %w", err)
	}

	if _, err := s.db.Exec(`
		INSERT INTO change_cache (snapshot_id, scan_fingerprint, plan_json, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(snapshot_id, scan_fingerprint)
		DO UPDATE SET plan_json = excluded.plan_json, created_at = excluded.created_at
	`, snapshotID, fingerprint, string(payload), time.Now().UTC().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("save change cache: %w", err)
	}
	return nil
}
