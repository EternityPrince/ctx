package storage

import (
	"database/sql"
	"fmt"
	"sort"
)

func (s *Store) SymbolHistory(symbolKey string, limit int) (SymbolHistoryView, error) {
	current, ok, err := s.CurrentSnapshot()
	if err != nil {
		return SymbolHistoryView{}, err
	}
	if !ok {
		return SymbolHistoryView{}, fmt.Errorf("no snapshots available")
	}

	symbol, ok, err := s.loadSymbolByKeyAtSnapshot(current.ID, symbolKey)
	if err != nil {
		return SymbolHistoryView{}, err
	}
	if !ok {
		return SymbolHistoryView{}, fmt.Errorf("symbol %q not found in current snapshot", symbolKey)
	}

	snapshots, err := s.listSnapshotsAscending()
	if err != nil {
		return SymbolHistoryView{}, err
	}

	view := SymbolHistoryView{Symbol: symbol}
	if view.IntroducedIn, ok, err = s.firstSnapshotForSymbol(symbol.QName, snapshots); err != nil {
		return SymbolHistoryView{}, err
	}
	if !ok {
		return SymbolHistoryView{}, fmt.Errorf("symbol %q has no snapshot history", symbol.QName)
	}

	events, err := s.symbolHistoryEvents(symbol.QName, snapshots)
	if err != nil {
		return SymbolHistoryView{}, err
	}
	if !hasIntroducedEvent(events) {
		events = append(events, SymbolHistoryEvent{
			FromSnapshotID: 0,
			ToSnapshot:     view.IntroducedIn,
			Status:         "introduced",
		})
	}
	sortSymbolHistoryEvents(events)
	if len(events) > 0 {
		view.LastChangedIn = events[0].ToSnapshot
	} else {
		view.LastChangedIn = view.IntroducedIn
	}
	if limit > 0 && len(events) > limit {
		events = events[:limit]
	}
	view.Events = events
	return view, nil
}

func (s *Store) PackageHistory(importPath string, limit int) (PackageHistoryView, error) {
	current, ok, err := s.CurrentSnapshot()
	if err != nil {
		return PackageHistoryView{}, err
	}
	if !ok {
		return PackageHistoryView{}, fmt.Errorf("no snapshots available")
	}

	pkg, ok, err := s.loadPackageSummaryAtSnapshot(current.ID, importPath)
	if err != nil {
		return PackageHistoryView{}, err
	}
	if !ok {
		return PackageHistoryView{}, fmt.Errorf("package %q not found in current snapshot", importPath)
	}

	snapshots, err := s.listSnapshotsAscending()
	if err != nil {
		return PackageHistoryView{}, err
	}

	view := PackageHistoryView{Package: pkg}
	if view.IntroducedIn, ok, err = s.firstSnapshotForPackage(importPath, snapshots); err != nil {
		return PackageHistoryView{}, err
	}
	if !ok {
		return PackageHistoryView{}, fmt.Errorf("package %q has no snapshot history", importPath)
	}

	events, err := s.packageHistoryEvents(importPath, snapshots)
	if err != nil {
		return PackageHistoryView{}, err
	}
	if !hasPackageIntroducedEvent(events) {
		events = append(events, PackageHistoryEvent{
			FromSnapshotID: 0,
			ToSnapshot:     view.IntroducedIn,
			Status:         "introduced",
		})
	}
	sortPackageHistoryEvents(events)
	if len(events) > 0 {
		view.LastChangedIn = events[0].ToSnapshot
	} else {
		view.LastChangedIn = view.IntroducedIn
	}
	if limit > 0 && len(events) > limit {
		events = events[:limit]
	}
	view.Events = events
	return view, nil
}

