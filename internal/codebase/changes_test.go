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

func TestDetectGoChangesTreatsGoSumAsNoOp(t *testing.T) {
	scanned := []ScanFile{
		{RelPath: "go.mod", Hash: "same", IsModule: true, Identity: "example.com/project"},
		{RelPath: "go.sum", Hash: "new", IsModule: true},
		{RelPath: "pkg/service.go", Hash: "same", PackageImportPath: "example.com/project/pkg"},
	}
	previous := map[string]PreviousFile{
		"go.mod":         {RelPath: "go.mod", Hash: "same", Identity: "example.com/project"},
		"go.sum":         {RelPath: "go.sum", Hash: "old"},
		"pkg/service.go": {RelPath: "pkg/service.go", PackageImportPath: "example.com/project/pkg", Hash: "same"},
	}

	plan := DetectGoChanges("example.com/project", scanned, previous)
	if plan.FullReindex || plan.Changes.Count() != 1 || len(plan.ImpactedPackages) != 0 {
		t.Fatalf("expected go.sum change to be a no-op invalidation, got %+v", plan)
	}
}

func TestDetectGoChangesKeepsGoModSameModuleAsNoOp(t *testing.T) {
	scanned := []ScanFile{
		{RelPath: "go.mod", Hash: "new", IsModule: true, Identity: "example.com/project", SemanticMeta: EncodeManifestMeta(ParseGoModManifestMeta([]byte("module example.com/project\n\ngo 1.26\nrequire github.com/acme/lib v1.2.0\n")))},
		{RelPath: "pkg/service.go", Hash: "same", PackageImportPath: "example.com/project/pkg"},
	}
	previous := map[string]PreviousFile{
		"go.mod":         {RelPath: "go.mod", Hash: "old", Identity: "example.com/project", SemanticMeta: EncodeManifestMeta(ParseGoModManifestMeta([]byte("module example.com/project\n\ngo 1.26\nrequire github.com/acme/lib v1.1.0\n")))},
		"pkg/service.go": {RelPath: "pkg/service.go", PackageImportPath: "example.com/project/pkg", Hash: "same"},
	}

	plan := DetectGoChanges("example.com/project", scanned, previous)
	if plan.FullReindex || len(plan.ImpactedPackages) != 0 || len(plan.ManifestChanges) == 0 {
		t.Fatalf("expected go.mod same-module change to avoid full reindex, got %+v", plan)
	}
}

func TestDetectGoChangesTreatsGoVersionBumpAsPartialAllPackages(t *testing.T) {
	scanned := []ScanFile{
		{RelPath: "go.mod", Hash: "new", IsModule: true, Identity: "example.com/project", SemanticMeta: EncodeManifestMeta(ParseGoModManifestMeta([]byte("module example.com/project\n\ngo 1.27\n")))},
		{RelPath: "pkg/service.go", Hash: "same", PackageImportPath: "example.com/project/pkg"},
		{RelPath: "api/handler.go", Hash: "same", PackageImportPath: "example.com/project/api"},
	}
	previous := map[string]PreviousFile{
		"go.mod":         {RelPath: "go.mod", Hash: "old", Identity: "example.com/project", SemanticMeta: EncodeManifestMeta(ParseGoModManifestMeta([]byte("module example.com/project\n\ngo 1.26\n")))},
		"pkg/service.go": {RelPath: "pkg/service.go", PackageImportPath: "example.com/project/pkg", Hash: "same"},
		"api/handler.go": {RelPath: "api/handler.go", PackageImportPath: "example.com/project/api", Hash: "same"},
	}

	plan := DetectGoChanges("example.com/project", scanned, previous)
	if plan.FullReindex || len(plan.ImpactedPackages) != 2 {
		t.Fatalf("expected go version bump to scope to all packages, got %+v", plan)
	}
}

func TestDetectGoChangesMarksModuleRenameForFullReindex(t *testing.T) {
	scanned := []ScanFile{{RelPath: "go.mod", Hash: "new", IsModule: true, Identity: "example.com/new"}}
	previous := map[string]PreviousFile{"go.mod": {RelPath: "go.mod", Hash: "old", Identity: "example.com/old"}}

	plan := DetectGoChanges("example.com/new", scanned, previous)
	if !plan.FullReindex {
		t.Fatalf("expected module path change to force full reindex, got %+v", plan)
	}
}

