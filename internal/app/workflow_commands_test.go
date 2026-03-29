package app

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/vladimirkasterin/ctx/internal/cli"
	"github.com/vladimirkasterin/ctx/internal/storage"
)

func TestRunTraceOutputsActionableSections(t *testing.T) {
	root, _, _ := seedWorkflowCommandFixture(t)

	var out bytes.Buffer
	if err := runTrace(cli.Command{
		Name:       "trace",
		Root:       root,
		Query:      "Run",
		Depth:      4,
		Limit:      4,
		Explain:    true,
		OutputMode: cli.OutputHuman,
	}, &out); err != nil {
		t.Fatalf("runTrace returned error: %v", err)
	}

	text := stripANSICodes(out.String())
	for _, expected := range []string{
		"CTX Trace",
		"Summary",
		"Explain",
		"Data Flow",
		"analyzer-backed",
		"Flow Path",
		"Direct Callers",
		"Tests To Read Before Change",
		"Read This Order",
		"Review Checklist",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected %q in trace output, got:\n%s", expected, text)
		}
	}
}

func TestRunTravelOutputsLaunchReadingPath(t *testing.T) {
	root := seedTravelCommandFixture(t)

	var out bytes.Buffer
	if err := runTravel(cli.Command{
		Name:          "travel",
		Root:          root,
		RunRecipe:     "go run ./cmd/tool/main.go",
		RunArgs:       []string{"demo", "--verbose"},
		TravelTimeout: 30 * time.Second,
		Depth:         4,
		Limit:         4,
		Explain:       true,
		OutputMode:    cli.OutputHuman,
	}, &out); err != nil {
		t.Fatalf("runTravel returned error: %v", err)
	}

	text := stripANSICodes(out.String())
	for _, expected := range []string{
		"CTX Travel",
		"Launch Overview",
		"Travel ID",
		"Run recipe",
		"go run ./cmd/tool/main.go",
		"Performance",
		"Wall time",
		"CPU total",
		"Peak RSS",
		"Likely Call Path",
		"Important Functions Along This Launch",
		"Read This Order",
		"parseArgs",
		"pipeline.Run",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected %q in travel output, got:\n%s", expected, text)
		}
	}
}

func TestRunTravelShowListsAndReopensSavedRuns(t *testing.T) {
	root := seedTravelCommandFixture(t)

	var first bytes.Buffer
	if err := runTravel(cli.Command{
		Name:          "travel",
		Root:          root,
		RunRecipe:     "go run ./cmd/tool/main.go",
		RunArgs:       []string{"demo"},
		TravelTimeout: 30 * time.Second,
		Depth:         4,
		Limit:         4,
		Explain:       true,
		OutputMode:    cli.OutputHuman,
	}, &first); err != nil {
		t.Fatalf("runTravel seed returned error: %v", err)
	}

	var list bytes.Buffer
	if err := runTravel(cli.Command{
		Name:       "travel",
		Root:       root,
		Scope:      "show-all",
		OutputMode: cli.OutputHuman,
	}, &list); err != nil {
		t.Fatalf("runTravel show all returned error: %v", err)
	}
	listText := stripANSICodes(list.String())
	if !strings.Contains(listText, "CTX Travel Runs") || !strings.Contains(listText, "#1") || !strings.Contains(listText, "go run ./cmd/tool/main.go") {
		t.Fatalf("unexpected travel show all output:\n%s", listText)
	}

	var one bytes.Buffer
	if err := runTravel(cli.Command{
		Name:        "travel",
		Root:        root,
		Scope:       "show-one",
		TravelRunID: 1,
		OutputMode:  cli.OutputHuman,
	}, &one); err != nil {
		t.Fatalf("runTravel show one returned error: %v", err)
	}
	oneText := stripANSICodes(one.String())
	for _, expected := range []string{"CTX Travel", "Travel ID", "1", "Likely Call Path"} {
		if !strings.Contains(oneText, expected) {
			t.Fatalf("expected %q in travel show one output, got:\n%s", expected, oneText)
		}
	}
}

