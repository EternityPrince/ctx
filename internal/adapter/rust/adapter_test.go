package rust

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vladimirkasterin/ctx/internal/codebase"
	"github.com/vladimirkasterin/ctx/internal/project"
)

func TestScanRecognizesRustWorkspaceFilesAndSkipsTarget(t *testing.T) {
	root := t.TempDir()
	writeRustFixture(t, root, "Cargo.toml", "[workspace]\nmembers = [\"crates/*\"]\n")
	writeRustFixture(t, root, "crates/app/Cargo.toml", "[package]\nname = \"app\"\nedition = \"2021\"\n")
	writeRustFixture(t, root, "crates/app/src/lib.rs", "pub fn run() {}\n")
	writeRustFixture(t, root, "crates/app/src/http/router.rs", "pub fn route() {}\n")
	writeRustFixture(t, root, "target/debug/generated.rs", "fn ignored() {}\n")

	adapter := NewAdapter()
	files, err := adapter.Scan(root)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	byPath := make(map[string]codebase.ScanFile)
	for _, file := range files {
		byPath[file.RelPath] = file
	}

	if _, ok := byPath["target/debug/generated.rs"]; ok {
		t.Fatalf("expected rust target file to be skipped, got %+v", byPath["target/debug/generated.rs"])
	}
	if file, ok := byPath["Cargo.toml"]; !ok || file.PackageImportPath != "" || file.Identity != "rust:workspace:" {
		t.Fatalf("expected workspace Cargo.toml to keep workspace identity only, got %+v", file)
	}
	if file, ok := byPath["crates/app/Cargo.toml"]; !ok || !file.IsModule {
		t.Fatalf("expected crate Cargo.toml to be scanned as module file, got %+v", file)
	}
	if file, ok := byPath["crates/app/Cargo.toml"]; !ok || file.PackageImportPath != "" || file.Identity != "rust:crate:app" {
		t.Fatalf("expected crate Cargo.toml to retain crate identity only, got %+v", file)
	}
	if file, ok := byPath["crates/app/src/lib.rs"]; !ok || file.PackageImportPath != "app" {
		t.Fatalf("expected crate root package path app, got %+v", file)
	}
	if file, ok := byPath["crates/app/src/http/router.rs"]; !ok || file.PackageImportPath != "app::http::router" {
		t.Fatalf("expected nested rust package path, got %+v", file)
	}
}

func TestAnalyzeBuildsRustPackagesSymbolsAndTests(t *testing.T) {
	root := t.TempDir()
	writeRustFixture(t, root, "Cargo.toml", "[package]\nname = \"demo\"\nedition = \"2021\"\n")
	writeRustFixture(t, root, "src/lib.rs", `pub struct Service;

impl Service {
    pub fn run(&self) {
        helper();
    }
}

pub fn helper() {}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn service_run() {
        let service: Service = Service;
        service.run();
    }
}
`)
	writeRustFixture(t, root, "tests/integration.rs", `#[test]
fn integration() {}
`)

	adapter := NewAdapter()
	scanned, err := adapter.Scan(root)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	result, err := adapter.Analyze(project.Info{
		Root:       root,
		ModulePath: "demo",
		GoVersion:  "2021",
		Language:   "rust",
	}, codebase.ScanMap(scanned), nil)
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	requireRustPackage(t, result.Packages, "demo")
	requireRustPackage(t, result.Packages, "demo::tests::integration")
	requireRustSymbol(t, result.Symbols, "demo::Service", "struct")
	requireRustSymbol(t, result.Symbols, "demo::Service::run", "method")
	requireRustSymbol(t, result.Symbols, "demo::helper", "func")
	requireRustSymbol(t, result.Symbols, "demo::tests::service_run", "func")

	symbolKeys := rustSymbolKeys(result.Symbols)
	assertRustCallEdge(t, result.Calls, symbolKeys, "demo::Service::run", "demo::helper")
	assertRustCallEdge(t, result.Calls, symbolKeys, "demo::tests::service_run", "demo::Service::run")
	if len(result.Tests) != 2 {
		t.Fatalf("expected integration and unit tests to be indexed, got %+v", result.Tests)
	}
	assertRustTestLink(t, result.TestLinks, symbolKeys, rustTestKeyByName(t, result.Tests, "tests::service_run"), "demo::Service::run", "receiver_match")
}

