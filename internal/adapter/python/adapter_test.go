package python

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/vladimirkasterin/ctx/internal/codebase"
	"github.com/vladimirkasterin/ctx/internal/project"
)

func TestAnalyzeBuildsPythonSymbolGraph(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required for python adapter tests")
	}

	root := t.TempDir()
	writePythonFixture(t, root, "pyproject.toml", "[project]\nname = \"demo-project\"\nrequires-python = \">=3.11\"\n")
	writePythonFixture(t, root, "pkg/__init__.py", "")
	writePythonFixture(t, root, "app/__init__.py", "")
	writePythonFixture(t, root, "pkg/service.py", `class Service:
    def run(self, value: int) -> int:
        return self.normalize(value)

    def normalize(self, value: int) -> int:
        return helper(value)


def helper(value: int) -> int:
    return value + 1
`)
	writePythonFixture(t, root, "app/runner.py", `from pkg.service import Service, helper


def execute() -> int:
    return helper(1)


def execute_method() -> int:
    return Service.run(Service(), 1)
`)
	writePythonFixture(t, root, "pkg/test_service.py", `def test_service_run():
    assert True


class TestService:
    def test_normalize(self):
        assert True
`)

	info, err := project.Resolve(root)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if info.Language != "python" {
		t.Fatalf("expected python language, got %q", info.Language)
	}
	if info.ModulePath != "demo_project" {
		t.Fatalf("expected normalized project name, got %q", info.ModulePath)
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

	symbolKeys := make(map[string]string, len(result.Symbols))
	for _, symbol := range result.Symbols {
		symbolKeys[symbol.QName] = symbol.SymbolKey
	}

	assertPackagePaths(t, result.Packages, []string{"app", "pkg"})
	assertSymbolKinds(t, result.Symbols, map[string]string{
		"app.runner.execute":            "func",
		"app.runner.execute_method":     "func",
		"pkg.service.Service":           "class",
		"pkg.service.Service.run":       "method",
		"pkg.service.Service.normalize": "method",
		"pkg.service.helper":            "func",
	})
	assertCallEdge(t, result.Calls, symbolKeys, "app.runner.execute", "pkg.service.helper")
	assertCallEdge(t, result.Calls, symbolKeys, "app.runner.execute_method", "pkg.service.Service.run")
	assertCallEdge(t, result.Calls, symbolKeys, "pkg.service.Service.run", "pkg.service.Service.normalize")
	assertCallEdge(t, result.Calls, symbolKeys, "pkg.service.Service.normalize", "pkg.service.helper")
	assertDependency(t, result.Dependencies, "app", "pkg")
	assertTestLink(t, result.TestLinks, symbolKeys, "test|pkg|pkg/test_service.py|test_service_run", "pkg.service.Service.run", "receiver_match")
	assertTestLink(t, result.TestLinks, symbolKeys, "test|pkg|pkg/test_service.py|Service.test_normalize", "pkg.service.Service.normalize", "receiver_match")
}

func TestAnalyzeTracksInstanceAssignmentsAliasesAndRelativeImports(t *testing.T) {
	result, symbolKeys := analyzePythonFixture(t, map[string]string{
		"pyproject.toml":  "[project]\nname = \"instance-demo\"\n",
		"pkg/__init__.py": "",
		"pkg/service.py": `class Service:
    def run(self) -> int:
        return 1
`,
		"pkg/worker.py": `from .service import Service as S


def execute() -> int:
    service = S()
    return service.run()
`,
	})

	assertCallEdge(t, result.Calls, symbolKeys, "pkg.worker.execute", "pkg.service.Service")
	assertCallEdge(t, result.Calls, symbolKeys, "pkg.worker.execute", "pkg.service.Service.run")
}

