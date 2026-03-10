package vm

import (
	"iter"
	"math"
	"reflect"
)

// Runtime type and value representations (based on reflect).

// Type is the representation of a runtime type.
type Type struct {
	PkgPath string
	Name    string
	Rtype   reflect.Type
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
func (t *Type) Elem() *Type { return &Type{Rtype: t.Rtype.Elem()} }

// Out returns the type's i'th output parameter.
func (t *Type) Out(i int) *Type { return &Type{Rtype: t.Rtype.Out(i)} }

// Value is the VM runtime value.
// Numeric types (bool, int*, uint*, float*) store their value inline in num.
// ref carries reflect.Zero(t) for type metadata on numeric types.
// Composite types (string, slice, map, struct, ptr, func, interface) use ref.
type Value struct {
	num uint64        // inline storage for numeric types (bool, int*, uint*, float*)
	ref reflect.Value // composite data OR reflect.Zero(t) for numeric type metadata
}

// Pre-computed zero reflect.Values for common numeric types (zero allocation).
var (
	zeroInt  = reflect.Zero(reflect.TypeOf(int(0)))
	zeroBool = reflect.Zero(reflect.TypeOf(false))
)

// isNum reports whether k is a numeric kind (bool through float64).
func isNum(k reflect.Kind) bool { return k >= reflect.Bool && k <= reflect.Float64 }

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
	case reflect.Func:
		// Function variables hold either a plain code address (int) or a Closure.
		// Use interface{} so reflect.Set can write either type through the shared pointer.
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

// Kind returns the reflect.Kind of the value.
func (v Value) Kind() reflect.Kind { return v.ref.Kind() }

// Type returns the reflect.Type of the value.
func (v Value) Type() reflect.Type { return v.ref.Type() }

// IsValid reports whether v represents a value (ref is set).
func (v Value) IsValid() bool { return v.ref.IsValid() }

// Int returns v's value as int64 (for numeric values stored in num).
func (v Value) Int() int64 { return int64(v.num) } //nolint:gosec

// Uint returns v's value as uint64 (for numeric values stored in num).
func (v Value) Uint() uint64 { return v.num }

// Float returns v's value as float64 (for numeric values stored in num).
func (v Value) Float() float64 { return math.Float64frombits(v.num) }

// Bool returns v's value as bool (for numeric values stored in num).
func (v Value) Bool() bool { return v.num != 0 }

// Interface returns v's value as interface{}.
func (v Value) Interface() any { return v.Reflect().Interface() }

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

// Equal reports whether v is equal to u.
func (v Value) Equal(u Value) bool {
	if isNum(v.ref.Kind()) && isNum(u.ref.Kind()) {
		return v.num == u.num
	}
	return v.ref.Equal(u.ref)
}

// PointerTo returns the pointer type with element t.
func PointerTo(t *Type) *Type {
	return &Type{Rtype: reflect.PointerTo(t.Rtype)}
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

// StructOf returns the struct type with the given field types.
func StructOf(fields []*Type) *Type {
	rf := make([]reflect.StructField, len(fields))
	for i, f := range fields {
		rf[i].Name = f.Name
		rf[i].PkgPath = f.PkgPath
		rf[i].Type = f.Rtype
	}
	return &Type{Rtype: reflect.StructOf(rf)}
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
