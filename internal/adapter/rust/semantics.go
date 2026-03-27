package rust

import (
	"sort"
	"strconv"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/codebase"
)

type rustParsedScanFile struct {
	File   codebase.ScanFile
	Parsed rustParsedFile
}

type rustSymbolIndexes struct {
	byKey      map[string]codebase.SymbolFact
	byQName    map[string]codebase.SymbolFact
	packageSet map[string]struct{}
	crateRoots map[string]struct{}
}

type rustResolvedImport struct {
	Alias        string
	ResolvedPath string
	Symbol       codebase.SymbolFact
	PackagePath  string
	Glob         bool
}

type rustResolvedType struct {
	Symbol codebase.SymbolFact
}

type rustSemanticState struct {
	refs      []codebase.ReferenceFact
	calls     []codebase.CallFact
	testLinks []codebase.TestLinkFact
	refSeen   map[string]struct{}
	callSeen  map[string]struct{}
	linkSeen  map[string]codebase.TestLinkFact
}

func buildRustIndexes(parsed []rustParsedScanFile) rustSymbolIndexes {
	indexes := rustSymbolIndexes{
		byKey:      make(map[string]codebase.SymbolFact),
		byQName:    make(map[string]codebase.SymbolFact),
		packageSet: make(map[string]struct{}),
		crateRoots: make(map[string]struct{}),
	}

	for _, file := range parsed {
		if pkg := strings.TrimSpace(file.File.PackageImportPath); pkg != "" {
			indexes.packageSet[pkg] = struct{}{}
			indexes.crateRoots[rustCrateRoot(pkg)] = struct{}{}
		}
		for _, symbol := range file.Parsed.Symbols {
			indexes.byKey[symbol.Fact.SymbolKey] = symbol.Fact
			indexes.byQName[symbol.Fact.QName] = symbol.Fact
			if pkg := strings.TrimSpace(symbol.Fact.PackageImportPath); pkg != "" {
				indexes.packageSet[pkg] = struct{}{}
				indexes.crateRoots[rustCrateRoot(pkg)] = struct{}{}
			}
		}
	}
	return indexes
}

func addRustPackageFact(facts map[string]*codebase.PackageFact, files map[string]map[string]struct{}, pkgPath, relPath string) {
	pkgPath = strings.TrimSpace(pkgPath)
	if pkgPath == "" {
		return
	}
	pkg := facts[pkgPath]
	if pkg == nil {
		pkg = &codebase.PackageFact{
			ImportPath: pkgPath,
			Name:       rustPackageName(pkgPath),
			DirPath:    relDirPath(relPath),
		}
		facts[pkgPath] = pkg
	}
	if files[pkgPath] == nil {
		files[pkgPath] = make(map[string]struct{})
	}
	if _, ok := files[pkgPath][relPath]; ok {
		return
	}
	files[pkgPath][relPath] = struct{}{}
	pkg.FileCount++
}

func relDirPath(relPath string) string {
	dir := strings.TrimSpace(strings.ReplaceAll(relPath, "\\", "/"))
	if idx := strings.LastIndex(dir, "/"); idx >= 0 {
		return dir[:idx]
	}
	return "."
}

func rustPackageName(pkgPath string) string {
	pkgPath = strings.TrimSpace(pkgPath)
	if pkgPath == "" {
		return ""
	}
	if idx := strings.LastIndex(pkgPath, "::"); idx >= 0 {
		return pkgPath[idx+2:]
	}
	return pkgPath
}

func selectedRustPackage(patterns map[string]struct{}, pkgPath string) bool {
	if len(patterns) == 0 {
		return true
	}
	_, ok := patterns[strings.TrimSpace(pkgPath)]
	return ok
}