func TestAnalyzeTracksAnnotatedParamsAndSelfAttributesAcrossMethods(t *testing.T) {
	result, symbolKeys := analyzePythonFixture(t, map[string]string{
		"pyproject.toml":  "[project]\nname = \"attr-demo\"\n",
		"pkg/__init__.py": "",
		"pkg/service.py": `class Service:
    def run(self) -> int:
        return 1
`,
		"pkg/runner.py": `from pkg.service import Service


class Runner:
    def __init__(self, service: Service):
        self.service = service

    def execute(self) -> int:
        local_service = self.service
        return local_service.run()
`,
	})

	assertCallEdge(t, result.Calls, symbolKeys, "pkg.runner.Runner.execute", "pkg.service.Service.run")
	assertReference(t, result.References, symbolKeys, "pkg.runner.Runner.execute", "pkg.service.Service.run")
}

func TestAnalyzeFiltersToRequestedPackages(t *testing.T) {
	root := t.TempDir()
	writePythonFixture(t, root, "pyproject.toml", "[project]\nname = \"filter-demo\"\n")
	writePythonFixture(t, root, "pkg/__init__.py", "")
	writePythonFixture(t, root, "pkg/service.py", "def run():\n    return 1\n")
	writePythonFixture(t, root, "app/__init__.py", "")
	writePythonFixture(t, root, "app/runner.py", "def execute():\n    return 1\n")

	info, err := project.Resolve(root)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}

	adapter := NewAdapter()
	scanned, err := adapter.Scan(root)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	result, err := adapter.Analyze(info, codebase.ScanMap(scanned), []string{"pkg"})
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	if len(result.Packages) != 1 || result.Packages[0].ImportPath != "pkg" {
		t.Fatalf("expected only pkg package in filtered result, got %+v", result.Packages)
	}
	if _, ok := result.ImpactedPackage["pkg"]; !ok {
		t.Fatalf("expected pkg to be marked as impacted, got %+v", result.ImpactedPackage)
	}
	if _, ok := result.ImpactedPackage["app"]; ok {
		t.Fatalf("did not expect app to be marked as impacted in filtered result, got %+v", result.ImpactedPackage)
	}
}

func TestAnalyzeUsesConfiguredSourceRootsForModuleAndPackageNames(t *testing.T) {
	result, symbolKeys := analyzePythonFixture(t, map[string]string{
		"pyproject.toml":      "[project]\nname = \"root-demo\"\n\n[tool.setuptools.packages.find]\nwhere = [\"lib\"]\n",
		"lib/pkg/__init__.py": "",
		"lib/pkg/service.py": `def run() -> int:
    return 1
`,
		"lib/app/__init__.py": "",
		"lib/app/runner.py": `from pkg.service import run

def execute() -> int:
    return run()
`,
	})

	assertPackagePaths(t, result.Packages, []string{"app", "pkg"})
	assertSymbolKinds(t, result.Symbols, map[string]string{
		"app.runner.execute": "func",
		"pkg.service.run":    "func",
	})
	assertCallEdge(t, result.Calls, symbolKeys, "app.runner.execute", "pkg.service.run")
	assertDependency(t, result.Dependencies, "app", "pkg")
}

func TestAnalyzeTracksNamespacePackageImportsWithoutInitFiles(t *testing.T) {
	result, symbolKeys := analyzePythonFixture(t, map[string]string{
		"pyproject.toml": "[project]\nname = \"ns-demo\"\n\n[tool.setuptools.packages.find]\nwhere = [\"lib\"]\n",
		"lib/pkg/service.py": `def run() -> int:
    return 1
`,
		"lib/app/runner.py": `import pkg.service

def execute() -> int:
    return pkg.service.run()
`,
	})

	assertPackagePaths(t, result.Packages, []string{"app", "pkg"})
	assertCallEdge(t, result.Calls, symbolKeys, "app.runner.execute", "pkg.service.run")
	assertDependency(t, result.Dependencies, "app", "pkg")
}

func TestAnalyzeTracksNestedImportsAsDependencies(t *testing.T) {
	result, symbolKeys := analyzePythonFixture(t, map[string]string{
		"pyproject.toml":  "[project]\nname = \"nested-demo\"\n",
		"pkg/__init__.py": "",
		"pkg/service.py": `def run() -> int:
    return 1
`,
		"app/__init__.py": "",
		"app/runner.py": `def execute() -> int:
    from pkg.service import run
    return run()
`,
	})

	assertCallEdge(t, result.Calls, symbolKeys, "app.runner.execute", "pkg.service.run")
	assertDependency(t, result.Dependencies, "app", "pkg")
}