func TestRunHandoffPackageAndFileOutputs(t *testing.T) {
	root, _, _ := seedWorkflowCommandFixture(t)

	t.Run("symbol", func(t *testing.T) {
		var out bytes.Buffer
		if err := runHandoff(cli.Command{
			Name:       "handoff",
			Root:       root,
			Query:      "Run",
			Limit:      4,
			Explain:    true,
			OutputMode: cli.OutputHuman,
		}, &out); err != nil {
			t.Fatalf("runHandoff symbol returned error: %v", err)
		}
		text := stripANSICodes(out.String())
		for _, expected := range []string{"CTX Handoff", "Scope: symbol", "Data Flow", "Read First", "Review Checklist"} {
			if !strings.Contains(text, expected) {
				t.Fatalf("expected %q in symbol handoff output, got:\n%s", expected, text)
			}
		}
	})

	t.Run("package", func(t *testing.T) {
		var out bytes.Buffer
		if err := runHandoff(cli.Command{
			Name:       "handoff",
			Root:       root,
			Query:      "example.com/workflow/pkg",
			Scope:      "package",
			Limit:      4,
			Explain:    true,
			OutputMode: cli.OutputHuman,
		}, &out); err != nil {
			t.Fatalf("runHandoff package returned error: %v", err)
		}
		text := stripANSICodes(out.String())
		for _, expected := range []string{"Scope: package", "Top Files", "Tests To Run", "Review Checklist"} {
			if !strings.Contains(text, expected) {
				t.Fatalf("expected %q in package handoff output, got:\n%s", expected, text)
			}
		}
	})

	t.Run("file", func(t *testing.T) {
		var out bytes.Buffer
		if err := runHandoff(cli.Command{
			Name:       "handoff",
			Root:       root,
			Query:      "pkg/service.go",
			Scope:      "file",
			Limit:      4,
			Explain:    true,
			OutputMode: cli.OutputHuman,
		}, &out); err != nil {
			t.Fatalf("runHandoff file returned error: %v", err)
		}
		text := stripANSICodes(out.String())
		for _, expected := range []string{"Scope: file", "Key Symbols", "Nearby Surface", "Review Checklist"} {
			if !strings.Contains(text, expected) {
				t.Fatalf("expected %q in file handoff output, got:\n%s", expected, text)
			}
		}
	})
}

func TestRunReviewWorkingTreeAndSnapshotOutputs(t *testing.T) {
	root, first, second := seedWorkflowCommandFixture(t)

	writeProjectStateFixture(t, root, "api/handler.go", `package api

import "example.com/workflow/pkg"

func Handle() string { return pkg.Run() }

func HandleVerbose() string { return ">>" + pkg.Run() }
`)

	t.Run("working-tree", func(t *testing.T) {
		var out bytes.Buffer
		if err := runReview(cli.Command{
			Name:       "review",
			Root:       root,
			Scope:      "working-tree",
			Limit:      4,
			Explain:    true,
			OutputMode: cli.OutputHuman,
		}, &out); err != nil {
			t.Fatalf("runReview working-tree returned error: %v", err)
		}
		text := stripANSICodes(out.String())
		for _, expected := range []string{"CTX Review", "working tree", "Changed Paths", "Review Checklist"} {
			if !strings.Contains(text, expected) {
				t.Fatalf("expected %q in working-tree review output, got:\n%s", expected, text)
			}
		}
	})

	t.Run("snapshot", func(t *testing.T) {
		var out bytes.Buffer
		if err := runReview(cli.Command{
			Name:         "review",
			Root:         root,
			Scope:        "snapshot",
			FromSnapshot: first.ID,
			ToSnapshot:   second.ID,
			Limit:        4,
			Explain:      true,
			OutputMode:   cli.OutputHuman,
		}, &out); err != nil {
			t.Fatalf("runReview snapshot returned error: %v", err)
		}
		text := stripANSICodes(out.String())
		for _, expected := range []string{"snapshot window", "Changed Files", "Changed Symbols", "Review Checklist"} {
			if !strings.Contains(text, expected) {
				t.Fatalf("expected %q in snapshot review output, got:\n%s", expected, text)
			}
		}
	})
}

