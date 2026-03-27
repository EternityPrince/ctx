package storage

import (
	"fmt"
	"strings"
)

type RankedSymbol struct {
	Symbol             SymbolMatch
	CallerCount        int
	CalleeCount        int
	ReferenceCount     int
	TestCount          int
	ReversePackageDeps int
	MethodCount        int
	GraphScore         int
	Score              int
	QualityWhy         []string
	Provenance         []ProvenanceItem
}

type RankedPackage struct {
	Summary         PackageSummary
	LocalDepCount   int
	ReverseDepCount int
	GraphScore      int
	Score           int
	QualityWhy      []string
	Provenance      []ProvenanceItem
}

type RankedFile struct {
	Summary          FileSummary
	GraphScore       int
	Score            int
	QualityWhy       []string
	PrimarySymbolKey string
	PrimaryLine      int
	TopSymbols       []string
}

type ReportView struct {
	Snapshot        SnapshotInfo
	TopPackages     []RankedPackage
	TopFiles        []RankedFile
	TopFunctions    []RankedSymbol
	TopTypes        []RankedSymbol
	ProvenanceNotes []string
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
	InboundCallCount      int
	InboundReferenceCount int
	ReversePackageDeps    int
	GraphScore            int
	QualityScore          int
	QualityWhy            []string
	IsEntrypoint          bool
	ChangedRecently       bool
	ChangeDistance        int
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

	candidateLimit := max(limit*4, 24)

	view := ReportView{Snapshot: current}
	if view.TopPackages, err = s.loadTopPackages(current.ID, candidateLimit); err != nil {
		return ReportView{}, err
	}
	fileSummaries, err := s.loadFileSummaries(current.ID, "")
	if err != nil {
		return ReportView{}, err
	}
	if view.TopFunctions, err = s.loadTopFunctions(current.ID, candidateLimit); err != nil {
		return ReportView{}, err
	}
	if view.TopTypes, err = s.loadTopTypes(current.ID, candidateLimit); err != nil {
		return ReportView{}, err
	}
	ctx, err := s.loadQualityContext(current)
	if err != nil {
		return ReportView{}, err
	}
	for path, summary := range fileSummaries {
		fileSummaries[path] = applyFileSummaryQuality(summary, ctx)
	}
	view.TopFiles = buildRankedFiles(fileSummaries, limit)
	if err := s.attachRankedFileSymbols(current.ID, view.TopFiles); err != nil {
		return ReportView{}, err
	}
	applyRankedPackageQuality(view.TopPackages, ctx)
	applyRankedSymbolQuality(view.TopFunctions, "function", ctx)
	applyRankedSymbolQuality(view.TopTypes, "type", ctx)
	if len(view.TopPackages) > limit {
		view.TopPackages = view.TopPackages[:limit]
	}
	if len(view.TopFunctions) > limit {
		view.TopFunctions = view.TopFunctions[:limit]
	}
	if len(view.TopTypes) > limit {
		view.TopTypes = view.TopTypes[:limit]
	}
	view.ProvenanceNotes = []string{
		fmt.Sprintf("Derived from indexed graph: packages=%d files=%d symbols=%d refs=%d calls=%d tests=%d.", current.TotalPackages, current.TotalFiles, current.TotalSymbols, current.TotalRefs, current.TotalCalls, current.TotalTests),
		"Quality score combines graph signals, recent change proximity, and entrypoint heuristics.",
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

	summaries, err := s.loadFileSummaries(current.ID, "")
	if err != nil {
		return nil, err
	}
	ctx, err := s.loadQualityContext(current)
	if err != nil {
		return nil, err
	}
	for path, summary := range summaries {
		summaries[path] = applyFileSummaryQuality(summary, ctx)
	}
	return summaries, nil
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
	ctx, err := s.loadQualityContext(current)
	if err != nil {
		return FileSummary{}, err
	}
	for path, summary := range summaries {
		summaries[path] = applyFileSummaryQuality(summary, ctx)
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
			COUNT(DISTINCT CASE WHEN tl.test_key IS NOT NULL AND s.kind IN ('func', 'method', 'struct', 'interface', 'type', 'alias', 'class') AND s.is_test = 0 THEN s.symbol_key END),
			(
				SELECT COUNT(DISTINCT c.caller_symbol_key || '|' || c.file_path || '|' || c.line)
				FROM call_edges c
				JOIN symbols target
					ON target.snapshot_id = c.snapshot_id
					AND target.symbol_key = c.callee_symbol_key
				WHERE c.snapshot_id = f.snapshot_id
					AND target.file_path = f.rel_path
			),
			(
				SELECT COUNT(DISTINCT r.from_symbol_key || '|' || r.file_path || '|' || r.line)
				FROM refs r
				JOIN symbols target
					ON target.snapshot_id = r.snapshot_id
					AND target.symbol_key = r.to_symbol_key
				WHERE r.snapshot_id = f.snapshot_id
					AND target.file_path = f.rel_path
					AND r.from_symbol_key != ''
			),
			(
				SELECT COUNT(DISTINCT pd.from_package_import_path)
				FROM package_deps pd
				WHERE pd.snapshot_id = f.snapshot_id
					AND pd.to_package_import_path = f.package_import_path
					AND pd.is_local = 1
			)
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
			&item.InboundCallCount,
			&item.InboundReferenceCount,
			&item.ReversePackageDeps,
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
		item.GraphScore = item.Score
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
		item.GraphScore = item.Score
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
		item.GraphScore = item.Score
		symbols = append(symbols, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate top types: %w", err)
	}
	return symbols, nil
}

func (s *Store) attachReportProvenance(view *ReportView) error {
	for idx := range view.TopPackages {
		items, err := s.loadPackageProvenance(view.Snapshot.ID, view.TopPackages[idx].Summary.ImportPath, 3)
		if err != nil {
			return err
		}
		view.TopPackages[idx].Provenance = items
	}
	for idx := range view.TopFunctions {
		items, err := s.loadSymbolProvenance(view.Snapshot.ID, view.TopFunctions[idx].Symbol, 3)
		if err != nil {
			return err
		}
		view.TopFunctions[idx].Provenance = items
	}
	for idx := range view.TopTypes {
		items, err := s.loadSymbolProvenance(view.Snapshot.ID, view.TopTypes[idx].Symbol, 3)
		if err != nil {
			return err
		}
		view.TopTypes[idx].Provenance = items
	}
	return nil
}

func (s *Store) ExplainReportView(view *ReportView) error {
	return s.attachReportProvenance(view)
}

func (s *Store) loadSymbolProvenance(snapshotID int64, symbol SymbolMatch, limit int) ([]ProvenanceItem, error) {
	if limit <= 0 {
		limit = 3
	}

	var items []ProvenanceItem
	appendItems := func(values []ProvenanceItem) {
		for _, value := range values {
			if len(items) >= limit {
				return
			}
			items = append(items, value)
		}
	}

	if callers, err := s.loadProvenanceRows(`
		SELECT 'call', src.qname, c.file_path, c.line, c.dispatch
		FROM call_edges c
		JOIN symbols src ON src.snapshot_id = c.snapshot_id AND src.symbol_key = c.caller_symbol_key
		WHERE c.snapshot_id = ? AND c.callee_symbol_key = ?
		ORDER BY src.qname, c.file_path, c.line
		LIMIT ?
	`, snapshotID, symbol.SymbolKey, limit); err != nil {
		return nil, fmt.Errorf("load caller provenance: %w", err)
	} else {
		appendItems(describeCallProvenance(callers))
	}

	if refs, err := s.loadProvenanceRows(`
		SELECT 'ref', src.qname, r.file_path, r.line, r.kind
		FROM refs r
		JOIN symbols src ON src.snapshot_id = r.snapshot_id AND src.symbol_key = r.from_symbol_key
		WHERE r.snapshot_id = ? AND r.to_symbol_key = ? AND r.from_symbol_key != ''
		ORDER BY src.qname, r.file_path, r.line
		LIMIT ?
	`, snapshotID, symbol.SymbolKey, limit); err != nil {
		return nil, fmt.Errorf("load reference provenance: %w", err)
	} else {
		appendItems(describeReferenceProvenance(refs))
	}

	if tests, err := s.loadProvenanceRows(`
		SELECT 'test', t.name, t.file_path, t.line, tl.link_kind || ':' || tl.confidence
		FROM test_links tl
		JOIN tests t ON t.snapshot_id = tl.snapshot_id AND t.test_key = tl.test_key
		WHERE tl.snapshot_id = ? AND tl.symbol_key = ?
		ORDER BY
			CASE tl.confidence WHEN 'high' THEN 3 WHEN 'medium' THEN 2 WHEN 'low' THEN 1 ELSE 0 END DESC,
			CASE tl.link_kind WHEN 'direct' THEN 3 WHEN 'receiver_match' THEN 3 WHEN 'related' THEN 2 WHEN 'name_match' THEN 2 WHEN 'global_name_match' THEN 1 ELSE 0 END DESC,
			t.file_path,
			t.line
		LIMIT ?
	`, snapshotID, symbol.SymbolKey, limit); err != nil {
		return nil, fmt.Errorf("load test provenance: %w", err)
	} else {
		appendItems(describeTestProvenance(tests))
	}

	if symbol.Kind == "struct" || symbol.Kind == "interface" || symbol.Kind == "type" || symbol.Kind == "alias" || symbol.Kind == "class" {
		if methods, err := s.loadProvenanceRows(`
			SELECT 'method', m.qname, m.file_path, m.line, ''
			FROM symbols m
			WHERE m.snapshot_id = ?
			  AND m.package_import_path = ?
			  AND m.kind = 'method'
			  AND (m.receiver = ? OR m.receiver = '*' || ?)
			ORDER BY m.file_path, m.line, m.qname
			LIMIT ?
		`, snapshotID, symbol.PackageImportPath, symbol.Name, symbol.Name, limit); err != nil {
			return nil, fmt.Errorf("load method provenance: %w", err)
		} else {
			appendItems(describeMethodProvenance(methods))
		}
	}

	if rdeps, err := s.loadProvenanceRows(`
		SELECT 'reverse_dep', pd.from_package_import_path, '', 0, ''
		FROM package_deps pd
		WHERE pd.snapshot_id = ? AND pd.to_package_import_path = ? AND pd.is_local = 1
		ORDER BY pd.from_package_import_path
		LIMIT ?
	`, snapshotID, symbol.PackageImportPath, limit); err != nil {
		return nil, fmt.Errorf("load reverse dependency provenance: %w", err)
	} else {
		appendItems(describeReverseDepProvenance(rdeps))
	}

	return items, nil
}

func (s *Store) loadPackageProvenance(snapshotID int64, importPath string, limit int) ([]ProvenanceItem, error) {
	if limit <= 0 {
		limit = 3
	}

	var items []ProvenanceItem
	appendItems := func(values []ProvenanceItem) {
		for _, value := range values {
			if len(items) >= limit {
				return
			}
			items = append(items, value)
		}
	}

	if rdeps, err := s.loadProvenanceRows(`
		SELECT 'reverse_dep', pd.from_package_import_path, '', 0, ''
		FROM package_deps pd
		WHERE pd.snapshot_id = ? AND pd.to_package_import_path = ? AND pd.is_local = 1
		ORDER BY pd.from_package_import_path
		LIMIT ?
	`, snapshotID, importPath, limit); err != nil {
		return nil, fmt.Errorf("load package reverse dependencies: %w", err)
	} else {
		appendItems(describeReverseDepProvenance(rdeps))
	}

	if symbols, err := s.loadProvenanceRows(`
		SELECT 'symbol', s.qname, s.file_path, s.line,
		       printf('callers=%d refs=%d tests=%d',
		           (SELECT COUNT(*) FROM call_edges c WHERE c.snapshot_id = s.snapshot_id AND c.callee_symbol_key = s.symbol_key),
		           (SELECT COUNT(*) FROM refs r WHERE r.snapshot_id = s.snapshot_id AND r.to_symbol_key = s.symbol_key AND r.from_symbol_key != ''),
		           (SELECT COUNT(DISTINCT tl.test_key) FROM test_links tl WHERE tl.snapshot_id = s.snapshot_id AND tl.symbol_key = s.symbol_key))
		FROM symbols s
		WHERE s.snapshot_id = ? AND s.package_import_path = ?
		ORDER BY
			((SELECT COUNT(*) FROM call_edges c WHERE c.snapshot_id = s.snapshot_id AND c.callee_symbol_key = s.symbol_key) * 5) +
			((SELECT COUNT(*) FROM refs r WHERE r.snapshot_id = s.snapshot_id AND r.to_symbol_key = s.symbol_key AND r.from_symbol_key != '') * 2) +
			((SELECT COUNT(DISTINCT tl.test_key) FROM test_links tl WHERE tl.snapshot_id = s.snapshot_id AND tl.symbol_key = s.symbol_key) * 3) DESC,
			s.qname
		LIMIT ?
	`, snapshotID, importPath, limit); err != nil {
		return nil, fmt.Errorf("load package symbol provenance: %w", err)
	} else {
		appendItems(describePackageSymbolProvenance(symbols))
	}

	if tests, err := s.loadProvenanceRows(`
		SELECT 'test', t.name, t.file_path, t.line, printf('links=%d', COUNT(DISTINCT tl.symbol_key))
		FROM tests t
		LEFT JOIN test_links tl
			ON tl.snapshot_id = t.snapshot_id
			AND tl.test_key = t.test_key
		WHERE t.snapshot_id = ? AND t.package_import_path = ?
		GROUP BY t.name, t.file_path, t.line
		ORDER BY COUNT(DISTINCT tl.symbol_key) DESC, t.file_path, t.line, t.name
		LIMIT ?
	`, snapshotID, importPath, limit); err != nil {
		return nil, fmt.Errorf("load package test provenance: %w", err)
	} else {
		appendItems(describePackageTestProvenance(tests))
	}

	return items, nil
}

type provenanceRow struct {
	Kind     string
	Label    string
	FilePath string
	Line     int
	Meta     string
}

func (s *Store) loadProvenanceRows(query string, args ...any) ([]provenanceRow, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []provenanceRow
	for rows.Next() {
		var item provenanceRow
		if err := rows.Scan(&item.Kind, &item.Label, &item.FilePath, &item.Line, &item.Meta); err != nil {
			return nil, fmt.Errorf("scan provenance row: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate provenance rows: %w", err)
	}
	return items, nil
}

func describeCallProvenance(rows []provenanceRow) []ProvenanceItem {
	items := make([]ProvenanceItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, ProvenanceItem{
			Kind:     row.Kind,
			Label:    row.Label,
			FilePath: row.FilePath,
			Line:     row.Line,
			Why:      describeCallRelation(row.Meta),
		})
	}
	return items
}

func describeReferenceProvenance(rows []provenanceRow) []ProvenanceItem {
	items := make([]ProvenanceItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, ProvenanceItem{
			Kind:     row.Kind,
			Label:    row.Label,
			FilePath: row.FilePath,
			Line:     row.Line,
			Why:      describeReferenceKind(row.Meta),
		})
	}
	return items
}

func describeTestProvenance(rows []provenanceRow) []ProvenanceItem {
	items := make([]ProvenanceItem, 0, len(rows))
	for _, row := range rows {
		linkKind, confidence := splitLinkMeta(row.Meta)
		items = append(items, ProvenanceItem{
			Kind:     row.Kind,
			Label:    row.Label,
			FilePath: row.FilePath,
			Line:     row.Line,
			Why:      describeDirectTestLink(linkKind, confidence),
		})
	}
	return items
}

func describeMethodProvenance(rows []provenanceRow) []ProvenanceItem {
	items := make([]ProvenanceItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, ProvenanceItem{
			Kind:     row.Kind,
			Label:    row.Label,
			FilePath: row.FilePath,
			Line:     row.Line,
			Why:      "attached method in indexed type surface",
		})
	}
	return items
}

