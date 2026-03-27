package storage

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

type packageDiffMetrics struct {
	ImportPath      string
	FileCount       int
	SymbolCount     int
	TestCount       int
	LocalDepCount   int
	ReverseDepCount int
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
	if diff.ChangedPackages, err = s.changedPackages(fromID, toID); err != nil {
		return DiffView{}, err
	}
	if diff.AddedCalls, err = s.diffCallEdges(fromID, toID, "added"); err != nil {
		return DiffView{}, err
	}
	if diff.RemovedCalls, err = s.diffCallEdges(fromID, toID, "removed"); err != nil {
		return DiffView{}, err
	}
	if diff.AddedRefs, err = s.diffRefs(fromID, toID, "added"); err != nil {
		return DiffView{}, err
	}
	if diff.RemovedRefs, err = s.diffRefs(fromID, toID, "removed"); err != nil {
		return DiffView{}, err
	}
	if diff.AddedTestLinks, err = s.diffTestLinks(fromID, toID, "added"); err != nil {
		return DiffView{}, err
	}
	if diff.RemovedTestLinks, err = s.diffTestLinks(fromID, toID, "removed"); err != nil {
		return DiffView{}, err
	}
	if diff.AddedPackageDeps, err = s.diffPackageDeps(fromID, toID, "added"); err != nil {
		return DiffView{}, err
	}
	if diff.RemovedPackageDeps, err = s.diffPackageDeps(fromID, toID, "removed"); err != nil {
		return DiffView{}, err
	}
	diff.ImpactedSymbols = buildImpactedSymbols(diff)

	return diff, nil
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
		SELECT s1.qname, s1.signature, s2.signature, s1.package_import_path, s2.package_import_path, s1.file_path, s2.file_path, s1.line, s2.line
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
		if err := rows.Scan(
			&item.QName,
			&item.FromSignature,
			&item.ToSignature,
			&item.FromPackageImportPath,
			&item.ToPackageImportPath,
			&item.FromFilePath,
			&item.ToFilePath,
			&item.FromLine,
			&item.ToLine,
		); err != nil {
			return nil, fmt.Errorf("scan changed symbol: %w", err)
		}
		item.ContractChanged = item.FromSignature != item.ToSignature
		item.Moved = item.FromFilePath != item.ToFilePath || item.FromLine != item.ToLine
		changed = append(changed, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate changed symbols: %w", err)
	}
	return changed, nil
}

func (s *Store) changedPackages(fromID, toID int64) ([]ChangedPackage, error) {
	fromMetrics, err := s.loadPackageDiffMetrics(fromID)
	if err != nil {
		return nil, err
	}
	toMetrics, err := s.loadPackageDiffMetrics(toID)
	if err != nil {
		return nil, err
	}

	keys := make(map[string]struct{}, len(fromMetrics)+len(toMetrics))
	for key := range fromMetrics {
		keys[key] = struct{}{}
	}
	for key := range toMetrics {
		keys[key] = struct{}{}
	}

	result := make([]ChangedPackage, 0, len(keys))
	for key := range keys {
		fromValue, fromOK := fromMetrics[key]
		toValue, toOK := toMetrics[key]

		item := ChangedPackage{ImportPath: key}
		switch {
		case !fromOK && toOK:
			item.Status = "added"
		case fromOK && !toOK:
			item.Status = "removed"
		default:
			item.Status = "changed"
		}
		if fromOK {
			item.FromFileCount = fromValue.FileCount
			item.FromSymbolCount = fromValue.SymbolCount
			item.FromTestCount = fromValue.TestCount
			item.FromLocalDepCount = fromValue.LocalDepCount
			item.FromReverseDepCount = fromValue.ReverseDepCount
		}
		if toOK {
			item.ToFileCount = toValue.FileCount
			item.ToSymbolCount = toValue.SymbolCount
			item.ToTestCount = toValue.TestCount
			item.ToLocalDepCount = toValue.LocalDepCount
			item.ToReverseDepCount = toValue.ReverseDepCount
		}

		if item.Status == "changed" &&
			item.FromFileCount == item.ToFileCount &&
			item.FromSymbolCount == item.ToSymbolCount &&
			item.FromTestCount == item.ToTestCount &&
			item.FromLocalDepCount == item.ToLocalDepCount &&
			item.FromReverseDepCount == item.ToReverseDepCount {
			continue
		}
		result = append(result, item)
	}

	sortChangedPackages(result)
	return result, nil
}

