package vm

import "reflect"

// Runtime type and value representations (based on reflect).

// Type is the representation of a runtime type.
type Type struct {
	Name  string
	Rtype reflect.Type
}

func (t *Type) Elem() *Type {
	return &Type{Rtype: t.Rtype.Elem()}
}

func (t *Type) Out(i int) *Type {
	return &Type{Rtype: t.Rtype.Out(i)}
}

// Value is the representation of a runtime value.
type Value struct {
	Type *Type
	Data reflect.Value
}

// NewValue returns an addressable zero value for the specified type.
func NewValue(typ *Type) Value {
	return Value{Type: typ, Data: reflect.New(typ.Rtype).Elem()}
}

// TypeOf returns the runtime type of v.
func TypeOf(v any) *Type {
	t := reflect.TypeOf(v)
	return &Type{Name: t.Name(), Rtype: t}
}

// ValueOf returns the runtime value of v.
func ValueOf(v any) Value {
	return Value{Data: reflect.ValueOf(v)}
}

func PointerTo(t *Type) *Type {
	return &Type{Rtype: reflect.PointerTo(t.Rtype)}
}

func ArrayOf(size int, t *Type) *Type {
	return &Type{Rtype: reflect.ArrayOf(size, t.Rtype)}
}

func SliceOf(t *Type) *Type {
	return &Type{Rtype: reflect.SliceOf(t.Rtype)}
}

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

func StructOf(fields []*Type) *Type {
	rf := make([]reflect.StructField, len(fields))
	for i, f := range fields {
		rf[i].Name = "X" + f.Name
		rf[i].Type = f.Rtype
	}
	return &Type{Rtype: reflect.StructOf(rf)}
}
