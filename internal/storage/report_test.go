package storage

import (
	"slices"
	"testing"

	"github.com/vladimirkasterin/ctx/internal/codebase"
)

func TestLoadReportViewAndFileSummaries(t *testing.T) {
	store := openTestStore(t)
	snapshot := commitReportSnapshot(t, store, reportFixtureV1(), true)

	if snapshot.TotalPackages != 2 || snapshot.TotalFiles != 3 || snapshot.TotalSymbols != 4 {
		t.Fatalf("unexpected snapshot totals: %+v", snapshot)
	}
	if snapshot.TotalCalls != 1 || snapshot.TotalRefs != 2 || snapshot.TotalTests != 1 {
		t.Fatalf("unexpected edge/test totals: %+v", snapshot)
	}

	summaries, err := store.LoadFileSummaries()
	if err != nil {
		t.Fatalf("LoadFileSummaries returned error: %v", err)
	}

	serviceSummary := summaries["pkg/service.go"]
	if serviceSummary.SymbolCount != 3 || serviceSummary.FuncCount != 1 || serviceSummary.MethodCount != 1 || serviceSummary.StructCount != 1 {
		t.Fatalf("unexpected service summary: %+v", serviceSummary)
	}
	if serviceSummary.RelatedTestCount != 1 || serviceSummary.TestLinkedSymbolCount != 2 || serviceSummary.RelevantSymbolCount != 3 {
		t.Fatalf("unexpected service test linkage summary: %+v", serviceSummary)
	}
	if serviceSummary.QualityScore <= 0 || len(serviceSummary.QualityWhy) == 0 {
		t.Fatalf("expected quality signals on service summary, got %+v", serviceSummary)
	}

	testSummary := summaries["pkg/service_test.go"]
	if !testSummary.IsTest || testSummary.DeclaredTestCount != 1 {
		t.Fatalf("unexpected service test summary: %+v", testSummary)
	}

	apiSummary := summaries["api/handler.go"]
	if apiSummary.PackageImportPath != "example.com/project/api" || apiSummary.FuncCount != 1 {
		t.Fatalf("unexpected api summary: %+v", apiSummary)
	}

	loadedServiceSummary, err := store.LoadFileSummary("pkg/service.go")
	if err != nil {
		t.Fatalf("LoadFileSummary returned error: %v", err)
	}
	if loadedServiceSummary.FilePath != serviceSummary.FilePath ||
		loadedServiceSummary.PackageImportPath != serviceSummary.PackageImportPath ||
		loadedServiceSummary.QualityScore != serviceSummary.QualityScore ||
		loadedServiceSummary.GraphScore != serviceSummary.GraphScore ||
		loadedServiceSummary.SymbolCount != serviceSummary.SymbolCount ||
		loadedServiceSummary.RelatedTestCount != serviceSummary.RelatedTestCount ||
		loadedServiceSummary.ReversePackageDeps != serviceSummary.ReversePackageDeps {
		t.Fatalf("expected direct file summary to match map summary: got %+v want %+v", loadedServiceSummary, serviceSummary)
	}

	report, err := store.LoadReportView(4)
	if err != nil {
		t.Fatalf("LoadReportView returned error: %v", err)
	}
	if len(report.TopPackages) != 2 || report.TopPackages[0].Summary.ImportPath != "example.com/project/pkg" {
		t.Fatalf("unexpected top packages: %+v", report.TopPackages)
	}
	if len(report.TopFunctions) == 0 || report.TopFunctions[0].Symbol.QName != "example.com/project/pkg.Service.Run" {
		t.Fatalf("unexpected top functions: %+v", report.TopFunctions)
	}
	if len(report.TopTypes) == 0 || report.TopTypes[0].Symbol.QName != "example.com/project/pkg.Service" {
		t.Fatalf("unexpected top types: %+v", report.TopTypes)
	}
	if len(report.TopFiles) == 0 || report.TopFiles[0].Summary.FilePath != "pkg/service.go" {
		t.Fatalf("unexpected top files: %+v", report.TopFiles)
	}
	if len(report.ProvenanceNotes) == 0 {
		t.Fatalf("expected report provenance notes, got %+v", report)
	}
	if err := store.ExplainReportView(&report); err != nil {
		t.Fatalf("ExplainReportView returned error: %v", err)
	}
	if !hasProvenanceKind(report.TopFunctions[0].Provenance, "call") || !hasProvenanceKind(report.TopFunctions[0].Provenance, "test") {
		t.Fatalf("expected top function provenance to include call and test evidence, got %+v", report.TopFunctions[0].Provenance)
	}
	if !hasProvenanceKind(report.TopPackages[0].Provenance, "reverse_dep") || !hasProvenanceKind(report.TopPackages[0].Provenance, "symbol") {
		t.Fatalf("expected top package provenance to include dependency and symbol evidence, got %+v", report.TopPackages[0].Provenance)
	}

	status, err := store.Status(3)
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if !status.HasSnapshot || status.Current.ID != snapshot.ID {
		t.Fatalf("unexpected status current snapshot: %+v", status)
	}
	if status.RootPath != "/tmp/project" || status.ModulePath != "example.com/project" {
		t.Fatalf("unexpected project status: %+v", status)
	}
	if status.Storage.SnapshotCount != 1 || status.Storage.CurrentDBPath == "" {
		t.Fatalf("unexpected storage status: %+v", status.Storage)
	}

	currentFiles, err := store.CurrentFiles()
	if err != nil {
		t.Fatalf("CurrentFiles returned error: %v", err)
	}
	if len(currentFiles) != 3 {
		t.Fatalf("unexpected current files count: got %d want %d", len(currentFiles), 3)
	}
	if file := currentFiles["pkg/service_test.go"]; !file.IsTest || file.PackageImportPath != "example.com/project/pkg" {
		t.Fatalf("unexpected current test file record: %+v", file)
	}

	reverse, err := store.ReverseDependencies(snapshot.ID, []string{"example.com/project/pkg"})
	if err != nil {
		t.Fatalf("ReverseDependencies returned error: %v", err)
	}
	if len(reverse) != 1 || reverse[0] != "example.com/project/api" {
		t.Fatalf("unexpected reverse dependencies: %+v", reverse)
	}
}

