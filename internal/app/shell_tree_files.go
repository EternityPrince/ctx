package app

import (
	"path/filepath"

	"github.com/vladimirkasterin/ctx/internal/storage"
	projecttree "github.com/vladimirkasterin/ctx/internal/tree"
)

func buildShellTreeLines(root *projecttree.Node, scope string, scannedFiles []shellScannedFile, summaries map[string]storage.FileSummary, dirLines map[string]int, currentFile string) []shellTreeLine {
	fileByPath := make(map[string]shellScannedFile, len(scannedFiles))
	for _, file := range scannedFiles {
		fileByPath[file.Path] = file
	}

	lines := []shellTreeLine{{
		Text:     root.Name + "/",
		Path:     scope,
		IsDir:    true,
		DirLines: dirLines[scope],
	}}
	fileIndex := 0
	for idx, child := range root.Children {
		appendShellTreeNode(&lines, child, "", idx == len(root.Children)-1, joinTreePath(scope, child.Name), currentFile, fileByPath, summaries, dirLines, &fileIndex)
	}
	return lines
}

func appendShellTreeNode(lines *[]shellTreeLine, node *projecttree.Node, prefix string, last bool, relPath, currentFile string, fileByPath map[string]shellScannedFile, summaries map[string]storage.FileSummary, dirLines map[string]int, fileIndex *int) {
	branch := "|-- "
	nextPrefix := prefix + "|   "
	if last {
		branch = "`-- "
		nextPrefix = prefix + "    "
	}

	line := shellTreeLine{
		Text:     prefix + branch + node.Name,
		Path:     filepath.ToSlash(relPath),
		IsDir:    node.IsDir,
		DirLines: dirLines[filepath.ToSlash(relPath)],
		Active:   filepath.ToSlash(relPath) == filepath.ToSlash(currentFile),
	}
	if node.IsDir {
		line.Text += "/"
	} else {
		line.Scanned = fileByPath[line.Path]
		line.Summary = summaries[line.Path]
		line.IsTest = line.Scanned.IsTest || line.Summary.IsTest
		*fileIndex++
		line.FileIndex = *fileIndex
	}
	*lines = append(*lines, line)

	for idx, child := range node.Children {
		childPath := filepath.ToSlash(filepath.Join(relPath, child.Name))
		appendShellTreeNode(lines, child, nextPrefix, idx == len(node.Children)-1, childPath, currentFile, fileByPath, summaries, dirLines, fileIndex)
	}
}
