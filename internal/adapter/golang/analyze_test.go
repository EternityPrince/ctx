package golang

import (
	"reflect"
	"testing"

	"github.com/vladimirkasterin/ctx/internal/codebase"
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
