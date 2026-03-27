# ctx

`ctx` is a local Go, Python, and Rust code-intelligence CLI for understanding a project as a system.

Give it a repository and it helps you explore in flow:

- what this function does
- where this method lives
- who calls it
- what it calls
- what this type influences
- what may break if you change it
- which files and tests are relevant

`ctx` is not a web UI, not an IDE plugin, and not an AI wrapper pretending to be a tool.

It is a real local CLI with a persistent project index, snapshot history, incremental refresh, symbol graph queries, file journeys, and an exploration shell that is pleasant enough to actually use.

## Why It Exists

Most project-context tools do one of three things:

- dump files
- search text
- summarize code with AI

Those are useful, but they do not reliably answer the day-to-day engineering questions behind real code changes:

- What is this function really responsible for?
- Which package owns this behavior?
- Which callers matter?
- Is this a local helper or a project seam?
- What else should I read before editing this?
- Which tests are closest to the change?
- What area am I about to disturb?

`ctx` exists to answer those questions quickly, locally, and deterministically.

## Philosophy

The core idea behind `ctx` is simple:

> Give me a codebase, and I will help you read it like a map, not like a pile of files.

That means:

- a function is not just a declaration, but a contract, a location, a neighborhood, a set of callers, a set of callees, and an impact surface
- a file is not just text, but a small subsystem with shape, reach, hotspots, and navigable entities
- a project is not just a tree, but a living graph that can be explored in a human flow

The tool is optimized for:

- local-first workflows
- deterministic engineering value without mandatory AI
- fast repeated use on the same repository
- human-readable output
- AI-friendly compact output when you explicitly want to feed context into another tool

## What `ctx` Gives You

`ctx` indexes a Go project, a Python project, a Rust project, or a mixed repository locally and keeps the result in a persistent SQLite-backed store.

It can:

- build an index from the current Go, Python, Rust, or mixed project
- detect changes since the previous snapshot
- update the index incrementally
- query functions, methods, structs, interfaces, classes, files, and packages
- show signatures, declaration ranges, doc comments, callers, callees, refs, related symbols, and tests
- estimate impact surface
- compare snapshots
- provide a shell for “project reading in motion”

## Core Experience

The experience is built around a few strong flows.

### 1. Build the project map

```bash
ctx index .
ctx report .
ctx shell
```

Then inside the shell:

```text
tree dirs
open 1
```

### 2. Inspect one symbol deeply

```bash
ctx symbol CreateSession
ctx impact CreateSession
```

You get:

- declaration
- signature
- file and package
- callers
- callees
- refs
- related symbols
- tests
- package context

### 3. Enter exploration mode

```bash
ctx shell
```

From there you can move naturally:

- `tree`
- `tree dirs`
- `tree hot`
- `file internal/app/app.go`
- `search Login`
- `find session token`
- `grep 'Run\\('`
- `walk`
- `open 4`
- `callers`
- `source`
- `full`
- `impact`
- `back`
- `home`

### 4. Work with a changing repo

`ctx` keeps snapshots and refreshes itself incrementally. For common read commands, it can auto-refresh before answering if the working tree changed since the last snapshot. Repeated no-op checks reuse a stored change cache keyed by the current snapshot and scanned tree fingerprint.
Dependency lockfiles and metadata files are now treated more precisely too: `go.sum`, `Cargo.lock`, and Python project metadata no longer force a full reindex when the current local source graph is unchanged, while `go.mod` and `Cargo.toml` now use semantic manifest diffs to decide between no-op, partial package refresh, and full reindex.
On macOS, Linux, and Windows, `ctx watch` now prefers real filesystem events, coalesces event bursts, and falls back to polling when a native watcher cannot be established.

```bash
ctx update .
ctx watch .
ctx diff --from 4 --to 5
ctx status .
ctx snapshots
ctx snapshot 5
```

## Installation

### Build locally

```bash
make build
./bin/ctx --help
```

### Install as `ctx`

```bash
make install
```

or directly:

```bash
./scripts/install.sh
```

Reinstall after changes:

```bash
./scripts/reinstall.sh
```

Remove:

```bash
make uninstall
```

## Quick Start

Index the current project:

