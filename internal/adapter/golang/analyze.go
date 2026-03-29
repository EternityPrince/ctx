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
	Start         token.Pos
	End           token.Pos
	SymbolKey     string
	ReceiverName  string
	ReceiverLabel string
	ParamNames    map[string]struct{}
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
		if len(pkg.Errors) > 0 {
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
			refs, calls, flows := extractRelationships(pkg, fileAST, relFile, fileRegions[relFile], info.ModulePath)
			result.References = append(result.References, refs...)
			result.Calls = append(result.Calls, calls...)
			result.Flows = append(result.Flows, flows...)
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
	healthyLocalPackages := 0
	errors := make([]string, 0, len(pkgs))
	seen := make(map[string]struct{})
	for _, pkg := range pkgs {
		if !isLocalPackage(pkg, root, modulePath) {
			continue
		}
		if len(pkg.Errors) == 0 && pkg.Types != nil && pkg.TypesInfo != nil {
			healthyLocalPackages++
			continue
		}
		if len(pkg.Errors) == 0 {
			continue
		}

		message := pkg.Errors[0].Msg
		if pkg.PkgPath != "" {
			message = pkg.PkgPath + ": " + message
		}
		if _, ok := seen[message]; ok {
			continue
		}
		seen[message] = struct{}{}
		errors = append(errors, message)
	}
	if healthyLocalPackages > 0 {
		return nil
	}
	if len(errors) > 0 {
		return fmt.Errorf("package loading returned errors: %s", strings.Join(errors, "; "))
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
				Start:         d.Pos(),
				End:           d.End(),
				SymbolKey:     symbolKey,
				ReceiverName:  funcReceiverName(d),
				ReceiverLabel: receiver,
				ParamNames:    funcParamNames(d),
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

func extractRelationships(pkg *packages.Package, fileAST *ast.File, relFile string, regions []funcRegion, modulePath string) ([]codebase.ReferenceFact, []codebase.CallFact, []codebase.FlowFact) {
	var refs []codebase.ReferenceFact
	var calls []codebase.CallFact
	var flows []codebase.FlowFact

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
			ownerRegion, ok := regionForPos(regions, n.Pos())
			if !ok || ownerRegion.SymbolKey == "" {
				return true
			}

			obj := calledObject(pkg.TypesInfo, n.Fun)
			if obj == nil {
				return true
			}

			calleeKey, calleeQName, kind, _, ok := symbolIdentityFromObject(obj)
			if !ok || !strings.HasPrefix(objectPackagePath(obj), modulePath) {
				return true
			}
			if kind != "func" && kind != "method" {
				return true
			}

			pos := pkg.Fset.Position(n.Pos())
			calls = append(calls, codebase.CallFact{
				CallerPackageImportPath: pkg.PkgPath,
				CallerSymbolKey:         ownerRegion.SymbolKey,
				CalleeSymbolKey:         calleeKey,
				FilePath:                relFile,
				Line:                    pos.Line,
				Column:                  pos.Column,
				Dispatch:                callDispatch(pkg.TypesInfo, n.Fun),
			})
			flows = append(flows, goCallFlowFacts(pkg.PkgPath, ownerRegion, relFile, pos.Line, pos.Column, n, calleeKey, calleeQName)...)

		case *ast.ReturnStmt:
			ownerRegion, ok := regionForPos(regions, n.Pos())
			if !ok || ownerRegion.SymbolKey == "" {
				return true
			}
			flows = append(flows, goReturnFlowFacts(pkg, ownerRegion, relFile, n, modulePath)...)
		}
		return true
	})

	return refs, calls, flows
}

func funcReceiverName(decl *ast.FuncDecl) string {
	if decl == nil || decl.Recv == nil || len(decl.Recv.List) == 0 {
		return ""
	}
	for _, field := range decl.Recv.List {
		for _, name := range field.Names {
			if name != nil && strings.TrimSpace(name.Name) != "" {
				return name.Name
			}
		}
	}
	return ""
}

func funcParamNames(decl *ast.FuncDecl) map[string]struct{} {
	params := make(map[string]struct{})
	if decl == nil || decl.Type == nil || decl.Type.Params == nil {
		return params
	}
	for _, field := range decl.Type.Params.List {
		for _, name := range field.Names {
			if name == nil {
				continue
			}
			if value := strings.TrimSpace(name.Name); value != "" {
				params[value] = struct{}{}
			}
		}
	}
	return params
}

func regionForPos(regions []funcRegion, pos token.Pos) (funcRegion, bool) {
	for _, region := range regions {
		if pos >= region.Start && pos <= region.End {
			return region, true
		}
	}
	return funcRegion{}, false
}

