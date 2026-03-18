# ADR-004: Lazy fixpoint for out-of-order declarations

**Status:** accepted
**Date:** 2024-01-15

## Context

Go allows out-of-order top-level declarations: a function can reference a
type or variable declared later in the source. A single-pass compiler
cannot resolve forward references without either a pre-pass or a retry
mechanism.

## Decision

The compiler uses a lazy fixpoint loop:

1. `ScanDecls` splits the source into top-level declarations.
2. `RegisterFunc` pre-registers function signatures (name + type) without
   parsing bodies.
3. Each declaration is compiled. If it fails with `ErrUndefined`, it is
   deferred to the next round.
4. The loop repeats until either all declarations succeed or a round makes
   no progress (at which point the remaining `ErrUndefined` is a real
   error).

Progress is tracked by comparing the symbol table key set and the lengths
of `Data` and `Code` before and after each round. On failure, symbols and
code/data added during the failed attempt are rolled back via
`SymTracker`.

## Consequences

**Easier:**
- No separate declaration-ordering pass needed.
- Works naturally with incremental REPL evaluation.
- Simple to implement: just retry and compare.

**Harder:**
- Worst case is O(n^2) in the number of declarations (each round resolves
  at least one). Acceptable for typical program sizes.
- Rollback requires tracking every symbol and code/data change, adding
  bookkeeping to the compiler.