func TestAnalyzeBuildsRustDependenciesCallsAndRefsAcrossCrates(t *testing.T) {
	root := t.TempDir()
	writeRustFixture(t, root, "Cargo.toml", "[workspace]\nmembers = [\"crates/*\"]\n")
	writeRustFixture(t, root, "crates/core/Cargo.toml", "[package]\nname = \"core\"\nedition = \"2021\"\n")
	writeRustFixture(t, root, "crates/core/src/lib.rs", `pub struct Service;

impl Service {
    pub fn run(&self) {}
}
`)
	writeRustFixture(t, root, "crates/app/Cargo.toml", "[package]\nname = \"app\"\nedition = \"2021\"\n")
	writeRustFixture(t, root, "crates/app/src/lib.rs", `use core::Service;

pub fn execute(service: Service) {
    service.run();
}
`)

	adapter := NewAdapter()
	scanned, err := adapter.Scan(root)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	result, err := adapter.Analyze(project.Info{
		Root:       root,
		ModulePath: "workspace",
		GoVersion:  "2021",
		Language:   "rust",
	}, codebase.ScanMap(scanned), nil)
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	symbolKeys := rustSymbolKeys(result.Symbols)
	assertRustDependency(t, result.Dependencies, "app", "core")
	assertRustCallEdge(t, result.Calls, symbolKeys, "app::execute", "core::Service::run")
	assertRustReference(t, result.References, symbolKeys, "app::execute", "core::Service")
}

func TestAnalyzeTracksRustModuleImportsAndPackageCalls(t *testing.T) {
	root := t.TempDir()
	writeRustFixture(t, root, "Cargo.toml", "[package]\nname = \"demo\"\nedition = \"2021\"\n")
	writeRustFixture(t, root, "src/lib.rs", `mod http;

pub struct Service;

impl Service {
    pub fn run(&self) {
        helper();
    }
}

pub fn helper() {}
`)
	writeRustFixture(t, root, "src/http.rs", `use crate::{Service, helper};

pub fn route(service: Service) {
    helper();
    Service::run(&service);
}
`)

	adapter := NewAdapter()
	scanned, err := adapter.Scan(root)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	result, err := adapter.Analyze(project.Info{
		Root:       root,
		ModulePath: "demo",
		GoVersion:  "2021",
		Language:   "rust",
	}, codebase.ScanMap(scanned), nil)
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	symbolKeys := rustSymbolKeys(result.Symbols)
	assertRustDependency(t, result.Dependencies, "demo::http", "demo")
	assertRustCallEdge(t, result.Calls, symbolKeys, "demo::http::route", "demo::helper")
	assertRustCallEdge(t, result.Calls, symbolKeys, "demo::http::route", "demo::Service::run")
	assertRustReference(t, result.References, symbolKeys, "demo::http::route", "demo::Service")
}

func TestAnalyzeFiltersRustPackagesButKeepsCrossPackageResolution(t *testing.T) {
	root := t.TempDir()
	writeRustFixture(t, root, "Cargo.toml", "[package]\nname = \"demo\"\nedition = \"2021\"\n")
	writeRustFixture(t, root, "src/lib.rs", `mod util;

pub fn run() {
    util::helper();
}
`)
	writeRustFixture(t, root, "src/util.rs", `pub fn helper() {}
`)

	adapter := NewAdapter()
	scanned, err := adapter.Scan(root)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	result, err := adapter.Analyze(project.Info{
		Root:       root,
		ModulePath: "demo",
		GoVersion:  "2021",
		Language:   "rust",
	}, codebase.ScanMap(scanned), []string{"demo"})
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	symbolKeys := rustSymbolKeys(result.Symbols)
	if len(result.Packages) != 1 || result.Packages[0].ImportPath != "demo" {
		t.Fatalf("expected only demo package in filtered result, got %+v", result.Packages)
	}
	assertRustCallEdge(t, result.Calls, symbolKeys, "demo::run", "demo::util::helper")
	if _, ok := result.ImpactedPackage["demo"]; !ok {
		t.Fatalf("expected demo to be marked as impacted, got %+v", result.ImpactedPackage)
	}
	if _, ok := result.ImpactedPackage["demo::util"]; ok {
		t.Fatalf("did not expect util package to be marked as impacted, got %+v", result.ImpactedPackage)
	}
}

