package cli

import (
	"strings"
	"testing"
)

func TestParseLegacyReportFlags(t *testing.T) {
	command, err := Parse([]string{"-copy"})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if command.Name != "report" {
		t.Fatalf("expected report command, got %q", command.Name)
	}
	if !command.Report.CopyToClipboard {
		t.Fatal("expected CopyToClipboard to be true")
	}
}

func TestParseRejectsCopyAndOutputTogether(t *testing.T) {
	_, err := Parse([]string{"-copy", "-output", "report.txt"})
	if err == nil {
		t.Fatal("expected Parse to reject -copy with -output")
	}

	if !strings.Contains(err.Error(), "cannot be used together") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseIndexCommand(t *testing.T) {
	command, err := Parse([]string{"index", ".", "--note", "initial"})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if command.Name != "index" {
		t.Fatalf("expected index command, got %q", command.Name)
	}
	if command.Note != "initial" {
		t.Fatalf("expected note to be parsed, got %q", command.Note)
	}
}

func TestParseSymbolCommand(t *testing.T) {
	command, err := Parse([]string{"symbol", "CreateSession"})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if command.Name != "symbol" {
		t.Fatalf("expected symbol command, got %q", command.Name)
	}
	if command.Query != "CreateSession" {
		t.Fatalf("expected query to be CreateSession, got %q", command.Query)
	}
}

func TestParseImpactCommand(t *testing.T) {
	command, err := Parse([]string{"impact", "CreateSession", "--depth", "4"})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if command.Name != "impact" {
		t.Fatalf("expected impact command, got %q", command.Name)
	}
	if command.Query != "CreateSession" {
		t.Fatalf("expected query to be CreateSession, got %q", command.Query)
	}
	if command.Depth != 4 {
		t.Fatalf("expected depth=4, got %d", command.Depth)
	}
}

func TestParseReportAICommand(t *testing.T) {
	command, err := Parse([]string{"report", ".", "-ai", "-limit", "5"})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if command.Name != "report" {
		t.Fatalf("expected report command, got %q", command.Name)
	}
	if command.OutputMode != OutputAI {
		t.Fatalf("expected ai output mode, got %q", command.OutputMode)
	}
	if command.Limit != 5 {
		t.Fatalf("expected limit=5, got %d", command.Limit)
	}
}

func TestParseReportHumanAlias(t *testing.T) {
	command, err := Parse([]string{"report", ".", "-h"})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if command.Name != "report" {
		t.Fatalf("expected report command, got %q", command.Name)
	}
	if command.OutputMode != OutputHuman {
		t.Fatalf("expected human output mode, got %q", command.OutputMode)
	}
}

func TestParseShellCommand(t *testing.T) {
	command, err := Parse([]string{"shell", "CreateSession"})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if command.Name != "shell" {
		t.Fatalf("expected shell command, got %q", command.Name)
	}
	if command.Query != "CreateSession" {
		t.Fatalf("expected query to be CreateSession, got %q", command.Query)
	}
}

func TestParseSymbolAIShortFlag(t *testing.T) {
	command, err := Parse([]string{"symbol", "CreateSession", "-a"})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if command.Name != "symbol" {
		t.Fatalf("expected symbol command, got %q", command.Name)
	}
	if command.OutputMode != OutputAI {
		t.Fatalf("expected ai output mode, got %q", command.OutputMode)
	}
}

func TestParseShellRootFlag(t *testing.T) {
	command, err := Parse([]string{"shell", "--root", "."})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if command.Name != "shell" {
		t.Fatalf("expected shell command, got %q", command.Name)
	}
	if command.Query != "" {
		t.Fatalf("expected empty query, got %q", command.Query)
	}
}

func TestParseProjectsRemove(t *testing.T) {
	command, err := Parse([]string{"projects", "rm", "abc123"})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if command.Name != "projects" || command.ProjectsVerb != "rm" {
		t.Fatalf("unexpected projects command: %+v", command)
	}
	if command.ProjectArg != "abc123" {
		t.Fatalf("unexpected project arg: %q", command.ProjectArg)
	}
}

func TestParseSnapshotsCommand(t *testing.T) {
	command, err := Parse([]string{"snapshots", ".", "-ai"})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if command.Name != "snapshots" {
		t.Fatalf("expected snapshots command, got %q", command.Name)
	}
	if command.OutputMode != OutputAI {
		t.Fatalf("expected ai output mode, got %q", command.OutputMode)
	}
}

func TestParseSnapshotCommand(t *testing.T) {
	command, err := Parse([]string{"snapshot", "7", "--root", "."})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if command.Name != "snapshot" {
		t.Fatalf("expected snapshot command, got %q", command.Name)
	}
	if command.SnapshotID != 7 {
		t.Fatalf("expected snapshot id 7, got %d", command.SnapshotID)
	}
}
