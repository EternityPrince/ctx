package golang

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/vladimirkasterin/ctx/internal/codebase"
	"github.com/vladimirkasterin/ctx/internal/project"
)

type funcRegion struct {
	Start     token.Pos
	End       token.Pos
	SymbolKey string
}

func (a *Adapter) Analyze(info project.Info, scanned map[string]codebase.ScanFile, patterns []string) (*codebase.Result, error) {
	if len(patterns) == 0 {
		patterns = defaultLoadPatterns(scanned)
	}
	if len(patterns) == 0 {
		return nil, fmt.Errorf("no buildable Go packages found")
	}

	cfg := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedCompiledGoFiles |
			packages.NeedImports |
			packages.NeedSyntax |
			packages.NeedTypes |
			packages.NeedTypesInfo,
		Dir: info.Root,
	}

	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, fmt.Errorf("load packages: %w", err)
	}
	if err := localPackageLoadError(pkgs, info.Root, info.ModulePath); err != nil {
		return nil, err
	}

	result := &codebase.Result{
		Root:            info.Root,
		ModulePath:      info.ModulePath,
		GoVersion:       info.GoVersion,
		ImpactedPackage: make(map[string]struct{}),
	}

	symbolIndex := make(map[string]codebase.SymbolFact)
	for _, pkg := range pkgs {
		if !isLocalPackage(pkg, info.Root, info.ModulePath) {
			continue
		}
		if pkg.Types == nil || pkg.TypesInfo == nil {
			continue
		}

		result.ImpactedPackage[pkg.PkgPath] = struct{}{}
		result.Packages = append(result.Packages, codebase.PackageFact{
			ImportPath: pkg.PkgPath,
			Name:       pkg.Name,
			DirPath:    relDir(info.Root, pkg.GoFiles),
			FileCount:  len(pkg.GoFiles),
		})
		result.Dependencies = append(result.Dependencies, dependencyFacts(pkg, info.ModulePath)...)

		objectKeys := make(map[types.Object]string)
		fileRegions := make(map[string][]funcRegion)

		for i, fileAST := range pkg.Syntax {
			if i >= len(pkg.CompiledGoFiles) {
				continue
			}

			relFile := toRelPath(info.Root, pkg.CompiledGoFiles[i])
			scanFile, ok := scanned[relFile]
			if ok {
				result.Files = append(result.Files, codebase.FileFact{
					RelPath:           relFile,
					PackageImportPath: pkg.PkgPath,
					Hash:              scanFile.Hash,
					SizeBytes:         scanFile.SizeBytes,
					IsTest:            false,
				})
			}

			symbols, regions := extractSymbolsFromFile(pkg, fileAST, relFile, objectKeys)
			result.Symbols = append(result.Symbols, symbols...)
			fileRegions[relFile] = append(fileRegions[relFile], regions...)
			for _, symbol := range symbols {
				symbolIndex[symbol.SymbolKey] = symbol
			}
		}

		for i, fileAST := range pkg.Syntax {
			if i >= len(pkg.CompiledGoFiles) {
				continue
			}

			relFile := toRelPath(info.Root, pkg.CompiledGoFiles[i])
			refs, calls := extractRelationships(pkg, fileAST, relFile, fileRegions[relFile], info.ModulePath)
			result.References = append(result.References, refs...)
			result.Calls = append(result.Calls, calls...)
		}
	}

	tests, links, err := extractTests(info, scanned, symbolIndex)
	if err != nil {
		return nil, err
	}
	result.Tests = tests
	result.TestLinks = links

	codebase.SortResult(result)
	return result, nil
}

func defaultLoadPatterns(scanned map[string]codebase.ScanFile) []string {
	patternsByPath := make(map[string]struct{})
	for _, file := range scanned {
		if !file.IsGo || file.IsTest {
			continue
		}
		dir := filepath.ToSlash(filepath.Dir(file.RelPath))
		pattern := "."
		if dir != "." {
			pattern = "./" + dir
		}
		patternsByPath[pattern] = struct{}{}
	}

	patterns := make([]string, 0, len(patternsByPath))
	for pattern := range patternsByPath {
		patterns = append(patterns, pattern)
	}
	sort.Strings(patterns)
	return patterns
}

func localPackageLoadError(pkgs []*packages.Package, root, modulePath string) error {
	for _, pkg := range pkgs {
		if !isLocalPackage(pkg, root, modulePath) {
			continue
		}
		if len(pkg.Errors) == 0 {
			continue
		}
		return fmt.Errorf("package loading returned errors: %s", pkg.Errors[0].Msg)
	}
	return nil
}