func buildImpactedSymbols(diff DiffView) []SymbolImpactDelta {
	byQName := make(map[string]*SymbolImpactDelta)
	ensure := func(qname, pkg, file string) *SymbolImpactDelta {
		qname = strings.TrimSpace(qname)
		if qname == "" {
			return nil
		}
		item, ok := byQName[qname]
		if !ok {
			item = &SymbolImpactDelta{
				QName:             qname,
				PackageImportPath: strings.TrimSpace(pkg),
				FilePath:          strings.TrimSpace(file),
			}
			byQName[qname] = item
		}
		if item.PackageImportPath == "" {
			item.PackageImportPath = strings.TrimSpace(pkg)
		}
		if item.FilePath == "" {
			item.FilePath = strings.TrimSpace(file)
		}
		return item
	}
	addWhy := func(item *SymbolImpactDelta, why string) {
		if item == nil {
			return
		}
		why = strings.TrimSpace(why)
		if why == "" {
			return
		}
		for _, existing := range item.Why {
			if existing == why {
				return
			}
		}
		item.Why = append(item.Why, why)
	}

	for _, symbol := range diff.AddedSymbols {
		item := ensure(symbol.QName, symbol.PackageImportPath, symbol.FilePath)
		if item == nil {
			continue
		}
		item.Status = "added"
		addWhy(item, "new symbol introduced")
	}
	for _, symbol := range diff.RemovedSymbols {
		item := ensure(symbol.QName, symbol.PackageImportPath, symbol.FilePath)
		if item == nil {
			continue
		}
		item.Status = "removed"
		addWhy(item, "symbol removed")
	}
	for _, symbol := range diff.ChangedSymbols {
		item := ensure(symbol.QName, symbol.ToPackageImportPath, symbol.ToFilePath)
		if item == nil {
			item = ensure(symbol.QName, symbol.FromPackageImportPath, symbol.FromFilePath)
		}
		if item == nil {
			continue
		}
		if item.Status == "" {
			item.Status = "changed"
		}
		item.ContractChanged = item.ContractChanged || symbol.ContractChanged
		item.Moved = item.Moved || symbol.Moved
		if symbol.ContractChanged {
			addWhy(item, "signature changed")
		}
		if symbol.Moved {
			addWhy(item, "declaration moved")
		}
	}
	for _, call := range diff.AddedCalls {
		if item := ensure(call.CallerQName, "", call.FilePath); item != nil {
			item.AddedCallees++
			addWhy(item, "new outgoing call edge")
		}
		if item := ensure(call.CalleeQName, "", call.FilePath); item != nil {
			item.AddedCallers++
			addWhy(item, "new incoming caller")
		}
	}
	for _, call := range diff.RemovedCalls {
		if item := ensure(call.CallerQName, "", call.FilePath); item != nil {
			item.RemovedCallees++
			addWhy(item, "outgoing call edge removed")
		}
		if item := ensure(call.CalleeQName, "", call.FilePath); item != nil {
			item.RemovedCallers++
			addWhy(item, "incoming caller removed")
		}
	}
	for _, ref := range diff.AddedRefs {
		if item := ensure(ref.ToQName, "", ref.FilePath); item != nil {
			item.AddedRefsIn++
			addWhy(item, "new inbound reference")
		}
		if item := ensure(ref.FromQName, ref.FromPackageImportPath, ref.FilePath); item != nil {
			item.AddedRefsOut++
			addWhy(item, "new outbound reference")
		}
	}
	for _, ref := range diff.RemovedRefs {
		if item := ensure(ref.ToQName, "", ref.FilePath); item != nil {
			item.RemovedRefsIn++
			addWhy(item, "inbound reference removed")
		}
		if item := ensure(ref.FromQName, ref.FromPackageImportPath, ref.FilePath); item != nil {
			item.RemovedRefsOut++
			addWhy(item, "outbound reference removed")
		}
	}
	for _, link := range diff.AddedTestLinks {
		if item := ensure(link.SymbolQName, "", ""); item != nil {
			item.AddedTests++
			addWhy(item, "new related test link")
		}
	}
	for _, link := range diff.RemovedTestLinks {
		if item := ensure(link.SymbolQName, "", ""); item != nil {
			item.RemovedTests++
			addWhy(item, "related test link removed")
		}
	}

	items := make([]SymbolImpactDelta, 0, len(byQName))
	for _, item := range byQName {
		item.BlastRadius = item.AddedCallers + item.RemovedCallers + item.AddedCallees + item.RemovedCallees +
			item.AddedRefsIn + item.RemovedRefsIn + item.AddedRefsOut + item.RemovedRefsOut +
			item.AddedTests + item.RemovedTests
		items = append(items, *item)
	}
	sort.Slice(items, func(i, j int) bool {
		if symbolDeltaScore(items[i]) != symbolDeltaScore(items[j]) {
			return symbolDeltaScore(items[i]) > symbolDeltaScore(items[j])
		}
		return items[i].QName < items[j].QName
	})
	return items
}