func TestAnalyzeTracksReexportChainsThroughPackageInit(t *testing.T) {
	result, symbolKeys := analyzePythonFixture(t, map[string]string{
		"pyproject.toml":  "[project]\nname = \"reexport-demo\"\n",
		"pkg/__init__.py": "from .service import run as public_run\n__all__ = [\"public_run\"]\n",
		"pkg/service.py": `def run() -> int:
    return 1
`,
		"app/__init__.py": "",
		"app/runner.py": `from pkg import public_run

def execute() -> int:
    return public_run()
`,
	})

	assertCallEdge(t, result.Calls, symbolKeys, "app.runner.execute", "pkg.service.run")
	assertCallDispatch(t, result.Calls, symbolKeys, "app.runner.execute", "pkg.service.run", "reexport")
	assertDependency(t, result.Dependencies, "app", "pkg")
}

func TestAnalyzeTracksStarReexportsViaAll(t *testing.T) {
	result, symbolKeys := analyzePythonFixture(t, map[string]string{
		"pyproject.toml":  "[project]\nname = \"all-demo\"\n",
		"pkg/__init__.py": "from .service import *\n__all__ = [\"run\"]\n",
		"pkg/service.py": `def run() -> int:
    return 1
`,
		"app/__init__.py": "",
		"app/runner.py": `from pkg import run

def execute() -> int:
    return run()
`,
	})

	assertCallEdge(t, result.Calls, symbolKeys, "app.runner.execute", "pkg.service.run")
	assertDependency(t, result.Dependencies, "app", "pkg")
}

func TestAnalyzeTracksTypeCheckingImportsAsDepsAndRefs(t *testing.T) {
	result, symbolKeys := analyzePythonFixture(t, map[string]string{
		"pyproject.toml":  "[project]\nname = \"typing-demo\"\n",
		"pkg/__init__.py": "",
		"pkg/service.py": `class Service:
    def run(self) -> int:
        return 1
`,
		"app/__init__.py": "",
		"app/runner.py": `from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from pkg.service import Service

def accepts(service: Service) -> int:
    return 1
`,
	})

	assertDependency(t, result.Dependencies, "app", "pkg")
	assertReference(t, result.References, symbolKeys, "app.runner.accepts", "pkg.service.Service")
}

func TestAnalyzeSkipsUnparseablePythonFilesInsteadOfAborting(t *testing.T) {
	result, symbolKeys := analyzePythonFixture(t, map[string]string{
		"pyproject.toml":  "[project]\nname = \"skip-demo\"\n",
		"pkg/__init__.py": "",
		"pkg/good.py": `def run() -> int:
    return 1
`,
		"pkg/bad.py":      "✨\n",
		"app/__init__.py": "",
		"app/runner.py": `from pkg.good import run

def execute() -> int:
    return run()
`,
	})

	assertPackagePaths(t, result.Packages, []string{"app", "pkg"})
	assertCallEdge(t, result.Calls, symbolKeys, "app.runner.execute", "pkg.good.run")
	assertDependency(t, result.Dependencies, "app", "pkg")
}

func TestAnalyzeTracksModuleExportAssignments(t *testing.T) {
	result, symbolKeys := analyzePythonFixture(t, map[string]string{
		"pyproject.toml":  "[project]\nname = \"alias-demo\"\n",
		"pkg/__init__.py": "from .service import run\npublic_run = run\n",
		"pkg/service.py": `def run() -> int:
    return 1
`,
		"app/__init__.py": "",
		"app/runner.py": `from pkg import public_run

def execute() -> int:
    return public_run()
`,
	})

	assertCallEdge(t, result.Calls, symbolKeys, "app.runner.execute", "pkg.service.run")
	assertDependency(t, result.Dependencies, "app", "pkg")
}

