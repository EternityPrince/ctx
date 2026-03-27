package python

import "github.com/vladimirkasterin/ctx/internal/codebase"

type analyzerInput struct {
	Root        string              `json:"root"`
	ProjectName string              `json:"project_name"`
	SourceRoots []string            `json:"source_roots,omitempty"`
	Patterns    []string            `json:"patterns,omitempty"`
	Files       []analyzerInputFile `json:"files"`
}

type analyzerInputFile struct {
	AbsPath string `json:"abs_path"`
	RelPath string `json:"rel_path"`
	IsTest  bool   `json:"is_test"`
}

type analyzerOutput struct {
	Packages         []codebase.PackageFact    `json:"packages"`
	Files            []codebase.FileFact       `json:"files"`
	Symbols          []codebase.SymbolFact     `json:"symbols"`
	Dependencies     []codebase.DependencyFact `json:"dependencies"`
	References       []codebase.ReferenceFact  `json:"references"`
	Calls            []codebase.CallFact       `json:"calls"`
	Tests            []codebase.TestFact       `json:"tests"`
	TestLinks        []codebase.TestLinkFact   `json:"test_links"`
	ImpactedPackages []string                  `json:"impacted_packages"`
}
