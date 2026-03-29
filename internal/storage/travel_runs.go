package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type TravelRunSummary struct {
	ID               int64
	CreatedAt        time.Time
	Recipe           string
	Launcher         string
	RunArgs          []string
	EntryFile        string
	EntrySymbolQName string
	Depth            int
	Limit            int
	RunStatus        string
	RunExitCode      int
	RunWallMs        int
	RunPeakRSSBytes  int64
}

type TravelRunRecord struct {
	TravelRunSummary
	Explain        bool
	TimeoutMs      int
	NoRun          bool
	RunAttempted   bool
	RunTimedOut    bool
	RunCPUUserMs   int
	RunCPUSystemMs int
	HumanOutput    string
	AIOutput       string
}

func (s *Store) CreateTravelRun(record TravelRunRecord) (TravelRunSummary, error) {
	runArgsJSON, err := json.Marshal(record.RunArgs)
	if err != nil {
		return TravelRunSummary{}, fmt.Errorf("marshal travel run args: %w", err)
	}
	createdAt := time.Now().UTC()
	result, err := s.db.Exec(`
		INSERT INTO travel_runs (
			created_at,
			recipe,
			launcher,
			run_args_json,
			entry_file,
			entry_symbol_qname,
			depth,
			limit_count,
			explain,
			timeout_ms,
			no_run,
			run_attempted,
			run_timed_out,
			run_status,
			run_exit_code,
			run_wall_ms,
			run_cpu_user_ms,
			run_cpu_system_ms,
			run_peak_rss_bytes
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		createdAt.Format(time.RFC3339),
		record.Recipe,
		record.Launcher,
		string(runArgsJSON),
		record.EntryFile,
		record.EntrySymbolQName,
		record.Depth,
		record.Limit,
		boolInt(record.Explain),
		record.TimeoutMs,
		boolInt(record.NoRun),
		boolInt(record.RunAttempted),
		boolInt(record.RunTimedOut),
		record.RunStatus,
		record.RunExitCode,
		record.RunWallMs,
		record.RunCPUUserMs,
		record.RunCPUSystemMs,
		record.RunPeakRSSBytes,
	)
	if err != nil {
		return TravelRunSummary{}, fmt.Errorf("insert travel run: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return TravelRunSummary{}, fmt.Errorf("resolve travel run id: %w", err)
	}
	record.ID = id
	record.CreatedAt = createdAt
	return record.TravelRunSummary, nil
}

func (s *Store) UpdateTravelRunOutputs(id int64, humanOutput, aiOutput string) error {
	if id <= 0 {
		return fmt.Errorf("invalid travel run id %d", id)
	}
	if _, err := s.db.Exec(`UPDATE travel_runs SET human_output = ?, ai_output = ? WHERE id = ?`, humanOutput, aiOutput, id); err != nil {
		return fmt.Errorf("update travel run outputs: %w", err)
	}
	return nil
}

func (s *Store) ListTravelRuns() ([]TravelRunSummary, error) {
	rows, err := s.db.Query(`
		SELECT
			id,
			created_at,
			recipe,
			launcher,
			run_args_json,
			entry_file,
			entry_symbol_qname,
			depth,
			limit_count,
			run_status,
			run_exit_code,
			run_wall_ms,
			run_peak_rss_bytes
		FROM travel_runs
		ORDER BY id DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query travel runs: %w", err)
	}
	defer rows.Close()

	var items []TravelRunSummary
	for rows.Next() {
		item, err := scanTravelRunSummary(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate travel runs: %w", err)
	}
	return items, nil
}

func (s *Store) TravelRunByID(id int64) (TravelRunRecord, error) {
	if id <= 0 {
		return TravelRunRecord{}, fmt.Errorf("invalid travel run id %d", id)
	}
	row := s.db.QueryRow(`
		SELECT
			id,
			created_at,
			recipe,
			launcher,
			run_args_json,
			entry_file,
			entry_symbol_qname,
			depth,
			limit_count,
			explain,
			timeout_ms,
			no_run,
			run_attempted,
			run_timed_out,
			run_status,
			run_exit_code,
			run_wall_ms,
			run_cpu_user_ms,
			run_cpu_system_ms,
			run_peak_rss_bytes,
			human_output,
			ai_output
		FROM travel_runs
		WHERE id = ?
	`, id)

	var record TravelRunRecord
	var createdAt string
	var runArgsJSON string
	err := row.Scan(
		&record.ID,
		&createdAt,
		&record.Recipe,
		&record.Launcher,
		&runArgsJSON,
		&record.EntryFile,
		&record.EntrySymbolQName,
		&record.Depth,
		&record.Limit,
		newBoolScanDest(&record.Explain),
		&record.TimeoutMs,
		newBoolScanDest(&record.NoRun),
		newBoolScanDest(&record.RunAttempted),
		newBoolScanDest(&record.RunTimedOut),
		&record.RunStatus,
		&record.RunExitCode,
		&record.RunWallMs,
		&record.RunCPUUserMs,
		&record.RunCPUSystemMs,
		&record.RunPeakRSSBytes,
		&record.HumanOutput,
		&record.AIOutput,
	)
	if err == sql.ErrNoRows {
		return TravelRunRecord{}, sql.ErrNoRows
	}
	if err != nil {
		return TravelRunRecord{}, fmt.Errorf("load travel run %d: %w", id, err)
	}
	parsedCreatedAt, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return TravelRunRecord{}, fmt.Errorf("parse travel run time: %w", err)
	}
	record.CreatedAt = parsedCreatedAt
	if err := json.Unmarshal([]byte(runArgsJSON), &record.RunArgs); err != nil {
		return TravelRunRecord{}, fmt.Errorf("parse travel run args: %w", err)
	}
	return record, nil
}

func scanTravelRunSummary(scanner interface {
	Scan(dest ...any) error
}) (TravelRunSummary, error) {
	var item TravelRunSummary
	var createdAt string
	var runArgsJSON string
	if err := scanner.Scan(
		&item.ID,
		&createdAt,
		&item.Recipe,
		&item.Launcher,
		&runArgsJSON,
		&item.EntryFile,
		&item.EntrySymbolQName,
		&item.Depth,
		&item.Limit,
		&item.RunStatus,
		&item.RunExitCode,
		&item.RunWallMs,
		&item.RunPeakRSSBytes,
	); err != nil {
		return TravelRunSummary{}, fmt.Errorf("scan travel run summary: %w", err)
	}
	parsedCreatedAt, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return TravelRunSummary{}, fmt.Errorf("parse travel run summary time: %w", err)
	}
	item.CreatedAt = parsedCreatedAt
	if err := json.Unmarshal([]byte(runArgsJSON), &item.RunArgs); err != nil {
		return TravelRunSummary{}, fmt.Errorf("parse travel run summary args: %w", err)
	}
	return item, nil
}