func TestAnalyzeTracksStringAnnotationsAndTypeCheckingCalls(t *testing.T) {
	result, symbolKeys := analyzePythonFixture(t, map[string]string{
		"pyproject.toml":  "[project]\nname = \"strings-demo\"\n",
		"pkg/__init__.py": "",
		"pkg/service.py": `class Service:
    def run(self) -> int:
        return 1
`,
		"app/__init__.py": "",
		"app/runner.py": `from __future__ import annotations
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from pkg.service import Service

def execute(service: "Service") -> int:
    return service.run()
`,
	})

	assertDependency(t, result.Dependencies, "app", "pkg")
	assertReference(t, result.References, symbolKeys, "app.runner.execute", "pkg.service.Service")
	assertReferenceKind(t, result.References, symbolKeys, "app.runner.execute", "pkg.service.Service", "type_checking_type")
	assertCallEdge(t, result.Calls, symbolKeys, "app.runner.execute", "pkg.service.Service.run")
}

func TestAnalyzeTracksImportlibDynamicImports(t *testing.T) {
	result, symbolKeys := analyzePythonFixture(t, map[string]string{
		"pyproject.toml":  "[project]\nname = \"importlib-demo\"\n",
		"pkg/__init__.py": "",
		"pkg/service.py": `def run() -> int:
    return 1
`,
		"app/__init__.py": "",
		"app/runner.py": `import importlib
from importlib import import_module as load

def execute() -> int:
    service = importlib.import_module("pkg.service")
    return service.run()

def execute_alias() -> int:
    service = load("pkg.service")
    return service.run()
`,
	})

	assertDependency(t, result.Dependencies, "app", "pkg")
	assertCallEdge(t, result.Calls, symbolKeys, "app.runner.execute", "pkg.service.run")
	assertCallEdge(t, result.Calls, symbolKeys, "app.runner.execute_alias", "pkg.service.run")
	assertCallDispatch(t, result.Calls, symbolKeys, "app.runner.execute", "pkg.service.run", "dynamic_import")
	assertCallDispatch(t, result.Calls, symbolKeys, "app.runner.execute_alias", "pkg.service.run", "dynamic_import")
}

func TestAnalyzeExtractsAnalyzerBackedFlowFacts(t *testing.T) {
	result, symbolKeys := analyzePythonFixture(t, map[string]string{
		"pyproject.toml":  "[project]\nname = \"flow-demo\"\n",
		"pkg/__init__.py": "",
		"pkg/service.py": `class Service:
    def normalize(self, value: str) -> str:
        return value

    def run(self, value: str) -> str:
        return self.normalize(value)
`,
	})

	assertFlow(t, result.Flows, symbolKeys, "pkg.service.Service.run", "receiver_to_call", "Service", "pkg.service.Service.normalize")
	assertFlow(t, result.Flows, symbolKeys, "pkg.service.Service.run", "param_to_call", "value", "pkg.service.Service.normalize")
	assertFlow(t, result.Flows, symbolKeys, "pkg.service.Service.run", "call_to_return", "pkg.service.Service.normalize", "return")
}

func analyzePythonFixture(t *testing.T, files map[string]string) (*codebase.Result, map[string]string) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required for python adapter tests")
	}

	root := t.TempDir()
	for relPath, content := range files {
		writePythonFixture(t, root, relPath, content)
	}

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

	symbolKeys := make(map[string]string, len(result.Symbols))
	for _, symbol := range result.Symbols {
		symbolKeys[symbol.QName] = symbol.SymbolKey
	}
	return result, symbolKeys
}

