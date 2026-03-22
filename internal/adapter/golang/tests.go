package golang

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/codebase"
	"github.com/vladimirkasterin/ctx/internal/project"
)

func extractTests(info project.Info, scanned map[string]codebase.ScanFile, symbols map[string]codebase.SymbolFact) ([]codebase.TestFact, []codebase.TestLinkFact, error) {
	byPackage := make(map[string][]codebase.SymbolFact)
	allSymbols := make([]codebase.SymbolFact, 0, len(symbols))
	for _, symbol := range symbols {
		byPackage[symbol.PackageImportPath] = append(byPackage[symbol.PackageImportPath], symbol)
		allSymbols = append(allSymbols, symbol)
	}

	tests := make([]codebase.TestFact, 0)
	links := make([]codebase.TestLinkFact, 0)
	fset := token.NewFileSet()

	for _, file := range scanned {
		if !file.IsTest {
			continue
		}

		astFile, err := parser.ParseFile(fset, file.AbsPath, nil, parser.ParseComments)
		if err != nil {
			return nil, nil, fmt.Errorf("parse test file %s: %w", file.RelPath, err)
		}

		pkgImportPath := codebase.PackageImportPath(info.ModulePath, file.RelPath)
		for _, decl := range astFile.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil {
				continue
			}

			testKind := testKindForName(fn.Name.Name)
			if testKind == "" {
				continue
			}

			pos := fset.Position(fn.Pos())
			testKey := fmt.Sprintf("test|%s|%s|%s", pkgImportPath, file.RelPath, fn.Name.Name)
			tests = append(tests, codebase.TestFact{
				TestKey:           testKey,
				PackageImportPath: pkgImportPath,
				FilePath:          file.RelPath,
				Name:              fn.Name.Name,
				Kind:              testKind,
				Line:              pos.Line,
			})

			links = append(links, linkTests(testKey, pkgImportPath, fn.Name.Name, byPackage[pkgImportPath], allSymbols)...)
		}
	}

	return tests, dedupeTestLinks(links), nil
}

func linkTests(testKey, packageImportPath, testName string, packageSymbols, allSymbols []codebase.SymbolFact) []codebase.TestLinkFact {
	base := trimTestPrefix(testName)
	if base == "" {
		return nil
	}

	baseParts := strings.Split(base, "_")
	normalizedBase := normalizeName(base)
	links := make([]codebase.TestLinkFact, 0, 4)

	addLink := func(symbol codebase.SymbolFact, linkKind, confidence string) {
		links = append(links, codebase.TestLinkFact{
			TestPackageImportPath: packageImportPath,
			TestKey:               testKey,
			SymbolKey:             symbol.SymbolKey,
			LinkKind:              linkKind,
			Confidence:            confidence,
		})
	}

	for _, symbol := range packageSymbols {
		if normalizeName(symbol.Name) == normalizedBase {
			addLink(symbol, "name_match", "medium")
		}
		if len(baseParts) == 2 && symbol.Kind == "method" {
			recv := normalizeName(strings.TrimPrefix(symbol.Receiver, "*"))
			if recv == normalizeName(baseParts[0]) && normalizeName(symbol.Name) == normalizeName(baseParts[1]) {
				addLink(symbol, "receiver_match", "high")
			}
		}
	}

	if len(links) > 0 {
		return links
	}

	for _, symbol := range allSymbols {
		if normalizeName(symbol.Name) == normalizedBase {
			addLink(symbol, "global_name_match", "low")
		}
	}
	return links
}

func dedupeTestLinks(links []codebase.TestLinkFact) []codebase.TestLinkFact {
	seen := make(map[string]struct{}, len(links))
	result := make([]codebase.TestLinkFact, 0, len(links))
	for _, link := range links {
		key := link.TestKey + "|" + link.SymbolKey + "|" + link.LinkKind
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, link)
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].TestKey == result[j].TestKey {
			return result[i].SymbolKey < result[j].SymbolKey
		}
		return result[i].TestKey < result[j].TestKey
	})
	return result
}
