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

	"github.com/vladimirkasterin/ctx/internal/codebase"
	"github.com/vladimirkasterin/ctx/internal/project"
)

type Store struct {
	db     *sql.DB
	dbPath string
}

type SymbolMatch struct {
	SymbolKey         string
	QName             string
	PackageImportPath string
	FilePath          string
	Name              string
	Kind              string
	Receiver          string
	Signature         string
	Doc               string
	Line              int
	Column            int
}

type RelatedSymbolView struct {
	Symbol      SymbolMatch
	UseFilePath string
	UseLine     int
	UseColumn   int
	Relation    string
}

type RefView struct {
	Symbol      SymbolMatch
	UseFilePath string
	UseLine     int
	UseColumn   int
	Kind        string
}

type TestView struct {
	TestKey           string
	PackageImportPath string
	Name              string
	FilePath          string
	Kind              string
	Line              int
	LinkKind          string
	Confidence        string
}

type PackageSummary struct {
	ImportPath  string
	Name        string
	DirPath     string
	FileCount   int
	SymbolCount int
	TestCount   int
	LocalDeps   []string
	ReverseDeps []string
}

type ImpactNode struct {
	Symbol SymbolMatch
	Depth  int
}

type ImpactView struct {
	Target            SymbolMatch
	Package           PackageSummary
	DirectCallers     []RelatedSymbolView
	TransitiveCallers []ImpactNode
	CallerPackages    []string
	Tests             []TestView
}

type SymbolView struct {
	Symbol        SymbolMatch
	Package       PackageSummary
	Callers       []RelatedSymbolView
	Callees       []RelatedSymbolView
	ReferencesIn  []RefView
	ReferencesOut []RefView
	Tests         []TestView
	Siblings      []SymbolMatch
}

type ChangedSymbol struct {
	QName         string
	FromSignature string
	ToSignature   string
}

type DiffView struct {
	FromSnapshotID int64
	ToSnapshotID   int64
	AddedFiles     []string
	ChangedFiles   []string
	DeletedFiles   []string
	AddedSymbols   []SymbolMatch
	RemovedSymbols []SymbolMatch
	ChangedSymbols []ChangedSymbol
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

func (s *Store) FindSymbols(query string) ([]SymbolMatch, error) {
	current, ok, err := s.CurrentSnapshot()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}

	pattern := "%" + query
	rows, err := s.db.Query(`
		SELECT symbol_key, qname, package_import_path, file_path, name, kind, receiver, signature, doc, line, col
		FROM symbols
		WHERE snapshot_id = ?
		  AND (symbol_key = ? OR qname = ? OR qname LIKE ? OR name = ?)
	`, current.ID, query, query, pattern, query)
	if err != nil {
		return nil, fmt.Errorf("query symbols: %w", err)
	}
	defer rows.Close()

	var symbols []SymbolMatch
	for rows.Next() {
		var symbol SymbolMatch
		if err := rows.Scan(
			&symbol.SymbolKey,
			&symbol.QName,
			&symbol.PackageImportPath,
			&symbol.FilePath,
			&symbol.Name,
			&symbol.Kind,
			&symbol.Receiver,
			&symbol.Signature,
			&symbol.Doc,
			&symbol.Line,
			&symbol.Column,
		); err != nil {
			return nil, fmt.Errorf("scan symbol match: %w", err)
		}
		symbols = append(symbols, symbol)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate symbols: %w", err)
	}

	sort.Slice(symbols, func(i, j int) bool {
		return symbolRank(symbols[i], query) < symbolRank(symbols[j], query)
	})
	return symbols, nil
}

