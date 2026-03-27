package cli

import (
	"errors"
	"flag"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/vladimirkasterin/ctx/internal/config"
)

type Command struct {
	Name          string
	Root          string
	Query         string
	Scope         string
	Depth         int
	Explain       bool
	OutputMode    string
	Limit         int
	Note          string
	ProjectArg    string
	ProjectsVerb  string
	SnapshotsVerb string
	FromSnapshot  int64
	ToSnapshot    int64
	SnapshotID    int64
	SnapshotIDs   []int64
	SnapshotLimit int
	WatchInterval time.Duration
	WatchDebounce time.Duration
	WatchCycles   int
	WatchQuiet    bool
	Report        config.Options
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
	case "watch":
		return parseWatch(args[1:])
	case "status":
		return parseStatus(args[1:])
	case "doctor":
		return parseDoctor(args[1:])
	case "symbol":
		return parseSymbol(args[1:])
	case "impact":
		return parseImpact(args[1:])
	case "trace":
		return parseTrace(args[1:])
	case "handoff":
		return parseHandoff(args[1:])
	case "review":
		return parseReview(args[1:])
	case "history":
		return parseHistory(args[1:])
	case "cochange":
		return parseCoChange(args[1:])
	case "diff":
		return parseDiff(args[1:])
	case "snapshots":
		return parseSnapshots(args[1:])
	case "snapshot":
		return parseSnapshot(args[1:])
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
	var explain bool
	fs.StringVar(&note, "note", "", "optional snapshot note")
	fs.BoolVar(&explain, "explain", false, "print incremental plan details")
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
		Explain:    explain,
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
	var explain bool
	var projectArg string
	fs.StringVar(&projectArg, "project", "", "indexed project id, prefix, module path, or root path")
	fs.BoolVar(&explain, "explain", false, "print why files/packages are currently marked as changed")
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
		ProjectArg: projectArg,
		Explain:    explain,
		OutputMode: mode,
	}, nil
}

func parseDoctor(args []string) (Command, error) {
	root := "."
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		root = args[0]
		args = args[1:]
	}

	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})
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
		Name:       "doctor",
		Root:       absRoot,
		OutputMode: OutputHuman,
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
	var explain bool
	fs.StringVar(&root, "root", ".", "project root or any path inside the project")
	fs.BoolVar(&explain, "explain", false, "show why this symbol matters and where the strongest evidence comes from")
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
		Explain:    explain,
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
	var explain bool
	fs.StringVar(&root, "root", ".", "project root or any path inside the project")
	fs.IntVar(&depth, "depth", 3, "transitive caller depth")
	fs.BoolVar(&explain, "explain", false, "show why packages/files/tests are included in this blast radius")
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
		Explain:    explain,
		OutputMode: mode,
	}, nil
}

func parseTrace(args []string) (Command, error) {
	query := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		query = args[0]
		args = args[1:]
	}

	fs := flag.NewFlagSet("trace", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	var root string
	var depth int
	var limit int
	var ai bool
	var human bool
	var explain bool
	fs.StringVar(&root, "root", ".", "project root or any path inside the project")
	fs.IntVar(&depth, "depth", 4, "transitive caller depth")
	fs.IntVar(&limit, "limit", 6, "number of items to show per trace section")
	fs.BoolVar(&explain, "explain", false, "show why each trace branch is included and what the blind spots are")
	bindOutputModeFlags(fs, &ai, &human)
	fs.Usage = func() {}

	if err := fs.Parse(args); err != nil {
		return Command{}, usageError(err)
	}

	remaining := fs.Args()
	if query == "" {
		if len(remaining) != 1 {
			return Command{}, usageError(errors.New("trace command requires exactly one query"))
		}
		query = remaining[0]
		remaining = remaining[1:]
	}
	if len(remaining) != 0 {
		return Command{}, usageError(errors.New("trace command requires exactly one query"))
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
		Name:       "trace",
		Root:       absRoot,
		Query:      query,
		Depth:      depth,
		Limit:      limit,
		Explain:    explain,
		OutputMode: mode,
	}, nil
}