func TestLoadSymbolViewIncludesExplainabilityWhy(t *testing.T) {
	store := openTestStore(t)
	commitReportSnapshot(t, store, reportFixtureV1(), true)

	runView, err := store.LoadSymbolView("example.com/project/pkg.Service.Run")
	if err != nil {
		t.Fatalf("LoadSymbolView(run) returned error: %v", err)
	}
	if runView.QualityScore <= 0 || len(runView.QualityWhy) == 0 {
		t.Fatalf("expected quality score on symbol view, got %+v", runView)
	}
	if len(runView.Callers) == 0 || runView.Callers[0].Why == "" || runView.Callers[0].Relation != "static" {
		t.Fatalf("expected explained caller edge, got %+v", runView.Callers)
	}
	if len(runView.Tests) == 0 || runView.Tests[0].Why == "" {
		t.Fatalf("expected explained test links, got %+v", runView.Tests)
	}

	serviceView, err := store.LoadSymbolView("example.com/project/pkg.Service")
	if err != nil {
		t.Fatalf("LoadSymbolView(service) returned error: %v", err)
	}
	if len(serviceView.ReferencesIn) == 0 {
		t.Fatalf("expected inbound references, got %+v", serviceView.ReferencesIn)
	}
	for _, ref := range serviceView.ReferencesIn {
		if ref.Why == "" {
			t.Fatalf("expected why for inbound refs, got %+v", serviceView.ReferencesIn)
		}
	}
}

