package app

import (
	"fmt"

	"github.com/vladimirkasterin/ctx/internal/storage"
)

func formatDurationMillis(value int) string {
	switch {
	case value <= 0:
		return "0ms"
	case value < 1000:
		return fmt.Sprintf("%dms", value)
	case value%1000 == 0:
		return fmt.Sprintf("%ds", value/1000)
	default:
		return fmt.Sprintf("%.1fs", float64(value)/1000)
	}
}

func formatSnapshotTelemetry(snapshot storage.SnapshotInfo) string {
	return fmt.Sprintf(
		"scan=%s analyze=%s write=%s total=%s bottleneck=%s scanned_files=%d mode=%s direct_pkgs=%d expanded_pkgs=%d reused_pkgs=%d reuse=%d%% plan_cache=%t",
		formatDurationMillis(snapshot.ScanDurationMs),
		formatDurationMillis(snapshot.AnalyzeDurationMs),
		formatDurationMillis(snapshot.WriteDurationMs),
		formatDurationMillis(snapshot.TotalDurationMs()),
		snapshot.TimingBottleneck(),
		snapshot.ScannedFiles,
		blankIf(snapshot.IncrementalMode, "unknown"),
		snapshot.DirectPackages,
		snapshot.ExpandedPackages,
		snapshot.ReusedPackages,
		snapshot.ReusePercent,
		snapshot.PlanCacheHit,
	)
}

func blankIf(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
