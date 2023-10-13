# Parser

See if we can build an almost single pass compiler for Go doing syntax
directed translation, without any complex data structure (no syntax
tree), only lists of tokens.

The goal is to have the shortest and simplest path from source to
bytecode.

## Design

The input of parser is a list of tokens produced by the scanner.
Multiple tokens are processed at once. The minimal set to get
meaningful results (not an error or nil) is a complete statement.

The output of parser is also a list of tokens, to be consummed by
the compiler to produce bytecode. The output tokens set is identical
to the bytecode instructions set except that:

- code locations may be provided as labels instead of numerical
  values,
- memory locations for constants and variables may be provided as
  symbol names instead of numerical values.

## Status

Go language support:

- [x] named functions
- [ ] anonymous functions (closures)
- [ ] methods
- [x] internal function calls
- [x] external function calls (calling runtime symbols in interpreter)
- [ ] export to runtime
- [ ] builtin calls (new, make, copy, delete, len, cap, ...)
- [ ] out of order declarations
- [ ] arbirtrary precision constants
- [x] basic types
- [ ] complete numeric types
- [x] function types
- [ ] variadic functions
- [ ] pointers
- [ ] structures
- [ ] embedded structures
- [ ] recursive structures
- [ ] interfaces
- [ ] arrays, slices
- [ ] maps
- [ ] deterministic maps
- [ ] channel types
- [ ] channel operations
- [x] var defined by assign :=
- [x] var assign =
- [ ] var declaration
- [ ] type declaration
- [x] func declaration
- [ ] const declaration
- [ ] iota expression
- [ ] defer statement
- [ ] recover statement
- [ ] go statement
- [x] if statement (including else and else if)
- [x] for statement
- [ ] switch statement
- [x] break statement
- [ ] continue statement
- [ ] fallthrough statement
- [ ] goto statement
- [ ] select statement
- [x] binary operators
- [ ] unary operators
- [ ] logical operators && and ||
- [ ] assign operators
- [ ] operator precedence
- [x] parenthesis expressions
- [x] call expressions
- [ ] index expressions
- [ ] selector expressions
- [ ] type conversions
- [ ] type assertions
- [ ] parametric types (generic)
- [ ] type parametric functions (generic)
- [ ] type constraints (generic)
- [ ] type checking
- [ ] comment pragmas
- [ ] packages import
- [ ] modules

Other items:

- [x] REPL
- [x] multiline statements in REPL
- [ ] completion, history in REPL
- [x] eval strings
- [x] eval files (including stdin, ...)
- [x] debug traces for scanner, parser, compiler, bytecode vm
- [x] simple interpreter tests to exercise from source to execution
- [ ] compile time error detection and diagnosis
- [ ] stack dump
- [ ] symbol tables, data tables, code binded to source lines
- [ ] interactive debugger: breaks, continue, instrospection, ...
- [ ] machine level debugger
- [ ] source level debugger
- [ ] replay debugger, backward instruction execution
- [ ] vm monitor: live memory / code display during run
- [ ] stdlib wrappers a la yaegi
- [ ] system and environment sandboxing
- [ ] build constraints (arch, sys, etc)
- [ ] test command (running go test / benchmark / example files)
- [ ] skipping / including test files
- [ ] test coverage
- [ ] fuzzy tests for scanner, vm, ...
