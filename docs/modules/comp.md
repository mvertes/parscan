# comp

> Bytecode compiler: walks the flat token stream, resolves symbols, emits
> VM instructions.

## Overview

The `comp` package bridges parsing and execution. Its `Compiler` embeds the
parser and compiles source in two phases: first resolving all declarations,
then generating bytecode. It emits `vm.Instruction` values into a `Code`
slice and populates a `Data` slice (the global memory segment).

## Key types and functions

- **`Compiler`** -- embeds `*goparser.Parser`. Manages `Code`, `Data`,
  `Entry` (start IP), string deduplication (`strings` map), method ID
  allocation (`methodIDs` map), a type-pointer dedup cache (`typeIdxs`),
  and `posBase` (byte offset of the current source in the `Sources`
  registry for position resolution).
- **`Compile(name, src string) error`** -- end-to-end: two-phase compilation
  (declarations first, then code generation) with fixpoint retry in each
  phase. `name` identifies the source (`"m:<content>"` for inline,
  `"f:<path>"` for file) and is registered in the scanner's `Sources` table
  for position resolution.
- **`Dump() / ApplyDump(d)`** -- snapshot and restore global variable
  state (used for REPL resets).

## Internal design

### Code generation

`generate(tokens)` iterates over the flat token stream. For each token it:

1. Looks up the corresponding symbol in `SymMap`.
2. Emits `Get`/`Set`/`Push` instructions based on symbol kind and locality.
3. For operators, emits the matching opcode (type-specific where possible).
4. For `Label`, records the code address; for `Goto`/`JumpFalse`, emits
   jumps and patches targets.

A symbolic stack shadows the VM stack to track types at compile time,
enabling type-specific opcode selection.

### Peephole optimization

After emitting a binary op, the compiler checks whether the preceding
instruction was a `Push` of an integer constant. If so, it folds both into
a single immediate-operand instruction (e.g. `Push 1; AddInt` becomes
`AddIntImm 1`). This reduces dispatch overhead in tight loops by ~20-30%.

### Two-phase compilation

`Compile` proceeds in two phases:

1. **Declaration phase.** `ScanDecls` splits the source into top-level
   declarations. Each is passed to `ParseDecl`, which handles `package`,
   `import`, `const`, `type`, and `var` (type registration only -- no
   initializers). For `func`, it calls `registerFunc` to record the
   function signature without parsing the body. Declarations that fail
   with `ErrUndefined` are retried in a fixpoint loop until either all
   succeed or no progress is made. Unhandled declarations (func bodies,
   var initializers) are collected for phase 2.

2. **Code generation phase.** The remaining declarations are parsed
   (`ParseOneStmt`) and compiled (`generate`) in a second fixpoint loop.
   At this point all type and function symbols are resolved, so retries
   are rare -- they only occur when a func body references a var whose
   type is inferred from an initializer not yet compiled.

This separation avoids interleaving declaration resolution with code
generation, reducing rollback complexity. Rollback on failure uses
`SymTracker` (for newly added symbols), `savedSlots` (for in-place data
updates), and code/data length checkpoints.

### Variadic call-site packing

When calling a variadic function, the compiler emits `MkSlice` to collect
the trailing arguments into a `[]T` before `Call`. The number of fixed
parameters is computed from the function type; `MkSlice` receives the count
of extra arguments and the element type index. The callee sees a normal
slice parameter.

### Built-in function dispatch

`compileBuiltin()` intercepts calls to Go builtins by matching on
`Symbol.Name`. It is called from both the `lang.Call` and `lang.CallX`
handlers. Each builtin emits a dedicated opcode:

| Builtin | Opcode(s) | Notes |
|---------|-----------|-------|
| `print` | (native `Value`) | Dispatched as a regular `CallX`; not a `Builtin` symbol |
| `println` | (native `Value`) | Same as `print` |
| `len` | `Len` + `Swap` + `Pop` | `Len` does not consume input (used in slice exprs too) |
| `cap` | `Cap` + `Swap` + `Pop` | Same pattern as `len` |
| `append` | `Append` | Uses `reflect.Append` for amortized growth |
| `copy` | `CopySlice` | Returns element count |
| `delete` | `DeleteMap` + `Pop` | Void; extra `Pop` discards the map value |
| `new` | `PtrNew` | Removes the `Fnew` emitted for the type argument |
| `make` | `MkSlice` (negative n) / `MkMap` | Reuses `MkSlice` with negative `Arg[0]` for make mode |
| `panic` | `Panic` | |
| `recover` | `Recover` | |
| `trap` | `Trap` | Zero arguments; pauses VM and enters interactive debug mode |

For `new` and `make`, the first argument is a type, not a value. The
parser's `Ident` handler emits a `Fnew`/`FnewE` instruction for type
symbols; `compileBuiltin` removes it via `removeFnew()` and uses the
type's data index directly.

### Method and interface dispatch

The compiler maintains a `methodIDs` map assigning unique integers to
method names. When a concrete type is wrapped in an interface
(`IfaceWrap`), the compiler verifies that all required methods exist.
`IfaceCall` dispatches by method ID at runtime.

## Dependencies

- `goparser/` -- token stream and parser.
- `symbol/` -- symbol table.
- `vm/` -- instructions, opcodes, `Value`, `Type`.
- `lang/` -- token types.