func parseHandoff(args []string) (Command, error) {
	query := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		query = args[0]
		args = args[1:]
	}

	fs := flag.NewFlagSet("handoff", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	var root string
	var limit int
	var ai bool
	var human bool
	var explain bool
	var packageScope bool
	var fileScope bool
	var symbolScope bool
	fs.StringVar(&root, "root", ".", "project root or any path inside the project")
	fs.IntVar(&limit, "limit", 6, "number of items to show per handoff section")
	fs.BoolVar(&explain, "explain", false, "show why ctx picked this area and how the handoff plan was built")
	fs.BoolVar(&packageScope, "package", false, "treat the query as a package/area")
	fs.BoolVar(&fileScope, "file", false, "treat the query as a file")
	fs.BoolVar(&symbolScope, "symbol", false, "treat the query as a symbol")
	bindOutputModeFlags(fs, &ai, &human)
	fs.Usage = func() {}

	if err := fs.Parse(args); err != nil {
		return Command{}, usageError(err)
	}

	remaining := fs.Args()
	if query == "" {
		if len(remaining) != 1 {
			return Command{}, usageError(errors.New("handoff command requires exactly one query"))
		}
		query = remaining[0]
		remaining = remaining[1:]
	}
	if len(remaining) != 0 {
		return Command{}, usageError(errors.New("handoff command requires exactly one query"))
	}
	if (packageScope && fileScope) || (packageScope && symbolScope) || (fileScope && symbolScope) {
		return Command{}, usageError(errors.New("handoff scope must be one of --symbol, --package, or --file"))
	}

	scope := "auto"
	switch {
	case packageScope:
		scope = "package"
	case fileScope:
		scope = "file"
	case symbolScope:
		scope = "symbol"
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
		Name:       "handoff",
		Root:       absRoot,
		Query:      query,
		Scope:      scope,
		Limit:      limit,
		Explain:    explain,
		OutputMode: mode,
	}, nil
}

