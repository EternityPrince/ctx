package cli

import (
	"errors"
	"flag"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/config"
)

func Parse(args []string) (config.Options, error) {
	var options config.Options

	fs := flag.NewFlagSet("ctx", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	fs.BoolVar(&options.IncludeHidden, "hidden", false, "include hidden files and directories")
	fs.Int64Var(&options.MaxFileSize, "max-file-size", 2*1024*1024, "maximum file size in bytes (0 disables the limit)")
	fs.StringVar(&options.OutputPath, "output", "", "write report to a file instead of stdout")
	fs.BoolVar(&options.CopyToClipboard, "copy", false, "copy report to the macOS clipboard")
	fs.StringVar(&options.ExtensionsRaw, "extensions", "", "comma-separated list of file extensions to include")
	fs.BoolVar(&options.SummaryOnly, "summary-only", false, "print only summary statistics")
	fs.BoolVar(&options.NoTree, "no-tree", false, "skip directory tree output")
	fs.BoolVar(&options.NoContents, "no-contents", false, "skip file contents output")

	fs.Usage = func() {}

	if err := fs.Parse(args); err != nil {
		return config.Options{}, usageError(err)
	}

	if options.CopyToClipboard && options.OutputPath != "" {
		return config.Options{}, usageError(errors.New("flags -copy and -output cannot be used together"))
	}

	remaining := fs.Args()
	if len(remaining) > 1 {
		return config.Options{}, usageError(errors.New("only one root path can be provided"))
	}

	root := "."
	if len(remaining) == 1 {
		root = remaining[0]
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return config.Options{}, fmt.Errorf("resolve root path: %w", err)
	}

	options.Root = absRoot
	options.Extensions = normalizeExtensions(options.ExtensionsRaw)

	return options, nil
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}

func usageError(err error) error {
	return fmt.Errorf("%w\n\nUsage:\n  ctx [path] [flags]\n\nFlags:\n  -hidden            include hidden files and directories\n  -max-file-size     maximum file size in bytes (default 2097152)\n  -output            write report to a file\n  -copy              copy report to the macOS clipboard\n  -extensions        comma-separated extension filter, for example go,md,yaml\n  -summary-only      print only summary statistics\n  -no-tree           skip directory tree output\n  -no-contents       skip file contents output", err)
}

func normalizeExtensions(raw string) []string {
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	extensions := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(strings.ToLower(part))
		if part == "" {
			continue
		}
		if !strings.HasPrefix(part, ".") {
			part = "." + part
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		extensions = append(extensions, part)
	}
	return extensions
}
