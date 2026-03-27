package app

import (
	"os/exec"
	"strings"
	"testing"
)

func TestSearchCommandShowsFuzzySymbolMatches(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required for mixed search tests")
	}

	session, output := newIndexedShellSession(t, map[string]string{
		"go.mod": "module example.com/search\n\ngo 1.26\n",
		"main.go": `package main

func main() {}
`,
		"pkg/service.py": `class Service:
    def run(self) -> int:
        return helper()


def helper() -> int:
    return 1
`,
	})

	if err := session.showSearchCommand([]string{"Servce"}); err != nil {
		t.Fatalf("showSearchCommand returned error: %v", err)
	}

	text := stripANSICodes(output.String())
	if !strings.Contains(text, "Symbol Matches") {
		t.Fatalf("expected symbol matches section, got:\n%s", text)
	}
	if !strings.Contains(text, "Service") || !strings.Contains(text, "[fuzzy]") {
		t.Fatalf("expected fuzzy Service match, got:\n%s", text)
	}
	if !strings.Contains(text, "why: fuzzy typo match") {
		t.Fatalf("expected fuzzy explanation in search output, got:\n%s", text)
	}
	if !strings.Contains(text, "risk:") {
		t.Fatalf("expected compact risk hints in search output, got:\n%s", text)
	}
}

func TestSearchCommandShowsTextMatchesAndPromptFallback(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required for mixed search tests")
	}

	session, output := newIndexedShellSession(t, map[string]string{
		"go.mod": "module example.com/search\n\ngo 1.26\n",
		"main.go": `package main

func main() {}
`,
		"pkg/service.py": `class Service:
    def run(self) -> int:
        return helper()


def helper() -> int:
    return 1
`,
	})

	if _, err := session.handleWithStop("return helper"); err != nil {
		t.Fatalf("handleWithStop returned error: %v", err)
	}

	text := stripANSICodes(output.String())
	if !strings.Contains(text, "Search") || !strings.Contains(text, "Text Matches") {
		t.Fatalf("expected smart search fallback with text matches, got:\n%s", text)
	}
	if !strings.Contains(text, "pkg/service.py:3:9") {
		t.Fatalf("expected python text match to be listed, got:\n%s", text)
	}
}

func TestGrepCommandUsesRegexSearch(t *testing.T) {
	session, output := newIndexedShellSession(t, map[string]string{
		"go.mod": "module example.com/grep\n\ngo 1.26\n",
		"main.go": `package main

func main() {
	Run()
}

func Run() {}
`,
	})

	if err := session.showGrepCommand([]string{"Run\\("}); err != nil {
		t.Fatalf("showGrepCommand returned error: %v", err)
	}

	text := stripANSICodes(output.String())
	if !strings.Contains(text, "Mode: regex") || !strings.Contains(text, "Text Matches") {
		t.Fatalf("expected regex search output, got:\n%s", text)
	}
	if !strings.Contains(text, "main.go:4:2") {
		t.Fatalf("expected regex match location, got:\n%s", text)
	}
}

func TestSearchCommandGroupsTextMatchesByPackageAndFile(t *testing.T) {
	session, output := newIndexedShellSession(t, map[string]string{
		"go.mod": "module example.com/search\n\ngo 1.26\n",
		"pkg/service.go": `package pkg

const helperToken = "sharedtoken"

func Run() string {
	return "sharedtoken"
}
`,
		"api/handler.go": `package api

func Handle() string {
	return "sharedtoken"
}
`,
	})

	if err := session.showSearchCommand([]string{"text", "sharedtoken"}); err != nil {
		t.Fatalf("showSearchCommand returned error: %v", err)
	}

	text := stripANSICodes(output.String())
	if !strings.Contains(text, "Text Matches (3 across 2 files / 2 packages)") {
		t.Fatalf("expected grouped text search summary, got:\n%s", text)
	}
	if !strings.Contains(text, "package: pkg") || !strings.Contains(text, "package: api") {
		t.Fatalf("expected package grouping in text search, got:\n%s", text)
	}
	if !strings.Contains(text, "file: pkg/service.go (2/2)") || !strings.Contains(text, "file: api/handler.go (1/1)") {
		t.Fatalf("expected file grouping in text search, got:\n%s", text)
	}
	if !strings.Contains(text, "why: matches=2") || !strings.Contains(text, "why: matches=1") {
		t.Fatalf("expected ranking explanation for grouped text matches, got:\n%s", text)
	}
}

