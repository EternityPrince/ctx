package app

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

func TestTreeDirsShowsDirectoryExtensionSummaries(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required for mixed tree tests")
	}

	session, output := newIndexedShellSession(t, map[string]string{
		"go.mod":          "module example.com/mixed\n\ngo 1.26\n",
		"main.go":         "package main\n\nfunc main() {}\n",
		"pkg/service.py":  "def run() -> int:\n    return 1\n",
		"pkg/helper.py":   "def help() -> int:\n    return 1\n",
		"docs/readme.txt": "hello\n",
		"docs/guide.md":   "# guide\n",
	})

	if err := session.showTreeCommand([]string{"dirs"}); err != nil {
		t.Fatalf("showTreeCommand returned error: %v", err)
	}

	text := stripANSICodes(output.String())
	if !strings.Contains(text, "Directory Overview") {
		t.Fatalf("expected directory overview screen, got:\n%s", text)
	}
	if !strings.Contains(text, "pkg/") || !strings.Contains(text, "py=2") {
		t.Fatalf("expected pkg directory to summarize python files, got:\n%s", text)
	}
	if !strings.Contains(text, "docs/") || !strings.Contains(text, "md=1") || !strings.Contains(text, "txt=1") {
		t.Fatalf("expected docs directory extension summary, got:\n%s", text)
	}
}

func TestTreeUsesLocalPageNumbers(t *testing.T) {
	files := map[string]string{
		"go.mod":  "module example.com/large\n\ngo 1.26\n",
		"main.go": "package main\n\nfunc main() {}\n",
	}
	for idx := 1; idx <= 35; idx++ {
		files[fmt.Sprintf("notes/file%02d.txt", idx)] = "note\n"
	}

	session, output := newIndexedShellSession(t, files)

	if err := session.showTreeCommand(nil); err != nil {
		t.Fatalf("showTreeCommand returned error: %v", err)
	}
	firstPage := stripANSICodes(output.String())
	if !strings.Contains(firstPage, "[  1]") {
		t.Fatalf("expected local numbering on first page, got:\n%s", firstPage)
	}

	output.Reset()
	if err := session.showTreeCommand([]string{"next"}); err != nil {
		t.Fatalf("showTreeCommand next returned error: %v", err)
	}
	secondPage := stripANSICodes(output.String())
	if !strings.Contains(secondPage, "[  1]") {
		t.Fatalf("expected numbering to restart on next page, got:\n%s", secondPage)
	}
	if strings.Contains(secondPage, "[ 28]") || strings.Contains(secondPage, "[ 29]") {
		t.Fatalf("did not expect global file numbering on next page, got:\n%s", secondPage)
	}
}

func TestTreeDirsOpenZoomsIntoDirectoryScope(t *testing.T) {
	session, output := newIndexedShellSession(t, map[string]string{
		"go.mod":         "module example.com/zoom\n\ngo 1.26\n",
		"main.go":        "package main\n\nfunc main() {}\n",
		"pkg/readme.txt": "details\n",
	})

	if err := session.showTreeCommand([]string{"dirs"}); err != nil {
		t.Fatalf("showTreeCommand returned error: %v", err)
	}

	output.Reset()
	if err := session.openIndex("1"); err != nil {
		t.Fatalf("openIndex returned error: %v", err)
	}

	text := stripANSICodes(output.String())
	if !strings.Contains(text, "Project Tree") || !strings.Contains(text, "Scope: pkg") {
		t.Fatalf("expected scoped tree after opening directory, got:\n%s", text)
	}
	if !strings.Contains(text, "pkg/readme.txt") {
		t.Fatalf("expected scoped tree to include directory file, got:\n%s", text)
	}
}