func TestLoadReportViewQualityRecognizesEntrypointFiles(t *testing.T) {
	store := openTestStore(t)
	fixture := reportFixture{
		scanned: []codebase.ScanFile{
			{RelPath: "main.go", Hash: "main-v1", SizeBytes: 80, IsGo: true},
			{RelPath: "pkg/run.go", Hash: "run-v1", SizeBytes: 120, IsGo: true},
		},
		result: &codebase.Result{
			Root:       "/tmp/project",
			ModulePath: "example.com/project",
			GoVersion:  "1.26",
			ImpactedPackage: map[string]struct{}{
				"example.com/project":     {},
				"example.com/project/pkg": {},
			},
			Packages: []codebase.PackageFact{
				{ImportPath: "example.com/project", Name: "main", DirPath: ".", FileCount: 1},
				{ImportPath: "example.com/project/pkg", Name: "pkg", DirPath: "pkg", FileCount: 1},
			},
			Symbols: []codebase.SymbolFact{
				{
					SymbolKey:         "example.com/project.main",
					QName:             "example.com/project.main",
					PackageImportPath: "example.com/project",
					FilePath:          "main.go",
					Name:              "main",
					Kind:              "func",
					Signature:         "func main()",
					Line:              3,
					Column:            1,
				},
				{
					SymbolKey:         "example.com/project/pkg.Run",
					QName:             "example.com/project/pkg.Run",
					PackageImportPath: "example.com/project/pkg",
					FilePath:          "pkg/run.go",
					Name:              "Run",
					Kind:              "func",
					Signature:         "func Run()",
					Line:              3,
					Column:            1,
					Exported:          true,
				},
			},
			Dependencies: []codebase.DependencyFact{
				{
					FromPackageImportPath: "example.com/project",
					ToPackageImportPath:   "example.com/project/pkg",
					IsLocal:               true,
				},
			},
			Calls: []codebase.CallFact{
				{
					CallerPackageImportPath: "example.com/project",
					CallerSymbolKey:         "example.com/project.main",
					CalleeSymbolKey:         "example.com/project/pkg.Run",
					FilePath:                "main.go",
					Line:                    4,
					Column:                  2,
					Dispatch:                "static",
				},
			},
		},
		changes: codebase.ChangeSet{
			Added: []string{"main.go", "pkg/run.go"},
		},
	}
	commitReportSnapshot(t, store, fixture, true)

	report, err := store.LoadReportView(8)
	if err != nil {
		t.Fatalf("LoadReportView returned error: %v", err)
	}

	var mainFile *RankedFile
	for idx := range report.TopFiles {
		if report.TopFiles[idx].Summary.FilePath == "main.go" {
			mainFile = &report.TopFiles[idx]
			break
		}
	}
	if mainFile == nil {
		t.Fatalf("expected main.go in top files, got %+v", report.TopFiles)
	}
	if !mainFile.Summary.IsEntrypoint {
		t.Fatalf("expected main.go to be marked as entrypoint, got %+v", mainFile)
	}
	if !containsString(mainFile.QualityWhy, "main entrypoint file") {
		t.Fatalf("expected entrypoint reason in %+v", mainFile.QualityWhy)
	}
}

func TestCommitSnapshotIncrementalKeepsUnchangedPackages(t *testing.T) {
	store := openTestStore(t)
	commitReportSnapshot(t, store, reportFixtureV1(), true)

	snapshot := commitReportSnapshot(t, store, reportFixtureV2(), false)
	if snapshot.ChangedFiles != 1 || snapshot.ChangedPackages != 1 || snapshot.ChangedSymbols != 1 {
		t.Fatalf("unexpected incremental snapshot counts: %+v", snapshot)
	}
	if snapshot.TotalPackages != 2 || snapshot.TotalSymbols != 4 {
		t.Fatalf("unexpected incremental totals: %+v", snapshot)
	}

	apiSymbols, err := store.LoadFileSymbols("api/handler.go")
	if err != nil {
		t.Fatalf("LoadFileSymbols(api/handler.go) returned error: %v", err)
	}
	requireQName(t, apiSymbols, "example.com/project/api.Handle")

	serviceSymbols, err := store.LoadFileSymbols("pkg/service.go")
	if err != nil {
		t.Fatalf("LoadFileSymbols(pkg/service.go) returned error: %v", err)
	}
	requireSymbolSignature(t, serviceSymbols, "example.com/project/pkg.Service.Run", "func (*Service) Run(ctx string) error")

	report, err := store.LoadReportView(4)
	if err != nil {
		t.Fatalf("LoadReportView returned error: %v", err)
	}
	if len(report.TopPackages) != 2 {
		t.Fatalf("expected copied-forward package summaries to remain available, got %+v", report.TopPackages)
	}
	if len(report.TopFiles) == 0 || !report.TopFiles[0].Summary.ChangedRecently {
		t.Fatalf("expected changed file quality to surface in top files, got %+v", report.TopFiles)
	}
}

