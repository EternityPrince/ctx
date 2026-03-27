package golang

import (
	"reflect"
	"strings"
	"testing"

	"github.com/vladimirkasterin/ctx/internal/codebase"
	"github.com/vladimirkasterin/ctx/internal/project"
)

func TestDefaultLoadPatternsUsesLocalPackagesFromScannedFiles(t *testing.T) {
	patterns := defaultLoadPatterns(map[string]codebase.ScanFile{
		"main.go":                 {RelPath: "main.go", IsGo: true},
		"pkg/service/service.go":  {RelPath: "pkg/service/service.go", IsGo: true},
		"pkg/service/service.go~": {RelPath: "pkg/service/service.go~"},
		"pkg/service/service_test.go": {
			RelPath: "pkg/service/service_test.go",
			IsGo:    true,
			IsTest:  true,
		},
	})

	want := []string{
		".",
		"./pkg/service",
	}
	if !reflect.DeepEqual(patterns, want) {
		t.Fatalf("unexpected patterns: got %v want %v", patterns, want)
	}
}

func TestAnalyzeContinuesWhenOneLocalPackageIsBroken(t *testing.T) {
	root := t.TempDir()

	mustWriteScanFile(t, root+"/go.mod", "module example.com/project\n\ngo 1.26\n")
	mustWriteScanFile(t, root+"/good/run.go", "package good\n\nfunc Run() int { return 1 }\n")
	mustWriteScanFile(t, root+"/broken/bad.go", "package broken\n\nfunc Broken(\n")

	info, err := project.Resolve(root)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}

	adapter := NewAdapter()
	scanned, err := adapter.Scan(root)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	result, err := adapter.Analyze(info, codebase.ScanMap(scanned), nil)
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}
	if len(result.Packages) == 0 {
		t.Fatalf("expected at least one indexed package, got %+v", result)
	}
	if result.Packages[0].ImportPath != "example.com/project/good" {
		t.Fatalf("expected good package to be indexed, got %+v", result.Packages)
	}
	for _, pkg := range result.Packages {
		if pkg.ImportPath == "example.com/project/broken" {
			t.Fatalf("did not expect broken package in indexed result, got %+v", result.Packages)
		}
	}
}

func TestAnalyzeFailsWhenAllLocalPackagesAreBroken(t *testing.T) {
	root := t.TempDir()

	mustWriteScanFile(t, root+"/go.mod", "module example.com/project\n\ngo 1.26\n")
	mustWriteScanFile(t, root+"/broken/bad.go", "package broken\n\nfunc Broken(\n")

	info, err := project.Resolve(root)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}

	adapter := NewAdapter()
	scanned, err := adapter.Scan(root)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	_, err = adapter.Analyze(info, codebase.ScanMap(scanned), nil)
	if err == nil {
		t.Fatal("expected Analyze to fail when all local packages are broken")
	}
	if !strings.Contains(err.Error(), "package loading returned errors") {
		t.Fatalf("unexpected error: %v", err)
	}
}