func TestTreeHotShowsRankedFiles(t *testing.T) {
	session, output := newIndexedShellSession(t, map[string]string{
		"go.mod": "module example.com/hot\n\ngo 1.26\n",
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

	if err := session.showTreeCommand([]string{"hot"}); err != nil {
		t.Fatalf("showTreeCommand returned error: %v", err)
	}

	text := stripANSICodes(output.String())
	if !strings.Contains(text, "Hot Files") {
		t.Fatalf("expected hot files screen, got:\n%s", text)
	}
	if !strings.Contains(text, "service.go") && !strings.Contains(text, "main.go") {
		t.Fatalf("expected ranked hot file list, got:\n%s", text)
	}
	if !strings.Contains(text, "risk=") {
		t.Fatalf("expected hot files to show compact risk flags, got:\n%s", text)
	}
}

func TestTreeOpenShowsPythonFileSymbolsImmediately(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required for mixed tree tests")
	}

	session, output := newIndexedShellSession(t, map[string]string{
		"go.mod":  "module example.com/pyflow\n\ngo 1.26\n",
		"main.go": "package main\n\nfunc main() {}\n",
		"pkg/service.py": `class Service:
    def run(self) -> int:
        return helper()


def helper() -> int:
    return 1
`,
	})

	if err := session.showTreeCommand(nil); err != nil {
		t.Fatalf("showTreeCommand returned error: %v", err)
	}

	output.Reset()
	if err := session.openIndex("1"); err != nil {
		t.Fatalf("openIndex returned error: %v", err)
	}

	text := stripANSICodes(output.String())
	if !strings.Contains(text, "File Journey") {
		t.Fatalf("expected file journey screen, got:\n%s", text)
	}
	if strings.Contains(text, "No indexed symbols in this file") {
		t.Fatalf("expected indexed python symbols to be shown immediately, got:\n%s", text)
	}
	if !strings.Contains(text, "Service") || !strings.Contains(text, "run") || !strings.Contains(text, "helper") {
		t.Fatalf("expected python symbol inventory in file journey, got:\n%s", text)
	}
}

func TestFileJourneyShowsRustFileSymbolsImmediately(t *testing.T) {
	session, output := newIndexedShellSession(t, map[string]string{
		"Cargo.toml": "[package]\nname = \"shell-demo\"\nedition = \"2021\"\n",
		"src/lib.rs": `pub struct Service;

impl Service {
    pub fn run(&self) {}
}

pub fn helper() {}
`,
	})

	if err := session.showFileJourney("src/lib.rs"); err != nil {
		t.Fatalf("showFileJourney returned error: %v", err)
	}

	text := stripANSICodes(output.String())
	if !strings.Contains(text, "File Journey") {
		t.Fatalf("expected file journey screen, got:\n%s", text)
	}
	if strings.Contains(text, "No indexed symbols in this file") {
		t.Fatalf("expected indexed rust symbols to be shown immediately, got:\n%s", text)
	}
	if !strings.Contains(text, "Service") || !strings.Contains(text, "run") || !strings.Contains(text, "helper") {
		t.Fatalf("expected rust symbol inventory in file journey, got:\n%s", text)
	}
}

func newIndexedShellSession(t *testing.T, files map[string]string) (*shellSession, *bytes.Buffer) {
	t.Helper()
	t.Setenv("CTX_HOME", t.TempDir())

	root := t.TempDir()
	for relPath, content := range files {
		writeProjectStateFixture(t, root, relPath, content)
	}

	state, err := openProjectState(root)
	if err != nil {
		t.Fatalf("openProjectState returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = state.Close()
	})

	if _, committed, err := projectService.ApplySnapshot(state, "index", "tree test", false); err != nil {
		t.Fatalf("ApplySnapshot returned error: %v", err)
	} else if !committed {
		t.Fatal("expected snapshot to be committed")
	}

	output := &bytes.Buffer{}
	session := &shellSession{
		info:       state.Info,
		store:      state.Store,
		stdout:     output,
		palette:    palette{},
		changedNow: projectService.ChangedNow(state),
	}
	return session, output
}
