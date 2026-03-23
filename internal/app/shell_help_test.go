package app

import (
	"bytes"
	"strings"
	"testing"

	"github.com/vladimirkasterin/ctx/internal/project"
)

func TestShellHelpMentionsTreeDirsAndSmartSearch(t *testing.T) {
	output := &bytes.Buffer{}
	session := &shellSession{
		info: project.Info{
			Root:       "/tmp/project",
			ModulePath: "example.com/demo",
		},
		stdout:  output,
		palette: palette{},
	}

	if err := session.printHelp(); err != nil {
		t.Fatalf("printHelp returned error: %v", err)
	}

	text := stripANSICodes(output.String())
	for _, snippet := range []string{
		"tree [dirs|hot|next|prev|page <n>|up|root]",
		"search [symbol|text|regex] <query>",
		"find <query>",
		"inspect its file-type mix",
		"plain text at the prompt now runs the smart search flow",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("expected help output to contain %q, got:\n%s", snippet, text)
		}
	}
}
