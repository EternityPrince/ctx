package storage

import (
	"strings"
	"testing"
)

func TestDiffIncludesSemanticChangesAndRelationships(t *testing.T) {
	store := openTestStore(t)
	commitReportSnapshot(t, store, reportFixtureV1(), true)
	from := commitReportSnapshot(t, store, reportFixtureV2(), false)
	to := commitReportSnapshot(t, store, reportFixtureV3(), false)

	diff, err := store.Diff(from.ID, to.ID)
	if err != nil {
		t.Fatalf("Diff returned error: %v", err)
	}
	if len(diff.AddedFiles) != 1 || diff.AddedFiles[0] != "pkg/runtime.go" {
		t.Fatalf("unexpected added files: %+v", diff.AddedFiles)
	}

	runChange := findChangedSymbol(diff.ChangedSymbols, "example.com/project/pkg.Service.Run")
	if runChange == nil {
		t.Fatalf("expected changed symbol for Service.Run, got %+v", diff.ChangedSymbols)
	}
	if !runChange.ContractChanged || !runChange.Moved {
		t.Fatalf("expected contract and move change for Service.Run, got %+v", runChange)
	}
	if runChange.FromFilePath != "pkg/service.go" || runChange.ToFilePath != "pkg/runtime.go" {
		t.Fatalf("unexpected move details: %+v", runChange)
	}

	pkgChange := findChangedPackage(diff.ChangedPackages, "example.com/project/pkg")
	if pkgChange == nil {
		t.Fatalf("expected changed package for pkg, got %+v", diff.ChangedPackages)
	}
	if pkgChange.FromFileCount != 2 || pkgChange.ToFileCount != 3 {
		t.Fatalf("unexpected package file counts: %+v", pkgChange)
	}

	if !hasCall(diff.AddedCalls, "example.com/project/api.Handle", "example.com/project/pkg.helper") {
		t.Fatalf("expected added call Handle -> helper, got %+v", diff.AddedCalls)
	}
	if !hasCall(diff.RemovedCalls, "example.com/project/api.Handle", "example.com/project/pkg.Service.Run") {
		t.Fatalf("expected removed call Handle -> Run, got %+v", diff.RemovedCalls)
	}
	if !hasRef(diff.AddedRefs, "example.com/project/pkg.helper") {
		t.Fatalf("expected added ref to helper, got %+v", diff.AddedRefs)
	}
	if !hasTestLink(diff.AddedTestLinks, "example.com/project/pkg:TestHelper", "example.com/project/pkg.helper") {
		t.Fatalf("expected added test link to helper, got %+v", diff.AddedTestLinks)
	}
	if !hasTestLink(diff.RemovedTestLinks, "example.com/project/pkg:TestServiceRun", "example.com/project/pkg.Service") {
		t.Fatalf("expected removed Service test link, got %+v", diff.RemovedTestLinks)
	}
	if !hasPackageDep(diff.AddedPackageDeps, "example.com/project/pkg", "example.com/project/api") {
		t.Fatalf("expected added package dependency pkg -> api, got %+v", diff.AddedPackageDeps)
	}
	runImpact := lookupSymbolImpactDelta(diff.ImpactedSymbols, "example.com/project/pkg.Service.Run")
	if runImpact == nil {
		t.Fatalf("expected impacted symbol for Service.Run, got %+v", diff.ImpactedSymbols)
	}
	if !runImpact.ContractChanged || !runImpact.Moved || runImpact.RemovedCallers == 0 {
		t.Fatalf("expected symbol-aware impact for Service.Run, got %+v", runImpact)
	}
	helperImpact := lookupSymbolImpactDelta(diff.ImpactedSymbols, "example.com/project/pkg.helper")
	if helperImpact == nil || helperImpact.AddedTests == 0 || helperImpact.BlastRadius == 0 {
		t.Fatalf("expected helper symbol to show added/test-linked impact, got %+v", helperImpact)
	}
}

func TestImpactViewIncludesRecentSymbolDelta(t *testing.T) {
	store := openTestStore(t)
	commitReportSnapshot(t, store, reportFixtureV1(), true)
	commitReportSnapshot(t, store, reportFixtureV2(), false)
	commitReportSnapshot(t, store, reportFixtureV3(), false)

	view, err := store.LoadImpactView("example.com/project/pkg.Service.Run", 3)
	if err != nil {
		t.Fatalf("LoadImpactView returned error: %v", err)
	}
	if !view.HasRecentDelta {
		t.Fatalf("expected recent delta on impact view, got %+v", view)
	}
	if view.RecentDelta.QName != "example.com/project/pkg.Service.Run" || !view.RecentDelta.ContractChanged || !view.RecentDelta.Moved {
		t.Fatalf("unexpected recent delta: %+v", view.RecentDelta)
	}
}

