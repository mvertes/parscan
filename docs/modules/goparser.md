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
- **`ParseDecl(toks Tokens) (handled bool, err error)`** -- process a
  single declaration during the compiler's phase 1. Handles `package`,
  `import`, `const`, `type`, and `var` (types only). For `func`, registers
  the signature via `registerFunc`. Returns `handled=false` when the
  declaration still needs full parse + code generation (func bodies, var
  initializers).
- **`ParseOneStmt(toks Tokens) (Tokens, error)`** -- parse a single
  statement (used during compilation phase 2).
- **`registerFunc(toks Tokens) error`** -- register a function's
  signature in the symbol table without parsing its body.

### Error types

- **`ErrUndefined{Name}`** -- symbol not yet defined. The compiler catches
  this to trigger retry in the lazy fixpoint loop.

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