func parseReview(args []string) (Command, error) {
	scope := "working-tree"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch strings.ToLower(strings.TrimSpace(args[0])) {
		case "working-tree", "workingtree", "working_tree", "wt":
			scope = "working-tree"
			args = args[1:]
		case "snapshot", "snapshots":
			scope = "snapshot"
			args = args[1:]
		}
	}

	fs := flag.NewFlagSet("review", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	var root string
	var fromID int64
	var toID int64
	var limit int
	var ai bool
	var human bool
	var explain bool
	fs.StringVar(&root, "root", ".", "project root or any path inside the project")
	fs.Int64Var(&fromID, "from", 0, "from snapshot id")
	fs.Int64Var(&toID, "to", 0, "to snapshot id")
	fs.IntVar(&limit, "limit", 6, "number of items to show per review section")
	fs.BoolVar(&explain, "explain", false, "show why ctx considers these areas risky or review-worthy")
	bindOutputModeFlags(fs, &ai, &human)
	fs.Usage = func() {}

	if err := fs.Parse(args); err != nil {
		return Command{}, usageError(err)
	}
	if len(fs.Args()) != 0 {
		return Command{}, usageError(errors.New("review does not accept extra positional arguments"))
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
		Name:         "review",
		Root:         absRoot,
		Scope:        scope,
		FromSnapshot: fromID,
		ToSnapshot:   toID,
		Limit:        limit,
		Explain:      explain,
		OutputMode:   mode,
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

func parseWatch(args []string) (Command, error) {
	root := "."
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		root = args[0]
		args = args[1:]
	}

	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	var interval time.Duration
	var debounce time.Duration
	var cycles int
	var explain bool
	var quiet bool
	fs.DurationVar(&interval, "interval", 2*time.Second, "poll interval between watch cycles")
	fs.DurationVar(&debounce, "debounce", 250*time.Millisecond, "coalesce a burst of filesystem events before reindexing")
	fs.IntVar(&cycles, "cycles", 0, "number of watch cycles to run before exiting (0 means run until interrupted)")
	fs.BoolVar(&explain, "explain", false, "print incremental plan details when watch applies an update")
	fs.BoolVar(&quiet, "quiet", false, "suppress idle watch cycles")
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
	if interval <= 0 {
		return Command{}, usageError(errors.New("watch interval must be greater than zero"))
	}
	if debounce < 0 {
		return Command{}, usageError(errors.New("watch debounce cannot be negative"))
	}
	if cycles < 0 {
		return Command{}, usageError(errors.New("watch cycles cannot be negative"))
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Command{}, fmt.Errorf("resolve root path: %w", err)
	}

	return Command{
		Name:          "watch",
		Root:          absRoot,
		Explain:       explain,
		OutputMode:    OutputHuman,
		WatchInterval: interval,
		WatchDebounce: debounce,
		WatchCycles:   cycles,
		WatchQuiet:    quiet,
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
	scope := "project"
	rootSet := false
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		if normalized, ok := parseReportScopeToken(args[0]); ok {
			scope = normalized
		} else {
			root = args[0]
			rootSet = true
		}
		args = args[1:]
	}

	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	var ai bool
	var human bool
	var limit int
	var rootFlag string
	var explain bool
	fs.StringVar(&rootFlag, "root", root, "project root or any path inside the project")
	fs.IntVar(&limit, "limit", 8, "number of entries per summary section")
	fs.BoolVar(&explain, "explain", false, "show provenance behind ranked packages, symbols, and tests")
	bindOutputModeFlags(fs, &ai, &human)
	fs.Usage = func() {}

	if err := fs.Parse(args); err != nil {
		return Command{}, usageError(err)
	}

	remaining := fs.Args()
	root = rootFlag
	for _, value := range remaining {
		if normalized, ok := parseReportScopeToken(value); ok && scope == "project" {
			scope = normalized
			continue
		}
		if !rootSet {
			root = value
			rootSet = true
			continue
		}
		return Command{}, usageError(errors.New("report accepts at most one scope and one path"))
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
		Scope:      scope,
		Explain:    explain,
		OutputMode: mode,
		Limit:      limit,
	}, nil
}

func parseReportScopeToken(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "project", "root":
		return "project", true
	case "risky", "risk":
		return "risky", true
	case "seams", "seam":
		return "seams", true
	case "hotspots", "hotspot", "hot":
		return "hotspots", true
	case "low-tested", "lowtested", "low_tests":
		return "low-tested", true
	case "changed-since", "changedsince", "changed", "changes":
		return "changed-since", true
	default:
		return "", false
	}
}

func parseDiff(args []string) (Command, error) {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	var root string
	var fromID int64
	var toID int64
	var explain bool
	fs.StringVar(&root, "root", ".", "project root or any path inside the project")
	fs.Int64Var(&fromID, "from", 0, "from snapshot id")
	fs.Int64Var(&toID, "to", 0, "to snapshot id")
	fs.BoolVar(&explain, "explain", false, "show how snapshot deltas are interpreted and ranked")
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
		Explain:      explain,
	}, nil
}

func parseHistory(args []string) (Command, error) {
	query := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		query = args[0]
		args = args[1:]
	}

	fs := flag.NewFlagSet("history", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	var root string
	var limit int
	var packageScope bool
	var symbolScope bool
	var explain bool
	fs.StringVar(&root, "root", ".", "project root or any path inside the project")
	fs.IntVar(&limit, "limit", 8, "number of recent history events to show")
	fs.BoolVar(&packageScope, "package", false, "treat the query as a package")
	fs.BoolVar(&symbolScope, "symbol", false, "treat the query as a symbol")
	fs.BoolVar(&explain, "explain", false, "show how history events are derived from stored snapshot diffs")
	fs.Usage = func() {}

	if err := fs.Parse(args); err != nil {
		return Command{}, usageError(err)
	}

	remaining := fs.Args()
	if query == "" {
		if len(remaining) != 1 {
			return Command{}, usageError(errors.New("history command requires exactly one query"))
		}
		query = remaining[0]
		remaining = remaining[1:]
	}
	if len(remaining) != 0 {
		return Command{}, usageError(errors.New("history command requires exactly one query"))
	}
	if packageScope && symbolScope {
		return Command{}, usageError(errors.New("history scope must be either --package or --symbol"))
	}

	scope := "symbol"
	if packageScope {
		scope = "package"
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Command{}, fmt.Errorf("resolve root path: %w", err)
	}

	return Command{
		Name:    "history",
		Root:    absRoot,
		Query:   query,
		Scope:   scope,
		Limit:   limit,
		Explain: explain,
	}, nil
}