func TestImpactViewIncludesBlastRadiusAndEmpiricalSignals(t *testing.T) {
	store := openTestStore(t)
	commitReportSnapshot(t, store, reportFixtureV1(), true)
	commitReportSnapshot(t, store, reportFixtureV2(), false)
	commitReportSnapshot(t, store, reportFixtureV3(), false)

	view, err := store.LoadImpactView("example.com/project/pkg.helper", 3)
	if err != nil {
		t.Fatalf("LoadImpactView returned error: %v", err)
	}
	if len(view.DirectCallers) == 0 || view.DirectCallers[0].Symbol.QName != "example.com/project/api.Handle" {
		t.Fatalf("expected direct caller on helper impact view, got %+v", view.DirectCallers)
	}
	if len(view.InboundRefs) == 0 || view.InboundRefs[0].Symbol.PackageImportPath != "example.com/project/api" {
		t.Fatalf("expected inbound ref package on helper impact view, got %+v", view.InboundRefs)
	}
	if !containsString(view.ReferencePackages, "example.com/project/api") {
		t.Fatalf("expected reference packages to include api, got %+v", view.ReferencePackages)
	}
	if !containsString(view.BlastPackages, "example.com/project/api") {
		t.Fatalf("expected blast packages to include api, got %+v", view.BlastPackages)
	}
	if !containsString(view.BlastFiles, "api/handler.go") || !containsString(view.BlastFiles, "pkg/service_test.go") {
		t.Fatalf("expected blast files to include caller and test files, got %+v", view.BlastFiles)
	}
	if len(view.EmpiricalPackages) == 0 || view.EmpiricalPackages[0].Label != "example.com/project/api" {
		t.Fatalf("expected empirical packages to include api, got %+v", view.EmpiricalPackages)
	}
	if len(view.EmpiricalFiles) == 0 || view.EmpiricalFiles[0].Label != "api/handler.go" {
		t.Fatalf("expected empirical files to include api/handler.go, got %+v", view.EmpiricalFiles)
	}
	if len(view.ExpansionWhy) == 0 {
		t.Fatalf("expected expansion why notes, got %+v", view)
	}
	apiPkg := findImpactPackageReason(view.BlastPackageReasons, "example.com/project/api")
	if apiPkg == nil {
		t.Fatalf("expected blast package reasons for api, got %+v", view.BlastPackageReasons)
	}
	for _, expected := range []string{
		"reverse package dependency in local graph",
		"empirical co-change signal",
		"recent symbol delta changed caller surface",
	} {
		if !containsSubstring(apiPkg.Why, expected) {
			t.Fatalf("expected %q in api blast package reasons, got %+v", expected, apiPkg.Why)
		}
	}
	handlerFile := findImpactFileReason(view.BlastFileReasons, "api/handler.go")
	if handlerFile == nil {
		t.Fatalf("expected blast file reasons for api/handler.go, got %+v", view.BlastFileReasons)
	}
	for _, expected := range []string{
		"static call edge from indexed call site",
		"recent symbol delta changed caller surface",
	} {
		if !containsSubstring(handlerFile.Why, expected) {
			t.Fatalf("expected %q in handler blast file reasons, got %+v", expected, handlerFile.Why)
		}
	}
	testFile := findImpactFileReason(view.BlastFileReasons, "pkg/service_test.go")
	if testFile == nil || !containsSubstring(testFile.Why, "recent symbol delta changed related test surface") {
		t.Fatalf("expected recent test-surface reason on service_test.go, got %+v", view.BlastFileReasons)
	}
}

func TestSymbolHistoryShowsIntroducedAndRecentChanges(t *testing.T) {
	store := openTestStore(t)
	first := commitReportSnapshot(t, store, reportFixtureV1(), true)
	second := commitReportSnapshot(t, store, reportFixtureV2(), false)
	third := commitReportSnapshot(t, store, reportFixtureV3(), false)

	view, err := store.SymbolHistory("example.com/project/pkg.Service.Run", 10)
	if err != nil {
		t.Fatalf("SymbolHistory returned error: %v", err)
	}
	if view.IntroducedIn.ID != first.ID {
		t.Fatalf("expected introduced in snapshot %d, got %+v", first.ID, view.IntroducedIn)
	}
	if view.LastChangedIn.ID != third.ID {
		t.Fatalf("expected last changed in snapshot %d, got %+v", third.ID, view.LastChangedIn)
	}
	if len(view.Events) < 3 {
		t.Fatalf("expected at least three history events, got %+v", view.Events)
	}

	latest := findSymbolHistoryEvent(view.Events, third.ID)
	if latest == nil {
		t.Fatalf("expected history event for snapshot %d, got %+v", third.ID, view.Events)
	}
	if latest.Status != "changed" || !latest.ContractChanged || !latest.Moved {
		t.Fatalf("unexpected latest event: %+v", latest)
	}
	if latest.RemovedCalls == 0 || latest.AddedRefs == 0 || latest.RemovedRefs == 0 {
		t.Fatalf("expected relation deltas on latest event, got %+v", latest)
	}

	middle := findSymbolHistoryEvent(view.Events, second.ID)
	if middle == nil || !middle.ContractChanged {
		t.Fatalf("expected contract change in snapshot %d, got %+v", second.ID, view.Events)
	}
}

