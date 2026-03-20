package vm

import (
	"iter"
	"math"
	"reflect"
)

// Runtime type and value representations (based on reflect).

// Method records a method's code location and receiver path for interface dispatch.
type Method struct {
	Index int   // data index of code address (-1 if unset)
	Path  []int // field index path to embedded receiver (nil = direct, []int{} = deref only)
}

// EmbeddedField records a parscan embedded field within a struct type.
type EmbeddedField struct {
	FieldIdx int   // index of this field in the parent struct
	Type     *Type // parscan type of the embedded field (shares identity with symbol table)
}

// Type is the representation of a runtime type.
type Type struct {
	PkgPath      string
	Name         string
	Rtype        reflect.Type
	IfaceMethods []IfaceMethod   // non-nil for interface types: required method signatures
	Methods      []Method        // concrete types: methods[methodID] = code location + receiver path
	Embedded     []EmbeddedField // parscan types of anonymous (embedded) fields, for promoted method lookup
}

// IfaceMethod describes a method required by an interface type.
type IfaceMethod struct {
	Name string
}

// Iface represents a boxed interface value at runtime.
// It preserves the concrete parscan type identity for dynamic method dispatch.
type Iface struct {
	Typ *Type // concrete parscan type (carries Name for method lookup)
	Val Value // the concrete value
}

var ifaceRtype = reflect.TypeOf(Iface{})

// IsInterface reports whether t represents an interface type.
func (t *Type) IsInterface() bool {
	return t != nil && t.Rtype.Kind() == reflect.Interface
}

func (t *Type) String() string {
	if t.Name != "" {
		if t.PkgPath != "" {
			return t.PkgPath + "." + t.Name
		}
		return t.Name
	}
	return t.Rtype.String()
}

// Elem returns a type's element type.
func (t *Type) Elem() *Type {
	e := t.Rtype.Elem()
	return &Type{Name: e.Name(), Rtype: e}
}

// Out returns the type's i'th output parameter.
func (t *Type) Out(i int) *Type {
	o := t.Rtype.Out(i)
	return &Type{Name: o.Name(), Rtype: o}
}

// Value is the VM runtime value.
// Numeric types (bool, int*, uint*, float*) store their value inline in num.
// ref carries reflect.Zero(t) for type metadata on numeric types.
// Composite types (string, slice, map, struct, ptr, func, interface) use ref.
type Value struct {
	num uint64        // inline storage for numeric types (bool, int*, uint*, float*)
	ref reflect.Value // composite data OR reflect.Zero(t) for numeric type metadata
}

// NumKindOffset maps a reflect.Kind to a 0-based offset into per-type opcode blocks.
// Returns -1 for non-numeric kinds.
var NumKindOffset [reflect.Float64 + 1]int

func init() {
	for i := range NumKindOffset {
		NumKindOffset[i] = -1
	}
	NumKindOffset[reflect.Int] = 0
	NumKindOffset[reflect.Int8] = 1
	NumKindOffset[reflect.Int16] = 2
	NumKindOffset[reflect.Int32] = 3
	NumKindOffset[reflect.Int64] = 4
	NumKindOffset[reflect.Uint] = 5
	NumKindOffset[reflect.Uint8] = 6
	NumKindOffset[reflect.Uint16] = 7
	NumKindOffset[reflect.Uint32] = 8
	NumKindOffset[reflect.Uint64] = 9
	NumKindOffset[reflect.Float32] = 10
	NumKindOffset[reflect.Float64] = 11
}

// Pre-computed zero reflect.Values for all numeric types (zero allocation).
var (
	zbool    = reflect.Zero(reflect.TypeOf(false))
	zint     = reflect.Zero(reflect.TypeOf(int(0)))
	zint8    = reflect.Zero(reflect.TypeOf(int8(0)))
	zint16   = reflect.Zero(reflect.TypeOf(int16(0)))
	zint32   = reflect.Zero(reflect.TypeOf(int32(0)))
	zint64   = reflect.Zero(reflect.TypeOf(int64(0)))
	zuint    = reflect.Zero(reflect.TypeOf(uint(0)))
	zuint8   = reflect.Zero(reflect.TypeOf(uint8(0)))
	zuint16  = reflect.Zero(reflect.TypeOf(uint16(0)))
	zuint32  = reflect.Zero(reflect.TypeOf(uint32(0)))
	zuint64  = reflect.Zero(reflect.TypeOf(uint64(0)))
	zfloat32 = reflect.Zero(reflect.TypeOf(float32(0)))
	zfloat64 = reflect.Zero(reflect.TypeOf(float64(0)))
)

// isNum reports whether k is a numeric kind.
func isNum(k reflect.Kind) bool { return k >= reflect.Bool && k <= reflect.Float64 }

// isFloat reports whether k is a floating-point kind.
func isFloat(k reflect.Kind) bool { return k == reflect.Float32 || k == reflect.Float64 }