func buildRustDependencies(parsed []rustParsedScanFile, indexes rustSymbolIndexes, patterns map[string]struct{}) []codebase.DependencyFact {
	seen := make(map[string]codebase.DependencyFact)
	for _, file := range parsed {
		fromPkg := strings.TrimSpace(file.File.PackageImportPath)
		if fromPkg == "" || !selectedRustPackage(patterns, fromPkg) {
			continue
		}
		for _, use := range file.Parsed.Uses {
			targetPkg := rustDependencyTarget(use.ResolvedPath, indexes)
			if targetPkg == "" || targetPkg == fromPkg {
				continue
			}
			key := fromPkg + "|" + targetPkg
			seen[key] = codebase.DependencyFact{
				FromPackageImportPath: fromPkg,
				ToPackageImportPath:   targetPkg,
				IsLocal:               rustIsLocalPackage(targetPkg, indexes),
			}
		}
	}

	result := make([]codebase.DependencyFact, 0, len(seen))
	for _, dep := range seen {
		result = append(result, dep)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].FromPackageImportPath == result[j].FromPackageImportPath {
			return result[i].ToPackageImportPath < result[j].ToPackageImportPath
		}
		return result[i].FromPackageImportPath < result[j].FromPackageImportPath
	})
	return result
}

func rustDependencyTarget(resolvedPath string, indexes rustSymbolIndexes) string {
	resolvedPath = strings.TrimSpace(resolvedPath)
	if resolvedPath == "" {
		return ""
	}
	if symbol, ok := indexes.byQName[resolvedPath]; ok {
		return symbol.PackageImportPath
	}
	candidate := resolvedPath
	for candidate != "" {
		if _, ok := indexes.packageSet[candidate]; ok {
			return candidate
		}
		next := rustTrimLastSegment(candidate)
		if next == candidate {
			break
		}
		candidate = next
	}
	if before, _, ok := strings.Cut(resolvedPath, "::"); ok {
		return before
	}
	return resolvedPath
}

func rustTrimLastSegment(value string) string {
	if idx := strings.LastIndex(value, "::"); idx >= 0 {
		return value[:idx]
	}
	return ""
}

func rustIsLocalPackage(pkgPath string, indexes rustSymbolIndexes) bool {
	if _, ok := indexes.packageSet[pkgPath]; ok {
		return true
	}
	root := rustCrateRoot(pkgPath)
	_, ok := indexes.crateRoots[root]
	return ok
}

func buildRustRelationships(parsed []rustParsedScanFile, indexes rustSymbolIndexes, patterns map[string]struct{}) ([]codebase.ReferenceFact, []codebase.CallFact, []codebase.TestLinkFact) {
	s := rustSemanticState{
		refs:      make([]codebase.ReferenceFact, 0, 16),
		calls:     make([]codebase.CallFact, 0, 16),
		testLinks: make([]codebase.TestLinkFact, 0, 8),
		refSeen:   make(map[string]struct{}),
		callSeen:  make(map[string]struct{}),
		linkSeen:  make(map[string]codebase.TestLinkFact),
	}

	for _, file := range parsed {
		for _, body := range file.Parsed.Bodies {
			if !selectedRustPackage(patterns, body.Owner.PackageImportPath) {
				continue
			}
			imports := resolveRustImports(file.Parsed.Uses, body, indexes)
			analyzeRustBody(body, imports, indexes, &s)
		}
	}

	for _, link := range s.linkSeen {
		s.testLinks = append(s.testLinks, link)
	}
	sort.Slice(s.testLinks, func(i, j int) bool {
		if s.testLinks[i].TestKey == s.testLinks[j].TestKey {
			return s.testLinks[i].SymbolKey < s.testLinks[j].SymbolKey
		}
		return s.testLinks[i].TestKey < s.testLinks[j].TestKey
	})
	return s.refs, s.calls, s.testLinks
}

