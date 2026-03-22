# ctx

`ctx` is a local Go code-intelligence CLI for understanding a project as a system.

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

`ctx` indexes a Go project locally and keeps the result in a persistent SQLite-backed store.

It can:

- build an index from the current Go project
- detect changes since the previous snapshot
- update the index incrementally
- query functions, methods, structs, interfaces, files, and packages
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
ctx tree
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
- `file internal/app/app.go`
- `walk`
- `open 4`
- `callers`
- `source`
- `full`
- `impact`
- `back`
- `home`

### 4. Work with a changing repo

`ctx` keeps snapshots and refreshes itself incrementally. For common read commands, it can auto-refresh before answering if the working tree changed since the last snapshot.

```bash
ctx update .
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

### `ctx status`

Show current snapshot, project inventory, and whether local changes exist.

```bash
ctx status .
ctx status . -ai
```

### `ctx report`

Show a high-level project map: important packages, symbols, and hotspots.

```bash
ctx report .
ctx report . -limit 10
ctx report . -ai
```

### `ctx symbol`

Show a connected view around a function, method, or type.

```bash
ctx symbol CreateSession
ctx symbol internal/auth.(*Service).Login
ctx symbol Parse -ai
```

### `ctx impact`

Estimate who may be affected if a symbol changes.

```bash
ctx impact CreateSession
ctx impact internal/auth.(*Service).Login --depth 4
```

### `ctx diff`

Compare snapshots.

```bash
ctx diff --from 4 --to 5
```

### `ctx snapshots`

List stored snapshots for the current project.

```bash
ctx snapshots
ctx snapshots . -ai
```

### `ctx snapshot`

Inspect one snapshot, or the current snapshot if no id is provided.

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
- `file [path|n]`
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

- project -> file
- file -> entity
- entity -> callers/callees/refs/tests
- entity -> full body
- back out when needed

It should feel closer to exploring a system than manually opening random files in an editor tab maze.

## What Makes It Different

`ctx` tries to make each level of the repository legible:

- **project level**: what the repo contains and which areas dominate
- **package level**: what subsystem a package represents
- **file level**: why a file matters, how large it is, what shape it has, what its hotspots are
- **symbol level**: what a function, method, or type means in context

This is the heart of the tool: not just “show me code”, but “help me understand where I am”.

## Technical Notes

Today `ctx` is focused on Go and uses a local persistent index built from the repository itself.

At a high level it relies on:

- `go/parser`
- `go/ast`
- `go/token`
- `go/types`
- `go/packages`
- SQLite for local storage and snapshots

The important point is not the implementation detail, but the product behavior:

- deterministic local indexing
- incremental updates
- snapshot history
- queryable project graph

## Current Scope

Current focus:

- local Go projects
- CLI-first workflows
- human shell exploration
- persistent project intelligence without a server

Not the goal right now:

- web UI
- mandatory AI dependency
- distributed indexing platform
- multi-language platform from day one

## Example Reading Session

```bash
ctx index .
ctx shell
```

Then inside the shell:

```text
tree
file internal/app/app.go
walk
next
full
open-current
callers
impact
back
home
```

That is the intended posture of the tool:

not “run one command and leave”,
but “stay in flow and keep learning the shape of the system”.

## Legacy Dump Mode

The older file-dump behavior still exists as `ctx dump`.

Supported flags:

- `-hidden`
- `-max-file-size`
- `-output`
- `-copy`
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
- more precise ranking of entrypoints vs helpers
- additional AI overlays for explanation on top of the deterministic core

But the foundation stays the same:

the main value of `ctx` should come from the tool itself, not from attaching a model to it.

## Summary

`ctx` is a tool for people who need to understand a codebase before they change it.

It helps you answer:

- what is this?
- why does it matter?
- where should I go next?
- what will this change touch?

If that sounds like your daily work, `ctx` is built for you.