func parseCoChange(args []string) (Command, error) {
	query := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		query = args[0]
		args = args[1:]
	}

	fs := flag.NewFlagSet("cochange", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	var root string
	var limit int
	var packageScope bool
	var symbolScope bool
	var explain bool
	fs.StringVar(&root, "root", ".", "project root or any path inside the project")
	fs.IntVar(&limit, "limit", 8, "number of co-change candidates to show")
	fs.BoolVar(&packageScope, "package", false, "treat the query as a package")
	fs.BoolVar(&symbolScope, "symbol", false, "treat the query as a symbol")
	fs.BoolVar(&explain, "explain", false, "show how co-change candidates are scored and what the limits are")
	fs.Usage = func() {}

	if err := fs.Parse(args); err != nil {
		return Command{}, usageError(err)
	}

	remaining := fs.Args()
	if query == "" {
		if len(remaining) != 1 {
			return Command{}, usageError(errors.New("cochange command requires exactly one query"))
		}
		query = remaining[0]
		remaining = remaining[1:]
	}
	if len(remaining) != 0 {
		return Command{}, usageError(errors.New("cochange command requires exactly one query"))
	}
	if packageScope && symbolScope {
		return Command{}, usageError(errors.New("cochange scope must be either --package or --symbol"))
	}

	scope := "symbol"
	if packageScope {
		scope = "package"
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Command{}, fmt.Errorf("resolve root path: %w", err)
	}

	return Command{
		Name:    "cochange",
		Root:    absRoot,
		Query:   query,
		Scope:   scope,
		Limit:   limit,
		Explain: explain,
	}, nil
}

func parseSnapshots(args []string) (Command, error) {
	if len(args) > 0 {
		switch args[0] {
		case "list", "rm", "limit":
			return parseSnapshotsSubcommand(args)
		}
	}

	root := "."
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		root = args[0]
		args = args[1:]
	}

	fs := flag.NewFlagSet("snapshots", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	var ai bool
	var human bool
	var projectArg string
	fs.StringVar(&projectArg, "project", "", "indexed project id, prefix, module path, or root path")
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
		Name:          "snapshots",
		Root:          absRoot,
		ProjectArg:    projectArg,
		SnapshotsVerb: "list",
		OutputMode:    mode,
	}, nil
}

