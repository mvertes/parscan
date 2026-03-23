# ADR-006: Native Go function interop (WrapFunc / ParscanFunc)
**Status:** accepted
**Date:** 2026-03-22

## Context

Parscan function values are represented at runtime as plain integers (code
addresses) or `Closure` structs. Neither can be stored directly into a
typed Go `func` field via `reflect.Value.Set`, because reflect requires the
stored value to match the declared field type exactly.

This matters when a parscan program passes a function literal as a callback
to native Go code -- for example, as an HTTP handler or an event listener
stored in a struct field.

## Decision

Two complementary mechanisms handle this:

1. **`funcFields` side-table in `Machine`.** A `map[uintptr]Value`
   keyed on the `reflect.Value` pointer of the target field. When the VM
   detects assignment of a parscan func to a native struct `func` field,
   it writes the parscan func into the side-table instead of (or in
   addition to) the field itself. Reads route back through the table.

2. **`WrapFunc` opcode and `ParscanFunc` type.** When a parscan func must
   be callable by native Go code that holds a `reflect.Value` reference,
   `WrapFunc` converts it into a `ParscanFunc{Val, GF}`:
   - `Val` is the original parscan func value (int or Closure), used for
     fast in-VM dispatch.
   - `GF` is a `reflect.MakeFunc`-constructed Go function that re-enters
     the VM via `Machine.CallFunc` when invoked from native Go.

   `CallFunc` is a re-entrant entry into the VM run loop, safe for
   single-threaded synchronous callbacks. Concurrent calls from separate
   goroutines on the same `Machine` are not safe.

## Consequences

**Easier:**
- Native Go libraries that accept `func` callbacks work without special
  wrapper code in the interpreted program.
- `Val` is preserved, so in-VM calls to wrapped functions remain fast
  (no reflect overhead).

**Harder:**
- `Machine` carries a `funcFields` side-table that must be consulted on
  every struct func-field read/write, adding indirection.
- `WrapFunc`-generated functions hold a reference to the `Machine`,
  so the machine cannot be garbage-collected while a native callback
  is live.
- Concurrent safety is the caller's responsibility; the VM provides no
  locking.