func (s *Store) SymbolCoChange(symbolKey string, limit int) (CoChangeView, error) {
	current, ok, err := s.CurrentSnapshot()
	if err != nil {
		return CoChangeView{}, err
	}
	if !ok {
		return CoChangeView{}, fmt.Errorf("no snapshots available")
	}

	symbol, ok, err := s.loadSymbolByKeyAtSnapshot(current.ID, symbolKey)
	if err != nil {
		return CoChangeView{}, err
	}
	if !ok {
		return CoChangeView{}, fmt.Errorf("symbol %q not found in current snapshot", symbolKey)
	}

	view := CoChangeView{
		Scope:         "symbol",
		Anchor:        symbol.QName,
		AnchorFile:    symbol.FilePath,
		AnchorPackage: symbol.PackageImportPath,
	}
	snapshots, err := s.listSnapshotsAscending()
	if err != nil {
		return CoChangeView{}, err
	}
	fileCounts, packageCounts, anchorChanges, err := s.coChangeCountsForSymbol(symbol.QName, symbol.FilePath, symbol.PackageImportPath, snapshots)
	if err != nil {
		return CoChangeView{}, err
	}
	view.AnchorChangeCount = anchorChanges
	view.Files = topCoChangeItems(fileCounts, anchorChanges, limit)
	view.Packages = topCoChangeItems(packageCounts, anchorChanges, limit)
	return view, nil
}

func (s *Store) PackageCoChange(importPath string, limit int) (CoChangeView, error) {
	current, ok, err := s.CurrentSnapshot()
	if err != nil {
		return CoChangeView{}, err
	}
	if !ok {
		return CoChangeView{}, fmt.Errorf("no snapshots available")
	}

	if _, ok, err := s.loadPackageSummaryAtSnapshot(current.ID, importPath); err != nil {
		return CoChangeView{}, err
	} else if !ok {
		return CoChangeView{}, fmt.Errorf("package %q not found in current snapshot", importPath)
	}

	view := CoChangeView{
		Scope:         "package",
		Anchor:        importPath,
		AnchorPackage: importPath,
	}
	snapshots, err := s.listSnapshotsAscending()
	if err != nil {
		return CoChangeView{}, err
	}
	fileCounts, packageCounts, anchorChanges, err := s.coChangeCountsForPackage(importPath, snapshots)
	if err != nil {
		return CoChangeView{}, err
	}
	view.AnchorChangeCount = anchorChanges
	view.Files = topCoChangeItems(fileCounts, anchorChanges, limit)
	view.Packages = topCoChangeItems(packageCounts, anchorChanges, limit)
	return view, nil
}

func (s *Store) listSnapshotsAscending() ([]SnapshotInfo, error) {
	snapshots, err := s.ListSnapshots()
	if err != nil {
		return nil, err
	}
	for left, right := 0, len(snapshots)-1; left < right; left, right = left+1, right-1 {
		snapshots[left], snapshots[right] = snapshots[right], snapshots[left]
	}
	return snapshots, nil
}

func (s *Store) loadSymbolByKeyAtSnapshot(snapshotID int64, symbolKey string) (SymbolMatch, bool, error) {
	var symbol SymbolMatch
	err := s.db.QueryRow(`
		SELECT symbol_key, qname, package_import_path, file_path, name, kind, receiver, signature, doc, line, col
		FROM symbols
		WHERE snapshot_id = ? AND symbol_key = ?
	`, snapshotID, symbolKey).Scan(symbolMatchScanDest(&symbol)...)
	if err == sql.ErrNoRows {
		return SymbolMatch{}, false, nil
	}
	if err != nil {
		return SymbolMatch{}, false, fmt.Errorf("load symbol by key: %w", err)
	}
	return symbol, true, nil
}

func (s *Store) loadSymbolByQNameAtSnapshot(snapshotID int64, qname string) (SymbolMatch, bool, error) {
	var symbol SymbolMatch
	err := s.db.QueryRow(`
		SELECT symbol_key, qname, package_import_path, file_path, name, kind, receiver, signature, doc, line, col
		FROM symbols
		WHERE snapshot_id = ? AND qname = ?
	`, snapshotID, qname).Scan(symbolMatchScanDest(&symbol)...)
	if err == sql.ErrNoRows {
		return SymbolMatch{}, false, nil
	}
	if err != nil {
		return SymbolMatch{}, false, fmt.Errorf("load symbol by qname: %w", err)
	}
	return symbol, true, nil
}