func symbolDeltaScore(item SymbolImpactDelta) int {
	score := item.BlastRadius
	switch item.Status {
	case "added", "removed":
		score += 12
	case "changed":
		score += 8
	}
	if item.ContractChanged {
		score += 8
	}
	if item.Moved {
		score += 4
	}
	return score
}

func sortChangedPackages(values []ChangedPackage) {
	if len(values) < 2 {
		return
	}
	for i := 0; i < len(values)-1; i++ {
		for j := i + 1; j < len(values); j++ {
			if values[j].ImportPath < values[i].ImportPath {
				values[i], values[j] = values[j], values[i]
			}
		}
	}
}

func (s *Store) loadPackageDiffMetrics(snapshotID int64) (map[string]packageDiffMetrics, error) {
	rows, err := s.db.Query(`
		SELECT p.import_path,
		       p.file_count,
		       (SELECT COUNT(*) FROM symbols s WHERE s.snapshot_id = p.snapshot_id AND s.package_import_path = p.import_path),
		       (SELECT COUNT(*) FROM tests t WHERE t.snapshot_id = p.snapshot_id AND t.package_import_path = p.import_path),
		       (SELECT COUNT(DISTINCT pd.to_package_import_path) FROM package_deps pd WHERE pd.snapshot_id = p.snapshot_id AND pd.from_package_import_path = p.import_path AND pd.is_local = 1),
		       (SELECT COUNT(DISTINCT pd.from_package_import_path) FROM package_deps pd WHERE pd.snapshot_id = p.snapshot_id AND pd.to_package_import_path = p.import_path AND pd.is_local = 1)
		FROM packages p
		WHERE p.snapshot_id = ?
	`, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("query package diff metrics: %w", err)
	}
	defer rows.Close()

	metrics := make(map[string]packageDiffMetrics)
	for rows.Next() {
		var item packageDiffMetrics
		if err := rows.Scan(
			&item.ImportPath,
			&item.FileCount,
			&item.SymbolCount,
			&item.TestCount,
			&item.LocalDepCount,
			&item.ReverseDepCount,
		); err != nil {
			return nil, fmt.Errorf("scan package diff metrics: %w", err)
		}
		metrics[item.ImportPath] = item
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate package diff metrics: %w", err)
	}
	return metrics, nil
}

