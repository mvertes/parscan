package stdlib

import (
	"container/heap"
	"flag"
	"io"
	"reflect"
	"sort"

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

// BridgeMarshalJSON bridges the json.Marshaler interface method.
type BridgeMarshalJSON struct{ Fn func() ([]byte, error) }

// MarshalJSON implements json.Marshaler.
func (b *BridgeMarshalJSON) MarshalJSON() ([]byte, error) { return b.Fn() }

// BridgeUnmarshalJSON bridges the json.Unmarshaler interface method.
type BridgeUnmarshalJSON struct{ Fn func([]byte) error }

// UnmarshalJSON implements json.Unmarshaler.
func (b *BridgeUnmarshalJSON) UnmarshalJSON(data []byte) error { return b.Fn(data) }

// BridgeWrite bridges the io.Writer interface method.
type BridgeWrite struct{ Fn func([]byte) (int, error) }

// Write implements io.Writer.
func (b *BridgeWrite) Write(p []byte) (int, error) { return b.Fn(p) }

// BridgeRead bridges the io.Reader interface method.
type BridgeRead struct{ Fn func([]byte) (int, error) }

// Read implements io.Reader.
func (b *BridgeRead) Read(p []byte) (int, error) { return b.Fn(p) }

// BridgeClose bridges the io.Closer interface method.
type BridgeClose struct{ Fn func() error }

// Close implements io.Closer.
func (b *BridgeClose) Close() error { return b.Fn() }

// BridgeWriteTo bridges the io.WriterTo interface method.
type BridgeWriteTo struct {
	Fn func(io.Writer) (int64, error)
}

// WriteTo implements io.WriterTo.
func (b *BridgeWriteTo) WriteTo(w io.Writer) (int64, error) { return b.Fn(w) }

// BridgeReadFrom bridges the io.ReaderFrom interface method.
type BridgeReadFrom struct {
	Fn func(io.Reader) (int64, error)
}

// ReadFrom implements io.ReaderFrom.
func (b *BridgeReadFrom) ReadFrom(r io.Reader) (int64, error) { return b.Fn(r) }

// BridgeReaderWriterTo is a composite bridge implementing io.Reader + io.WriterTo.
// Used to preserve WriterTo capability when wrapping for an io.Reader target (e.g. io.Copy).
type BridgeReaderWriterTo struct {
	FnRead    func([]byte) (int, error)
	FnWriteTo func(io.Writer) (int64, error)
}

func (b *BridgeReaderWriterTo) Read(p []byte) (int, error) { return b.FnRead(p) }

// WriteTo implements io.WriterTo.
func (b *BridgeReaderWriterTo) WriteTo(w io.Writer) (int64, error) { return b.FnWriteTo(w) }

// BridgeWriterReaderFrom is a composite bridge implementing io.Writer + io.ReaderFrom.
// Used to preserve ReaderFrom capability when wrapping for an io.Writer target (e.g. io.Copy).
type BridgeWriterReaderFrom struct {
	FnWrite    func([]byte) (int, error)
	FnReadFrom func(io.Reader) (int64, error)
}

func (b *BridgeWriterReaderFrom) Write(p []byte) (int, error) { return b.FnWrite(p) }

// ReadFrom implements io.ReaderFrom.
func (b *BridgeWriterReaderFrom) ReadFrom(r io.Reader) (int64, error) { return b.FnReadFrom(r) }

// BridgeSortInterface bridges sort.Interface (Len, Less, Swap).
type BridgeSortInterface struct {
	FnLen  func() int
	FnLess func(int, int) bool
	FnSwap func(int, int)
}

func (b *BridgeSortInterface) Len() int           { return b.FnLen() }
func (b *BridgeSortInterface) Less(i, j int) bool { return b.FnLess(i, j) }
func (b *BridgeSortInterface) Swap(i, j int)      { b.FnSwap(i, j) }

// BridgeHeapInterface bridges heap.Interface (Len, Less, Swap, Push, Pop).
// Embeds BridgeSortInterface for the sort.Interface methods.
type BridgeHeapInterface struct {
	BridgeSortInterface
	FnPush func(any)
	FnPop  func() any
}

// Push implements heap.Interface.
func (b *BridgeHeapInterface) Push(x any) { b.FnPush(x) }

// Pop implements heap.Interface.
func (b *BridgeHeapInterface) Pop() any { return b.FnPop() }

// BridgeFlagValue bridges flag.Value (String, Set).
type BridgeFlagValue struct {
	FnString func() string
	FnSet    func(string) error
}

// String implements flag.Value.
func (b *BridgeFlagValue) String() string { return b.FnString() }

// Set implements flag.Value.
func (b *BridgeFlagValue) Set(s string) error { return b.FnSet(s) }

func init() {
	vm.Bridges["Error"] = reflect.TypeOf((*BridgeError)(nil))
	vm.Bridges["GoString"] = reflect.TypeOf((*BridgeGoString)(nil))
	vm.Bridges["MarshalJSON"] = reflect.TypeOf((*BridgeMarshalJSON)(nil))
	vm.Bridges["String"] = reflect.TypeOf((*BridgeString)(nil))
	vm.Bridges["UnmarshalJSON"] = reflect.TypeOf((*BridgeUnmarshalJSON)(nil))
	vm.Bridges["Write"] = reflect.TypeOf((*BridgeWrite)(nil))
	vm.Bridges["Read"] = reflect.TypeOf((*BridgeRead)(nil))
	vm.Bridges["Close"] = reflect.TypeOf((*BridgeClose)(nil))
	vm.Bridges["WriteTo"] = reflect.TypeOf((*BridgeWriteTo)(nil))
	vm.Bridges["ReadFrom"] = reflect.TypeOf((*BridgeReadFrom)(nil))

	vm.CompositeBridges[[2]string{"Read", "WriteTo"}] = reflect.TypeOf((*BridgeReaderWriterTo)(nil))
	vm.CompositeBridges[[2]string{"ReadFrom", "Write"}] = reflect.TypeOf((*BridgeWriterReaderFrom)(nil))

	// Display bridges are used when the target is interface{}/any.
	vm.DisplayBridges["Error"] = true
	vm.DisplayBridges["GoString"] = true
	vm.DisplayBridges["MarshalJSON"] = true
	vm.DisplayBridges["String"] = true
	vm.DisplayBridges["UnmarshalJSON"] = true

	vm.InterfaceBridges[reflect.TypeOf((*sort.Interface)(nil)).Elem()] = reflect.TypeOf((*BridgeSortInterface)(nil))
	vm.InterfaceBridges[reflect.TypeOf((*heap.Interface)(nil)).Elem()] = reflect.TypeOf((*BridgeHeapInterface)(nil))
	vm.InterfaceBridges[reflect.TypeOf((*flag.Value)(nil)).Elem()] = reflect.TypeOf((*BridgeFlagValue)(nil))
}
