package cli

import (
	"errors"
	"flag"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/vladimirkasterin/ctx/internal/config"
)

type Command struct {
	Name         string
	Root         string
	Query        string
	Depth        int
	OutputMode   string
	Limit        int
	Note         string
	ProjectArg   string
	ProjectsVerb string
	FromSnapshot int64
	ToSnapshot   int64
	Report       config.Options
}

const (
	OutputHuman = "human"
	OutputAI    = "ai"
)

func Parse(args []string) (Command, error) {
	if len(args) == 0 || shouldTreatAsLegacyReport(args[0]) {
		report, err := parseLegacyReport(args)
		if err != nil {
			return Command{}, err
		}
		return Command{
			Name:   "report",
			Root:   report.Root,
			Report: report,
		}, nil
	}

	switch args[0] {
	case "index":
		return parseRootCommand("index", args[1:])
	case "update":
		return parseRootCommand("update", args[1:])
	case "shell":
		return parseShell(args[1:])
	case "status":
		return parseStatus(args[1:])
	case "symbol":
		return parseSymbol(args[1:])
	case "impact":
		return parseImpact(args[1:])
	case "diff":
		return parseDiff(args[1:])
	case "projects":
		return parseProjects(args[1:])
	case "report":
		return parseReport(args[1:])
	case "dump":
		report, err := parseLegacyReport(args[1:])
		if err != nil {
			return Command{}, err
		}
		return Command{
			Name:   "legacy-report",
			Root:   report.Root,
			Report: report,
		}, nil
	case "help", "-h", "--help":
		return Command{}, usageError(nil)
	default:
		return Command{}, usageError(fmt.Errorf("unknown command %q", args[0]))
	}
}

func parseRootCommand(name string, args []string) (Command, error) {
	root := "."
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		root = args[0]
		args = args[1:]
	}

	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	var note string
	fs.StringVar(&note, "note", "", "optional snapshot note")
	fs.Usage = func() {}

	if err := fs.Parse(args); err != nil {
		return Command{}, usageError(err)
	}

	remaining := fs.Args()
	if len(remaining) > 1 {
		return Command{}, usageError(errors.New("only one path can be provided"))
	}
	if len(remaining) == 1 {
		root = remaining[0]
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Command{}, fmt.Errorf("resolve root path: %w", err)
	}

	return Command{
		Name:       name,
		Root:       absRoot,
		Note:       note,
		OutputMode: OutputHuman,
	}, nil
}

func parseStatus(args []string) (Command, error) {
	root := "."
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		root = args[0]
		args = args[1:]
	}

	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	var ai bool
	var human bool
	bindOutputModeFlags(fs, &ai, &human)
	fs.Usage = func() {}

	if err := fs.Parse(args); err != nil {
		return Command{}, usageError(err)
	}

	remaining := fs.Args()
	if len(remaining) > 1 {
		return Command{}, usageError(errors.New("only one path can be provided"))
	}
	if len(remaining) == 1 {
		root = remaining[0]
	}

	mode, err := resolveOutputMode(ai, human)
	if err != nil {
		return Command{}, usageError(err)
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Command{}, fmt.Errorf("resolve root path: %w", err)
	}

	return Command{
		Name:       "status",
		Root:       absRoot,
		OutputMode: mode,
	}, nil
}

func parseSymbol(args []string) (Command, error) {
	query := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		query = args[0]
		args = args[1:]
	}

	fs := flag.NewFlagSet("symbol", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	var root string
	var ai bool
	var human bool
	fs.StringVar(&root, "root", ".", "project root or any path inside the project")
	bindOutputModeFlags(fs, &ai, &human)
	fs.Usage = func() {}

	if err := fs.Parse(args); err != nil {
		return Command{}, usageError(err)
	}

	remaining := fs.Args()
	if query == "" {
		if len(remaining) != 1 {
			return Command{}, usageError(errors.New("symbol command requires exactly one query"))
		}
		query = remaining[0]
		remaining = remaining[1:]
	}
	if len(remaining) != 0 {
		return Command{}, usageError(errors.New("symbol command requires exactly one query"))
	}

	mode, err := resolveOutputMode(ai, human)
	if err != nil {
		return Command{}, usageError(err)
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Command{}, fmt.Errorf("resolve root path: %w", err)
	}

	return Command{
		Name:       "symbol",
		Root:       absRoot,
		Query:      query,
		OutputMode: mode,
	}, nil
}

