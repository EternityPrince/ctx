package app

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/vladimirkasterin/ctx/internal/cli"
	"github.com/vladimirkasterin/ctx/internal/storage"
)

func TestRunDiffExplainRendersUnifiedExplain(t *testing.T) {
	t.Setenv("CTX_HOME", t.TempDir())

	root := t.TempDir()
	writeProjectStateFixture(t, root, "Cargo.toml", "[package]\nname = \"demo\"\nedition = \"2021\"\n")
	writeProjectStateFixture(t, root, "src/lib.rs", "pub fn run() {}\n")

	state, err := openProjectState(root)
	if err != nil {
		t.Fatalf("openProjectState returned error: %v", err)
	}
	first, committed, err := projectService.ApplySnapshot(state, "index", "initial", false)
	if err != nil {
		_ = state.Close()
		t.Fatalf("ApplySnapshot initial returned error: %v", err)
	}
	if !committed {
		_ = state.Close()
		t.Fatal("expected initial snapshot to be committed")
	}
	if err := state.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	writeProjectStateFixture(t, root, "src/lib.rs", "pub fn run() {\n    helper();\n}\n\nfn helper() {}\n")

	state, err = openProjectState(root)
	if err != nil {
		t.Fatalf("openProjectState second returned error: %v", err)
	}
	second, committed, err := projectService.ApplySnapshot(state, "update", "change", false)
	if err != nil {
		_ = state.Close()
		t.Fatalf("ApplySnapshot second returned error: %v", err)
	}
	if !committed {
		_ = state.Close()
		t.Fatal("expected second snapshot to be committed")
	}
	if err := state.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	var out bytes.Buffer
	if err := runDiff(cli.Command{Name: "diff", Root: root, FromSnapshot: first.ID, ToSnapshot: second.ID, Explain: true}, &out); err != nil {
		t.Fatalf("runDiff returned error: %v", err)
	}

	text := stripANSICodes(out.String())
	for _, expected := range []string{
		"CTX Diff",
		"Summary",
		"Explain",
		"Precision:",
		"Changed Files",
		"Impacted Symbols",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected %q in diff output, got:\n%s", expected, text)
		}
	}
}

func TestRenderHumanHistoryAndCoChangeExplain(t *testing.T) {
	symbolHistory := storage.SymbolHistoryView{
		Symbol: storage.SymbolMatch{
			QName:             "example.com/project/pkg.Run",
			PackageImportPath: "example.com/project/pkg",
			FilePath:          "pkg/run.go",
			Kind:              "func",
			Signature:         "func Run() error",
			Line:              4,
		},
		IntroducedIn:  storage.SnapshotInfo{ID: 1, CreatedAt: time.Date(2026, time.March, 26, 10, 0, 0, 0, time.UTC)},
		LastChangedIn: storage.SnapshotInfo{ID: 3, CreatedAt: time.Date(2026, time.March, 27, 10, 0, 0, 0, time.UTC)},
		Events: []storage.SymbolHistoryEvent{
			{
				ToSnapshot:      storage.SnapshotInfo{ID: 3, CreatedAt: time.Date(2026, time.March, 27, 10, 0, 0, 0, time.UTC)},
				Status:          "changed",
				ContractChanged: true,
				AddedCalls:      1,
			},
		},
	}

	var historyOut bytes.Buffer
	if err := renderHumanSymbolHistory(&historyOut, "/tmp/project", "example.com/project", symbolHistory, true); err != nil {
		t.Fatalf("renderHumanSymbolHistory returned error: %v", err)
	}
	historyText := stripANSICodes(historyOut.String())
	for _, expected := range []string{"CTX History", "Explain", "Event source:", "Recent Change Events"} {
		if !strings.Contains(historyText, expected) {
			t.Fatalf("expected %q in history output, got:\n%s", expected, historyText)
		}
	}

	cochange := storage.CoChangeView{
		Scope:             "symbol",
		Anchor:            "example.com/project/pkg.Run",
		AnchorFile:        "pkg/run.go",
		AnchorPackage:     "example.com/project/pkg",
		AnchorChangeCount: 3,
		Files:             []storage.CoChangeItem{{Label: "api/handler.go", Count: 2, Frequency: 0.67}},
		Packages:          []storage.CoChangeItem{{Label: "example.com/project/api", Count: 2, Frequency: 0.67}},
	}

	var cochangeOut bytes.Buffer
	if err := renderHumanCoChange(&cochangeOut, "example.com/project", cochange, true); err != nil {
		t.Fatalf("renderHumanCoChange returned error: %v", err)
	}
	cochangeText := stripANSICodes(cochangeOut.String())
	for _, expected := range []string{"CTX Co-Change", "Explain", "Anchor changes:", "Files That Change Together"} {
		if !strings.Contains(cochangeText, expected) {
			t.Fatalf("expected %q in cochange output, got:\n%s", expected, cochangeText)
		}
	}
}