func (s *Store) diffCallEdges(fromID, toID int64, mode string) ([]CallEdgeChange, error) {
	var query string
	var args []any
	switch mode {
	case "added":
		query = `
			SELECT c2.caller_symbol_key, COALESCE(caller.qname, c2.caller_symbol_key),
			       c2.callee_symbol_key, COALESCE(callee.qname, c2.callee_symbol_key),
			       c2.file_path, c2.line, c2.dispatch
			FROM call_edges c2
			LEFT JOIN call_edges c1
				ON c1.snapshot_id = ?
				AND c1.caller_symbol_key = c2.caller_symbol_key
				AND c1.callee_symbol_key = c2.callee_symbol_key
				AND c1.file_path = c2.file_path
				AND c1.line = c2.line
				AND c1.col = c2.col
			LEFT JOIN symbols caller ON caller.snapshot_id = ? AND caller.symbol_key = c2.caller_symbol_key
			LEFT JOIN symbols callee ON callee.snapshot_id = ? AND callee.symbol_key = c2.callee_symbol_key
			WHERE c2.snapshot_id = ? AND c1.caller_symbol_key IS NULL
			ORDER BY 2, 4, c2.file_path, c2.line
		`
		args = []any{fromID, toID, toID, toID}
	case "removed":
		query = `
			SELECT c1.caller_symbol_key, COALESCE(caller.qname, c1.caller_symbol_key),
			       c1.callee_symbol_key, COALESCE(callee.qname, c1.callee_symbol_key),
			       c1.file_path, c1.line, c1.dispatch
			FROM call_edges c1
			LEFT JOIN call_edges c2
				ON c2.snapshot_id = ?
				AND c2.caller_symbol_key = c1.caller_symbol_key
				AND c2.callee_symbol_key = c1.callee_symbol_key
				AND c2.file_path = c1.file_path
				AND c2.line = c1.line
				AND c2.col = c1.col
			LEFT JOIN symbols caller ON caller.snapshot_id = ? AND caller.symbol_key = c1.caller_symbol_key
			LEFT JOIN symbols callee ON callee.snapshot_id = ? AND callee.symbol_key = c1.callee_symbol_key
			WHERE c1.snapshot_id = ? AND c2.caller_symbol_key IS NULL
			ORDER BY 2, 4, c1.file_path, c1.line
		`
		args = []any{toID, fromID, fromID, fromID}
	default:
		return nil, fmt.Errorf("unsupported call diff mode %q", mode)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query call diff: %w", err)
	}
	defer rows.Close()

	var changes []CallEdgeChange
	for rows.Next() {
		var item CallEdgeChange
		if err := rows.Scan(
			&item.CallerSymbolKey,
			&item.CallerQName,
			&item.CalleeSymbolKey,
			&item.CalleeQName,
			&item.FilePath,
			&item.Line,
			&item.Dispatch,
		); err != nil {
			return nil, fmt.Errorf("scan call diff: %w", err)
		}
		changes = append(changes, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate call diff: %w", err)
	}
	return changes, nil
}

func (s *Store) diffRefs(fromID, toID int64, mode string) ([]RefChange, error) {
	var query string
	var args []any
	switch mode {
	case "added":
		query = `
			SELECT r2.from_package_import_path, r2.from_symbol_key, COALESCE(src.qname, ''),
			       r2.to_symbol_key, COALESCE(dst.qname, r2.to_symbol_key),
			       r2.file_path, r2.line, r2.kind
			FROM refs r2
			LEFT JOIN refs r1
				ON r1.snapshot_id = ?
				AND r1.from_package_import_path = r2.from_package_import_path
				AND r1.from_symbol_key = r2.from_symbol_key
				AND r1.to_symbol_key = r2.to_symbol_key
				AND r1.file_path = r2.file_path
				AND r1.line = r2.line
				AND r1.col = r2.col
				AND r1.kind = r2.kind
			LEFT JOIN symbols src ON src.snapshot_id = ? AND src.symbol_key = r2.from_symbol_key
			LEFT JOIN symbols dst ON dst.snapshot_id = ? AND dst.symbol_key = r2.to_symbol_key
			WHERE r2.snapshot_id = ? AND r1.to_symbol_key IS NULL
			ORDER BY 5, r2.file_path, r2.line
		`
		args = []any{fromID, toID, toID, toID}
	case "removed":
		query = `
			SELECT r1.from_package_import_path, r1.from_symbol_key, COALESCE(src.qname, ''),
			       r1.to_symbol_key, COALESCE(dst.qname, r1.to_symbol_key),
			       r1.file_path, r1.line, r1.kind
			FROM refs r1
			LEFT JOIN refs r2
				ON r2.snapshot_id = ?
				AND r2.from_package_import_path = r1.from_package_import_path
				AND r2.from_symbol_key = r1.from_symbol_key
				AND r2.to_symbol_key = r1.to_symbol_key
				AND r2.file_path = r1.file_path
				AND r2.line = r1.line
				AND r2.col = r1.col
				AND r2.kind = r1.kind
			LEFT JOIN symbols src ON src.snapshot_id = ? AND src.symbol_key = r1.from_symbol_key
			LEFT JOIN symbols dst ON dst.snapshot_id = ? AND dst.symbol_key = r1.to_symbol_key
			WHERE r1.snapshot_id = ? AND r2.to_symbol_key IS NULL
			ORDER BY 5, r1.file_path, r1.line
		`
		args = []any{toID, fromID, fromID, fromID}
	default:
		return nil, fmt.Errorf("unsupported ref diff mode %q", mode)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query ref diff: %w", err)
	}
	defer rows.Close()

	var changes []RefChange
	for rows.Next() {
		var item RefChange
		if err := rows.Scan(
			&item.FromPackageImportPath,
			&item.FromSymbolKey,
			&item.FromQName,
			&item.ToSymbolKey,
			&item.ToQName,
			&item.FilePath,
			&item.Line,
			&item.Kind,
		); err != nil {
			return nil, fmt.Errorf("scan ref diff: %w", err)
		}
		changes = append(changes, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ref diff: %w", err)
	}
	return changes, nil
}

func (s *Store) diffTestLinks(fromID, toID int64, mode string) ([]TestLinkChange, error) {
	var query string
	var args []any
	switch mode {
	case "added":
		query = `
			SELECT tl2.test_key, tl2.test_package_import_path, COALESCE(t.name, tl2.test_key),
			       tl2.symbol_key, COALESCE(sym.qname, tl2.symbol_key), tl2.link_kind, tl2.confidence
			FROM test_links tl2
			LEFT JOIN test_links tl1
				ON tl1.snapshot_id = ?
				AND tl1.test_key = tl2.test_key
				AND tl1.symbol_key = tl2.symbol_key
				AND tl1.link_kind = tl2.link_kind
				AND tl1.confidence = tl2.confidence
			LEFT JOIN tests t ON t.snapshot_id = ? AND t.test_key = tl2.test_key
			LEFT JOIN symbols sym ON sym.snapshot_id = ? AND sym.symbol_key = tl2.symbol_key
			WHERE tl2.snapshot_id = ? AND tl1.test_key IS NULL
			ORDER BY tl2.test_key, 5, tl2.link_kind
		`
		args = []any{fromID, toID, toID, toID}
	case "removed":
		query = `
			SELECT tl1.test_key, tl1.test_package_import_path, COALESCE(t.name, tl1.test_key),
			       tl1.symbol_key, COALESCE(sym.qname, tl1.symbol_key), tl1.link_kind, tl1.confidence
			FROM test_links tl1
			LEFT JOIN test_links tl2
				ON tl2.snapshot_id = ?
				AND tl2.test_key = tl1.test_key
				AND tl2.symbol_key = tl1.symbol_key
				AND tl2.link_kind = tl1.link_kind
				AND tl2.confidence = tl1.confidence
			LEFT JOIN tests t ON t.snapshot_id = ? AND t.test_key = tl1.test_key
			LEFT JOIN symbols sym ON sym.snapshot_id = ? AND sym.symbol_key = tl1.symbol_key
			WHERE tl1.snapshot_id = ? AND tl2.test_key IS NULL
			ORDER BY tl1.test_key, 5, tl1.link_kind
		`
		args = []any{toID, fromID, fromID, fromID}
	default:
		return nil, fmt.Errorf("unsupported test link diff mode %q", mode)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query test link diff: %w", err)
	}
	defer rows.Close()

	var changes []TestLinkChange
	for rows.Next() {
		var item TestLinkChange
		if err := rows.Scan(
			&item.TestKey,
			&item.TestPackageImportPath,
			&item.TestName,
			&item.SymbolKey,
			&item.SymbolQName,
			&item.LinkKind,
			&item.Confidence,
		); err != nil {
			return nil, fmt.Errorf("scan test link diff: %w", err)
		}
		changes = append(changes, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate test link diff: %w", err)
	}
	return changes, nil
}

func (s *Store) diffPackageDeps(fromID, toID int64, mode string) ([]PackageDepChange, error) {
	var query string
	switch mode {
	case "added":
		query = `
			SELECT p2.from_package_import_path, p2.to_package_import_path
			FROM package_deps p2
			LEFT JOIN package_deps p1
				ON p1.snapshot_id = ?
				AND p1.from_package_import_path = p2.from_package_import_path
				AND p1.to_package_import_path = p2.to_package_import_path
			WHERE p2.snapshot_id = ? AND p2.is_local = 1 AND p1.from_package_import_path IS NULL
			ORDER BY p2.from_package_import_path, p2.to_package_import_path
		`
	case "removed":
		query = `
			SELECT p1.from_package_import_path, p1.to_package_import_path
			FROM package_deps p1
			LEFT JOIN package_deps p2
				ON p2.snapshot_id = ?
				AND p2.from_package_import_path = p1.from_package_import_path
				AND p2.to_package_import_path = p1.to_package_import_path
			WHERE p1.snapshot_id = ? AND p1.is_local = 1 AND p2.from_package_import_path IS NULL
			ORDER BY p1.from_package_import_path, p1.to_package_import_path
		`
	default:
		return nil, fmt.Errorf("unsupported package dep diff mode %q", mode)
	}

	rows, err := s.db.Query(query, fromID, toID)
	if err != nil {
		return nil, fmt.Errorf("query package dep diff: %w", err)
	}
	defer rows.Close()

	var changes []PackageDepChange
	for rows.Next() {
		var item PackageDepChange
		if err := rows.Scan(&item.FromPackageImportPath, &item.ToPackageImportPath); err != nil {
			return nil, fmt.Errorf("scan package dep diff: %w", err)
		}
		changes = append(changes, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate package dep diff: %w", err)
	}
	return changes, nil
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
