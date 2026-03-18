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
  panic state, and per-frame metadata.
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
  `Embedded []EmbeddedField`.
- **`Iface{Typ *Type, Val Value}`** -- boxed interface value.
- **`Closure{Code int, Env []*Value}`** -- captured function.

### Opcodes

- **`Op`** (int enum) -- 120+ opcodes organized in groups:
  - Stack/control: `Nop`, `Pop`, `Push`, `Swap`, `Exit`, `Jump`,
    `JumpTrue`, `JumpFalse`, `Call`, `Return`.
  - Memory: `Get`, `Set`, `Addr`, `Deref`, `Grow`.
  - Arithmetic: `Add`, `Sub`, `Mul`, `Neg`, `Equal`, `Greater`, `Lower`
    (plus per-type variants: `AddInt`, `AddFloat64`, etc.).
  - Immediate: `AddIntImm`, `SubIntImm`, `MulIntImm`, `GreaterIntImm`,
    `LowerIntImm`, `EqualIntImm`.
  - Bitwise: `BitAnd`, `BitOr`, `BitXor`, `BitShl`, `BitShr`, `BitComp`.
  - Collections: `Index`, `IndexSet`, `MapIndex`, `MapSet`, `Slice`,
    `Field`, `FieldSet`.
  - Types: `Convert`, `TypeAssert`, `TypeBranch`, `New`, `MkSlice`,
    `Composite`.
  - Closures: `HAlloc`, `HGet`, `HSet`, `HPtr`, `MkClosure`.
  - Interfaces: `IfaceWrap`, `IfaceCall`.
  - Range: `Next`, `Next2`, `Pull`, `Pull2`, `Stop`.
  - Exceptions: `Panic`, `Recover`, `DeferPush`, `DeferRet`.

## Internal design

### Memory layout

```
mem[0 .. dataLen-1]    globals (vars, func code addresses, string literals)
mem[dataLen ..]        call stack (grows upward)
```

### Call frame

```
[ ... args | retIP | prevFP | locals ... ]
              ^
              fp
```

`Call` pushes `retIP` and `prevFP`, then sets `fp` past them. `Return`
restores `fp` and `ip` from the frame.

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

## Dependencies

None (leaf package -- only standard library).