func (s *Store) LoadSymbolView(symbolKey string) (SymbolView, error) {
	current, ok, err := s.CurrentSnapshot()
	if err != nil {
		return SymbolView{}, err
	}
	if !ok {
		return SymbolView{}, fmt.Errorf("no snapshots available")
	}

	view := SymbolView{}
	err = s.db.QueryRow(`
		SELECT symbol_key, qname, package_import_path, file_path, name, kind, receiver, signature, doc, line, col
		FROM symbols
		WHERE snapshot_id = ? AND symbol_key = ?
	`, current.ID, symbolKey).Scan(symbolMatchScanDest(&view.Symbol)...)
	if err != nil {
		return SymbolView{}, fmt.Errorf("load symbol: %w", err)
	}

	if view.Package, err = s.loadPackageSummary(current.ID, view.Symbol.PackageImportPath); err != nil {
		return SymbolView{}, err
	}
	if view.Callers, err = s.loadCallers(current.ID, symbolKey); err != nil {
		return SymbolView{}, err
	}
	if view.Callees, err = s.loadCallees(current.ID, symbolKey); err != nil {
		return SymbolView{}, err
	}
	if view.ReferencesIn, err = s.loadReferences(current.ID, symbolKey, true); err != nil {
		return SymbolView{}, err
	}
	if view.ReferencesOut, err = s.loadReferences(current.ID, symbolKey, false); err != nil {
		return SymbolView{}, err
	}
	if view.Tests, err = s.loadTests(current.ID, symbolKey); err != nil {
		return SymbolView{}, err
	}
	if view.Siblings, err = s.loadSiblings(current.ID, view.Symbol); err != nil {
		return SymbolView{}, err
	}

	return view, nil
}

func (s *Store) LoadFileSymbols(filePath string) ([]SymbolMatch, error) {
	current, ok, err := s.CurrentSnapshot()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("no snapshots available")
	}

	rows, err := s.db.Query(`
		SELECT symbol_key, qname, package_import_path, file_path, name, kind, receiver, signature, doc, line, col
		FROM symbols
		WHERE snapshot_id = ? AND file_path = ?
		ORDER BY line, col
	`, current.ID, filePath)
	if err != nil {
		return nil, fmt.Errorf("query file symbols: %w", err)
	}
	defer rows.Close()

	var symbols []SymbolMatch
	for rows.Next() {
		var symbol SymbolMatch
		if err := rows.Scan(
			&symbol.SymbolKey,
			&symbol.QName,
			&symbol.PackageImportPath,
			&symbol.FilePath,
			&symbol.Name,
			&symbol.Kind,
			&symbol.Receiver,
			&symbol.Signature,
			&symbol.Doc,
			&symbol.Line,
			&symbol.Column,
		); err != nil {
			return nil, fmt.Errorf("scan file symbol: %w", err)
		}
		symbols = append(symbols, symbol)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate file symbols: %w", err)
	}
	return symbols, nil
}

func (s *Store) LoadImpactView(symbolKey string, depth int) (ImpactView, error) {
	current, ok, err := s.CurrentSnapshot()
	if err != nil {
		return ImpactView{}, err
	}
	if !ok {
		return ImpactView{}, fmt.Errorf("no snapshots available")
	}
	if depth < 1 {
		depth = 2
	}

	var view ImpactView
	err = s.db.QueryRow(`
		SELECT symbol_key, qname, package_import_path, file_path, name, kind, receiver, signature, doc, line, col
		FROM symbols
		WHERE snapshot_id = ? AND symbol_key = ?
	`, current.ID, symbolKey).Scan(symbolMatchScanDest(&view.Target)...)
	if err != nil {
		return ImpactView{}, fmt.Errorf("load impact target: %w", err)
	}

	if view.Package, err = s.loadPackageSummary(current.ID, view.Target.PackageImportPath); err != nil {
		return ImpactView{}, err
	}
	if view.DirectCallers, err = s.loadCallers(current.ID, symbolKey); err != nil {
		return ImpactView{}, err
	}
	if view.TransitiveCallers, err = s.loadTransitiveCallers(current.ID, symbolKey, depth); err != nil {
		return ImpactView{}, err
	}
	if view.CallerPackages, err = s.loadImpactCallerPackages(current.ID, symbolKey, depth); err != nil {
		return ImpactView{}, err
	}
	if view.Tests, err = s.loadTests(current.ID, symbolKey); err != nil {
		return ImpactView{}, err
	}

	return view, nil
}

