package cli

import (
	"strings"
	"testing"
)

func TestParseCopyFlag(t *testing.T) {
	options, err := Parse([]string{"-copy"})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if !options.CopyToClipboard {
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
