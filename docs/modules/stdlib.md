# stdlib

> Standard library wrappers for importing native Go packages into parscan.

## Overview

The `stdlib` package provides a registry of native Go functions and values
that parscan programs can import. It bridges the gap between parscan's
interpreted code and the Go standard library by wrapping `reflect.Value`
entries in a map keyed by package name and symbol name.

## Key types and functions

- **`Values`** (`map[string]map[string]reflect.Value`) -- the global
  registry. The outer key is the package path (e.g. `"fmt"`), the inner
  key is the symbol name (e.g. `"Println"`). Each value is a
  `reflect.Value` wrapping the native Go function or variable.

## Internal design

Each supported package has its own source file (e.g. `fmt.go`) with an
`init()` function that registers entries into `Values`. Adding a new
stdlib package amounts to creating a file with the appropriate `init()`.

The interpreter calls `ImportPackageValues(stdlib.Values)` on the parser
at startup, which converts each entry into a `symbol.Package` via
`symbol.BinPkg` and stores it in `Parser.Packages`.

When parscan source code contains `import "fmt"`, the parser looks up the
package in `Packages`. If found and marked as binary (`Bin: true`), the
exported symbols are made available to the compiled code without source
parsing.

## Current packages

| Package | Symbols | Notes |
|---------|---------|-------|
| `fmt` | `Println` | Wraps `fmt.Println` |

## Dependencies

- `reflect` -- standard library (wrapping Go values).
- `symbol/` -- `BinPkg` creates package descriptors (called by the parser).