func writePythonFixture(t *testing.T, root, relPath, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writePythonFixtureBytes(t *testing.T, root, relPath string, content []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertPackagePaths(t *testing.T, packages []codebase.PackageFact, want []string) {
	t.Helper()
	got := make([]string, 0, len(packages))
	for _, pkg := range packages {
		got = append(got, pkg.ImportPath)
	}
	if len(got) != len(want) {
		t.Fatalf("unexpected packages: got %v want %v", got, want)
	}
	for idx := range want {
		if got[idx] != want[idx] {
			t.Fatalf("unexpected packages: got %v want %v", got, want)
		}
	}
}

func assertSymbolKinds(t *testing.T, symbols []codebase.SymbolFact, want map[string]string) {
	t.Helper()
	got := make(map[string]string, len(symbols))
	for _, symbol := range symbols {
		got[symbol.QName] = symbol.Kind
	}
	for qname, kind := range want {
		if got[qname] != kind {
			t.Fatalf("unexpected symbol kind for %s: got %q want %q", qname, got[qname], kind)
		}
	}
}

func assertCallEdge(t *testing.T, calls []codebase.CallFact, symbolKeys map[string]string, callerQName, calleeQName string) {
	t.Helper()

	callerKey := symbolKeys[callerQName]
	calleeKey := symbolKeys[calleeQName]

	for _, call := range calls {
		if call.CallerSymbolKey == callerKey && call.CalleeSymbolKey == calleeKey {
			return
		}
	}
	t.Fatalf("expected call edge %s -> %s, got %+v", callerQName, calleeQName, calls)
}

func assertCallDispatch(t *testing.T, calls []codebase.CallFact, symbolKeys map[string]string, callerQName, calleeQName, dispatch string) {
	t.Helper()

	callerKey := symbolKeys[callerQName]
	calleeKey := symbolKeys[calleeQName]

	for _, call := range calls {
		if call.CallerSymbolKey == callerKey && call.CalleeSymbolKey == calleeKey {
			if call.Dispatch != dispatch {
				t.Fatalf("unexpected call dispatch for %s -> %s: got %q want %q", callerQName, calleeQName, call.Dispatch, dispatch)
			}
			return
		}
	}
	t.Fatalf("expected call edge %s -> %s, got %+v", callerQName, calleeQName, calls)
}

func assertDependency(t *testing.T, deps []codebase.DependencyFact, fromPackage, toPackage string) {
	t.Helper()
	for _, dep := range deps {
		if dep.FromPackageImportPath == fromPackage && dep.ToPackageImportPath == toPackage {
			return
		}
	}
	t.Fatalf("expected dependency %s -> %s, got %+v", fromPackage, toPackage, deps)
}

func assertReference(t *testing.T, refs []codebase.ReferenceFact, symbolKeys map[string]string, fromQName, toQName string) {
	t.Helper()
	fromKey := symbolKeys[fromQName]
	toKey := symbolKeys[toQName]
	for _, ref := range refs {
		if ref.FromSymbolKey == fromKey && ref.ToSymbolKey == toKey {
			return
		}
	}
	t.Fatalf("expected reference %s -> %s, got %+v", fromQName, toQName, refs)
}

func assertReferenceKind(t *testing.T, refs []codebase.ReferenceFact, symbolKeys map[string]string, fromQName, toQName, kind string) {
	t.Helper()
	fromKey := symbolKeys[fromQName]
	toKey := symbolKeys[toQName]
	for _, ref := range refs {
		if ref.FromSymbolKey == fromKey && ref.ToSymbolKey == toKey {
			if ref.Kind != kind {
				t.Fatalf("unexpected reference kind for %s -> %s: got %q want %q", fromQName, toQName, ref.Kind, kind)
			}
			return
		}
	}
	t.Fatalf("expected reference %s -> %s, got %+v", fromQName, toQName, refs)
}

func assertTestLink(t *testing.T, links []codebase.TestLinkFact, symbolKeys map[string]string, testKey, symbolQName, linkKind string) {
	t.Helper()
	symbolKey := symbolKeys[symbolQName]
	for _, link := range links {
		if link.TestKey == testKey && link.SymbolKey == symbolKey && link.LinkKind == linkKind {
			return
		}
	}
	t.Fatalf("expected test link %s -> %s (%s), got %+v", testKey, symbolQName, linkKind, links)
}

func assertFlow(t *testing.T, flows []codebase.FlowFact, symbolKeys map[string]string, ownerQName, kind, sourceLabel, target string) {
	t.Helper()
	ownerKey := symbolKeys[ownerQName]
	for _, flow := range flows {
		if flow.OwnerSymbolKey != ownerKey || flow.Kind != kind || flow.SourceLabel != sourceLabel {
			continue
		}
		if flow.TargetSymbolKey == target || flow.TargetLabel == target {
			return
		}
	}
	t.Fatalf("expected flow %s %s %s -> %s, got %+v", ownerQName, kind, sourceLabel, target, flows)
}