func TestSearchResultsShowInvestigationRoutesAndLenses(t *testing.T) {
	session, output := newIndexedShellSession(t, map[string]string{
		"go.mod": "module example.com/routes\n\ngo 1.26\n",
		"pkg/service.go": `package pkg

func Run() int {
	return helper()
}

func helper() int {
	return 1
}
`,
		"pkg/service_test.go": `package pkg

import "testing"

func TestRun(t *testing.T) {
	if Run() == 0 {
		t.Fatal("unexpected zero")
	}
}
`,
		"api/handler.go": `package api

import "example.com/routes/pkg"

func Handle() int {
	return pkg.Run()
}
`,
	})

	if err := session.showSearchCommand([]string{"Run"}); err != nil {
		t.Fatalf("showSearchCommand returned error: %v", err)
	}

	text := stripANSICodes(output.String())
	if !strings.Contains(text, "next: open 1") || !strings.Contains(text, "callers 1") || !strings.Contains(text, "tests 1") || !strings.Contains(text, "impact 1") {
		t.Fatalf("expected immediate investigation routes in search output, got:\n%s", text)
	}
	if !strings.Contains(text, "lenses:") || !strings.Contains(text, "lens verify 1") {
		t.Fatalf("expected named lenses in search output, got:\n%s", text)
	}
	if !strings.Contains(text, "risk: seam") {
		t.Fatalf("expected compact seam/blast risk hints in search output, got:\n%s", text)
	}
}

func TestSearchResultQuickTransitionsOpenInvestigationViews(t *testing.T) {
	session, output := newIndexedShellSession(t, map[string]string{
		"go.mod": "module example.com/routes\n\ngo 1.26\n",
		"pkg/service.go": `package pkg

func Run() int {
	return helper()
}

func helper() int {
	return 1
}
`,
		"pkg/service_test.go": `package pkg

import "testing"

func TestRun(t *testing.T) {
	if Run() == 0 {
		t.Fatal("unexpected zero")
	}
}
`,
		"api/handler.go": `package api

import "example.com/routes/pkg"

func Handle() int {
	return pkg.Run()
}
`,
	})

	if err := session.showSearchCommand([]string{"Run"}); err != nil {
		t.Fatalf("showSearchCommand returned error: %v", err)
	}
	if _, err := session.handleWithStop("lens verify 1"); err != nil {
		t.Fatalf("lens verify 1 returned error: %v", err)
	}
	if err := session.showSearchCommand([]string{"Run"}); err != nil {
		t.Fatalf("showSearchCommand returned error: %v", err)
	}
	if _, err := session.handleWithStop("callers 1"); err != nil {
		t.Fatalf("callers 1 returned error: %v", err)
	}

	text := stripANSICodes(output.String())
	if !strings.Contains(text, "Related Tests") || !strings.Contains(text, "TestRun") {
		t.Fatalf("expected lens verify <n> to open test investigation, got:\n%s", text)
	}
	if !strings.Contains(text, "Direct Callers") || !strings.Contains(text, "Handle") {
		t.Fatalf("expected callers <n> to open caller investigation, got:\n%s", text)
	}
}

