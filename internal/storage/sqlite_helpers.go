package storage

import (
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/vladimirkasterin/ctx/internal/codebase"
)

func scanRelatedSymbols(rows *sql.Rows) ([]RelatedSymbolView, error) {
	var edges []RelatedSymbolView
	for rows.Next() {
		var edge RelatedSymbolView
		if err := rows.Scan(
			append(symbolMatchScanDest(&edge.Symbol), &edge.UseFilePath, &edge.UseLine, &edge.UseColumn, &edge.Relation)...,
		); err != nil {
			return nil, fmt.Errorf("scan edge: %w", err)
		}
		edge.Why = describeCallRelation(edge.Relation)
		edges = append(edges, edge)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate edges: %w", err)
	}
	return edges, nil
}

func symbolMatchScanDest(symbol *SymbolMatch) []any {
	return []any{
		&symbol.SymbolKey,
		&symbol.QName,
		&symbol.PackageImportPath,
		&symbol.FilePath,
		&symbol.Name,
		&symbol.Kind,
		&symbol.Receiver,
		&symbol.Signature,
		&symbol.Doc,
		&symbol.Line,
		&symbol.Column,
	}
}

func loadStringRows(rows *sql.Rows, err error) ([]string, error) {
	if err != nil {
		return nil, fmt.Errorf("query string rows: %w", err)
	}
	defer rows.Close()

	var values []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("scan string row: %w", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate string rows: %w", err)
	}
	return values, nil
}

