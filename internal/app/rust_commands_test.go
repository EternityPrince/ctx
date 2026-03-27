package app

import (
	"bytes"
	"strings"
	"testing"

	"github.com/vladimirkasterin/ctx/internal/cli"
)

func TestRunStatusAndProjectReportOnRustProject(t *testing.T) {
	t.Setenv("CTX_HOME", t.TempDir())

	root := t.TempDir()
	writeProjectStateFixture(t, root, "Cargo.toml", "[package]\nname = \"report-demo\"\nedition = \"2021\"\n")
	writeProjectStateFixture(t, root, "src/lib.rs", `pub struct Service;

impl Service {
    pub fn run(&self) {}
}
`)

	state, err := openProjectState(root)
	if err != nil {
		t.Fatalf("openProjectState returned error: %v", err)
	}
	if _, committed, err := projectService.ApplySnapshot(state, "index", "rust command test", false); err != nil {
		_ = state.Close()
		t.Fatalf("ApplySnapshot returned error: %v", err)
	} else if !committed {
		_ = state.Close()
		t.Fatal("expected rust snapshot to be committed")
	}
	if err := state.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	statusOut := &bytes.Buffer{}
	if err := runStatus(cli.Command{Name: "status", Root: root, OutputMode: cli.OutputHuman}, statusOut); err != nil {
		t.Fatalf("runStatus returned error: %v", err)
	}
	statusText := stripANSICodes(statusOut.String())
	if !strings.Contains(statusText, "Language: Rust") || !strings.Contains(statusText, "Version: 2021") {
		t.Fatalf("expected rust status output, got:\n%s", statusText)
	}
	if !strings.Contains(statusText, "Composition: rs=1") || !strings.Contains(statusText, "Capabilities: Rust=best-effort") || !strings.Contains(statusText, "Timings: scan=") {
		t.Fatalf("expected rust composition/capabilities in status, got:\n%s", statusText)
	}

	reportOut := &bytes.Buffer{}
	if err := runProjectReport(cli.Command{Name: "report", Root: root, OutputMode: cli.OutputHuman, Limit: 4}, reportOut); err != nil {
		t.Fatalf("runProjectReport returned error: %v", err)
	}
	reportText := stripANSICodes(reportOut.String())
	if !strings.Contains(reportText, "CTX Project Report") || !strings.Contains(reportText, "report-demo") {
		t.Fatalf("expected rust project report output, got:\n%s", reportText)
	}
	if !strings.Contains(reportText, "Composition: rs=1") || !strings.Contains(reportText, "Capabilities: Rust=best-effort") {
		t.Fatalf("expected rust composition/capabilities in report, got:\n%s", reportText)
	}
}
