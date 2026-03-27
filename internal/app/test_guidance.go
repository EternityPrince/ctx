package app

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/storage"
)

type symbolTestGuidance struct {
	ReadBefore       []storage.TestView
	DirectCount      int
	StrongDirect     int
	NearbyCount      int
	Signal           string
	ImportantWarning string
}

type weakChangedArea struct {
	FilePath            string
	PackageImportPath   string
	HotScore            int
	Risk                string
	RelatedTestCount    int
	TestLinkedCount     int
	RelevantSymbolCount int
	CoveragePercent     int
}

type reportTestWatch struct {
	ThinDirectSymbols []storage.RankedSymbol
	WeakChangedAreas  []weakChangedArea
}

func buildSymbolTestGuidance(store *storage.Store, view storage.SymbolView, limit int) (symbolTestGuidance, error) {
	if limit <= 0 {
		limit = 8
	}

	directCount := len(view.Tests)
	strongDirect := 0
	for _, test := range view.Tests {
		if appTestConfidenceRank(test.Confidence) >= 2 {
			strongDirect++
		}
	}

	type candidate struct {
		test storage.TestView
	}
	byKey := make(map[string]candidate)
	mergeCandidate := func(test storage.TestView) {
		if test.TestKey == "" {
			return
		}
		current, ok := byKey[test.TestKey]
		if !ok || test.Score > current.test.Score {
			if ok && current.test.Why != "" && current.test.Why != test.Why {
				test.Why = mergeWhyParts(test.Why, current.test.Why)
			}
			byKey[test.TestKey] = candidate{test: test}
			return
		}
		current.test.Why = mergeWhyParts(current.test.Why, test.Why)
		byKey[test.TestKey] = current
	}

	for _, test := range view.Tests {
		candidate := test
		if candidate.Relation == "" {
			candidate.Relation = "direct"
		}
		if candidate.Score == 0 {
			candidate.Score = directTestScore(candidate)
		}
		if candidate.Why == "" {
			candidate.Why = directTestWhy(candidate)
		}
		candidate.Score += 320
		mergeCandidate(candidate)
	}

	addRelated := func(symbolKey, symbolName, relation string, limitPerSymbol int) error {
		relatedView, err := store.LoadSymbolView(symbolKey)
		if err != nil {
			return err
		}
		for _, test := range relatedView.Tests[:min(limitPerSymbol, len(relatedView.Tests))] {
			candidate := test
			candidate.Relation = relation
			candidate.Score = relationBaseScore(relation) + directTestScore(test)
			candidate.Why = relationWhy(relation, symbolName, test)
			mergeCandidate(candidate)
		}
		return nil
	}

	for _, caller := range view.Callers[:min(3, len(view.Callers))] {
		if err := addRelated(caller.Symbol.SymbolKey, caller.Symbol.Name, "caller", 3); err != nil {
			return symbolTestGuidance{}, err
		}
	}
	for _, callee := range view.Callees[:min(3, len(view.Callees))] {
		if err := addRelated(callee.Symbol.SymbolKey, callee.Symbol.Name, "callee", 2); err != nil {
			return symbolTestGuidance{}, err
		}
	}
	for _, sibling := range view.Siblings[:min(3, len(view.Siblings))] {
		if err := addRelated(sibling.SymbolKey, sibling.Name, "sibling", 2); err != nil {
			return symbolTestGuidance{}, err
		}
	}

	packageTests, err := store.LoadPackageTests(view.Symbol.PackageImportPath, 10)
	if err != nil {
		return symbolTestGuidance{}, err
	}
	for _, test := range packageTests {
		candidate := test
		candidate.Relation = "package"
		candidate.Score = relationBaseScore("package") + packageFallbackScore(view.Symbol, test)
		candidate.Why = packageTestWhy(view.Symbol, test)
		mergeCandidate(candidate)
	}

	recommended := make([]storage.TestView, 0, len(byKey))
	for _, item := range byKey {
		recommended = append(recommended, item.test)
	}
	sort.Slice(recommended, func(i, j int) bool {
		if recommended[i].Score != recommended[j].Score {
			return recommended[i].Score > recommended[j].Score
		}
		if recommended[i].FilePath != recommended[j].FilePath {
			return recommended[i].FilePath < recommended[j].FilePath
		}
		if recommended[i].Line != recommended[j].Line {
			return recommended[i].Line < recommended[j].Line
		}
		return recommended[i].Name < recommended[j].Name
	})
	if len(recommended) > limit {
		recommended = recommended[:limit]
	}

	nearbyCount := 0
	for _, test := range recommended {
		if test.Relation != "direct" {
			nearbyCount++
		}
	}

	guidance := symbolTestGuidance{
		ReadBefore:   recommended,
		DirectCount:  directCount,
		StrongDirect: strongDirect,
		NearbyCount:  nearbyCount,
	}
	guidance.Signal = summarizeTestGuidance(view, guidance)
	if strongDirect == 0 && nearbyCount == 0 && impactLabel(len(view.Callers), len(view.ReferencesIn), len(view.Tests), len(view.Package.ReverseDeps)) != "low" {
		guidance.ImportantWarning = "important symbol without good nearby tests"
	}
	return guidance, nil
}