```bash
ctx index .
```

Read the project:

```bash
ctx report .
```

Inspect a symbol:

```bash
ctx symbol Parse
```

Estimate change impact:

```bash
ctx impact Parse
```

Enter the shell:

```bash
ctx shell
```

Choose an area first when the repository is large:

```text
tree dirs
open 3
tree hot
search text refresh token
```

## Commands

### `ctx index`

Create the first snapshot or force a fresh indexing pass.

```bash
ctx index .
ctx index . --note "baseline"
```

### `ctx update`

Refresh the index incrementally after local changes.

```bash
ctx update .
```

### `ctx watch`

Keep the snapshot fresh in a long-running loop. It prefers native filesystem events on macOS, Linux, and Windows when available, coalesces event bursts with `--debounce`, applies incremental updates when needed, and can stay quiet on repeated no-op states with `--quiet`.

```bash
ctx watch .
ctx watch . --interval 2s
ctx watch . --debounce 500ms --quiet
ctx watch . --interval 500ms --cycles 5 --explain
```

### `ctx status`

Show current snapshot, project inventory, latest index timings, and whether local changes exist.
Use `--explain` to see the incremental plan in the same explain format used by `update`, `watch`, `symbol`, `impact`, `diff`, `history`, `cochange`, and `report`.

```bash
ctx status .
ctx status . -ai
ctx status . --explain
```

### `ctx doctor`

Inspect project detection, config, schema health, DB quick-check status, snapshot timing, and incremental update readiness.

```bash
ctx doctor .
```

### `ctx report`

Show a high-level project map: important packages, files, symbols, and hotspots.
Quality is now ranked with graph signals plus recent change proximity and entrypoint heuristics.
Use `--explain` when you want the quality reasons and strongest indexed evidence behind each item.

```bash
ctx report .
ctx report . -limit 10
ctx report . -ai
ctx report . --explain
```

### `ctx symbol`

Show a connected view around a function, method, or type.
Use `--explain` for a concise "why this matters" summary with quality, precision, and strongest signals.

```bash
ctx symbol CreateSession
ctx symbol internal/auth.(*Service).Login
ctx symbol Parse -ai
ctx symbol Parse --explain
```

### `ctx impact`

Estimate who may be affected if a symbol changes.
Use `--explain` to see the expansion logic, recent deltas, and blast-radius caveats in the same explain section style used elsewhere.

```bash
ctx impact CreateSession
ctx impact internal/auth.(*Service).Login --depth 4
ctx impact internal/auth.(*Service).Login --depth 4 --explain
```

### `ctx diff`

Compare snapshots.
Use `--explain` for a summary of how the snapshot delta is interpreted and how impacted symbols are widened from direct changes.

```bash
ctx diff --from 4 --to 5
ctx diff --from 4 --to 5 --explain
```

### `ctx snapshots`

List stored snapshots for the current project, including stored scan/analyze/write timings.

```bash
ctx snapshots
ctx snapshots . -ai
```

### `ctx snapshot`

Inspect one snapshot, or the current snapshot if no id is provided, with stored inventory and timing telemetry.

```bash
ctx snapshot
ctx snapshot 5
ctx snapshot 5 --root .
```

### `ctx shell`

Enter an interactive reading flow over the indexed project.

```bash
ctx shell
ctx shell Parse
```

### `ctx projects`

Manage local project indexes.

```bash
ctx projects list
ctx projects rm <id-or-root>
ctx projects prune
```

### `ctx dump`

Legacy full-project dump mode for clipboard/export scenarios.

```bash
ctx dump
ctx dump . -copy
ctx dump . -output report.txt
ctx dump . -keep-empty -include-generated
```

## Human Output vs AI Output

Many read commands support two modes.

### Human mode

Default mode, also selectable with `-h` or `-human`.

Optimized for:

- readable sections
- richer labels
- project exploration
- shell-first workflows

### AI mode

Selectable with `-a` or `-ai`.

Optimized for:

- lower token count
- compact machine-friendly lines
- piping into external tools or prompts

## Shell Workflow

`ctx shell` is where the tool becomes a reading environment rather than a static command.

Useful commands inside the shell:

