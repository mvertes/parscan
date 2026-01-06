package vm

import "reflect"

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
func (t *Type) Elem() *Type {
	return &Type{Rtype: t.Rtype.Elem()}
}

// Out returns the type's i'th output parameter.
func (t *Type) Out(i int) *Type {
	return &Type{Rtype: t.Rtype.Out(i)}
}

// Value is the representation of a runtime value.
type Value struct {
	*Type
	reflect.Value
}

// NewValue returns an addressable zero value for the specified type.
func NewValue(typ *Type) Value {
	if typ.Rtype.Kind() == reflect.Func {
		typ = TypeOf(0) // Function value is its index in the code segment.
	}
	return Value{Type: typ, Value: reflect.New(typ.Rtype).Elem()}
}

// TypeOf returns the runtime type of v.
func TypeOf(v any) *Type {
	t := reflect.TypeOf(v)
	return &Type{Name: t.Name(), Rtype: t}
}

// ValueOf returns the runtime value of v.
func ValueOf(v any) Value {
	return Value{Value: reflect.ValueOf(v)}
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