func parseImpact(args []string) (Command, error) {
	query := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		query = args[0]
		args = args[1:]
	}

	fs := flag.NewFlagSet("impact", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	var root string
	var depth int
	var ai bool
	var human bool
	fs.StringVar(&root, "root", ".", "project root or any path inside the project")
	fs.IntVar(&depth, "depth", 3, "transitive caller depth")
	bindOutputModeFlags(fs, &ai, &human)
	fs.Usage = func() {}

	if err := fs.Parse(args); err != nil {
		return Command{}, usageError(err)
	}

	remaining := fs.Args()
	if query == "" {
		if len(remaining) != 1 {
			return Command{}, usageError(errors.New("impact command requires exactly one query"))
		}
		query = remaining[0]
		remaining = remaining[1:]
	}
	if len(remaining) != 0 {
		return Command{}, usageError(errors.New("impact command requires exactly one query"))
	}

	mode, err := resolveOutputMode(ai, human)
	if err != nil {
		return Command{}, usageError(err)
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Command{}, fmt.Errorf("resolve root path: %w", err)
	}

	return Command{
		Name:       "impact",
		Root:       absRoot,
		Query:      query,
		Depth:      depth,
		OutputMode: mode,
	}, nil
}

func parseShell(args []string) (Command, error) {
	query := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		query = args[0]
		args = args[1:]
	}

	fs := flag.NewFlagSet("shell", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	var root string
	fs.StringVar(&root, "root", ".", "project root or any path inside the project")
	fs.Usage = func() {}

	if err := fs.Parse(args); err != nil {
		return Command{}, usageError(err)
	}

	remaining := fs.Args()
	if query == "" && len(remaining) > 0 {
		query = remaining[0]
		remaining = remaining[1:]
	}
	if len(remaining) != 0 {
		return Command{}, usageError(errors.New("shell accepts at most one query"))
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Command{}, fmt.Errorf("resolve root path: %w", err)
	}

	return Command{
		Name:       "shell",
		Root:       absRoot,
		Query:      query,
		OutputMode: OutputHuman,
	}, nil
}

func parseReport(args []string) (Command, error) {
	if shouldUseLegacyReport(args) {
		report, err := parseLegacyReport(stripLegacyReportFlags(args))
		if err != nil {
			return Command{}, err
		}
		return Command{
			Name:   "legacy-report",
			Root:   report.Root,
			Report: report,
		}, nil
	}

	root := "."
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		root = args[0]
		args = args[1:]
	}

	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	var ai bool
	var human bool
	var limit int
	fs.IntVar(&limit, "limit", 8, "number of entries per summary section")
	bindOutputModeFlags(fs, &ai, &human)
	fs.Usage = func() {}

	if err := fs.Parse(args); err != nil {
		return Command{}, usageError(err)
	}

	remaining := fs.Args()
	if len(remaining) > 1 {
		return Command{}, usageError(errors.New("only one path can be provided"))
	}
	if len(remaining) == 1 {
		root = remaining[0]
	}

	mode, err := resolveOutputMode(ai, human)
	if err != nil {
		return Command{}, usageError(err)
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Command{}, fmt.Errorf("resolve root path: %w", err)
	}

	return Command{
		Name:       "report",
		Root:       absRoot,
		OutputMode: mode,
		Limit:      limit,
	}, nil
}

func parseDiff(args []string) (Command, error) {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	var root string
	var fromID int64
	var toID int64
	fs.StringVar(&root, "root", ".", "project root or any path inside the project")
	fs.Int64Var(&fromID, "from", 0, "from snapshot id")
	fs.Int64Var(&toID, "to", 0, "to snapshot id")
	fs.Usage = func() {}

	if err := fs.Parse(args); err != nil {
		return Command{}, usageError(err)
	}

	remaining := fs.Args()
	if len(remaining) > 0 {
		return Command{}, usageError(errors.New("diff does not accept positional arguments"))
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Command{}, fmt.Errorf("resolve root path: %w", err)
	}

	return Command{
		Name:         "diff",
		Root:         absRoot,
		FromSnapshot: fromID,
		ToSnapshot:   toID,
	}, nil
}

