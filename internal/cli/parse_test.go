package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseLegacyReportFlags(t *testing.T) {
	command, err := Parse([]string{"-copy", "-keep-empty", "-include-generated", "-include-minified", "-include-artifacts"})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if command.Name != "report" {
		t.Fatalf("expected report command, got %q", command.Name)
	}
	if !command.Report.CopyToClipboard {
		t.Fatal("expected CopyToClipboard to be true")
	}
	if !command.Report.KeepEmpty || !command.Report.IncludeGenerated || !command.Report.IncludeMinified || !command.Report.IncludeArtifacts {
		t.Fatalf("expected new dump flags to be parsed, got %+v", command.Report)
	}
}

func TestParseLegacyReportLoadsCtxConfigProfile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".ctxconfig")
	if err := os.WriteFile(path, []byte("[dump]\nmax_file_size = 64\ninclude_generated = true\n"), 0o644); err != nil {
		t.Fatalf("write .ctxconfig: %v", err)
	}

	command, err := Parse([]string{"dump", root})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if command.Report.MaxFileSize != 64 || !command.Report.IncludeGenerated {
		t.Fatalf("expected dump config to be applied, got %+v", command.Report)
	}
	if command.Report.ConfigPath == "" {
		t.Fatalf("expected config path to be captured, got %+v", command.Report)
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
	command, err := Parse([]string{"index", ".", "--note", "initial", "--explain"})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if command.Name != "index" {
		t.Fatalf("expected index command, got %q", command.Name)
	}
	if command.Note != "initial" {
		t.Fatalf("expected note to be parsed, got %q", command.Note)
	}
	if !command.Explain {
		t.Fatal("expected explain flag to be parsed")
	}
}

func TestParseDoctorCommand(t *testing.T) {
	command, err := Parse([]string{"doctor", "."})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if command.Name != "doctor" {
		t.Fatalf("expected doctor command, got %q", command.Name)
	}
}

func TestParseProjectsDevReset(t *testing.T) {
	command, err := Parse([]string{"projects", "--dev-reset"})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if command.Name != "projects" || command.ProjectsVerb != "dev-reset" {
		t.Fatalf("expected projects dev-reset command, got %+v", command)
	}
}

func TestParseWatchCommand(t *testing.T) {
	command, err := Parse([]string{"watch", ".", "--interval", "250ms", "--debounce", "500ms", "--cycles", "3", "--quiet", "--explain"})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if command.Name != "watch" {
		t.Fatalf("expected watch command, got %q", command.Name)
	}
	if command.WatchInterval != 250*time.Millisecond {
		t.Fatalf("expected watch interval to be parsed, got %s", command.WatchInterval)
	}
	if command.WatchDebounce != 500*time.Millisecond {
		t.Fatalf("expected watch debounce to be parsed, got %s", command.WatchDebounce)
	}
	if command.WatchCycles != 3 {
		t.Fatalf("expected watch cycles=3, got %d", command.WatchCycles)
	}
	if !command.WatchQuiet {
		t.Fatal("expected watch quiet flag to be parsed")
	}
	if !command.Explain {
		t.Fatal("expected watch explain flag to be parsed")
	}
}

func TestParseSymbolCommand(t *testing.T) {
	command, err := Parse([]string{"symbol", "CreateSession", "--explain"})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if command.Name != "symbol" {
		t.Fatalf("expected symbol command, got %q", command.Name)
	}
	if command.Query != "CreateSession" {
		t.Fatalf("expected query to be CreateSession, got %q", command.Query)
	}
	if !command.Explain {
		t.Fatal("expected symbol explain flag to be parsed")
	}
}

func TestParseImpactCommand(t *testing.T) {
	command, err := Parse([]string{"impact", "CreateSession", "--depth", "4", "--explain"})
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
	if !command.Explain {
		t.Fatal("expected impact explain flag to be parsed")
	}
}

func TestParseDiffHistoryAndCoChangeExplain(t *testing.T) {
	diff, err := Parse([]string{"diff", "--from", "1", "--to", "2", "--explain"})
	if err != nil {
		t.Fatalf("Parse diff returned error: %v", err)
	}
	if diff.Name != "diff" || !diff.Explain {
		t.Fatalf("expected diff explain flag to be parsed, got %+v", diff)
	}

	history, err := Parse([]string{"history", "CreateSession", "--explain"})
	if err != nil {
		t.Fatalf("Parse history returned error: %v", err)
	}
	if history.Name != "history" || !history.Explain {
		t.Fatalf("expected history explain flag to be parsed, got %+v", history)
	}

	cochange, err := Parse([]string{"cochange", "CreateSession", "--explain"})
	if err != nil {
		t.Fatalf("Parse cochange returned error: %v", err)
	}
	if cochange.Name != "cochange" || !cochange.Explain {
		t.Fatalf("expected cochange explain flag to be parsed, got %+v", cochange)
	}
}