func (s *Store) loadPackageSummaryAtSnapshot(snapshotID int64, importPath string) (PackageSummary, bool, error) {
	var exists int
	if err := s.db.QueryRow(`
		SELECT COUNT(*)
		FROM packages
		WHERE snapshot_id = ? AND import_path = ?
	`, snapshotID, importPath).Scan(&exists); err != nil {
		return PackageSummary{}, false, fmt.Errorf("check package existence: %w", err)
	}
	if exists == 0 {
		return PackageSummary{}, false, nil
	}
	summary, err := s.loadPackageSummary(snapshotID, importPath)
	if err != nil {
		return PackageSummary{}, false, err
	}
	return summary, true, nil
}

func (s *Store) firstSnapshotForSymbol(qname string, snapshots []SnapshotInfo) (SnapshotInfo, bool, error) {
	for _, snapshot := range snapshots {
		if _, ok, err := s.loadSymbolByQNameAtSnapshot(snapshot.ID, qname); err != nil {
			return SnapshotInfo{}, false, err
		} else if ok {
			return snapshot, true, nil
		}
	}
	return SnapshotInfo{}, false, nil
}

func (s *Store) firstSnapshotForPackage(importPath string, snapshots []SnapshotInfo) (SnapshotInfo, bool, error) {
	for _, snapshot := range snapshots {
		if _, ok, err := s.loadPackageSummaryAtSnapshot(snapshot.ID, importPath); err != nil {
			return SnapshotInfo{}, false, err
		} else if ok {
			return snapshot, true, nil
		}
	}
	return SnapshotInfo{}, false, nil
}

func (s *Store) symbolHistoryEvents(qname string, snapshots []SnapshotInfo) ([]SymbolHistoryEvent, error) {
	events := make([]SymbolHistoryEvent, 0, len(snapshots))
	for idx := 1; idx < len(snapshots); idx++ {
		diff, err := s.Diff(snapshots[idx-1].ID, snapshots[idx].ID)
		if err != nil {
			return nil, err
		}
		if event, ok := symbolHistoryEventFromDiff(qname, diff, snapshots[idx]); ok {
			events = append(events, event)
		}
	}
	return events, nil
}

func (s *Store) packageHistoryEvents(importPath string, snapshots []SnapshotInfo) ([]PackageHistoryEvent, error) {
	events := make([]PackageHistoryEvent, 0, len(snapshots))
	for idx := 1; idx < len(snapshots); idx++ {
		diff, err := s.Diff(snapshots[idx-1].ID, snapshots[idx].ID)
		if err != nil {
			return nil, err
		}
		if event, ok := packageHistoryEventFromDiff(importPath, diff, snapshots[idx]); ok {
			events = append(events, event)
		}
	}
	return events, nil
}

