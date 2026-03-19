# symbol

> Scoped symbol table for variables, types, functions, and labels.

## Overview

The `symbol` package provides `SymMap`, a flat map from scoped names to
`Symbol` entries. It is shared between the parser and compiler: the parser
populates it during parsing; the compiler reads it during code generation
to resolve addresses and types.

## Key types and functions

- **`SymMap`** (type `map[string]*Symbol`) -- symbol table. Keys are
  scoped names like `main/foo/x` or `0/int` (scope `0` for builtins).
- **`Symbol`** -- a table entry:
  - `Kind` -- one of `Value`, `Type`, `Label`, `Const`, `Var`, `Func`,
    `Pkg`, `Builtin`.
  - `Index` -- address in the VM data segment or frame.
  - `Local` -- whether the address is frame-relative.
  - `Type` -- `*vm.Type` for the symbol's runtime type.
  - `Captured` / `FreeVars` -- closure capture metadata.
- **`Kind`** (int enum) -- symbol classification.
- **`Get(name, scope string) (*Symbol, string, bool)`** -- lookup by
  walking from the innermost scope outward.
- **`MethodByName(sym, name) (*Symbol, []int)`** -- find a method on a
  type, including promoted methods from embedded fields.
- **`Init()`** -- populates builtin types (`int`, `string`, `bool`, ...),
  values (`nil`, `true`, `false`, `iota`), `println` (as `Value` with a
  native Go function), and builtin functions (`len`, `cap`, `append`,
  `copy`, `delete`, `new`, `make`, `panic`, `recover`, `trap`) with
  `Kind: Builtin`.

## Internal design

Symbol lookup walks the scope hierarchy: given scope `main/foo/for0` and
name `x`, it tries `main/foo/for0/x`, then `main/foo/x`, then `main/x`,
then `0/x` (builtins). First match wins.

Method lookup traverses embedded field chains to find promoted methods,
returning the field index path needed to reach the receiver.

## Dependencies

- `vm/` -- `Type`, `Value` structures.