func analyzeRustBody(body rustParsedBody, imports []rustResolvedImport, indexes rustSymbolIndexes, s *rustSemanticState) {
	varTypes := make(map[string]rustResolvedType)
	if body.Owner.Receiver != "" {
		if symbol, ok := indexes.byQName[joinRustPath(body.ScopePath, body.Owner.Receiver)]; ok {
			varTypes["self"] = rustResolvedType{Symbol: symbol}
		}
	}

	signature := rustBodySignature(body.SourceLines)
	rustCollectSignatureTypes(body, signature, imports, indexes, varTypes, s)

	bodyStarted := false
	for offset, rawLine := range body.SourceLines {
		if !bodyStarted {
			braceIdx := strings.Index(rawLine, "{")
			if braceIdx < 0 {
				continue
			}
			rawLine = rawLine[braceIdx+1:]
			bodyStarted = true
		}
		lineNo := body.StartLine + offset
		line := sanitizeRustLine(rawLine)
		if strings.TrimSpace(line) == "" {
			continue
		}

		rustInferLetTypes(body, line, lineNo, imports, indexes, varTypes, s)
		rustCollectStructRefs(body, line, lineNo, imports, indexes, s)
		rustCollectAssociatedCalls(body, line, lineNo, imports, indexes, s)
		rustCollectMethodCalls(body, line, lineNo, imports, indexes, varTypes, s)
		rustCollectFunctionCalls(body, line, lineNo, imports, indexes, s)
	}
}

func resolveRustImports(uses []rustParsedUse, body rustParsedBody, indexes rustSymbolIndexes) []rustResolvedImport {
	result := make([]rustResolvedImport, 0, len(uses))
	for _, use := range uses {
		if use.ScopePath != body.ScopePath {
			continue
		}
		if use.OwnerSymbolKey != "" && use.OwnerSymbolKey != body.Owner.SymbolKey {
			continue
		}

		resolved := rustResolveExpressionPath(body.ScopePath, use.ResolvedPath, indexes)
		importItem := rustResolvedImport{
			Alias:        use.Alias,
			ResolvedPath: resolved,
			Glob:         use.Glob,
			PackagePath:  rustDependencyTarget(resolved, indexes),
		}
		if symbol, ok := indexes.byQName[resolved]; ok {
			importItem.Symbol = symbol
		}
		result = append(result, importItem)
	}
	return result
}

func rustResolveExpressionPath(scopePath, rawPath string, indexes rustSymbolIndexes) string {
	rawPath = resolveRustQualifiedPath(scopePath, rawPath)
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return ""
	}
	if _, ok := indexes.byQName[rawPath]; ok {
		return rawPath
	}
	if _, ok := indexes.packageSet[rawPath]; ok {
		return rawPath
	}
	candidates := []string{
		joinRustPath(scopePath, rawPath),
		joinRustPath(rustCrateRoot(scopePath), rawPath),
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, ok := indexes.byQName[candidate]; ok {
			return candidate
		}
		if _, ok := indexes.packageSet[candidate]; ok {
			return candidate
		}
	}
	return rawPath
}

func rustBodySignature(lines []string) string {
	var parts []string
	for _, line := range lines {
		value := sanitizeRustLine(line)
		parts = append(parts, strings.TrimSpace(value))
		if strings.Contains(value, "{") {
			break
		}
	}
	return strings.Join(parts, " ")
}

func rustCollectSignatureTypes(body rustParsedBody, signature string, imports []rustResolvedImport, indexes rustSymbolIndexes, varTypes map[string]rustResolvedType, s *rustSemanticState) {
	start := strings.Index(signature, "(")
	end := strings.LastIndex(signature, ")")
	if start >= 0 && end > start {
		params := splitRustTopLevel(signature[start+1:end], ',')
		for _, param := range params {
			param = strings.TrimSpace(param)
			if param == "" || param == "&self" || param == "self" || strings.HasSuffix(param, " self") {
				continue
			}
			matches := rustParamTypeRE.FindStringSubmatch(param)
			if len(matches) != 3 {
				continue
			}
			name := strings.TrimSpace(matches[1])
			typeExpr := strings.TrimSpace(matches[2])
			if resolved, ok := rustResolveTypeExpr(body.ScopePath, typeExpr, imports, indexes); ok {
				varTypes[name] = resolved
				rustAddReference(body, resolved.Symbol, body.StartLine, max(strings.Index(signature, name)+1, 1), "type_ref", s)
			}
		}
	}

	if matches := rustReturnTypeRE.FindStringSubmatch(signature); len(matches) == 2 {
		if resolved, ok := rustResolveTypeExpr(body.ScopePath, strings.TrimSpace(matches[1]), imports, indexes); ok {
			rustAddReference(body, resolved.Symbol, body.StartLine, max(strings.Index(signature, "->")+1, 1), "type_ref", s)
		}
	}
}

