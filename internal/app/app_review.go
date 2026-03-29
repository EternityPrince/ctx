package app

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/cli"
	"github.com/vladimirkasterin/ctx/internal/codebase"
	"github.com/vladimirkasterin/ctx/internal/core"
	"github.com/vladimirkasterin/ctx/internal/storage"
)

type workingTreeReviewView struct {
	Status        storage.ProjectStatus
	Plan          codebase.ChangePlan
	ChangedPaths  []string
	ManifestLines []string
	Tests         []storage.TestView
	Checklist     []string
}

type snapshotReviewView struct {
	Status    storage.ProjectStatus
	Diff      storage.DiffView
	Tests     []storage.TestView
	Checklist []string
}

func runReview(command cli.Command, stdout io.Writer) error {
	switch command.Scope {
	case "snapshot":
		return runSnapshotReview(command, stdout)
	default:
		return runWorkingTreeReview(command, stdout)
	}
}

func runWorkingTreeReview(command cli.Command, stdout io.Writer) error {
	state, err := openProjectState(command.Root)
	if err != nil {
		return err
	}
	defer state.Close()

	status, ok, err := ensureIndexedSnapshot(stdout, state)
	if err != nil || !ok {
		return err
	}

	view, err := buildWorkingTreeReviewView(state, status, command.Limit)
	if err != nil {
		return err
	}

	switch command.OutputMode {
	case cli.OutputAI:
		return renderAIWorkingTreeReview(stdout, state.Info.ModulePath, view, command.Explain)
	default:
		return renderHumanWorkingTreeReview(stdout, state.Info.Root, state.Info.ModulePath, view, command.Explain)
	}
}

func runSnapshotReview(command cli.Command, stdout io.Writer) error {
	state, err := openProjectState(command.Root)
	if err != nil {
		return err
	}
	defer state.Close()

	status, ok, err := ensureIndexedSnapshot(stdout, state)
	if err != nil || !ok {
		return err
	}

	view, err := buildSnapshotReviewView(state.Store, status, command.FromSnapshot, command.ToSnapshot, command.Limit)
	if err != nil {
		if strings.Contains(err.Error(), "from snapshot is required") {
			_, printErr := fmt.Fprintln(stdout, "Need at least two snapshots for snapshot review. Run `ctx update` after making a change.")
			return printErr
		}
		return err
	}

	switch command.OutputMode {
	case cli.OutputAI:
		return renderAISnapshotReview(stdout, state.Info.ModulePath, view, command.Explain)
	default:
		return renderHumanSnapshotReview(stdout, state.Info.Root, state.Info.ModulePath, view, command.Limit, command.Explain)
	}
}

func buildWorkingTreeReviewView(state core.ProjectState, status storage.ProjectStatus, limit int) (workingTreeReviewView, error) {
	if limit <= 0 {
		limit = 6
	}
	plan := projectService.Plan(state, false)
	summaries, err := state.Store.LoadFileSummaries()
	if err != nil {
		return workingTreeReviewView{}, err
	}
	riskCtx, err := loadWorkflowRiskContext(state.Store)
	if err != nil {
		return workingTreeReviewView{}, err
	}

	changedPaths := buildWorkingTreeChangedPathLines(state, plan, summaries, riskCtx)
	manifestLines := buildManifestReviewLines(plan)
	tests, err := buildPackageReviewTests(state.Store, reviewPackagesFromPlan(state, plan), max(limit+2, 8))
	if err != nil {
		return workingTreeReviewView{}, err
	}

	view := workingTreeReviewView{
		Status:        status,
		Plan:          plan,
		ChangedPaths:  changedPaths,
		ManifestLines: manifestLines,
		Tests:         tests,
	}
	view.Checklist = buildWorkingTreeChecklist(view)
	return view, nil
}

func buildSnapshotReviewView(store *storage.Store, status storage.ProjectStatus, fromID, toID int64, limit int) (snapshotReviewView, error) {
	if limit <= 0 {
		limit = 6
	}
	diff, err := store.Diff(fromID, toID)
	if err != nil {
		return snapshotReviewView{}, err
	}
	packages := make([]string, 0, len(diff.ChangedPackages))
	for _, pkg := range diff.ChangedPackages {
		packages = append(packages, pkg.ImportPath)
	}
	tests, err := buildPackageReviewTests(store, packages, max(limit+2, 8))
	if err != nil {
		return snapshotReviewView{}, err
	}
	view := snapshotReviewView{
		Status: status,
		Diff:   diff,
		Tests:  tests,
	}
	view.Checklist = buildSnapshotChecklist(view)
	return view, nil
}

