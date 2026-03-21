package render

import (
	"strings"
	"testing"
	"time"

	"github.com/vladimirkasterin/ctx/internal/config"
	"github.com/vladimirkasterin/ctx/internal/model"
	"github.com/vladimirkasterin/ctx/internal/tree"
)

func TestReportIncludesSummaryTreeAndFiles(t *testing.T) {
	snapshot := &model.Snapshot{
		Root:        "/tmp/project",
		GeneratedAt: time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC),
		Directories: []string{"cmd", "cmd/ctx"},
		Files: []model.File{
			{
				RelativePath: "cmd/ctx/main.go",
				Content:      "package main\n",
			},
		},
		Stats: model.Stats{
			DirectoriesScanned: 3,
			FilesScanned:       1,
			FilesIncluded:      1,
			TotalLines:         1,
			TotalNonEmptyLines: 1,
			TotalBytes:         13,
			AverageLines:       1,
			LargestFile: model.FileMetric{
				Path:      "cmd/ctx/main.go",
				Lines:     1,
				SizeBytes: 13,
			},
		},
	}

	projectTree := tree.Build(snapshot.Root, snapshot.Directories, snapshot.Files)
	report := Report(snapshot, projectTree, config.Options{})

	for _, expected := range []string{
		"CTX REPORT",
		"DIRECTORY TREE",
		"FILES",
		"cmd/ctx/main.go",
		"package main",
	} {
		if !strings.Contains(report, expected) {
			t.Fatalf("report should contain %q", expected)
		}
	}
}

func TestReportSummaryOnly(t *testing.T) {
	snapshot := &model.Snapshot{
		Root:        "/tmp/project",
		GeneratedAt: time.Now(),
		Stats: model.Stats{
			FilesIncluded: 1,
			AverageLines:  2,
		},
	}

	report := Report(snapshot, tree.Build(snapshot.Root, nil, nil), config.Options{SummaryOnly: true})
	if strings.Contains(report, "DIRECTORY TREE") {
		t.Fatal("summary-only report should not include tree")
	}
	if strings.Contains(report, "FILES") {
		t.Fatal("summary-only report should not include file section")
	}
}
