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
- **`Eval(src string) (reflect.Value, error)`** -- compile and execute
  source code. Pushes new data and code to the VM incrementally. Calls
  `main()` automatically if defined.
- **`Repl(in io.Reader) error`** -- interactive read-eval-print loop.
  Accumulates multiline input when the scanner reports an incomplete block
  (`scan.ErrBlock`).

## Internal design

### Incremental evaluation

`Eval` tracks the previous lengths of `Data` and `Code`. On each call it
compiles new source, then pushes only the delta to the VM. This allows the
REPL to build up state across evaluations without recompiling everything.

### Lazy fixpoint (via compiler)

Out-of-order declarations are handled by the embedded `Compiler.Compile`,
which retries failed declarations. See [comp](comp.md) for details.

### Main function

If a `main` function is defined, `Eval` emits a `Call` to it after pushing
the compiled code. This mirrors `go run` behavior for standalone programs.

### Lazy DebugInfo

`Eval` registers a `debugInfoFn` closure on the VM via
`SetDebugInfoBuilder`. This closure calls `Compiler.BuildDebugInfo(src)` to
produce a `*vm.DebugInfo` populated with label names, global symbol names,
and per-function local variable mappings. The builder is only invoked if
the program hits a `trap()` call, so there is no cost for normal execution.

## Dependencies

- `comp/` -- compiler (embedded).
- `vm/` -- virtual machine (embedded).
- `lang/` -- language spec.
