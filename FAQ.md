# ctx FAQ

## What languages does `ctx` support?

`ctx` supports Go, Python, and mixed Go + Python repositories.

For Go it builds a typed project graph from the local repository.
For Python it uses a local AST-based analyzer to expose files, functions, classes, methods, imports, tests, and common call relationships.

## Does Python support match Go exactly?

Not exactly.

The day-to-day navigation experience is meant to feel the same: file journeys, symbol lookup, search, tree navigation, related tests, and mixed-project indexing all work for Python too.

The main difference is precision. Go analysis is type-driven. Python analysis is heuristic in places because the language is dynamic.

## Which Python relationships are usually reliable?

The strongest cases today are:

- direct imported-function calls
- `self.method(...)`
- `Class.method(...)`
- common local instance flows like `service = Service(); service.run()`
- straightforward attribute assignment flows like `self.client = client`

## Which Python cases are still approximate?

These can still be partial or ambiguous:

- factory-returned instances with weak type clues
- heavy dynamic attribute mutation
- monkey patching
- reflection-like dispatch
- metaprogramming-heavy frameworks

If you see missing edges in one of those cases, that is a current analyzer limitation rather than a shell problem.

## Why do `search` and `grep` only scan indexed files?

They intentionally search the current snapshot instead of every file on disk.

That keeps the shell fast, deterministic, and aligned with the indexed project graph. It also avoids showing results from files that the current snapshot does not know how to navigate yet.

## How do I navigate huge repositories without paging through dozens of tree screens?

Start with directory mode:

```text
tree dirs
open 3
tree up
tree root
```

`tree dirs` summarizes each directory and shows extension counts like `go=14`, `py=9`, `md=4`.

Then use:

- `tree hot` to jump to important files
- `search <query>` or `find <query>` when you know the concept but not the exact symbol
- `grep <regex>` when you want text or pattern matches

## What is the difference between `search`, `find`, and `grep`?

- `search [symbol|text|regex] <query>` is the explicit smart-search command
- `find <query>` runs the combined symbol + text search
- `grep <regex>` runs regex search across indexed files

Inside the shell, plain text at the prompt also runs the smart search flow, so you can often just type the thing you are looking for.

## Why do I need `python3` installed?

Python analysis runs through a bundled local analyzer launched with `python3`.

If `python3` is missing from `PATH`, Go indexing still works, but Python files cannot be analyzed.

## When should I run `index` versus `update`?

Use:

- `ctx index .` for the first snapshot or a deliberate rebuild
- `ctx update .` after local changes when you want the stored snapshot refreshed

Many read flows can auto-refresh when the working tree changed, but `update` is still the explicit way to commit the new project state.