func TestParseReportAICommand(t *testing.T) {
	command, err := Parse([]string{"report", ".", "-ai", "-limit", "5", "--explain"})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if command.Name != "report" {
		t.Fatalf("expected report command, got %q", command.Name)
	}
	if command.OutputMode != OutputAI {
		t.Fatalf("expected ai output mode, got %q", command.OutputMode)
	}
	if command.Scope != "project" {
		t.Fatalf("expected default project scope, got %q", command.Scope)
	}
	if command.Limit != 5 {
		t.Fatalf("expected limit=5, got %d", command.Limit)
	}
	if !command.Explain {
		t.Fatal("expected report explain flag to be parsed")
	}
}

func TestParseReportSliceCommand(t *testing.T) {
	command, err := Parse([]string{"report", "risky", "--root", ".", "-limit", "7"})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if command.Name != "report" {
		t.Fatalf("expected report command, got %q", command.Name)
	}
	if command.Scope != "risky" {
		t.Fatalf("expected risky scope, got %q", command.Scope)
	}
	if command.Limit != 7 {
		t.Fatalf("expected limit=7, got %d", command.Limit)
	}
}

func TestParseReportChangedSinceCommand(t *testing.T) {
	command, err := Parse([]string{"report", "changed-since", "."})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if command.Name != "report" {
		t.Fatalf("expected report command, got %q", command.Name)
	}
	if command.Scope != "changed-since" {
		t.Fatalf("expected changed-since scope, got %q", command.Scope)
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
	if command.SnapshotsVerb != "list" {
		t.Fatalf("expected list verb, got %q", command.SnapshotsVerb)
	}
}

func TestParseSnapshotsRemoveCommand(t *testing.T) {
	command, err := Parse([]string{"snapshots", "rm", "--project", "abc123", "7", "9"})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if command.Name != "snapshots" || command.SnapshotsVerb != "rm" {
		t.Fatalf("unexpected snapshots command: %+v", command)
	}
	if command.ProjectArg != "abc123" {
		t.Fatalf("unexpected project arg: %q", command.ProjectArg)
	}
	if len(command.SnapshotIDs) != 2 || command.SnapshotIDs[0] != 7 || command.SnapshotIDs[1] != 9 {
		t.Fatalf("unexpected snapshot ids: %+v", command.SnapshotIDs)
	}
}

func TestParseSnapshotsLimitCommand(t *testing.T) {
	command, err := Parse([]string{"snapshots", "limit", "5"})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if command.SnapshotsVerb != "limit" {
		t.Fatalf("expected limit verb, got %q", command.SnapshotsVerb)
	}
	if command.SnapshotLimit != 5 {
		t.Fatalf("expected snapshot limit 5, got %d", command.SnapshotLimit)
	}
}

func TestParseStatusProjectFlag(t *testing.T) {
	command, err := Parse([]string{"status", "--project", "abc123"})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if command.ProjectArg != "abc123" {
		t.Fatalf("unexpected project arg: %q", command.ProjectArg)
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

func TestParseHistoryCommand(t *testing.T) {
	command, err := Parse([]string{"history", "CreateSession", "--package", "--root", "."})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if command.Name != "history" {
		t.Fatalf("expected history command, got %q", command.Name)
	}
	if command.Query != "CreateSession" {
		t.Fatalf("unexpected history query: %q", command.Query)
	}
	if command.Scope != "package" {
		t.Fatalf("expected package scope, got %q", command.Scope)
	}
}

func TestParseCoChangeCommand(t *testing.T) {
	command, err := Parse([]string{"cochange", "CreateSession", "--limit", "5"})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if command.Name != "cochange" {
		t.Fatalf("expected cochange command, got %q", command.Name)
	}
	if command.Query != "CreateSession" {
		t.Fatalf("unexpected cochange query: %q", command.Query)
	}
	if command.Scope != "symbol" {
		t.Fatalf("expected default symbol scope, got %q", command.Scope)
	}
	if command.Limit != 5 {
		t.Fatalf("expected limit 5, got %d", command.Limit)
	}
}

func TestHelpUsageMentionsPythonAndShellSearchFlow(t *testing.T) {
	_, err := Parse([]string{"help"})
	if err == nil {
		t.Fatal("expected help to return usage text")
	}

	text := err.Error()
	for _, snippet := range []string{
		"local Go and Python code intelligence",
		"tree [dirs|hot|next|prev|page <n>|up|root]",
		"search [symbol|text|regex] <query>",
		"ctx history <query>",
		"Python analysis requires python3 on PATH.",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("expected help text to contain %q, got:\n%s", snippet, text)
		}
	}
}
