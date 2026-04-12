package vm

import "reflect"

// Bridges maps interface method names to their bridge pointer types.
// Each bridge type is a struct with a Fn field and a pointer-receiver method
// that delegates to Fn. Populated at init time by stdlib (or any compiled
// package binding). The VM uses these to build wrapper types that make
// interpreted values satisfy Go interfaces at the native call boundary.
var Bridges = map[string]reflect.Type{}

// InterfaceBridges maps Go interface types to bridge pointer types that
// implement all methods of the interface. Each bridge struct has fields
// named Fn<MethodName> for each method. Used for multi-method interfaces
// like heap.Interface or sort.Interface.
var InterfaceBridges = map[reflect.Type]reflect.Type{}