func parseSnapshotsSubcommand(args []string) (Command, error) {
	verb := args[0]
	args = args[1:]

	fs := flag.NewFlagSet("snapshots "+verb, flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	var root string
	var projectArg string
	var ai bool
	var human bool
	fs.StringVar(&root, "root", ".", "project root or any path inside the project")
	fs.StringVar(&projectArg, "project", "", "indexed project id, prefix, module path, or root path")
	bindOutputModeFlags(fs, &ai, &human)
	fs.Usage = func() {}

	if err := fs.Parse(args); err != nil {
		return Command{}, usageError(err)
	}

	mode, err := resolveOutputMode(ai, human)
	if err != nil {
		return Command{}, usageError(err)
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Command{}, fmt.Errorf("resolve root path: %w", err)
	}

	command := Command{
		Name:          "snapshots",
		Root:          absRoot,
		ProjectArg:    projectArg,
		SnapshotsVerb: verb,
		OutputMode:    mode,
	}

	remaining := fs.Args()
	switch verb {
	case "list":
		if len(remaining) != 0 {
			return Command{}, usageError(errors.New("snapshots list does not accept extra arguments"))
		}
	case "rm":
		if len(remaining) == 0 {
			return Command{}, usageError(errors.New("snapshots rm requires snapshot ids or 'all'"))
		}
		if len(remaining) == 1 && remaining[0] == "all" {
			command.Query = "all"
			return command, nil
		}
		command.SnapshotIDs = make([]int64, 0, len(remaining))
		for _, raw := range remaining {
			value, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				return Command{}, usageError(fmt.Errorf("snapshot id must be a number: %w", err))
			}
			command.SnapshotIDs = append(command.SnapshotIDs, value)
		}
	case "limit":
		if len(remaining) != 1 {
			return Command{}, usageError(errors.New("snapshots limit requires exactly one numeric value"))
		}
		value, err := strconv.Atoi(remaining[0])
		if err != nil {
			return Command{}, usageError(fmt.Errorf("snapshot limit must be a number: %w", err))
		}
		if value < 0 {
			return Command{}, usageError(errors.New("snapshot limit cannot be negative"))
		}
		command.SnapshotLimit = value
	default:
		return Command{}, usageError(fmt.Errorf("unknown snapshots subcommand %q", verb))
	}

	return command, nil
}

func parseSnapshot(args []string) (Command, error) {
	var snapshotID int64
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		value, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return Command{}, usageError(fmt.Errorf("snapshot id must be a number: %w", err))
		}
		snapshotID = value
		args = args[1:]
	}

	fs := flag.NewFlagSet("snapshot", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	var root string
	var projectArg string
	var ai bool
	var human bool
	fs.StringVar(&root, "root", ".", "project root or any path inside the project")
	fs.StringVar(&projectArg, "project", "", "indexed project id, prefix, module path, or root path")
	bindOutputModeFlags(fs, &ai, &human)
	fs.Usage = func() {}

	if err := fs.Parse(args); err != nil {
		return Command{}, usageError(err)
	}

	remaining := fs.Args()
	if snapshotID == 0 && len(remaining) > 0 {
		value, err := strconv.ParseInt(remaining[0], 10, 64)
		if err != nil {
			return Command{}, usageError(fmt.Errorf("snapshot id must be a number: %w", err))
		}
		snapshotID = value
		remaining = remaining[1:]
	}
	if len(remaining) != 0 {
		return Command{}, usageError(errors.New("snapshot accepts at most one snapshot id"))
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
		Name:       "snapshot",
		Root:       absRoot,
		ProjectArg: projectArg,
		SnapshotID: snapshotID,
		OutputMode: mode,
	}, nil
}

func parseProjects(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{Name: "projects", ProjectsVerb: "list"}, nil
	}
	if len(args) == 1 && args[0] == "--dev-reset" {
		return Command{Name: "projects", ProjectsVerb: "dev-reset"}, nil
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
	case "dev-reset":
		if len(args) != 1 {
			return Command{}, usageError(errors.New("projects dev-reset does not accept extra arguments"))
		}
		return Command{Name: "projects", ProjectsVerb: "dev-reset"}, nil
	case "status":
		if len(args) != 2 {
			return Command{}, usageError(errors.New("projects status requires a project id or root path"))
		}
		return Command{Name: "projects", ProjectsVerb: "status", ProjectArg: args[1]}, nil
	case "snapshots":
		if len(args) < 3 {
			return Command{}, usageError(errors.New("projects snapshots requires a project id and snapshots subcommand"))
		}
		command, err := parseSnapshotsSubcommand(append(args[2:], "--project", args[1]))
		if err != nil {
			return Command{}, err
		}
		return command, nil
	default:
		return Command{}, usageError(fmt.Errorf("unknown projects subcommand %q", verb))
	}
}