func TestScanHonorsGitIgnoreInRustWorkspace(t *testing.T) {
	root := t.TempDir()
	writeRustFixture(t, root, ".gitignore", "crates/app/src/ignored.rs\n")
	writeRustFixture(t, root, "Cargo.toml", "[workspace]\nmembers = [\"crates/*\"]\n")
	writeRustFixture(t, root, "crates/app/Cargo.toml", "[package]\nname = \"app\"\nedition = \"2021\"\n")
	writeRustFixture(t, root, "crates/app/src/lib.rs", "pub fn run() {}\n")
	writeRustFixture(t, root, "crates/app/src/ignored.rs", "pub fn ignored() {}\n")

	adapter := NewAdapter()
	files, err := adapter.Scan(root)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	for _, file := range files {
		if file.RelPath == "crates/app/src/ignored.rs" {
			t.Fatalf("expected gitignored rust file to be skipped, got %+v", file)
		}
	}
}

func writeRustFixture(t *testing.T, root, relPath, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func requireRustPackage(t *testing.T, packages []codebase.PackageFact, importPath string) {
	t.Helper()
	for _, pkg := range packages {
		if pkg.ImportPath == importPath {
			return
		}
	}
	t.Fatalf("expected rust package %s in %+v", importPath, packages)
}

func requireRustSymbol(t *testing.T, symbols []codebase.SymbolFact, qname, kind string) {
	t.Helper()
	for _, symbol := range symbols {
		if symbol.QName == qname {
			if symbol.Kind != kind {
				t.Fatalf("unexpected rust symbol kind for %s: got %q want %q", qname, symbol.Kind, kind)
			}
			return
		}
	}
	t.Fatalf("expected rust symbol %s in %+v", qname, symbols)
}

func rustSymbolKeys(symbols []codebase.SymbolFact) map[string]string {
	result := make(map[string]string, len(symbols))
	for _, symbol := range symbols {
		result[symbol.QName] = symbol.SymbolKey
	}
	return result
}

func assertRustDependency(t *testing.T, deps []codebase.DependencyFact, fromPackage, toPackage string) {
	t.Helper()
	for _, dep := range deps {
		if dep.FromPackageImportPath == fromPackage && dep.ToPackageImportPath == toPackage {
			return
		}
	}
	t.Fatalf("expected rust dependency %s -> %s, got %+v", fromPackage, toPackage, deps)
}

func assertRustCallEdge(t *testing.T, calls []codebase.CallFact, symbolKeys map[string]string, callerQName, calleeQName string) {
	t.Helper()
	callerKey := symbolKeys[callerQName]
	calleeKey := symbolKeys[calleeQName]
	if callerKey == "" {
		callerKey = callerQName
	}
	if calleeKey == "" {
		calleeKey = calleeQName
	}
	for _, call := range calls {
		if call.CallerSymbolKey == callerKey && call.CalleeSymbolKey == calleeKey {
			return
		}
	}
	t.Fatalf("expected rust call edge %s -> %s, got %+v", callerQName, calleeQName, calls)
}

func assertRustReference(t *testing.T, refs []codebase.ReferenceFact, symbolKeys map[string]string, fromQName, toQName string) {
	t.Helper()
	fromKey := symbolKeys[fromQName]
	toKey := symbolKeys[toQName]
	if fromKey == "" {
		fromKey = fromQName
	}
	if toKey == "" {
		toKey = toQName
	}
	for _, ref := range refs {
		if ref.FromSymbolKey == fromKey && ref.ToSymbolKey == toKey {
			return
		}
	}
	t.Fatalf("expected rust reference %s -> %s, got %+v", fromQName, toQName, refs)
}

func assertRustTestLink(t *testing.T, links []codebase.TestLinkFact, symbolKeys map[string]string, testKey, qname, kind string) {
	t.Helper()
	symbolKey := symbolKeys[qname]
	if symbolKey == "" {
		symbolKey = qname
	}
	for _, link := range links {
		if link.TestKey == testKey && link.SymbolKey == symbolKey && link.LinkKind == kind {
			return
		}
	}
	t.Fatalf("expected rust test link %s -> %s (%s), got %+v", testKey, qname, kind, links)
}

func rustTestKeyByName(t *testing.T, tests []codebase.TestFact, name string) string {
	t.Helper()
	for _, test := range tests {
		if test.Name == name {
			return test.TestKey
		}
	}
	t.Fatalf("expected rust test %s in %+v", name, tests)
	return ""
}
