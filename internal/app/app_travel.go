package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/vladimirkasterin/ctx/internal/cli"
	"github.com/vladimirkasterin/ctx/internal/storage"
)

type travelResolution struct {
	Recipe      string
	RunArgs     []string
	Language    string
	Launcher    string
	FilePath    string
	Symbol      storage.SymbolMatch
	Inference   []string
	Candidates  []string
	TargetScore int
}

type travelView struct {
	Resolution  travelResolution
	Runtime     travelRunMetrics
	StoredRunID int64
	StoredAt    time.Time
	Entry       traceView
	File        handoffFileView
	CallPath    []string
	Significant []storage.RankedSymbol
	ReadFirst   []string
	Checklist   []string
}

type travelRunMetrics struct {
	Attempted   bool
	Skipped     bool
	TimedOut    bool
	ExitCode    int
	Status      string
	Error       string
	Stderr      string
	Wall        time.Duration
	UserCPU     time.Duration
	SystemCPU   time.Duration
	MaxRSSBytes int64
	Timeout     time.Duration
}

type travelTableRow struct {
	Left  string
	Value string
	Note  string
}

type limitedBuffer struct {
	Limit int
	Bytes []byte
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	original := len(p)
	if b.Limit <= 0 {
		return original, nil
	}
	remaining := b.Limit - len(b.Bytes)
	if remaining <= 0 {
		return original, nil
	}
	if len(p) > remaining {
		p = p[:remaining]
	}
	b.Bytes = append(b.Bytes, p...)
	return original, nil
}

func (b *limitedBuffer) String() string {
	return strings.TrimSpace(string(b.Bytes))
}

func runTravel(command cli.Command, stdout io.Writer) error {
	switch command.Scope {
	case "show-all", "show-one":
		return runStoredTravel(command, stdout)
	}

	state, err := openPreparedProjectState(command)
	if err != nil {
		return err
	}
	defer state.Close()

	if _, ok, err := ensureIndexedSnapshot(stdout, state); err != nil || !ok {
		return err
	}

	summaries, err := state.Store.LoadFileSummaries()
	if err != nil {
		return err
	}

	resolution, found, err := resolveTravelTarget(state.Info.Root, state.Info.ModulePath, state.Info.Language, state.Store, summaries, command.RunRecipe, command.RunArgs)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}

	view := travelView{Resolution: resolution}
	view.Runtime = measureTravelRun(state.Info.Root, view.Resolution, command.TravelTimeout, command.TravelNoRun)
	if resolution.Symbol.SymbolKey != "" {
		view.Entry, err = buildTraceView(state.Info.ModulePath, state.Store, resolution.Symbol.SymbolKey, command.Depth, command.Limit)
		if err != nil {
			return err
		}
		view.Entry.Flow = buildTraceFlow(state.Info.Root, state.Info.ModulePath, view.Entry.Symbol.Symbol, view.Entry.Symbol.Callers, view.Entry.Symbol.Callees, view.Entry.Symbol.ReferencesOut, view.Entry.Symbol.Flow)
		view.CallPath, view.Significant, err = buildTravelCallPath(state.Info.ModulePath, state.Store, view.Entry.Symbol.Symbol.SymbolKey, command.Depth, command.Limit)
		if err != nil {
			return err
		}
		view.ReadFirst = buildTravelReadFirst(state.Info.ModulePath, view.Resolution, view.Entry, view.CallPath, view.Significant)
		view.Checklist = buildTravelChecklist(view.Resolution, view.Entry, view.CallPath)
	} else {
		view.File, err = buildFileHandoffView(state.Info.ModulePath, state.Store, resolution.FilePath, command.Limit)
		if err != nil {
			return err
		}
		view.ReadFirst = buildTravelFileReadFirst(view.Resolution, view.File)
		view.Checklist = buildTravelFileChecklist(view.Resolution, view.File)
	}

	saved, err := state.Store.CreateTravelRun(buildStoredTravelRecord(command, view))
	if err != nil {
		return err
	}
	view.StoredRunID = saved.ID
	view.StoredAt = saved.CreatedAt

	humanOutput, err := renderHumanTravelString(state.Info.Root, state.Info.ModulePath, view, command.Depth, command.Limit, command.Explain)
	if err != nil {
		return err
	}
	aiOutput, err := renderAITravelString(state.Info.Root, state.Info.ModulePath, view, command.Depth, command.Limit, command.Explain)
	if err != nil {
		return err
	}
	if err := state.Store.UpdateTravelRunOutputs(saved.ID, humanOutput, aiOutput); err != nil {
		return err
	}

	switch command.OutputMode {
	case cli.OutputAI:
		_, err = io.WriteString(stdout, aiOutput)
	default:
		_, err = io.WriteString(stdout, humanOutput)
	}
	return err
}

func resolveTravelTarget(root, modulePath, language string, store *storage.Store, summaries map[string]storage.FileSummary, recipe string, runArgs []string) (travelResolution, bool, error) {
	tokens, err := splitTravelRecipe(recipe)
	if err != nil {
		return travelResolution{}, false, err
	}
	if len(tokens) == 0 {
		return travelResolution{}, false, errors.New("travel run recipe is empty")
	}

	trimmed, notes := stripTravelWrappers(tokens)
	if len(trimmed) == 0 {
		return travelResolution{}, false, errors.New("travel run recipe does not contain an executable after wrappers")
	}

	resolution := travelResolution{
		Recipe:    recipe,
		RunArgs:   append([]string(nil), runArgs...),
		Language:  language,
		Launcher:  travelLauncherLabel(trimmed),
		Inference: append([]string(nil), notes...),
	}

	candidates, inferNotes, err := inferTravelFileCandidates(root, language, store, summaries, trimmed)
	if err != nil {
		return travelResolution{}, false, err
	}
	resolution.Inference = append(resolution.Inference, inferNotes...)
	if len(candidates) == 0 {
		return travelResolution{}, false, fmt.Errorf("could not infer an indexed entrypoint from %q", recipe)
	}

	resolution.Candidates = make([]string, 0, min(len(candidates), 4))
	for _, candidate := range candidates[:min(len(candidates), 4)] {
		resolution.Candidates = append(resolution.Candidates, candidate.FilePath)
	}

	chosen := candidates[0]
	resolution.FilePath = chosen.FilePath
	resolution.TargetScore = chosen.QualityScore
	resolution.Inference = append(resolution.Inference, fmt.Sprintf("entry file inferred as %s.", chosen.FilePath))

	symbol, symbolNotes, found, err := resolveTravelEntrySymbol(modulePath, store, chosen.FilePath)
	if err != nil {
		return travelResolution{}, false, err
	}
	resolution.Inference = append(resolution.Inference, symbolNotes...)
	if found {
		resolution.Symbol = symbol
	}
	return resolution, true, nil
}

