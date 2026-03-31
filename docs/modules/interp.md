# interp

> Integration layer: wires scan, parse, compile, and execute into a single
> `Eval()` call.

## Overview

The `interp` package provides `Interp`, which embeds both `*comp.Compiler`
and `*vm.Machine`. It is the main entry point for evaluating Go source code
and powers the REPL.

## Key types and functions

- **`Interp`** -- embeds compiler and VM.
- **`NewInterpreter(spec *lang.Spec) *Interp`** -- create an interpreter
  for the given language spec.
- **`Eval(name, src string) (reflect.Value, error)`** -- compile and execute
  source code. `name` identifies the source (`"m:<content>"` for inline,
  `"f:<path>"` for file). Pushes new data and code to the VM incrementally.
  Calls `main()` automatically if defined.
- **`Repl(in io.Reader) error`** -- interactive read-eval-print loop.
  Feeds input line by line to `Eval`. When `Eval` returns `scan.ErrBlock`
  (the scanner detected an unbalanced block), the prompt switches to `>>`
  and the line is accumulated for retry on the next input.

## Internal design

### Incremental evaluation

`Eval` tracks the previous lengths of `Data` and `Code`. On each call it
removes the trailing `Exit` instruction added by the previous run
(`PopExit`), compiles new source, then pushes only the delta to the VM.
This allows the REPL to build up state across evaluations without
recompiling everything. The entry point for the new code is
`max(codeOffset, i.Entry)`, so module-level init code runs before `main`.

### Main function

If a `main` entry exists in `Compiler.Symbols` (the parser/compiler symbol
table), `Eval` emits a `Call` to it after pushing the compiled code. This
mirrors `go run` behavior for standalone programs.

### File-based tests

`interp/file_test.go` provides `TestFile`, which reads every `.go` file
under `_samples/` and runs it through the interpreter. Expected output or
expected error strings are encoded in the last block comment of the file
using the conventions `// Output:\n...` and `// Error:\n...`. This gives
a lightweight integration test suite that exercises the full pipeline end
to end on real Go programs.

### Lazy DebugInfo

`Eval` registers a `debugInfoFn` closure on the VM via `SetDebugInfo`.
This closure calls `Compiler.BuildDebugInfo()` to produce a `*vm.DebugInfo`
populated with the `scan.Sources` registry, label names, global symbol
names, and per-function local variable mappings. The builder is only
invoked if the program hits a `trap()` call, so there is no cost for
normal execution.

## Dependencies

- `comp/` -- compiler (embedded).
- `vm/` -- virtual machine (embedded).
- `lang/` -- language spec.
