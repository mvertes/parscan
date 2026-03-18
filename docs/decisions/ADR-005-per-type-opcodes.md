# ADR-005: Per-type opcodes with immediate variants

**Status:** accepted
**Date:** 2024-01-15

## Context

A generic `Add` opcode must inspect the operand type at runtime to select
the correct arithmetic operation. In a tight loop this type-switch overhead
is significant.

## Decision

The compiler emits type-specific opcodes when the operand type is known at
compile time. There are 12 numeric type variants (int, int8, int16, int32,
int64, uint, uint8, uint16, uint32, uint64, float32, float64), each with
its own `Add`, `Sub`, `Mul`, `Div`, `Rem`, `Neg`, `Greater`, `Lower`
opcode (96 opcodes total).

Additionally, a peephole optimization in `generate()` folds `Push N; BinOp`
sequences into single immediate-operand instructions (`AddIntImm`,
`SubIntImm`, `MulIntImm`, `GreaterIntImm`, `LowerIntImm`, `EqualIntImm`).
These avoid pushing the constant onto the stack entirely.

## Consequences

**Easier:**
- The VM dispatch loop hits a single opcode with no type branching.
- Immediate variants eliminate a push and a stack slot for constant
  operands, reducing loop overhead by ~20-30%.

**Harder:**
- Large opcode space (120+ opcodes). The `op_string.go` stringer file is
  generated to keep names in sync.
- Adding a new numeric type or operation requires adding opcodes in
  several places and running `make generate`.
- The peephole pass adds complexity to the compiler's `generate` function.
