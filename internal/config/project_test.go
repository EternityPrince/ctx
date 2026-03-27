package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProjectParsesProfilesAndAppliesDumpDefaults(t *testing.T) {
	root := t.TempDir()
	writeConfigFixture(t, root, `.ctxconfig`, `
include_hidden = true
extensions = .go, .rs

[dump]
max_file_size = 64
include_generated = true
exclude_paths = tmp/**
include_paths = tmp/keep.rs
`)

	cfg, err := LoadProject(root)
	if err != nil {
		t.Fatalf("LoadProject returned error: %v", err)
	}
	if !HasConfigFile(cfg) {
		t.Fatal("expected config file to be detected")
	}

	profile := EffectiveProfile(cfg, "dump")
	if profile.IncludeHidden == nil || !*profile.IncludeHidden {
		t.Fatalf("expected include_hidden to flow from global profile, got %+v", profile)
	}
	if profile.MaxFileSize == nil || *profile.MaxFileSize != 64 {
		t.Fatalf("expected dump max_file_size=64, got %+v", profile.MaxFileSize)
	}
	if len(profile.Extensions) != 2 || profile.Extensions[0] != ".go" || profile.Extensions[1] != ".rs" {
		t.Fatalf("unexpected extensions: %+v", profile.Extensions)
	}
	if len(profile.ExcludePaths) != 1 || profile.ExcludePaths[0] != "tmp/**" {
		t.Fatalf("unexpected exclude paths: %+v", profile.ExcludePaths)
	}

	options := ApplyProfile(Options{Root: root, MaxFileSize: -1}, cfg, "dump")
	if !options.IncludeHidden || options.MaxFileSize != 64 || !options.IncludeGenerated {
		t.Fatalf("expected dump profile to be applied, got %+v", options)
	}
}

func TestLoadProjectRejectsUnknownKeys(t *testing.T) {
	root := t.TempDir()
	writeConfigFixture(t, root, `.ctxconfig`, `mystery = true`)

	if _, err := LoadProject(root); err == nil {
		t.Fatal("expected invalid key to fail parsing")
	}
}

func writeConfigFixture(t *testing.T, root, relPath, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