type reportFixture struct {
	scanned []codebase.ScanFile
	result  *codebase.Result
	changes codebase.ChangeSet
}

func reportFixtureV1() reportFixture {
	return reportFixture{
		scanned: []codebase.ScanFile{
			{RelPath: "pkg/service.go", Hash: "svc-v1", SizeBytes: 120, IsGo: true},
			{RelPath: "pkg/service_test.go", Hash: "svc-test-v1", SizeBytes: 90, IsGo: true, IsTest: true},
			{RelPath: "api/handler.go", Hash: "api-v1", SizeBytes: 110, IsGo: true},
		},
		result: &codebase.Result{
			Root:       "/tmp/project",
			ModulePath: "example.com/project",
			GoVersion:  "1.26",
			ImpactedPackage: map[string]struct{}{
				"example.com/project/api": {},
				"example.com/project/pkg": {},
			},
			Packages: []codebase.PackageFact{
				{ImportPath: "example.com/project/pkg", Name: "pkg", DirPath: "pkg", FileCount: 2},
				{ImportPath: "example.com/project/api", Name: "api", DirPath: "api", FileCount: 1},
			},
			Symbols: []codebase.SymbolFact{
				{
					SymbolKey:         "example.com/project/pkg.Service",
					QName:             "example.com/project/pkg.Service",
					PackageImportPath: "example.com/project/pkg",
					FilePath:          "pkg/service.go",
					Name:              "Service",
					Kind:              "struct",
					Signature:         "type Service struct{}",
					Line:              3,
					Column:            6,
					Exported:          true,
				},
				{
					SymbolKey:         "example.com/project/pkg.Service.Run",
					QName:             "example.com/project/pkg.Service.Run",
					PackageImportPath: "example.com/project/pkg",
					FilePath:          "pkg/service.go",
					Name:              "Run",
					Kind:              "method",
					Receiver:          "*Service",
					Signature:         "func (*Service) Run() error",
					Line:              5,
					Column:            1,
					Exported:          true,
				},
				{
					SymbolKey:         "example.com/project/pkg.helper",
					QName:             "example.com/project/pkg.helper",
					PackageImportPath: "example.com/project/pkg",
					FilePath:          "pkg/service.go",
					Name:              "helper",
					Kind:              "func",
					Signature:         "func helper()",
					Line:              9,
					Column:            1,
				},
				{
					SymbolKey:         "example.com/project/api.Handle",
					QName:             "example.com/project/api.Handle",
					PackageImportPath: "example.com/project/api",
					FilePath:          "api/handler.go",
					Name:              "Handle",
					Kind:              "func",
					Signature:         "func Handle()",
					Line:              3,
					Column:            1,
					Exported:          true,
				},
			},
			Dependencies: []codebase.DependencyFact{
				{
					FromPackageImportPath: "example.com/project/api",
					ToPackageImportPath:   "example.com/project/pkg",
					IsLocal:               true,
				},
			},
			References: []codebase.ReferenceFact{
				{
					FromPackageImportPath: "example.com/project/pkg",
					FromSymbolKey:         "example.com/project/pkg.Service.Run",
					ToSymbolKey:           "example.com/project/pkg.Service",
					FilePath:              "pkg/service.go",
					Line:                  5,
					Column:                7,
					Kind:                  "receiver",
				},
				{
					FromPackageImportPath: "example.com/project/api",
					FromSymbolKey:         "example.com/project/api.Handle",
					ToSymbolKey:           "example.com/project/pkg.Service",
					FilePath:              "api/handler.go",
					Line:                  5,
					Column:                10,
					Kind:                  "type",
				},
			},
			Calls: []codebase.CallFact{
				{
					CallerPackageImportPath: "example.com/project/api",
					CallerSymbolKey:         "example.com/project/api.Handle",
					CalleeSymbolKey:         "example.com/project/pkg.Service.Run",
					FilePath:                "api/handler.go",
					Line:                    6,
					Column:                  2,
					Dispatch:                "static",
				},
			},
			Tests: []codebase.TestFact{
				{
					TestKey:           "example.com/project/pkg:TestServiceRun",
					PackageImportPath: "example.com/project/pkg",
					FilePath:          "pkg/service_test.go",
					Name:              "TestServiceRun",
					Kind:              "func",
					Line:              3,
				},
			},
			TestLinks: []codebase.TestLinkFact{
				{
					TestPackageImportPath: "example.com/project/pkg",
					TestKey:               "example.com/project/pkg:TestServiceRun",
					SymbolKey:             "example.com/project/pkg.Service.Run",
					LinkKind:              "direct",
					Confidence:            "high",
				},
				{
					TestPackageImportPath: "example.com/project/pkg",
					TestKey:               "example.com/project/pkg:TestServiceRun",
					SymbolKey:             "example.com/project/pkg.Service",
					LinkKind:              "related",
					Confidence:            "medium",
				},
			},
		},
		changes: codebase.ChangeSet{
			Added: []string{"api/handler.go", "pkg/service.go", "pkg/service_test.go"},
		},
	}
}