func declaredFuncIdentity(fn *types.Func, relFile string, line, column int) (string, string, string, string) {
	key, qname, kind, receiver := symbolIdentityFromFunc(fn)
	if fn == nil || fn.Name() != "init" {
		return key, qname, kind, receiver
	}

	suffix := fmt.Sprintf("|%s|%d|%d", relFile, line, column)
	return key + suffix, qname + suffix, kind, receiver
}

func extractSymbolsFromFile(pkg *packages.Package, fileAST *ast.File, relFile string, objectKeys map[types.Object]string) ([]codebase.SymbolFact, []funcRegion) {
	symbols := make([]codebase.SymbolFact, 0, len(fileAST.Decls))
	regions := make([]funcRegion, 0, 4)

	for _, decl := range fileAST.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			obj, _ := pkg.TypesInfo.Defs[d.Name].(*types.Func)
			if obj == nil {
				continue
			}

			pos := pkg.Fset.Position(d.Pos())
			symbolKey, qname, kind, receiver := declaredFuncIdentity(obj, relFile, pos.Line, pos.Column)
			symbols = append(symbols, codebase.SymbolFact{
				SymbolKey:         symbolKey,
				QName:             qname,
				PackageImportPath: pkg.PkgPath,
				FilePath:          relFile,
				Name:              d.Name.Name,
				Kind:              kind,
				Receiver:          receiver,
				Signature:         types.TypeString(obj.Type(), qualifierFor(pkg.PkgPath)),
				Doc:               docText(d.Doc),
				Line:              pos.Line,
				Column:            pos.Column,
				Exported:          ast.IsExported(d.Name.Name),
			})
			objectKeys[obj] = symbolKey
			regions = append(regions, funcRegion{
				Start:     d.Pos(),
				End:       d.End(),
				SymbolKey: symbolKey,
			})

		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				continue
			}
			for _, spec := range d.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}

				obj, _ := pkg.TypesInfo.Defs[typeSpec.Name].(*types.TypeName)
				if obj == nil {
					continue
				}

				symbolKey, qname, kind := symbolIdentityFromType(obj)
				pos := pkg.Fset.Position(typeSpec.Pos())
				symbols = append(symbols, codebase.SymbolFact{
					SymbolKey:         symbolKey,
					QName:             qname,
					PackageImportPath: pkg.PkgPath,
					FilePath:          relFile,
					Name:              typeSpec.Name.Name,
					Kind:              kind,
					Signature:         types.TypeString(obj.Type(), qualifierFor(pkg.PkgPath)),
					Doc:               docTextForType(typeSpec, d),
					Line:              pos.Line,
					Column:            pos.Column,
					Exported:          ast.IsExported(typeSpec.Name.Name),
				})
				objectKeys[obj] = symbolKey
			}
		}
	}

	return symbols, regions
}

func extractRelationships(pkg *packages.Package, fileAST *ast.File, relFile string, regions []funcRegion, modulePath string) ([]codebase.ReferenceFact, []codebase.CallFact) {
	var refs []codebase.ReferenceFact
	var calls []codebase.CallFact

	ast.Inspect(fileAST, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.Ident:
			obj := pkg.TypesInfo.Uses[n]
			if obj == nil {
				return true
			}

			targetKey, _, targetKind, _, ok := symbolIdentityFromObject(obj)
			if !ok || !strings.HasPrefix(objectPackagePath(obj), modulePath) {
				return true
			}

			pos := pkg.Fset.Position(n.Pos())
			refs = append(refs, codebase.ReferenceFact{
				FromPackageImportPath: pkg.PkgPath,
				FromSymbolKey:         ownerForPos(regions, n.Pos()),
				ToSymbolKey:           targetKey,
				FilePath:              relFile,
				Line:                  pos.Line,
				Column:                pos.Column,
				Kind:                  referenceKind(targetKind),
			})

		case *ast.CallExpr:
			callerKey := ownerForPos(regions, n.Pos())
			if callerKey == "" {
				return true
			}

			obj := calledObject(pkg.TypesInfo, n.Fun)
			if obj == nil {
				return true
			}

			calleeKey, _, kind, _, ok := symbolIdentityFromObject(obj)
			if !ok || !strings.HasPrefix(objectPackagePath(obj), modulePath) {
				return true
			}
			if kind != "func" && kind != "method" {
				return true
			}

			pos := pkg.Fset.Position(n.Pos())
			calls = append(calls, codebase.CallFact{
				CallerPackageImportPath: pkg.PkgPath,
				CallerSymbolKey:         callerKey,
				CalleeSymbolKey:         calleeKey,
				FilePath:                relFile,
				Line:                    pos.Line,
				Column:                  pos.Column,
				Dispatch:                callDispatch(pkg.TypesInfo, n.Fun),
			})
		}
		return true
	})

	return refs, calls
}
