# ADR-009: Interface bridging for native Go calls

**Status:** accepted
**Date:** 2026-04-09

## Context

Parscan creates struct types at runtime via `reflect.StructOf`. Go's reflect
package cannot register methods on these types -- there is no public API to add
methods after type creation, and the runtime's `itab` dispatch requires methods
to exist in the type descriptor.

This means that when an interpreted type with a `String() string` method is
passed to `fmt.Println`, Go's interface dispatcher does not find `fmt.Stringer`
and falls back to default struct formatting. The same applies to `error`,
`json.Marshaler`, and any other interface checked by native Go code.

The problem also affects non-struct named types (`type T int`) where the
`reflect.Type` is shared with the underlying type, making a type-registry
approach insufficient for carrying method identity.

## Decision

A three-layer bridge mechanism:

1. **Bridge types** are defined in `stdlib/` (or any compiled package). Each
   bridge is a Go struct with a `Fn` field and one pointer-receiver method:
   ```go
   type BridgeString struct{ Fn func() string }
   func (b *BridgeString) String() string { return b.Fn() }
   ```
   Registered in `vm.Bridges` at init time. New bridges require no changes
   to `vm/` or `comp/`.

2. **IfaceWrap at compile time.** The compiler emits `IfaceWrap` for arguments
   to native function calls with interface parameters (the `s.Kind == symbol.Value`
   path). This wraps the value in `Iface{Typ, Val}`, carrying the parscan type
   identity across the boundary.

3. **Bridge dispatch at runtime.** `bridgeArgs` in the VM's native-call path
   scans arguments for `Iface` values. When a concrete type has a method with a
   registered bridge, a bridge instance is allocated with a `Fn` closure that
   invokes the interpreted method via `CallFunc`. Non-bridged values are
   unwrapped to their concrete form.

Separately, `fmt.Print`/`Printf`/`Println` are overridden in the interpreter
with closures that write to the machine's configured output writer instead of
`os.Stdout`.

## Consequences

**Easier:**
- Interpreted types transparently satisfy Go interfaces at native boundaries.
- Adding support for a new interface method requires only a bridge struct in
  `stdlib/` -- no core package changes.

**Harder / limitations:**
- Only one bridge is applied per value (the first matching method). A type
  implementing both `Stringer` and `Marshaler` will only bridge one at any
  given native call.
- `bridgeArgs` runs on every native function call. The early-exit check
  (`len(Bridges) == 0`) is cheap, and the per-arg reflect type comparison
  is small relative to `reflect.Value.Call` overhead.
- Bridge closures create a fresh `Machine` for re-entrant execution, which
  allocates. This only occurs when a bridge method is actually called by
  native code.