func symbolHistoryEventFromDiff(qname string, diff DiffView, toSnapshot SnapshotInfo) (SymbolHistoryEvent, bool) {
	event := SymbolHistoryEvent{
		FromSnapshotID: diff.FromSnapshotID,
		ToSnapshot:     toSnapshot,
	}

	for _, symbol := range diff.AddedSymbols {
		if symbol.QName == qname {
			event.Status = "introduced"
			break
		}
	}
	for _, symbol := range diff.RemovedSymbols {
		if symbol.QName == qname {
			event.Status = "removed"
			break
		}
	}
	for _, symbol := range diff.ChangedSymbols {
		if symbol.QName != qname {
			continue
		}
		event.ContractChanged = event.ContractChanged || symbol.ContractChanged
		event.Moved = event.Moved || symbol.Moved
		if event.Status == "" {
			event.Status = "changed"
		}
	}
	for _, edge := range diff.AddedCalls {
		if edge.CallerQName == qname || edge.CalleeQName == qname {
			event.AddedCalls++
		}
	}
	for _, edge := range diff.RemovedCalls {
		if edge.CallerQName == qname || edge.CalleeQName == qname {
			event.RemovedCalls++
		}
	}
	for _, ref := range diff.AddedRefs {
		if ref.FromQName == qname || ref.ToQName == qname {
			event.AddedRefs++
		}
	}
	for _, ref := range diff.RemovedRefs {
		if ref.FromQName == qname || ref.ToQName == qname {
			event.RemovedRefs++
		}
	}
	for _, link := range diff.AddedTestLinks {
		if link.SymbolQName == qname {
			event.AddedTests++
		}
	}
	for _, link := range diff.RemovedTestLinks {
		if link.SymbolQName == qname {
			event.RemovedTests++
		}
	}

	if event.Status == "" && (event.AddedCalls > 0 || event.RemovedCalls > 0 || event.AddedRefs > 0 || event.RemovedRefs > 0 || event.AddedTests > 0 || event.RemovedTests > 0) {
		event.Status = "changed"
	}
	if event.Status == "" {
		return SymbolHistoryEvent{}, false
	}
	return event, true
}

func packageHistoryEventFromDiff(importPath string, diff DiffView, toSnapshot SnapshotInfo) (PackageHistoryEvent, bool) {
	event := PackageHistoryEvent{
		FromSnapshotID: diff.FromSnapshotID,
		ToSnapshot:     toSnapshot,
	}

	for _, pkg := range diff.ChangedPackages {
		if pkg.ImportPath != importPath {
			continue
		}
		event.Status = pkg.Status
		event.FileDelta = pkg.ToFileCount - pkg.FromFileCount
		event.SymbolDelta = pkg.ToSymbolCount - pkg.FromSymbolCount
		event.TestDelta = pkg.ToTestCount - pkg.FromTestCount
		break
	}

	for _, symbol := range diff.ChangedSymbols {
		if symbol.FromPackageImportPath == importPath || symbol.ToPackageImportPath == importPath {
			if symbol.ContractChanged {
				event.ChangedContracts++
			}
			if symbol.Moved {
				event.MovedSymbols++
			}
			if event.Status == "" {
				event.Status = "changed"
			}
		}
	}
	for _, dep := range diff.AddedPackageDeps {
		if dep.FromPackageImportPath == importPath || dep.ToPackageImportPath == importPath {
			event.AddedDeps++
			if event.Status == "" {
				event.Status = "changed"
			}
		}
	}
	for _, dep := range diff.RemovedPackageDeps {
		if dep.FromPackageImportPath == importPath || dep.ToPackageImportPath == importPath {
			event.RemovedDeps++
			if event.Status == "" {
				event.Status = "changed"
			}
		}
	}

	if event.Status == "" {
		return PackageHistoryEvent{}, false
	}
	return event, true
}

func anchorSymbolFootprint(qname, anchorFile, anchorPackage string, diff DiffView) (map[string]struct{}, map[string]struct{}) {
	files := make(map[string]struct{}, 2)
	packages := make(map[string]struct{}, 2)
	if anchorFile != "" {
		files[anchorFile] = struct{}{}
	}
	if anchorPackage != "" {
		packages[anchorPackage] = struct{}{}
	}

	addSymbol := func(filePath, packageImportPath string) {
		if filePath != "" {
			files[filePath] = struct{}{}
		}
		if packageImportPath != "" {
			packages[packageImportPath] = struct{}{}
		}
	}

	for _, symbol := range diff.AddedSymbols {
		if symbol.QName == qname {
			addSymbol(symbol.FilePath, symbol.PackageImportPath)
		}
	}
	for _, symbol := range diff.RemovedSymbols {
		if symbol.QName == qname {
			addSymbol(symbol.FilePath, symbol.PackageImportPath)
		}
	}
	for _, symbol := range diff.ChangedSymbols {
		if symbol.QName != qname {
			continue
		}
		addSymbol(symbol.FromFilePath, symbol.FromPackageImportPath)
		addSymbol(symbol.ToFilePath, symbol.ToPackageImportPath)
	}
	return files, packages
}