func parseLegacyReport(args []string) (config.Options, error) {
	var options config.Options

	fs := flag.NewFlagSet("ctx", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})

	fs.BoolVar(&options.IncludeHidden, "hidden", false, "include hidden files and directories")
	fs.Int64Var(&options.MaxFileSize, "max-file-size", -1, "maximum file size in bytes (0 disables the limit)")
	fs.StringVar(&options.OutputPath, "output", "", "write report to a file instead of stdout")
	fs.BoolVar(&options.CopyToClipboard, "copy", false, "copy report to the macOS clipboard")
	fs.BoolVar(&options.Explain, "explain", false, "include effective filters and file decision reasons")
	fs.BoolVar(&options.KeepEmpty, "keep-empty", false, "keep empty and whitespace-only files in dump output")
	fs.BoolVar(&options.IncludeGenerated, "include-generated", false, "include generated files that dump skips by default")
	fs.BoolVar(&options.IncludeMinified, "include-minified", false, "include minified bundle-like files that dump skips by default")
	fs.BoolVar(&options.IncludeArtifacts, "include-artifacts", false, "include low-value artifacts like .gitkeep, *.log, and *.tmp")
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
	options.ConfigProfile = "dump"
	cliExtensions := normalizeExtensions(options.ExtensionsRaw)
	cfg, err := config.LoadProject(absRoot)
	if err != nil {
		return config.Options{}, err
	}
	options = config.ApplyProfile(options, cfg, "dump")
	if config.HasConfigFile(cfg) {
		options.ConfigPath = cfg.Path
	}
	if len(cliExtensions) > 0 {
		options.Extensions = cliExtensions
	}
	if options.MaxFileSize < 0 {
		options.MaxFileSize = 2 * 1024 * 1024
	}

	return options, nil
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}

