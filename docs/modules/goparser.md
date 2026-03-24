# goparser

> Go parser producing a flat token stream with control flow encoded as
> Label/Goto/JumpFalse -- no AST.

## Overview

The `goparser` package takes scanner tokens and produces a flat `Tokens`
slice suitable for single-pass code generation. It performs scope tracking,
type resolution, and expression rewriting (infix to postfix). It is the
most complex stage in the pipeline.

## Key types and functions

- **`Parser`** -- embeds `*scan.Scanner` and a `symbol.SymMap`. Tracks the
  current scope path, break/continue labels, closure state, and named
  return variables.
- **`Token`** -- extends `scan.Token` with an `Arg []any` field for
  label targets, type info, etc.
- **`Tokens`** -- slice of `Token` with helper methods (`Index`, `Split`,
  `SplitStart`).
- **`Parse(src string) (Tokens, error)`** -- full parse: scan, then
  parse all statements into a postfix token stream.
- **`ScanDecls(src string) ([]Tokens, error)`** -- scan and split source
  into top-level declaration token groups without parsing bodies.
- **`ParseDecl(toks Tokens) (handled bool, err error)`** -- resolve a
  single declaration during Phase 1 without emitting code. Delegates to
  `parsePackage`, `parseImports`, `parseConst`, `parseType`,
  `registerFunc`, or `parseVarDecl`. Returns `handled=false` when the
  declaration needs full parse + code generation (func bodies, var
  initializers).
- **`ParseOneStmt(toks Tokens) (Tokens, error)`** -- parse a single
  statement (used during compilation phase 2).
- **`registerFunc(toks Tokens) error`** -- register a function or method
  signature in the symbol table without parsing its body. For methods
  (`func (recv) Name(...)`), extracts the receiver type via
  `recvTypeName` and registers under `TypeName.MethodName`. Parses the
  signature in `typeOnly` mode to suppress parameter symbol registration.
- **`SplitAndSortVarDecls(decls []Tokens) []Tokens`** -- expands
  `var(...)` blocks into individual declarations and topologically sorts
  them by dependency (references between var initializers). Non-var
  declarations keep their original positions.
- **`recvTypeName(recvr Tokens) string`** -- extracts the type name from
  scanned receiver tokens (e.g. `"T"` from `(t T)`, `"*T"` from
  `(t *T)`).

### Error types

- **`ErrUndefined{Name}`** -- symbol not yet defined. The compiler catches
  this to trigger retry during Phase 1 declaration resolution.

## Internal design

### Expression parsing

`parseExpr` converts infix expressions to postfix using a shunting-yard
algorithm. Operator precedence and associativity come from `lang.TokenProps`.

### Control flow encoding

Instead of building an AST, control structures are lowered to
`Label`/`Goto`/`JumpFalse` tokens:

```
if cond { body }
-->  cond, JumpFalse(L1), body..., Label(L1)

for init; cond; post { body }
-->  init, Label(L0), cond, JumpFalse(L1), body..., post, Goto(L0), Label(L1)
```

Labels are scoped and auto-numbered (e.g. `for0`, `if1`) via `labelCount`.

### Scope tracking

Scopes are slash-separated paths pushed/popped as the parser enters/leaves
blocks. The scope path is used as a prefix key in `symbol.SymMap`.

### Closure analysis

When a function literal references a variable from an outer scope, the
parser marks that variable as `Captured` and records it in `FreeVars`.
This drives `HAlloc`/`HGet`/`HSet` emission during compilation.

### Method registration and receiver handling

`registerFunc` (Phase 1) and `parseFunc` (Phase 2) both handle methods.

In Phase 1, `registerFunc` detects the receiver `ParenBlock` before the
method name, scans it, and calls `recvTypeName` to extract the type name
(handling both value and pointer receivers). The method is registered under
`TypeName.MethodName` in the symbol table.

In Phase 2, `parseFunc` parses the full method body. A subtlety: calling
`parseParamTypes` on the receiver block registers a `LocalVar` symbol at
the outer scope, which may clobber an existing global symbol with the same
name. `parseFunc` saves the original symbol (`savedRecvOuter`) before
parsing, copies the receiver symbol into the function scope, then restores
(or deletes) the outer-scope entry.

### Variadic parameters

`parseParamTypes` detects `...T` syntax (the `Ellipsis` token) and converts
it to a `[]T` slice type, setting a `variadic` flag. This flag propagates
through `FuncOf` so the compiler knows to pack trailing arguments at the
call site.

## Dependencies

- `scan/` -- scanner tokens.
- `lang/` -- token types, `Spec`.
- `symbol/` -- symbol table.
- `vm/` -- `Type`, `Value` (for symbol metadata).

## Open questions / TODOs

- Generic type parameters are not parsed.