func (s *Store) coChangeCountsForSymbol(qname, anchorFile, anchorPackage string, snapshots []SnapshotInfo) (map[string]int, map[string]int, int, error) {
	fileCounts := make(map[string]int)
	packageCounts := make(map[string]int)
	anchorChanges := 0
	filePackageCache := make(map[string]string)

	for idx := 1; idx < len(snapshots); idx++ {
		diff, err := s.Diff(snapshots[idx-1].ID, snapshots[idx].ID)
		if err != nil {
			return nil, nil, 0, err
		}
		if _, ok := symbolHistoryEventFromDiff(qname, diff, snapshots[idx]); !ok {
			continue
		}
		anchorChanges++
		anchorFiles, anchorPackages := anchorSymbolFootprint(qname, anchorFile, anchorPackage, diff)
		for _, path := range uniqueDiffFiles(diff) {
			if _, excluded := anchorFiles[path]; excluded {
				continue
			}
			fileCounts[path]++
		}
		packages, err := s.packagesForDiff(diff, filePackageCache)
		if err != nil {
			return nil, nil, 0, err
		}
		for pkg := range packages {
			if pkg == "" {
				continue
			}
			if _, excluded := anchorPackages[pkg]; excluded {
				continue
			}
			packageCounts[pkg]++
		}
	}
	return fileCounts, packageCounts, anchorChanges, nil
}

func (s *Store) coChangeCountsForPackage(importPath string, snapshots []SnapshotInfo) (map[string]int, map[string]int, int, error) {
	fileCounts := make(map[string]int)
	packageCounts := make(map[string]int)
	anchorChanges := 0
	filePackageCache := make(map[string]string)

	for idx := 1; idx < len(snapshots); idx++ {
		diff, err := s.Diff(snapshots[idx-1].ID, snapshots[idx].ID)
		if err != nil {
			return nil, nil, 0, err
		}
		if !packageAffectedByDiff(importPath, diff) {
			continue
		}
		anchorChanges++
		for _, path := range uniqueDiffFiles(diff) {
			pkg, err := s.packageForFileDiff(diff.FromSnapshotID, diff.ToSnapshotID, path, filePackageCache)
			if err != nil {
				return nil, nil, 0, err
			}
			if pkg == importPath {
				continue
			}
			fileCounts[path]++
		}
		packages, err := s.packagesForDiff(diff, filePackageCache)
		if err != nil {
			return nil, nil, 0, err
		}
		for pkg := range packages {
			if pkg == "" || pkg == importPath {
				continue
			}
			packageCounts[pkg]++
		}
	}
	return fileCounts, packageCounts, anchorChanges, nil
}

func packageAffectedByDiff(importPath string, diff DiffView) bool {
	for _, pkg := range diff.ChangedPackages {
		if pkg.ImportPath == importPath {
			return true
		}
	}
	for _, symbol := range diff.AddedSymbols {
		if symbol.PackageImportPath == importPath {
			return true
		}
	}
	for _, symbol := range diff.RemovedSymbols {
		if symbol.PackageImportPath == importPath {
			return true
		}
	}
	for _, symbol := range diff.ChangedSymbols {
		if symbol.FromPackageImportPath == importPath || symbol.ToPackageImportPath == importPath {
			return true
		}
	}
	for _, dep := range diff.AddedPackageDeps {
		if dep.FromPackageImportPath == importPath || dep.ToPackageImportPath == importPath {
			return true
		}
	}
	for _, dep := range diff.RemovedPackageDeps {
		if dep.FromPackageImportPath == importPath || dep.ToPackageImportPath == importPath {
			return true
		}
	}
	return false
}