// numBits extracts the raw bits from a numeric reflect.Value.
func numBits(rv reflect.Value) uint64 {
	switch rv.Kind() {
	case reflect.Bool:
		if rv.Bool() {
			return 1
		}
		return 0
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return uint64(rv.Int()) //nolint:gosec
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return rv.Uint()
	case reflect.Float32, reflect.Float64:
		return math.Float64bits(rv.Float())
	}
	return 0
}

// NewValue returns a zero value for the specified reflect.Type.
func NewValue(typ reflect.Type, arg ...int) Value {
	if isNum(typ.Kind()) {
		return Value{ref: reflect.New(typ).Elem()}
	}
	switch typ.Kind() {
	case reflect.Slice:
		if len(arg) == 1 {
			v := reflect.New(typ).Elem()
			v.Set(reflect.MakeSlice(typ, arg[0], arg[0]))
			return Value{ref: v}
		}
	case reflect.Map:
		if len(arg) == 1 {
			v := reflect.New(typ).Elem()
			v.Set(reflect.MakeMapWithSize(typ, arg[0]))
			return Value{ref: v}
		}
	case reflect.Func, reflect.Interface:
		// Func/interface variables hold heterogeneous values (int, Closure, Iface).
		// Use interface{} so reflect.Set can accept any of them.
		ifaceType := reflect.TypeOf((*any)(nil)).Elem()
		return Value{ref: reflect.New(ifaceType).Elem()}
	}
	return Value{ref: reflect.New(typ).Elem()}
}

// TypeOf returns the runtime type of v.
func TypeOf(v any) *Type {
	t := reflect.TypeOf(v)
	return &Type{Name: t.Name(), Rtype: t}
}

// ValueOf returns the runtime value of v.
func ValueOf(v any) Value {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return Value{}
	}
	if isNum(rv.Kind()) {
		return Value{num: numBits(rv), ref: reflect.Zero(rv.Type())}
	}
	return Value{ref: rv}
}

// boolVal returns a bool Value without reflect overhead.
func boolVal(b bool) Value {
	v := Value{ref: zbool}
	if b {
		v.num = 1
	}
	return v
}

// Kind returns the reflect.Kind of the value.
func (v Value) Kind() reflect.Kind { return v.ref.Kind() }

// Type returns the reflect.Type of the value.
func (v Value) Type() reflect.Type { return v.ref.Type() }

// IsValid reports whether v represents a value (ref is set).
func (v Value) IsValid() bool { return v.ref.IsValid() }

// Int returns v's value as int64.
func (v Value) Int() int64 { return int64(v.num) } //nolint:gosec

// Uint returns v's value as uint64.
func (v Value) Uint() uint64 { return v.num }

// Float returns v's value as float64.
func (v Value) Float() float64 { return math.Float64frombits(v.num) }

// Bool returns v's value as bool.
func (v Value) Bool() bool { return v.num != 0 }

// Interface returns v's value as interface{}.
func (v Value) Interface() any {
	if v.IsIface() {
		return v.IfaceVal().Val.Interface()
	}
	return v.Reflect().Interface()
}

// CanInt reports whether Int can be called without panicking.
func (v Value) CanInt() bool {
	k := v.ref.Kind()
	return k >= reflect.Int && k <= reflect.Int64
}

// CanAddr reports whether the value is addressable.
func (v Value) CanAddr() bool { return v.ref.CanAddr() }

// Reflect reconstructs a reflect.Value from an inline numeric Value.
// For composite types, returns ref directly.
// This may allocate for numeric types; use only at reflect boundaries.
func (v Value) Reflect() reflect.Value {
	if !v.ref.IsValid() || !isNum(v.ref.Kind()) || v.ref.CanAddr() {
		return v.ref
	}
	// Non addressable numeric value: allocate, set and return a new reflect value.
	r := reflect.New(v.ref.Type()).Elem()
	switch v.ref.Kind() {
	case reflect.Bool:
		r.SetBool(v.num != 0)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		r.SetInt(int64(v.num)) //nolint:gosec
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		r.SetUint(v.num)
	case reflect.Float32, reflect.Float64:
		r.SetFloat(math.Float64frombits(v.num))
	}
	return r
}

// Addr returns a pointer value representing the address of v.
func (v Value) Addr() reflect.Value { return v.ref.Addr() }

// Elem returns the value that the interface v contains or the pointer v points to.
func (v Value) Elem() reflect.Value { return v.ref.Elem() }

// Len returns v's length.
func (v Value) Len() int { return v.ref.Len() }

// Index returns v's i'th element.
func (v Value) Index(i int) reflect.Value { return v.ref.Index(i) }

// Field returns v's i'th field.
func (v Value) Field(i int) reflect.Value { return v.ref.Field(i) }

// FieldByIndex returns the nested field corresponding to index.
func (v Value) FieldByIndex(index []int) reflect.Value { return v.ref.FieldByIndex(index) }

// MapIndex returns the value associated with key in the map v.
func (v Value) MapIndex(key reflect.Value) reflect.Value { return v.ref.MapIndex(key) }

// SetMapIndex sets the element associated with key in the map v.
func (v Value) SetMapIndex(key, elem reflect.Value) { v.ref.SetMapIndex(key, elem) }

// Set assigns x to the value v.
func (v Value) Set(x reflect.Value) { v.ref.Set(x) }