func renderHumanTravel(stdout io.Writer, projectRoot, modulePath string, view travelView, depth, limit int, explain bool) error {
	p := newPalette()
	title := p.title("CTX Travel")
	if _, err := fmt.Fprintf(stdout, "%s\n%s\n\n", p.rule("Travel"), title); err != nil {
		return err
	}

	argsLabel := "none"
	if len(view.Resolution.RunArgs) > 0 {
		argsLabel = strings.Join(view.Resolution.RunArgs, " ")
	}
	target := view.Resolution.FilePath
	if view.Resolution.Symbol.SymbolKey != "" {
		target = shortenQName(modulePath, view.Resolution.Symbol.QName)
	}
	if err := renderHumanTravelOverview(stdout, p, view, target, argsLabel, depth); err != nil {
		return err
	}
	if err := renderHumanTravelRuntime(stdout, p, view.Runtime); err != nil {
		return err
	}

	if explain {
		if err := renderHumanExplainSection(stdout, p, buildTravelExplain(modulePath, view, depth)); err != nil {
			return err
		}
	}

	if view.Resolution.Symbol.SymbolKey != "" {
		if err := renderHumanDeclaration(stdout, p, projectRoot, view.Entry.Symbol.Symbol); err != nil {
			return err
		}
		if err := renderHumanSource(stdout, p, projectRoot, view.Entry.Symbol.Symbol.FilePath, view.Entry.Symbol.Symbol.Line); err != nil {
			return err
		}
		if err := renderHumanTraceFlow(stdout, p, view.Entry.Flow, max(limit, 6)); err != nil {
			return err
		}
		if err := renderHumanChecklist(stdout, p, "Likely Call Path", view.CallPath, max(limit+2, 8)); err != nil {
			return err
		}
		if err := renderHumanRankedSymbols(stdout, p, modulePath, "Important Functions Along This Launch", view.Significant, explain); err != nil {
			return err
		}
		if err := renderHumanTestGuidance(stdout, p, projectRoot, view.Entry.Guidance, max(limit+2, 8)); err != nil {
			return err
		}
	} else {
		if err := renderHumanChecklist(stdout, p, "Key Symbols", view.File.SymbolLines, max(limit, 6)); err != nil {
			return err
		}
		if err := renderHumanChecklist(stdout, p, "Nearby Surface", view.File.SurfaceLines, max(limit, 6)); err != nil {
			return err
		}
		if err := renderHumanTests(stdout, p, projectRoot, "Tests To Read", view.File.Tests, max(limit+2, 8)); err != nil {
			return err
		}
	}
	if err := renderHumanChecklist(stdout, p, "Read This Order", view.ReadFirst, max(limit, 6)); err != nil {
		return err
	}
	return renderHumanChecklist(stdout, p, "Review Checklist", view.Checklist, max(limit, 6))
}

func renderAITravel(stdout io.Writer, projectRoot, modulePath string, view travelView, depth, limit int, explain bool) error {
	if _, err := fmt.Fprintf(
		stdout,
		"travel id=%d created_at=%s launcher=%q language=%s recipe=%q args=%d depth=%d entry_file=%s entry_symbol=%s\n",
		view.StoredRunID,
		travelStoredAtValue(view.StoredAt),
		view.Resolution.Launcher,
		view.Resolution.Language,
		view.Resolution.Recipe,
		len(view.Resolution.RunArgs),
		depth,
		view.Resolution.FilePath,
		shortenQName(modulePath, view.Resolution.Symbol.QName),
	); err != nil {
		return err
	}
	for _, arg := range view.Resolution.RunArgs {
		if _, err := fmt.Fprintf(stdout, "arg=%q\n", arg); err != nil {
			return err
		}
	}
	if err := renderAITravelRuntime(stdout, view.Runtime); err != nil {
		return err
	}
	if explain {
		if err := renderAIExplainSection(stdout, "explain", buildTravelExplain(modulePath, view, depth)); err != nil {
			return err
		}
	}
	if view.Resolution.Symbol.SymbolKey != "" {
		if err := renderAITraceFlow(stdout, view.Entry.Flow, max(limit, 6)); err != nil {
			return err
		}
		if err := renderAIChecklist(stdout, "call_path", view.CallPath, max(limit+2, 8)); err != nil {
			return err
		}
		if err := renderAIRankedSymbols(stdout, modulePath, "important_functions", view.Significant, explain); err != nil {
			return err
		}
		if err := renderAITests(stdout, "tests", view.Entry.Guidance.ReadBefore, max(limit+2, 8)); err != nil {
			return err
		}
	} else {
		if err := renderAIChecklist(stdout, "key_symbols", view.File.SymbolLines, max(limit, 6)); err != nil {
			return err
		}
		if err := renderAITests(stdout, "tests", view.File.Tests, max(limit+2, 8)); err != nil {
			return err
		}
	}
	if err := renderAIChecklist(stdout, "read_order", view.ReadFirst, max(limit, 6)); err != nil {
		return err
	}
	return renderAIChecklist(stdout, "review_checklist", view.Checklist, max(limit, 6))
}

func renderHumanTravelString(projectRoot, modulePath string, view travelView, depth, limit int, explain bool) (string, error) {
	var out strings.Builder
	if err := renderHumanTravel(&out, projectRoot, modulePath, view, depth, limit, explain); err != nil {
		return "", err
	}
	return out.String(), nil
}

func renderAITravelString(projectRoot, modulePath string, view travelView, depth, limit int, explain bool) (string, error) {
	var out strings.Builder
	if err := renderAITravel(&out, projectRoot, modulePath, view, depth, limit, explain); err != nil {
		return "", err
	}
	return out.String(), nil
}

func buildTravelExplain(modulePath string, view travelView, depth int) explainSection {
	section := explainSection{
		Title: "Explain",
		Facts: []explainFact{
			{Key: "Scope", Value: "travel from " + view.Resolution.Recipe},
			{Key: "Entry", Value: entryTravelLabel(modulePath, view.Resolution)},
			{Key: "Precision", Value: fmt.Sprintf("static entrypoint inference plus indexed call/ref graph, walking up to depth %d from the inferred launch surface", depth)},
		},
		Notes: []string{
			"travel does not execute the target program; reflection, plugin loading, generated wiring, and runtime-only dispatch remain best-effort",
		},
	}
	if !view.Runtime.Skipped {
		section.Notes = []string{
			"travel runs the recipe to collect wall time, CPU time, and peak RSS, but the reading path still comes from static indexed relationships",
			"performance metrics cover the full recipe invocation, including wrappers such as `go run` compile cost or `uv run` startup cost",
		}
		if view.Runtime.Timeout > 0 {
			section.Notes = append(section.Notes, "runtime measurement is bounded by the configured timeout and may describe a partial run if the process does not exit in time")
		}
	}
	if len(view.Resolution.RunArgs) > 0 {
		section.Notes = append(section.Notes, "runtime args are preserved in the summary, but today they only inform the reading context rather than control-flow pruning")
	}
	if len(view.Resolution.Inference) > 0 {
		items := make([]explainItem, 0, min(5, len(view.Resolution.Inference)))
		for _, note := range view.Resolution.Inference[:min(5, len(view.Resolution.Inference))] {
			items = append(items, explainItem{Label: note})
		}
		section.Groups = append(section.Groups, explainGroup{Title: "Inference path", Items: items})
	}
	if len(view.CallPath) > 0 {
		items := make([]explainItem, 0, min(5, len(view.CallPath)))
		for _, item := range view.CallPath[:min(5, len(view.CallPath))] {
			items = append(items, explainItem{Label: item})
		}
		section.Groups = append(section.Groups, explainGroup{Title: "Likely call path", Items: items})
	}
	return section
}