func uniqueDiffFiles(diff DiffView) []string {
	seen := make(map[string]struct{})
	var result []string
	appendValues := func(values []string) {
		for _, value := range values {
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	appendValues(diff.AddedFiles)
	appendValues(diff.ChangedFiles)
	appendValues(diff.DeletedFiles)
	sort.Strings(result)
	return result
}

func (s *Store) packagesForDiff(diff DiffView, cache map[string]string) (map[string]struct{}, error) {
	packages := make(map[string]struct{})
	for _, pkg := range diff.ChangedPackages {
		packages[pkg.ImportPath] = struct{}{}
	}
	for _, symbol := range diff.AddedSymbols {
		packages[symbol.PackageImportPath] = struct{}{}
	}
	for _, symbol := range diff.RemovedSymbols {
		packages[symbol.PackageImportPath] = struct{}{}
	}
	for _, symbol := range diff.ChangedSymbols {
		if symbol.FromPackageImportPath != "" {
			packages[symbol.FromPackageImportPath] = struct{}{}
		}
		if symbol.ToPackageImportPath != "" {
			packages[symbol.ToPackageImportPath] = struct{}{}
		}
	}
	for _, dep := range diff.AddedPackageDeps {
		packages[dep.FromPackageImportPath] = struct{}{}
		packages[dep.ToPackageImportPath] = struct{}{}
	}
	for _, dep := range diff.RemovedPackageDeps {
		packages[dep.FromPackageImportPath] = struct{}{}
		packages[dep.ToPackageImportPath] = struct{}{}
	}
	for _, path := range uniqueDiffFiles(diff) {
		pkg, err := s.packageForFileDiff(diff.FromSnapshotID, diff.ToSnapshotID, path, cache)
		if err != nil {
			return nil, err
		}
		if pkg != "" {
			packages[pkg] = struct{}{}
		}
	}
	return packages, nil
}

func (s *Store) packageForFileDiff(fromID, toID int64, filePath string, cache map[string]string) (string, error) {
	if value, ok := cache[filePath]; ok {
		return value, nil
	}

	var pkg string
	err := s.db.QueryRow(`
		SELECT package_import_path
		FROM files
		WHERE rel_path = ? AND snapshot_id IN (?, ?)
		ORDER BY CASE snapshot_id WHEN ? THEN 0 ELSE 1 END
		LIMIT 1
	`, filePath, toID, fromID, toID).Scan(&pkg)
	if err == sql.ErrNoRows {
		cache[filePath] = ""
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("resolve package for file diff: %w", err)
	}
	cache[filePath] = pkg
	return pkg, nil
}

func topCoChangeItems(counts map[string]int, anchorChanges, limit int) []CoChangeItem {
	if limit <= 0 {
		limit = 8
	}
	items := make([]CoChangeItem, 0, len(counts))
	for label, count := range counts {
		frequency := 0.0
		if anchorChanges > 0 {
			frequency = float64(count) / float64(anchorChanges)
		}
		items = append(items, CoChangeItem{
			Label:     label,
			Count:     count,
			Frequency: frequency,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count != items[j].Count {
			return items[i].Count > items[j].Count
		}
		return items[i].Label < items[j].Label
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items
}

func hasIntroducedEvent(events []SymbolHistoryEvent) bool {
	for _, event := range events {
		if event.Status == "introduced" {
			return true
		}
	}
	return false
}

func hasPackageIntroducedEvent(events []PackageHistoryEvent) bool {
	for _, event := range events {
		if event.Status == "introduced" {
			return true
		}
	}
	return false
}

func sortSymbolHistoryEvents(events []SymbolHistoryEvent) {
	sort.Slice(events, func(i, j int) bool {
		if events[i].ToSnapshot.ID != events[j].ToSnapshot.ID {
			return events[i].ToSnapshot.ID > events[j].ToSnapshot.ID
		}
		return events[i].Status < events[j].Status
	})
}

func sortPackageHistoryEvents(events []PackageHistoryEvent) {
	sort.Slice(events, func(i, j int) bool {
		if events[i].ToSnapshot.ID != events[j].ToSnapshot.ID {
			return events[i].ToSnapshot.ID > events[j].ToSnapshot.ID
		}
		return events[i].Status < events[j].Status
	})
}