func renderHumanWorkingTreeReview(stdout io.Writer, projectRoot, modulePath string, view workingTreeReviewView, explain bool) error {
	p := newPalette()
	if _, err := fmt.Fprintf(stdout, "%s\n%s\n\n", p.rule("Review"), p.title("CTX Review")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		stdout,
		"%s\n  %s working tree\n  %s snapshot %d (%s)\n  %s %s\n  %s added=%d changed=%d deleted=%d\n  %s direct=%d expanded=%d reused=%d (%d%%)\n\n",
		p.section("Summary"),
		p.label("Scope:"),
		p.label("Base:"),
		view.Status.Current.ID,
		view.Status.Current.CreatedAt.Format(timeFormat),
		p.label("Strategy:"),
		describeChangePlanStrategy(view.Plan),
		p.label("Changes:"),
		len(view.Plan.Changes.Added),
		len(view.Plan.Changes.Changed),
		len(view.Plan.Changes.Deleted),
		p.label("Package scope:"),
		view.Plan.Metrics.DirectPackageCount,
		view.Plan.Metrics.ExpandedPackageCount,
		view.Plan.Metrics.ReusedPackageCount,
		view.Plan.Metrics.ReusePercent,
	); err != nil {
		return err
	}
	if explain {
		explained, err := buildChangePlanExplainFromView(view.Plan, view.ChangedPaths, view.ManifestLines)
		if err != nil {
			return err
		}
		if err := renderHumanExplainSection(stdout, p, explained); err != nil {
			return err
		}
	}
	if err := renderHumanChecklist(stdout, p, "Changed Paths", view.ChangedPaths, 10); err != nil {
		return err
	}
	if err := renderHumanChecklist(stdout, p, "Manifest Semantics", view.ManifestLines, 8); err != nil {
		return err
	}
	if err := renderHumanStringList(stdout, p, "Directly Impacted Packages", shortenValues(modulePath, view.Plan.ImpactedPackages), 10); err != nil {
		return err
	}
	if err := renderHumanStringList(stdout, p, "Expanded Review Packages", shortenValues(modulePath, view.Plan.ExpandedPackages), 10); err != nil {
		return err
	}
	if err := renderHumanTests(stdout, p, projectRoot, "Tests To Re-Run", view.Tests, 8); err != nil {
		return err
	}
	return renderHumanChecklist(stdout, p, "Review Checklist", view.Checklist, 8)
}

func renderAIWorkingTreeReview(stdout io.Writer, modulePath string, view workingTreeReviewView, explain bool) error {
	if _, err := fmt.Fprintf(stdout, "review scope=working-tree base_snapshot=%d strategy=%s changes=+%d/~%d/-%d direct=%d expanded=%d reused=%d reuse=%d%%\n", view.Status.Current.ID, describeChangePlanStrategy(view.Plan), len(view.Plan.Changes.Added), len(view.Plan.Changes.Changed), len(view.Plan.Changes.Deleted), view.Plan.Metrics.DirectPackageCount, view.Plan.Metrics.ExpandedPackageCount, view.Plan.Metrics.ReusedPackageCount, view.Plan.Metrics.ReusePercent); err != nil {
		return err
	}
	if explain {
		explained, err := buildChangePlanExplainFromView(view.Plan, view.ChangedPaths, view.ManifestLines)
		if err != nil {
			return err
		}
		if err := renderAIExplainSection(stdout, "explain", explained); err != nil {
			return err
		}
	}
	if err := renderAIChecklist(stdout, "changed_paths", view.ChangedPaths, 10); err != nil {
		return err
	}
	if err := renderAIStringList(stdout, "direct_packages", shortenValues(modulePath, view.Plan.ImpactedPackages), 10); err != nil {
		return err
	}
	if err := renderAIStringList(stdout, "expanded_packages", shortenValues(modulePath, view.Plan.ExpandedPackages), 10); err != nil {
		return err
	}
	if err := renderAITests(stdout, "tests", view.Tests, 8); err != nil {
		return err
	}
	return renderAIChecklist(stdout, "review_checklist", view.Checklist, 8)
}