func TestPackageHistoryShowsContractsMovesAndDeps(t *testing.T) {
	store := openTestStore(t)
	first := commitReportSnapshot(t, store, reportFixtureV1(), true)
	_ = commitReportSnapshot(t, store, reportFixtureV2(), false)
	third := commitReportSnapshot(t, store, reportFixtureV3(), false)

	view, err := store.PackageHistory("example.com/project/pkg", 10)
	if err != nil {
		t.Fatalf("PackageHistory returned error: %v", err)
	}
	if view.IntroducedIn.ID != first.ID {
		t.Fatalf("expected introduced in snapshot %d, got %+v", first.ID, view.IntroducedIn)
	}
	if view.LastChangedIn.ID != third.ID {
		t.Fatalf("expected last changed in snapshot %d, got %+v", third.ID, view.LastChangedIn)
	}

	latest := findPackageHistoryEvent(view.Events, third.ID)
	if latest == nil {
		t.Fatalf("expected history event for snapshot %d, got %+v", third.ID, view.Events)
	}
	if latest.FileDelta != 1 {
		t.Fatalf("expected file delta +1, got %+v", latest)
	}
	if latest.MovedSymbols == 0 || latest.ChangedContracts == 0 {
		t.Fatalf("expected moved symbols and contract changes, got %+v", latest)
	}
	if latest.AddedDeps == 0 {
		t.Fatalf("expected added deps in latest event, got %+v", latest)
	}
}

func TestCoChangeFindsFilesAndPackagesAroundSymbol(t *testing.T) {
	store := openTestStore(t)
	commitReportSnapshot(t, store, reportFixtureV1(), true)
	commitReportSnapshot(t, store, reportFixtureV2(), false)
	commitReportSnapshot(t, store, reportFixtureV3(), false)

	view, err := store.SymbolCoChange("example.com/project/pkg.Service.Run", 5)
	if err != nil {
		t.Fatalf("SymbolCoChange returned error: %v", err)
	}
	if view.AnchorChangeCount != 2 {
		t.Fatalf("expected 2 anchor changes, got %+v", view)
	}
	if len(view.Files) == 0 || view.Files[0].Label != "api/handler.go" || view.Files[0].Count != 1 {
		t.Fatalf("expected api/handler.go as top co-change file, got %+v", view.Files)
	}
	if len(view.Packages) == 0 || view.Packages[0].Label != "example.com/project/api" || view.Packages[0].Count != 1 {
		t.Fatalf("expected api package as top co-change package, got %+v", view.Packages)
	}
}

func findChangedSymbol(values []ChangedSymbol, qname string) *ChangedSymbol {
	for idx := range values {
		if values[idx].QName == qname {
			return &values[idx]
		}
	}
	return nil
}

func findChangedPackage(values []ChangedPackage, importPath string) *ChangedPackage {
	for idx := range values {
		if values[idx].ImportPath == importPath {
			return &values[idx]
		}
	}
	return nil
}

func lookupSymbolImpactDelta(values []SymbolImpactDelta, qname string) *SymbolImpactDelta {
	for idx := range values {
		if values[idx].QName == qname {
			return &values[idx]
		}
	}
	return nil
}

func findImpactPackageReason(values []ImpactPackageReason, importPath string) *ImpactPackageReason {
	for idx := range values {
		if values[idx].PackageImportPath == importPath {
			return &values[idx]
		}
	}
	return nil
}

func findImpactFileReason(values []ImpactFileReason, filePath string) *ImpactFileReason {
	for idx := range values {
		if values[idx].FilePath == filePath {
			return &values[idx]
		}
	}
	return nil
}

func containsSubstring(values []string, want string) bool {
	for _, value := range values {
		if strings.Contains(value, want) {
			return true
		}
	}
	return false
}

func hasCall(values []CallEdgeChange, caller, callee string) bool {
	for _, value := range values {
		if value.CallerQName == caller && value.CalleeQName == callee {
			return true
		}
	}
	return false
}

func hasRef(values []RefChange, toQName string) bool {
	for _, value := range values {
		if value.ToQName == toQName {
			return true
		}
	}
	return false
}

func hasTestLink(values []TestLinkChange, testKey, symbolQName string) bool {
	for _, value := range values {
		if value.TestKey == testKey && value.SymbolQName == symbolQName {
			return true
		}
	}
	return false
}

func hasPackageDep(values []PackageDepChange, fromPackage, toPackage string) bool {
	for _, value := range values {
		if value.FromPackageImportPath == fromPackage && value.ToPackageImportPath == toPackage {
			return true
		}
	}
	return false
}

func findSymbolHistoryEvent(values []SymbolHistoryEvent, snapshotID int64) *SymbolHistoryEvent {
	for idx := range values {
		if values[idx].ToSnapshot.ID == snapshotID {
			return &values[idx]
		}
	}
	return nil
}

func findPackageHistoryEvent(values []PackageHistoryEvent, snapshotID int64) *PackageHistoryEvent {
	for idx := range values {
		if values[idx].ToSnapshot.ID == snapshotID {
			return &values[idx]
		}
	}
	return nil
}
