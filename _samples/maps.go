package main

import "maps"

// Type inference through the ~map[K]V constraint is not yet supported by the
// interpreter (see project_generic_nested_inference.md). All calls below use
// explicit type arguments. Once inference is fixed, the natural forms
// `maps.Equal(a, b)` etc. should work identically.

func main() {
	a := map[string]int{"x": 1, "y": 2, "z": 3}
	b := map[string]int{"x": 1, "y": 2, "z": 3}
	c := map[string]int{"x": 1, "y": 2, "z": 4}
	println(maps.Equal[map[string]int, map[string]int, string, int](a, b))
	println(maps.Equal[map[string]int, map[string]int, string, int](a, c))

	eq := func(v1, v2 int) bool { return v1 == v2 }
	println(maps.EqualFunc[map[string]int, map[string]int, string, int, int](a, b, eq))
	println(maps.EqualFunc[map[string]int, map[string]int, string, int, int](a, c, eq))

	dst := map[string]int{"x": 10, "w": 99}
	maps.Copy[map[string]int, map[string]int, string, int](dst, a)
	println(dst["x"], dst["y"], dst["z"], dst["w"])

	m := map[string]int{"a": 1, "b": 2, "c": 3, "d": 4}
	maps.DeleteFunc[map[string]int, string, int](m, func(k string, v int) bool { return v%2 == 1 })
	println(len(m), m["b"], m["d"])
}

// Output:
// true
// false
// true
// false
// 1 2 3 99
// 2 2 4
