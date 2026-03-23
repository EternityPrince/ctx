package storage

import (
	"testing"

	"github.com/vladimirkasterin/ctx/internal/codebase"
)

func TestFindSymbolsSupportsExactAndFuzzyMatching(t *testing.T) {
	store := openTestStore(t)
	commitReportSnapshot(t, store, reportFixtureV1(), true)

	exact, err := store.FindSymbols("Service")
	if err != nil {
		t.Fatalf("FindSymbols exact returned error: %v", err)
	}
	if len(exact) == 0 {
		t.Fatal("expected exact symbol matches")
	}
	if exact[0].QName != "example.com/project/pkg.Service" || exact[0].SearchKind != "name" {
		t.Fatalf("unexpected exact search result: %+v", exact[0])
	}

	fuzzy, err := store.FindSymbols("Servce")
	if err != nil {
		t.Fatalf("FindSymbols fuzzy returned error: %v", err)
	}
	if len(fuzzy) == 0 {
		t.Fatal("expected fuzzy symbol matches")
	}
	if fuzzy[0].QName != "example.com/project/pkg.Service" || fuzzy[0].SearchKind != "fuzzy" {
		t.Fatalf("unexpected fuzzy search result: %+v", fuzzy[0])
	}

	contains, err := store.FindSymbols("Handle")
	if err != nil {
		t.Fatalf("FindSymbols contains returned error: %v", err)
	}
	if len(contains) == 0 || contains[0].QName != "example.com/project/api.Handle" {
		t.Fatalf("unexpected contains search result: %+v", contains)
	}
}

func TestFindSymbolsRanksRelevantEntitiesWithinTheSameMatchTier(t *testing.T) {
	store := openTestStore(t)
	fixture := reportFixtureV1()
	fixture.scanned = append(fixture.scanned, codebase.ScanFile{
		RelPath:   "util/run.go",
		Hash:      "util-run-v1",
		SizeBytes: 80,
		IsGo:      true,
	})
	fixture.result.ImpactedPackage["example.com/project/util"] = struct{}{}
	fixture.result.Packages = append(fixture.result.Packages, codebase.PackageFact{
		ImportPath: "example.com/project/util",
		Name:       "util",
		DirPath:    "util",
		FileCount:  1,
	})
	fixture.result.Symbols = append(fixture.result.Symbols, codebase.SymbolFact{
		SymbolKey:         "example.com/project/util.Run",
		QName:             "example.com/project/util.Run",
		PackageImportPath: "example.com/project/util",
		FilePath:          "util/run.go",
		Name:              "Run",
		Kind:              "func",
		Signature:         "func Run()",
		Line:              3,
		Column:            1,
		Exported:          true,
	})
	commitReportSnapshot(t, store, fixture, true)

	matches, err := store.FindSymbols("Run")
	if err != nil {
		t.Fatalf("FindSymbols returned error: %v", err)
	}
	if len(matches) < 2 {
		t.Fatalf("expected at least two Run matches, got %+v", matches)
	}
	if matches[0].QName != "example.com/project/pkg.Service.Run" {
		t.Fatalf("expected structurally relevant Run first, got %+v", matches)
	}
	if matches[1].QName != "example.com/project/util.Run" {
		t.Fatalf("expected utility Run second, got %+v", matches)
	}
	if matches[0].CallerCount != 1 || matches[0].TestCount != 1 || matches[0].ReversePackageDeps != 1 {
		t.Fatalf("expected first match to carry search metrics, got %+v", matches[0])
	}
	if matches[0].PackageImportance <= matches[1].PackageImportance {
		t.Fatalf("expected package importance to favor pkg.Service.Run, got first=%+v second=%+v", matches[0], matches[1])
	}
}