func parseProjects(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{Name: "projects", ProjectsVerb: "list"}, nil
	}

	verb := args[0]
	switch verb {
	case "list":
		if len(args) != 1 {
			return Command{}, usageError(errors.New("projects list does not accept extra arguments"))
		}
		return Command{Name: "projects", ProjectsVerb: "list"}, nil
	case "rm":
		if len(args) != 2 {
			return Command{}, usageError(errors.New("projects rm requires a project id or root path"))
		}
		return Command{Name: "projects", ProjectsVerb: "rm", ProjectArg: args[1]}, nil
	case "prune":
		if len(args) != 1 {
			return Command{}, usageError(errors.New("projects prune does not accept extra arguments"))
		}
		return Command{Name: "projects", ProjectsVerb: "prune"}, nil
	default:
		return Command{}, usageError(fmt.Errorf("unknown projects subcommand %q", verb))
	}
}

func parseLegacyReport(args []string) (config.Options, error) {
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
	message := `ctx: local Go code intelligence for project exploration.

Modes:
  Human mode is the default. For explicit human-friendly output use -h or -human on supported commands.
  AI mode uses -a or -ai for compact token-efficient output.

Usage:
  ctx [path] [legacy report flags]
  ctx shell [query] [--root path]
  ctx dump [path] [legacy report flags]
  ctx index [path] [--note text]
  ctx update [path] [--note text]
  ctx status [path] [-h|-human|-a|-ai]
  ctx symbol <query> [--root path] [-h|-human|-a|-ai]
  ctx impact <query> [--root path] [--depth N] [-h|-human|-a|-ai]
  ctx report [path] [-h|-human|-a|-ai] [-limit N]
  ctx diff [--root path] [--from N] [--to N]
  ctx projects [list|rm|prune]

Legacy report flags:
  -hidden            include hidden files and directories
  -max-file-size     maximum file size in bytes (default 2097152)
  -output            write report to a file
  -copy              copy report to the macOS clipboard
  -extensions        comma-separated extension filter, for example go,md,yaml
  -summary-only      print only summary statistics
  -no-tree           skip directory tree output
  -no-contents       skip file contents output`

	if err == nil {
		return errors.New(message)
	}
	return fmt.Errorf("%w\n\n%s", err, message)
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

func shouldTreatAsLegacyReport(firstArg string) bool {
	switch firstArg {
	case "index", "update", "shell", "status", "symbol", "impact", "diff", "projects", "report", "dump", "help", "-h", "--help":
		return false
	}
	if strings.HasPrefix(firstArg, "-") {
		return true
	}
	return firstArg == "." || strings.HasPrefix(firstArg, "/") || strings.HasPrefix(firstArg, "./") || strings.HasPrefix(firstArg, "../")
}

func bindOutputModeFlags(fs *flag.FlagSet, ai, human *bool) {
	fs.BoolVar(ai, "a", false, "compact AI-oriented output")
	fs.BoolVar(ai, "ai", false, "compact AI-oriented output")
	fs.BoolVar(human, "h", false, "human-oriented output")
	fs.BoolVar(human, "human", false, "human-oriented output")
}

func resolveOutputMode(ai, human bool) (string, error) {
	if ai && human {
		return "", errors.New("flags -ai and -human cannot be used together")
	}
	if ai {
		return OutputAI, nil
	}
	return OutputHuman, nil
}

func shouldUseLegacyReport(args []string) bool {
	for _, arg := range args {
		switch arg {
		case "-legacy", "--legacy", "-hidden", "-max-file-size", "-output", "-copy", "-extensions", "-summary-only", "-no-tree", "-no-contents":
			return true
		}
	}
	return false
}

func stripLegacyReportFlags(args []string) []string {
	result := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "-legacy" || arg == "--legacy" {
			continue
		}
		result = append(result, arg)
	}
	return result
}
