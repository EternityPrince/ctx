package app

import (
	"fmt"
	"io"

	"github.com/vladimirkasterin/ctx/internal/cli"
	"github.com/vladimirkasterin/ctx/internal/storage"
)

type traceView struct {
	Symbol    storage.SymbolView
	Impact    storage.ImpactView
	History   storage.SymbolHistoryView
	CoChange  storage.CoChangeView
	Guidance  symbolTestGuidance
	File      storage.FileSummary
	Flow      traceFlowView
	Risk      string
	ReadOrder []string
	Checklist []string
}

func runTrace(command cli.Command, stdout io.Writer) error {
	state, err := openPreparedProjectState(command)
	if err != nil {
		return err
	}
	defer state.Close()

	if _, ok, err := ensureIndexedSnapshot(stdout, state); err != nil || !ok {
		return err
	}

	match, found, err := resolveSingleSymbolQuery(stdout, state.Info.ModulePath, state.Store, command.Query)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}

	view, err := buildTraceView(state.Info.ModulePath, state.Store, match.SymbolKey, command.Depth, command.Limit)
	if err != nil {
		return err
	}
	view.Flow = buildTraceFlow(state.Info.Root, state.Info.ModulePath, view.Symbol.Symbol, view.Symbol.Callers, view.Symbol.Callees, view.Symbol.ReferencesOut, view.Symbol.Flow)

	switch command.OutputMode {
	case cli.OutputAI:
		return renderAITrace(stdout, state.Info.ModulePath, view, command.Depth, command.Limit, command.Explain)
	default:
		return renderHumanTrace(stdout, state.Info.Root, state.Info.ModulePath, view, command.Depth, command.Limit, command.Explain)
	}
}

func buildTraceView(modulePath string, store *storage.Store, symbolKey string, depth, limit int) (traceView, error) {
	if depth < 1 {
		depth = 3
	}
	if limit <= 0 {
		limit = 6
	}

	symbolView, err := store.LoadSymbolView(symbolKey)
	if err != nil {
		return traceView{}, err
	}
	impactView, err := store.LoadImpactView(symbolKey, depth)
	if err != nil {
		return traceView{}, err
	}
	historyView, err := store.SymbolHistory(symbolKey, max(limit, 6))
	if err != nil {
		return traceView{}, err
	}
	cochangeView, err := store.SymbolCoChange(symbolKey, max(limit, 6))
	if err != nil {
		return traceView{}, err
	}
	guidance, err := buildSymbolTestGuidance(store, symbolView, max(limit+2, 8))
	if err != nil {
		return traceView{}, err
	}
	fileSummary, err := store.LoadFileSummary(symbolView.Symbol.FilePath)
	if err != nil {
		return traceView{}, err
	}
	riskCtx, err := loadWorkflowRiskContext(store)
	if err != nil {
		return traceView{}, err
	}

	view := traceView{
		Symbol:   symbolView,
		Impact:   impactView,
		History:  historyView,
		CoChange: cochangeView,
		Guidance: guidance,
		File:     fileSummary,
		Risk:     symbolViewRiskWithFileSummary(symbolView, fileSummary, riskCtx.hotScore(symbolView.Symbol.FilePath), riskCtx.recentChanged(symbolView.Symbol.FilePath)),
	}
	view.ReadOrder = buildTraceReadOrder(modulePath, view, limit)
	view.Checklist = buildTraceChecklist(view)
	return view, nil
}