func buildTravelCallPath(modulePath string, store *storage.Store, entryKey string, depth, limit int) ([]string, []storage.RankedSymbol, error) {
	if depth < 1 {
		depth = 4
	}
	if limit <= 0 {
		limit = 6
	}

	cache := make(map[string]storage.SymbolView)
	loadView := func(symbolKey string) (storage.SymbolView, error) {
		if view, ok := cache[symbolKey]; ok {
			return view, nil
		}
		view, err := store.LoadSymbolView(symbolKey)
		if err != nil {
			return storage.SymbolView{}, err
		}
		cache[symbolKey] = view
		return view, nil
	}

	entryView, err := loadView(entryKey)
	if err != nil {
		return nil, nil, err
	}

	type queueItem struct {
		Key   string
		Depth int
	}
	type edgeItem struct {
		ParentKey string
		ChildKey  string
		UseFile   string
		UseLine   int
	}

	depthByKey := map[string]int{entryKey: 0}
	parentByKey := map[string]string{}
	order := []string{entryKey}
	edges := make([]edgeItem, 0)
	queue := []queueItem{{Key: entryKey, Depth: 0}}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current.Depth >= depth {
			continue
		}

		view, err := loadView(current.Key)
		if err != nil {
			return nil, nil, err
		}
		callees := sortRelatedByUseSite(view.Callees)
		for _, callee := range callees {
			childKey := strings.TrimSpace(callee.Symbol.SymbolKey)
			if childKey == "" {
				continue
			}
			nextDepth := current.Depth + 1
			if seenDepth, ok := depthByKey[childKey]; ok && seenDepth <= nextDepth {
				continue
			}
			if _, err := loadView(childKey); err != nil {
				return nil, nil, err
			}
			depthByKey[childKey] = nextDepth
			parentByKey[childKey] = current.Key
			order = append(order, childKey)
			edges = append(edges, edgeItem{
				ParentKey: current.Key,
				ChildKey:  childKey,
				UseFile:   callee.UseFilePath,
				UseLine:   callee.UseLine,
			})
			queue = append(queue, queueItem{Key: childKey, Depth: nextDepth})
		}
	}

	callPath := make([]string, 0, min(len(edges), max(limit+2, 8)))
	for _, edge := range edges[:min(len(edges), max(limit+2, 8))] {
		parent := cache[edge.ParentKey]
		child := cache[edge.ChildKey]
		callPath = append(callPath, fmt.Sprintf("%s -> %s @ %s:%d", shortenQName(modulePath, parent.Symbol.QName), shortenQName(modulePath, child.Symbol.QName), edge.UseFile, edge.UseLine))
	}

	significant := make([]storage.RankedSymbol, 0, len(order))
	for _, key := range order {
		view := cache[key]
		depthValue := depthByKey[key]
		parentLabel := ""
		if parentKey, ok := parentByKey[key]; ok {
			parentLabel = shortenQName(modulePath, cache[parentKey].Symbol.QName)
		}
		significant = append(significant, buildTravelRankedSymbol(modulePath, view, depthValue, key == entryKey, parentLabel))
	}

	sort.Slice(significant, func(i, j int) bool {
		if significant[i].Score != significant[j].Score {
			return significant[i].Score > significant[j].Score
		}
		if depthByKey[significant[i].Symbol.SymbolKey] != depthByKey[significant[j].Symbol.SymbolKey] {
			return depthByKey[significant[i].Symbol.SymbolKey] < depthByKey[significant[j].Symbol.SymbolKey]
		}
		if significant[i].Symbol.Line != significant[j].Symbol.Line {
			return significant[i].Symbol.Line < significant[j].Symbol.Line
		}
		return significant[i].Symbol.QName < significant[j].Symbol.QName
	})
	if len(significant) > max(limit, 5) {
		significant = significant[:max(limit, 5)]
	}
	if len(callPath) == 0 && len(entryView.Callees) == 0 {
		callPath = append(callPath, "No indexed callee path from the inferred entrypoint; read the entry file and local helpers first.")
	}
	return callPath, significant, nil
}

func buildTravelRankedSymbol(modulePath string, view storage.SymbolView, depth int, entry bool, parentLabel string) storage.RankedSymbol {
	score := view.QualityScore + len(view.Callers)*3 + len(view.Callees) + len(view.ReferencesIn)*2 + len(view.Tests)*4 + len(view.Package.ReverseDeps)*2
	why := make([]string, 0, 6)

	if entry {
		score += 18
		why = append(why, "launch entrypoint")
	} else {
		bonus := max(2, 10-depth*2)
		score += bonus
		why = append(why, fmt.Sprintf("reachable in %d call(s) from the entrypoint", depth))
	}
	if parentLabel != "" {
		why = append(why, "called from "+parentLabel)
	}
	if strings.EqualFold(view.Symbol.Name, "main") && view.Symbol.Kind == "func" {
		score += 10
		why = append(why, "main entry symbol")
	}
	if travelLaunchVerb(view.Symbol.Name) {
		score += 6
		why = append(why, "launch-orchestration name")
	}
	if len(view.Flow) > 0 {
		score += 4
		why = append(why, "has analyzer-backed flow edges")
	}
	if len(view.Tests) > 0 {
		why = append(why, fmt.Sprintf("%d direct tests", len(view.Tests)))
	}
	why = append(why, fmt.Sprintf("risk=%s", symbolViewRiskSummary(view)))

	return storage.RankedSymbol{
		Symbol:             view.Symbol,
		CallerCount:        len(view.Callers),
		CalleeCount:        len(view.Callees),
		ReferenceCount:     len(view.ReferencesIn),
		TestCount:          len(view.Tests),
		ReversePackageDeps: len(view.Package.ReverseDeps),
		GraphScore:         view.QualityScore,
		Score:              score,
		QualityWhy:         uniqueStrings(why),
		Provenance: []storage.ProvenanceItem{
			{
				Kind:     "travel",
				Label:    shortenQName(modulePath, view.Symbol.QName),
				FilePath: view.Symbol.FilePath,
				Line:     view.Symbol.Line,
				Why:      strings.Join(uniqueStrings(why), "; "),
			},
		},
	}
}

func buildTravelReadFirst(modulePath string, resolution travelResolution, entry traceView, callPath []string, significant []storage.RankedSymbol) []string {
	items := []string{
		fmt.Sprintf("Start with %s because the run recipe lands there first.", entryTravelLabel(modulePath, resolution)),
	}
	if entry.Symbol.Symbol.SymbolKey != "" {
		items = append(items, fmt.Sprintf("Read the body of %s before widening to helpers.", shortenQName(modulePath, entry.Symbol.Symbol.QName)))
	}
	if len(callPath) > 0 {
		items = append(items, "Then follow the first call-path edge to understand how control leaves the entrypoint.")
	}
	if len(significant) > 1 {
		items = append(items, fmt.Sprintf("After the entrypoint, inspect %s as the highest-signal downstream function.", shortenQName(modulePath, significant[1].Symbol.QName)))
	}
	if len(entry.Guidance.ReadBefore) > 0 {
		test := entry.Guidance.ReadBefore[0]
		items = append(items, fmt.Sprintf("Read test %s at %s:%d before changing behavior.", test.Name, test.FilePath, test.Line))
	}
	if len(resolution.RunArgs) > 0 {
		items = append(items, "Keep the provided runtime args in mind while reading any parsing and normalization helpers.")
	}
	return uniqueStrings(items)
}

func buildTravelChecklist(resolution travelResolution, entry traceView, callPath []string) []string {
	items := make([]string, 0, 6)
	if len(resolution.Candidates) > 1 {
		items = append(items, "Multiple entry files looked plausible, so sanity-check the inferred launch surface before editing deeper behavior.")
	}
	if len(callPath) == 0 {
		items = append(items, "The indexed call path is thin here; read the entry file and adjacent helpers manually.")
	}
	if len(entry.Guidance.ReadBefore) == 0 {
		items = append(items, "Nearby tests are weak, so plan an explicit verification step for this launch path.")
	}
	if len(entry.Symbol.Callees) == 0 {
		items = append(items, "The entrypoint has no indexed callees, so watch for runtime wiring, reflection, or external script boundaries.")
	}
	if len(resolution.RunArgs) > 0 {
		items = append(items, "If behavior depends on flags or subcommands, inspect the arg parsing branch before following deep callees.")
	}
	items = append(items, "Use `--depth` to widen the call path if the current summary stops too early.")
	return uniqueStrings(items)
}

