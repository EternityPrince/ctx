package indexer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
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

type ChangeSet struct {
	Added   []string
	Changed []string
	Deleted []string
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

type PreviousFile struct {
	RelPath           string
	PackageImportPath string
	Hash              string
	IsTest            bool
}

type PreviousSymbol struct {
	SymbolKey         string
	PackageImportPath string
	Name              string
	Kind              string
	Receiver          string
}

func Scan(root string) ([]ScanFile, error) {
	files := make([]ScanFile, 0, 64)

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor":
				if path != root {
					return filepath.SkipDir
				}
			}
			return nil
		}

		name := d.Name()
		isGo := strings.HasSuffix(name, ".go")
		isModule := name == "go.mod" || name == "go.sum"
		if !isGo && !isModule {
			return nil
		}

		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("make relative path: %w", err)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read file %s: %w", path, err)
		}

		sum := sha256.Sum256(data)
		files = append(files, ScanFile{
			AbsPath:   path,
			RelPath:   filepath.ToSlash(relPath),
			Hash:      hex.EncodeToString(sum[:]),
			SizeBytes: int64(len(data)),
			IsGo:      isGo,
			IsTest:    isGo && strings.HasSuffix(name, "_test.go"),
			IsModule:  isModule,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].RelPath < files[j].RelPath
	})
	return files, nil
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

func DetectImpactedPackages(root, modulePath string, scanned []ScanFile, previous map[string]PreviousFile) (ChangeSet, []string, bool) {
	changes := Diff(scanned, previous)
	if len(previous) == 0 {
		return changes, nil, true
	}

	impacted := make(map[string]struct{})
	fullReindex := false

	for _, relPath := range append(append([]string{}, changes.Added...), changes.Changed...) {
		file := findScanFile(scanned, relPath)
		if file == nil {
			continue
		}
		if file.IsModule {
			fullReindex = true
			continue
		}
		pkg := derivePackageForFile(root, modulePath, file.RelPath)
		if prev, ok := previous[file.RelPath]; ok && prev.PackageImportPath != "" {
			pkg = prev.PackageImportPath
		}
		if pkg != "" {
			impacted[pkg] = struct{}{}
		}
	}

	for _, relPath := range changes.Deleted {
		prev, ok := previous[relPath]
		if !ok {
			continue
		}
		if prev.PackageImportPath == "" {
			fullReindex = true
			continue
		}
		impacted[prev.PackageImportPath] = struct{}{}
	}

	list := make([]string, 0, len(impacted))
	for pkg := range impacted {
		list = append(list, pkg)
	}
	sort.Strings(list)
	return changes, list, fullReindex
}