func TestShellFileQuickRouteKeepsWholeFileFullView(t *testing.T) {
	session, output := newIndexedShellSession(t, map[string]string{
		"go.mod": "module example.com/fullfile\n\ngo 1.26\n",
		"pkg/service.go": `package pkg

func helper() int {
	return 1
}
`,
	})

	if err := session.showSearchCommand([]string{"helper"}); err != nil {
		t.Fatalf("showSearchCommand returned error: %v", err)
	}
	output.Reset()
	if _, err := session.handleWithStop("file 1"); err != nil {
		t.Fatalf("file 1 returned error: %v", err)
	}
	fileCard := stripANSICodes(output.String())
	if !strings.Contains(fileCard, "File Journey") {
		t.Fatalf("expected file journey screen, got:\n%s", fileCard)
	}

	output.Reset()
	if _, err := session.handleWithStop("full"); err != nil {
		t.Fatalf("full returned error: %v", err)
	}
	text := stripANSICodes(output.String())
	if !strings.Contains(text, "Full File") || !strings.Contains(text, "func helper() int") {
		t.Fatalf("expected whole-file full view after file <n>, got:\n%s", text)
	}
}

func TestShellFileQuickRouteKeepsIndexedFullBodyByNumber(t *testing.T) {
	session, output := newIndexedShellSession(t, map[string]string{
		"go.mod": "module example.com/fullbody\n\ngo 1.26\n",
		"pkg/service.go": `package pkg

func helper() int {
	return 1
}
`,
	})

	if err := session.showSearchCommand([]string{"helper"}); err != nil {
		t.Fatalf("showSearchCommand returned error: %v", err)
	}
	output.Reset()
	if _, err := session.handleWithStop("file 1"); err != nil {
		t.Fatalf("file 1 returned error: %v", err)
	}

	output.Reset()
	if _, err := session.handleWithStop("full 1"); err != nil {
		t.Fatalf("full 1 returned error: %v", err)
	}
	text := stripANSICodes(output.String())
	if !strings.Contains(text, "Full Entity Body") || !strings.Contains(text, "func helper() int") {
		t.Fatalf("expected indexed entity full view after file <n>, got:\n%s", text)
	}
}

func TestShellFileQuickRouteSyncsFocusedSymbolForSource(t *testing.T) {
	session, output := newIndexedShellSession(t, map[string]string{
		"go.mod": "module example.com/filefocus\n\ngo 1.26\n",
		"pkg/service.go": `package pkg

func helper() int {
	return 1
}
`,
	})

	if err := session.showSearchCommand([]string{"helper"}); err != nil {
		t.Fatalf("showSearchCommand returned error: %v", err)
	}
	output.Reset()
	if _, err := session.handleWithStop("file 1"); err != nil {
		t.Fatalf("file 1 returned error: %v", err)
	}

	output.Reset()
	if _, err := session.handleWithStop("source"); err != nil {
		t.Fatalf("source returned error: %v", err)
	}
	text := stripANSICodes(output.String())
	if !strings.Contains(text, "Source") || !strings.Contains(text, "func helper() int") {
		t.Fatalf("expected file journey focus to drive plain source, got:\n%s", text)
	}
}

func TestShellPythonFileQuickRouteKeepsFocusedSource(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required for python shell tests")
	}

	session, output := newIndexedShellSession(t, map[string]string{
		"pyproject.toml":  "[project]\nname = \"shell-python\"\n",
		"pkg/__init__.py": "",
		"pkg/service.py": `def helper() -> int:
    return 1
`,
	})

	if err := session.showSearchCommand([]string{"helper"}); err != nil {
		t.Fatalf("showSearchCommand returned error: %v", err)
	}
	output.Reset()
	if _, err := session.handleWithStop("file 1"); err != nil {
		t.Fatalf("file 1 returned error: %v", err)
	}

	output.Reset()
	if _, err := session.handleWithStop("source"); err != nil {
		t.Fatalf("source returned error: %v", err)
	}
	text := stripANSICodes(output.String())
	if !strings.Contains(text, "Source") || !strings.Contains(text, "def helper() -> int") {
		t.Fatalf("expected python file journey focus to drive source output, got:\n%s", text)
	}
}