func describeReverseDepProvenance(rows []provenanceRow) []ProvenanceItem {
	items := make([]ProvenanceItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, ProvenanceItem{
			Kind:  row.Kind,
			Label: row.Label,
			Why:   "reverse package dependency in local graph",
		})
	}
	return items
}

func describePackageSymbolProvenance(rows []provenanceRow) []ProvenanceItem {
	items := make([]ProvenanceItem, 0, len(rows))
	for _, row := range rows {
		why := "hot symbol in package ranking"
		if row.Meta != "" {
			why += " (" + row.Meta + ")"
		}
		items = append(items, ProvenanceItem{
			Kind:     row.Kind,
			Label:    row.Label,
			FilePath: row.FilePath,
			Line:     row.Line,
			Why:      why,
		})
	}
	return items
}

func describePackageTestProvenance(rows []provenanceRow) []ProvenanceItem {
	items := make([]ProvenanceItem, 0, len(rows))
	for _, row := range rows {
		why := "package test signal"
		if row.Meta != "" {
			why += " (" + row.Meta + ")"
		}
		items = append(items, ProvenanceItem{
			Kind:     row.Kind,
			Label:    row.Label,
			FilePath: row.FilePath,
			Line:     row.Line,
			Why:      why,
		})
	}
	return items
}

func splitLinkMeta(value string) (string, string) {
	parts := strings.SplitN(value, ":", 2)
	if len(parts) == 0 {
		return "", ""
	}
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}