func TestDetectPythonChangesTreatsProjectFilesAsNoOp(t *testing.T) {
	scanned := []ScanFile{
		{RelPath: "pyproject.toml", Hash: "new", IsModule: true, SemanticMeta: EncodeManifestMeta(ParsePyProjectManifestMeta([]byte("[project]\nname = \"demo-project\"\nrequires-python = \">=3.12\"\n")))},
		{RelPath: "src/app.py", Hash: "same"},
	}
	previous := map[string]PreviousFile{
		"pyproject.toml": {RelPath: "pyproject.toml", Hash: "old", SemanticMeta: EncodeManifestMeta(ParsePyProjectManifestMeta([]byte("[project]\nname = \"demo-project\"\nrequires-python = \">=3.11\"\n")))},
		"src/app.py":     {RelPath: "src/app.py", Hash: "same"},
	}

	plan := DetectPythonChanges("demo_project", scanned, previous)
	if plan.FullReindex || len(plan.ImpactedPackages) != 0 {
		t.Fatalf("expected python project metadata change to avoid full reindex, got %+v", plan)
	}
}

func TestDetectPythonChangesTreatsLocalDependencyShiftAsPartial(t *testing.T) {
	scanned := []ScanFile{
		{RelPath: "pyproject.toml", Hash: "new", IsModule: true, SemanticMeta: EncodeManifestMeta(ParsePyProjectManifestMeta([]byte("[project]\nname = \"demo-project\"\n[tool.poetry.dependencies]\ncore = { path = \"../core\" }\n")))},
		{RelPath: "src/app.py", Hash: "same", PackageImportPath: "app"},
		{RelPath: "src/pkg.py", Hash: "same", PackageImportPath: "pkg"},
	}
	previous := map[string]PreviousFile{
		"pyproject.toml": {RelPath: "pyproject.toml", Hash: "old", SemanticMeta: EncodeManifestMeta(ParsePyProjectManifestMeta([]byte("[project]\nname = \"demo-project\"\n")))},
		"src/app.py":     {RelPath: "src/app.py", Hash: "same", PackageImportPath: "app"},
		"src/pkg.py":     {RelPath: "src/pkg.py", Hash: "same", PackageImportPath: "pkg"},
	}

	plan := DetectPythonChanges("demo_project", scanned, previous)
	if plan.FullReindex || len(plan.ImpactedPackages) != 2 {
		t.Fatalf("expected python local dep shift to scope all packages, got %+v", plan)
	}
}

func TestDetectPythonChangesTreatsPackageRootShiftAsFull(t *testing.T) {
	scanned := []ScanFile{
		{RelPath: "pyproject.toml", Hash: "new", IsModule: true, SemanticMeta: EncodeManifestMeta(ParsePyProjectManifestMeta([]byte("[project]\nname = \"demo-project\"\n[tool.setuptools.packages.find]\nwhere = [\"lib\"]\n")))},
	}
	previous := map[string]PreviousFile{
		"pyproject.toml": {RelPath: "pyproject.toml", Hash: "old", SemanticMeta: EncodeManifestMeta(ParsePyProjectManifestMeta([]byte("[project]\nname = \"demo-project\"\n[tool.setuptools.packages.find]\nwhere = [\"src\"]\n")))},
	}

	plan := DetectPythonChanges("demo_project", scanned, previous)
	if !plan.FullReindex || len(plan.ManifestChanges) == 0 {
		t.Fatalf("expected python package-root shift to force full reindex, got %+v", plan)
	}
}

func TestDetectRustChangesTreatsCargoLockAsNoOp(t *testing.T) {
	scanned := []ScanFile{
		{RelPath: "Cargo.lock", Hash: "new", IsModule: true},
		{RelPath: "src/lib.rs", Hash: "same", IsRust: true, PackageImportPath: "demo"},
	}
	previous := map[string]PreviousFile{
		"Cargo.lock": {RelPath: "Cargo.lock", Hash: "old"},
		"src/lib.rs": {RelPath: "src/lib.rs", Hash: "same", PackageImportPath: "demo"},
	}

	plan := DetectRustChanges("demo", scanned, previous)
	if plan.FullReindex || len(plan.ImpactedPackages) != 0 {
		t.Fatalf("expected Cargo.lock change to avoid full reindex, got %+v", plan)
	}
}

