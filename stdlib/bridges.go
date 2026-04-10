package stdlib

import (
	"reflect"

	"github.com/mvertes/parscan/vm"
)

// Bridge types for common interface methods.
// Each bridge is a struct with a Fn field and a pointer-receiver method
// that delegates to Fn. At the native call boundary, the VM allocates a
// bridge instance with Fn set to a closure that invokes the interpreted method.

// BridgeError bridges the error interface method.
type BridgeError struct{ Fn func() string }

func (b *BridgeError) Error() string { return b.Fn() }

// BridgeGoString bridges the fmt.GoStringer interface method.
type BridgeGoString struct{ Fn func() string }

// GoString implements fmt.GoStringer.
func (b *BridgeGoString) GoString() string { return b.Fn() }

// BridgeString bridges the fmt.Stringer interface method.
type BridgeString struct{ Fn func() string }

// String implements fmt.Stringer.
func (b *BridgeString) String() string { return b.Fn() }

func init() {
	vm.Bridges["Error"] = reflect.TypeOf((*BridgeError)(nil))
	vm.Bridges["GoString"] = reflect.TypeOf((*BridgeGoString)(nil))
	vm.Bridges["String"] = reflect.TypeOf((*BridgeString)(nil))
}