func rustInferLetTypes(body rustParsedBody, line string, lineNo int, imports []rustResolvedImport, indexes rustSymbolIndexes, varTypes map[string]rustResolvedType, s *rustSemanticState) {
	if matches := rustTypedLetRE.FindStringSubmatch(line); len(matches) == 3 {
		name := strings.TrimSpace(matches[1])
		typeExpr := strings.TrimSpace(matches[2])
		if resolved, ok := rustResolveTypeExpr(body.ScopePath, typeExpr, imports, indexes); ok {
			varTypes[name] = resolved
			rustAddReference(body, resolved.Symbol, lineNo, max(strings.Index(line, typeExpr)+1, 1), "type_ref", s)
		}
	}
	if matches := rustAssignedLetRE.FindStringSubmatch(line); len(matches) == 3 {
		name := strings.TrimSpace(matches[1])
		expr := strings.TrimSpace(matches[2])
		if resolved, ok := rustResolveConstructedType(body.ScopePath, expr, imports, indexes); ok {
			varTypes[name] = resolved
		}
	}
}

func rustCollectStructRefs(body rustParsedBody, line string, lineNo int, imports []rustResolvedImport, indexes rustSymbolIndexes, s *rustSemanticState) {
	matches := rustStructLiteralRE.FindAllStringSubmatchIndex(line, -1)
	for _, match := range matches {
		if len(match) < 4 {
			continue
		}
		typeExpr := strings.TrimSpace(line[match[2]:match[3]])
		if resolved, ok := rustResolveTypeExpr(body.ScopePath, typeExpr, imports, indexes); ok {
			rustAddReference(body, resolved.Symbol, lineNo, match[2]+1, "type_ref", s)
		}
	}
}

func rustCollectAssociatedCalls(body rustParsedBody, line string, lineNo int, imports []rustResolvedImport, indexes rustSymbolIndexes, s *rustSemanticState) {
	matches := rustAssocCallRE.FindAllStringSubmatchIndex(line, -1)
	for _, match := range matches {
		if len(match) < 6 {
			continue
		}
		left := strings.TrimSpace(line[match[2]:match[3]])
		name := strings.TrimSpace(line[match[4]:match[5]])
		if target, ok := rustResolveAssociatedTarget(body.ScopePath, left, name, imports, indexes); ok {
			rustAddCall(body, target, lineNo, match[4]+1, s)
		}
	}
}

func rustCollectMethodCalls(body rustParsedBody, line string, lineNo int, imports []rustResolvedImport, indexes rustSymbolIndexes, varTypes map[string]rustResolvedType, s *rustSemanticState) {
	matches := rustMethodCallRE.FindAllStringSubmatchIndex(line, -1)
	for _, match := range matches {
		if len(match) < 6 {
			continue
		}
		recvExpr := strings.TrimSpace(line[match[2]:match[3]])
		name := strings.TrimSpace(line[match[4]:match[5]])
		target, ok := rustResolveMethodTarget(body, recvExpr, name, varTypes, indexes)
		if !ok {
			continue
		}
		rustAddCall(body, target, lineNo, match[4]+1, s)
	}
}

