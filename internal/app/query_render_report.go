package app

import (
	"fmt"
	"io"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/storage"
)

func renderHumanReport(stdout io.Writer, projectRoot, modulePath string, status storage.ProjectStatus, view storage.ReportView, watch reportTestWatch, composition projectComposition, explain bool) error {
	p := newPalette()

	if _, err := fmt.Fprintf(stdout, "%s\n%s\n", p.rule("Project Report"), p.title("CTX Project Report")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		stdout,
		"%s %s\n%s %s\n%s %s\n%s %d (%s)\n%s packages=%d files=%d symbols=%d refs=%d calls=%d tests=%d\n%s %d\n%s %s\n%s graph + change proximity + entrypoint heuristics\n",
		p.label("Root:"),
		projectRoot,
		p.label("Module:"),
		modulePath,
		p.label("Composition:"),
		composition.Display(),
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
		p.label("Capabilities:"),
		composition.Capabilities(),
		p.label("Quality model:"),
	); err != nil {
		return err
	}
	if len(view.ProvenanceNotes) > 0 {
		if _, err := fmt.Fprintf(stdout, "%s %s\n", p.label("Provenance:"), strings.Join(view.ProvenanceNotes, " ")); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(stdout); err != nil {
		return err
	}

	if err := renderHumanPackages(stdout, p, modulePath, "Key Packages", view.TopPackages, explain); err != nil {
		return err
	}
	if err := renderHumanRankedFiles(stdout, p, "Key Files", view.TopFiles, explain); err != nil {
		return err
	}
	if err := renderHumanRankedSymbols(stdout, p, modulePath, "Critical Functions and Methods", view.TopFunctions, explain); err != nil {
		return err
	}
	if err := renderHumanRankedSymbols(stdout, p, modulePath, "Important Types", view.TopTypes, explain); err != nil {
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
			if err := renderHumanRankedSymbols(stdout, p, modulePath, "Thin Direct Test Coverage", watch.ThinDirectSymbols, explain); err != nil {
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

func renderAIReport(stdout io.Writer, modulePath string, status storage.ProjectStatus, view storage.ReportView, watch reportTestWatch, composition projectComposition, explain bool) error {
	if _, err := fmt.Fprintf(
		stdout,
		"report module=%s snapshot=%d snapshot_at=%s composition=%s capabilities=%q changed_now=%d packages=%d files=%d symbols=%d refs=%d calls=%d tests=%d explain=%t\n",
		modulePath,
		view.Snapshot.ID,
		view.Snapshot.CreatedAt.Format(timeFormat),
		composition.Display(),
		composition.Capabilities(),
		status.ChangedNow,
		view.Snapshot.TotalPackages,
		view.Snapshot.TotalFiles,
		view.Snapshot.TotalSymbols,
		view.Snapshot.TotalRefs,
		view.Snapshot.TotalCalls,
		view.Snapshot.TotalTests,
		explain,
	); err != nil {
		return err
	}
	for _, note := range view.ProvenanceNotes {
		if _, err := fmt.Fprintf(stdout, "provenance=%q\n", note); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintf(stdout, "top_packages=%d\n", len(view.TopPackages)); err != nil {
		return err
	}
	for _, item := range view.TopPackages {
		if _, err := fmt.Fprintf(
			stdout,
			"pkg q=%s files=%d symbols=%d tests=%d deps=%d rdeps=%d score=%d importance=%s",
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
		if explain {
			if _, err := fmt.Fprintf(stdout, " why=%q", formatExplainInline(modulePath, item.QualityWhy, item.Provenance)); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(stdout); err != nil {
			return err
		}
	}
	if err := renderAIRankedFiles(stdout, "top_files", view.TopFiles, len(view.TopFiles), explain); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(stdout, "top_functions=%d\n", len(view.TopFunctions)); err != nil {
		return err
	}
	for _, item := range view.TopFunctions {
		if _, err := fmt.Fprintf(
			stdout,
			"fn q=%s sig=%q file=%s:%d callers=%d callees=%d refs=%d tests=%d rdeps=%d score=%d importance=%s",
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
		if explain {
			if _, err := fmt.Fprintf(stdout, " why=%q", formatExplainInline(modulePath, item.QualityWhy, item.Provenance)); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(stdout); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintf(stdout, "top_types=%d\n", len(view.TopTypes)); err != nil {
		return err
	}
	for _, item := range view.TopTypes {
		if _, err := fmt.Fprintf(
			stdout,
			"type q=%s kind=%s sig=%q file=%s:%d refs=%d tests=%d methods=%d rdeps=%d score=%d importance=%s",
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
		if explain {
			if _, err := fmt.Fprintf(stdout, " why=%q", formatExplainInline(modulePath, item.QualityWhy, item.Provenance)); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(stdout); err != nil {
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

func formatProvenanceInline(modulePath string, items []storage.ProvenanceItem) string {
	if len(items) == 0 {
		return ""
	}

	parts := make([]string, 0, len(items))
	for _, item := range items {
		label := item.Label
		switch item.Kind {
		case "call", "ref", "method", "symbol", "reverse_dep":
			label = shortenQName(modulePath, item.Label)
		}

		part := item.Why + ": " + label
		if item.FilePath != "" && item.Line > 0 {
			part += fmt.Sprintf(" @ %s:%d", item.FilePath, item.Line)
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, " | ")
}

func formatExplainInline(modulePath string, quality []string, items []storage.ProvenanceItem) string {
	parts := make([]string, 0, len(quality)+1)
	for _, item := range quality {
		item = strings.TrimSpace(item)
		if item != "" {
			parts = append(parts, item)
		}
	}
	if text := formatProvenanceInline(modulePath, items); text != "" {
		parts = append(parts, text)
	}
	return strings.Join(parts, " | ")
}
