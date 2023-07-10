# parscan

Parscan is an experimental project to test a single parser to multiple
languages and virtual machines.

The first language definition is a subset of Go, enough to implement
simple benchmarks, as fibonacci numbers.

The first VM is a stack machine, operated by walking  directly the AST.

The next step is to add a byte-code based VM and the corresponding byte code
generator.

Further steps is to get closer to full Go spec and / or introduce new
languages definitions and new VM implementations.

Note: this is highly experimental and unstable.


## Usage

`go run ./cmd/gint < ./samples/fib`