func TestDetectRustChangesTreatsStableCrateManifestAsNoOp(t *testing.T) {
	scanned := []ScanFile{
		{RelPath: "crates/app/Cargo.toml", Hash: "new", IsModule: true, Identity: "rust:crate:app", SemanticMeta: EncodeManifestMeta(ParseCargoManifestMeta([]byte("[package]\nname = \"app\"\nedition = \"2021\"\n[dependencies]\nserde = \"1\"\n")))},
		{RelPath: "crates/app/src/lib.rs", Hash: "same", IsRust: true, PackageImportPath: "app"},
	}
	previous := map[string]PreviousFile{
		"crates/app/Cargo.toml": {RelPath: "crates/app/Cargo.toml", Hash: "old", Identity: "rust:crate:app", SemanticMeta: EncodeManifestMeta(ParseCargoManifestMeta([]byte("[package]\nname = \"app\"\nedition = \"2021\"\n[dependencies]\nserde = \"1.0\"\n")))},
		"crates/app/src/lib.rs": {RelPath: "crates/app/src/lib.rs", Hash: "same", PackageImportPath: "app"},
	}

	plan := DetectRustChanges("workspace", scanned, previous)
	if plan.FullReindex || len(plan.ImpactedPackages) != 0 {
		t.Fatalf("expected stable crate manifest change to avoid full reindex, got %+v", plan)
	}
}

func TestDetectRustChangesKeepsWorkspaceManifestConservative(t *testing.T) {
	scanned := []ScanFile{{RelPath: "Cargo.toml", Hash: "new", IsModule: true, Identity: "rust:workspace:", SemanticMeta: EncodeManifestMeta(ParseCargoManifestMeta([]byte("[workspace]\nmembers=[\"crates/*\"]\n")))}}
	previous := map[string]PreviousFile{"Cargo.toml": {RelPath: "Cargo.toml", Hash: "old", Identity: "rust:workspace:", SemanticMeta: EncodeManifestMeta(ParseCargoManifestMeta([]byte("[workspace]\nmembers=[\"crates/*\"]\n[workspace.dependencies]\nserde = \"1\"\n")))}}

	plan := DetectRustChanges("workspace", scanned, previous)
	if plan.FullReindex || len(plan.ImpactedPackages) != 0 {
		t.Fatalf("expected stable workspace manifest change to avoid full reindex, got %+v", plan)
	}
}

func TestDetectRustChangesTreatsCrateLocalDepsAsPackageScoped(t *testing.T) {
	scanned := []ScanFile{
		{RelPath: "crates/app/Cargo.toml", Hash: "new", IsModule: true, Identity: "rust:crate:app", SemanticMeta: EncodeManifestMeta(ParseCargoManifestMeta([]byte("[package]\nname = \"app\"\nedition = \"2021\"\n[dependencies]\ncore = { path = \"../core\" }\n")))},
		{RelPath: "crates/app/src/lib.rs", Hash: "same", IsRust: true, PackageImportPath: "app"},
		{RelPath: "crates/core/src/lib.rs", Hash: "same", IsRust: true, PackageImportPath: "core"},
	}
	previous := map[string]PreviousFile{
		"crates/app/Cargo.toml":  {RelPath: "crates/app/Cargo.toml", Hash: "old", IsTest: false, Identity: "rust:crate:app", SemanticMeta: EncodeManifestMeta(ParseCargoManifestMeta([]byte("[package]\nname = \"app\"\nedition = \"2021\"\n")))},
		"crates/app/src/lib.rs":  {RelPath: "crates/app/src/lib.rs", Hash: "same", PackageImportPath: "app"},
		"crates/core/src/lib.rs": {RelPath: "crates/core/src/lib.rs", Hash: "same", PackageImportPath: "core"},
	}

	plan := DetectRustChanges("workspace", scanned, previous)
	if plan.FullReindex || len(plan.ImpactedPackages) != 1 || plan.ImpactedPackages[0] != "app" {
		t.Fatalf("expected local cargo dep change to scope to crate package, got %+v", plan)
	}
}

func TestDetectRustChangesTreatsWorkspaceMembersAsFullReindex(t *testing.T) {
	scanned := []ScanFile{{RelPath: "Cargo.toml", Hash: "new", IsModule: true, Identity: "rust:workspace:", SemanticMeta: EncodeManifestMeta(ParseCargoManifestMeta([]byte("[workspace]\nmembers=[\"crates/*\", \"tools/*\"]\n")))}}
	previous := map[string]PreviousFile{"Cargo.toml": {RelPath: "Cargo.toml", Hash: "old", Identity: "rust:workspace:", SemanticMeta: EncodeManifestMeta(ParseCargoManifestMeta([]byte("[workspace]\nmembers=[\"crates/*\"]\n")))}}

	plan := DetectRustChanges("workspace", scanned, previous)
	if !plan.FullReindex || len(plan.ManifestChanges) == 0 {
		t.Fatalf("expected workspace member change to force full reindex, got %+v", plan)
	}
}