func (s *Store) Diff(fromID, toID int64) (DiffView, error) {
	if toID == 0 {
		current, ok, err := s.CurrentSnapshot()
		if err != nil {
			return DiffView{}, err
		}
		if !ok {
			return DiffView{}, fmt.Errorf("no snapshots available")
		}
		toID = current.ID
		if fromID == 0 && current.ParentID.Valid {
			fromID = current.ParentID.Int64
		}
		if fromID == 0 {
			fromID, err = s.previousSnapshotID(current.ID)
			if err != nil {
				return DiffView{}, err
			}
		}
	}
	if fromID == 0 {
		return DiffView{}, fmt.Errorf("from snapshot is required")
	}

	diff := DiffView{
		FromSnapshotID: fromID,
		ToSnapshotID:   toID,
	}

	var err error
	if diff.AddedFiles, err = s.diffFiles(fromID, toID, "added"); err != nil {
		return DiffView{}, err
	}
	if diff.ChangedFiles, err = s.diffFiles(fromID, toID, "changed"); err != nil {
		return DiffView{}, err
	}
	if diff.DeletedFiles, err = s.diffFiles(fromID, toID, "deleted"); err != nil {
		return DiffView{}, err
	}
	if diff.AddedSymbols, err = s.diffSymbols(fromID, toID, "added"); err != nil {
		return DiffView{}, err
	}
	if diff.RemovedSymbols, err = s.diffSymbols(fromID, toID, "removed"); err != nil {
		return DiffView{}, err
	}
	if diff.ChangedSymbols, err = s.changedSymbols(fromID, toID); err != nil {
		return DiffView{}, err
	}

	return diff, nil
}

