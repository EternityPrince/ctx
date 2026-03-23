package storage

import "fmt"

type RankedSymbol struct {
	Symbol             SymbolMatch
	CallerCount        int
	CalleeCount        int
	ReferenceCount     int
	TestCount          int
	ReversePackageDeps int
	MethodCount        int
	Score              int
}

type RankedPackage struct {
	Summary         PackageSummary
	LocalDepCount   int
	ReverseDepCount int
	Score           int
}

type ReportView struct {
	Snapshot     SnapshotInfo
	TopPackages  []RankedPackage
	TopFunctions []RankedSymbol
	TopTypes     []RankedSymbol
}

type FileSummary struct {
	FilePath              string
	PackageImportPath     string
	SizeBytes             int64
	IsTest                bool
	SymbolCount           int
	FuncCount             int
	MethodCount           int
	StructCount           int
	DeclaredTestCount     int
	RelatedTestCount      int
	RelevantSymbolCount   int
	TestLinkedSymbolCount int
}

func (s *Store) LoadReportView(limit int) (ReportView, error) {
	if limit <= 0 {
		limit = 8
	}

	current, ok, err := s.CurrentSnapshot()
	if err != nil {
		return ReportView{}, err
	}
	if !ok {
		return ReportView{}, fmt.Errorf("no snapshots available")
	}

	view := ReportView{Snapshot: current}
	if view.TopPackages, err = s.loadTopPackages(current.ID, limit); err != nil {
		return ReportView{}, err
	}
	if view.TopFunctions, err = s.loadTopFunctions(current.ID, limit); err != nil {
		return ReportView{}, err
	}
	if view.TopTypes, err = s.loadTopTypes(current.ID, limit); err != nil {
		return ReportView{}, err
	}
	return view, nil
}

func (s *Store) LoadFileSummaries() (map[string]FileSummary, error) {
	current, ok, err := s.CurrentSnapshot()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("no snapshots available")
	}

	return s.loadFileSummaries(current.ID, "")
}

func (s *Store) LoadFileSummary(filePath string) (FileSummary, error) {
	current, ok, err := s.CurrentSnapshot()
	if err != nil {
		return FileSummary{}, err
	}
	if !ok {
		return FileSummary{}, fmt.Errorf("no snapshots available")
	}

	summaries, err := s.loadFileSummaries(current.ID, filePath)
	if err != nil {
		return FileSummary{}, err
	}
	if summary, ok := summaries[filePath]; ok {
		return summary, nil
	}
	return FileSummary{FilePath: filePath}, nil
}

func (s *Store) loadFileSummaries(snapshotID int64, filePath string) (map[string]FileSummary, error) {
	query := `
		SELECT
			f.rel_path,
			f.package_import_path,
			f.size_bytes,
			f.is_test,
			COUNT(DISTINCT s.symbol_key),
			COUNT(DISTINCT CASE WHEN s.kind = 'func' AND s.is_test = 0 THEN s.symbol_key END),
			COUNT(DISTINCT CASE WHEN s.kind = 'method' AND s.is_test = 0 THEN s.symbol_key END),
			COUNT(DISTINCT CASE WHEN s.kind IN ('struct', 'interface', 'type', 'alias', 'class') AND s.is_test = 0 THEN s.symbol_key END),
			COUNT(DISTINCT tests.test_key),
			COUNT(DISTINCT tl.test_key),
			COUNT(DISTINCT CASE WHEN s.kind IN ('func', 'method', 'struct', 'interface', 'type', 'alias', 'class') AND s.is_test = 0 THEN s.symbol_key END),
			COUNT(DISTINCT CASE WHEN tl.test_key IS NOT NULL AND s.kind IN ('func', 'method', 'struct', 'interface', 'type', 'alias', 'class') AND s.is_test = 0 THEN s.symbol_key END)
		FROM files f
		LEFT JOIN symbols s
			ON s.snapshot_id = f.snapshot_id
			AND s.file_path = f.rel_path
		LEFT JOIN tests
			ON tests.snapshot_id = f.snapshot_id
			AND tests.file_path = f.rel_path
		LEFT JOIN test_links tl
			ON tl.snapshot_id = f.snapshot_id
			AND tl.symbol_key = s.symbol_key
		WHERE f.snapshot_id = ?
	`
	args := []any{snapshotID}
	if filePath != "" {
		query += ` AND f.rel_path = ?`
		args = append(args, filePath)
	}
	query += `
		GROUP BY f.rel_path, f.package_import_path, f.size_bytes, f.is_test
		ORDER BY f.rel_path
	`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query file summaries: %w", err)
	}
	defer rows.Close()

	summaries := make(map[string]FileSummary)
	for rows.Next() {
		var item FileSummary
		var isTest int
		if err := rows.Scan(
			&item.FilePath,
			&item.PackageImportPath,
			&item.SizeBytes,
			&isTest,
			&item.SymbolCount,
			&item.FuncCount,
			&item.MethodCount,
			&item.StructCount,
			&item.DeclaredTestCount,
			&item.RelatedTestCount,
			&item.RelevantSymbolCount,
			&item.TestLinkedSymbolCount,
		); err != nil {
			return nil, fmt.Errorf("scan file summary: %w", err)
		}
		item.IsTest = isTest == 1
		summaries[item.FilePath] = item
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate file summaries: %w", err)
	}
	return summaries, nil
}

