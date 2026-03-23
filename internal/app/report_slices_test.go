package app

import (
	"strings"
	"testing"

	"github.com/vladimirkasterin/ctx/internal/storage"
)

func TestBuildReportSlicesExposeRiskSeamsAndLowTests(t *testing.T) {
	state := openIndexedAppState(t, map[string]string{
		"go.mod": "module example.com/report\n\ngo 1.26\n",
		"core/run.go": `package core

type Service struct{}

func Run() int {
	return helper() + 1
}

func helper() int {
	return 1
}

func (Service) Execute() int {
	return Run()
}
`,
		"api/handler.go": `package api

import "example.com/report/core"

func Handle() int {
	return core.Run()
}
`,
		"worker/job.go": `package worker

import "example.com/report/core"

func Job() int {
	return core.Run()
}
`,
		"cli/cmd.go": `package cli

import (
	"example.com/report/api"
	"example.com/report/core"
)

func Command() int {
	return api.Handle() + core.Run()
}
`,
	})

	report, err := state.Store.LoadReportView(24)
	if err != nil {
		t.Fatalf("LoadReportView returned error: %v", err)
	}
	watch, err := buildReportTestWatch(state.Store, report)
	if err != nil {
		t.Fatalf("buildReportTestWatch returned error: %v", err)
	}

	risky, err := buildReportSlice("risky", state.Store, report, watch, 6)
	if err != nil {
		t.Fatalf("buildReportSlice(risky) returned error: %v", err)
	}
	if !containsRankedSymbolQName(risky.Symbols, "example.com/report/core.Run") {
		t.Fatalf("expected risky slice to include core.Run, got %+v", risky.Symbols)
	}
	if !containsRankedPackageImport(risky.Packages, "example.com/report/core") {
		t.Fatalf("expected risky slice to include core package, got %+v", risky.Packages)
	}

	seams, err := buildReportSlice("seams", state.Store, report, watch, 6)
	if err != nil {
		t.Fatalf("buildReportSlice(seams) returned error: %v", err)
	}
	if !containsRankedSymbolQName(seams.Symbols, "example.com/report/core.Run") {
		t.Fatalf("expected seams slice to include core.Run, got %+v", seams.Symbols)
	}
	if !containsRankedPackageImport(seams.Packages, "example.com/report/api") {
		t.Fatalf("expected seams slice to include api package, got %+v", seams.Packages)
	}

	lowTest, err := buildReportSlice("low-tested", state.Store, report, watch, 6)
	if err != nil {
		t.Fatalf("buildReportSlice(low-tested) returned error: %v", err)
	}
	if !containsRankedSymbolQName(lowTest.Symbols, "example.com/report/core.Run") {
		t.Fatalf("expected low-tested slice to include core.Run, got %+v", lowTest.Symbols)
	}
	if !containsRankedPackageImport(lowTest.Packages, "example.com/report/core") {
		t.Fatalf("expected low-tested slice to include core package, got %+v", lowTest.Packages)
	}
}

func TestBuildReportSliceChangedSinceUsesLatestSnapshotDiff(t *testing.T) {
	t.Setenv("CTX_HOME", t.TempDir())

	root := t.TempDir()
	writeProjectStateFixture(t, root, "go.mod", "module example.com/reportdiff\n\ngo 1.26\n")
	writeProjectStateFixture(t, root, "pkg/service.go", `package pkg

func Run() int { return 1 }
`)
	writeProjectStateFixture(t, root, "pkg/service_test.go", `package pkg

import "testing"

func TestRun(t *testing.T) {
	if Run() == 0 {
		t.Fatal("unexpected zero")
	}
}
`)
	writeProjectStateFixture(t, root, "api/handler.go", `package api

import "example.com/reportdiff/pkg"

func Handle() int { return pkg.Run() }
`)

	state, err := openProjectState(root)
	if err != nil {
		t.Fatalf("openProjectState returned error: %v", err)
	}
	if _, committed, err := projectService.ApplySnapshot(state, "index", "initial", false); err != nil {
		_ = state.Close()
		t.Fatalf("ApplySnapshot initial returned error: %v", err)
	} else if !committed {
		_ = state.Close()
		t.Fatal("expected initial snapshot to commit")
	}
	if err := state.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	writeProjectStateFixture(t, root, "api/handler.go", `package api

import "example.com/reportdiff/pkg"

func Handle() int {
	value := pkg.Run()
	if value == 0 {
		return 1
	}
	return value
}
`)

	refreshed, err := openProjectState(root)
	if err != nil {
		t.Fatalf("openProjectState refresh returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = refreshed.Close()
	})
	if _, committed, err := projectService.ApplySnapshot(refreshed, "update", "change handler", false); err != nil {
		t.Fatalf("ApplySnapshot update returned error: %v", err)
	} else if !committed {
		t.Fatal("expected update snapshot to commit")
	}

	report, err := refreshed.Store.LoadReportView(24)
	if err != nil {
		t.Fatalf("LoadReportView returned error: %v", err)
	}
	watch, err := buildReportTestWatch(refreshed.Store, report)
	if err != nil {
		t.Fatalf("buildReportTestWatch returned error: %v", err)
	}

	slice, err := buildReportSlice("changed-since", refreshed.Store, report, watch, 6)
	if err != nil {
		t.Fatalf("buildReportSlice(changed-since) returned error: %v", err)
	}
	if !slice.HasDiff {
		t.Fatalf("expected changed-since slice to include diff, got %+v", slice)
	}
	if !containsString(slice.Diff.ChangedFiles, "api/handler.go") {
		t.Fatalf("expected changed file api/handler.go, got %+v", slice.Diff.ChangedFiles)
	}
	if !containsWeakChangedArea(slice.WeakChangedAreas, "api/handler.go") {
		t.Fatalf("expected weak changed area for api/handler.go, got %+v", slice.WeakChangedAreas)
	}
}

func TestShellReportSupportsHotspotsSlice(t *testing.T) {
	session, output := newIndexedShellSession(t, map[string]string{
		"go.mod": "module example.com/hotreport\n\ngo 1.26\n",
		"main.go": `package main

func main() {
	Run()
}
`,
		"service.go": `package main

func Run() {
	helper()
}

func helper() {}
`,
	})

	if err := session.showContextReport([]string{"hotspots"}); err != nil {
		t.Fatalf("showContextReport returned error: %v", err)
	}

	text := stripANSICodes(output.String())
	if !strings.Contains(text, "CTX Report · Hotspots") {
		t.Fatalf("expected hotspots report title, got:\n%s", text)
	}
	if !strings.Contains(text, "Hot Files") {
		t.Fatalf("expected hotspots slice content, got:\n%s", text)
	}
}

func containsRankedSymbolQName(values []storage.RankedSymbol, qname string) bool {
	for _, value := range values {
		if value.Symbol.QName == qname {
			return true
		}
	}
	return false
}

func containsRankedPackageImport(values []storage.RankedPackage, importPath string) bool {
	for _, value := range values {
		if value.Summary.ImportPath == importPath {
			return true
		}
	}
	return false
}

func containsWeakChangedArea(values []weakChangedArea, filePath string) bool {
	for _, value := range values {
		if value.FilePath == filePath {
			return true
		}
	}
	return false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