func (s *Store) init() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS project_meta (
			project_id TEXT NOT NULL,
			root_path TEXT NOT NULL,
			module_path TEXT NOT NULL,
			go_version TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			current_snapshot_id INTEGER NOT NULL DEFAULT 0
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

func (s *Store) loadCallers(snapshotID int64, symbolKey string) ([]RelatedSymbolView, error) {
	rows, err := s.db.Query(`
		SELECT s.symbol_key, s.qname, s.package_import_path, s.file_path, s.name, s.kind, s.receiver, s.signature, s.doc, s.line, s.col,
		       c.file_path, c.line, c.col, c.dispatch
		FROM call_edges c
		JOIN symbols s ON s.snapshot_id = c.snapshot_id AND s.symbol_key = c.caller_symbol_key
		WHERE c.snapshot_id = ? AND c.callee_symbol_key = ?
		ORDER BY s.qname
	`, snapshotID, symbolKey)
	if err != nil {
		return nil, fmt.Errorf("query callers: %w", err)
	}
	defer rows.Close()
	return scanRelatedSymbols(rows)
}

func (s *Store) loadCallees(snapshotID int64, symbolKey string) ([]RelatedSymbolView, error) {
	rows, err := s.db.Query(`
		SELECT s.symbol_key, s.qname, s.package_import_path, s.file_path, s.name, s.kind, s.receiver, s.signature, s.doc, s.line, s.col,
		       c.file_path, c.line, c.col, c.dispatch
		FROM call_edges c
		JOIN symbols s ON s.snapshot_id = c.snapshot_id AND s.symbol_key = c.callee_symbol_key
		WHERE c.snapshot_id = ? AND c.caller_symbol_key = ?
		ORDER BY s.qname
	`, snapshotID, symbolKey)
	if err != nil {
		return nil, fmt.Errorf("query callees: %w", err)
	}
	defer rows.Close()
	return scanRelatedSymbols(rows)
}

func (s *Store) loadReferences(snapshotID int64, symbolKey string, inbound bool) ([]RefView, error) {
	query := `
		SELECT target.symbol_key, target.qname, target.package_import_path, target.file_path, target.name, target.kind,
		       target.receiver, target.signature, target.doc, target.line, target.col,
		       r.file_path, r.line, r.col, r.kind
		FROM refs r
		JOIN symbols target ON target.snapshot_id = r.snapshot_id AND target.symbol_key = `
	if inbound {
		query += `r.from_symbol_key`
	} else {
		query += `r.to_symbol_key`
	}
	query += `
		WHERE r.snapshot_id = ? AND `
	if inbound {
		query += `r.to_symbol_key = ? AND r.from_symbol_key != ''`
	} else {
		query += `r.from_symbol_key = ?`
	}
	query += ` ORDER BY target.qname`

	rows, err := s.db.Query(query, snapshotID, symbolKey)
	if err != nil {
		return nil, fmt.Errorf("query references: %w", err)
	}
	defer rows.Close()

	var refs []RefView
	for rows.Next() {
		var ref RefView
		if err := rows.Scan(
			append(symbolMatchScanDest(&ref.Symbol), &ref.UseFilePath, &ref.UseLine, &ref.UseColumn, &ref.Kind)...,
		); err != nil {
			return nil, fmt.Errorf("scan ref: %w", err)
		}
		refs = append(refs, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate refs: %w", err)
	}
	return refs, nil
}

func (s *Store) loadTests(snapshotID int64, symbolKey string) ([]TestView, error) {
	rows, err := s.db.Query(`
		SELECT t.test_key, t.package_import_path, t.name, t.file_path, t.kind, t.line, tl.link_kind, tl.confidence
		FROM test_links tl
		JOIN tests t ON t.snapshot_id = tl.snapshot_id AND t.test_key = tl.test_key
		WHERE tl.snapshot_id = ? AND tl.symbol_key = ?
		ORDER BY t.name
	`, snapshotID, symbolKey)
	if err != nil {
		return nil, fmt.Errorf("query tests: %w", err)
	}
	defer rows.Close()

	var tests []TestView
	for rows.Next() {
		var test TestView
		if err := rows.Scan(&test.TestKey, &test.PackageImportPath, &test.Name, &test.FilePath, &test.Kind, &test.Line, &test.LinkKind, &test.Confidence); err != nil {
			return nil, fmt.Errorf("scan test view: %w", err)
		}
		tests = append(tests, test)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tests: %w", err)
	}
	return tests, nil
}

func (s *Store) loadSiblings(snapshotID int64, symbol SymbolMatch) ([]SymbolMatch, error) {
	rows, err := s.db.Query(`
		SELECT symbol_key, qname, package_import_path, file_path, name, kind, receiver, signature, doc, line, col
		FROM symbols
		WHERE snapshot_id = ?
		  AND package_import_path = ?
		  AND symbol_key != ?
		  AND (file_path = ? OR receiver = ?)
		ORDER BY file_path, line
		LIMIT 8
	`, snapshotID, symbol.PackageImportPath, symbol.SymbolKey, symbol.FilePath, symbol.Receiver)
	if err != nil {
		return nil, fmt.Errorf("query sibling symbols: %w", err)
	}
	defer rows.Close()

	var siblings []SymbolMatch
	for rows.Next() {
		var sibling SymbolMatch
		if err := rows.Scan(
			&sibling.SymbolKey,
			&sibling.QName,
			&sibling.PackageImportPath,
			&sibling.FilePath,
			&sibling.Name,
			&sibling.Kind,
			&sibling.Receiver,
			&sibling.Signature,
			&sibling.Doc,
			&sibling.Line,
			&sibling.Column,
		); err != nil {
			return nil, fmt.Errorf("scan sibling symbol: %w", err)
		}
		siblings = append(siblings, sibling)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sibling symbols: %w", err)
	}
	return siblings, nil
}

func (s *Store) loadPackageSummary(snapshotID int64, importPath string) (PackageSummary, error) {
	summary := PackageSummary{ImportPath: importPath}
	err := s.db.QueryRow(`
		SELECT p.import_path, p.name, p.dir_path, p.file_count,
		       (SELECT COUNT(*) FROM symbols s WHERE s.snapshot_id = p.snapshot_id AND s.package_import_path = p.import_path),
		       (SELECT COUNT(*) FROM tests t WHERE t.snapshot_id = p.snapshot_id AND t.package_import_path = p.import_path)
		FROM packages p
		WHERE p.snapshot_id = ? AND p.import_path = ?
	`, snapshotID, importPath).Scan(
		&summary.ImportPath,
		&summary.Name,
		&summary.DirPath,
		&summary.FileCount,
		&summary.SymbolCount,
		&summary.TestCount,
	)
	if err != nil {
		return PackageSummary{}, fmt.Errorf("load package summary: %w", err)
	}

	localDeps, err := loadStringRows(s.db.Query(`
		SELECT DISTINCT to_package_import_path
		FROM package_deps
		WHERE snapshot_id = ? AND from_package_import_path = ? AND is_local = 1
		ORDER BY to_package_import_path
	`, snapshotID, importPath))
	if err != nil {
		return PackageSummary{}, err
	}
	reverseDeps, err := loadStringRows(s.db.Query(`
		SELECT DISTINCT from_package_import_path
		FROM package_deps
		WHERE snapshot_id = ? AND to_package_import_path = ? AND is_local = 1
		ORDER BY from_package_import_path
	`, snapshotID, importPath))
	if err != nil {
		return PackageSummary{}, err
	}

	summary.LocalDeps = localDeps
	summary.ReverseDeps = reverseDeps
	return summary, nil
}

func (s *Store) loadTransitiveCallers(snapshotID int64, symbolKey string, depth int) ([]ImpactNode, error) {
	rows, err := s.db.Query(`
		WITH RECURSIVE caller_walk(symbol_key, depth) AS (
			SELECT caller_symbol_key, 1
			FROM call_edges
			WHERE snapshot_id = ? AND callee_symbol_key = ?
			UNION
			SELECT c.caller_symbol_key, caller_walk.depth + 1
			FROM call_edges c
			JOIN caller_walk ON c.snapshot_id = ? AND c.callee_symbol_key = caller_walk.symbol_key
			WHERE caller_walk.depth < ?
		),
		caller_min AS (
			SELECT symbol_key, MIN(depth) AS depth
			FROM caller_walk
			GROUP BY symbol_key
		)
		SELECT s.symbol_key, s.qname, s.package_import_path, s.file_path, s.name, s.kind, s.receiver, s.signature, s.doc, s.line, s.col,
		       cm.depth
		FROM caller_min cm
		JOIN symbols s ON s.snapshot_id = ? AND s.symbol_key = cm.symbol_key
		ORDER BY cm.depth, s.qname
	`, snapshotID, symbolKey, snapshotID, depth, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("query transitive callers: %w", err)
	}
	defer rows.Close()

	var nodes []ImpactNode
	for rows.Next() {
		var node ImpactNode
		if err := rows.Scan(append(symbolMatchScanDest(&node.Symbol), &node.Depth)...); err != nil {
			return nil, fmt.Errorf("scan transitive caller: %w", err)
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate transitive callers: %w", err)
	}
	return nodes, nil
}

func (s *Store) loadImpactCallerPackages(snapshotID int64, symbolKey string, depth int) ([]string, error) {
	rows, err := s.db.Query(`
		WITH RECURSIVE caller_walk(symbol_key, depth) AS (
			SELECT caller_symbol_key, 1
			FROM call_edges
			WHERE snapshot_id = ? AND callee_symbol_key = ?
			UNION
			SELECT c.caller_symbol_key, caller_walk.depth + 1
			FROM call_edges c
			JOIN caller_walk ON c.snapshot_id = ? AND c.callee_symbol_key = caller_walk.symbol_key
			WHERE caller_walk.depth < ?
		)
		SELECT DISTINCT s.package_import_path
		FROM caller_walk cw
		JOIN symbols s ON s.snapshot_id = ? AND s.symbol_key = cw.symbol_key
		ORDER BY s.package_import_path
	`, snapshotID, symbolKey, snapshotID, depth, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("query impact caller packages: %w", err)
	}
	defer rows.Close()

	var packages []string
	for rows.Next() {
		var pkg string
		if err := rows.Scan(&pkg); err != nil {
			return nil, fmt.Errorf("scan impact caller package: %w", err)
		}
		packages = append(packages, pkg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate impact caller packages: %w", err)
	}
	return packages, nil
}

func (s *Store) diffFiles(fromID, toID int64, mode string) ([]string, error) {
	var query string
	switch mode {
	case "added":
		query = `
			SELECT f2.rel_path
			FROM files f2
			LEFT JOIN files f1 ON f1.snapshot_id = ? AND f1.rel_path = f2.rel_path
			WHERE f2.snapshot_id = ? AND f1.rel_path IS NULL
			ORDER BY f2.rel_path
		`
	case "deleted":
		query = `
			SELECT f1.rel_path
			FROM files f1
			LEFT JOIN files f2 ON f2.snapshot_id = ? AND f2.rel_path = f1.rel_path
			WHERE f1.snapshot_id = ? AND f2.rel_path IS NULL
			ORDER BY f1.rel_path
		`
	case "changed":
		query = `
			SELECT f2.rel_path
			FROM files f2
			JOIN files f1 ON f1.snapshot_id = ? AND f1.rel_path = f2.rel_path
			WHERE f2.snapshot_id = ? AND f1.content_hash != f2.content_hash
			ORDER BY f2.rel_path
		`
	default:
		return nil, fmt.Errorf("unsupported diff mode %q", mode)
	}

	rows, err := s.db.Query(query, fromID, toID)
	if err != nil {
		return nil, fmt.Errorf("query file diff: %w", err)
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var relPath string
		if err := rows.Scan(&relPath); err != nil {
			return nil, fmt.Errorf("scan file diff: %w", err)
		}
		paths = append(paths, relPath)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate file diff: %w", err)
	}
	return paths, nil
}

func (s *Store) diffSymbols(fromID, toID int64, mode string) ([]SymbolMatch, error) {
	var query string
	switch mode {
	case "added":
		query = `
			SELECT s2.symbol_key, s2.qname, s2.package_import_path, s2.file_path, s2.name, s2.kind, s2.receiver, s2.signature, s2.doc, s2.line, s2.col
			FROM symbols s2
			LEFT JOIN symbols s1 ON s1.snapshot_id = ? AND s1.qname = s2.qname
			WHERE s2.snapshot_id = ? AND s1.qname IS NULL
			ORDER BY s2.qname
		`
	case "removed":
		query = `
			SELECT s1.symbol_key, s1.qname, s1.package_import_path, s1.file_path, s1.name, s1.kind, s1.receiver, s1.signature, s1.doc, s1.line, s1.col
			FROM symbols s1
			LEFT JOIN symbols s2 ON s2.snapshot_id = ? AND s2.qname = s1.qname
			WHERE s1.snapshot_id = ? AND s2.qname IS NULL
			ORDER BY s1.qname
		`
	default:
		return nil, fmt.Errorf("unsupported symbol diff mode %q", mode)
	}

	rows, err := s.db.Query(query, fromID, toID)
	if err != nil {
		return nil, fmt.Errorf("query symbol diff: %w", err)
	}
	defer rows.Close()

	var symbols []SymbolMatch
	for rows.Next() {
		var symbol SymbolMatch
		if err := rows.Scan(
			&symbol.SymbolKey,
			&symbol.QName,
			&symbol.PackageImportPath,
			&symbol.FilePath,
			&symbol.Name,
			&symbol.Kind,
			&symbol.Receiver,
			&symbol.Signature,
			&symbol.Doc,
			&symbol.Line,
			&symbol.Column,
		); err != nil {
			return nil, fmt.Errorf("scan symbol diff: %w", err)
		}
		symbols = append(symbols, symbol)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate symbol diff: %w", err)
	}
	return symbols, nil
}

func (s *Store) changedSymbols(fromID, toID int64) ([]ChangedSymbol, error) {
	rows, err := s.db.Query(`
		SELECT s1.qname, s1.signature, s2.signature
		FROM symbols s1
		JOIN symbols s2 ON s2.snapshot_id = ? AND s2.qname = s1.qname
		WHERE s1.snapshot_id = ?
		  AND (s1.signature != s2.signature OR s1.file_path != s2.file_path OR s1.line != s2.line)
		ORDER BY s1.qname
	`, toID, fromID)
	if err != nil {
		return nil, fmt.Errorf("query changed symbols: %w", err)
	}
	defer rows.Close()

	var changed []ChangedSymbol
	for rows.Next() {
		var item ChangedSymbol
		if err := rows.Scan(&item.QName, &item.FromSignature, &item.ToSignature); err != nil {
			return nil, fmt.Errorf("scan changed symbol: %w", err)
		}
		changed = append(changed, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate changed symbols: %w", err)
	}
	return changed, nil
}

func scanRelatedSymbols(rows *sql.Rows) ([]RelatedSymbolView, error) {
	var edges []RelatedSymbolView
	for rows.Next() {
		var edge RelatedSymbolView
		if err := rows.Scan(
			append(symbolMatchScanDest(&edge.Symbol), &edge.UseFilePath, &edge.UseLine, &edge.UseColumn, &edge.Relation)...,
		); err != nil {
			return nil, fmt.Errorf("scan edge: %w", err)
		}
		edges = append(edges, edge)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate edges: %w", err)
	}
	return edges, nil
}

func symbolMatchScanDest(symbol *SymbolMatch) []any {
	return []any{
		&symbol.SymbolKey,
		&symbol.QName,
		&symbol.PackageImportPath,
		&symbol.FilePath,
		&symbol.Name,
		&symbol.Kind,
		&symbol.Receiver,
		&symbol.Signature,
		&symbol.Doc,
		&symbol.Line,
		&symbol.Column,
	}
}

func loadStringRows(rows *sql.Rows, err error) ([]string, error) {
	if err != nil {
		return nil, fmt.Errorf("query string rows: %w", err)
	}
	defer rows.Close()

	var values []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("scan string row: %w", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate string rows: %w", err)
	}
	return values, nil
}

func packageList(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func copyTableByPackage(tx *sql.Tx, table, column string, fromID, toID int64, impacted []string) error {
	args := make([]any, 0, len(impacted)+2)
	args = append(args, toID, fromID)
	placeholders := make([]string, 0, len(impacted))
	for _, value := range impacted {
		args = append(args, value)
		placeholders = append(placeholders, "?")
	}

	query := `INSERT INTO ` + table + ` SELECT ?, ` + forwardColumns(table) + ` FROM ` + table + ` WHERE snapshot_id = ?`
	if len(impacted) > 0 {
		query += ` AND ` + column + ` NOT IN (` + strings.Join(placeholders, ",") + `)`
	}

	if _, err := tx.Exec(query, args...); err != nil {
		return fmt.Errorf("copy forward %s: %w", table, err)
	}
	return nil
}

func forwardColumns(table string) string {
	switch table {
	case "packages":
		return `import_path, name, dir_path, file_count`
	case "symbols":
		return `symbol_key, qname, package_import_path, file_path, name, kind, receiver, signature, doc, line, col, exported, is_test`
	case "package_deps":
		return `from_package_import_path, to_package_import_path, is_local`
	case "refs":
		return `from_package_import_path, from_symbol_key, to_symbol_key, file_path, line, col, kind`
	case "call_edges":
		return `caller_package_import_path, caller_symbol_key, callee_symbol_key, file_path, line, col, dispatch`
	case "tests":
		return `test_key, package_import_path, file_path, name, kind, line`
	case "test_links":
		return `test_package_import_path, test_key, symbol_key, link_kind, confidence`
	default:
		panic("unsupported forward table: " + table)
	}
}

func derivePackageForFile(root, modulePath, relPath string) string {
	if relPath == "go.mod" || relPath == "go.sum" {
		return ""
	}
	_ = root
	return codebase.PackageImportPath(modulePath, relPath)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func currentID(ok bool, value int64) int64 {
	if !ok {
		return 0
	}
	return value
}

func symbolRank(symbol SymbolMatch, query string) string {
	switch {
	case symbol.SymbolKey == query:
		return "0|" + symbol.QName
	case symbol.QName == query:
		return "1|" + symbol.QName
	case symbol.Name == query:
		return "2|" + symbol.QName
	case strings.HasSuffix(symbol.QName, query):
		return "3|" + symbol.QName
	default:
		return "9|" + symbol.QName
	}
}

func (s *Store) previousSnapshotID(currentID int64) (int64, error) {
	var previous sql.NullInt64
	err := s.db.QueryRow(`SELECT MAX(id) FROM snapshots WHERE id < ?`, currentID).Scan(&previous)
	if err != nil {
		return 0, fmt.Errorf("query previous snapshot id: %w", err)
	}
	if !previous.Valid {
		return 0, nil
	}
	return previous.Int64, nil
}
