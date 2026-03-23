package codebase

import "sort"

func NewResult(root, modulePath, goVersion string) *Result {
	return &Result{
		Root:            root,
		ModulePath:      modulePath,
		GoVersion:       goVersion,
		ImpactedPackage: make(map[string]struct{}),
	}
}

func MergeResult(dst, src *Result) {
	if dst == nil || src == nil {
		return
	}
	if dst.ImpactedPackage == nil {
		dst.ImpactedPackage = make(map[string]struct{})
	}
	dst.Packages = append(dst.Packages, src.Packages...)
	dst.Files = append(dst.Files, src.Files...)
	dst.Symbols = append(dst.Symbols, src.Symbols...)
	dst.Dependencies = append(dst.Dependencies, src.Dependencies...)
	dst.References = append(dst.References, src.References...)
	dst.Calls = append(dst.Calls, src.Calls...)
	dst.Tests = append(dst.Tests, src.Tests...)
	dst.TestLinks = append(dst.TestLinks, src.TestLinks...)
	for pkg := range src.ImpactedPackage {
		dst.ImpactedPackage[pkg] = struct{}{}
	}
}

func SortResult(result *Result) {
	if result == nil {
		return
	}

	sort.Slice(result.Packages, func(i, j int) bool {
		return result.Packages[i].ImportPath < result.Packages[j].ImportPath
	})
	sort.Slice(result.Files, func(i, j int) bool {
		return result.Files[i].RelPath < result.Files[j].RelPath
	})
	sort.Slice(result.Symbols, func(i, j int) bool {
		return result.Symbols[i].QName < result.Symbols[j].QName
	})
	sort.Slice(result.Dependencies, func(i, j int) bool {
		if result.Dependencies[i].FromPackageImportPath == result.Dependencies[j].FromPackageImportPath {
			return result.Dependencies[i].ToPackageImportPath < result.Dependencies[j].ToPackageImportPath
		}
		return result.Dependencies[i].FromPackageImportPath < result.Dependencies[j].FromPackageImportPath
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
}

func sortedSetKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
