package tree

import (
	"path/filepath"
	"testing"

	"github.com/vladimirkasterin/ctx/internal/model"
)

func TestBuildCreatesSortedHierarchy(t *testing.T) {
	root := Build("/tmp/project", []string{
		"internal/adapter",
		"internal/adapter/python",
		"docs",
	}, []model.File{
		{RelativePath: "README.md"},
		{RelativePath: "internal/adapter/adapter.go"},
		{RelativePath: "internal/adapter/python/analyzer.py"},
		{RelativePath: "docs/guide.txt"},
	})

	if !root.IsDir {
		t.Fatal("expected root node to be a directory")
	}
	if root.Name != "project" {
		t.Fatalf("unexpected root name: got %q want %q", root.Name, "project")
	}

	assertNodeOrder(t, root, []nodeShape{
		{Name: "docs", IsDir: true},
		{Name: "internal", IsDir: true},
		{Name: "README.md", IsDir: false},
	})

	internal := mustChild(t, root, "internal", true)
	adapter := mustChild(t, internal, "adapter", true)
	assertNodeOrder(t, adapter, []nodeShape{
		{Name: "python", IsDir: true},
		{Name: "adapter.go", IsDir: false},
	})

	python := mustChild(t, adapter, "python", true)
	assertNodeOrder(t, python, []nodeShape{
		{Name: "analyzer.py", IsDir: false},
	})
}

func TestBuildKeepsFilesystemRootName(t *testing.T) {
	root := Build(string(filepath.Separator), nil, nil)
	if root.Name != string(filepath.Separator) {
		t.Fatalf("unexpected filesystem root name: got %q", root.Name)
	}
}

type nodeShape struct {
	Name  string
	IsDir bool
}

func assertNodeOrder(t *testing.T, node *Node, want []nodeShape) {
	t.Helper()
	if len(node.Children) != len(want) {
		t.Fatalf("unexpected child count for %s: got %d want %d", node.Name, len(node.Children), len(want))
	}
	for idx, child := range node.Children {
		if child.Name != want[idx].Name || child.IsDir != want[idx].IsDir {
			t.Fatalf("unexpected child at %d for %s: got (%q, dir=%v) want (%q, dir=%v)", idx, node.Name, child.Name, child.IsDir, want[idx].Name, want[idx].IsDir)
		}
	}
}

func mustChild(t *testing.T, node *Node, name string, isDir bool) *Node {
	t.Helper()
	child := findChild(node, name, isDir)
	if child == nil {
		t.Fatalf("expected child %q (dir=%v) under %q", name, isDir, node.Name)
	}
	return child
}
