package app

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/vladimirkasterin/ctx/internal/storage"
)

func TestRenderHumanReportExplainIncludesProvenance(t *testing.T) {
	view := storage.ReportView{
		Snapshot: storage.SnapshotInfo{
			ID:            7,
			CreatedAt:     time.Date(2026, time.March, 26, 12, 0, 0, 0, time.UTC),
			TotalPackages: 2,
			TotalFiles:    3,
			TotalSymbols:  4,
			TotalRefs:     2,
			TotalCalls:    1,
			TotalTests:    1,
		},
		ProvenanceNotes: []string{
			"Derived from indexed graph: packages=2 files=3 symbols=4 refs=2 calls=1 tests=1.",
		},
		TopPackages: []storage.RankedPackage{
			{
				Summary: storage.PackageSummary{
					ImportPath:  "example.com/project/pkg",
					FileCount:   2,
					SymbolCount: 3,
					TestCount:   1,
				},
				ReverseDepCount: 1,
				Score:           9,
				Provenance: []storage.ProvenanceItem{
					{Kind: "reverse_dep", Label: "example.com/project/api", Why: "reverse package dependency in local graph"},
				},
			},
		},
		TopFiles: []storage.RankedFile{
			{
				Summary: storage.FileSummary{
					FilePath:              "cmd/app/main.go",
					InboundCallCount:      1,
					InboundReferenceCount: 1,
					RelatedTestCount:      1,
					ReversePackageDeps:    1,
					RelevantSymbolCount:   2,
				},
				Score:      15,
				QualityWhy: []string{"main entrypoint file", "recently changed file"},
				TopSymbols: []string{"main", "Run"},
			},
		},
		TopFunctions: []storage.RankedSymbol{
			{
				Symbol: storage.SymbolMatch{
					QName:    "example.com/project/pkg.Service.Run",
					Kind:     "method",
					FilePath: "pkg/service.go",
					Line:     5,
				},
				CallerCount: 1,
				Score:       11,
				Provenance: []storage.ProvenanceItem{
					{Kind: "call", Label: "example.com/project/api.Handle", FilePath: "api/handler.go", Line: 6, Why: "static call edge from indexed call site"},
				},
			},
		},
		TopTypes: []storage.RankedSymbol{
			{
				Symbol: storage.SymbolMatch{
					QName:    "example.com/project/pkg.Service",
					Kind:     "struct",
					FilePath: "pkg/service.go",
					Line:     3,
				},
				ReferenceCount: 2,
				Score:          8,
				Provenance: []storage.ProvenanceItem{
					{Kind: "ref", Label: "example.com/project/api.Handle", FilePath: "api/handler.go", Line: 5, Why: "type reference in indexed source"},
				},
			},
		},
	}

	var out bytes.Buffer
	err := renderHumanReport(&out, "/tmp/project", "example.com/project", storage.ProjectStatus{ChangedNow: 1}, view, reportTestWatch{}, projectComposition{Go: 2}, true)
	if err != nil {
		t.Fatalf("renderHumanReport returned error: %v", err)
	}

	text := stripANSICodes(out.String())
	for _, expected := range []string{
		"Explain",
		"Precision:",
		"Provenance:",
		"Key Files",
		"why: main entrypoint file",
		"why: recently changed file",
		"Derived from indexed graph",
		"why: reverse package dependency in local graph",
		"why: static call edge from indexed call site",
		"why: type reference in indexed source",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected %q in report output, got:\n%s", expected, text)
		}
	}
}