func TestShellPythonImpactExplainsBlastPackageReasons(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required for python shell tests")
	}

	session, output := newIndexedShellSession(t, map[string]string{
		"pyproject.toml":  "[project]\nname = \"impact-python\"\n",
		"pkg/__init__.py": "from .service import Service as PublicService\n",
		"pkg/service.py": `class Service:
    def run(self) -> int:
        return 1
`,
		"app/__init__.py": "",
		"app/runner.py": `from __future__ import annotations
from pkg import PublicService
from typing import TYPE_CHECKING
import importlib

if TYPE_CHECKING:
    from pkg.service import Service

def execute_public() -> int:
    return PublicService().run()

def execute_dynamic() -> int:
    service_module = importlib.import_module("pkg.service")
    return service_module.Service().run()

def accepts(service: "Service") -> int:
    return 1
`,
	})

	if err := session.openSymbolKey("class|pkg.service|Service", true); err != nil {
		t.Fatalf("openSymbolKey returned error: %v", err)
	}
	output.Reset()
	if err := session.showImpact(""); err != nil {
		t.Fatalf("showImpact returned error: %v", err)
	}

	text := stripANSICodes(output.String())
	for _, expected := range []string{
		"Blast Packages",
		"why: dynamic importlib call edge from indexed call site",
		"why: re-exported call edge from indexed call site",
		"why: TYPE_CHECKING type reference in indexed source",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected %q in python impact output, got:\n%s", expected, text)
		}
	}
}

func TestSymbolJourneyShowsAdjacentRoutesAndNamedLenses(t *testing.T) {
	session, output := newIndexedShellSession(t, map[string]string{
		"go.mod": "module example.com/routes\n\ngo 1.26\n",
		"pkg/service.go": `package pkg

func Run() int {
	return helper()
}

func helper() int {
	return 1
}
`,
		"pkg/service_test.go": `package pkg

import "testing"

func TestRun(t *testing.T) {
	if Run() == 0 {
		t.Fatal("unexpected zero")
	}
}
`,
		"api/handler.go": `package api

import "example.com/routes/pkg"

func Handle() int {
	return pkg.Run()
}
`,
	})

	if err := session.showSymbol("pkg.Run", true); err != nil {
		t.Fatalf("showSymbol returned error: %v", err)
	}

	text := stripANSICodes(output.String())
	if !strings.Contains(text, "Adjacent Routes") || !strings.Contains(text, "Saved Views / Named Lenses") {
		t.Fatalf("expected symbol journey to show neighboring routes and lenses, got:\n%s", text)
	}
	if !strings.Contains(text, "Lens verify") || !strings.Contains(text, "Lens incoming") {
		t.Fatalf("expected named lenses to be listed on the symbol card, got:\n%s", text)
	}
	if !strings.Contains(text, "Risk:") || !strings.Contains(text, "seam") {
		t.Fatalf("expected symbol journey to show compact risk hints, got:\n%s", text)
	}
}

func TestTestsCommandShowsReadBeforeChangePlan(t *testing.T) {
	session, output := newIndexedShellSession(t, map[string]string{
		"go.mod": "module example.com/testshell\n\ngo 1.26\n",
		"pkg/service.go": `package pkg

func Run() int {
	return helper()
}

func helper() int {
	return 1
}
`,
		"pkg/service_test.go": `package pkg

import "testing"

func TestHelper(t *testing.T) {
	if helper() == 0 {
		t.Fatal("unexpected zero")
	}
}
`,
		"api/handler.go": `package api

import "example.com/testshell/pkg"

func Handle() int {
	return pkg.Run()
}
`,
		"api/handler_test.go": `package api

import "testing"

func TestHandle(t *testing.T) {
	if Handle() == 0 {
		t.Fatal("unexpected zero")
	}
}
`,
	})

	if err := session.showSymbol("pkg.Run", true); err != nil {
		t.Fatalf("showSymbol returned error: %v", err)
	}
	output.Reset()
	if _, err := session.handleWithStop("tests"); err != nil {
		t.Fatalf("tests returned error: %v", err)
	}

	text := stripANSICodes(output.String())
	if !strings.Contains(text, "Tests To Read Before Change") || !strings.Contains(text, "Coverage posture: nearby-only=2") {
		t.Fatalf("expected ranked read-before-change tests plan, got:\n%s", text)
	}
	if !strings.Contains(text, "covers caller Handle") || !strings.Contains(text, "covers callee helper") {
		t.Fatalf("expected caller/callee why hints in test plan, got:\n%s", text)
	}
}
