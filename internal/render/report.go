package render

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/config"
	"github.com/vladimirkasterin/ctx/internal/model"
	"github.com/vladimirkasterin/ctx/internal/tree"
)

func Report(snapshot *model.Snapshot, projectTree *tree.Node, options config.Options) string {
	var b strings.Builder

	writeSummary(&b, snapshot, options)
	if options.SummaryOnly {
		return b.String()
	}

	if !options.NoTree {
		b.WriteString("\nDIRECTORY TREE\n")
		writeTree(&b, projectTree)
	}

	if !options.NoContents {
		b.WriteString("\nFILES\n")
		writeFiles(&b, snapshot.Files)
	}

	return b.String()
}

func writeSummary(b *strings.Builder, snapshot *model.Snapshot, options config.Options) {
	stats := snapshot.Stats

	b.WriteString("CTX REPORT\n")
	b.WriteString(fmt.Sprintf("Root: %s\n", snapshot.Root))
	b.WriteString(fmt.Sprintf("Generated: %s\n", snapshot.GeneratedAt.Format("2006-01-02 15:04:05")))
	b.WriteString(fmt.Sprintf("Directories scanned: %d\n", stats.DirectoriesScanned))
	b.WriteString(fmt.Sprintf("Directories skipped: %d\n", stats.DirectoriesSkipped))
	b.WriteString(fmt.Sprintf("Files scanned: %d\n", stats.FilesScanned))
	b.WriteString(fmt.Sprintf("Files included: %d\n", stats.FilesIncluded))
	b.WriteString(fmt.Sprintf("Files skipped: %d\n", stats.FilesSkipped))
	b.WriteString(fmt.Sprintf("Total lines: %d\n", stats.TotalLines))
	b.WriteString(fmt.Sprintf("Non-empty lines: %d\n", stats.TotalNonEmptyLines))
	b.WriteString(fmt.Sprintf("Average lines per file: %.1f\n", stats.AverageLines))
	b.WriteString(fmt.Sprintf("Total size: %s\n", humanSize(stats.TotalBytes)))

	if stats.LargestFile.Path != "" {
		b.WriteString(fmt.Sprintf("Largest file: %s (%d lines, %s)\n", stats.LargestFile.Path, stats.LargestFile.Lines, humanSize(stats.LargestFile.SizeBytes)))
	}

	if len(stats.SkipReasons) > 0 {
		b.WriteString("\nSkipped files and directories:\n")
		for _, item := range stats.SkipReasons {
			b.WriteString(fmt.Sprintf("  - %s: %d\n", item.Name, item.Count))
		}
	}

	if len(stats.Extensions) > 0 {
		b.WriteString("\nExtension breakdown:\n")
		for _, item := range stats.Extensions {
			b.WriteString(fmt.Sprintf("  - %s: %d files, %d lines, %s\n", item.Name, item.Files, item.Lines, humanSize(item.SizeBytes)))
		}
	}

	if len(stats.TopFiles) > 0 {
		b.WriteString("\nTop files by line count:\n")
		for _, file := range stats.TopFiles {
			b.WriteString(fmt.Sprintf("  - %s: %d lines, %s\n", file.Path, file.Lines, humanSize(file.SizeBytes)))
		}
	}

	if options.Explain {
		b.WriteString("\nExplainability:\n")
		if options.ConfigPath != "" {
			b.WriteString(fmt.Sprintf("  - config: %s\n", options.ConfigPath))
		}
		if options.ConfigProfile != "" {
			b.WriteString(fmt.Sprintf("  - profile: %s\n", options.ConfigProfile))
		}
		b.WriteString(fmt.Sprintf("  - include hidden: %t\n", options.IncludeHidden))
		b.WriteString(fmt.Sprintf("  - max file size: %d\n", options.MaxFileSize))
		b.WriteString(fmt.Sprintf("  - keep empty: %t\n", options.KeepEmpty))
		b.WriteString(fmt.Sprintf("  - include generated: %t\n", options.IncludeGenerated))
		b.WriteString(fmt.Sprintf("  - include minified: %t\n", options.IncludeMinified))
		b.WriteString(fmt.Sprintf("  - include artifacts: %t\n", options.IncludeArtifacts))
		if len(options.Extensions) > 0 {
			b.WriteString(fmt.Sprintf("  - extensions: %s\n", strings.Join(options.Extensions, ", ")))
		}
		if len(snapshot.Decisions) > 0 {
			b.WriteString("\nDecision log:\n")
			for _, decision := range snapshot.Decisions {
				status := "skipped"
				if decision.Included {
					status = "included"
				}
				b.WriteString(fmt.Sprintf("  - [%s] %s (%s): %s\n", status, decision.Path, decision.Kind, decision.Reason))
			}
		}
	}
}

func writeTree(b *strings.Builder, root *tree.Node) {
	b.WriteString(root.Name)
	b.WriteString("/\n")
	for i, child := range root.Children {
		last := i == len(root.Children)-1
		writeTreeNode(b, child, "", last)
	}
}

func writeTreeNode(b *strings.Builder, node *tree.Node, prefix string, last bool) {
	branch := "|-- "
	nextPrefix := prefix + "|   "
	if last {
		branch = "`-- "
		nextPrefix = prefix + "    "
	}

	b.WriteString(prefix)
	b.WriteString(branch)
	b.WriteString(node.Name)
	if node.IsDir {
		b.WriteString("/")
	}
	b.WriteByte('\n')

	for i, child := range node.Children {
		writeTreeNode(b, child, nextPrefix, i == len(node.Children)-1)
	}
}

func writeFiles(b *strings.Builder, files []model.File) {
	for i, file := range files {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(fmt.Sprintf("===== %s =====\n", filepath.ToSlash(file.RelativePath)))
		b.WriteString(file.Content)
		if file.Content != "" && !strings.HasSuffix(file.Content, "\n") {
			b.WriteByte('\n')
		}
	}
}

func humanSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}

	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	suffixes := []string{"KB", "MB", "GB", "TB"}
	return fmt.Sprintf("%.1f %s", float64(size)/float64(div), suffixes[exp])
}