func seedWorkflowCommandFixture(t *testing.T) (string, storage.SnapshotInfo, storage.SnapshotInfo) {
	t.Helper()
	t.Setenv("CTX_HOME", t.TempDir())

	root := t.TempDir()
	writeProjectStateFixture(t, root, "go.mod", "module example.com/workflow\n\ngo 1.26\n")
	writeProjectStateFixture(t, root, "pkg/service.go", `package pkg

func Run() int { return helper() }

func helper() int { return 1 }
`)
	writeProjectStateFixture(t, root, "api/handler.go", `package api

import "example.com/workflow/pkg"

func Handle() int { return pkg.Run() }
`)
	writeProjectStateFixture(t, root, "pkg/service_test.go", `package pkg

import "testing"

func TestRun(t *testing.T) {
	if Run() != 1 {
		t.Fatal("bad")
	}
}
`)

	state, err := openProjectState(root)
	if err != nil {
		t.Fatalf("openProjectState initial returned error: %v", err)
	}
	first, committed, err := projectService.ApplySnapshot(state, "index", "initial workflow", false)
	if err != nil {
		_ = state.Close()
		t.Fatalf("ApplySnapshot initial returned error: %v", err)
	}
	if !committed {
		_ = state.Close()
		t.Fatal("expected initial snapshot to be committed")
	}
	if err := state.Close(); err != nil {
		t.Fatalf("Close initial returned error: %v", err)
	}

	writeProjectStateFixture(t, root, "pkg/service.go", `package pkg

func Run() string { return normalize("ok") }

func normalize(value string) string { return value }
`)
	writeProjectStateFixture(t, root, "api/handler.go", `package api

import "example.com/workflow/pkg"

func Handle() string { return pkg.Run() }
`)
	writeProjectStateFixture(t, root, "pkg/service_test.go", `package pkg

import "testing"

func TestRun(t *testing.T) {
	if Run() != "ok" {
		t.Fatal("bad")
	}
}
`)

	state, err = openProjectState(root)
	if err != nil {
		t.Fatalf("openProjectState second returned error: %v", err)
	}
	second, committed, err := projectService.ApplySnapshot(state, "update", "workflow change", false)
	if err != nil {
		_ = state.Close()
		t.Fatalf("ApplySnapshot second returned error: %v", err)
	}
	if !committed {
		_ = state.Close()
		t.Fatal("expected second snapshot to be committed")
	}
	if err := state.Close(); err != nil {
		t.Fatalf("Close second returned error: %v", err)
	}
	return root, first, second
}

func seedTravelCommandFixture(t *testing.T) string {
	t.Helper()
	t.Setenv("CTX_HOME", t.TempDir())

	root := t.TempDir()
	writeProjectStateFixture(t, root, "go.mod", "module example.com/travel\n\ngo 1.26\n")
	writeProjectStateFixture(t, root, "cmd/tool/main.go", `package main

import "example.com/travel/internal/pipeline"

func main() {
	cfg := parseArgs()
	result := pipeline.Run(cfg)
	emit(result)
}

func parseArgs() string { return loadConfig("cli") }

func loadConfig(source string) string { return source }

func emit(value string) string { return value }
`)
	writeProjectStateFixture(t, root, "internal/pipeline/run.go", `package pipeline

func Run(cfg string) string {
	prepared := normalize(cfg)
	return execute(prepared)
}

func normalize(value string) string { return value }

func execute(value string) string { return value + "-ok" }
`)
	writeProjectStateFixture(t, root, "internal/pipeline/run_test.go", `package pipeline

import "testing"

func TestRun(t *testing.T) {
	if Run("cli") == "" {
		t.Fatal("empty")
	}
}
`)

	state, err := openProjectState(root)
	if err != nil {
		t.Fatalf("openProjectState returned error: %v", err)
	}
	if _, committed, err := projectService.ApplySnapshot(state, "index", "travel fixture", false); err != nil {
		_ = state.Close()
		t.Fatalf("ApplySnapshot returned error: %v", err)
	} else if !committed {
		_ = state.Close()
		t.Fatal("expected travel fixture snapshot to be committed")
	}
	if err := state.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	return root
}