func buildReportTestWatch(store *storage.Store, report storage.ReportView) (reportTestWatch, error) {
	watch := reportTestWatch{
		ThinDirectSymbols: thinDirectTestSymbols(report, 6),
	}

	hotScores := make(map[string]int)
	for _, item := range rankShellHotFiles(report, "") {
		hotScores[item.Path] = item.Score
	}
	recentChanged, err := loadRecentChangedFileSet(store)
	if err != nil {
		return reportTestWatch{}, err
	}
	if len(recentChanged) == 0 {
		return watch, nil
	}

	summaries, err := store.LoadFileSummaries()
	if err != nil {
		return reportTestWatch{}, err
	}
	areas := make([]weakChangedArea, 0, len(recentChanged))
	for filePath := range recentChanged {
		summary, ok := summaries[filePath]
		if !ok || summary.IsTest {
			continue
		}
		risk := fileRiskSummary(summary, hotScores[filePath], true)
		if !strings.Contains(risk, "weak-test") {
			continue
		}
		areas = append(areas, weakChangedArea{
			FilePath:            filePath,
			PackageImportPath:   summary.PackageImportPath,
			HotScore:            hotScores[filePath],
			Risk:                risk,
			RelatedTestCount:    summary.RelatedTestCount,
			TestLinkedCount:     summary.TestLinkedSymbolCount,
			RelevantSymbolCount: summary.RelevantSymbolCount,
			CoveragePercent:     coveragePercent(summary.TestLinkedSymbolCount, summary.RelevantSymbolCount),
		})
	}
	sort.Slice(areas, func(i, j int) bool {
		if areas[i].HotScore != areas[j].HotScore {
			return areas[i].HotScore > areas[j].HotScore
		}
		if areas[i].CoveragePercent != areas[j].CoveragePercent {
			return areas[i].CoveragePercent < areas[j].CoveragePercent
		}
		return areas[i].FilePath < areas[j].FilePath
	})
	if len(areas) > 6 {
		areas = areas[:6]
	}
	watch.WeakChangedAreas = areas
	return watch, nil
}

func thinDirectTestSymbols(report storage.ReportView, limit int) []storage.RankedSymbol {
	if limit <= 0 {
		limit = 6
	}
	seen := make(map[string]struct{})
	values := make([]storage.RankedSymbol, 0, len(report.TopFunctions)+len(report.TopTypes))
	appendValues := func(items []storage.RankedSymbol) {
		for _, item := range items {
			if item.TestCount > 0 || item.Score < 7 {
				continue
			}
			if _, ok := seen[item.Symbol.SymbolKey]; ok {
				continue
			}
			seen[item.Symbol.SymbolKey] = struct{}{}
			values = append(values, item)
		}
	}
	appendValues(report.TopFunctions)
	appendValues(report.TopTypes)
	sort.Slice(values, func(i, j int) bool {
		if values[i].Score != values[j].Score {
			return values[i].Score > values[j].Score
		}
		return values[i].Symbol.QName < values[j].Symbol.QName
	})
	if len(values) > limit {
		values = values[:limit]
	}
	return values
}

