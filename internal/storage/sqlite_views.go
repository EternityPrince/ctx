package storage

import (
	"fmt"
	"sort"
)

func (s *Store) FindPackages(query string) ([]PackageMatch, error) {
	current, ok, err := s.CurrentSnapshot()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}

	rows, err := s.db.Query(`
		SELECT import_path, name, dir_path
		FROM packages
		WHERE snapshot_id = ?
	`, current.ID)
	if err != nil {
		return nil, fmt.Errorf("query packages: %w", err)
	}
	defer rows.Close()

	var matches []PackageMatch
	for rows.Next() {
		var match PackageMatch
		if err := rows.Scan(&match.ImportPath, &match.Name, &match.DirPath); err != nil {
			return nil, fmt.Errorf("scan package match: %w", err)
		}
		score, kind := packageSearchScore(match, query)
		if score == 0 {
			continue
		}
		match.SearchScore = score
		match.SearchKind = kind
		matches = append(matches, match)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate packages: %w", err)
	}

	sort.Slice(matches, func(i, j int) bool {
		if packageSearchKindRank(matches[i].SearchKind) != packageSearchKindRank(matches[j].SearchKind) {
			return packageSearchKindRank(matches[i].SearchKind) < packageSearchKindRank(matches[j].SearchKind)
		}
		if matches[i].SearchScore != matches[j].SearchScore {
			return matches[i].SearchScore > matches[j].SearchScore
		}
		return matches[i].ImportPath < matches[j].ImportPath
	})
	if len(matches) > 40 {
		matches = matches[:40]
	}
	return matches, nil
}

func (s *Store) FindSymbols(query string) ([]SymbolMatch, error) {
	current, ok, err := s.CurrentSnapshot()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}

	packageMetrics, err := s.loadSearchPackageMetrics(current.ID)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.Query(`
		SELECT s.symbol_key, s.qname, s.package_import_path, s.file_path, s.name, s.kind, s.receiver, s.signature, s.doc, s.line, s.col,
		       COALESCE(callers.caller_count, 0),
		       COALESCE(callees.callee_count, 0),
		       COALESCE(refs.reference_count, 0),
		       COALESCE(tests.test_count, 0)
		FROM symbols s
		LEFT JOIN (
			SELECT callee_symbol_key, COUNT(*) AS caller_count
			FROM call_edges
			WHERE snapshot_id = ?
			GROUP BY callee_symbol_key
		) callers ON callers.callee_symbol_key = s.symbol_key
		LEFT JOIN (
			SELECT caller_symbol_key, COUNT(*) AS callee_count
			FROM call_edges
			WHERE snapshot_id = ?
			GROUP BY caller_symbol_key
		) callees ON callees.caller_symbol_key = s.symbol_key
		LEFT JOIN (
			SELECT to_symbol_key, COUNT(*) AS reference_count
			FROM refs
			WHERE snapshot_id = ? AND from_symbol_key != ''
			GROUP BY to_symbol_key
		) refs ON refs.to_symbol_key = s.symbol_key
		LEFT JOIN (
			SELECT symbol_key, COUNT(DISTINCT test_key) AS test_count
			FROM test_links
			WHERE snapshot_id = ?
			GROUP BY symbol_key
		) tests ON tests.symbol_key = s.symbol_key
		WHERE s.snapshot_id = ?
	`, current.ID, current.ID, current.ID, current.ID, current.ID)
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
			&symbol.CallerCount,
			&symbol.CalleeCount,
			&symbol.ReferenceCount,
			&symbol.TestCount,
		); err != nil {
			return nil, fmt.Errorf("scan symbol match: %w", err)
		}
		if pkg, ok := packageMetrics[symbol.PackageImportPath]; ok {
			symbol.ReversePackageDeps = pkg.ReverseDepCount
			symbol.PackageImportance = pkg.ImportanceScore
		}
		score, kind := symbolSearchScore(symbol, query)
		if score == 0 {
			continue
		}
		symbol.SearchScore = score
		symbol.SearchKind = kind
		symbol.RelevanceScore = symbolRelevanceScore(symbol)
		symbols = append(symbols, symbol)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate symbols: %w", err)
	}

	sort.Slice(symbols, func(i, j int) bool {
		left := symbols[i]
		right := symbols[j]
		if symbolSearchKindRank(left.SearchKind) != symbolSearchKindRank(right.SearchKind) {
			return symbolSearchKindRank(left.SearchKind) < symbolSearchKindRank(right.SearchKind)
		}
		if left.SearchScore != right.SearchScore {
			return left.SearchScore > right.SearchScore
		}
		if left.RelevanceScore != right.RelevanceScore {
			return left.RelevanceScore > right.RelevanceScore
		}
		if left.PackageImportance != right.PackageImportance {
			return left.PackageImportance > right.PackageImportance
		}
		if left.Kind != right.Kind {
			if symbolKindSearchBoost(left.Kind) != symbolKindSearchBoost(right.Kind) {
				return symbolKindSearchBoost(left.Kind) > symbolKindSearchBoost(right.Kind)
			}
			return left.Kind < right.Kind
		}
		if left.Line != right.Line {
			return left.Line < right.Line
		}
		return left.QName < right.QName
	})
	if len(symbols) > 40 {
		symbols = symbols[:40]
	}
	return symbols, nil
}

