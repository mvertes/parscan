package main

import "slices"

// Type inference through the ~[]E constraint is not yet supported by the
// interpreter (see project_generic_nested_inference.md). All calls below use
// explicit type arguments. Once inference is fixed, the natural forms
// `slices.Equal(a, b)` etc. should work identically.

func main() {
	a := []int{1, 2, 3, 4, 5}
	b := []int{1, 2, 3, 4, 5}
	c := []int{1, 2, 3, 4, 6}
	println(slices.Equal[[]int, int](a, b))
	println(slices.Equal[[]int, int](a, c))

	println(slices.Compare[[]int, int](a, b))
	println(slices.Compare[[]int, int](a, c))
	println(slices.Compare[[]int, int](c, a))

	println(slices.Index[[]int, int](a, 3))
	println(slices.Index[[]int, int](a, 99))
	println(slices.Contains[[]int, int](a, 4))
	println(slices.Contains[[]int, int](a, 99))

	println(slices.Max[[]int, int](a))
	println(slices.Min[[]int, int](a))

	idx, ok := slices.BinarySearch[[]int, int](a, 3)
	println(idx, ok)
	idx, ok = slices.BinarySearch[[]int, int](a, 99)
	println(idx, ok)

	r := []int{1, 2, 3}
	slices.Reverse[[]int, int](r)
	println(r[0], r[1], r[2])
}

// Output:
// true
// false
// 0
// -1
// 1
// 2
// -1
// true
// false
// 5
// 1
// 2 true
// 5 false
// 3 2 1
