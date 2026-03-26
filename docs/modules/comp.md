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

### Call handling

The `lang.Call` token in the flat stream triggers a unified call-handling
path. The compiler distinguishes two cases based on the callee symbol's kind:

- **Parscan function** (`Kind: Func`, `LocalVar`, etc.) -- emits `vm.Call`
  after optionally packing variadic args with `MkSlice`.
- **Native Go function value** (`Kind: Value`) -- emits `vm.CallX`, which
  invokes the callee via `reflect.Value.Call`.

The `lang.CallX` parser token was removed; the distinction is now made
entirely inside the `case lang.Call` handler using the compile-time symbolic
stack. Builtin symbols (`Kind: Builtin`) are intercepted by `compileBuiltin`
before either path is reached.

### Peephole optimization

After emitting a binary op, the compiler checks whether the preceding
instruction was a `Push` of an integer constant. If so, it folds both into
a single immediate-operand instruction (e.g. `Push 1; AddInt` becomes
`AddIntImm 1`). This reduces dispatch overhead in tight loops by ~20-30%.

### Two-phase compilation

`Compile` proceeds in two phases:

1. **Phase 1 -- Declarations.** `ScanDecls` splits the source into
   top-level declarations. Each is passed to `ParseDecl`, which handles
   `package`, `import`, `const`, `type`, and `var` (type registration
   only -- no initializers). For `func` (including methods), it calls
   `registerFunc` to record the function signature without parsing the
   body. Declarations that fail with `ErrUndefined` are retried in a
   fixpoint loop until convergence. Rollback on failure is lightweight:
   only `SymTracker` keys are deleted from the symbol table; there is no
   code or data to revert since Phase 1 emits neither.

2. **Phase 2 -- Code generation.** `SplitAndSortVarDecls` expands
   `var(...)` blocks and topologically sorts var declarations by
   dependency. `allocGlobalSlots` then pre-assigns data indices for
   every `Var` and `Func` symbol, so code generation never encounters
   an unresolved index. Code is generated in two passes:
   - **Pass 1:** var initializers, so all global var types are concrete.
   - **Pass 2:** func bodies and expression statements.

   Because all symbols have allocated slots, Phase 2 needs no retries or
   rollback machinery.

#### allocGlobalSlots

After Phase 1, every `Func` and `Var` symbol has a signature or type but
`Index == UnsetAddr`. `allocGlobalSlots` iterates the symbol table and
assigns a `Data` slot to each, appending the symbol's `Value` (or a
`NewValue` zero for uninitialized vars). Type and Value symbols are still
allocated lazily in the `Ident` handler, since many built-in types may
never be referenced.

### Variadic call-site packing

When calling a variadic function, the compiler emits `MkSlice` to collect
the trailing arguments into a `[]T` before `Call`. The number of fixed
parameters is computed from the function type; `MkSlice` receives the count
of extra arguments and the element type index. The callee sees a normal
slice parameter.

### Built-in function dispatch

`compileBuiltin()` intercepts calls to Go builtins by matching on
`Symbol.Name`. It is called from the `lang.Call` handler (which now
handles both parscan function calls and native Go value calls). Each
builtin emits a dedicated opcode:

| Builtin | Opcode(s) | Notes |
|---------|-----------|-------|
| `print` | `vm.CallX` (native reflect call) | Registered as `Kind: Value` with a Go func; dispatched via `vm.CallX` opcode, not a `Builtin` symbol |
| `println` | `vm.CallX` (native reflect call) | Same as `print` |
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