func renderHumanTrace(stdout io.Writer, projectRoot, modulePath string, view traceView, depth, limit int, explain bool) error {
	p := newPalette()
	surface := impactLabel(len(view.Symbol.Callers), len(view.Symbol.ReferencesIn), len(view.Guidance.ReadBefore), len(view.Symbol.Package.ReverseDeps))

	if _, err := fmt.Fprintf(
		stdout,
		"%s\n%s %s %s %s\n\n",
		p.rule("Trace"),
		p.title("CTX Trace"),
		p.kindBadge(view.Symbol.Symbol.Kind),
		p.accent(shortenQName(modulePath, view.Symbol.Symbol.QName)),
		p.badge(surface),
	); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(
		stdout,
		"%s\n  %s symbol\n  %s %s\n  %s %s\n  %s %s\n  %s %d\n  %s %s\n  %s snapshot %d (%s)\n  %s snapshot %d (%s)\n  %s cochange files=%d packages=%d\n\n",
		p.section("Summary"),
		p.label("Scope:"),
		p.label("Target:"),
		shortenQName(modulePath, view.Symbol.Symbol.QName),
		p.label("Surface:"),
		p.badge(surface),
		p.label("Risk:"),
		view.Risk,
		p.label("Quality score:"),
		view.Symbol.QualityScore,
		p.label("Test signal:"),
		view.Guidance.Signal,
		p.label("Introduced in:"),
		view.History.IntroducedIn.ID,
		view.History.IntroducedIn.CreatedAt.Format(timeFormat),
		p.label("Changed since:"),
		view.History.LastChangedIn.ID,
		view.History.LastChangedIn.CreatedAt.Format(timeFormat),
		p.label("Empirical context:"),
		len(view.CoChange.Files),
		len(view.CoChange.Packages),
	); err != nil {
		return err
	}

	if explain {
		if err := renderHumanExplainSection(stdout, p, buildTraceExplain(modulePath, view, depth)); err != nil {
			return err
		}
	}
	if err := renderHumanDeclaration(stdout, p, projectRoot, view.Symbol.Symbol); err != nil {
		return err
	}
	if err := renderHumanSource(stdout, p, projectRoot, view.Symbol.Symbol.FilePath, view.Symbol.Symbol.Line); err != nil {
		return err
	}
	if err := renderHumanPackageSummary(stdout, p, modulePath, view.Symbol.Package); err != nil {
		return err
	}
	if err := renderHumanTraceFlow(stdout, p, view.Flow, max(limit, 6)); err != nil {
		return err
	}
	if err := renderHumanRelatedSymbols(stdout, p, projectRoot, modulePath, "Direct Callers", view.Symbol.Callers, max(limit, 4), true); err != nil {
		return err
	}
	if err := renderHumanImpactNodes(stdout, p, projectRoot, modulePath, fmt.Sprintf("Transitive Callers (depth<=%d)", depth), view.Impact.TransitiveCallers, max(limit, 6)); err != nil {
		return err
	}
	if err := renderHumanRelatedSymbols(stdout, p, projectRoot, modulePath, "Direct Callees", view.Symbol.Callees, max(limit, 4), true); err != nil {
		return err
	}
	if err := renderHumanReferences(stdout, p, projectRoot, modulePath, "Inbound References", view.Symbol.ReferencesIn, max(limit, 4)); err != nil {
		return err
	}
	if err := renderHumanReferences(stdout, p, projectRoot, modulePath, "Outbound References", view.Symbol.ReferencesOut, max(limit, 4)); err != nil {
		return err
	}
	if view.Impact.HasRecentDelta {
		if err := renderHumanRecentImpactDelta(stdout, p, modulePath, view.Impact.RecentDelta); err != nil {
			return err
		}
	}
	if len(view.Impact.BlastPackageReasons) > 0 {
		if err := renderHumanPackageReasons(stdout, p, modulePath, "Blast Packages", view.Impact.BlastPackageReasons, max(limit, 6)); err != nil {
			return err
		}
	}
	if len(view.Impact.BlastFileReasons) > 0 {
		if err := renderHumanFileReasons(stdout, p, "Blast Files", view.Impact.BlastFileReasons, max(limit, 6)); err != nil {
			return err
		}
	}
	if err := renderHumanCoChangeItems(stdout, p, "Files That Change Together", view.CoChange.Files, max(limit, 6)); err != nil {
		return err
	}
	if err := renderHumanCoChangeItems(stdout, p, "Packages That Change Together", view.CoChange.Packages, max(limit, 6)); err != nil {
		return err
	}
	if err := renderHumanTestGuidance(stdout, p, projectRoot, view.Guidance, max(limit+2, 8)); err != nil {
		return err
	}
	if err := renderHumanChecklist(stdout, p, "Read This Order", view.ReadOrder, max(limit, 5)); err != nil {
		return err
	}
	return renderHumanChecklist(stdout, p, "Review Checklist", view.Checklist, max(limit, 6))
}