// Slice returns v[i:j].
func (v Value) Slice(i, j int) reflect.Value { return v.ref.Slice(i, j) }

// Slice3 returns v[i:j:k].
func (v Value) Slice3(i, j, k int) reflect.Value { return v.ref.Slice3(i, j, k) }

// Seq returns a range-over iterator for the value v.
func (v Value) Seq() iter.Seq[reflect.Value] { return v.ref.Seq() }

// Seq2 returns a range-over-2 iterator for the value v.
func (v Value) Seq2() iter.Seq2[reflect.Value, reflect.Value] { return v.ref.Seq2() }

// fromReflect wraps a reflect.Value into a Value.
// Numeric types get their bits extracted into num; composites use ref directly.
func fromReflect(rv reflect.Value) Value {
	if isNum(rv.Kind()) {
		return Value{num: numBits(rv), ref: reflect.Zero(rv.Type())}
	}
	return Value{ref: rv}
}

// resetNumRef ensures ref is non-addressable after an arithmetic operation.
// This prevents Reflect()/Interface() from using a stale addressable ref.
// Skips the reflect.Zero call when ref is already non-addressable (common case).
func resetNumRef(v *Value) {
	if v.ref.CanAddr() {
		v.ref = reflect.Zero(v.ref.Type())
	}
}

// IsIface reports whether v holds a boxed interface value.
func (v Value) IsIface() bool {
	if !v.ref.IsValid() {
		return false
	}
	if v.ref.Type() == ifaceRtype {
		return true
	}
	// Check inside interface{} slots: v.ref is an any holding an Iface.
	if v.ref.Kind() == reflect.Interface && v.ref.Elem().IsValid() && v.ref.Elem().Type() == ifaceRtype {
		return true
	}
	return false
}

// IfaceVal extracts the Iface from a boxed interface value.
func (v Value) IfaceVal() Iface {
	if v.ref.Kind() == reflect.Interface {
		return v.ref.Elem().Interface().(Iface)
	}
	return v.ref.Interface().(Iface)
}

// Equal reports whether v is equal to u.
func (v Value) Equal(u Value) bool {
	if v.IsIface() {
		if !u.IsValid() {
			return false // non-nil interface != nil
		}
		if u.IsIface() {
			return v.IfaceVal().Val.Equal(u.IfaceVal().Val)
		}
		return v.IfaceVal().Val.Equal(u)
	}
	if isNum(v.ref.Kind()) && isNum(u.ref.Kind()) {
		return v.num == u.num
	}
	return v.ref.Equal(u.ref)
}

// PointerTo returns the pointer type with element t.
func PointerTo(t *Type) *Type {
	return &Type{Name: t.Name, Rtype: reflect.PointerTo(t.Rtype)}
}

// ArrayOf returns the array type with the given length and element type.
func ArrayOf(length int, t *Type) *Type {
	return &Type{Rtype: reflect.ArrayOf(length, t.Rtype)}
}

// SliceOf returns the slice type with the given element type.
func SliceOf(t *Type) *Type {
	return &Type{Rtype: reflect.SliceOf(t.Rtype)}
}

// MapOf returns the map type with the given key and element types.
func MapOf(k, e *Type) *Type {
	return &Type{Rtype: reflect.MapOf(k.Rtype, e.Rtype)}
}

// FuncOf returns the function type with the given argument and result types.
func FuncOf(arg, ret []*Type, variadic bool) *Type {
	a := make([]reflect.Type, len(arg))
	for i, e := range arg {
		a[i] = e.Rtype
	}
	r := make([]reflect.Type, len(ret))
	for i, e := range ret {
		r[i] = e.Rtype
	}
	return &Type{Rtype: reflect.FuncOf(a, r, variadic)}
}

// StructOf returns the struct type with the given field types and embedded field info.
func StructOf(fields []*Type, embedded []EmbeddedField) *Type {
	rf := make([]reflect.StructField, len(fields))
	embSet := make(map[int]bool, len(embedded))
	for _, e := range embedded {
		embSet[e.FieldIdx] = true
	}
	for i, f := range fields {
		rf[i].Name = f.Name
		rf[i].PkgPath = f.PkgPath
		rf[i].Type = f.Rtype
		rf[i].Anonymous = embSet[i]
	}
	return &Type{Rtype: reflect.StructOf(rf), Embedded: embedded}
}

// FieldIndex returns the index of struct field name.
func (t *Type) FieldIndex(name string) []int {
	for _, f := range reflect.VisibleFields(t.Rtype) {
		if f.Name == name {
			return f.Index
		}
	}
	return nil
}

// FieldType returns the type of struct field name.
func (t *Type) FieldType(name string) *Type {
	for _, f := range reflect.VisibleFields(t.Rtype) {
		if f.Name == name {
			return &Type{Name: f.Name, PkgPath: f.PkgPath, Rtype: f.Type}
		}
	}
	return nil
}

// IsPtr returns true if type t is of pointer kind.
func (t *Type) IsPtr() bool { return t.Rtype.Kind() == reflect.Pointer }