func flowSourceForExpr(expr ast.Expr, region funcRegion) (string, string) {
	switch value := expr.(type) {
	case *ast.ParenExpr:
		return flowSourceForExpr(value.X, region)
	case *ast.StarExpr:
		return flowSourceForExpr(value.X, region)
	case *ast.UnaryExpr:
		return flowSourceForExpr(value.X, region)
	case *ast.Ident:
		if region.ReceiverName != "" && value.Name == region.ReceiverName {
			return "receiver", receiverFlowLabel(region)
		}
		if _, ok := region.ParamNames[value.Name]; ok {
			return "param", value.Name
		}
	case *ast.SelectorExpr:
		if ident, ok := value.X.(*ast.Ident); ok {
			if region.ReceiverName != "" && ident.Name == region.ReceiverName {
				return "receiver", receiverFlowLabel(region)
			}
			if _, ok := region.ParamNames[ident.Name]; ok {
				return "param", ident.Name
			}
		}
	}
	return "", ""
}

func receiverFlowLabel(region funcRegion) string {
	if strings.TrimSpace(region.ReceiverLabel) != "" {
		return region.ReceiverLabel
	}
	return "receiver"
}

func goCallFlowFacts(pkgPath string, region funcRegion, relFile string, line, column int, call *ast.CallExpr, calleeKey, calleeQName string) []codebase.FlowFact {
	flows := make([]codebase.FlowFact, 0, len(call.Args)+1)
	if sourceKind, sourceLabel := flowSourceForExpr(call.Fun, region); sourceKind == "receiver" {
		flows = append(flows, codebase.FlowFact{
			OwnerPackageImportPath: pkgPath,
			OwnerSymbolKey:         region.SymbolKey,
			FilePath:               relFile,
			Line:                   line,
			Column:                 column,
			Kind:                   "receiver_to_call",
			SourceKind:             sourceKind,
			SourceLabel:            sourceLabel,
			TargetKind:             "call",
			TargetLabel:            calleeQName,
			TargetSymbolKey:        calleeKey,
		})
	}
	for _, arg := range call.Args {
		sourceKind, sourceLabel := flowSourceForExpr(arg, region)
		if sourceKind == "" || sourceLabel == "" {
			continue
		}
		flows = append(flows, codebase.FlowFact{
			OwnerPackageImportPath: pkgPath,
			OwnerSymbolKey:         region.SymbolKey,
			FilePath:               relFile,
			Line:                   line,
			Column:                 column,
			Kind:                   sourceKind + "_to_call",
			SourceKind:             sourceKind,
			SourceLabel:            sourceLabel,
			TargetKind:             "call",
			TargetLabel:            calleeQName,
			TargetSymbolKey:        calleeKey,
		})
	}
	return flows
}

func goReturnFlowFacts(pkg *packages.Package, region funcRegion, relFile string, stmt *ast.ReturnStmt, modulePath string) []codebase.FlowFact {
	flows := make([]codebase.FlowFact, 0, len(stmt.Results))
	for _, result := range stmt.Results {
		pos := pkg.Fset.Position(result.Pos())
		if callExpr, ok := result.(*ast.CallExpr); ok {
			obj := calledObject(pkg.TypesInfo, callExpr.Fun)
			calleeKey, calleeQName, kind, _, ok := symbolIdentityFromObject(obj)
			if ok && strings.HasPrefix(objectPackagePath(obj), modulePath) && (kind == "func" || kind == "method") {
				flows = append(flows, codebase.FlowFact{
					OwnerPackageImportPath: pkg.PkgPath,
					OwnerSymbolKey:         region.SymbolKey,
					FilePath:               relFile,
					Line:                   pos.Line,
					Column:                 pos.Column,
					Kind:                   "call_to_return",
					SourceKind:             "call",
					SourceLabel:            calleeQName,
					SourceSymbolKey:        calleeKey,
					TargetKind:             "return",
					TargetLabel:            "return",
				})
				continue
			}
		}

		sourceKind, sourceLabel := flowSourceForExpr(result, region)
		if sourceKind == "" || sourceLabel == "" {
			continue
		}
		flows = append(flows, codebase.FlowFact{
			OwnerPackageImportPath: pkg.PkgPath,
			OwnerSymbolKey:         region.SymbolKey,
			FilePath:               relFile,
			Line:                   pos.Line,
			Column:                 pos.Column,
			Kind:                   sourceKind + "_to_return",
			SourceKind:             sourceKind,
			SourceLabel:            sourceLabel,
			TargetKind:             "return",
			TargetLabel:            "return",
		})
	}
	return flows
}