func renderHumanSnapshotReview(stdout io.Writer, projectRoot, modulePath string, view snapshotReviewView, limit int, explain bool) error {
	p := newPalette()
	if _, err := fmt.Fprintf(stdout, "%s\n%s\n\n", p.rule("Review"), p.title("CTX Review")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		stdout,
		"%s\n  %s snapshot window\n  %s %d -> %d\n  %s files +%d ~%d -%d\n  %s symbols +%d -%d ~%d impacted=%d\n  %s packages=%d calls +%d/-%d refs +%d/-%d tests +%d/-%d\n\n",
		p.section("Summary"),
		p.label("Scope:"),
		p.label("Window:"),
		view.Diff.FromSnapshotID,
		view.Diff.ToSnapshotID,
		p.label("Delta:"),
		len(view.Diff.AddedFiles),
		len(view.Diff.ChangedFiles),
		len(view.Diff.DeletedFiles),
		p.label("Surface:"),
		len(view.Diff.AddedSymbols),
		len(view.Diff.RemovedSymbols),
		len(view.Diff.ChangedSymbols),
		len(view.Diff.ImpactedSymbols),
		p.label("Edges:"),
		len(view.Diff.ChangedPackages),
		len(view.Diff.AddedCalls),
		len(view.Diff.RemovedCalls),
		len(view.Diff.AddedRefs),
		len(view.Diff.RemovedRefs),
		len(view.Diff.AddedTestLinks),
		len(view.Diff.RemovedTestLinks),
	); err != nil {
		return err
	}
	if explain {
		if err := renderHumanExplainSection(stdout, p, buildDiffExplain(view.Diff)); err != nil {
			return err
		}
	}
	if err := renderHumanStringList(stdout, p, "Added Files", view.Diff.AddedFiles, max(limit, 6)); err != nil {
		return err
	}
	if err := renderHumanStringList(stdout, p, "Changed Files", view.Diff.ChangedFiles, max(limit, 6)); err != nil {
		return err
	}
	if err := renderHumanChangedPackagesSlice(stdout, p, modulePath, view.Diff.ChangedPackages, max(limit, 6)); err != nil {
		return err
	}
	if err := renderHumanChangedSymbolsSlice(stdout, p, modulePath, view.Diff.ChangedSymbols, max(limit, 6)); err != nil {
		return err
	}
	if err := printImpactedSymbols(stdout, p, view.Diff.ImpactedSymbols[:min(len(view.Diff.ImpactedSymbols), max(limit, 6))]); err != nil {
		return err
	}
	if err := renderHumanTests(stdout, p, projectRoot, "Tests To Re-Run", view.Tests, 8); err != nil {
		return err
	}
	return renderHumanChecklist(stdout, p, "Review Checklist", view.Checklist, 8)
}

func renderAISnapshotReview(stdout io.Writer, modulePath string, view snapshotReviewView, explain bool) error {
	if _, err := fmt.Fprintf(stdout, "review scope=snapshot window=%d:%d files=+%d/~%d/-%d symbols=+%d/-%d/~%d impacted=%d packages=%d calls=+%d/-%d refs=+%d/-%d tests=+%d/-%d\n", view.Diff.FromSnapshotID, view.Diff.ToSnapshotID, len(view.Diff.AddedFiles), len(view.Diff.ChangedFiles), len(view.Diff.DeletedFiles), len(view.Diff.AddedSymbols), len(view.Diff.RemovedSymbols), len(view.Diff.ChangedSymbols), len(view.Diff.ImpactedSymbols), len(view.Diff.ChangedPackages), len(view.Diff.AddedCalls), len(view.Diff.RemovedCalls), len(view.Diff.AddedRefs), len(view.Diff.RemovedRefs), len(view.Diff.AddedTestLinks), len(view.Diff.RemovedTestLinks)); err != nil {
		return err
	}
	if explain {
		if err := renderAIExplainSection(stdout, "explain", buildDiffExplain(view.Diff)); err != nil {
			return err
		}
	}
	if err := renderAIStringList(stdout, "added_files", view.Diff.AddedFiles, 8); err != nil {
		return err
	}
	if err := renderAIStringList(stdout, "changed_files", view.Diff.ChangedFiles, 8); err != nil {
		return err
	}
	if err := renderAITests(stdout, "tests", view.Tests, 8); err != nil {
		return err
	}
	return renderAIChecklist(stdout, "review_checklist", view.Checklist, 8)
}

