package rust

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/codebase"
)

var (
	rustFnPattern        = regexp.MustCompile(`\bfn\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	rustStructRE         = regexp.MustCompile(`\bstruct\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	rustEnumRE           = regexp.MustCompile(`\benum\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	rustTraitRE          = regexp.MustCompile(`\btrait\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	rustTypeAliasRE      = regexp.MustCompile(`\btype\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	rustImplRE           = regexp.MustCompile(`^\s*impl(?:<[^>]+>)?\s+(.+?)\s*\{`)
	rustUseStartRE       = regexp.MustCompile(`^(?:pub(?:\([^)]*\))?\s+)?use\b`)
	rustVisibilityRE     = regexp.MustCompile(`^pub(?:\([^)]*\))?\s+`)
	rustModDeclRE        = regexp.MustCompile(`^(?:pub(?:\([^)]*\))?\s+)?mod\s+([A-Za-z_][A-Za-z0-9_]*)\s*([;{])`)
	rustLeafAliasRE      = regexp.MustCompile(`^(.+?)\s+as\s+([A-Za-z_][A-Za-z0-9_]*)$`)
	rustPathTypeRE       = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_:<>]*$`)
	rustTypedLetRE       = regexp.MustCompile(`\blet\s+(?:mut\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*:\s*([^=;]+)`)
	rustAssignedLetRE    = regexp.MustCompile(`\blet\s+(?:mut\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.+?);?$`)
	rustStructLiteralRE  = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_:]*)\s*\{`)
	rustAssocCallRE      = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_:]*)::([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	rustMethodCallRE     = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_\.]*)\.([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	rustFuncCallRE       = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_:]*)\s*\(`)
	rustParamTypeRE      = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\s*:\s*(.+)$`)
	rustReturnTypeRE     = regexp.MustCompile(`\)\s*->\s*([^{]+)`)
	rustKeywordsCallSkip = map[string]struct{}{
		"assert": {}, "assert_eq": {}, "assert_ne": {}, "dbg": {}, "if": {}, "loop": {},
		"match": {}, "panic": {}, "println": {}, "return": {}, "self": {}, "super": {},
		"while": {}, "Some": {}, "None": {}, "Ok": {}, "Err": {},
	}
)

type rustParsedFile struct {
	Symbols []rustParsedSymbol
	Tests   []codebase.TestFact
	Uses    []rustParsedUse
	Bodies  []rustParsedBody
}

type rustParsedSymbol struct {
	Fact      codebase.SymbolFact
	ScopePath string
	EndLine   int
}

type rustParsedUse struct {
	ScopePath      string
	OwnerSymbolKey string
	Alias          string
	ResolvedPath   string
	Glob           bool
	Line           int
	Column         int
}

type rustParsedBody struct {
	Owner            codebase.SymbolFact
	ScopePath        string
	StartLine        int
	EndLine          int
	SourceLines      []string
	DeclaredTestKey  string
	DeclaredTestName string
}

type rustImplBlock struct {
	Receiver string
	Depth    int
}

type rustModuleBlock struct {
	Name  string
	Depth int
}

type rustBodyBlock struct {
	SymbolKey string
	Depth     int
}

type rustUseBinding struct {
	Path  string
	Alias string
	Glob  bool
}

func LocateSymbolBlock(data []byte, name, kind, receiver string, line int) (int, int, error) {
	parsed := parseRustFile(string(data), "", "")
	for _, symbol := range parsed.Symbols {
		if symbol.Fact.Name != name || symbol.Fact.Kind != kind || symbol.Fact.Line != line {
			continue
		}
		if strings.TrimSpace(receiver) != "" && symbol.Fact.Receiver != strings.TrimSpace(receiver) {
			continue
		}
		return symbol.Fact.Line, symbol.EndLine, nil
	}
	return 0, 0, fmt.Errorf("rust symbol block not found")
}

func parseRustFile(content, relPath, pkgPath string) rustParsedFile {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	parsed := rustParsedFile{
		Symbols: make([]rustParsedSymbol, 0, 16),
		Tests:   make([]codebase.TestFact, 0, 4),
		Uses:    make([]rustParsedUse, 0, 8),
		Bodies:  make([]rustParsedBody, 0, 8),
	}

	var docLines []string
	var attrs []string
	currentDepth := 0
	moduleStack := make([]rustModuleBlock, 0, 4)
	implStack := make([]rustImplBlock, 0, 2)
	bodyStack := make([]rustBodyBlock, 0, 2)

	for idx := 0; idx < len(lines); idx++ {
		for len(moduleStack) > 0 && currentDepth < moduleStack[len(moduleStack)-1].Depth {
			moduleStack = moduleStack[:len(moduleStack)-1]
		}
		for len(implStack) > 0 && currentDepth < implStack[len(implStack)-1].Depth {
			implStack = implStack[:len(implStack)-1]
		}
		for len(bodyStack) > 0 && currentDepth < bodyStack[len(bodyStack)-1].Depth {
			bodyStack = bodyStack[:len(bodyStack)-1]
		}

		rawLine := lines[idx]
		line := strings.TrimSpace(stripRustLineComment(rawLine))
		if line == "" {
			if len(attrs) == 0 {
				docLines = nil
			}
			currentDepth += rustBraceDelta(rawLine)
			continue
		}

		switch {
		case strings.HasPrefix(line, "///") || strings.HasPrefix(line, "//!"):
			docLines = append(docLines, strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "///"), "//!")))
			continue
		case strings.HasPrefix(line, "#["):
			attrs = append(attrs, line)
			continue
		}

		scopePath := rustScopePath(pkgPath, moduleStack)
		ownerKey := ""
		if len(bodyStack) > 0 {
			ownerKey = bodyStack[len(bodyStack)-1].SymbolKey
		}

		if rustUseStartRE.MatchString(line) {
			statement, endIdx := consumeRustStatement(lines, idx)
			parsed.Uses = append(parsed.Uses, extractRustUses(statement, scopePath, ownerKey, idx+1)...)
			idx = endIdx
			docLines = nil
			attrs = nil
			continue
		}

		if ownerKey != "" {
			docLines = nil
			attrs = nil
			currentDepth += rustBraceDelta(rawLine)
			continue
		}

		if name, inline, ok := rustModuleDeclaration(line); ok {
			modulePath := joinRustPath(scopePath, name)
			parsed.Uses = append(parsed.Uses, rustParsedUse{
				ScopePath:    scopePath,
				Alias:        name,
				ResolvedPath: modulePath,
				Line:         idx + 1,
				Column:       max(strings.Index(rawLine, name)+1, 1),
			})
			if inline {
				delta := rustBraceDelta(rawLine)
				if delta > 0 {
					moduleStack = append(moduleStack, rustModuleBlock{
						Name:  name,
						Depth: currentDepth + delta,
					})
				}
			}
		}

		if receiver, ok := rustImplReceiver(line); ok {
			delta := rustBraceDelta(rawLine)
			if delta > 0 {
				implStack = append(implStack, rustImplBlock{
					Receiver: receiver,
					Depth:    currentDepth + delta,
				})
			}
		}

		isPublic := strings.Contains(line, "pub ")
		activeReceiver := ""
		if len(implStack) > 0 {
			activeReceiver = implStack[len(implStack)-1].Receiver
		}

		if matches := rustFnPattern.FindStringSubmatch(line); len(matches) == 2 {
			name := matches[1]
			lineNo := idx + 1
			column := max(strings.Index(rawLine, name)+1, 1)
			endLine := findRustItemEnd(lines, idx)
			kind := "func"
			receiver := ""
			symbolQName := joinRustPath(scopePath, name)
			if activeReceiver != "" {
				kind = "method"
				receiver = activeReceiver
				symbolQName = rustSymbolKey(scopePath, receiver, name)
			}
			fact := codebase.SymbolFact{
				SymbolKey:         symbolQName,
				QName:             symbolQName,
				PackageImportPath: pkgPath,
				FilePath:          relPath,
				Name:              name,
				Kind:              kind,
				Receiver:          receiver,
				Signature:         strings.TrimSpace(rawLine),
				Doc:               strings.Join(docLines, "\n"),
				Line:              lineNo,
				Column:            column,
				Exported:          isPublic,
				IsTest:            rustHasTestAttribute(attrs),
			}
			parsed.Symbols = append(parsed.Symbols, rustParsedSymbol{
				Fact:      fact,
				ScopePath: scopePath,
				EndLine:   endLine,
			})

			testName := rustScopedDisplayName(pkgPath, scopePath, name)
			testKey := ""
			if rustHasTestAttribute(attrs) && pkgPath != "" {
				testKey = fmt.Sprintf("%s:%s:%d", pkgPath, testName, lineNo)
				parsed.Tests = append(parsed.Tests, codebase.TestFact{
					TestKey:           testKey,
					PackageImportPath: pkgPath,
					FilePath:          relPath,
					Name:              testName,
					Kind:              "test",
					Line:              lineNo,
				})
			}
			sourceLines := append([]string(nil), lines[idx:endLine]...)
			parsed.Bodies = append(parsed.Bodies, rustParsedBody{
				Owner:            fact,
				ScopePath:        scopePath,
				StartLine:        lineNo,
				EndLine:          endLine,
				SourceLines:      sourceLines,
				DeclaredTestKey:  testKey,
				DeclaredTestName: testName,
			})

			delta := rustBraceDelta(rawLine)
			if delta > 0 {
				bodyStack = append(bodyStack, rustBodyBlock{
					SymbolKey: fact.SymbolKey,
					Depth:     currentDepth + delta,
				})
			}
			docLines = nil
			attrs = nil
			currentDepth += delta
			continue
		}

		if symbol, ok := rustTypeSymbol(line, relPath, pkgPath, scopePath, rawLine, idx+1, isPublic, docLines, lines, idx); ok {
			parsed.Symbols = append(parsed.Symbols, symbol)
			docLines = nil
			attrs = nil
			currentDepth += rustBraceDelta(rawLine)
			continue
		}

		docLines = nil
		attrs = nil
		currentDepth += rustBraceDelta(rawLine)
	}

	return parsed
}

func rustTypeSymbol(line, relPath, pkgPath, scopePath, rawLine string, lineNo int, exported bool, docLines, lines []string, idx int) (rustParsedSymbol, bool) {
	type candidate struct {
		re   *regexp.Regexp
		kind string
	}

	for _, item := range []candidate{
		{re: rustStructRE, kind: "struct"},
		{re: rustEnumRE, kind: "type"},
		{re: rustTraitRE, kind: "interface"},
		{re: rustTypeAliasRE, kind: "alias"},
	} {
		matches := item.re.FindStringSubmatch(line)
		if len(matches) != 2 {
			continue
		}
		name := matches[1]
		column := max(strings.Index(rawLine, name)+1, 1)
		qname := joinRustPath(scopePath, name)
		return rustParsedSymbol{
			Fact: codebase.SymbolFact{
				SymbolKey:         qname,
				QName:             qname,
				PackageImportPath: pkgPath,
				FilePath:          relPath,
				Name:              name,
				Kind:              item.kind,
				Signature:         strings.TrimSpace(rawLine),
				Doc:               strings.Join(docLines, "\n"),
				Line:              lineNo,
				Column:            column,
				Exported:          exported,
			},
			ScopePath: scopePath,
			EndLine:   findRustItemEnd(lines, idx),
		}, true
	}
	return rustParsedSymbol{}, false
}

func rustSymbolKey(scopePath, receiver, name string) string {
	scopePath = strings.TrimSpace(scopePath)
	name = strings.TrimSpace(name)
	receiver = strings.TrimSpace(receiver)
	switch {
	case scopePath == "" && receiver == "":
		return name
	case receiver == "":
		return joinRustPath(scopePath, name)
	default:
		return joinRustPath(scopePath, receiver, name)
	}
}

func rustScopePath(pkgPath string, modules []rustModuleBlock) string {
	parts := make([]string, 0, len(modules)+1)
	if value := strings.TrimSpace(pkgPath); value != "" {
		parts = append(parts, value)
	}
	for _, module := range modules {
		parts = append(parts, module.Name)
	}
	return joinRustPath(parts...)
}

func rustScopedDisplayName(pkgPath, scopePath, name string) string {
	scopePath = strings.TrimSpace(scopePath)
	pkgPath = strings.TrimSpace(pkgPath)
	if scopePath == "" || scopePath == pkgPath {
		return name
	}
	relative := strings.TrimPrefix(scopePath, pkgPath)
	relative = strings.TrimPrefix(relative, "::")
	if relative == "" {
		return name
	}
	return relative + "::" + name
}

func rustModuleDeclaration(line string) (string, bool, bool) {
	matches := rustModDeclRE.FindStringSubmatch(line)
	if len(matches) != 3 {
		return "", false, false
	}
	return matches[1], matches[2] == "{", true
}

func consumeRustStatement(lines []string, start int) (string, int) {
	parts := make([]string, 0, 4)
	for idx := start; idx < len(lines); idx++ {
		line := strings.TrimSpace(stripRustLineComment(lines[idx]))
		if line != "" {
			parts = append(parts, line)
		}
		if strings.Contains(line, ";") {
			return strings.Join(parts, " "), idx
		}
	}
	return strings.Join(parts, " "), len(lines) - 1
}

func extractRustUses(statement, scopePath, ownerSymbolKey string, lineNo int) []rustParsedUse {
	statement = strings.TrimSpace(statement)
	statement = strings.TrimSuffix(statement, ";")
	statement = rustVisibilityRE.ReplaceAllString(statement, "")
	statement = strings.TrimSpace(strings.TrimPrefix(statement, "use"))
	if statement == "" {
		return nil
	}

	bindings := expandRustUseSpec(statement)
	result := make([]rustParsedUse, 0, len(bindings))
	for _, binding := range bindings {
		resolved := resolveRustQualifiedPath(scopePath, binding.Path)
		alias := strings.TrimSpace(binding.Alias)
		if alias == "" && !binding.Glob {
			alias = rustLastSegment(resolved)
		}
		result = append(result, rustParsedUse{
			ScopePath:      scopePath,
			OwnerSymbolKey: ownerSymbolKey,
			Alias:          alias,
			ResolvedPath:   resolved,
			Glob:           binding.Glob,
			Line:           lineNo,
			Column:         1,
		})
	}
	return result
}

func expandRustUseSpec(spec string) []rustUseBinding {
	items := splitRustTopLevel(spec, ',')
	result := make([]rustUseBinding, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		braceIdx := topLevelBraceIndex(item)
		if braceIdx >= 0 && strings.HasSuffix(item, "}") {
			prefix := strings.TrimSpace(item[:braceIdx])
			prefix = strings.TrimSuffix(prefix, "::")
			inner := item[braceIdx+1 : len(item)-1]
			children := expandRustUseSpec(inner)
			for _, child := range children {
				if child.Path == "self" {
					child.Path = prefix
				} else {
					child.Path = joinRustPath(prefix, child.Path)
				}
				result = append(result, child)
			}
			continue
		}

		alias := ""
		path := item
		if matches := rustLeafAliasRE.FindStringSubmatch(item); len(matches) == 3 {
			path = strings.TrimSpace(matches[1])
			alias = strings.TrimSpace(matches[2])
		}
		switch path {
		case "*":
			result = append(result, rustUseBinding{Path: "", Alias: alias, Glob: true})
		case "self":
			result = append(result, rustUseBinding{Path: "self", Alias: alias})
		default:
			if strings.HasSuffix(path, "::*") {
				result = append(result, rustUseBinding{
					Path: strings.TrimSuffix(path, "::*"),
					Glob: true,
				})
				continue
			}
			result = append(result, rustUseBinding{
				Path:  path,
				Alias: alias,
			})
		}
	}
	return result
}

func splitRustTopLevel(value string, sep rune) []string {
	var result []string
	start := 0
	depthBrace := 0
	depthAngle := 0
	for idx, r := range value {
		switch r {
		case '{':
			depthBrace++
		case '}':
			if depthBrace > 0 {
				depthBrace--
			}
		case '<':
			depthAngle++
		case '>':
			if depthAngle > 0 {
				depthAngle--
			}
		case sep:
			if depthBrace == 0 && depthAngle == 0 {
				result = append(result, value[start:idx])
				start = idx + 1
			}
		}
	}
	result = append(result, value[start:])
	return result
}

func topLevelBraceIndex(value string) int {
	depthAngle := 0
	for idx, r := range value {
		switch r {
		case '<':
			depthAngle++
		case '>':
			if depthAngle > 0 {
				depthAngle--
			}
		case '{':
			if depthAngle == 0 {
				return idx
			}
		}
	}
	return -1
}

func resolveRustQualifiedPath(scopePath, rawPath string) string {
	scopePath = strings.TrimSpace(scopePath)
	rawPath = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rawPath), "::"))
	if rawPath == "" {
		return ""
	}
	switch rawPath {
	case "self":
		return scopePath
	case "crate":
		return rustCrateRoot(scopePath)
	}

	for strings.HasPrefix(rawPath, "super::") {
		scopePath = rustParentPath(scopePath)
		rawPath = strings.TrimPrefix(rawPath, "super::")
	}

	switch {
	case rawPath == "super":
		return rustParentPath(scopePath)
	case strings.HasPrefix(rawPath, "crate::"):
		return joinRustPath(rustCrateRoot(scopePath), strings.TrimPrefix(rawPath, "crate::"))
	case strings.HasPrefix(rawPath, "self::"):
		return joinRustPath(scopePath, strings.TrimPrefix(rawPath, "self::"))
	default:
		return rawPath
	}
}

func rustCrateRoot(scopePath string) string {
	scopePath = strings.TrimSpace(scopePath)
	if scopePath == "" {
		return ""
	}
	if idx := strings.Index(scopePath, "::"); idx >= 0 {
		return scopePath[:idx]
	}
	return scopePath
}

func rustParentPath(value string) string {
	value = strings.TrimSpace(value)
	if idx := strings.LastIndex(value, "::"); idx >= 0 {
		return value[:idx]
	}
	return rustCrateRoot(value)
}

func joinRustPath(parts ...string) string {
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		part = strings.TrimPrefix(part, "::")
		part = strings.TrimSuffix(part, "::")
		if part == "" {
			continue
		}
		result = append(result, part)
	}
	return strings.Join(result, "::")
}

func rustLastSegment(value string) string {
	value = strings.TrimSpace(strings.TrimSuffix(value, "::*"))
	if idx := strings.LastIndex(value, "::"); idx >= 0 {
		return value[idx+2:]
	}
	return value
}

func rustImplReceiver(line string) (string, bool) {
	matches := rustImplRE.FindStringSubmatch(line)
	if len(matches) != 2 {
		return "", false
	}

	target := strings.TrimSpace(matches[1])
	if idx := strings.Index(target, " where "); idx >= 0 {
		target = target[:idx]
	}
	if idx := strings.LastIndex(target, " for "); idx >= 0 {
		target = target[idx+5:]
	}
	target = strings.TrimSpace(target)
	target = strings.TrimPrefix(target, "&")
	target = strings.TrimPrefix(target, "mut ")
	target = strings.TrimPrefix(target, "dyn ")
	target = strings.TrimPrefix(target, "'_ ")
	target = strings.TrimSpace(target)
	if idx := strings.Index(target, "<"); idx >= 0 {
		target = target[:idx]
	}
	target = strings.TrimPrefix(target, "::")
	target = strings.TrimPrefix(target, "*")
	target = strings.TrimSpace(target)
	if idx := strings.LastIndex(target, "::"); idx >= 0 {
		target = target[idx+2:]
	}
	target = strings.Trim(target, "{}() ")
	if target == "" {
		return "", false
	}
	return target, true
}

func stripRustLineComment(line string) string {
	if idx := strings.Index(line, "//"); idx >= 0 {
		return line[:idx]
	}
	return line
}

func sanitizeRustLine(line string) string {
	var builder strings.Builder
	inSingle := false
	inDouble := false
	escaped := false
	for idx, r := range line {
		if escaped {
			builder.WriteRune(' ')
			escaped = false
			continue
		}
		if !inSingle && !inDouble && idx+1 < len(line) && line[idx] == '/' && line[idx+1] == '/' {
			break
		}
		switch r {
		case '\\':
			if inSingle || inDouble {
				builder.WriteRune(' ')
				escaped = true
				continue
			}
		case '\'':
			if !inDouble {
				inSingle = !inSingle
				builder.WriteRune(' ')
				continue
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
				builder.WriteRune(' ')
				continue
			}
		}
		if inSingle || inDouble {
			builder.WriteRune(' ')
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

func rustBraceDelta(line string) int {
	line = sanitizeRustLine(line)
	open := 0
	close := 0
	for _, r := range line {
		switch r {
		case '{':
			open++
		case '}':
			close++
		}
	}
	return open - close
}

func findRustItemEnd(lines []string, start int) int {
	startDepth := 0
	seenBody := false
	for idx := start; idx < len(lines); idx++ {
		line := sanitizeRustLine(lines[idx])
		delta := rustBraceDelta(line)
		if strings.Contains(line, "{") {
			seenBody = true
		}
		startDepth += delta
		if seenBody && startDepth <= 0 {
			return idx + 1
		}
		if !seenBody && strings.Contains(line, ";") {
			return idx + 1
		}
	}
	return start + 1
}

func rustHasTestAttribute(attrs []string) bool {
	for _, attr := range attrs {
		if strings.Contains(attr, "test") {
			return true
		}
	}
	return false
}
