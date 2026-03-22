package codebase

import (
	"path/filepath"
	"sort"
)

type ScanFile struct {
	AbsPath   string
	RelPath   string
	Hash      string
	SizeBytes int64
	IsGo      bool
	IsTest    bool
	IsModule  bool
}

type PreviousFile struct {
	RelPath           string
	PackageImportPath string
	Hash              string
	IsTest            bool
}

type ChangeSet struct {
	Added   []string
	Changed []string
	Deleted []string
}

type ChangePlan struct {
	Changes          ChangeSet
	ImpactedPackages []string
	FullReindex      bool
}

func (c ChangeSet) Count() int {
	return len(c.Added) + len(c.Changed) + len(c.Deleted)
}

func Diff(scanned []ScanFile, previous map[string]PreviousFile) ChangeSet {
	current := make(map[string]ScanFile, len(scanned))
	for _, file := range scanned {
		current[file.RelPath] = file
	}

	var changes ChangeSet
	for _, file := range scanned {
		prev, ok := previous[file.RelPath]
		if !ok {
			changes.Added = append(changes.Added, file.RelPath)
			continue
		}
		if prev.Hash != file.Hash {
			changes.Changed = append(changes.Changed, file.RelPath)
		}
	}
	for relPath := range previous {
		if _, ok := current[relPath]; !ok {
			changes.Deleted = append(changes.Deleted, relPath)
		}
	}

	sort.Strings(changes.Added)
	sort.Strings(changes.Changed)
	sort.Strings(changes.Deleted)
	return changes
}

func ScanMap(scanned []ScanFile) map[string]ScanFile {
	byPath := make(map[string]ScanFile, len(scanned))
	for _, file := range scanned {
		byPath[file.RelPath] = file
	}
	return byPath
}

func PackageImportPath(modulePath, relPath string) string {
	dir := filepath.Dir(relPath)
	if dir == "." {
		return modulePath
	}
	return modulePath + "/" + filepath.ToSlash(dir)
}

type Result struct {
	Root            string
	ModulePath      string
	GoVersion       string
	ImpactedPackage map[string]struct{}
	Packages        []PackageFact
	Files           []FileFact
	Symbols         []SymbolFact
	Dependencies    []DependencyFact
	References      []ReferenceFact
	Calls           []CallFact
	Tests           []TestFact
	TestLinks       []TestLinkFact
}

type PackageFact struct {
	ImportPath string
	Name       string
	DirPath    string
	FileCount  int
}

type FileFact struct {
	RelPath           string
	PackageImportPath string
	Hash              string
	SizeBytes         int64
	IsTest            bool
}

type SymbolFact struct {
	SymbolKey         string
	QName             string
	PackageImportPath string
	FilePath          string
	Name              string
	Kind              string
	Receiver          string
	Signature         string
	Doc               string
	Line              int
	Column            int
	Exported          bool
	IsTest            bool
}

type DependencyFact struct {
	FromPackageImportPath string
	ToPackageImportPath   string
	IsLocal               bool
}

type ReferenceFact struct {
	FromPackageImportPath string
	FromSymbolKey         string
	ToSymbolKey           string
	FilePath              string
	Line                  int
	Column                int
	Kind                  string
}

type CallFact struct {
	CallerPackageImportPath string
	CallerSymbolKey         string
	CalleeSymbolKey         string
	FilePath                string
	Line                    int
	Column                  int
	Dispatch                string
}

type TestFact struct {
	TestKey           string
	PackageImportPath string
	FilePath          string
	Name              string
	Kind              string
	Line              int
}

type TestLinkFact struct {
	TestPackageImportPath string
	TestKey               string
	SymbolKey             string
	LinkKind              string
	Confidence            string
}