func reportFixtureV2() reportFixture {
	return reportFixture{
		scanned: []codebase.ScanFile{
			{RelPath: "pkg/service.go", Hash: "svc-v2", SizeBytes: 126, IsGo: true},
			{RelPath: "pkg/service_test.go", Hash: "svc-test-v1", SizeBytes: 90, IsGo: true, IsTest: true},
			{RelPath: "api/handler.go", Hash: "api-v1", SizeBytes: 110, IsGo: true},
		},
		result: &codebase.Result{
			Root:       "/tmp/project",
			ModulePath: "example.com/project",
			GoVersion:  "1.26",
			ImpactedPackage: map[string]struct{}{
				"example.com/project/pkg": {},
			},
			Packages: []codebase.PackageFact{
				{ImportPath: "example.com/project/pkg", Name: "pkg", DirPath: "pkg", FileCount: 2},
			},
			Symbols: []codebase.SymbolFact{
				{
					SymbolKey:         "example.com/project/pkg.Service",
					QName:             "example.com/project/pkg.Service",
					PackageImportPath: "example.com/project/pkg",
					FilePath:          "pkg/service.go",
					Name:              "Service",
					Kind:              "struct",
					Signature:         "type Service struct{}",
					Line:              3,
					Column:            6,
					Exported:          true,
				},
				{
					SymbolKey:         "example.com/project/pkg.Service.Run",
					QName:             "example.com/project/pkg.Service.Run",
					PackageImportPath: "example.com/project/pkg",
					FilePath:          "pkg/service.go",
					Name:              "Run",
					Kind:              "method",
					Receiver:          "*Service",
					Signature:         "func (*Service) Run(ctx string) error",
					Line:              5,
					Column:            1,
					Exported:          true,
				},
				{
					SymbolKey:         "example.com/project/pkg.helper",
					QName:             "example.com/project/pkg.helper",
					PackageImportPath: "example.com/project/pkg",
					FilePath:          "pkg/service.go",
					Name:              "helper",
					Kind:              "func",
					Signature:         "func helper()",
					Line:              9,
					Column:            1,
				},
			},
			References: []codebase.ReferenceFact{
				{
					FromPackageImportPath: "example.com/project/pkg",
					FromSymbolKey:         "example.com/project/pkg.Service.Run",
					ToSymbolKey:           "example.com/project/pkg.Service",
					FilePath:              "pkg/service.go",
					Line:                  5,
					Column:                7,
					Kind:                  "receiver",
				},
			},
			Tests: []codebase.TestFact{
				{
					TestKey:           "example.com/project/pkg:TestServiceRun",
					PackageImportPath: "example.com/project/pkg",
					FilePath:          "pkg/service_test.go",
					Name:              "TestServiceRun",
					Kind:              "func",
					Line:              3,
				},
			},
			TestLinks: []codebase.TestLinkFact{
				{
					TestPackageImportPath: "example.com/project/pkg",
					TestKey:               "example.com/project/pkg:TestServiceRun",
					SymbolKey:             "example.com/project/pkg.Service.Run",
					LinkKind:              "direct",
					Confidence:            "high",
				},
				{
					TestPackageImportPath: "example.com/project/pkg",
					TestKey:               "example.com/project/pkg:TestServiceRun",
					SymbolKey:             "example.com/project/pkg.Service",
					LinkKind:              "related",
					Confidence:            "medium",
				},
			},
		},
		changes: codebase.ChangeSet{
			Changed: []string{"pkg/service.go"},
		},
	}
}

