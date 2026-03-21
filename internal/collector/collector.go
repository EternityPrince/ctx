package collector

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/vladimirkasterin/ctx/internal/config"
	"github.com/vladimirkasterin/ctx/internal/filter"
	"github.com/vladimirkasterin/ctx/internal/model"
	"github.com/vladimirkasterin/ctx/internal/text"
)

func Collect(options config.Options) (*model.Snapshot, error) {
	snapshot := &model.Snapshot{
		Root:        options.Root,
		GeneratedAt: time.Now(),
	}

	extensionFilter := makeExtensionFilter(options.Extensions)
	extensionStats := make(map[string]*model.ExtensionMetric)
	skipReasons := make(map[string]int)

	err := filepath.WalkDir(options.Root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relPath, err := filepath.Rel(options.Root, path)
		if err != nil {
			return fmt.Errorf("make relative path: %w", err)
		}

		if relPath == "." {
			snapshot.Stats.DirectoriesScanned++
			return nil
		}

		name := entry.Name()
		if entry.IsDir() {
			snapshot.Stats.DirectoriesScanned++
			if skip, reason := filter.SkipDirectory(name, options.IncludeHidden); skip {
				snapshot.Stats.DirectoriesSkipped++
				skipReasons[reason]++
				return filepath.SkipDir
			}
			snapshot.Directories = append(snapshot.Directories, filepath.ToSlash(relPath))
			return nil
		}

		snapshot.Stats.FilesScanned++

		if skip, reason := filter.SkipFile(name, options.IncludeHidden); skip {
			skipReasons[reason]++
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("read file info for %s: %w", path, err)
		}

		if options.MaxFileSize > 0 && info.Size() > options.MaxFileSize {
			skipReasons["file exceeds size limit"]++
			return nil
		}

		extension := strings.ToLower(filepath.Ext(name))
		if !extensionFilter.match(extension) {
			skipReasons["filtered by extension"]++
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read file %s: %w", path, err)
		}

		if filter.IsLikelyBinary(data) {
			skipReasons["binary file"]++
			return nil
		}

		content := text.NormalizeNewlines(string(data))
		totalLines, nonEmptyLines := text.CountLines(content)

		file := model.File{
			Name:          name,
			AbsolutePath:  path,
			RelativePath:  filepath.ToSlash(relPath),
			Extension:     extensionOrFallback(extension),
			SizeBytes:     info.Size(),
			LineCount:     totalLines,
			NonEmptyLines: nonEmptyLines,
			Content:       content,
		}

		snapshot.Files = append(snapshot.Files, file)
		accumulateStats(&snapshot.Stats, extensionStats, file)
		return nil
	})
	if err != nil {
		return nil, err
	}

	slices.Sort(snapshot.Directories)
	sort.Slice(snapshot.Files, func(i, j int) bool {
		return snapshot.Files[i].RelativePath < snapshot.Files[j].RelativePath
	})

	finalizeStats(&snapshot.Stats, extensionStats, skipReasons)

	return snapshot, nil
}

type extensionFilter struct {
	active bool
	values map[string]struct{}
}

func makeExtensionFilter(extensions []string) extensionFilter {
	if len(extensions) == 0 {
		return extensionFilter{}
	}

	values := make(map[string]struct{}, len(extensions))
	for _, extension := range extensions {
		values[strings.ToLower(extension)] = struct{}{}
	}

	return extensionFilter{
		active: true,
		values: values,
	}
}

func (f extensionFilter) match(extension string) bool {
	if !f.active {
		return true
	}
	_, ok := f.values[extension]
	return ok
}

func accumulateStats(stats *model.Stats, extensionStats map[string]*model.ExtensionMetric, file model.File) {
	stats.FilesIncluded++
	stats.TotalBytes += file.SizeBytes
	stats.TotalLines += file.LineCount
	stats.TotalNonEmptyLines += file.NonEmptyLines

	if file.LineCount > stats.LargestFile.Lines || (file.LineCount == stats.LargestFile.Lines && file.SizeBytes > stats.LargestFile.SizeBytes) {
		stats.LargestFile = model.FileMetric{
			Path:      file.RelativePath,
			SizeBytes: file.SizeBytes,
			Lines:     file.LineCount,
		}
	}

	stats.TopFiles = append(stats.TopFiles, model.FileMetric{
		Path:      file.RelativePath,
		SizeBytes: file.SizeBytes,
		Lines:     file.LineCount,
	})

	metric, ok := extensionStats[file.Extension]
	if !ok {
		metric = &model.ExtensionMetric{Name: file.Extension}
		extensionStats[file.Extension] = metric
	}
	metric.Files++
	metric.SizeBytes += file.SizeBytes
	metric.Lines += file.LineCount
}

func finalizeStats(stats *model.Stats, extensionStats map[string]*model.ExtensionMetric, skipReasons map[string]int) {
	stats.FilesSkipped = stats.FilesScanned - stats.FilesIncluded
	if stats.FilesIncluded > 0 {
		stats.AverageLines = float64(stats.TotalLines) / float64(stats.FilesIncluded)
	}

	for _, metric := range extensionStats {
		stats.Extensions = append(stats.Extensions, *metric)
	}
	sort.Slice(stats.Extensions, func(i, j int) bool {
		if stats.Extensions[i].Files == stats.Extensions[j].Files {
			return stats.Extensions[i].Name < stats.Extensions[j].Name
		}
		return stats.Extensions[i].Files > stats.Extensions[j].Files
	})

	for reason, count := range skipReasons {
		stats.SkipReasons = append(stats.SkipReasons, model.NamedMetric{Name: reason, Count: count})
	}
	sort.Slice(stats.SkipReasons, func(i, j int) bool {
		if stats.SkipReasons[i].Count == stats.SkipReasons[j].Count {
			return stats.SkipReasons[i].Name < stats.SkipReasons[j].Name
		}
		return stats.SkipReasons[i].Count > stats.SkipReasons[j].Count
	})

	sort.Slice(stats.TopFiles, func(i, j int) bool {
		if stats.TopFiles[i].Lines == stats.TopFiles[j].Lines {
			return stats.TopFiles[i].Path < stats.TopFiles[j].Path
		}
		return stats.TopFiles[i].Lines > stats.TopFiles[j].Lines
	})
	if len(stats.TopFiles) > 5 {
		stats.TopFiles = stats.TopFiles[:5]
	}
}

func extensionOrFallback(extension string) string {
	if extension == "" {
		return "[no extension]"
	}
	return extension
}