func renderAITrace(stdout io.Writer, modulePath string, view traceView, depth, limit int, explain bool) error {
	if _, err := fmt.Fprintf(
		stdout,
		"trace q=%s kind=%s file=%s:%d package=%s surface=%s risk=%q quality=%d depth=%d\n",
		shortenQName(modulePath, view.Symbol.Symbol.QName),
		view.Symbol.Symbol.Kind,
		view.Symbol.Symbol.FilePath,
		view.Symbol.Symbol.Line,
		shortenQName(modulePath, view.Symbol.Symbol.PackageImportPath),
		impactLabel(len(view.Symbol.Callers), len(view.Symbol.ReferencesIn), len(view.Guidance.ReadBefore), len(view.Symbol.Package.ReverseDeps)),
		view.Risk,
		view.Symbol.QualityScore,
		depth,
	); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "sig=%q\n", displaySignature(view.Symbol.Symbol)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "history introduced=%d changed_since=%d cochange_files=%d cochange_packages=%d test_signal=%q\n", view.History.IntroducedIn.ID, view.History.LastChangedIn.ID, len(view.CoChange.Files), len(view.CoChange.Packages), view.Guidance.Signal); err != nil {
		return err
	}
	if explain {
		if err := renderAIExplainSection(stdout, "explain", buildTraceExplain(modulePath, view, depth)); err != nil {
			return err
		}
	}
	if err := renderAITraceFlow(stdout, view.Flow, max(limit, 6)); err != nil {
		return err
	}
	if err := renderAIRelatedSymbols(stdout, modulePath, "callers", view.Symbol.Callers, max(limit, 4), true); err != nil {
		return err
	}
	if err := renderAIImpactNodes(stdout, modulePath, "transitive_callers", view.Impact.TransitiveCallers, max(limit, 6)); err != nil {
		return err
	}
	if err := renderAIRelatedSymbols(stdout, modulePath, "callees", view.Symbol.Callees, max(limit, 4), true); err != nil {
		return err
	}
	if err := renderAIReferences(stdout, modulePath, "refs_in", view.Symbol.ReferencesIn, max(limit, 4)); err != nil {
		return err
	}
	if err := renderAIReferences(stdout, modulePath, "refs_out", view.Symbol.ReferencesOut, max(limit, 4)); err != nil {
		return err
	}
	if err := renderAITests(stdout, "tests", view.Guidance.ReadBefore, max(limit+2, 8)); err != nil {
		return err
	}
	if err := renderAIPackageReasons(stdout, modulePath, "blast_packages", view.Impact.BlastPackageReasons, max(limit, 6)); err != nil {
		return err
	}
	if err := renderAIFileReasons(stdout, "blast_files", view.Impact.BlastFileReasons, max(limit, 6)); err != nil {
		return err
	}
	if err := renderAIChecklist(stdout, "read_order", view.ReadOrder, max(limit, 5)); err != nil {
		return err
	}
	return renderAIChecklist(stdout, "review_checklist", view.Checklist, max(limit, 6))
}