func (s *Store) loadTopPackages(snapshotID int64, limit int) ([]RankedPackage, error) {
	rows, err := s.db.Query(`
		SELECT p.import_path, p.name, p.dir_path, p.file_count,
		       (SELECT COUNT(*) FROM symbols s WHERE s.snapshot_id = p.snapshot_id AND s.package_import_path = p.import_path),
		       (SELECT COUNT(*) FROM tests t WHERE t.snapshot_id = p.snapshot_id AND t.package_import_path = p.import_path),
		       (SELECT COUNT(DISTINCT pd.to_package_import_path) FROM package_deps pd WHERE pd.snapshot_id = p.snapshot_id AND pd.from_package_import_path = p.import_path AND pd.is_local = 1),
		       (SELECT COUNT(DISTINCT pd.from_package_import_path) FROM package_deps pd WHERE pd.snapshot_id = p.snapshot_id AND pd.to_package_import_path = p.import_path AND pd.is_local = 1)
		FROM packages p
		WHERE p.snapshot_id = ?
		ORDER BY 8 DESC, 5 DESC, 6 DESC, p.import_path
		LIMIT ?
	`, snapshotID, limit)
	if err != nil {
		return nil, fmt.Errorf("query top packages: %w", err)
	}
	defer rows.Close()

	var packages []RankedPackage
	for rows.Next() {
		var item RankedPackage
		if err := rows.Scan(
			&item.Summary.ImportPath,
			&item.Summary.Name,
			&item.Summary.DirPath,
			&item.Summary.FileCount,
			&item.Summary.SymbolCount,
			&item.Summary.TestCount,
			&item.LocalDepCount,
			&item.ReverseDepCount,
		); err != nil {
			return nil, fmt.Errorf("scan top package: %w", err)
		}
		item.Score = item.ReverseDepCount*4 + item.Summary.SymbolCount + item.Summary.TestCount*2
		packages = append(packages, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate top packages: %w", err)
	}
	return packages, nil
}

func (s *Store) loadTopFunctions(snapshotID int64, limit int) ([]RankedSymbol, error) {
	rows, err := s.db.Query(`
		SELECT s.symbol_key, s.qname, s.package_import_path, s.file_path, s.name, s.kind, s.receiver, s.signature, s.doc, s.line, s.col,
		       (SELECT COUNT(*) FROM call_edges c WHERE c.snapshot_id = s.snapshot_id AND c.callee_symbol_key = s.symbol_key),
		       (SELECT COUNT(*) FROM call_edges c WHERE c.snapshot_id = s.snapshot_id AND c.caller_symbol_key = s.symbol_key),
		       (SELECT COUNT(*) FROM refs r WHERE r.snapshot_id = s.snapshot_id AND r.to_symbol_key = s.symbol_key AND r.from_symbol_key != ''),
		       (SELECT COUNT(DISTINCT tl.test_key) FROM test_links tl WHERE tl.snapshot_id = s.snapshot_id AND tl.symbol_key = s.symbol_key),
		       (SELECT COUNT(DISTINCT pd.from_package_import_path) FROM package_deps pd WHERE pd.snapshot_id = s.snapshot_id AND pd.to_package_import_path = s.package_import_path AND pd.is_local = 1)
		FROM symbols s
		WHERE s.snapshot_id = ? AND s.kind IN ('func', 'method')
		ORDER BY ((SELECT COUNT(*) FROM call_edges c WHERE c.snapshot_id = s.snapshot_id AND c.callee_symbol_key = s.symbol_key) * 5) +
		         (SELECT COUNT(*) FROM call_edges c WHERE c.snapshot_id = s.snapshot_id AND c.caller_symbol_key = s.symbol_key) +
		         ((SELECT COUNT(*) FROM refs r WHERE r.snapshot_id = s.snapshot_id AND r.to_symbol_key = s.symbol_key AND r.from_symbol_key != '') * 2) +
		         ((SELECT COUNT(DISTINCT tl.test_key) FROM test_links tl WHERE tl.snapshot_id = s.snapshot_id AND tl.symbol_key = s.symbol_key) * 3) +
		         ((SELECT COUNT(DISTINCT pd.from_package_import_path) FROM package_deps pd WHERE pd.snapshot_id = s.snapshot_id AND pd.to_package_import_path = s.package_import_path AND pd.is_local = 1) * 2) DESC,
		         s.qname
		LIMIT ?
	`, snapshotID, limit)
	if err != nil {
		return nil, fmt.Errorf("query top functions: %w", err)
	}
	defer rows.Close()

	var symbols []RankedSymbol
	for rows.Next() {
		var item RankedSymbol
		dest := append(
			symbolMatchScanDest(&item.Symbol),
			&item.CallerCount,
			&item.CalleeCount,
			&item.ReferenceCount,
			&item.TestCount,
			&item.ReversePackageDeps,
		)
		if err := rows.Scan(dest...); err != nil {
			return nil, fmt.Errorf("scan top function: %w", err)
		}
		item.Score = item.CallerCount*5 + item.CalleeCount + item.ReferenceCount*2 + item.TestCount*3 + item.ReversePackageDeps*2
		symbols = append(symbols, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate top functions: %w", err)
	}
	return symbols, nil
}

func (s *Store) loadTopTypes(snapshotID int64, limit int) ([]RankedSymbol, error) {
	rows, err := s.db.Query(`
		SELECT s.symbol_key, s.qname, s.package_import_path, s.file_path, s.name, s.kind, s.receiver, s.signature, s.doc, s.line, s.col,
		       (SELECT COUNT(*) FROM refs r WHERE r.snapshot_id = s.snapshot_id AND r.to_symbol_key = s.symbol_key AND r.from_symbol_key != ''),
		       (SELECT COUNT(DISTINCT tl.test_key) FROM test_links tl WHERE tl.snapshot_id = s.snapshot_id AND tl.symbol_key = s.symbol_key),
		       (SELECT COUNT(DISTINCT pd.from_package_import_path) FROM package_deps pd WHERE pd.snapshot_id = s.snapshot_id AND pd.to_package_import_path = s.package_import_path AND pd.is_local = 1),
		       (SELECT COUNT(*) FROM symbols m WHERE m.snapshot_id = s.snapshot_id AND m.package_import_path = s.package_import_path AND m.kind = 'method' AND (m.receiver = s.name OR m.receiver = '*' || s.name))
		FROM symbols s
		WHERE s.snapshot_id = ? AND s.kind IN ('struct', 'interface', 'type', 'alias', 'class')
		ORDER BY ((SELECT COUNT(*) FROM refs r WHERE r.snapshot_id = s.snapshot_id AND r.to_symbol_key = s.symbol_key AND r.from_symbol_key != '') * 4) +
		         ((SELECT COUNT(DISTINCT tl.test_key) FROM test_links tl WHERE tl.snapshot_id = s.snapshot_id AND tl.symbol_key = s.symbol_key) * 3) +
		         ((SELECT COUNT(*) FROM symbols m WHERE m.snapshot_id = s.snapshot_id AND m.package_import_path = s.package_import_path AND m.kind = 'method' AND (m.receiver = s.name OR m.receiver = '*' || s.name)) * 2) +
		         ((SELECT COUNT(DISTINCT pd.from_package_import_path) FROM package_deps pd WHERE pd.snapshot_id = s.snapshot_id AND pd.to_package_import_path = s.package_import_path AND pd.is_local = 1) * 2) DESC,
		         s.qname
		LIMIT ?
	`, snapshotID, limit)
	if err != nil {
		return nil, fmt.Errorf("query top types: %w", err)
	}
	defer rows.Close()

	var symbols []RankedSymbol
	for rows.Next() {
		var item RankedSymbol
		dest := append(
			symbolMatchScanDest(&item.Symbol),
			&item.ReferenceCount,
			&item.TestCount,
			&item.ReversePackageDeps,
			&item.MethodCount,
		)
		if err := rows.Scan(dest...); err != nil {
			return nil, fmt.Errorf("scan top type: %w", err)
		}
		item.Score = item.ReferenceCount*4 + item.TestCount*3 + item.MethodCount*2 + item.ReversePackageDeps*2
		symbols = append(symbols, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate top types: %w", err)
	}
	return symbols, nil
}
