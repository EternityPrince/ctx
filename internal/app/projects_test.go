package app

import (
	"bytes"
	"strings"
	"testing"

	"github.com/vladimirkasterin/ctx/internal/cli"
	"github.com/vladimirkasterin/ctx/internal/storage"
)

func TestRunProjectsDevResetClearsAllIndexedProjects(t *testing.T) {
	t.Setenv("CTX_HOME", t.TempDir())

	rootA := t.TempDir()
	writeProjectStateFixture(t, rootA, "Cargo.toml", "[package]\nname = \"reset-a\"\nedition = \"2021\"\n")
	writeProjectStateFixture(t, rootA, "src/lib.rs", "pub fn run() {}\n")

	stateA, err := openProjectState(rootA)
	if err != nil {
		t.Fatalf("openProjectState A returned error: %v", err)
	}
	if _, committed, err := projectService.ApplySnapshot(stateA, "index", "initial a", false); err != nil {
		_ = stateA.Close()
		t.Fatalf("ApplySnapshot A returned error: %v", err)
	} else if !committed {
		_ = stateA.Close()
		t.Fatal("expected initial snapshot A to be committed")
	}
	if err := stateA.Close(); err != nil {
		t.Fatalf("Close A returned error: %v", err)
	}

	rootB := t.TempDir()
	writeProjectStateFixture(t, rootB, "go.mod", "module example.com/resetb\n\ngo 1.26\n")
	writeProjectStateFixture(t, rootB, "main.go", "package main\n\nfunc main() {}\n")

	stateB, err := openProjectState(rootB)
	if err != nil {
		t.Fatalf("openProjectState B returned error: %v", err)
	}
	if _, committed, err := projectService.ApplySnapshot(stateB, "index", "initial b", false); err != nil {
		_ = stateB.Close()
		t.Fatalf("ApplySnapshot B returned error: %v", err)
	} else if !committed {
		_ = stateB.Close()
		t.Fatal("expected initial snapshot B to be committed")
	}
	if err := stateB.Close(); err != nil {
		t.Fatalf("Close B returned error: %v", err)
	}

	records, err := storage.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects before reset returned error: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected two indexed projects before reset, got %d", len(records))
	}

	var out bytes.Buffer
	if err := runProjects(cli.Command{Name: "projects", ProjectsVerb: "dev-reset"}, &out); err != nil {
		t.Fatalf("runProjects dev-reset returned error: %v", err)
	}

	text := stripANSICodes(out.String())
	for _, expected := range []string{
		"Dev reset complete:",
		"projects=2",
		"snapshots=2",
		"freed=",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected %q in dev-reset output, got:\n%s", expected, text)
		}
	}

	records, err = storage.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects after reset returned error: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("expected no indexed projects after reset, got %d", len(records))
	}
}