const usageMessage = `ctx: local Go and Python code intelligence for exploring a project as a system.

Philosophy:
  Give ctx a Go codebase, a Python codebase, a Rust codebase, or a mixed repository and it helps you read it in flow:
  find a function, class, or method, understand its contract, inspect its callers and callees,
  see where it lives, what depends on it, which tests are nearby, and move on.

  ctx is built to be useful even without AI:
  indexing, symbol lookup, impact analysis, file/package context, shell navigation,
  snapshots, and project diffs all work as deterministic local features.

Quick Start:
  ctx index .                 build the first project snapshot
  ctx symbol CreateSession    inspect one symbol deeply
  ctx impact CreateSession    estimate blast radius
  ctx report .                get a project map
  ctx shell                   enter the exploration shell
  ctx watch .                 keep snapshots fresh in the background
  ctx history CreateSession   inspect symbol or package change history
  ctx cochange CreateSession  find files and packages that change with it

  Then inside the shell:
    tree dirs                 choose an area first on large repositories
    search Login              fuzzy-find symbols or text
    grep 'Run\('              regex-search indexed files

Usage:
  ctx [path] [legacy report flags]
  ctx dump [path] [legacy report flags]
  ctx index [path] [--note text]
  ctx update [path] [--note text]
  ctx status [path] [--explain] [-h|-human|-a|-ai]
  ctx doctor [path]
  ctx report [project|risky|seams|hotspots|low-tested|changed-since] [path] [--explain] [-h|-human|-a|-ai] [-limit N]
  ctx symbol <query> [--root path] [--explain] [-h|-human|-a|-ai]
  ctx impact <query> [--root path] [--depth N] [--explain] [-h|-human|-a|-ai]
  ctx history <query> [--root path] [--package|--symbol] [--limit N] [--explain]
  ctx cochange <query> [--root path] [--package|--symbol] [--limit N] [--explain]
  ctx diff [--root path] [--from N] [--to N] [--explain]
  ctx snapshots [path] [-h|-human|-a|-ai]
  ctx snapshots list [--root path|--project id] [-h|-human|-a|-ai]
  ctx snapshots rm <id...|all> [--root path|--project id]
  ctx snapshots limit <n> [--root path|--project id]
  ctx snapshot [id] [--root path] [-h|-human|-a|-ai]
  ctx shell [query] [--root path]
  ctx watch [path] [--interval 2s] [--debounce 250ms] [--cycles N] [--quiet] [--explain]
  ctx projects [list|rm|prune|status]
  ctx projects --dev-reset
  ctx projects snapshots <project> [list|rm|limit] ...

Core Commands:
  index      create or rebuild a snapshot-backed index for the current Go, Python, Rust, or mixed project
  update     incrementally refresh the index after local code changes
  watch      keep the snapshot fresh with native filesystem events when available, otherwise polling, and apply incremental updates
  status     show snapshot freshness, inventory, and latest index timing telemetry
  doctor     inspect project detection, config, schema/storage health, and incremental readiness diagnostics
  report     summarize the project or deterministic slices like risky/seams/hotspots/low-tested/changed-since, with optional provenance
  symbol     show declaration, signature, context, refs, callers, callees, tests, impact
  impact     show what may be affected if a symbol changes
  history    trace when a symbol or package was introduced and how it changed across snapshots
  cochange   find files and packages that often move together in snapshot diffs
  diff       compare snapshots and see what changed
  snapshots  list and manage stored snapshots for the project
  snapshot   inspect one snapshot and its stored inventory
  shell      explore the project in a human-oriented flow shell
  projects   inspect and clean up locally indexed projects and jump into one globally; --dev-reset wipes all local project DBs
  dump       legacy full file/context dump for clipboard or external tooling

Output Modes:
  Human mode is the default. Use -h or -human for clear, sectioned, readable output.
  AI mode uses -a or -ai for compact, token-efficient output that is easy to pipe into tools.

Shell Flow:
  tree [dirs|hot|next|prev|page <n>|up|root]
                  browse files, directory summaries, and hot files
  file [path|n]   inspect a file as a map of entities
  walk            move through file entities one by one
  search [symbol|text|regex] <query>
                  fuzzy-search symbols, search indexed file text, or run regex
  find <query>    run the combined symbol + text search
  grep <regex>    regex-search indexed files directly
  callers/callees follow graph edges
  refs/tests      inspect usage and verification context
  source/full     preview or fully open the current entity body
  tree/home/report
                  move between project, directory, file, and entity perspectives

Runtime Notes:
  Python analysis requires python3 on PATH.
  Rust analysis is local and practical-first: files/packages/tests plus best-effort symbols today, not a full typed semantic graph yet.
  Incremental invalidation is language-aware: lockfiles and some metadata changes no longer force full reindex when the local indexed graph is unchanged.
  watch prefers native filesystem events on macOS, Linux, and Windows, coalesces event bursts with --debounce, and falls back to polling elsewhere or when native setup fails.
  .ctxconfig can supply global or profile-specific settings for dump/index/report and path include/exclude rules.
  search and grep operate on indexed files from the current snapshot.

Install:
  make build
  make install
  ./scripts/install.sh
  ./scripts/reinstall.sh

Legacy dump flags:
  -hidden            include hidden files and directories
  -max-file-size     maximum file size in bytes (default 2097152)
  -output            write report to a file
  -copy              copy report to the macOS clipboard
  -explain           include effective filters and file decision reasons
  -keep-empty        include empty and whitespace-only files
  -include-generated include generated files
  -include-minified  include minified bundle-like files
  -include-artifacts include low-value artifacts like .gitkeep, *.log, and *.tmp
  -extensions        comma-separated extension filter, for example go,md,yaml
  -summary-only      print only summary statistics
  -no-tree           skip directory tree output
  -no-contents       skip file contents output`

func usageError(err error) error {
	if err == nil {
		return errors.New(usageMessage)
	}
	return fmt.Errorf("%w\n\n%s", err, usageMessage)
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
	case "index", "update", "shell", "watch", "status", "symbol", "impact", "history", "cochange", "diff", "snapshots", "snapshot", "projects", "report", "dump", "help", "-h", "--help":
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
		case "-legacy", "--legacy", "-hidden", "-max-file-size", "-output", "-copy", "-keep-empty", "-include-generated", "-include-minified", "-include-artifacts", "-extensions", "-summary-only", "-no-tree", "-no-contents":
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