func rustCollectFunctionCalls(body rustParsedBody, line string, lineNo int, imports []rustResolvedImport, indexes rustSymbolIndexes, s *rustSemanticState) {
	matches := rustFuncCallRE.FindAllStringSubmatchIndex(line, -1)
	for _, match := range matches {
		if len(match) < 4 {
			continue
		}
		start := match[2]
		if start > 0 {
			prev := line[start-1]
			if prev == '.' || prev == ':' || prev == '!' {
				continue
			}
		}
		name := strings.TrimSpace(line[match[2]:match[3]])
		if _, skip := rustKeywordsCallSkip[name]; skip {
			continue
		}
		target, ok := rustResolveFunctionTarget(body.ScopePath, name, imports, indexes)
		if !ok {
			continue
		}
		rustAddCall(body, target, lineNo, match[2]+1, s)
	}
}

func rustResolveAssociatedTarget(scopePath, left, name string, imports []rustResolvedImport, indexes rustSymbolIndexes) (codebase.SymbolFact, bool) {
	if left == "" || name == "" {
		return codebase.SymbolFact{}, false
	}
	if resolved, ok := rustResolveTypeExpr(scopePath, left, imports, indexes); ok {
		if symbol, ok := indexes.byQName[joinRustPath(resolved.Symbol.QName, name)]; ok && symbol.Kind == "method" {
			return symbol, true
		}
	}

	leftPath := rustResolveFunctionPath(scopePath, left, imports, indexes)
	if leftPath == "" {
		return codebase.SymbolFact{}, false
	}
	if symbol, ok := indexes.byQName[joinRustPath(leftPath, name)]; ok && symbol.Kind != "method" {
		return symbol, true
	}
	return codebase.SymbolFact{}, false
}

func rustResolveMethodTarget(body rustParsedBody, recvExpr, name string, varTypes map[string]rustResolvedType, indexes rustSymbolIndexes) (codebase.SymbolFact, bool) {
	recvExpr = strings.TrimSpace(recvExpr)
	if recvExpr == "" || strings.Contains(recvExpr, ".") {
		if recvExpr != "self" {
			return codebase.SymbolFact{}, false
		}
	}

	var typeSymbol codebase.SymbolFact
	switch {
	case recvExpr == "self" && body.Owner.Receiver != "":
		typeSymbol = indexes.byQName[joinRustPath(body.ScopePath, body.Owner.Receiver)]
	case !strings.Contains(recvExpr, "."):
		typeSymbol = varTypes[recvExpr].Symbol
	}
	if typeSymbol.SymbolKey == "" {
		return codebase.SymbolFact{}, false
	}
	symbol, ok := indexes.byQName[joinRustPath(typeSymbol.QName, name)]
	if !ok || symbol.Kind != "method" {
		return codebase.SymbolFact{}, false
	}
	return symbol, true
}

func rustResolveFunctionTarget(scopePath, name string, imports []rustResolvedImport, indexes rustSymbolIndexes) (codebase.SymbolFact, bool) {
	if name == "" {
		return codebase.SymbolFact{}, false
	}
	for _, item := range imports {
		if item.Glob {
			if symbol, ok := indexes.byQName[joinRustPath(item.ResolvedPath, name)]; ok && symbol.Kind != "method" {
				return symbol, true
			}
			continue
		}
		if item.Alias == name && item.Symbol.SymbolKey != "" && item.Symbol.Kind != "method" {
			return item.Symbol, true
		}
		if item.Alias == name && item.PackagePath != "" {
			if symbol, ok := indexes.byQName[joinRustPath(item.ResolvedPath, name)]; ok && symbol.Kind != "method" {
				return symbol, true
			}
		}
	}
	if symbol, ok := indexes.byQName[joinRustPath(scopePath, name)]; ok && symbol.Kind != "method" {
		return symbol, true
	}
	return codebase.SymbolFact{}, false
}

func rustResolveFunctionPath(scopePath, rawPath string, imports []rustResolvedImport, indexes rustSymbolIndexes) string {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return ""
	}
	if strings.Contains(rawPath, "::") {
		return rustResolveExpressionPath(scopePath, rawPath, indexes)
	}
	for _, item := range imports {
		if item.Glob {
			if _, ok := indexes.packageSet[joinRustPath(item.ResolvedPath, rawPath)]; ok {
				return joinRustPath(item.ResolvedPath, rawPath)
			}
			continue
		}
		if item.Alias == rawPath {
			if item.PackagePath != "" {
				return item.ResolvedPath
			}
			if item.Symbol.SymbolKey != "" {
				return item.Symbol.QName
			}
		}
	}
	return rustResolveExpressionPath(scopePath, rawPath, indexes)
}