func Analyze(root, modulePath, goVersion string, scanned map[string]ScanFile, patterns []string) (*Result, error) {
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}

	cfg := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedCompiledGoFiles |
			packages.NeedImports |
			packages.NeedSyntax |
			packages.NeedTypes |
			packages.NeedTypesInfo,
		Dir: root,
	}

	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, fmt.Errorf("load packages: %w", err)
	}

	if packages.PrintErrors(pkgs) > 0 {
		return nil, fmt.Errorf("package loading returned errors")
	}

	result := &Result{
		Root:            root,
		ModulePath:      modulePath,
		GoVersion:       goVersion,
		ImpactedPackage: make(map[string]struct{}),
	}

	symbolIndex := make(map[string]SymbolFact)

	for _, pkg := range pkgs {
		if !isLocalPackage(pkg, root, modulePath) {
			continue
		}
		if pkg.Types == nil || pkg.TypesInfo == nil {
			continue
		}

		result.ImpactedPackage[pkg.PkgPath] = struct{}{}
		result.Packages = append(result.Packages, PackageFact{
			ImportPath: pkg.PkgPath,
			Name:       pkg.Name,
			DirPath:    relDir(root, pkg.GoFiles),
			FileCount:  len(pkg.GoFiles),
		})

		for _, dep := range dependencyFacts(pkg, modulePath) {
			result.Dependencies = append(result.Dependencies, dep)
		}

		objectKeys := make(map[types.Object]string)
		fileRegions := make(map[string][]funcRegion)

		for i, fileAST := range pkg.Syntax {
			if i >= len(pkg.CompiledGoFiles) {
				continue
			}
			absFile := pkg.CompiledGoFiles[i]
			relFile := toRelPath(root, absFile)
			scanFile, ok := scanned[relFile]
			if ok {
				result.Files = append(result.Files, FileFact{
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
			relFile := toRelPath(root, pkg.CompiledGoFiles[i])
			refs, calls := extractRelationships(pkg, fileAST, relFile, objectKeys, fileRegions[relFile], modulePath)
			result.References = append(result.References, refs...)
			result.Calls = append(result.Calls, calls...)
		}
	}

	tests, links, err := extractTests(root, modulePath, scanned, symbolIndex)
	if err != nil {
		return nil, err
	}
	result.Tests = tests
	result.TestLinks = links

	sort.Slice(result.Packages, func(i, j int) bool {
		return result.Packages[i].ImportPath < result.Packages[j].ImportPath
	})
	sort.Slice(result.Files, func(i, j int) bool {
		return result.Files[i].RelPath < result.Files[j].RelPath
	})
	sort.Slice(result.Symbols, func(i, j int) bool {
		return result.Symbols[i].QName < result.Symbols[j].QName
	})
	sort.Slice(result.References, func(i, j int) bool {
		if result.References[i].ToSymbolKey == result.References[j].ToSymbolKey {
			if result.References[i].FilePath == result.References[j].FilePath {
				return result.References[i].Line < result.References[j].Line
			}
			return result.References[i].FilePath < result.References[j].FilePath
		}
		return result.References[i].ToSymbolKey < result.References[j].ToSymbolKey
	})
	sort.Slice(result.Calls, func(i, j int) bool {
		if result.Calls[i].CallerSymbolKey == result.Calls[j].CallerSymbolKey {
			return result.Calls[i].CalleeSymbolKey < result.Calls[j].CalleeSymbolKey
		}
		return result.Calls[i].CallerSymbolKey < result.Calls[j].CallerSymbolKey
	})
	sort.Slice(result.Tests, func(i, j int) bool {
		return result.Tests[i].TestKey < result.Tests[j].TestKey
	})
	sort.Slice(result.TestLinks, func(i, j int) bool {
		if result.TestLinks[i].TestKey == result.TestLinks[j].TestKey {
			return result.TestLinks[i].SymbolKey < result.TestLinks[j].SymbolKey
		}
		return result.TestLinks[i].TestKey < result.TestLinks[j].TestKey
	})

	return result, nil
}

type funcRegion struct {
	Start     token.Pos
	End       token.Pos
	SymbolKey string
}

func extractSymbolsFromFile(pkg *packages.Package, fileAST *ast.File, relFile string, objectKeys map[types.Object]string) ([]SymbolFact, []funcRegion) {
	symbols := make([]SymbolFact, 0, len(fileAST.Decls))
	regions := make([]funcRegion, 0, 4)

	for _, decl := range fileAST.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			obj, _ := pkg.TypesInfo.Defs[d.Name].(*types.Func)
			if obj == nil {
				continue
			}

			symbolKey, qname, kind, receiver := symbolIdentityFromFunc(obj)
			signature := types.TypeString(obj.Type(), qualifierFor(pkg.PkgPath))
			pos := pkg.Fset.Position(d.Pos())
			symbols = append(symbols, SymbolFact{
				SymbolKey:         symbolKey,
				QName:             qname,
				PackageImportPath: pkg.PkgPath,
				FilePath:          relFile,
				Name:              d.Name.Name,
				Kind:              kind,
				Receiver:          receiver,
				Signature:         signature,
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
				symbols = append(symbols, SymbolFact{
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

func extractRelationships(pkg *packages.Package, fileAST *ast.File, relFile string, objectKeys map[types.Object]string, regions []funcRegion, modulePath string) ([]ReferenceFact, []CallFact) {
	var refs []ReferenceFact
	var calls []CallFact

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
			refs = append(refs, ReferenceFact{
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
			calls = append(calls, CallFact{
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

func extractTests(root, modulePath string, scanned map[string]ScanFile, symbols map[string]SymbolFact) ([]TestFact, []TestLinkFact, error) {
	byPackage := make(map[string][]SymbolFact)
	allSymbols := make([]SymbolFact, 0, len(symbols))
	for _, symbol := range symbols {
		byPackage[symbol.PackageImportPath] = append(byPackage[symbol.PackageImportPath], symbol)
		allSymbols = append(allSymbols, symbol)
	}

	tests := make([]TestFact, 0)
	links := make([]TestLinkFact, 0)
	fset := token.NewFileSet()

	for _, file := range scanned {
		if !file.IsTest {
			continue
		}

		astFile, err := parser.ParseFile(fset, file.AbsPath, nil, parser.ParseComments)
		if err != nil {
			return nil, nil, fmt.Errorf("parse test file %s: %w", file.RelPath, err)
		}

		pkgImportPath := derivePackageForFile(root, modulePath, file.RelPath)
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
			tests = append(tests, TestFact{
				TestKey:           testKey,
				PackageImportPath: pkgImportPath,
				FilePath:          file.RelPath,
				Name:              fn.Name.Name,
				Kind:              testKind,
				Line:              pos.Line,
			})

			for _, link := range linkTests(testKey, pkgImportPath, fn.Name.Name, byPackage[pkgImportPath], allSymbols) {
				links = append(links, link)
			}
		}
	}

	return tests, dedupeTestLinks(links), nil
}

func linkTests(testKey, packageImportPath, testName string, packageSymbols, allSymbols []SymbolFact) []TestLinkFact {
	base := trimTestPrefix(testName)
	if base == "" {
		return nil
	}

	baseParts := strings.Split(base, "_")
	normalizedBase := normalizeName(base)
	links := make([]TestLinkFact, 0, 4)

	addLink := func(symbol SymbolFact, linkKind, confidence string) {
		links = append(links, TestLinkFact{
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

func dedupeTestLinks(links []TestLinkFact) []TestLinkFact {
	seen := make(map[string]struct{}, len(links))
	result := make([]TestLinkFact, 0, len(links))
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

func dependencyFacts(pkg *packages.Package, modulePath string) []DependencyFact {
	deps := make([]DependencyFact, 0, len(pkg.Imports))
	for _, dep := range pkg.Imports {
		if dep == nil || dep.PkgPath == "" {
			continue
		}
		deps = append(deps, DependencyFact{
			FromPackageImportPath: pkg.PkgPath,
			ToPackageImportPath:   dep.PkgPath,
			IsLocal:               strings.HasPrefix(dep.PkgPath, modulePath),
		})
	}
	sort.Slice(deps, func(i, j int) bool {
		return deps[i].ToPackageImportPath < deps[j].ToPackageImportPath
	})
	return deps
}

func isLocalPackage(pkg *packages.Package, root, modulePath string) bool {
	if pkg == nil {
		return false
	}
	if !strings.HasPrefix(pkg.PkgPath, modulePath) {
		return false
	}
	for _, file := range append([]string{}, pkg.CompiledGoFiles...) {
		if isWithinRoot(root, file) {
			return true
		}
	}
	for _, file := range pkg.GoFiles {
		if isWithinRoot(root, file) {
			return true
		}
	}
	return false
}

func toRelPath(root, path string) string {
	relPath, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(relPath)
}

func relDir(root string, files []string) string {
	if len(files) == 0 {
		return "."
	}
	relPath := toRelPath(root, filepath.Dir(files[0]))
	if relPath == "" {
		return "."
	}
	return relPath
}

func ownerForPos(regions []funcRegion, pos token.Pos) string {
	for _, region := range regions {
		if pos >= region.Start && pos <= region.End {
			return region.SymbolKey
		}
	}
	return ""
}

func calledObject(info *types.Info, expr ast.Expr) types.Object {
	switch v := expr.(type) {
	case *ast.Ident:
		return info.Uses[v]
	case *ast.SelectorExpr:
		return info.Uses[v.Sel]
	default:
		return nil
	}
}

func callDispatch(info *types.Info, expr ast.Expr) string {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return "static"
	}
	selection := info.Selections[selector]
	if selection == nil {
		return "static"
	}
	if _, ok := selection.Recv().Underlying().(*types.Interface); ok {
		return "interface"
	}
	return "static"
}

func symbolIdentityFromObject(obj types.Object) (string, string, string, string, bool) {
	if obj == nil || obj.Pkg() == nil {
		return "", "", "", "", false
	}
	switch v := obj.(type) {
	case *types.Func:
		key, qname, kind, receiver := symbolIdentityFromFunc(v)
		return key, qname, kind, receiver, true
	case *types.TypeName:
		key, qname, kind := symbolIdentityFromType(v)
		return key, qname, kind, "", true
	default:
		return "", "", "", "", false
	}
}

func symbolIdentityFromFunc(fn *types.Func) (string, string, string, string) {
	sig, _ := fn.Type().(*types.Signature)
	if sig != nil && sig.Recv() != nil {
		display, stable := receiverIdentity(sig.Recv().Type())
		key := "method|" + fn.Pkg().Path() + "|" + stable + "|" + fn.Name()
		qname := fn.Pkg().Path() + ".(" + display + ")." + fn.Name()
		return key, qname, "method", display
	}
	key := "func|" + fn.Pkg().Path() + "|" + fn.Name()
	qname := fn.Pkg().Path() + "." + fn.Name()
	return key, qname, "func", ""
}

func symbolIdentityFromType(obj *types.TypeName) (string, string, string) {
	kind := "type"
	if obj.IsAlias() {
		kind = "alias"
	} else {
		switch obj.Type().Underlying().(type) {
		case *types.Struct:
			kind = "struct"
		case *types.Interface:
			kind = "interface"
		}
	}
	key := "type|" + obj.Pkg().Path() + "|" + obj.Name()
	qname := obj.Pkg().Path() + "." + obj.Name()
	return key, qname, kind
}

func receiverIdentity(t types.Type) (string, string) {
	switch v := t.(type) {
	case *types.Pointer:
		if named, ok := v.Elem().(*types.Named); ok && named.Obj() != nil && named.Obj().Pkg() != nil {
			display := "*" + named.Obj().Name()
			stable := named.Obj().Pkg().Path() + "." + named.Obj().Name() + "|ptr"
			return display, stable
		}
	case *types.Named:
		if v.Obj() != nil && v.Obj().Pkg() != nil {
			display := v.Obj().Name()
			stable := v.Obj().Pkg().Path() + "." + v.Obj().Name() + "|val"
			return display, stable
		}
	}
	display := types.TypeString(t, qualifierFor(""))
	return display, display
}

func objectPackagePath(obj types.Object) string {
	if obj == nil || obj.Pkg() == nil {
		return ""
	}
	return obj.Pkg().Path()
}

func qualifierFor(currentPkg string) types.Qualifier {
	return func(other *types.Package) string {
		if other == nil {
			return ""
		}
		if currentPkg != "" && other.Path() == currentPkg {
			return ""
		}
		return other.Path()
	}
}

func referenceKind(kind string) string {
	if kind == "struct" || kind == "interface" || kind == "type" || kind == "alias" {
		return "type_ref"
	}
	return "value_ref"
}

func docTextForType(typeSpec *ast.TypeSpec, decl *ast.GenDecl) string {
	if typeSpec.Doc != nil {
		return strings.TrimSpace(typeSpec.Doc.Text())
	}
	if decl.Doc != nil {
		return strings.TrimSpace(decl.Doc.Text())
	}
	return ""
}

func docText(group *ast.CommentGroup) string {
	if group == nil {
		return ""
	}
	return strings.TrimSpace(group.Text())
}

func testKindForName(name string) string {
	switch {
	case strings.HasPrefix(name, "Test"):
		return "test"
	case strings.HasPrefix(name, "Benchmark"):
		return "benchmark"
	case strings.HasPrefix(name, "Fuzz"):
		return "fuzz"
	default:
		return ""
	}
}

func trimTestPrefix(name string) string {
	switch {
	case strings.HasPrefix(name, "Test"):
		return strings.TrimPrefix(name, "Test")
	case strings.HasPrefix(name, "Benchmark"):
		return strings.TrimPrefix(name, "Benchmark")
	case strings.HasPrefix(name, "Fuzz"):
		return strings.TrimPrefix(name, "Fuzz")
	default:
		return ""
	}
}

func normalizeName(value string) string {
	value = strings.ReplaceAll(value, "_", "")
	value = strings.ReplaceAll(value, "-", "")
	return strings.ToLower(value)
}

func derivePackageForFile(root, modulePath, relPath string) string {
	dir := filepath.Dir(relPath)
	if dir == "." {
		return modulePath
	}
	return modulePath + "/" + filepath.ToSlash(dir)
}

func findScanFile(scanned []ScanFile, relPath string) *ScanFile {
	for i := range scanned {
		if scanned[i].RelPath == relPath {
			return &scanned[i]
		}
	}
	return nil
}

func isWithinRoot(root, path string) bool {
	relPath, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return relPath == "." || !strings.HasPrefix(relPath, "..")
}
