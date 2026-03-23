package app

import (
	"testing"

	projecttree "github.com/vladimirkasterin/ctx/internal/tree"
)

func TestParseTreeCommandSwitchesModeAndPreservesScope(t *testing.T) {
	session := &shellSession{
		currentMode: "tree",
		treeMode:    shellTreeModeDirs,
		treeScope:   "pkg/sub",
		treePage:    1,
	}

	mode, scope, page, err := session.parseTreeCommand([]string{"hot", "page", "3"})
	if err != nil {
		t.Fatalf("parseTreeCommand returned error: %v", err)
	}
	if mode != shellTreeModeHot {
		t.Fatalf("unexpected mode: got %q want %q", mode, shellTreeModeHot)
	}
	if scope != "pkg/sub" {
		t.Fatalf("unexpected scope: got %q", scope)
	}
	if page != 2 {
		t.Fatalf("unexpected page: got %d want %d", page, 2)
	}
}

func TestParseTreeCommandHandlesScopeNavigation(t *testing.T) {
	session := &shellSession{
		currentMode: "tree",
		treeMode:    shellTreeModeFiles,
		treeScope:   "pkg/sub",
		treePage:    4,
	}

	_, scope, page, err := session.parseTreeCommand([]string{"up"})
	if err != nil {
		t.Fatalf("parseTreeCommand returned error: %v", err)
	}
	if scope != "pkg" || page != 0 {
		t.Fatalf("unexpected up navigation result: scope=%q page=%d", scope, page)
	}

	_, scope, page, err = session.parseTreeCommand([]string{"root"})
	if err != nil {
		t.Fatalf("parseTreeCommand returned error: %v", err)
	}
	if scope != "" || page != 0 {
		t.Fatalf("unexpected root navigation result: scope=%q page=%d", scope, page)
	}
}

func TestParseTreeCommandRejectsInvalidPage(t *testing.T) {
	session := &shellSession{currentMode: "tree"}

	if _, _, _, err := session.parseTreeCommand([]string{"page", "0"}); err == nil {
		t.Fatal("expected invalid page value to fail")
	}
	if _, _, _, err := session.parseTreeCommand([]string{"unknown"}); err == nil {
		t.Fatal("expected invalid token to fail")
	}
}

func TestTreeScopeHelpers(t *testing.T) {
	if got := normalizeTreeScope("./pkg/../pkg/sub"); got != "pkg/sub" {
		t.Fatalf("unexpected normalized scope: got %q", got)
	}
	if got := treeParentScope("pkg/sub"); got != "pkg" {
		t.Fatalf("unexpected parent scope: got %q", got)
	}
	if got := joinTreePath("pkg", "service.go"); got != "pkg/service.go" {
		t.Fatalf("unexpected joined path: got %q", got)
	}
	if !pathInTreeScope("pkg/service.go", "pkg") {
		t.Fatal("expected path to be inside scope")
	}
	if pathInTreeScope("api/handler.go", "pkg") {
		t.Fatal("did not expect unrelated path to be inside scope")
	}
}

func TestTreeNodeForScopeFindsNestedDirectory(t *testing.T) {
	root := &projecttree.Node{
		Name:  "project",
		IsDir: true,
		Children: []*projecttree.Node{
			{
				Name:  "pkg",
				IsDir: true,
				Children: []*projecttree.Node{
					{Name: "sub", IsDir: true},
				},
			},
		},
	}

	node, err := treeNodeForScope(root, "pkg/sub")
	if err != nil {
		t.Fatalf("treeNodeForScope returned error: %v", err)
	}
	if node.Name != "sub" || !node.IsDir {
		t.Fatalf("unexpected scoped node: %+v", node)
	}

	if _, err := treeNodeForScope(root, "missing"); err == nil {
		t.Fatal("expected missing scope to fail")
	}
}