func rustResolveTypeExpr(scopePath, typeExpr string, imports []rustResolvedImport, indexes rustSymbolIndexes) (rustResolvedType, bool) {
	typeExpr = rustBaseType(typeExpr)
	if typeExpr == "" {
		return rustResolvedType{}, false
	}
	for _, item := range imports {
		if item.Glob {
			if symbol, ok := indexes.byQName[joinRustPath(item.ResolvedPath, typeExpr)]; ok && rustIsTypeKind(symbol.Kind) {
				return rustResolvedType{Symbol: symbol}, true
			}
			continue
		}
		if item.Alias == typeExpr && item.Symbol.SymbolKey != "" && rustIsTypeKind(item.Symbol.Kind) {
			return rustResolvedType{Symbol: item.Symbol}, true
		}
	}

	resolvedPath := typeExpr
	if strings.Contains(typeExpr, "::") {
		resolvedPath = rustResolveExpressionPath(scopePath, typeExpr, indexes)
	}
	if symbol, ok := indexes.byQName[resolvedPath]; ok && rustIsTypeKind(symbol.Kind) {
		return rustResolvedType{Symbol: symbol}, true
	}
	if symbol, ok := indexes.byQName[joinRustPath(scopePath, typeExpr)]; ok && rustIsTypeKind(symbol.Kind) {
		return rustResolvedType{Symbol: symbol}, true
	}
	return rustResolvedType{}, false
}

func rustResolveConstructedType(scopePath, expr string, imports []rustResolvedImport, indexes rustSymbolIndexes) (rustResolvedType, bool) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return rustResolvedType{}, false
	}
	if before, _, ok := strings.Cut(expr, "::"); ok {
		left := strings.TrimSpace(before)
		if resolved, ok := rustResolveTypeExpr(scopePath, left, imports, indexes); ok {
			return resolved, true
		}
	}
	if before, _, ok := strings.Cut(expr, "{"); ok {
		if resolved, ok := rustResolveTypeExpr(scopePath, before, imports, indexes); ok {
			return resolved, true
		}
	}
	if before, _, ok := strings.Cut(expr, "("); ok {
		candidate := strings.TrimSpace(before)
		if resolved, ok := rustResolveTypeExpr(scopePath, candidate, imports, indexes); ok {
			return resolved, true
		}
	}
	return rustResolvedType{}, false
}

func rustBaseType(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "&")
	value = strings.TrimPrefix(value, "mut ")
	value = strings.TrimPrefix(value, "'_ ")
	value = strings.TrimSpace(value)
	for strings.HasPrefix(value, "&") {
		value = strings.TrimSpace(strings.TrimPrefix(value, "&"))
	}
	if idx := strings.Index(value, "<"); idx >= 0 {
		value = value[:idx]
	}
	value = strings.TrimSuffix(value, ",")
	value = strings.Trim(value, "[]() ")
	if !rustPathTypeRE.MatchString(value) {
		return ""
	}
	return value
}

func rustIsTypeKind(kind string) bool {
	switch kind {
	case "struct", "type", "interface", "alias":
		return true
	default:
		return false
	}
}

