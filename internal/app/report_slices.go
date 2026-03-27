package app

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/storage"
)

type reportSliceView struct {
	Scope            string
	Symbols          []storage.RankedSymbol
	Types            []storage.RankedSymbol
	Packages         []storage.RankedPackage
	HotFiles         []shellHotFile
	WeakChangedAreas []weakChangedArea
	Diff             storage.DiffView
	HasDiff          bool
}

func buildReportSlice(scope string, store *storage.Store, report storage.ReportView, watch reportTestWatch, limit int) (reportSliceView, error) {
	scope = normalizeReportSliceScope(scope)
	if limit <= 0 {
		limit = 8
	}

	view := reportSliceView{Scope: scope}
	switch scope {
	case "risky":
		view.Symbols = riskyReportSymbols(report, limit)
		view.Packages = riskyReportPackages(report, limit)
		view.WeakChangedAreas = limitWeakChangedAreas(watch.WeakChangedAreas, limit)
	case "seams":
		view.Symbols = seamReportSymbols(report, limit)
		view.Packages = seamReportPackages(report, limit)
	case "hotspots":
		view.HotFiles = limitHotFiles(rankShellHotFiles(report, ""), limit)
		view.Symbols = limitRankedSymbols(report.TopFunctions, limit)
		view.Types = limitRankedSymbols(report.TopTypes, limit)
		view.Packages = limitRankedPackages(report.TopPackages, limit)
	case "low-tested":
		view.Symbols = limitRankedSymbols(watch.ThinDirectSymbols, limit)
		view.Packages = lowTestReportPackages(report, limit)
		view.WeakChangedAreas = limitWeakChangedAreas(watch.WeakChangedAreas, limit)
	case "changed-since":
		if report.Snapshot.ParentID.Valid {
			diff, err := store.Diff(report.Snapshot.ParentID.Int64, report.Snapshot.ID)
			if err != nil {
				return reportSliceView{}, err
			}
			view.HasDiff = true
			view.Diff = diff
		}
		view.WeakChangedAreas = limitWeakChangedAreas(watch.WeakChangedAreas, limit)
	default:
		view.Scope = "project"
	}
	return view, nil
}

func normalizeReportSliceScope(scope string) string {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "risky", "risk":
		return "risky"
	case "seams", "seam":
		return "seams"
	case "hotspots", "hotspot", "hot":
		return "hotspots"
	case "low-tested", "lowtested", "low_tests":
		return "low-tested"
	case "changed-since", "changedsince", "changed", "changes":
		return "changed-since"
	default:
		return "project"
	}
}

func reportSliceTitle(scope string) string {
	switch normalizeReportSliceScope(scope) {
	case "risky":
		return "Risky"
	case "seams":
		return "Seams"
	case "hotspots":
		return "Hotspots"
	case "low-tested":
		return "Low-Tested"
	case "changed-since":
		return "Changed Since"
	default:
		return "Project"
	}
}

