# Parser

A parser takes an array of tokens (produced by the scanner) in input and
returns a node representing a syntax tree. A node is an object
containing a kind, the corresponding token and the ordered references to
descendent nodes.

A goal is to make the parser generic enough so it can generate syntax
trees for most of existing programming languages (no claim of generality
yet), provided a small set of generating rules per language, and a small
set of validating rules (yet to be defined) to detect invalid
constructs.

The input tokens are particular in the sense that they include classical
lexical items such as words, separators, numbers, but also strings and
nested blocks, which are resolved at scanning stage rather than parsing
stage. See the scanner for more details.

The language specification includes the following:

- a scanner specification, to produce the set of possible tokens.
- a map of node specification per token name. The node specification
  defines some parameters influing how the tree is generated.

## Development status

A successful test must be provided to check the status.

- [x] binary operator expressions
- [x] unary operator (prefix) expressions
- [ ] unary operator (suffix) expressions
- [x] operator precedence rules
- [x] parenthesis in expressions
- [ ] semi-colon automatic insertion rules
- [x] call expressions
- [ ] nested calls
- [x] index expressions
- [x] single assignments
- [ ] multi assignments
- [x] simple `if` statement (no `else`)
- [ ] full `if` statement (including `else`, `else if`)
- [x] init expressions in `if` statements
- [x] statement blocks
- [ ] comments
- [ ] for statement
- [ ] switch statement
- [ ] select statement
- [x] return statement
- [x] function declaration
- [ ] method declaration 
- [ ] anonymous functions (closures)
- [ ] type declaration
- [ ] var, const, type single declaration
- [ ] var, const, type multi declaration
- [ ] type parametric expressions
- [x] literal numbers (see scanner)
- [x] literal strings 
- [ ] composite literal
- [ ] import statements
- [ ] go.mod syntax
