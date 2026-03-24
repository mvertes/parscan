# ADR-004: Lazy fixpoint for out-of-order declarations

**Status:** accepted
**Date:** 2024-01-15

## Context

Go allows out-of-order top-level declarations: a function can reference a
type or variable declared later in the source. A single-pass compiler
cannot resolve forward references without either a pre-pass or a retry
mechanism.

## Decision

The compiler uses a two-phase approach, each with a lazy fixpoint loop:

**Phase 1 -- Declarations.** `ScanDecls` splits the source into top-level
declarations. Each is passed to `ParseDecl`, which resolves `package`,
`import`, `const`, `type`, and `var` (type registration only) and
registers function signatures via `registerFunc` without parsing bodies.
Declarations that fail with `ErrUndefined` are retried until convergence.
Unresolved declarations (func bodies, var initializers) are collected for
phase 2.

**Phase 2 -- Code generation.** Remaining declarations are fully parsed
(`ParseOneStmt`) and compiled (`generate`). Because all symbols are now
defined, retries are rare -- they only occur when a func body references
a var whose type is inferred from an initializer not yet compiled.

In both phases, the loop repeats until either all declarations succeed or
a round makes no progress (at which point the remaining `ErrUndefined` is
a real error).

On failure, rollback uses `SymTracker` (newly added symbol keys),
`savedSlots` (data slot updates to restore), and code/data length
checkpoints.

## Consequences

**Easier:**
- No topological sort or dependency graph needed -- the fixpoint retry
  resolves ordering naturally.
- Works naturally with incremental REPL evaluation.
- The two-phase split means phase 2 rarely retries, since all symbols
  are resolved by then.

**Harder:**
- Worst case is O(n^2) in the number of declarations (each round resolves
  at least one). Acceptable for typical program sizes.
- Rollback requires tracking symbols (`SymTracker`), data slot updates
  (`savedSlots`), and code/data lengths, adding bookkeeping to the
  compiler.
