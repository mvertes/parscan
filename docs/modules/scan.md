# scan

> Language-independent lexical scanner.

## Overview

The `scan` package tokenizes source code into a flat slice of `Token` values.
It is driven entirely by a `lang.Spec`, making it reusable across languages.
It sits at the start of the pipeline, feeding tokens to `goparser`.

## Key types and functions

- **`Scanner`** -- holds a `*lang.Spec`, a compiled regex for string
  delimiters, and a `Sources` registry. Created via `NewScanner(spec)`.
- **`Source`** -- describes a registered source: name, base offset, length.
- **`Sources`** -- ordered list of `Source` entries mapping global byte
  offsets to file/line/col. Methods: `Add(name, src) int` (returns base
  offset), `Resolve(pos) (name, line, col)`, `FormatPos(pos) string`.
- **`Token`** -- a single lexical unit: token type (`lang.Token`), source
  position, text, and block delimiter lengths.
- **`Scan(src string, semiEOF bool) ([]Token, error)`** -- tokenizes the
  entire source. When `semiEOF` is true, appends a semicolon at end-of-input
  if the last token warrants one.
- **`Next(src string) (Token, error)`** -- returns the next single token
  (used internally by `Scan`).

## Internal design

The scanner is a state machine that classifies characters via `lang.Spec.CharProp`
(a 128-entry ASCII lookup table). It handles:

- **Identifiers and numbers** -- classified by character properties.
- **Operators** -- longest-match from the spec's token table.
- **String literals** -- delimiters and escape sequences from
  `Spec.End` and `Spec.BlockProp`.
- **Nested blocks** -- `()`, `[]`, `{}` are matched and balanced at scan
  time, simplifying the parser. An `ErrBlock` is returned if input ends
  mid-block, allowing the REPL to prompt for continuation.
- **Automatic semicolons** -- inserted after newlines when the preceding
  token's `SkipSemi` property is set (mirrors Go's semicolon rules).

## Dependencies

- `lang/` -- token types, `Spec`, character property constants.

## Open questions / TODOs

- The scanner is currently ASCII-only (`ASCIILen = 128`). Unicode identifier
  support would require extending `CharProp` or switching to a different
  classification strategy.
