package app

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	projecttree "github.com/vladimirkasterin/ctx/internal/tree"
)

const (
	shellTreeModeFiles = "files"
	shellTreeModeDirs  = "dirs"
	shellTreeModeHot   = "hot"
)

func (s *shellSession) treeState() (string, string, int) {
	mode := s.treeMode
	if mode == "" {
		mode = shellTreeModeFiles
	}
	scope := normalizeTreeScope(s.treeScope)
	page := s.treePage
	if s.currentMode != "tree" {
		mode = shellTreeModeFiles
		scope = ""
		page = 0
	}
	return mode, scope, page
}

func (s *shellSession) parseTreeCommand(args []string) (string, string, int, error) {
	mode, scope, page := s.treeState()

	for idx := 0; idx < len(args); idx++ {
		token := strings.ToLower(strings.TrimSpace(args[idx]))
		switch token {
		case "":
			continue
		case shellTreeModeFiles, shellTreeModeDirs, shellTreeModeHot:
			if mode != token {
				page = 0
			}
			mode = token
		case "next", "more":
			page++
		case "prev", "back":
			if page > 0 {
				page--
			}
		case "root":
			scope = ""
			page = 0
		case "up":
			scope = treeParentScope(scope)
			page = 0
		case "page":
			if idx+1 >= len(args) {
				return "", "", 0, fmt.Errorf("Usage: tree [dirs|hot|next|prev|page <n>|up|root]")
			}
			value, err := strconv.Atoi(strings.TrimSpace(args[idx+1]))
			if err != nil || value < 1 {
				return "", "", 0, fmt.Errorf("Invalid tree page %q", args[idx+1])
			}
			page = value - 1
			idx++
		default:
			value, err := strconv.Atoi(token)
			if err != nil || value < 1 {
				return "", "", 0, fmt.Errorf("Usage: tree [dirs|hot|next|prev|page <n>|up|root]")
			}
			page = value - 1
		}
	}

	return mode, scope, page, nil
}

func normalizeTreeScope(scope string) string {
	scope = strings.TrimSpace(scope)
	if scope == "" || scope == "." {
		return ""
	}
	scope = filepath.ToSlash(filepath.Clean(scope))
	scope = strings.TrimPrefix(scope, "./")
	if scope == "." || scope == "/" {
		return ""
	}
	return scope
}

func treeParentScope(scope string) string {
	scope = normalizeTreeScope(scope)
	if scope == "" {
		return ""
	}
	parent := filepath.ToSlash(filepath.Dir(scope))
	if parent == "." {
		return ""
	}
	return parent
}

func joinTreePath(base, name string) string {
	if base == "" {
		return filepath.ToSlash(name)
	}
	return filepath.ToSlash(filepath.Join(base, name))
}

func pathInTreeScope(path, scope string) bool {
	scope = normalizeTreeScope(scope)
	path = filepath.ToSlash(strings.TrimSpace(path))
	if scope == "" {
		return path != ""
	}
	return path == scope || strings.HasPrefix(path, scope+"/")
}

func treeScopeLabel(scope string) string {
	scope = normalizeTreeScope(scope)
	if scope == "" {
		return "."
	}
	return scope
}

func treeNodeForScope(root *projecttree.Node, scope string) (*projecttree.Node, error) {
	scope = normalizeTreeScope(scope)
	if scope == "" {
		return root, nil
	}

	current := root
	for _, part := range strings.Split(scope, "/") {
		found := false
		for _, child := range current.Children {
			if child.IsDir && child.Name == part {
				current = child
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("Tree scope %q not found", scope)
		}
	}
	return current, nil
}

func filterTreeFilesByScope(files []shellScannedFile, scope string) []shellScannedFile {
	if normalizeTreeScope(scope) == "" {
		return append([]shellScannedFile{}, files...)
	}
	filtered := make([]shellScannedFile, 0, len(files))
	for _, file := range files {
		if pathInTreeScope(file.Path, scope) {
			filtered = append(filtered, file)
		}
	}
	return filtered
}