func renderHumanReportSlice(stdout io.Writer, projectRoot, modulePath string, status storage.ProjectStatus, report storage.ReportView, slice reportSliceView, limit int, explain bool) error {
	p := newPalette()
	title := reportSliceTitle(slice.Scope)

	if _, err := fmt.Fprintf(stdout, "%s\n%s\n", p.rule("Project Report"), p.title("CTX Report · "+title)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		stdout,
		"%s %s\n%s %s\n%s %d (%s)\n%s %s\n%s %d\n\n",
		p.label("Root:"),
		projectRoot,
		p.label("Module:"),
		modulePath,
		p.label("Snapshot:"),
		report.Snapshot.ID,
		report.Snapshot.CreatedAt.Format(timeFormat),
		p.label("Slice:"),
		title,
		p.label("Changed now:"),
		status.ChangedNow,
	); err != nil {
		return err
	}

	switch slice.Scope {
	case "risky":
		if err := renderHumanRankedSymbols(stdout, p, modulePath, "Risky Symbols", slice.Symbols, explain); err != nil {
			return err
		}
		if err := renderHumanPackages(stdout, p, modulePath, "Risky Packages", slice.Packages, explain); err != nil {
			return err
		}
		return renderHumanWeakChangedAreas(stdout, p, modulePath, "Changed Areas With Weak Test Links", slice.WeakChangedAreas)
	case "seams":
		if err := renderHumanRankedSymbols(stdout, p, modulePath, "Seam Symbols", slice.Symbols, explain); err != nil {
			return err
		}
		return renderHumanPackages(stdout, p, modulePath, "Seam Packages", slice.Packages, explain)
	case "hotspots":
		if err := renderHumanReportHotFiles(stdout, p, "Hot Files", slice.HotFiles); err != nil {
			return err
		}
		if err := renderHumanRankedSymbols(stdout, p, modulePath, "Hot Functions and Methods", slice.Symbols, explain); err != nil {
			return err
		}
		if err := renderHumanRankedSymbols(stdout, p, modulePath, "Hot Types", slice.Types, explain); err != nil {
			return err
		}
		return renderHumanPackages(stdout, p, modulePath, "Hot Packages", slice.Packages, explain)
	case "low-tested":
		if err := renderHumanRankedSymbols(stdout, p, modulePath, "High-Signal Symbols With Thin Direct Links", slice.Symbols, explain); err != nil {
			return err
		}
		if err := renderHumanPackages(stdout, p, modulePath, "Low-Test Packages", slice.Packages, explain); err != nil {
			return err
		}
		return renderHumanWeakChangedAreas(stdout, p, modulePath, "Changed Areas With Weak Test Links", slice.WeakChangedAreas)
	case "changed-since":
		if !slice.HasDiff {
			_, err := fmt.Fprintf(stdout, "%s\n  %s\n\n", p.section("Changed Since"), p.muted("Need at least two snapshots to compute this slice."))
			return err
		}
		if _, err := fmt.Fprintf(
			stdout,
			"%s\n  %s %d -> %d\n  %s added=%d changed=%d deleted=%d\n\n",
			p.section("Latest Snapshot Diff"),
			p.label("Window:"),
			slice.Diff.FromSnapshotID,
			slice.Diff.ToSnapshotID,
			p.label("Files:"),
			len(slice.Diff.AddedFiles),
			len(slice.Diff.ChangedFiles),
			len(slice.Diff.DeletedFiles),
		); err != nil {
			return err
		}
		if err := renderHumanStringList(stdout, p, "Added Files", slice.Diff.AddedFiles, limit); err != nil {
			return err
		}
		if err := renderHumanStringList(stdout, p, "Changed Files", slice.Diff.ChangedFiles, limit); err != nil {
			return err
		}
		if err := renderHumanChangedPackagesSlice(stdout, p, modulePath, slice.Diff.ChangedPackages, limit); err != nil {
			return err
		}
		if err := renderHumanChangedSymbolsSlice(stdout, p, modulePath, slice.Diff.ChangedSymbols, limit); err != nil {
			return err
		}
		return renderHumanWeakChangedAreas(stdout, p, modulePath, "Changed Areas With Weak Test Links", slice.WeakChangedAreas)
	default:
		return nil
	}
}