func buildTraceExplain(modulePath string, view traceView, depth int) explainSection {
	section := explainSection{
		Title: "Explain",
		Facts: []explainFact{
			{Key: "Scope", Value: "trace around " + shortenQName(modulePath, view.Symbol.Symbol.QName)},
			{Key: "Surface", Value: impactLabel(len(view.Symbol.Callers), len(view.Symbol.ReferencesIn), len(view.Guidance.ReadBefore), len(view.Symbol.Package.ReverseDeps))},
			{Key: "History", Value: fmt.Sprintf("introduced snapshot %d; last changed snapshot %d", view.History.IntroducedIn.ID, view.History.LastChangedIn.ID)},
			{Key: "Precision", Value: fmt.Sprintf("static callers/refs/tests + snapshot history + co-change, with caller depth %d", depth)},
		},
		Notes: []string{
			"read order starts at the contract, then moves upstream to real callers and safety tests before widening to blast files",
			"dynamic runtime paths and non-indexed behavior remain best-effort, especially around reflection, macros, and import-time wiring",
		},
	}
	if len(view.Symbol.Callers) > 0 {
		items := make([]explainItem, 0, min(4, len(view.Symbol.Callers)))
		for _, caller := range view.Symbol.Callers[:min(4, len(view.Symbol.Callers))] {
			detail := caller.Why
			if detail == "" {
				detail = fmt.Sprintf("via %s @ %s:%d", shortenQName(modulePath, caller.Symbol.QName), caller.UseFilePath, caller.UseLine)
			}
			items = append(items, explainItem{
				Label:   shortenQName(modulePath, caller.Symbol.QName),
				Details: []string{detail},
			})
		}
		section.Groups = append(section.Groups, explainGroup{Title: "Strongest upstream signals", Items: items})
	}
	if len(view.Impact.BlastPackageReasons) > 0 {
		items := make([]explainItem, 0, min(4, len(view.Impact.BlastPackageReasons)))
		for _, reason := range view.Impact.BlastPackageReasons[:min(4, len(view.Impact.BlastPackageReasons))] {
			items = append(items, explainItem{
				Label:   shortenQName(modulePath, reason.PackageImportPath),
				Details: append([]string(nil), reason.Why...),
			})
		}
		section.Groups = append(section.Groups, explainGroup{Title: "Blast packages", Items: items})
	}
	if len(view.ReadOrder) > 0 {
		items := make([]explainItem, 0, min(5, len(view.ReadOrder)))
		for _, item := range view.ReadOrder[:min(5, len(view.ReadOrder))] {
			items = append(items, explainItem{Label: item})
		}
		section.Groups = append(section.Groups, explainGroup{Title: "Read this order", Items: items})
	}
	if !traceFlowEmpty(view.Flow) && len(view.Flow.FlowPath) > 0 {
		items := make([]explainItem, 0, min(5, len(view.Flow.FlowPath)))
		for _, item := range view.Flow.FlowPath[:min(5, len(view.Flow.FlowPath))] {
			items = append(items, explainItem{Label: item})
		}
		section.Groups = append(section.Groups, explainGroup{Title: "Data flow path", Items: items})
	}
	return section
}

func buildTraceReadOrder(modulePath string, view traceView, limit int) []string {
	if limit <= 0 {
		limit = 5
	}
	items := []string{
		fmt.Sprintf("Start at %s in %s:%d to lock the current contract and nearby helpers.", shortenQName(modulePath, view.Symbol.Symbol.QName), view.Symbol.Symbol.FilePath, view.Symbol.Symbol.Line),
	}
	for _, caller := range view.Symbol.Callers[:min(2, len(view.Symbol.Callers))] {
		items = append(items, fmt.Sprintf("Then inspect caller %s via %s:%d.", shortenQName(modulePath, caller.Symbol.QName), caller.UseFilePath, caller.UseLine))
	}
	for _, test := range view.Guidance.ReadBefore[:min(2, len(view.Guidance.ReadBefore))] {
		items = append(items, fmt.Sprintf("Read test %s at %s:%d before editing behavior.", test.Name, test.FilePath, test.Line))
	}
	for _, file := range view.Impact.BlastFiles[:min(2, len(view.Impact.BlastFiles))] {
		items = append(items, fmt.Sprintf("Skim blast file %s because it shares the same change surface.", file))
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return items
}

func buildTraceChecklist(view traceView) []string {
	items := make([]string, 0, 6)
	if len(view.Symbol.Callers) > 0 || len(view.Symbol.ReferencesIn) > 0 {
		items = append(items, "Validate direct callers and inbound refs before changing the contract.")
	}
	if len(view.Symbol.Callees) > 0 {
		items = append(items, "Check direct callees before refactoring internals so helper assumptions stay intact.")
	}
	if view.Guidance.StrongDirect == 0 {
		items = append(items, "Nearby direct test coverage is thin, so plan an extra verification pass.")
	}
	if len(view.Impact.BlastPackages) > 0 {
		items = append(items, "Scan blast packages for shared assumptions and reverse-dependency pressure.")
	}
	if view.Impact.HasRecentDelta {
		items = append(items, "Recent snapshot history already changed this symbol surface, so watch for compounding regressions.")
	}
	if len(items) == 0 {
		items = append(items, "This symbol is relatively contained; verify local tests and callers, then edit in place.")
	}
	return items
}
