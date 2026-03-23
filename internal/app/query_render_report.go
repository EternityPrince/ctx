package app

import (
	"fmt"
	"io"

	"github.com/vladimirkasterin/ctx/internal/storage"
)

func renderHumanReport(stdout io.Writer, projectRoot, modulePath string, status storage.ProjectStatus, view storage.ReportView, watch reportTestWatch) error {
	p := newPalette()

	if _, err := fmt.Fprintf(stdout, "%s\n%s\n", p.rule("Project Report"), p.title("CTX Project Report")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		stdout,
		"%s %s\n%s %s\n%s %d (%s)\n%s packages=%d files=%d symbols=%d refs=%d calls=%d tests=%d\n%s %d\n%s callers + refs + tests + reverse deps\n\n",
		p.label("Root:"),
		projectRoot,
		p.label("Module:"),
		modulePath,
		p.label("Snapshot:"),
		view.Snapshot.ID,
		view.Snapshot.CreatedAt.Format(timeFormat),
		p.label("Inventory:"),
		view.Snapshot.TotalPackages,
		view.Snapshot.TotalFiles,
		view.Snapshot.TotalSymbols,
		view.Snapshot.TotalRefs,
		view.Snapshot.TotalCalls,
		view.Snapshot.TotalTests,
		p.label("Changed now:"),
		status.ChangedNow,
		p.label("Importance model:"),
	); err != nil {
		return err
	}

	if err := renderHumanPackages(stdout, p, modulePath, "Key Packages", view.TopPackages); err != nil {
		return err
	}
	if err := renderHumanRankedSymbols(stdout, p, modulePath, "Critical Functions and Methods", view.TopFunctions); err != nil {
		return err
	}
	if err := renderHumanRankedSymbols(stdout, p, modulePath, "Important Types", view.TopTypes); err != nil {
		return err
	}
	if len(watch.ThinDirectSymbols) > 0 || len(watch.WeakChangedAreas) > 0 {
		if _, err := fmt.Fprintf(stdout, "%s\n", p.section("Test Watch")); err != nil {
			return err
		}
		if len(watch.ThinDirectSymbols) > 0 {
			if _, err := fmt.Fprintf(stdout, "  %s high-signal symbols with thin direct test links\n\n", p.label("Focus:")); err != nil {
				return err
			}
			if err := renderHumanRankedSymbols(stdout, p, modulePath, "Thin Direct Test Coverage", watch.ThinDirectSymbols); err != nil {
				return err
			}
		}
		if len(watch.WeakChangedAreas) > 0 {
			if err := renderHumanWeakChangedAreas(stdout, p, modulePath, "Changed Areas With Weak Test Links", watch.WeakChangedAreas); err != nil {
				return err
			}
		}
	}

	_, err := fmt.Fprintf(stdout, "%s `ctx symbol <name>` for a full graph slice.\n", p.label("Next step:"))
	return err
}

func renderAIReport(stdout io.Writer, modulePath string, status storage.ProjectStatus, view storage.ReportView, watch reportTestWatch) error {
	if _, err := fmt.Fprintf(
		stdout,
		"report module=%s snapshot=%d snapshot_at=%s changed_now=%d packages=%d files=%d symbols=%d refs=%d calls=%d tests=%d\n",
		modulePath,
		view.Snapshot.ID,
		view.Snapshot.CreatedAt.Format(timeFormat),
		status.ChangedNow,
		view.Snapshot.TotalPackages,
		view.Snapshot.TotalFiles,
		view.Snapshot.TotalSymbols,
		view.Snapshot.TotalRefs,
		view.Snapshot.TotalCalls,
		view.Snapshot.TotalTests,
	); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(stdout, "top_packages=%d\n", len(view.TopPackages)); err != nil {
		return err
	}
	for _, item := range view.TopPackages {
		if _, err := fmt.Fprintf(
			stdout,
			"pkg q=%s files=%d symbols=%d tests=%d deps=%d rdeps=%d score=%d importance=%s\n",
			shortenQName(modulePath, item.Summary.ImportPath),
			item.Summary.FileCount,
			item.Summary.SymbolCount,
			item.Summary.TestCount,
			item.LocalDepCount,
			item.ReverseDepCount,
			item.Score,
			reportImportance(item.Score),
		); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintf(stdout, "top_functions=%d\n", len(view.TopFunctions)); err != nil {
		return err
	}
	for _, item := range view.TopFunctions {
		if _, err := fmt.Fprintf(
			stdout,
			"fn q=%s sig=%q file=%s:%d callers=%d callees=%d refs=%d tests=%d rdeps=%d score=%d importance=%s\n",
			shortenQName(modulePath, item.Symbol.QName),
			displaySignature(item.Symbol),
			item.Symbol.FilePath,
			item.Symbol.Line,
			item.CallerCount,
			item.CalleeCount,
			item.ReferenceCount,
			item.TestCount,
			item.ReversePackageDeps,
			item.Score,
			reportImportance(item.Score),
		); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintf(stdout, "top_types=%d\n", len(view.TopTypes)); err != nil {
		return err
	}
	for _, item := range view.TopTypes {
		if _, err := fmt.Fprintf(
			stdout,
			"type q=%s kind=%s sig=%q file=%s:%d refs=%d tests=%d methods=%d rdeps=%d score=%d importance=%s\n",
			shortenQName(modulePath, item.Symbol.QName),
			item.Symbol.Kind,
			displaySignature(item.Symbol),
			item.Symbol.FilePath,
			item.Symbol.Line,
			item.ReferenceCount,
			item.TestCount,
			item.MethodCount,
			item.ReversePackageDeps,
			item.Score,
			reportImportance(item.Score),
		); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(stdout, "thin_test_symbols=%d weak_changed_areas=%d\n", len(watch.ThinDirectSymbols), len(watch.WeakChangedAreas)); err != nil {
		return err
	}
	for _, item := range watch.ThinDirectSymbols {
		if _, err := fmt.Fprintf(stdout, "thin_symbol q=%s score=%d risk=%q\n", shortenQName(modulePath, item.Symbol.QName), item.Score, rankedSymbolRiskSummary(item)); err != nil {
			return err
		}
	}
	for _, area := range watch.WeakChangedAreas {
		if _, err := fmt.Fprintf(stdout, "weak_area file=%s package=%s tests=%d linked=%d coverage=%d hot=%d risk=%q\n", area.FilePath, shortenQName(modulePath, area.PackageImportPath), area.RelatedTestCount, area.TestLinkedCount, area.CoveragePercent, area.HotScore, area.Risk); err != nil {
			return err
		}
	}
	return nil
}