func renderAIReportSlice(stdout io.Writer, modulePath string, status storage.ProjectStatus, report storage.ReportView, slice reportSliceView, limit int, explain bool) error {
	if _, err := fmt.Fprintf(
		stdout,
		"report_slice scope=%s snapshot=%d changed_now=%d limit=%d\n",
		slice.Scope,
		report.Snapshot.ID,
		status.ChangedNow,
		limit,
	); err != nil {
		return err
	}

	switch slice.Scope {
	case "risky", "seams", "low-tested":
		for _, item := range slice.Symbols {
			if _, err := fmt.Fprintf(stdout, "symbol q=%s score=%d risk=%q", shortenQName(modulePath, item.Symbol.QName), item.Score, rankedSymbolRiskSummary(item)); err != nil {
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
		for _, item := range slice.Packages {
			if _, err := fmt.Fprintf(stdout, "pkg q=%s score=%d risk=%q", shortenQName(modulePath, item.Summary.ImportPath), item.Score, rankedPackageRiskSummary(item)); err != nil {
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
		for _, area := range slice.WeakChangedAreas {
			if _, err := fmt.Fprintf(stdout, "weak_area file=%s risk=%q coverage=%d hot=%d\n", area.FilePath, area.Risk, area.CoveragePercent, area.HotScore); err != nil {
				return err
			}
		}
	case "hotspots":
		for _, item := range slice.HotFiles {
			if _, err := fmt.Fprintf(stdout, "hot_file path=%s score=%d symbols=%q\n", item.Path, item.Score, strings.Join(item.Symbols, ",")); err != nil {
				return err
			}
		}
		for _, item := range slice.Symbols {
			if _, err := fmt.Fprintf(stdout, "fn q=%s score=%d risk=%q", shortenQName(modulePath, item.Symbol.QName), item.Score, rankedSymbolRiskSummary(item)); err != nil {
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
		for _, item := range slice.Types {
			if _, err := fmt.Fprintf(stdout, "type q=%s score=%d risk=%q", shortenQName(modulePath, item.Symbol.QName), item.Score, rankedSymbolRiskSummary(item)); err != nil {
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
		for _, item := range slice.Packages {
			if _, err := fmt.Fprintf(stdout, "pkg q=%s score=%d risk=%q", shortenQName(modulePath, item.Summary.ImportPath), item.Score, rankedPackageRiskSummary(item)); err != nil {
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
	case "changed-since":
		if _, err := fmt.Fprintf(stdout, "has_diff=%t from=%d to=%d\n", slice.HasDiff, slice.Diff.FromSnapshotID, slice.Diff.ToSnapshotID); err != nil {
			return err
		}
		for _, file := range slice.Diff.ChangedFiles[:min(limit, len(slice.Diff.ChangedFiles))] {
			if _, err := fmt.Fprintf(stdout, "changed_file=%s\n", file); err != nil {
				return err
			}
		}
		for _, item := range slice.Diff.ChangedPackages[:min(limit, len(slice.Diff.ChangedPackages))] {
			if _, err := fmt.Fprintf(stdout, "changed_pkg q=%s status=%s\n", shortenQName(modulePath, item.ImportPath), item.Status); err != nil {
				return err
			}
		}
		for _, item := range slice.Diff.ChangedSymbols[:min(limit, len(slice.Diff.ChangedSymbols))] {
			if _, err := fmt.Fprintf(stdout, "changed_symbol q=%s moved=%t contract=%t\n", shortenQName(modulePath, item.QName), item.Moved, item.ContractChanged); err != nil {
				return err
			}
		}
		for _, area := range slice.WeakChangedAreas {
			if _, err := fmt.Fprintf(stdout, "weak_area file=%s risk=%q coverage=%d hot=%d\n", area.FilePath, area.Risk, area.CoveragePercent, area.HotScore); err != nil {
				return err
			}
		}
	}
	return nil
}

func riskyReportSymbols(report storage.ReportView, limit int) []storage.RankedSymbol {
	values := mergeRankedSymbols(report.TopFunctions, report.TopTypes)
	filtered := make([]storage.RankedSymbol, 0, len(values))
	for _, item := range values {
		risk := rankedSymbolRiskSummary(item)
		if risk == "contained" {
			continue
		}
		if strings.Contains(risk, "blast=") || strings.Contains(risk, "hotspot") || strings.Contains(risk, "tests=thin") || strings.Contains(risk, "pkg-rdeps") {
			filtered = append(filtered, item)
		}
	}
	return limitRankedSymbols(filtered, limit)
}

func seamReportSymbols(report storage.ReportView, limit int) []storage.RankedSymbol {
	values := mergeRankedSymbols(report.TopFunctions, report.TopTypes)
	filtered := make([]storage.RankedSymbol, 0, len(values))
	for _, item := range values {
		if strings.Contains(rankedSymbolRiskSummary(item), "seam") {
			filtered = append(filtered, item)
		}
	}
	return limitRankedSymbols(filtered, limit)
}

func riskyReportPackages(report storage.ReportView, limit int) []storage.RankedPackage {
	filtered := make([]storage.RankedPackage, 0, len(report.TopPackages))
	for _, item := range report.TopPackages {
		risk := rankedPackageRiskSummary(item)
		if risk == "contained" {
			continue
		}
		filtered = append(filtered, item)
	}
	return limitRankedPackages(filtered, limit)
}

func seamReportPackages(report storage.ReportView, limit int) []storage.RankedPackage {
	filtered := make([]storage.RankedPackage, 0, len(report.TopPackages))
	for _, item := range report.TopPackages {
		if strings.Contains(rankedPackageRiskSummary(item), "seam") {
			filtered = append(filtered, item)
		}
	}
	return limitRankedPackages(filtered, limit)
}

func lowTestReportPackages(report storage.ReportView, limit int) []storage.RankedPackage {
	filtered := make([]storage.RankedPackage, 0, len(report.TopPackages))
	for _, item := range report.TopPackages {
		if strings.Contains(rankedPackageRiskSummary(item), "tests=thin") {
			filtered = append(filtered, item)
		}
	}
	return limitRankedPackages(filtered, limit)
}

func mergeRankedSymbols(parts ...[]storage.RankedSymbol) []storage.RankedSymbol {
	byKey := make(map[string]storage.RankedSymbol)
	for _, values := range parts {
		for _, value := range values {
			current, ok := byKey[value.Symbol.SymbolKey]
			if !ok || value.Score > current.Score {
				byKey[value.Symbol.SymbolKey] = value
			}
		}
	}
	merged := make([]storage.RankedSymbol, 0, len(byKey))
	for _, value := range byKey {
		merged = append(merged, value)
	}
	sort.Slice(merged, func(i, j int) bool {
		if merged[i].Score != merged[j].Score {
			return merged[i].Score > merged[j].Score
		}
		return merged[i].Symbol.QName < merged[j].Symbol.QName
	})
	return merged
}

func limitRankedSymbols(values []storage.RankedSymbol, limit int) []storage.RankedSymbol {
	if len(values) > limit {
		return values[:limit]
	}
	return values
}

func limitRankedPackages(values []storage.RankedPackage, limit int) []storage.RankedPackage {
	if len(values) > limit {
		return values[:limit]
	}
	return values
}

func limitHotFiles(values []shellHotFile, limit int) []shellHotFile {
	if len(values) > limit {
		return values[:limit]
	}
	return values
}

func limitWeakChangedAreas(values []weakChangedArea, limit int) []weakChangedArea {
	if len(values) > limit {
		return values[:limit]
	}
	return values
}

func renderHumanReportHotFiles(stdout io.Writer, p palette, title string, values []shellHotFile) error {
	if _, err := fmt.Fprintf(stdout, "%s\n", p.section(title)); err != nil {
		return err
	}
	if len(values) == 0 {
		return renderHumanEmpty(stdout, p)
	}
	for _, value := range values {
		if _, err := fmt.Fprintf(stdout, "  %s\n    %s score=%d symbols=%s\n", value.Path, p.label("metrics:"), value.Score, strings.Join(value.Symbols, ", ")); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(stdout)
	return err
}

func renderHumanChangedPackagesSlice(stdout io.Writer, p palette, modulePath string, values []storage.ChangedPackage, limit int) error {
	if _, err := fmt.Fprintf(stdout, "%s (%d)\n", p.section("Changed Packages"), len(values)); err != nil {
		return err
	}
	if len(values) == 0 {
		return renderHumanEmpty(stdout, p)
	}
	for _, value := range values[:min(limit, len(values))] {
		if _, err := fmt.Fprintf(
			stdout,
			"  %s [%s]\n    %s files %d -> %d  symbols %d -> %d  tests %d -> %d  deps %d -> %d  rdeps %d -> %d\n",
			shortenQName(modulePath, value.ImportPath),
			value.Status,
			p.label("delta:"),
			value.FromFileCount,
			value.ToFileCount,
			value.FromSymbolCount,
			value.ToSymbolCount,
			value.FromTestCount,
			value.ToTestCount,
			value.FromLocalDepCount,
			value.ToLocalDepCount,
			value.FromReverseDepCount,
			value.ToReverseDepCount,
		); err != nil {
			return err
		}
	}
	return renderMoreLine(stdout, len(values), limit)
}

func renderHumanChangedSymbolsSlice(stdout io.Writer, p palette, modulePath string, values []storage.ChangedSymbol, limit int) error {
	if _, err := fmt.Fprintf(stdout, "%s (%d)\n", p.section("Changed Symbols"), len(values)); err != nil {
		return err
	}
	if len(values) == 0 {
		return renderHumanEmpty(stdout, p)
	}
	for _, value := range values[:min(limit, len(values))] {
		flags := make([]string, 0, 2)
		if value.ContractChanged {
			flags = append(flags, "contract")
		}
		if value.Moved {
			flags = append(flags, "moved")
		}
		if _, err := fmt.Fprintf(
			stdout,
			"  %s [%s]\n    %s %s @ %s:%d\n    %s %s @ %s:%d\n",
			shortenQName(modulePath, value.QName),
			strings.Join(flags, ", "),
			p.label("from:"),
			value.FromSignature,
			value.FromFilePath,
			value.FromLine,
			p.label("to:"),
			value.ToSignature,
			value.ToFilePath,
			value.ToLine,
		); err != nil {
			return err
		}
	}
	return renderMoreLine(stdout, len(values), limit)
}