func buildWorkingTreeChangedPathLines(state core.ProjectState, plan codebase.ChangePlan, summaries map[string]storage.FileSummary, riskCtx workflowRiskContext) []string {
	currentByPath := codebase.ScanMap(state.Scanned)
	lines := make([]string, 0, plan.Changes.Count())
	appendLine := func(relPath, status, detail string) {
		line := fmt.Sprintf("%s [%s] %s", relPath, status, detail)
		if summary, ok := summaries[relPath]; ok {
			risk := fileRiskSummary(summary, riskCtx.hotScore(relPath), riskCtx.recentChanged(relPath))
			line += fmt.Sprintf(" | risk=%s", risk)
		}
		lines = append(lines, line)
	}
	for _, relPath := range plan.Changes.Added {
		appendLine(relPath, "added", explainChangedPath(relPath, currentByPath, state.Previous))
	}
	for _, relPath := range plan.Changes.Changed {
		appendLine(relPath, "changed", explainChangedPath(relPath, currentByPath, state.Previous))
	}
	for _, relPath := range plan.Changes.Deleted {
		appendLine(relPath, "deleted", explainDeletedPath(relPath, state.Previous))
	}
	return lines
}

func buildManifestReviewLines(plan codebase.ChangePlan) []string {
	lines := make([]string, 0, len(plan.ManifestChanges))
	for _, delta := range plan.ManifestChanges {
		line := fmt.Sprintf("%s [%s/%s]", delta.RelPath, delta.Kind, delta.Impact)
		if len(delta.Details) > 0 {
			line += " " + strings.Join(delta.Details, "; ")
		}
		if len(delta.Packages) > 0 {
			line += " | packages=" + strings.Join(delta.Packages, ", ")
		}
		lines = append(lines, line)
	}
	return lines
}

func buildPackageReviewTests(store *storage.Store, packages []string, limit int) ([]storage.TestView, error) {
	if limit <= 0 {
		limit = 8
	}
	seen := make(map[string]struct{}, len(packages))
	tests := make([]storage.TestView, 0)
	for _, pkg := range packages {
		pkg = strings.TrimSpace(pkg)
		if pkg == "" {
			continue
		}
		if _, ok := seen[pkg]; ok {
			continue
		}
		seen[pkg] = struct{}{}
		values, err := store.LoadPackageTests(pkg, limit)
		if err != nil {
			return nil, err
		}
		tests = append(tests, values...)
	}
	return dedupeTestsByBestScore(tests, limit), nil
}

func reviewPackagesFromPlan(state core.ProjectState, plan codebase.ChangePlan) []string {
	packages := append([]string(nil), plan.ImpactedPackages...)
	packages = append(packages, plan.ExpandedPackages...)
	currentByPath := codebase.ScanMap(state.Scanned)
	for _, relPath := range append(append([]string{}, plan.Changes.Added...), plan.Changes.Changed...) {
		if file, ok := currentByPath[relPath]; ok && strings.TrimSpace(file.PackageImportPath) != "" {
			packages = append(packages, file.PackageImportPath)
		}
	}
	for _, relPath := range plan.Changes.Deleted {
		if prev, ok := state.Previous[relPath]; ok && strings.TrimSpace(prev.PackageImportPath) != "" {
			packages = append(packages, prev.PackageImportPath)
		}
	}
	seen := make(map[string]struct{}, len(packages))
	values := make([]string, 0, len(packages))
	for _, pkg := range packages {
		pkg = strings.TrimSpace(pkg)
		if pkg == "" {
			continue
		}
		if _, ok := seen[pkg]; ok {
			continue
		}
		seen[pkg] = struct{}{}
		values = append(values, pkg)
	}
	sort.Strings(values)
	return values
}