func reportFixtureV3() reportFixture {
	return reportFixture{
		scanned: []codebase.ScanFile{
			{RelPath: "pkg/service.go", Hash: "svc-v3", SizeBytes: 128, IsGo: true},
			{RelPath: "pkg/runtime.go", Hash: "runtime-v1", SizeBytes: 96, IsGo: true},
			{RelPath: "pkg/service_test.go", Hash: "svc-test-v2", SizeBytes: 120, IsGo: true, IsTest: true},
			{RelPath: "api/handler.go", Hash: "api-v2", SizeBytes: 118, IsGo: true},
		},
		result: &codebase.Result{
			Root:       "/tmp/project",
			ModulePath: "example.com/project",
			GoVersion:  "1.26",
			ImpactedPackage: map[string]struct{}{
				"example.com/project/api": {},
				"example.com/project/pkg": {},
			},
			Packages: []codebase.PackageFact{
				{ImportPath: "example.com/project/pkg", Name: "pkg", DirPath: "pkg", FileCount: 3},
				{ImportPath: "example.com/project/api", Name: "api", DirPath: "api", FileCount: 1},
			},
			Symbols: []codebase.SymbolFact{
				{
					SymbolKey:         "example.com/project/pkg.Service",
					QName:             "example.com/project/pkg.Service",
					PackageImportPath: "example.com/project/pkg",
					FilePath:          "pkg/service.go",
					Name:              "Service",
					Kind:              "struct",
					Signature:         "type Service struct{}",
					Line:              3,
					Column:            6,
					Exported:          true,
				},
				{
					SymbolKey:         "example.com/project/pkg.Service.Run",
					QName:             "example.com/project/pkg.Service.Run",
					PackageImportPath: "example.com/project/pkg",
					FilePath:          "pkg/runtime.go",
					Name:              "Run",
					Kind:              "method",
					Receiver:          "*Service",
					Signature:         "func (*Service) Run(ctx context.Context) error",
					Line:              4,
					Column:            1,
					Exported:          true,
				},
				{
					SymbolKey:         "example.com/project/pkg.helper",
					QName:             "example.com/project/pkg.helper",
					PackageImportPath: "example.com/project/pkg",
					FilePath:          "pkg/service.go",
					Name:              "helper",
					Kind:              "func",
					Signature:         "func helper()",
					Line:              8,
					Column:            1,
				},
				{
					SymbolKey:         "example.com/project/api.Handle",
					QName:             "example.com/project/api.Handle",
					PackageImportPath: "example.com/project/api",
					FilePath:          "api/handler.go",
					Name:              "Handle",
					Kind:              "func",
					Signature:         "func Handle()",
					Line:              3,
					Column:            1,
					Exported:          true,
				},
			},
			Dependencies: []codebase.DependencyFact{
				{
					FromPackageImportPath: "example.com/project/api",
					ToPackageImportPath:   "example.com/project/pkg",
					IsLocal:               true,
				},
				{
					FromPackageImportPath: "example.com/project/pkg",
					ToPackageImportPath:   "example.com/project/api",
					IsLocal:               true,
				},
			},
			References: []codebase.ReferenceFact{
				{
					FromPackageImportPath: "example.com/project/pkg",
					FromSymbolKey:         "example.com/project/pkg.Service.Run",
					ToSymbolKey:           "example.com/project/pkg.Service",
					FilePath:              "pkg/runtime.go",
					Line:                  4,
					Column:                7,
					Kind:                  "receiver",
				},
				{
					FromPackageImportPath: "example.com/project/api",
					FromSymbolKey:         "example.com/project/api.Handle",
					ToSymbolKey:           "example.com/project/pkg.helper",
					FilePath:              "api/handler.go",
					Line:                  5,
					Column:                9,
					Kind:                  "func",
				},
			},
			Calls: []codebase.CallFact{
				{
					CallerPackageImportPath: "example.com/project/api",
					CallerSymbolKey:         "example.com/project/api.Handle",
					CalleeSymbolKey:         "example.com/project/pkg.helper",
					FilePath:                "api/handler.go",
					Line:                    6,
					Column:                  2,
					Dispatch:                "static",
				},
			},
			Tests: []codebase.TestFact{
				{
					TestKey:           "example.com/project/pkg:TestServiceRun",
					PackageImportPath: "example.com/project/pkg",
					FilePath:          "pkg/service_test.go",
					Name:              "TestServiceRun",
					Kind:              "func",
					Line:              3,
				},
				{
					TestKey:           "example.com/project/pkg:TestHelper",
					PackageImportPath: "example.com/project/pkg",
					FilePath:          "pkg/service_test.go",
					Name:              "TestHelper",
					Kind:              "func",
					Line:              9,
				},
			},
			TestLinks: []codebase.TestLinkFact{
				{
					TestPackageImportPath: "example.com/project/pkg",
					TestKey:               "example.com/project/pkg:TestServiceRun",
					SymbolKey:             "example.com/project/pkg.Service.Run",
					LinkKind:              "direct",
					Confidence:            "high",
				},
				{
					TestPackageImportPath: "example.com/project/pkg",
					TestKey:               "example.com/project/pkg:TestHelper",
					SymbolKey:             "example.com/project/pkg.helper",
					LinkKind:              "direct",
					Confidence:            "high",
				},
			},
		},
		changes: codebase.ChangeSet{
			Added:   []string{"pkg/runtime.go"},
			Changed: []string{"api/handler.go", "pkg/service.go", "pkg/service_test.go"},
		},
	}
}