func summarizeTestGuidance(view storage.SymbolView, guidance symbolTestGuidance) string {
	switch {
	case guidance.StrongDirect > 0 && guidance.NearbyCount > 0:
		return fmt.Sprintf("direct=%d strong | nearby=%d", guidance.StrongDirect, guidance.NearbyCount)
	case guidance.StrongDirect > 0:
		return fmt.Sprintf("direct=%d strong", guidance.StrongDirect)
	case guidance.DirectCount > 0 && guidance.NearbyCount > 0:
		return fmt.Sprintf("direct=%d weak | nearby=%d", guidance.DirectCount, guidance.NearbyCount)
	case guidance.DirectCount > 0:
		return fmt.Sprintf("direct=%d weak", guidance.DirectCount)
	case guidance.NearbyCount > 0:
		return fmt.Sprintf("nearby-only=%d", guidance.NearbyCount)
	default:
		if impactLabel(len(view.Callers), len(view.ReferencesIn), len(view.Tests), len(view.Package.ReverseDeps)) == "low" {
			return "light coverage posture"
		}
		return "thin coverage"
	}
}

func directTestScore(test storage.TestView) int {
	return 60 + appTestConfidenceRank(test.Confidence)*18 + appTestLinkKindRank(test.LinkKind)*6
}

func relationBaseScore(relation string) int {
	switch relation {
	case "direct":
		return 320
	case "caller":
		return 230
	case "callee":
		return 210
	case "sibling":
		return 180
	case "package":
		return 90
	default:
		return 0
	}
}

func relationWhy(relation, symbolName string, test storage.TestView) string {
	base := directTestWhy(test)
	if symbolName == "" {
		symbolName = "nearby symbol"
	}
	switch relation {
	case "caller":
		return fmt.Sprintf("covers caller %s via %s", symbolName, base)
	case "callee":
		return fmt.Sprintf("covers callee %s via %s", symbolName, base)
	case "sibling":
		return fmt.Sprintf("covers nearby %s via %s", symbolName, base)
	default:
		return base
	}
}

func directTestWhy(test storage.TestView) string {
	if test.Why != "" {
		return test.Why
	}
	switch test.LinkKind {
	case "direct":
		return "direct test link"
	case "related":
		return "related test link"
	case "receiver_match":
		return "direct receiver match"
	case "name_match":
		return "direct name match"
	case "global_name_match":
		return "direct global name match"
	default:
		return "direct test link"
	}
}

func packageFallbackScore(symbol storage.SymbolMatch, test storage.TestView) int {
	score := test.Score
	if sameFileFamily(symbol.FilePath, test.FilePath) {
		score += 60
	}
	if testNameMatchesSymbol(symbol, test.Name) {
		score += 50
	}
	return score
}

func packageTestWhy(symbol storage.SymbolMatch, test storage.TestView) string {
	parts := []string{"same package fallback"}
	if sameFileFamily(symbol.FilePath, test.FilePath) {
		parts = append(parts, "same file family")
	}
	if testNameMatchesSymbol(symbol, test.Name) {
		parts = append(parts, "name affinity")
	}
	return strings.Join(parts, ", ")
}

func sameFileFamily(symbolFile, testFile string) bool {
	symbolBase := strings.TrimSuffix(filepath.Base(symbolFile), filepath.Ext(symbolFile))
	testBase := strings.TrimSuffix(filepath.Base(testFile), filepath.Ext(testFile))
	testBase = strings.TrimSuffix(testBase, "_test")
	return symbolBase != "" && symbolBase == testBase
}

func testNameMatchesSymbol(symbol storage.SymbolMatch, testName string) bool {
	if testName == "" {
		return false
	}
	normalizedTest := normalizeSymbolToken(testName)
	if strings.Contains(normalizedTest, normalizeSymbolToken(symbol.Name)) {
		return true
	}
	if symbol.Receiver != "" {
		receiver := normalizeSymbolToken(strings.TrimPrefix(symbol.Receiver, "*"))
		return strings.Contains(normalizedTest, receiver) && strings.Contains(normalizedTest, normalizeSymbolToken(symbol.Name))
	}
	return false
}

func normalizeSymbolToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func mergeWhyParts(left, right string) string {
	switch {
	case left == "":
		return right
	case right == "" || left == right:
		return left
	case strings.Contains(left, right):
		return left
	case strings.Contains(right, left):
		return right
	default:
		return left + "; " + right
	}
}

func appTestConfidenceRank(confidence string) int {
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

func appTestLinkKindRank(kind string) int {
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

func formatTestRelationLabel(test storage.TestView) string {
	switch {
	case test.LinkKind != "" && test.Confidence != "":
		return fmt.Sprintf("[%s/%s]", test.LinkKind, test.Confidence)
	case test.LinkKind != "":
		return fmt.Sprintf("[%s]", test.LinkKind)
	case test.Relation != "":
		return fmt.Sprintf("[%s]", test.Relation)
	default:
		return "[test]"
	}
}