func (s *Store) LoadSearchPackageMetrics() (map[string]SearchPackageMetrics, error) {
	current, ok, err := s.CurrentSnapshot()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return s.loadSearchPackageMetrics(current.ID)
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
		ORDER BY
			CASE tl.confidence WHEN 'high' THEN 3 WHEN 'medium' THEN 2 WHEN 'low' THEN 1 ELSE 0 END DESC,
			CASE tl.link_kind WHEN 'receiver_match' THEN 3 WHEN 'name_match' THEN 2 WHEN 'global_name_match' THEN 1 ELSE 0 END DESC,
			t.file_path,
			t.line,
			t.name
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
		test.Relation = "direct"
		test.Score = 240 + testConfidenceRank(test.Confidence)*20 + testLinkKindRank(test.LinkKind)*6
		test.Why = describeDirectTestLink(test.LinkKind, test.Confidence)
		tests = append(tests, test)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tests: %w", err)
	}
	return tests, nil
}

func (s *Store) LoadPackageTests(importPath string, limit int) ([]TestView, error) {
	current, ok, err := s.CurrentSnapshot()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("no snapshots available")
	}
	if limit <= 0 {
		limit = 12
	}

	rows, err := s.db.Query(`
		SELECT t.test_key, t.package_import_path, t.name, t.file_path, t.kind, t.line,
		       COUNT(DISTINCT tl.symbol_key) AS linked_symbols
		FROM tests t
		LEFT JOIN test_links tl
			ON tl.snapshot_id = t.snapshot_id
			AND tl.test_key = t.test_key
		WHERE t.snapshot_id = ? AND t.package_import_path = ?
		GROUP BY t.test_key, t.package_import_path, t.name, t.file_path, t.kind, t.line
		ORDER BY linked_symbols DESC, t.file_path, t.line, t.name
		LIMIT ?
	`, current.ID, importPath, limit)
	if err != nil {
		return nil, fmt.Errorf("query package tests: %w", err)
	}
	defer rows.Close()

	var tests []TestView
	for rows.Next() {
		var test TestView
		var linkedSymbols int
		if err := rows.Scan(&test.TestKey, &test.PackageImportPath, &test.Name, &test.FilePath, &test.Kind, &test.Line, &linkedSymbols); err != nil {
			return nil, fmt.Errorf("scan package test view: %w", err)
		}
		test.Relation = "package"
		test.Score = linkedSymbols * 10
		test.Why = "same package fallback"
		tests = append(tests, test)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate package tests: %w", err)
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

func (s *Store) loadSearchPackageMetrics(snapshotID int64) (map[string]SearchPackageMetrics, error) {
	rows, err := s.db.Query(`
		SELECT p.import_path,
		       (SELECT COUNT(*) FROM symbols s WHERE s.snapshot_id = p.snapshot_id AND s.package_import_path = p.import_path),
		       (SELECT COUNT(*) FROM tests t WHERE t.snapshot_id = p.snapshot_id AND t.package_import_path = p.import_path),
		       (SELECT COUNT(DISTINCT pd.to_package_import_path) FROM package_deps pd WHERE pd.snapshot_id = p.snapshot_id AND pd.from_package_import_path = p.import_path AND pd.is_local = 1),
		       (SELECT COUNT(DISTINCT pd.from_package_import_path) FROM package_deps pd WHERE pd.snapshot_id = p.snapshot_id AND pd.to_package_import_path = p.import_path AND pd.is_local = 1)
		FROM packages p
		WHERE p.snapshot_id = ?
	`, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("query search package metrics: %w", err)
	}
	defer rows.Close()

	metrics := make(map[string]SearchPackageMetrics)
	for rows.Next() {
		var item SearchPackageMetrics
		if err := rows.Scan(
			&item.ImportPath,
			&item.SymbolCount,
			&item.TestCount,
			&item.LocalDepCount,
			&item.ReverseDepCount,
		); err != nil {
			return nil, fmt.Errorf("scan search package metrics: %w", err)
		}
		item.ImportanceScore = item.ReverseDepCount*4 + item.SymbolCount + item.TestCount*2
		metrics[item.ImportPath] = item
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate search package metrics: %w", err)
	}
	return metrics, nil
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
