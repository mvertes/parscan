package vm

import "reflect"

// Bridges maps interface method names to their bridge pointer types.
// Each bridge type is a struct with a Fn field and a pointer-receiver method
// that delegates to Fn. Populated at init time by stdlib (or any compiled
// package binding). The VM uses these to build wrapper types that make
// interpreted values satisfy Go interfaces at the native call boundary.
var Bridges = map[string]reflect.Type{}

// DisplayBridges is the subset of Bridges that should be used when the
// target type is interface{}/any. These are "display" methods (String,
// Error, GoString, etc.) that change how the value appears in fmt output.
// Behavioral methods (Write, Read, Close) are NOT in this set because
// wrapping a value as e.g. BridgeWrite for an interface{} parameter
// changes its identity without benefit.
var DisplayBridges = map[string]bool{}

// InterfaceBridges maps Go interface types to bridge pointer types that
// implement all methods of the interface. Each bridge struct has fields
// named Fn<MethodName> for each method. Used for multi-method interfaces
// like heap.Interface or sort.Interface.
var InterfaceBridges = map[reflect.Type]reflect.Type{}

// CompositeBridges maps sorted pairs of method names to composite bridge
// pointer types that implement both methods. Used to preserve additional
// interface capabilities when wrapping for a single-method target interface
// (e.g. wrapping a Reader+WriterTo value for an io.Reader parameter keeps
// the WriterTo capability so io.Copy's internal type assertion succeeds).
var CompositeBridges = map[[2]string]reflect.Type{}