func buildWorkingTreeChecklist(view workingTreeReviewView) []string {
	items := make([]string, 0, 6)
	if view.Plan.FullReindex {
		items = append(items, "Project metadata changed enough to require a full reindex, so review manifest and root-level shifts first.")
	}
	if len(view.ManifestLines) > 0 {
		items = append(items, "Open changed manifests before code diffs because they can widen review scope immediately.")
	}
	if view.Plan.Metrics.ExpandedPackageCount > view.Plan.Metrics.DirectPackageCount {
		items = append(items, "Reverse deps expanded the review surface beyond directly changed packages.")
	}
	if len(view.Tests) == 0 {
		items = append(items, "No strong package tests were found for this working-tree delta, so plan manual verification.")
	}
	if view.Plan.Changes.Count() == 0 {
		items = append(items, "Working tree matches the current snapshot; there may be nothing review-worthy right now.")
	}
	if len(items) == 0 {
		items = append(items, "Review changed paths first, then spot-check impacted packages and nearby tests.")
	}
	return items
}

func buildSnapshotChecklist(view snapshotReviewView) []string {
	items := make([]string, 0, 6)
	if len(view.Diff.ChangedSymbols) > 0 {
		items = append(items, "Check changed symbols for contract and location shifts before reading raw file diffs.")
	}
	if len(view.Diff.AddedCalls)+len(view.Diff.RemovedCalls) > 0 {
		items = append(items, "Review added and removed call edges because control flow changed.")
	}
	if len(view.Diff.AddedRefs)+len(view.Diff.RemovedRefs) > 0 {
		items = append(items, "Inspect new or removed references for hidden coupling changes.")
	}
	if len(view.Diff.ImpactedSymbols) > 0 {
		items = append(items, "Use impacted symbols as the shortlist for blast-radius inspection.")
	}
	if len(view.Tests) == 0 {
		items = append(items, "Snapshot diff has weak explicit test coverage, so plan manual validation.")
	}
	if len(items) == 0 {
		items = append(items, "Diff is small and contained; verify files, then scan changed packages and tests.")
	}
	return items
}

func buildChangePlanExplainFromView(plan codebase.ChangePlan, changedPaths, manifestLines []string) (explainSection, error) {
	section := explainSection{
		Title: "Explain",
		Facts: []explainFact{
			{Key: "Strategy", Value: describeChangePlanStrategy(plan)},
			{Key: "Reason", Value: blankIf(strings.TrimSpace(plan.Reason), "none")},
			{Key: "Change cache", Value: fmt.Sprintf("%s (%s)", yesNo(plan.CacheHit), shortFingerprint(plan.Fingerprint))},
			{Key: "Changes", Value: fmt.Sprintf("added=%d changed=%d deleted=%d", len(plan.Changes.Added), len(plan.Changes.Changed), len(plan.Changes.Deleted))},
			{Key: "Package scope", Value: fmt.Sprintf("direct=%d expanded=%d reused=%d (%d%%)", plan.Metrics.DirectPackageCount, plan.Metrics.ExpandedPackageCount, plan.Metrics.ReusedPackageCount, plan.Metrics.ReusePercent)},
			{Key: "Precision", Value: "working-tree review is built from the indexed baseline plus current scan fingerprints; non-indexed runtime behavior may be missing"},
		},
	}
	if len(changedPaths) > 0 {
		items := make([]explainItem, 0, min(6, len(changedPaths)))
		for _, line := range changedPaths[:min(6, len(changedPaths))] {
			items = append(items, explainItem{Label: line})
		}
		section.Groups = append(section.Groups, explainGroup{Title: "Changed paths", Items: items})
	}
	if len(manifestLines) > 0 {
		items := make([]explainItem, 0, min(6, len(manifestLines)))
		for _, line := range manifestLines[:min(6, len(manifestLines))] {
			items = append(items, explainItem{Label: line})
		}
		section.Groups = append(section.Groups, explainGroup{Title: "Manifest semantics", Items: items})
	}
	if len(plan.ExpandedPackages) > 0 {
		items := make([]explainItem, 0, min(6, len(plan.ExpandedPackages)))
		for _, pkg := range plan.ExpandedPackages[:min(6, len(plan.ExpandedPackages))] {
			items = append(items, explainItem{Label: pkg})
		}
		section.Groups = append(section.Groups, explainGroup{Title: "Expanded review packages", Items: items})
	}
	return section, nil
}
