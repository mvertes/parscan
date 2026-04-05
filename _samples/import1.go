package main

import "example.com/pkg1"

func main() {
	println(pkg1.V, pkg1.F())
}

// skip: not ready
// Output:
// hello 3
