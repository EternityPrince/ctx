package golang

import (
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/vladimirkasterin/ctx/internal/codebase"
)

func dependencyFacts(pkg *packages.Package, modulePath string) []codebase.DependencyFact {
	deps := make([]codebase.DependencyFact, 0, len(pkg.Imports))
	for _, dep := range pkg.Imports {
		if dep == nil || dep.PkgPath == "" {
			continue
		}
		deps = append(deps, codebase.DependencyFact{
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

func isWithinRoot(root, path string) bool {
	relPath, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return relPath == "." || !strings.HasPrefix(relPath, "..")
}
