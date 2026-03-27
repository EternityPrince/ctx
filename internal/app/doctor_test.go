package app

import (
	"bytes"
	"strings"
	"testing"

	"github.com/vladimirkasterin/ctx/internal/cli"
)

func TestRunStatusExplainShowsCurrentPlanState(t *testing.T) {
	t.Setenv("CTX_HOME", t.TempDir())

	root := t.TempDir()
	writeProjectStateFixture(t, root, "Cargo.toml", "[package]\nname = \"demo\"\nedition = \"2021\"\n")
	writeProjectStateFixture(t, root, "src/lib.rs", "pub fn run() {}\n")

	state, err := openProjectState(root)
	if err != nil {
		t.Fatalf("openProjectState returned error: %v", err)
	}
	if _, committed, err := projectService.ApplySnapshot(state, "index", "initial", false); err != nil {
		_ = state.Close()
		t.Fatalf("ApplySnapshot returned error: %v", err)
	} else if !committed {
		_ = state.Close()
		t.Fatal("expected initial snapshot to be committed")
	}
	if err := state.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	writeProjectStateFixture(t, root, "src/lib.rs", "pub fn run() {\n    helper();\n}\n\nfn helper() {}\n")

	var out bytes.Buffer
	if err := runStatus(cli.Command{Name: "status", Root: root, OutputMode: cli.OutputHuman, Explain: true}, &out); err != nil {
		t.Fatalf("runStatus returned error: %v", err)
	}
	text := stripANSICodes(out.String())
	if !strings.Contains(text, "Explain:") || !strings.Contains(text, "changes: added=0 changed=0 deleted=0") || !strings.Contains(text, "Timings: scan=") {
		t.Fatalf("expected explainability details in status output, got:\n%s", text)
	}
}

func TestRunUpdateExplainShowsImpactedPackages(t *testing.T) {
	t.Setenv("CTX_HOME", t.TempDir())

	root := t.TempDir()
	writeProjectStateFixture(t, root, "Cargo.toml", "[package]\nname = \"demo\"\nedition = \"2021\"\n")
	writeProjectStateFixture(t, root, "src/lib.rs", "pub fn run() {}\n")

	state, err := openProjectState(root)
	if err != nil {
		t.Fatalf("openProjectState returned error: %v", err)
	}
	if _, committed, err := projectService.ApplySnapshot(state, "index", "initial", false); err != nil {
		_ = state.Close()
		t.Fatalf("ApplySnapshot returned error: %v", err)
	} else if !committed {
		_ = state.Close()
		t.Fatal("expected initial snapshot to be committed")
	}
	if err := state.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	writeProjectStateFixture(t, root, "src/lib.rs", "pub fn run() {\n    helper();\n}\n\nfn helper() {}\n")

	var out bytes.Buffer
	if err := runIndexLike(cli.Command{Name: "update", Root: root, OutputMode: cli.OutputHuman, Explain: true}, &out, false); err != nil {
		t.Fatalf("runIndexLike returned error: %v", err)
	}
	text := stripANSICodes(out.String())
	if !strings.Contains(text, "Explain:") || !strings.Contains(text, "directly impacted packages:") || !strings.Contains(text, "demo") || !strings.Contains(text, "scan_ms=") {
		t.Fatalf("expected explainability details in status output, got:\n%s", text)
	}
}

func TestRunDoctorReportsConfigAndSnapshotHealth(t *testing.T) {
	t.Setenv("CTX_HOME", t.TempDir())

	root := t.TempDir()
	writeProjectStateFixture(t, root, ".ctxconfig", "[index]\nexclude_paths = generated/**\n")
	writeProjectStateFixture(t, root, "Cargo.toml", "[package]\nname = \"doctor-demo\"\nedition = \"2021\"\n")
	writeProjectStateFixture(t, root, "src/lib.rs", "pub fn run() {}\n")

	state, err := openProjectState(root)
	if err != nil {
		t.Fatalf("openProjectState returned error: %v", err)
	}
	if _, committed, err := projectService.ApplySnapshot(state, "index", "initial", false); err != nil {
		_ = state.Close()
		t.Fatalf("ApplySnapshot returned error: %v", err)
	} else if !committed {
		_ = state.Close()
		t.Fatal("expected initial snapshot to be committed")
	}
	if err := state.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	var out bytes.Buffer
	if err := runDoctor(cli.Command{Name: "doctor", Root: root, OutputMode: cli.OutputHuman}, &out); err != nil {
		t.Fatalf("runDoctor returned error: %v", err)
	}
	text := stripANSICodes(out.String())
	for _, expected := range []string{
		"CTX Doctor",
		"Project detection: ok",
		"Config: ok",
		"Schema: ok",
		"DB quick check: ok",
		"Snapshot telemetry:",
		"Snapshot chain: ok",
		"Snapshot inventory: ok",
		"Freshness: clean",
		"Incremental: no-op ready",
		"Change cache:",
		"Config rules:",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected %q in doctor output, got:\n%s", expected, text)
		}
	}
}
