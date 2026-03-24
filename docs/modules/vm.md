# vm

> Stack-based bytecode virtual machine.

## Overview

The `vm` package executes compiled bytecode. `Machine.Run()` interprets a
`Code` (slice of `Instruction`) over a flat `mem []Value` that holds both
globals and the call stack. The package also defines `Value` (the runtime
value representation) and `Type` (runtime type metadata).

## Key types and functions

### Execution

- **`Machine`** -- VM state: `code`, `mem`, `ip`, `fp`, closure `env`,
  a `frames []frame` stack (caller env + packed nret/narg per call),
  panic state (`panicking`, `panicVal`), a `funcFields` side-table for
  parscan funcs stored in native struct fields, a `Symbols` map for
  name-to-mem-index lookup, and debug state.
- **`frame`** -- per-call metadata saved on the caller side. Fields:
  `env []*Value` (caller's closure env) and `info int` (packed
  `nret | (narg << 16)`). Stored in `Machine.frames`.
- **`Symbols map[string]int`** -- maps symbol names to global memory
  indices. Populated by `interp.Eval` from the compiler symbol table
  after each compilation. Used for REPL resolution and `main` dispatch.
- **`Run() error`** -- main execution loop. Dispatches on `Op` via a
  switch statement.
- **`Push(vals ...Value)`** / **`Pop() Value`** -- stack manipulation.
- **`PushCode(instrs ...Instruction)`** -- append instructions (for
  incremental evaluation).

### Values

- **`Value`** -- hybrid runtime value:
  - `num uint64` -- inline storage for numeric types (bool, int*, uint*,
    float*). Holds raw bits.
  - `ref reflect.Value` -- composite data (string, slice, map, struct,
    func) or type metadata for numerics.
  - Variable slots (from `NewValue`): `ref` is addressable.
  - Temporaries (from arithmetic): `ref` is `reflect.Zero(typ)`,
    non-addressable; `num` is canonical.
- **`NewValue(reflect.Type) Value`** -- allocate a zero value (addressable).
- **`ValueOf(any) Value`** -- wrap a Go value.

### Types

- **`Type`** -- runtime type: `Rtype reflect.Type`, `Name`, `PkgPath`,
  `Methods []Method`, `IfaceMethods []IfaceMethod`,
  `Embedded []EmbeddedField`, `Params []*Type`, `Fields []*Type`.
- **`Iface{Typ *Type, Val Value}`** -- boxed interface value.
- **`Closure{Code int, Env []*Value}`** -- captured function.
- **`ParscanFunc{Val Value, GF reflect.Value}`** -- wraps a parscan
  function value alongside its `reflect.MakeFunc`-generated Go wrapper.
  Stored when a parscan func is assigned to a struct field of func type
  so that native Go callbacks (e.g. HTTP handlers) can call back into
  the VM. `WrapFunc` creates it; the inner VM always dispatches via `Val`.

### Opcodes

- **`Op`** (int enum) -- 200+ opcodes organized in groups:
  - Stack/control: `Nop`, `Pop`, `Push`, `Swap`, `Exit`, `Jump`,
    `JumpTrue`, `JumpFalse`, `JumpSetTrue`, `JumpSetFalse`, `Call`,
    `CallX`, `Return`.
  - Memory: `Get`, `Set`, `SetS`, `Addr`, `Deref`, `DerefSet`, `Grow`.
  - Arithmetic: `Add`, `Sub`, `Mul`, `Neg`, `Equal`, `EqualSet`,
    `Greater`, `Lower` (generic); per-type variants:
    `AddInt`...`AddFloat64`, `SubInt`...`SubFloat64`,
    `MulInt`...`MulFloat64`, `DivInt`...`DivFloat64`,
    `RemInt`...`RemFloat64`, `NegInt`...`NegFloat64`,
    `GreaterInt`...`GreaterFloat64`, `LowerInt`...`LowerFloat64`.
  - Immediate: `AddIntImm`, `SubIntImm`, `MulIntImm`, `GreaterIntImm`,
    `GreaterUintImm`, `LowerIntImm`, `LowerUintImm`, `EqualIntImm`.
  - Bitwise: `BitAnd`, `BitOr`, `BitXor`, `BitAndNot`, `BitShl`,
    `BitShr`, `BitComp`.
  - Collections: `Index`, `IndexSet`, `MapIndex`, `MapSet`, `Slice`,
    `Slice3`, `Field`, `FieldSet`, `FieldFset`.
  - Types: `Convert`, `TypeAssert`, `TypeBranch`, `Fnew`, `FnewE`,
    `New`, `MkSlice`, `MkMap`.
  - Builtins: `Append`, `CopySlice`, `DeleteMap`, `Cap`, `Len`,
    `PtrNew`.
  - Closures: `HAlloc`, `HGet`, `HSet`, `HPtr`, `MkClosure`.
  - Interfaces: `IfaceWrap`, `IfaceCall`.
  - Native interop: `WrapFunc` -- wraps a parscan function value in a
    `reflect.MakeFunc` adapter so it can be called by native Go code.
    Produces a `ParscanFunc` on the stack.
  - Range: `Next`, `Next2`, `Pull`, `Pull2`, `Stop`.
  - Exceptions: `Panic`, `Recover`, `DeferPush`, `DeferRet`.
  - Debug: `Trap`.

## Internal design

### Memory layout

