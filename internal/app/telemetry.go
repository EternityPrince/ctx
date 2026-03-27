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
		"scan=%s analyze=%s write=%s total=%s bottleneck=%s scanned_files=%d",
		formatDurationMillis(snapshot.ScanDurationMs),
		formatDurationMillis(snapshot.AnalyzeDurationMs),
		formatDurationMillis(snapshot.WriteDurationMs),
		formatDurationMillis(snapshot.TotalDurationMs()),
		snapshot.TimingBottleneck(),
		snapshot.ScannedFiles,
	)
}
