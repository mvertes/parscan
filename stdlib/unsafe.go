// Hand-written binding for the "unsafe" pseudo-package.
// unsafe has no parseable Go source, so cmd/extract cannot generate this file.
// Do not add the cmd/extract "Code generated" marker: `make clean_generate`
// greps for that marker and would delete this file.

package stdlib

import (
	"reflect"
	"unsafe"
)

// Sizeof and Alignof are compile-time intrinsics intercepted in
// comp.compileBuiltin by matching the qualified symbol name; these stubs
// only exist so that `unsafe.Sizeof` / `unsafe.Alignof` resolve as package
// members during symbol lookup. They must never run.
func unsafeSizeof(any) uintptr  { panic("unsafe.Sizeof: not intercepted") }
func unsafeAlignof(any) uintptr { panic("unsafe.Alignof: not intercepted") }

func init() {
	Values["unsafe"] = map[string]reflect.Value{
		"Pointer": reflect.ValueOf((*unsafe.Pointer)(nil)),
		"Sizeof":  reflect.ValueOf(unsafeSizeof),
		"Alignof": reflect.ValueOf(unsafeAlignof),
	}
}