- `tree`
- `tree dirs`
- `tree hot`
- `tree up`
- `tree root`
- `file [path|n]`
- `search [symbol|text|regex] <query>`
- `find <query>`
- `grep <regex>`
- `walk`
- `callers`
- `callees`
- `refs`
- `tests`
- `related`
- `impact`
- `source`
- `full`
- `report`
- `back`
- `forward`
- `home`
- `quit`

The shell is designed around movement:

- project -> directory -> file
- file -> entity
- entity -> callers/callees/refs/tests
- entity -> full body
- back out when needed

Two details matter on large repositories:

- `tree dirs` gives you directory summaries with file-type counts like `py=12`, `go=8`, `md=3`
- plain text at the shell prompt runs the smart search flow, so you do not have to remember exact symbol names first

It should feel closer to exploring a system than manually opening random files in an editor tab maze.

## What Makes It Different

`ctx` tries to make each level of the repository legible:

- **project level**: what the repo contains and which areas dominate
- **package level**: what subsystem a package represents
- **file level**: why a file matters, how large it is, what shape it has, what its hotspots are
- **symbol level**: what a function, method, or type means in context

This is the heart of the tool: not just “show me code”, but “help me understand where I am”.

## Technical Notes

Today `ctx` is focused on Go, Python, and practical Rust support and uses a local persistent index built from the repository itself.

At a high level it relies on:

- `go/parser`
- `go/ast`
- `go/token`
- `go/types`
- `go/packages`
- Python's built-in `ast` module through a bundled local analyzer
- Cargo manifest discovery plus a local Rust source parser for crates, workspaces, files, packages, tests, and common symbols
- SQLite for local storage and snapshots

The important point is not the implementation detail, but the product behavior:

- deterministic local indexing
- incremental updates
- snapshot history
- queryable project graph

For Python analysis, `python3` needs to be available on your `PATH`.
Rust support is currently best-effort for symbols and tests and does not yet provide a full typed call/reference graph like Go.

## Current Scope

Current focus:

- local Go projects
- local Python projects
- local Rust projects
- mixed Go + Python + Rust repositories
- CLI-first workflows
- human shell exploration
- persistent project intelligence without a server

Known limitations:

- web UI
- mandatory AI dependency
- distributed indexing platform
- perfect Python type inference or runtime-aware dataflow
- perfect Rust macro expansion, trait-resolution-aware references, or a full rustc-quality semantic graph

## Example Reading Session

```bash
ctx index .
ctx shell
```

Then inside the shell:

```text
tree dirs
open 2
file internal/app/app.go
walk
next
full
callers
search text refresh token
tree hot
back
home
```

That is the intended posture of the tool:

not “run one command and leave”,
but “stay in flow and keep learning the shape of the system”.

## Legacy Dump Mode

The older file-dump behavior still exists as `ctx dump`.

By default it tries to stay practical and compact:

- respects `.gitignore` and `.ctxignore`
- skips empty or whitespace-only files
- skips generated files and obvious minified bundles
- skips low-value artifacts like `.gitkeep`, `*.log`, and `*.tmp`

Supported flags:

- `-hidden`
- `-max-file-size`
- `-output`
- `-copy`
- `-keep-empty`
- `-include-generated`
- `-include-minified`
- `-include-artifacts`
- `-extensions`
- `-summary-only`
- `-no-tree`
- `-no-contents`

This is still useful for clipboard export or full textual snapshots, but it is no longer the center of the product.

## Roadmap Direction

The next meaningful improvements are likely to be:

- stronger package-level travel
- better test/coverage integration
- denser shell layouts for big files
- more precise Python relationship recovery for dynamic flows
- more precise ranking of entrypoints vs helpers
- additional AI overlays for explanation on top of the deterministic core

But the foundation stays the same:

the main value of `ctx` should come from the tool itself, not from attaching a model to it.

## FAQ

Practical questions about Python support, smart search, big trees, and snapshot behavior live in [FAQ.md](FAQ.md).

## Summary

`ctx` is a tool for people who need to understand a codebase before they change it.

It helps you answer:

- what is this?
- why does it matter?
- where should I go next?
- what will this change touch?

If that sounds like your daily work, `ctx` is built for you.