func TestRenderHumanSymbolViewIncludesWhyLines(t *testing.T) {
	view := storage.SymbolView{
		Symbol: storage.SymbolMatch{
			SymbolKey:         "example.com/project/pkg.Service.Run",
			QName:             "example.com/project/pkg.Service.Run",
			PackageImportPath: "example.com/project/pkg",
			FilePath:          "pkg/service.go",
			Name:              "Run",
			Kind:              "method",
			Receiver:          "*Service",
			Signature:         "func (*Service) Run() error",
			Line:              5,
		},
		Package: storage.PackageSummary{
			ImportPath:  "example.com/project/pkg",
			DirPath:     "pkg",
			FileCount:   2,
			SymbolCount: 3,
			TestCount:   1,
		},
		Callers: []storage.RelatedSymbolView{
			{
				Symbol: storage.SymbolMatch{
					SymbolKey:         "example.com/project/api.Handle",
					QName:             "example.com/project/api.Handle",
					PackageImportPath: "example.com/project/api",
					FilePath:          "api/handler.go",
					Name:              "Handle",
					Kind:              "func",
					Signature:         "func Handle()",
					Line:              3,
				},
				UseFilePath: "api/handler.go",
				UseLine:     6,
				Relation:    "static",
				Why:         "static call edge from indexed call site",
			},
		},
		ReferencesIn: []storage.RefView{
			{
				Symbol: storage.SymbolMatch{
					SymbolKey:         "example.com/project/pkg.Service",
					QName:             "example.com/project/pkg.Service",
					PackageImportPath: "example.com/project/pkg",
					FilePath:          "pkg/service.go",
					Name:              "Service",
					Kind:              "struct",
					Signature:         "type Service struct{}",
					Line:              3,
				},
				UseFilePath: "pkg/service.go",
				UseLine:     5,
				Kind:        "receiver",
				Why:         "receiver reference in indexed source",
			},
		},
		Tests: []storage.TestView{
			{
				TestKey:    "example.com/project/pkg:TestServiceRun",
				Name:       "TestServiceRun",
				FilePath:   "pkg/service_test.go",
				Line:       3,
				Kind:       "func",
				LinkKind:   "receiver_match",
				Confidence: "high",
				Why:        "direct receiver match (high)",
			},
		},
	}

	guidance := symbolTestGuidance{
		ReadBefore:   view.Tests,
		DirectCount:  1,
		StrongDirect: 1,
		Signal:       "direct=1 strong",
	}

	var out bytes.Buffer
	err := renderHumanSymbolView(&out, "/tmp/project", "example.com/project", view, guidance, true)
	if err != nil {
		t.Fatalf("renderHumanSymbolView returned error: %v", err)
	}

	text := stripANSICodes(out.String())
	for _, expected := range []string{
		"Explain",
		"Quality score:",
		"why: static call edge from indexed call site",
		"why: receiver reference in indexed source",
		"why: direct receiver match (high)",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected %q in symbol output, got:\n%s", expected, text)
		}
	}
}