func buildTravelFileReadFirst(resolution travelResolution, view handoffFileView) []string {
	items := []string{
		fmt.Sprintf("Start with entry file %s from the launch recipe.", resolution.FilePath),
	}
	if len(view.SymbolLines) > 0 {
		items = append(items, "Then inspect the first symbol in that file as the most likely launch anchor.")
	}
	if len(view.Tests) > 0 {
		test := view.Tests[0]
		items = append(items, fmt.Sprintf("Read test %s at %s:%d before changing launch behavior.", test.Name, test.FilePath, test.Line))
	}
	return uniqueStrings(items)
}

func buildTravelFileChecklist(resolution travelResolution, view handoffFileView) []string {
	items := []string{
		"The inferred entry file had no clear entry function, so verify the actual launch handoff manually.",
	}
	if len(resolution.Candidates) > 1 {
		items = append(items, "Several entry files matched the run recipe, so confirm which one is real before editing.")
	}
	if len(view.Tests) == 0 {
		items = append(items, "Test coverage near the inferred launch file is thin, so plan a manual verification pass.")
	}
	return uniqueStrings(items)
}

func inferTravelFileCandidates(root, language string, store *storage.Store, summaries map[string]storage.FileSummary, tokens []string) ([]storage.FileSummary, []string, error) {
	command := filepath.Base(tokens[0])
	lowered := strings.ToLower(command)
	notes := make([]string, 0, 4)
	queries := make([]string, 0, 4)

	switch {
	case lowered == "go" && len(tokens) > 1 && tokens[1] == "run":
		queries = collectGoRunTargets(tokens[2:])
		if len(queries) == 0 {
			queries = append(queries, ".")
		}
		notes = append(notes, "using `go run` targets as launch hints")
	case (strings.HasPrefix(lowered, "python") || lowered == "py") && len(tokens) > 1:
		queries = collectPythonTargets(tokens[1:])
		if len(queries) == 0 {
			queries = append(queries, ".")
		}
		notes = append(notes, "using Python script/module hints from the run recipe")
	case lowered == "cargo" && len(tokens) > 1 && tokens[1] == "run":
		queries = collectCargoRunTargets(tokens[2:])
		if len(queries) == 0 {
			queries = append(queries, "src/main.rs")
		}
		notes = append(notes, "using cargo binary target hints")
	default:
		queries = collectGenericTravelTargets(tokens, language)
		if len(queries) == 0 {
			queries = append(queries, ".")
		}
		notes = append(notes, "using generic path hints from the run recipe")
	}

	byPath := make(map[string]storage.FileSummary)
	for _, query := range queries {
		for _, summary := range matchTravelFiles(root, language, store, summaries, query) {
			byPath[summary.FilePath] = summary
		}
	}
	if len(byPath) == 0 {
		for _, summary := range topTravelEntrypointFiles(language, summaries, 4) {
			byPath[summary.FilePath] = summary
		}
		if len(byPath) > 0 {
			notes = append(notes, "no exact run target matched, so falling back to top-ranked entrypoint files")
		}
	}

	candidates := make([]storage.FileSummary, 0, len(byPath))
	for _, summary := range byPath {
		candidates = append(candidates, summary)
	}
	sortTravelFileCandidates(candidates, queries, language)
	return candidates, notes, nil
}

func resolveTravelEntrySymbol(modulePath string, store *storage.Store, filePath string) (storage.SymbolMatch, []string, bool, error) {
	symbols, err := store.LoadFileSymbols(filePath)
	if err != nil {
		return storage.SymbolMatch{}, nil, false, err
	}
	if len(symbols) == 0 {
		return storage.SymbolMatch{}, []string{fmt.Sprintf("file %s has no indexed symbols, so travel stays file-scoped.", filePath)}, false, nil
	}

	type candidate struct {
		symbol storage.SymbolMatch
		score  int
		notes  []string
	}
	candidates := make([]candidate, 0, len(symbols))
	for _, symbol := range symbols {
		view, err := store.LoadSymbolView(symbol.SymbolKey)
		if err != nil {
			return storage.SymbolMatch{}, nil, false, err
		}
		score := view.QualityScore + len(view.Callees)*3 + len(view.ReferencesIn)*2 + len(view.Tests)*4
		notes := make([]string, 0, 5)
		if symbol.Kind == "func" || symbol.Kind == "method" {
			score += 12
			notes = append(notes, "callable symbol")
		}
		if strings.EqualFold(symbol.Name, "main") && symbol.Kind == "func" {
			score += 100
			notes = append(notes, "main entry symbol")
		}
		if travelLaunchVerb(symbol.Name) {
			score += 18
			notes = append(notes, "launch-style name")
		}
		if symbol.Line > 0 && symbol.Line <= 40 {
			score += 6
			notes = append(notes, "declared near the top of the file")
		}
		if len(view.Callees) > 0 {
			notes = append(notes, fmt.Sprintf("%d direct callees", len(view.Callees)))
		}
		candidates = append(candidates, candidate{symbol: symbol, score: score, notes: notes})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		if candidates[i].symbol.Line != candidates[j].symbol.Line {
			return candidates[i].symbol.Line < candidates[j].symbol.Line
		}
		return candidates[i].symbol.QName < candidates[j].symbol.QName
	})

	chosen := candidates[0]
	notes := []string{
		fmt.Sprintf("entry symbol selected as %s.", shortenQName(modulePath, chosen.symbol.QName)),
	}
	if len(chosen.notes) > 0 {
		notes = append(notes, "selection signals: "+strings.Join(chosen.notes, "; "))
	}
	if len(candidates) > 1 {
		others := make([]string, 0, min(3, len(candidates)-1))
		for _, item := range candidates[1:min(len(candidates), 4)] {
			others = append(others, shortenQName(modulePath, item.symbol.QName))
		}
		if len(others) > 0 {
			notes = append(notes, "other nearby candidates: "+strings.Join(others, ", "))
		}
	}
	return chosen.symbol, notes, true, nil
}

func stripTravelWrappers(tokens []string) ([]string, []string) {
	trimmed := append([]string(nil), tokens...)
	notes := make([]string, 0, 4)
	for len(trimmed) > 0 {
		switch {
		case trimmed[0] == "env":
			trimmed = trimmed[1:]
			for len(trimmed) > 0 && looksLikeEnvAssignment(trimmed[0]) {
				notes = append(notes, "ignored environment override "+trimmed[0])
				trimmed = trimmed[1:]
			}
		case looksLikeEnvAssignment(trimmed[0]):
			notes = append(notes, "ignored environment override "+trimmed[0])
			trimmed = trimmed[1:]
		case len(trimmed) >= 2 && (trimmed[0] == "uv" || trimmed[0] == "poetry" || trimmed[0] == "pipenv") && trimmed[1] == "run":
			notes = append(notes, "ignored wrapper "+trimmed[0]+" run")
			trimmed = trimmed[2:]
		default:
			return trimmed, notes
		}
	}
	return trimmed, notes
}

