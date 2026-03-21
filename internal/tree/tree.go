package tree

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/model"
)

type Node struct {
	Name     string
	IsDir    bool
	Children []*Node
}

func Build(root string, directories []string, files []model.File) *Node {
	name := filepath.Base(root)
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = root
	}

	rootNode := &Node{Name: name, IsDir: true}

	for _, dir := range directories {
		ensureNode(rootNode, dir, true)
	}
	for _, file := range files {
		ensureNode(rootNode, file.RelativePath, false)
	}

	sortChildren(rootNode)
	return rootNode
}

func ensureNode(root *Node, relativePath string, isDir bool) *Node {
	parts := strings.Split(relativePath, "/")
	current := root
	for i, part := range parts {
		last := i == len(parts)-1
		wantDir := !last || isDir
		child := findChild(current, part, wantDir)
		if child == nil {
			child = &Node{Name: part, IsDir: wantDir}
			current.Children = append(current.Children, child)
		}
		current = child
	}
	return current
}

func findChild(node *Node, name string, isDir bool) *Node {
	for _, child := range node.Children {
		if child.Name == name && child.IsDir == isDir {
			return child
		}
	}
	return nil
}

func sortChildren(node *Node) {
	slices.SortFunc(node.Children, func(a, b *Node) int {
		if a.IsDir != b.IsDir {
			if a.IsDir {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	for _, child := range node.Children {
		sortChildren(child)
	}
}