```
mem[0 .. dataLen-1]    globals (vars, func code addresses, string literals)
mem[dataLen ..]        call stack (grows upward)
```

### Call frame

```
[ ... args | deferHead | retIP | prevFP | locals ... ]
                                  ^
                                  fp  (frameOverhead = 3 slots)
```

`Call` pushes `deferHead`, `retIP`, and `prevFP` onto the stack, then sets
`fp` past all three. It also pushes a `frame{env, info}` entry onto
`Machine.frames` to save the caller's closure env and the packed
`nret | (narg << 16)` value. `Return` inspects `deferHead` (at
`mem[fp-3]`) for pending deferred calls, then pops `frames` to restore
`env` and recover nret/narg.

- `mem[fp-3]` -- `deferHead`: index of the topmost deferred-call record
  (0 = none). Updated by `DeferPush`.
- `mem[fp-2]` -- `retIP`: return address (or `deferSentinelIP = -1` for
  a deferred frame).
- `mem[fp-1]` -- `prevFP`: the caller's frame pointer.
- `frames[top]` -- `frame{env, info}`: the caller's closure env and
  packed nret/narg (side-channel, not on the value stack).

### Per-type numeric ops

To avoid type-switch overhead on the hot path, the compiler emits
type-specific opcodes. There are 12 numeric type variants (int, int8, ...,
float64), each with its own `Add`, `Sub`, `Mul`, `Div`, `Rem`, `Neg`,
`Greater`, `Lower` opcode. The generic `add[T]`, `sub[T]`, etc. functions
in `numops.go` use Go generics internally.

### Closure dispatch

When a closure is called, `Machine` swaps in its `Env` (saved heap cells)
and restores the caller's env on return. `HGet`/`HSet` read/write through
`env[i]` pointers.

### Panic / defer / recover

- `DeferPush` saves a sentinel frame pointing to a deferred function.
- `Panic` sets `panicking = true` and unwinds, calling deferred functions.
- `Recover` clears the panic state if called inside a deferred function.
- `DeferRet` is emitted at function exit to run deferred functions in LIFO
  order.

### Native Go interop (WrapFunc / CallFunc)

Parscan functions are integers (code addresses) or `Closure` values at
runtime. Neither can be stored directly in a typed Go `func` field via
`reflect.Set`. Two mechanisms bridge this gap:

1. **`funcFields` side-table.** The VM maintains a `map[uintptr]Value`
   keyed by the address of the target reflect.Value. When the compiler
   detects assignment of a parscan func to a native struct field, it emits
   an instruction that stores the parscan func into `funcFields` while
   writing a zero/stub into the actual field.

2. **`WrapFunc` opcode.** Converts the parscan func on the stack into a
   `ParscanFunc` by calling `reflect.MakeFunc` with a trampoline that
   re-enters the VM via `Machine.CallFunc`. The resulting `GF`
   `reflect.Value` is assignable to any Go func field of the matching type.

### Re-entrant execution (CallFunc)

`CallFunc` provides re-entrant VM execution for native Go callbacks. It
saves all volatile state (`mem`, `ip`, `fp`, `env`, `frames`, panic
state, code length), resets per-call state, copies globals to a fresh
stack, pushes the function value and arguments, appends a temporary
`Call` + `Exit` sequence, and runs the inner loop. On return (including
via `defer`), all saved state is restored.

This is safe for single-threaded synchronous callbacks (e.g. an HTTP
handler calling back into the interpreter). Concurrent goroutines
calling different wrapped functions on the same `Machine` are not safe.

### Trap and interactive debug mode

The `Trap` opcode pauses VM execution and enters an interactive debug
session. It is emitted by the compiler for calls to the `trap()` builtin.

**Sentinel IP mechanism.** The run loop uses special `ip` values to handle
out-of-band control flow. `Trap` works the same way as `deferSentinelIP`
and `panicUnwindIP`: the opcode handler saves the resume address in
`trapOrig`, sets `ip = trapIP` (constant `-3`), and continues. On the next
iteration the run loop detects the sentinel, syncs Machine state, and calls
`enterDebug()`. When the user types `cont`, `enterDebug` returns and the
loop restores `ip` from `Machine.ip` to resume normal execution.

**Debug fields on Machine:**

- `debugInfoFn func() *DebugInfo` -- builder registered by the interpreter.
  Called on demand inside `enterDebug` to produce symbolic info (labels,
  globals, locals, source registry). Not cached on Machine.
- `debugIn` / `debugOut` -- I/O overrides for the debug REPL (default:
  stdin/stderr). Tests inject buffers here.
- `trapOrig int` -- the ip to resume after the debug session ends.

**Debug REPL commands:**

| Command | Action |
|---------|--------|
| `bt`, `stack` | Dump the full call stack with frame layout and symbol names |
| `c`, `cont` | Continue execution |
| `h`, `help` | Show available commands |

**DebugInfo** (`vm/debug.go`) holds symbolic metadata populated by
`comp.Compiler.BuildDebugInfo()`: a `scan.Sources` registry for
multi-file/REPL position resolution, label-to-name mappings,
global-index-to-name mappings, and per-function local variable lists.
`DumpFrame` and `DumpCallStack` use this information to annotate memory
slots with human-readable names and source positions.

## Dependencies

- `scan` -- for `scan.Sources` (source position registry used by `DebugInfo`).