func packageList(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func copyTableByPackage(tx *sql.Tx, table, column string, fromID, toID int64, impacted []string) error {
	args := make([]any, 0, len(impacted)+2)
	args = append(args, toID, fromID)
	placeholders := make([]string, 0, len(impacted))
	for _, value := range impacted {
		args = append(args, value)
		placeholders = append(placeholders, "?")
	}

	query := `INSERT INTO ` + table + ` SELECT ?, ` + forwardColumns(table) + ` FROM ` + table + ` WHERE snapshot_id = ?`
	if len(impacted) > 0 {
		query += ` AND ` + column + ` NOT IN (` + strings.Join(placeholders, ",") + `)`
	}

	if _, err := tx.Exec(query, args...); err != nil {
		return fmt.Errorf("copy forward %s: %w", table, err)
	}
	return nil
}

func forwardColumns(table string) string {
	switch table {
	case "packages":
		return `import_path, name, dir_path, file_count`
	case "symbols":
		return `symbol_key, qname, package_import_path, file_path, name, kind, receiver, signature, doc, line, col, exported, is_test`
	case "package_deps":
		return `from_package_import_path, to_package_import_path, is_local`
	case "refs":
		return `from_package_import_path, from_symbol_key, to_symbol_key, file_path, line, col, kind`
	case "call_edges":
		return `caller_package_import_path, caller_symbol_key, callee_symbol_key, file_path, line, col, dispatch`
	case "flow_edges":
		return `owner_package_import_path, owner_symbol_key, file_path, line, col, kind, source_kind, source_label, source_symbol_key, target_kind, target_label, target_symbol_key`
	case "tests":
		return `test_key, package_import_path, file_path, name, kind, line`
	case "test_links":
		return `test_package_import_path, test_key, symbol_key, link_kind, confidence`
	default:
		panic("unsupported forward table: " + table)
	}
}

func derivePackageForFile(root, modulePath string, file codebase.ScanFile) string {
	if value := strings.TrimSpace(file.PackageImportPath); value != "" {
		return value
	}
	relPath := file.RelPath
	if relPath == "go.mod" || relPath == "go.sum" || relPath == "Cargo.toml" || relPath == "Cargo.lock" {
		return ""
	}
	_ = root
	return codebase.ScanPackageImportPath(modulePath, file)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func currentID(ok bool, value int64) int64 {
	if !ok {
		return 0
	}
	return value
}

func symbolSearchKindRank(kind string) int {
	switch kind {
	case "exact":
		return 0
	case "name":
		return 1
	case "prefix":
		return 2
	case "contains":
		return 3
	case "shape":
		return 4
	case "fuzzy":
		return 5
	default:
		return 9
	}
}

func symbolKindSearchBoost(kind string) int {
	switch kind {
	case "func":
		return 8
	case "method":
		return 9
	case "struct", "class":
		return 7
	case "interface", "type", "alias":
		return 6
	default:
		return 2
	}
}

func symbolRelevanceScore(symbol SymbolMatch) int {
	return symbol.CallerCount*7 +
		symbol.ReferenceCount*4 +
		symbol.TestCount*6 +
		symbol.ReversePackageDeps*5 +
		symbol.PackageImportance*2 +
		symbolKindSearchBoost(symbol.Kind)
}

func testConfidenceRank(confidence string) int {
	switch confidence {
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

func testLinkKindRank(kind string) int {
	switch kind {
	case "direct", "receiver_match":
		return 3
	case "related", "name_match":
		return 2
	case "global_name_match":
		return 1
	default:
		return 0
	}
}

func describeDirectTestLink(kind, confidence string) string {
	label := "direct test link"
	switch kind {
	case "direct":
		label = "direct test link"
	case "related":
		label = "related test link"
	case "receiver_match":
		label = "direct receiver match"
	case "name_match":
		label = "direct name match"
	case "global_name_match":
		label = "direct global name match"
	}
	if confidence == "" {
		return label
	}
	return label + " (" + confidence + ")"
}

func describeCallRelation(dispatch string) string {
	switch dispatch {
	case "static":
		return "static call edge from indexed call site"
	case "method":
		return "method call edge from indexed call site"
	case "dynamic":
		return "dynamic call edge from indexed call site"
	case "dynamic_import":
		return "dynamic importlib call edge from indexed call site"
	case "reexport":
		return "re-exported call edge from indexed call site"
	case "":
		return "call edge from indexed call site"
	default:
		return dispatch + " call edge from indexed call site"
	}
}

func describeReferenceKind(kind string) string {
	switch kind {
	case "receiver":
		return "receiver reference in indexed source"
	case "type":
		return "type reference in indexed source"
	case "annotation":
		return "annotation reference in indexed source"
	case "annotation_type":
		return "annotation type reference in indexed source"
	case "type_checking":
		return "TYPE_CHECKING reference in indexed source"
	case "type_checking_type":
		return "TYPE_CHECKING type reference in indexed source"
	case "reexport":
		return "re-export reference in indexed source"
	case "reexport_type":
		return "re-exported type reference in indexed source"
	case "call":
		return "call-site reference in indexed source"
	case "use":
		return "import or use reference in indexed source"
	case "value":
		return "value reference in indexed source"
	case "":
		return "reference in indexed source"
	default:
		return kind + " reference in indexed source"
	}
}

func describeFlowKind(kind string) string {
	switch kind {
	case "param_to_call":
		return "parameter-to-call flow captured by the analyzer"
	case "receiver_to_call":
		return "receiver-to-call flow captured by the analyzer"
	case "call_to_return":
		return "returned call result captured by the analyzer"
	case "param_to_return":
		return "parameter-to-return flow captured by the analyzer"
	case "receiver_to_return":
		return "receiver-to-return flow captured by the analyzer"
	case "receiver_to_state":
		return "receiver state touch captured by the analyzer"
	case "param_to_state":
		return "parameter state touch captured by the analyzer"
	case "":
		return "flow edge captured by the analyzer"
	default:
		return kind + " flow captured by the analyzer"
	}
}

func packageSearchKindRank(kind string) int {
	switch kind {
	case "exact":
		return 0
	case "name":
		return 1
	case "prefix":
		return 2
	case "contains":
		return 3
	default:
		return 9
	}
}

func packageSearchScore(match PackageMatch, query string) (int, string) {
	query = strings.TrimSpace(query)
	if query == "" {
		return 0, ""
	}

	queryLower := strings.ToLower(query)
	importLower := strings.ToLower(strings.TrimSpace(match.ImportPath))
	nameLower := strings.ToLower(strings.TrimSpace(match.Name))
	dirLower := strings.ToLower(strings.TrimSpace(match.DirPath))

	switch {
	case importLower == queryLower:
		return 1200, "exact"
	case nameLower == queryLower:
		return 1150, "name"
	case strings.HasSuffix(importLower, "/"+queryLower) || dirLower == queryLower:
		return 1100, "name"
	case strings.HasPrefix(importLower, queryLower) || strings.HasPrefix(dirLower, queryLower):
		return 1000, "prefix"
	case strings.HasPrefix(nameLower, queryLower):
		return 975, "prefix"
	case strings.Contains(importLower, queryLower) || strings.Contains(dirLower, queryLower):
		return 875, "contains"
	case strings.Contains(nameLower, queryLower):
		return 850, "contains"
	default:
		return 0, ""
	}
}

func symbolSearchScore(symbol SymbolMatch, query string) (int, string) {
	query = strings.TrimSpace(query)
	if query == "" {
		return 0, ""
	}

	queryLower := strings.ToLower(query)
	queryCanonical := canonicalSearchValue(query)
	nameLower := strings.ToLower(strings.TrimSpace(symbol.Name))
	qnameLower := strings.ToLower(strings.TrimSpace(symbol.QName))
	keyLower := strings.ToLower(strings.TrimSpace(symbol.SymbolKey))
	tailLower := symbolQNameTail(qnameLower)

	switch {
	case keyLower == queryLower || qnameLower == queryLower:
		return 1200, "exact"
	case nameLower == queryLower || tailLower == queryLower:
		return 1150, "name"
	case strings.HasPrefix(nameLower, queryLower):
		return 1025, "prefix"
	case strings.HasPrefix(tailLower, queryLower):
		return 1000, "prefix"
	case strings.HasPrefix(qnameLower, queryLower) || strings.HasPrefix(keyLower, queryLower):
		return 975, "prefix"
	case strings.Contains(nameLower, queryLower):
		return 900, "contains"
	case strings.Contains(tailLower, queryLower):
		return 875, "contains"
	case strings.Contains(qnameLower, queryLower) || strings.Contains(keyLower, queryLower):
		return 850, "contains"
	}

	nameCanonical := canonicalSearchValue(symbol.Name)
	tailCanonical := canonicalSearchValue(tailLower)
	qnameCanonical := canonicalSearchValue(qnameLower)
	keyCanonical := canonicalSearchValue(keyLower)

	switch {
	case queryCanonical != "" && (nameCanonical == queryCanonical || tailCanonical == queryCanonical):
		return 825, "shape"
	case queryCanonical != "" && (strings.HasPrefix(nameCanonical, queryCanonical) || strings.HasPrefix(tailCanonical, queryCanonical)):
		return 775, "shape"
	case queryCanonical != "" && (strings.Contains(nameCanonical, queryCanonical) || strings.Contains(tailCanonical, queryCanonical) || strings.Contains(qnameCanonical, queryCanonical) || strings.Contains(keyCanonical, queryCanonical)):
		return 725, "shape"
	}

	if len(queryCanonical) >= 3 {
		switch {
		case isSubsequence(queryCanonical, nameCanonical) || isSubsequence(queryCanonical, tailCanonical):
			return 650, "fuzzy"
		case isSubsequence(queryCanonical, qnameCanonical) || isSubsequence(queryCanonical, keyCanonical):
			return 625, "fuzzy"
		}

		bestDistance := bestSymbolDistance(queryCanonical, nameCanonical, tailCanonical, qnameCanonical, keyCanonical)
		if bestDistance >= 0 {
			maxDistance := max(2, len(queryCanonical)/4)
			if bestDistance <= maxDistance {
				return max(500-bestDistance*60, 320), "fuzzy"
			}
		}
	}

	return 0, ""
}

func canonicalSearchValue(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}

	var builder strings.Builder
	builder.Grow(len(value))
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func symbolQNameTail(qname string) string {
	qname = strings.TrimSpace(qname)
	if qname == "" {
		return ""
	}
	idx := strings.LastIndexAny(qname, "./")
	if idx < 0 || idx+1 >= len(qname) {
		return qname
	}
	return qname[idx+1:]
}

func isSubsequence(needle, haystack string) bool {
	if needle == "" {
		return true
	}
	if haystack == "" {
		return false
	}

	needleRunes := []rune(needle)
	haystackRunes := []rune(haystack)
	idx := 0
	for _, r := range haystackRunes {
		if r == needleRunes[idx] {
			idx++
			if idx == len(needleRunes) {
				return true
			}
		}
	}
	return false
}

func bestSymbolDistance(query string, values ...string) int {
	best := -1
	for _, value := range values {
		if value == "" {
			continue
		}
		distance := levenshteinDistance(query, value)
		if best < 0 || distance < best {
			best = distance
		}
	}
	return best
}

func levenshteinDistance(left, right string) int {
	leftRunes := []rune(left)
	rightRunes := []rune(right)
	if len(leftRunes) == 0 {
		return len(rightRunes)
	}
	if len(rightRunes) == 0 {
		return len(leftRunes)
	}

	prev := make([]int, len(rightRunes)+1)
	curr := make([]int, len(rightRunes)+1)
	for j := 0; j <= len(rightRunes); j++ {
		prev[j] = j
	}

	for i := 1; i <= len(leftRunes); i++ {
		curr[0] = i
		for j := 1; j <= len(rightRunes); j++ {
			cost := 0
			if leftRunes[i-1] != rightRunes[j-1] {
				cost = 1
			}
			curr[j] = min(
				prev[j]+1,
				curr[j-1]+1,
				prev[j-1]+cost,
			)
		}
		copy(prev, curr)
	}
	return int(math.Max(float64(prev[len(rightRunes)]), 0))
}