func commitReportSnapshot(t *testing.T, store *Store, fixture reportFixture, full bool) SnapshotInfo {
	t.Helper()

	snapshot, err := store.CommitSnapshot("index", "test snapshot", fixture.scanned, fixture.result, fixture.changes, full)
	if err != nil {
		t.Fatalf("CommitSnapshot returned error: %v", err)
	}
	return snapshot
}

func requireQName(t *testing.T, symbols []SymbolMatch, qname string) {
	t.Helper()
	for _, symbol := range symbols {
		if symbol.QName == qname {
			return
		}
	}
	t.Fatalf("expected symbol %q in %+v", qname, symbols)
}

func requireSymbolSignature(t *testing.T, symbols []SymbolMatch, qname, signature string) {
	t.Helper()
	for _, symbol := range symbols {
		if symbol.QName == qname {
			if symbol.Signature != signature {
				t.Fatalf("unexpected signature for %s: got %q want %q", qname, symbol.Signature, signature)
			}
			return
		}
	}
	t.Fatalf("expected symbol %q in %+v", qname, symbols)
}

func hasProvenanceKind(values []ProvenanceItem, kind string) bool {
	for _, value := range values {
		if value.Kind == kind {
			return true
		}
	}
	return false
}

func containsString(values []string, want string) bool {
	return slices.Contains(values, want)
}
