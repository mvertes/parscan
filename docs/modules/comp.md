# comp

> Bytecode compiler: walks the flat token stream, resolves symbols, emits
> VM instructions.

## Overview

The `comp` package bridges parsing and execution. Its `Compiler` embeds the
parser and walks the flat token stream in a single pass, emitting
`vm.Instruction` values into a `Code` slice and populating a `Data` slice
(the global memory segment).

## Key types and functions

- **`Compiler`** -- embeds `*goparser.Parser`. Manages `Code`, `Data`,
  `Entry` (start IP), string deduplication, and method ID allocation.
- **`Compile(src string) error`** -- end-to-end: parse, register forward
  references, then generate bytecode with lazy fixpoint retry.
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

### Lazy fixpoint

`Compile` calls `ScanDecls` to get all top-level declarations, then
`RegisterFunc` for each function. It then attempts to generate code for
each declaration. If `ErrUndefined` is returned, the declaration is
deferred. The loop retries until either all declarations succeed or no
progress is made (a true undefined-symbol error).

Symbol and code rollback on failure is tracked via `SymTracker` and
code/data length checkpoints.

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
