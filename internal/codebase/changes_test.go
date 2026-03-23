package codebase

import "testing"

func TestDetectPackageChangesMarksModuleUpdatesForFullReindex(t *testing.T) {
	scanned := []ScanFile{
		{RelPath: "go.mod", Hash: "new", IsModule: true},
		{RelPath: "pkg/service.go", Hash: "same"},
	}
	previous := map[string]PreviousFile{
		"go.mod":         {RelPath: "go.mod", Hash: "old"},
		"pkg/service.go": {RelPath: "pkg/service.go", PackageImportPath: "example.com/project/pkg", Hash: "same"},
	}

	plan := DetectPackageChanges("example.com/project", scanned, previous)
	if !plan.FullReindex {
		t.Fatal("expected module change to force full reindex")
	}
}

func TestMergeChangePlansDeduplicatesImpactedPackages(t *testing.T) {
	plan := MergeChangePlans(ChangeSet{Changed: []string{"pkg/service.py"}}, ChangePlan{
		ImpactedPackages: []string{"pkg", "pkg.internal"},
	}, ChangePlan{
		ImpactedPackages: []string{"pkg"},
		FullReindex:      true,
	})

	if !plan.FullReindex {
		t.Fatal("expected merged plan to preserve full reindex")
	}
	if len(plan.ImpactedPackages) != 2 || plan.ImpactedPackages[0] != "pkg" || plan.ImpactedPackages[1] != "pkg.internal" {
		t.Fatalf("unexpected impacted packages: %+v", plan.ImpactedPackages)
	}
}

func TestSortResultOrdersAllFactSlices(t *testing.T) {
	result := &Result{
		Packages: []PackageFact{
			{ImportPath: "pkg.z"},
			{ImportPath: "pkg.a"},
		},
		Files: []FileFact{
			{RelPath: "z.py"},
			{RelPath: "a.py"},
		},
		Symbols: []SymbolFact{
			{QName: "pkg.z.run"},
			{QName: "pkg.a.run"},
		},
		Dependencies: []DependencyFact{
			{FromPackageImportPath: "pkg.z", ToPackageImportPath: "pkg.b"},
			{FromPackageImportPath: "pkg.a", ToPackageImportPath: "pkg.c"},
		},
		References: []ReferenceFact{
			{ToSymbolKey: "b", FilePath: "z.py", Line: 4},
			{ToSymbolKey: "a", FilePath: "a.py", Line: 2},
		},
		Calls: []CallFact{
			{CallerSymbolKey: "b", CalleeSymbolKey: "c"},
			{CallerSymbolKey: "a", CalleeSymbolKey: "d"},
		},
		Tests: []TestFact{
			{TestKey: "test-b"},
			{TestKey: "test-a"},
		},
		TestLinks: []TestLinkFact{
			{TestKey: "test-b", SymbolKey: "b"},
			{TestKey: "test-a", SymbolKey: "a"},
		},
	}

	SortResult(result)

	if result.Packages[0].ImportPath != "pkg.a" || result.Files[0].RelPath != "a.py" || result.Symbols[0].QName != "pkg.a.run" {
		t.Fatalf("expected sorted result slices, got %+v", result)
	}
	if result.Dependencies[0].FromPackageImportPath != "pkg.a" || result.References[0].ToSymbolKey != "a" || result.Calls[0].CallerSymbolKey != "a" {
		t.Fatalf("expected sorted dependency/reference/call slices, got %+v", result)
	}
	if result.Tests[0].TestKey != "test-a" || result.TestLinks[0].TestKey != "test-a" {
		t.Fatalf("expected sorted test slices, got %+v", result)
	}
}