func TestRenderHumanImpactViewIncludesBlastRadiusSections(t *testing.T) {
	view := storage.ImpactView{
		Target: storage.SymbolMatch{
			SymbolKey:         "example.com/project/pkg.helper",
			QName:             "example.com/project/pkg.helper",
			PackageImportPath: "example.com/project/pkg",
			FilePath:          "pkg/service.go",
			Name:              "helper",
			Kind:              "func",
			Signature:         "func helper()",
			Line:              8,
		},
		Package: storage.PackageSummary{
			ImportPath:  "example.com/project/pkg",
			DirPath:     "pkg",
			FileCount:   3,
			SymbolCount: 3,
			TestCount:   2,
			ReverseDeps: []string{"example.com/project/api"},
		},
		DirectCallers: []storage.RelatedSymbolView{
			{
				Symbol: storage.SymbolMatch{
					SymbolKey:         "example.com/project/api.Handle",
					QName:             "example.com/project/api.Handle",
					PackageImportPath: "example.com/project/api",
					FilePath:          "api/handler.go",
					Name:              "Handle",
					Kind:              "func",
					Signature:         "func Handle()",
					Line:              3,
				},
				UseFilePath: "api/handler.go",
				UseLine:     6,
				Relation:    "static",
				Why:         "static call edge from indexed call site",
			},
		},
		InboundRefs: []storage.RefView{
			{
				Symbol: storage.SymbolMatch{
					SymbolKey:         "example.com/project/api.Handle",
					QName:             "example.com/project/api.Handle",
					PackageImportPath: "example.com/project/api",
					FilePath:          "api/handler.go",
					Name:              "Handle",
					Kind:              "func",
					Signature:         "func Handle()",
					Line:              3,
				},
				UseFilePath: "api/handler.go",
				UseLine:     5,
				Kind:        "func",
				Why:         "function reference in indexed source",
			},
		},
		CallerPackages:    []string{"example.com/project/api"},
		ReferencePackages: []string{"example.com/project/api"},
		BlastPackages:     []string{"example.com/project/api"},
		CallerPackageReasons: []storage.ImpactPackageReason{
			{
				PackageImportPath: "example.com/project/api",
				Why:               []string{"static call edge from indexed call site via example.com/project/api.Handle @ api/handler.go:6"},
			},
		},
		ReferencePackageReasons: []storage.ImpactPackageReason{
			{
				PackageImportPath: "example.com/project/api",
				Why:               []string{"function reference in indexed source via example.com/project/api.Handle @ api/handler.go:5"},
			},
		},
		BlastPackageReasons: []storage.ImpactPackageReason{
			{
				PackageImportPath: "example.com/project/api",
				Why: []string{
					"static call edge from indexed call site via example.com/project/api.Handle @ api/handler.go:6",
					"function reference in indexed source via example.com/project/api.Handle @ api/handler.go:5",
					"reverse package dependency in local graph",
				},
			},
		},
		BlastFileReasons: []storage.ImpactFileReason{
			{
				FilePath: "api/handler.go",
				Why: []string{
					"static call edge from indexed call site via example.com/project/api.Handle @ api/handler.go:6",
					"function reference in indexed source via example.com/project/api.Handle @ api/handler.go:5",
				},
			},
			{
				FilePath: "pkg/service_test.go",
				Why: []string{
					"direct symbol match (high) via TestHelper @ pkg/service_test.go:9",
				},
			},
		},
		ExpansionWhy: []string{
			"reverse package dependencies widen package blast radius (1 package(s))",
			"empirical co-change history widens impact beyond direct graph edges",
			"recent symbol delta biases impact toward recently changed caller/reference/test surface",
		},
		BlastFiles:        []string{"api/handler.go", "pkg/service_test.go"},
		EmpiricalPackages: []storage.CoChangeItem{{Label: "example.com/project/api", Count: 1, Frequency: 0.5}},
		EmpiricalFiles:    []storage.CoChangeItem{{Label: "api/handler.go", Count: 1, Frequency: 0.5}},
		Tests: []storage.TestView{
			{
				TestKey:    "example.com/project/pkg:TestHelper",
				Name:       "TestHelper",
				FilePath:   "pkg/service_test.go",
				Line:       9,
				Kind:       "func",
				LinkKind:   "direct",
				Confidence: "high",
				Why:        "direct symbol match (high)",
			},
		},
	}

	guidance := symbolTestGuidance{
		ReadBefore:   view.Tests,
		DirectCount:  1,
		StrongDirect: 1,
		Signal:       "direct=1 strong",
	}

	var out bytes.Buffer
	err := renderHumanImpactView(&out, "/tmp/project", "example.com/project", view, guidance, 3, true)
	if err != nil {
		t.Fatalf("renderHumanImpactView returned error: %v", err)
	}

	text := stripANSICodes(out.String())
	for _, expected := range []string{
		"Explain",
		"Blast radius:",
		"Inbound References",
		"Reference Packages",
		"Blast Packages",
		"Blast Files",
		"why: reverse package dependencies widen package blast radius (1 package(s))",
		"why: empirical co-change history widens impact beyond direct graph edges",
		"Empirical Packages",
		"Empirical Files",
		"why: static call edge from indexed call site via example.com/project/api.Handle @ api/handler.go:6",
		"why: function reference in indexed source via example.com/project/api.Handle @ api/handler.go:5",
		"why: direct symbol match (high) via TestHelper @ pkg/service_test.go:9",
		"api/handler.go (count=1 freq=0.50)",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected %q in impact output, got:\n%s", expected, text)
		}
	}
}
