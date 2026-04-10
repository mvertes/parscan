package vm

import "reflect"

// Bridges maps interface method names to their bridge pointer types.
// Each bridge type is a struct with a Fn field and a pointer-receiver method
// that delegates to Fn. Populated at init time by stdlib (or any compiled
// package binding). The VM uses these to build wrapper types that make
// interpreted values satisfy Go interfaces at the native call boundary.
var Bridges = map[string]reflect.Type{}