func looksLikeEnvAssignment(value string) bool {
	if strings.HasPrefix(value, "-") {
		return false
	}
	idx := strings.IndexByte(value, '=')
	if idx <= 0 {
		return false
	}
	key := value[:idx]
	return !strings.ContainsAny(key, `/\`)
}

func travelLauncherLabel(tokens []string) string {
	if len(tokens) >= 2 {
		head := filepath.Base(tokens[0])
		switch {
		case head == "go" && tokens[1] == "run":
			return "go run"
		case head == "cargo" && tokens[1] == "run":
			return "cargo run"
		case strings.HasPrefix(head, "python") && tokens[1] == "-m":
			return head + " -m"
		}
	}
	return filepath.Base(tokens[0])
}

func collectGoRunTargets(tokens []string) []string {
	targets := make([]string, 0, 2)
	for idx := 0; idx < len(tokens); idx++ {
		token := tokens[idx]
		if token == "--" {
			break
		}
		if strings.HasPrefix(token, "-") {
			if goRunFlagNeedsValue(token) && idx+1 < len(tokens) {
				idx++
			}
			continue
		}
		targets = append(targets, token)
	}
	return targets
}

func goRunFlagNeedsValue(token string) bool {
	if strings.Contains(token, "=") {
		return false
	}
	switch token {
	case "-C", "-exec", "-modfile", "-overlay", "-pgo", "-tags", "-ldflags", "-gcflags", "-asmflags", "-buildmode", "-compiler":
		return true
	default:
		return false
	}
}

func collectPythonTargets(tokens []string) []string {
	if len(tokens) == 0 {
		return nil
	}
	if tokens[0] == "-m" && len(tokens) > 1 {
		module := strings.TrimSpace(tokens[1])
		if module == "" {
			return nil
		}
		modPath := strings.ReplaceAll(module, ".", "/")
		return []string{
			modPath + ".py",
			path.Join(modPath, "__main__.py"),
			path.Join("src", modPath+".py"),
			path.Join("src", modPath, "__main__.py"),
		}
	}
	for _, token := range tokens {
		if strings.HasPrefix(token, "-") {
			continue
		}
		return []string{token}
	}
	return nil
}

func collectCargoRunTargets(tokens []string) []string {
	targets := make([]string, 0, 3)
	for idx := 0; idx < len(tokens); idx++ {
		token := tokens[idx]
		switch token {
		case "--bin":
			if idx+1 < len(tokens) {
				name := strings.TrimSpace(tokens[idx+1])
				targets = append(targets, path.Join("src", "bin", name+".rs"))
				targets = append(targets, path.Join("src", "bin", name, "main.rs"))
				idx++
			}
		case "--example":
			if idx+1 < len(tokens) {
				name := strings.TrimSpace(tokens[idx+1])
				targets = append(targets, path.Join("examples", name+".rs"))
				idx++
			}
		}
	}
	if len(targets) == 0 {
		targets = append(targets, "src/main.rs")
	}
	return targets
}

func collectGenericTravelTargets(tokens []string, language string) []string {
	targets := make([]string, 0, 4)
	for _, token := range tokens {
		if strings.HasPrefix(token, "-") {
			continue
		}
		if strings.HasSuffix(token, ".go") || strings.HasSuffix(token, ".py") || strings.HasSuffix(token, ".rs") {
			targets = append(targets, token)
			continue
		}
		if strings.Contains(token, "/") || token == "." {
			targets = append(targets, token)
		}
	}
	if len(targets) == 0 {
		switch language {
		case "python":
			targets = append(targets, "__main__.py", "main.py")
		case "rust":
			targets = append(targets, "src/main.rs")
		default:
			targets = append(targets, "main.go")
		}
	}
	return uniqueStrings(targets)
}

func matchTravelFiles(root, language string, store *storage.Store, summaries map[string]storage.FileSummary, query string) []storage.FileSummary {
	query = normalizeTravelQuery(root, query)
	candidates := make([]storage.FileSummary, 0, 8)
	seen := make(map[string]struct{})
	appendSummary := func(summary storage.FileSummary) {
		if summary.FilePath == "" {
			return
		}
		if _, ok := seen[summary.FilePath]; ok {
			return
		}
		seen[summary.FilePath] = struct{}{}
		candidates = append(candidates, summary)
	}

	if query == "" || query == "." {
		for _, summary := range topTravelEntrypointFiles(language, summaries, 6) {
			appendSummary(summary)
		}
		return candidates
	}
	if summary, ok := summaries[query]; ok {
		appendSummary(summary)
		return candidates
	}

	if strings.Contains(query, "...") {
		parts := strings.SplitN(query, "...", 2)
		prefix := strings.TrimSuffix(parts[0], "/")
		suffix := strings.TrimPrefix(parts[1], "/")
		for _, summary := range summaries {
			if prefix != "" && !strings.HasPrefix(summary.FilePath, prefix) {
				continue
			}
			if suffix != "" && !strings.HasSuffix(summary.FilePath, suffix) {
				continue
			}
			appendSummary(summary)
		}
		sortTravelFileCandidates(candidates, []string{query}, language)
		return candidates
	}

	if packages, err := store.FindPackages(query); err == nil {
		for _, pkg := range packages {
			if pkg.ImportPath != query && pkg.DirPath != query {
				continue
			}
			dir := strings.TrimPrefix(filepath.ToSlash(pkg.DirPath), "./")
			for _, summary := range summaries {
				if summary.FilePath == dir || strings.HasPrefix(summary.FilePath, dir+"/") {
					appendSummary(summary)
				}
			}
		}
	}

	for _, summary := range summaries {
		switch {
		case summary.FilePath == query:
			appendSummary(summary)
		case strings.HasSuffix(summary.FilePath, "/"+query):
			appendSummary(summary)
		case strings.HasPrefix(summary.FilePath, query+"/"):
			appendSummary(summary)
		case filepath.Base(summary.FilePath) == filepath.Base(query):
			appendSummary(summary)
		}
	}

	if len(candidates) == 0 {
		defaults := defaultTravelFilesForQuery(query, language)
		for _, item := range defaults {
			if summary, ok := summaries[item]; ok {
				appendSummary(summary)
			}
		}
	}

	sortTravelFileCandidates(candidates, []string{query}, language)
	return candidates
}

func defaultTravelFilesForQuery(query, language string) []string {
	query = strings.TrimSuffix(query, "/")
	switch language {
	case "python":
		return []string{
			path.Join(query, "__main__.py"),
			path.Join(query, "main.py"),
		}
	case "rust":
		return []string{
			path.Join(query, "src", "main.rs"),
			path.Join(query, "main.rs"),
		}
	default:
		return []string{
			path.Join(query, "main.go"),
			path.Join(query, "cmd", "main.go"),
		}
	}
}

func topTravelEntrypointFiles(language string, summaries map[string]storage.FileSummary, limit int) []storage.FileSummary {
	values := make([]storage.FileSummary, 0, len(summaries))
	for _, summary := range summaries {
		if summary.IsTest {
			continue
		}
		if summary.IsEntrypoint {
			values = append(values, summary)
			continue
		}
		switch language {
		case "python":
			if strings.HasSuffix(summary.FilePath, "/__main__.py") || summary.FilePath == "__main__.py" || strings.HasSuffix(summary.FilePath, "/main.py") || summary.FilePath == "main.py" {
				values = append(values, summary)
			}
		case "rust":
			if summary.FilePath == "src/main.rs" || strings.HasSuffix(summary.FilePath, "/main.rs") {
				values = append(values, summary)
			}
		default:
			if strings.HasSuffix(summary.FilePath, "/main.go") || summary.FilePath == "main.go" {
				values = append(values, summary)
			}
		}
	}
	sortTravelFileCandidates(values, nil, language)
	if limit > 0 && len(values) > limit {
		values = values[:limit]
	}
	return values
}

func sortTravelFileCandidates(values []storage.FileSummary, queries []string, language string) {
	sort.Slice(values, func(i, j int) bool {
		leftScore := travelFileMatchScore(values[i], queries, language)
		rightScore := travelFileMatchScore(values[j], queries, language)
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		if values[i].QualityScore != values[j].QualityScore {
			return values[i].QualityScore > values[j].QualityScore
		}
		return values[i].FilePath < values[j].FilePath
	})
}

func travelFileMatchScore(summary storage.FileSummary, queries []string, language string) int {
	score := summary.QualityScore
	if summary.IsEntrypoint {
		score += 24
	}
	switch language {
	case "python":
		if strings.HasSuffix(summary.FilePath, "/__main__.py") || summary.FilePath == "__main__.py" {
			score += 12
		}
	case "rust":
		if summary.FilePath == "src/main.rs" || strings.HasSuffix(summary.FilePath, "/main.rs") {
			score += 12
		}
	default:
		if strings.HasSuffix(summary.FilePath, "/main.go") || summary.FilePath == "main.go" {
			score += 12
		}
	}

	for _, raw := range queries {
		query := strings.TrimSuffix(normalizeTravelQuery("", raw), "/")
		if query == "" || query == "." {
			continue
		}
		switch {
		case summary.FilePath == query:
			score += 64
		case strings.HasSuffix(summary.FilePath, "/"+query):
			score += 40
		case strings.HasPrefix(summary.FilePath, query+"/"):
			score += 24
		case filepath.Base(summary.FilePath) == filepath.Base(query):
			score += 12
		}
	}
	return score
}

func normalizeTravelQuery(root, query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return ""
	}
	if root != "" && filepath.IsAbs(query) {
		if rel, err := filepath.Rel(root, query); err == nil {
			query = rel
		}
	}
	query = filepath.ToSlash(filepath.Clean(query))
	query = strings.TrimPrefix(query, "./")
	if query == "." {
		return "."
	}
	return query
}

func splitTravelRecipe(value string) ([]string, error) {
	var tokens []string
	var current strings.Builder
	var quote rune
	escaped := false

	flush := func() {
		if current.Len() == 0 {
			return
		}
		tokens = append(tokens, current.String())
		current.Reset()
	}

	for _, r := range value {
		switch {
		case escaped:
			current.WriteRune(r)
			escaped = false
		case r == '\\':
			escaped = true
		case quote != 0:
			if r == quote {
				quote = 0
				continue
			}
			current.WriteRune(r)
		case r == '\'' || r == '"':
			quote = r
		case r == ' ' || r == '\t' || r == '\n':
			flush()
		default:
			current.WriteRune(r)
		}
	}
	if escaped {
		return nil, errors.New("travel run recipe ends with a dangling escape")
	}
	if quote != 0 {
		return nil, errors.New("travel run recipe has an unclosed quote")
	}
	flush()
	return tokens, nil
}

func entryTravelLabel(modulePath string, resolution travelResolution) string {
	if resolution.Symbol.SymbolKey != "" {
		return shortenQName(modulePath, resolution.Symbol.QName)
	}
	return resolution.FilePath
}

func travelLaunchVerb(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "run", "start", "execute", "exec", "serve", "handle", "bootstrap", "dispatch", "main":
		return true
	default:
		return false
	}
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func renderAIRankedSymbols(stdout io.Writer, modulePath, key string, values []storage.RankedSymbol, explain bool) error {
	if _, err := fmt.Fprintf(stdout, "%s=%d\n", key, len(values)); err != nil {
		return err
	}
	for _, value := range values {
		if _, err := fmt.Fprintf(
			stdout,
			"%s_item=%q kind=%s file=%s:%d callers=%d callees=%d refs=%d tests=%d score=%d\n",
			key,
			shortenQName(modulePath, value.Symbol.QName),
			value.Symbol.Kind,
			value.Symbol.FilePath,
			value.Symbol.Line,
			value.CallerCount,
			value.CalleeCount,
			value.ReferenceCount,
			value.TestCount,
			value.Score,
		); err != nil {
			return err
		}
		if explain {
			for _, why := range value.QualityWhy {
				if _, err := fmt.Fprintf(stdout, "%s_why=%q\n", key, why); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func measureTravelRun(root string, resolution travelResolution, timeout time.Duration, skip bool) travelRunMetrics {
	metrics := travelRunMetrics{
		Skipped:  skip,
		ExitCode: -1,
		Timeout:  timeout,
	}
	if skip {
		metrics.Status = "skipped"
		return metrics
	}
	metrics.Attempted = true

	tokens, err := splitTravelRecipe(resolution.Recipe)
	if err != nil {
		metrics.Status = "invalid recipe"
		metrics.Error = err.Error()
		return metrics
	}
	name, args, env, err := buildTravelExec(tokens, resolution.RunArgs)
	if err != nil {
		metrics.Status = "invalid recipe"
		metrics.Error = err.Error()
		return metrics
	}

	stderr := &limitedBuffer{Limit: 4096}
	var cmd *exec.Cmd
	started := time.Now()
	if timeout > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		cmd = exec.CommandContext(ctx, name, args...)
		cmd.Dir = root
		cmd.Stdout = io.Discard
		cmd.Stderr = stderr
		if len(env) > 0 {
			cmd.Env = append(os.Environ(), env...)
		}
		err = cmd.Run()
		metrics.Wall = time.Since(started)
		metrics.Stderr = stderr.String()
		populateTravelRuntimeMetrics(&metrics, cmd.ProcessState)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			metrics.TimedOut = true
			metrics.Status = "timed out"
			if metrics.Error == "" {
				metrics.Error = fmt.Sprintf("run exceeded timeout %s", formatTravelDuration(timeout))
			}
			return metrics
		}
	} else {
		cmd = exec.Command(name, args...)
		cmd.Dir = root
		cmd.Stdout = io.Discard
		cmd.Stderr = stderr
		if len(env) > 0 {
			cmd.Env = append(os.Environ(), env...)
		}
		err = cmd.Run()
		metrics.Wall = time.Since(started)
		metrics.Stderr = stderr.String()
		populateTravelRuntimeMetrics(&metrics, cmd.ProcessState)
	}

	var exitErr *exec.ExitError
	switch {
	case err == nil:
		metrics.Status = "ok"
		if metrics.ExitCode < 0 {
			metrics.ExitCode = 0
		}
	case errors.As(err, &exitErr):
		metrics.Status = fmt.Sprintf("exit %d", exitErr.ExitCode())
		metrics.Error = err.Error()
		if metrics.ExitCode < 0 {
			metrics.ExitCode = exitErr.ExitCode()
		}
	default:
		metrics.Status = "start failed"
		metrics.Error = err.Error()
	}
	return metrics
}

func buildTravelExec(tokens, runArgs []string) (string, []string, []string, error) {
	if len(tokens) == 0 {
		return "", nil, nil, errors.New("empty run recipe")
	}
	env := make([]string, 0)
	current := append([]string(nil), tokens...)

	if current[0] == "env" {
		idx := 1
		for idx < len(current) && looksLikeEnvAssignment(current[idx]) {
			env = append(env, current[idx])
			idx++
		}
		if idx < len(current) && !strings.HasPrefix(current[idx], "-") {
			current = current[idx:]
		}
	}
	for len(current) > 0 && looksLikeEnvAssignment(current[0]) {
		env = append(env, current[0])
		current = current[1:]
	}
	if len(current) == 0 {
		return "", nil, nil, errors.New("run recipe does not contain an executable")
	}
	return current[0], append(append([]string(nil), current[1:]...), runArgs...), env, nil
}

func populateTravelRuntimeMetrics(metrics *travelRunMetrics, state *os.ProcessState) {
	if metrics == nil || state == nil {
		return
	}
	metrics.UserCPU = state.UserTime()
	metrics.SystemCPU = state.SystemTime()
	metrics.MaxRSSBytes = travelPeakRSSBytes(state)
	if exitCode := state.ExitCode(); exitCode >= 0 {
		metrics.ExitCode = exitCode
	}
}

func renderHumanTravelRuntime(stdout io.Writer, p palette, metrics travelRunMetrics) error {
	rows := buildTravelRuntimeRows(metrics)
	if err := renderHumanTravelTable(stdout, p, "Performance", "Metric", "Value", "Notes", rows); err != nil {
		return err
	}
	if strings.TrimSpace(metrics.Error) != "" {
		if _, err := fmt.Fprintf(stdout, "  %s %s\n", p.label("Run note:"), oneLine(metrics.Error)); err != nil {
			return err
		}
	}
	if strings.TrimSpace(metrics.Stderr) != "" && (metrics.ExitCode != 0 || metrics.TimedOut) {
		if _, err := fmt.Fprintf(stdout, "  %s %s\n", p.label("stderr excerpt:"), oneLine(metrics.Stderr)); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(stdout)
	return err
}

func renderAITravelRuntime(stdout io.Writer, metrics travelRunMetrics) error {
	if _, err := fmt.Fprintf(stdout, "run_attempted=%t\nrun_skipped=%t\nrun_timed_out=%t\nrun_status=%q\nrun_exit_code=%d\nrun_wall_ms=%d\nrun_cpu_user_ms=%d\nrun_cpu_system_ms=%d\nrun_cpu_total_ms=%d\nrun_peak_rss_bytes=%d\nrun_timeout_ms=%d\n",
		metrics.Attempted,
		metrics.Skipped,
		metrics.TimedOut,
		metrics.Status,
		metrics.ExitCode,
		metrics.Wall.Milliseconds(),
		metrics.UserCPU.Milliseconds(),
		metrics.SystemCPU.Milliseconds(),
		(metrics.UserCPU + metrics.SystemCPU).Milliseconds(),
		metrics.MaxRSSBytes,
		metrics.Timeout.Milliseconds(),
	); err != nil {
		return err
	}
	if strings.TrimSpace(metrics.Error) != "" {
		if _, err := fmt.Fprintf(stdout, "run_error=%q\n", oneLine(metrics.Error)); err != nil {
			return err
		}
	}
	if strings.TrimSpace(metrics.Stderr) != "" {
		if _, err := fmt.Fprintf(stdout, "run_stderr=%q\n", oneLine(metrics.Stderr)); err != nil {
			return err
		}
	}
	return nil
}

func travelStatusLabel(metrics travelRunMetrics) string {
	status := strings.TrimSpace(metrics.Status)
	if status == "" {
		status = "unknown"
	}
	switch {
	case metrics.Status == "ok":
		return fmt.Sprintf("ok (exit %d)", max(metrics.ExitCode, 0))
	case metrics.TimedOut && metrics.Timeout > 0:
		return fmt.Sprintf("%s after %s", status, formatTravelDuration(metrics.Timeout))
	case metrics.ExitCode >= 0:
		return fmt.Sprintf("%s (exit %d)", status, metrics.ExitCode)
	default:
		return status
	}
}

func formatTravelDuration(value time.Duration) string {
	switch {
	case value <= 0:
		return "0ms"
	case value < time.Millisecond:
		return fmt.Sprintf("%dµs", value.Microseconds())
	case value < time.Second:
		return fmt.Sprintf("%.1fms", float64(value)/float64(time.Millisecond))
	default:
		return fmt.Sprintf("%.3fs", float64(value)/float64(time.Second))
	}
}

func travelDurationMillis(value time.Duration) int {
	if value <= 0 {
		return 0
	}
	return int(value / time.Millisecond)
}

func buildStoredTravelRecord(command cli.Command, view travelView) storage.TravelRunRecord {
	return storage.TravelRunRecord{
		TravelRunSummary: storage.TravelRunSummary{
			Recipe:           view.Resolution.Recipe,
			Launcher:         view.Resolution.Launcher,
			RunArgs:          append([]string(nil), view.Resolution.RunArgs...),
			EntryFile:        view.Resolution.FilePath,
			EntrySymbolQName: view.Resolution.Symbol.QName,
			Depth:            command.Depth,
			Limit:            command.Limit,
			RunStatus:        view.Runtime.Status,
			RunExitCode:      view.Runtime.ExitCode,
			RunWallMs:        travelDurationMillis(view.Runtime.Wall),
			RunPeakRSSBytes:  view.Runtime.MaxRSSBytes,
		},
		Explain:        command.Explain,
		TimeoutMs:      travelDurationMillis(command.TravelTimeout),
		NoRun:          command.TravelNoRun,
		RunAttempted:   view.Runtime.Attempted,
		RunTimedOut:    view.Runtime.TimedOut,
		RunCPUUserMs:   travelDurationMillis(view.Runtime.UserCPU),
		RunCPUSystemMs: travelDurationMillis(view.Runtime.SystemCPU),
	}
}

func runStoredTravel(command cli.Command, stdout io.Writer) error {
	state, err := openProjectState(command.Root)
	if err != nil {
		return err
	}
	defer state.Close()

	switch command.Scope {
	case "show-all":
		runs, err := state.Store.ListTravelRuns()
		if err != nil {
			return err
		}
		switch command.OutputMode {
		case cli.OutputAI:
			return renderAITravelRuns(stdout, state.Info.Root, state.Info.ModulePath, runs)
		default:
			return renderHumanTravelRuns(stdout, state.Info.Root, state.Info.ModulePath, runs)
		}
	case "show-one":
		run, err := state.Store.TravelRunByID(command.TravelRunID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				_, writeErr := fmt.Fprintf(stdout, "No saved travel run with id %d\n", command.TravelRunID)
				return writeErr
			}
			return err
		}
		switch command.OutputMode {
		case cli.OutputAI:
			if strings.TrimSpace(run.AIOutput) == "" {
				return fmt.Errorf("saved ai output for travel run %d is empty", run.ID)
			}
			_, err = io.WriteString(stdout, run.AIOutput)
		default:
			if strings.TrimSpace(run.HumanOutput) == "" {
				return fmt.Errorf("saved human output for travel run %d is empty", run.ID)
			}
			_, err = io.WriteString(stdout, run.HumanOutput)
		}
		return err
	default:
		return fmt.Errorf("unsupported travel scope %q", command.Scope)
	}
}

func renderHumanTravelRuns(stdout io.Writer, root, modulePath string, runs []storage.TravelRunSummary) error {
	p := newPalette()
	if _, err := fmt.Fprintf(stdout, "%s\n%s\n\n", p.rule("Travel Runs"), p.title("CTX Travel Runs")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "%s %s\n%s %s\n\n", p.label("Root:"), root, p.label("Module:"), modulePath); err != nil {
		return err
	}
	if len(runs) == 0 {
		_, err := fmt.Fprintf(stdout, "No saved travel runs for %s. Run `ctx travel ...` first.\n", modulePath)
		return err
	}

	rows := make([]travelTableRow, 0, len(runs))
	for _, run := range runs {
		rows = append(rows, travelTableRow{
			Left:  fmt.Sprintf("#%d", run.ID),
			Value: travelSummaryTarget(modulePath, run),
			Note:  fmt.Sprintf("%s | %s | %s", run.CreatedAt.Format(timeFormat), travelSummaryStatus(run), oneLine(run.Recipe)),
		})
	}
	if err := renderHumanTravelTable(stdout, p, "Saved Runs", "ID", "Entry", "Saved / Status / Recipe", rows); err != nil {
		return err
	}
	_, err := fmt.Fprintf(stdout, "%s use `ctx travel show <id>` to reopen a saved run.\n", p.label("Next step:"))
	return err
}

func renderAITravelRuns(stdout io.Writer, root, modulePath string, runs []storage.TravelRunSummary) error {
	if _, err := fmt.Fprintf(stdout, "root=%s\nmodule=%s\ntravel_runs=%d\n", root, modulePath, len(runs)); err != nil {
		return err
	}
	for _, run := range runs {
		if _, err := fmt.Fprintf(
			stdout,
			"travel_run=%d created_at=%s entry=%q status=%q exit_code=%d wall_ms=%d rss_bytes=%d recipe=%q\n",
			run.ID,
			run.CreatedAt.Format(timeFormat),
			travelSummaryTarget(modulePath, run),
			travelSummaryStatus(run),
			run.RunExitCode,
			run.RunWallMs,
			run.RunPeakRSSBytes,
			run.Recipe,
		); err != nil {
			return err
		}
	}
	return nil
}

func travelStoredIDValue(id int64) string {
	if id <= 0 {
		return "pending"
	}
	return fmt.Sprintf("%d", id)
}

func travelStoredAtValue(value time.Time) string {
	if value.IsZero() {
		return "pending"
	}
	return value.Format(timeFormat)
}

func travelSummaryTarget(modulePath string, run storage.TravelRunSummary) string {
	if strings.TrimSpace(run.EntrySymbolQName) != "" {
		return shortenQName(modulePath, run.EntrySymbolQName)
	}
	if strings.TrimSpace(run.EntryFile) != "" {
		return run.EntryFile
	}
	return "unknown"
}

func travelSummaryStatus(run storage.TravelRunSummary) string {
	status := strings.TrimSpace(run.RunStatus)
	if status == "" {
		status = "unknown"
	}
	switch {
	case run.RunExitCode >= 0:
		return fmt.Sprintf("%s (exit %d, wall=%s)", status, run.RunExitCode, formatTravelDuration(time.Duration(run.RunWallMs)*time.Millisecond))
	case run.RunWallMs > 0:
		return fmt.Sprintf("%s (wall=%s)", status, formatTravelDuration(time.Duration(run.RunWallMs)*time.Millisecond))
	default:
		return status
	}
}

func renderHumanTravelOverview(stdout io.Writer, p palette, view travelView, target, argsLabel string, depth int) error {
	entryNote := "inferred static launch anchor"
	if len(view.Resolution.Candidates) > 1 {
		entryNote = fmt.Sprintf("picked from %d plausible entry files", len(view.Resolution.Candidates))
	}
	rows := []travelTableRow{
		{Left: "Travel ID", Value: travelStoredIDValue(view.StoredRunID), Note: "saved run identifier for `ctx travel show <id>`"},
		{Left: "Saved at", Value: travelStoredAtValue(view.StoredAt), Note: "timestamp recorded in the project database"},
		{Left: "Run recipe", Value: view.Resolution.Recipe, Note: "full command before forwarded args"},
		{Left: "Launcher", Value: view.Resolution.Launcher, Note: "wrapper or executable family"},
		{Left: "Entry target", Value: target, Note: entryNote},
		{Left: "Runtime args", Value: argsLabel, Note: "arguments passed after the recipe"},
		{Left: "Depth", Value: fmt.Sprintf("%d", depth), Note: "max static callee hops to follow"},
		{Left: "Language", Value: displayLanguage(view.Resolution.Language), Note: "resolved from the indexed project"},
	}
	return renderHumanTravelTable(stdout, p, "Launch Overview", "Field", "Value", "Reading Hint", rows)
}

func buildTravelRuntimeRows(metrics travelRunMetrics) []travelTableRow {
	switch {
	case metrics.Skipped:
		return []travelTableRow{
			{Left: "Status", Value: "skipped", Note: "execution disabled with `--no-run`"},
		}
	case !metrics.Attempted:
		return []travelTableRow{
			{Left: "Status", Value: "not attempted", Note: "no runtime measurement was collected"},
		}
	}

	rows := []travelTableRow{
		{Left: "Status", Value: travelStatusLabel(metrics), Note: buildTravelStatusNote(metrics)},
		{Left: "Wall time", Value: formatTravelDuration(metrics.Wall), Note: "elapsed end-to-end runtime"},
		{
			Left:  "CPU total",
			Value: formatTravelDuration(metrics.UserCPU + metrics.SystemCPU),
			Note:  fmt.Sprintf("user=%s system=%s", formatTravelDuration(metrics.UserCPU), formatTravelDuration(metrics.SystemCPU)),
		},
	}
	if metrics.MaxRSSBytes > 0 {
		rows = append(rows, travelTableRow{Left: "Peak RSS", Value: shellHumanSize(metrics.MaxRSSBytes), Note: "maximum resident memory seen by the process"})
	} else {
		rows = append(rows, travelTableRow{Left: "Peak RSS", Value: "n/a", Note: "platform metric unavailable"})
	}
	if metrics.Timeout > 0 {
		rows = append(rows, travelTableRow{Left: "Timeout cap", Value: formatTravelDuration(metrics.Timeout), Note: "measurement stops here if the process keeps running"})
	}
	return rows
}

func buildTravelStatusNote(metrics travelRunMetrics) string {
	switch {
	case metrics.TimedOut:
		return "process hit the measurement timeout before exiting"
	case metrics.Status == "ok":
		return "command completed successfully within the measurement window"
	case metrics.ExitCode > 0:
		return "command exited non-zero; see note/stderr below if needed"
	default:
		return "runtime measurement captured process exit details"
	}
}

func renderHumanTravelTable(stdout io.Writer, p palette, title, leftHeader, valueHeader, noteHeader string, rows []travelTableRow) error {
	if _, err := fmt.Fprintf(stdout, "%s\n", p.section(title)); err != nil {
		return err
	}
	if len(rows) == 0 {
		return renderHumanEmpty(stdout, p)
	}

	leftWidth := max(len(leftHeader), 5)
	valueWidth := max(len(valueHeader), 5)
	noteWidth := max(len(noteHeader), 5)
	for _, row := range rows {
		leftWidth = max(leftWidth, len(row.Left))
		valueWidth = max(valueWidth, len(row.Value))
		noteWidth = max(noteWidth, len(row.Note))
	}

	border := fmt.Sprintf("  +-%s-+--%s-+--%s-+\n", strings.Repeat("-", leftWidth), strings.Repeat("-", valueWidth), strings.Repeat("-", noteWidth))
	if _, err := fmt.Fprint(stdout, border); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "  | %-*s | %-*s | %-*s |\n", leftWidth, leftHeader, valueWidth, valueHeader, noteWidth, noteHeader); err != nil {
		return err
	}
	if _, err := fmt.Fprint(stdout, border); err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := fmt.Fprintf(stdout, "  | %-*s | %-*s | %-*s |\n", leftWidth, row.Left, valueWidth, row.Value, noteWidth, row.Note); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprint(stdout, border); err != nil {
		return err
	}
	_, err := fmt.Fprintln(stdout)
	return err
}