func rustAddReference(body rustParsedBody, target codebase.SymbolFact, line, column int, kind string, s *rustSemanticState) {
	if target.SymbolKey == "" {
		return
	}
	ref := codebase.ReferenceFact{
		FromPackageImportPath: body.Owner.PackageImportPath,
		FromSymbolKey:         body.Owner.SymbolKey,
		ToSymbolKey:           target.SymbolKey,
		FilePath:              body.Owner.FilePath,
		Line:                  line,
		Column:                column,
		Kind:                  kind,
	}
	key := ref.FromPackageImportPath + "|" + ref.FilePath + "|" + ref.FromSymbolKey + "|" + ref.ToSymbolKey + "|" + kind + "|" + strconv.Itoa(line) + "|" + strconv.Itoa(column)
	if _, ok := s.refSeen[key]; ok {
		return
	}
	s.refSeen[key] = struct{}{}
	s.refs = append(s.refs, ref)
	rustAddTestLink(body, target, kind, false, s)
}

func rustAddCall(body rustParsedBody, target codebase.SymbolFact, line, column int, s *rustSemanticState) {
	if target.SymbolKey == "" {
		return
	}
	call := codebase.CallFact{
		CallerPackageImportPath: body.Owner.PackageImportPath,
		CallerSymbolKey:         body.Owner.SymbolKey,
		CalleeSymbolKey:         target.SymbolKey,
		FilePath:                body.Owner.FilePath,
		Line:                    line,
		Column:                  column,
		Dispatch:                "static",
	}
	key := call.CallerPackageImportPath + "|" + call.FilePath + "|" + call.CallerSymbolKey + "|" + call.CalleeSymbolKey + "|" + strconv.Itoa(line) + "|" + strconv.Itoa(column)
	if _, ok := s.callSeen[key]; ok {
		return
	}
	s.callSeen[key] = struct{}{}
	s.calls = append(s.calls, call)
	rustAddReference(body, target, line, column, "value_ref", s)
	rustAddTestLink(body, target, "value_ref", true, s)
}

func rustAddTestLink(body rustParsedBody, target codebase.SymbolFact, refKind string, direct bool, s *rustSemanticState) {
	if body.DeclaredTestKey == "" || target.SymbolKey == "" {
		return
	}
	linkKind := "name_match"
	confidence := "medium"
	if target.Kind == "method" {
		linkKind = "receiver_match"
	}
	if direct {
		confidence = "high"
	}
	if refKind == "type_ref" && confidence == "high" {
		confidence = "medium"
	}
	key := body.DeclaredTestKey + "|" + target.SymbolKey + "|" + linkKind
	current, ok := s.linkSeen[key]
	if ok && rustConfidenceRank(current.Confidence) >= rustConfidenceRank(confidence) {
		return
	}
	s.linkSeen[key] = codebase.TestLinkFact{
		TestPackageImportPath: body.Owner.PackageImportPath,
		TestKey:               body.DeclaredTestKey,
		SymbolKey:             target.SymbolKey,
		LinkKind:              linkKind,
		Confidence:            confidence,
	}
}

func rustConfidenceRank(value string) int {
	switch value {
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

func sortRustSemantics(refs []codebase.ReferenceFact, calls []codebase.CallFact) {
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].ToSymbolKey == refs[j].ToSymbolKey {
			if refs[i].FilePath == refs[j].FilePath {
				if refs[i].Line == refs[j].Line {
					return refs[i].Column < refs[j].Column
				}
				return refs[i].Line < refs[j].Line
			}
			return refs[i].FilePath < refs[j].FilePath
		}
		return refs[i].ToSymbolKey < refs[j].ToSymbolKey
	})
	sort.Slice(calls, func(i, j int) bool {
		if calls[i].CallerSymbolKey == calls[j].CallerSymbolKey {
			if calls[i].CalleeSymbolKey == calls[j].CalleeSymbolKey {
				if calls[i].Line == calls[j].Line {
					return calls[i].Column < calls[j].Column
				}
				return calls[i].Line < calls[j].Line
			}
			return calls[i].CalleeSymbolKey < calls[j].CalleeSymbolKey
		}
		return calls[i].CallerSymbolKey < calls[j].CallerSymbolKey
	})
}

func normalizeRustName(value string) string {
	value = strings.ReplaceAll(value, "_", "")
	value = strings.ReplaceAll(value, "-", "")
	value = strings.TrimPrefix(value, "test")
	return strings.ToLower(value)
}
