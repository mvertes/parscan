// Package jsonx is a parscan-aware replacement for the encoding/json
// functions that need to honour parscan-defined methods on struct
// types (MarshalJSON, UnmarshalJSON). Unlike the generated stdlib
// wrappers, jsonx receives raw vm.Value arguments through the VM's
// parscan-aware callable mechanism (Machine.RegisterParscanAware),
// walks parscan *vm.Type metadata to traverse struct fields, and
// dispatches parscan methods via Machine.CallFunc. Values whose
// parscan type is unknown (*vm.Type == nil) are forwarded to the
// native encoding/json implementation so unaffected code is untouched.
package jsonx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/mvertes/parscan/vm"
)

// Register installs the parscan-aware json replacements onto m. Each
// entry in values is a native stdlib binding (by name) that we
// intercept. The stubs are used only for compile-time signature
// matching - at runtime the VM diverts to the callables below.
func Register(m *vm.Machine, values map[string]vm.Value) {
	if stub, ok := values["Marshal"]; ok {
		m.RegisterParscanAware(stub, marshalCallable)
	}
	if stub, ok := values["MarshalIndent"]; ok {
		m.RegisterParscanAware(stub, marshalIndentCallable)
	}
	if stub, ok := values["Unmarshal"]; ok {
		m.RegisterParscanAware(stub, unmarshalCallable)
	}
}

// marshalCallable implements `json.Marshal(v any) ([]byte, error)`.
func marshalCallable(m *vm.Machine, args []vm.Value) []vm.Value {
	if len(args) != 1 {
		return bytesErrResult(nil, fmt.Errorf("json.Marshal: expected 1 arg, got %d", len(args)))
	}
	data, err := marshalValue(m, args[0])
	return bytesErrResult(data, err)
}

// unmarshalCallable implements `json.Unmarshal(data []byte, v any) error`.
func unmarshalCallable(m *vm.Machine, args []vm.Value) []vm.Value {
	if len(args) != 2 {
		return []vm.Value{errValue(fmt.Errorf("json.Unmarshal: expected 2 args, got %d", len(args)))}
	}
	dataRV := args[0].Reflect()
	if !dataRV.IsValid() {
		return []vm.Value{errValue(errors.New("json.Unmarshal: nil data"))}
	}
	data, ok := dataRV.Interface().([]byte)
	if !ok {
		return []vm.Value{errValue(fmt.Errorf("json.Unmarshal: data is not []byte (got %v)", dataRV.Type()))}
	}
	return []vm.Value{errValue(unmarshalValue(m, data, args[1]))}
}

// unmarshalValue decodes data into the destination described by dst.
// dst is expected to hold a pointer (boxed as Iface when parscan code
// passed it through `any`).
func unmarshalValue(m *vm.Machine, data []byte, dst vm.Value) error {
	if dst.IsIface() {
		ifc := dst.IfaceVal()
		return unmarshalIface(m, data, ifc)
	}
	rv := dst.Reflect()
	if !rv.IsValid() {
		return errors.New("json.Unmarshal: nil destination")
	}
	// Pure-native value: delegate.
	return json.Unmarshal(data, rv.Interface())
}

// unmarshalIface decodes data into ifc (a pointer boxed as Iface).
func unmarshalIface(m *vm.Machine, data []byte, ifc vm.Iface) error {
	if ifc.Typ == nil {
		rv := ifc.Val.Reflect()
		if !rv.IsValid() {
			return errors.New("json.Unmarshal: invalid destination")
		}
		return json.Unmarshal(data, rv.Interface())
	}
	// Parscan UnmarshalJSON on the pointer type (or the pointee).
	if ok, err := callUnmarshalJSON(m, data, ifc); ok {
		return err
	}
	rv := ifc.Val.Reflect()
	if !rv.IsValid() {
		return errors.New("json.Unmarshal: invalid destination")
	}
	if ifc.Typ.Rtype.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return errors.New("json.Unmarshal: nil pointer")
		}
		elem := rv.Elem()
		elemTyp := ifc.Typ.ElemType
		return decodeInto(m, data, elem, elemTyp)
	}
	return json.Unmarshal(data, rv.Interface())
}

// decodeInto decodes data into the addressable reflect.Value dst
// (typed via parscan type typ).
func decodeInto(m *vm.Machine, data []byte, dst reflect.Value, typ *vm.Type) error {
	if typ == nil {
		if !dst.CanAddr() {
			return errors.New("json.Unmarshal: destination is not addressable")
		}
		return json.Unmarshal(data, dst.Addr().Interface())
	}
	// If the pointee type has UnmarshalJSON, build a pointer Iface and dispatch.
	if method, ok := m.MethodByName(typ, "UnmarshalJSON"); ok {
		if dst.CanAddr() {
			ptrIfc := vm.Iface{Typ: typ, Val: vm.FromReflect(dst.Addr())}
			return invokeUnmarshalJSON(m, data, ptrIfc, method)
		}
	}
	switch typ.Rtype.Kind() {
	case reflect.Struct:
		return decodeStruct(m, data, dst, typ)
	case reflect.Pointer:
		if dst.IsNil() {
			dst.Set(reflect.New(typ.Rtype.Elem()))
		}
		return decodeInto(m, data, dst.Elem(), typ.ElemType)
	case reflect.Slice:
		return decodeSlice(m, data, dst, typ)
	case reflect.Map:
		return decodeMap(m, data, dst, typ)
	default:
		if !dst.CanAddr() {
			return fmt.Errorf("json.Unmarshal: non-addressable destination for kind %v", typ.Rtype.Kind())
		}
		return json.Unmarshal(data, dst.Addr().Interface())
	}
}

// decodeStruct decodes a JSON object into an addressable struct value.
func decodeStruct(m *vm.Machine, data []byte, dst reflect.Value, typ *vm.Type) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	rtype := typ.Rtype
	for name, piece := range raw {
		idx, fieldTyp := lookupField(rtype, typ, name)
		if idx < 0 {
			continue
		}
		fv := dst.Field(idx)
		if !fv.CanSet() {
			continue
		}
		if err := decodeInto(m, piece, fv, fieldTyp); err != nil {
			return err
		}
	}
	return nil
}

// lookupField finds the struct field matching name (via json tag or
// field name). Returns field index + parscan type (or -1 / nil).
func lookupField(rtype reflect.Type, typ *vm.Type, name string) (int, *vm.Type) {
	// First pass: match explicit json tag names.
	for i := range rtype.NumField() {
		sf := rtype.Field(i)
		if !sf.IsExported() {
			continue
		}
		tagName, _ := parseJSONTag(sf.Tag.Get("json"))
		if tagName == "-" {
			continue
		}
		if tagName != "" && tagName == name {
			return i, fieldTypeAt(typ, i)
		}
	}
	// Second pass: case-exact name, then case-insensitive.
	exact, ci := -1, -1
	for i := range rtype.NumField() {
		sf := rtype.Field(i)
		if !sf.IsExported() {
			continue
		}
		tagName, _ := parseJSONTag(sf.Tag.Get("json"))
		if tagName == "-" || tagName != "" {
			continue
		}
		if sf.Name == name {
			exact = i
			break
		}
		if ci == -1 && strings.EqualFold(sf.Name, name) {
			ci = i
		}
	}
	if exact >= 0 {
		return exact, fieldTypeAt(typ, exact)
	}
	if ci >= 0 {
		return ci, fieldTypeAt(typ, ci)
	}
	return -1, nil
}

func fieldTypeAt(typ *vm.Type, i int) *vm.Type {
	if i < len(typ.Fields) {
		return typ.Fields[i]
	}
	return nil
}

// decodeSlice decodes a JSON array into an addressable slice value.
func decodeSlice(m *vm.Machine, data []byte, dst reflect.Value, typ *vm.Type) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		dst.Set(reflect.Zero(typ.Rtype))
		return nil
	}
	// []byte is encoded as base64 string - delegate.
	if typ.Rtype.Elem().Kind() == reflect.Uint8 {
		var b []byte
		if err := json.Unmarshal(data, &b); err != nil {
			return err
		}
		dst.SetBytes(b)
		return nil
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	sl := reflect.MakeSlice(typ.Rtype, len(raw), len(raw))
	for i, piece := range raw {
		if err := decodeInto(m, piece, sl.Index(i), typ.ElemType); err != nil {
			return err
		}
	}
	dst.Set(sl)
	return nil
}

// decodeMap decodes a JSON object into an addressable map value.
// Existing entries are preserved (encoding/json semantics): keys
// present in data overwrite, others remain untouched.
func decodeMap(m *vm.Machine, data []byte, dst reflect.Value, typ *vm.Type) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		dst.Set(reflect.Zero(typ.Rtype))
		return nil
	}
	if typ.Rtype.Key().Kind() != reflect.String {
		if !dst.CanAddr() {
			return errors.New("json.Unmarshal: non-addressable map destination")
		}
		return json.Unmarshal(data, dst.Addr().Interface())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if dst.IsNil() {
		dst.Set(reflect.MakeMapWithSize(typ.Rtype, len(raw)))
	}
	for k, piece := range raw {
		elem := reflect.New(typ.Rtype.Elem()).Elem()
		if err := decodeInto(m, piece, elem, typ.ElemType); err != nil {
			return err
		}
		dst.SetMapIndex(reflect.ValueOf(k).Convert(typ.Rtype.Key()), elem)
	}
	return nil
}

// callUnmarshalJSON checks whether ifc (a pointer-boxed parscan value)
// has a registered UnmarshalJSON method and dispatches it. ok=false
// means no such method.
func callUnmarshalJSON(m *vm.Machine, data []byte, ifc vm.Iface) (bool, error) {
	method, found := m.MethodByName(ifc.Typ, "UnmarshalJSON")
	if !found {
		return false, nil
	}
	return true, invokeUnmarshalJSON(m, data, ifc, method)
}

// invokeUnmarshalJSON dispatches the parscan UnmarshalJSON method via
// the VM with the given bytes.
func invokeUnmarshalJSON(m *vm.Machine, data []byte, ifc vm.Iface, method vm.Method) error {
	fval := m.MakeMethodCallable(ifc, method)
	fnType := reflect.TypeOf((func([]byte) error)(nil))
	in := []reflect.Value{reflect.ValueOf(data)}
	out, err := m.CallFunc(fval, fnType, in)
	if err != nil {
		return err
	}
	if len(out) != 1 {
		return fmt.Errorf("UnmarshalJSON: expected 1 return, got %d", len(out))
	}
	if out[0].IsValid() && !out[0].IsNil() {
		if e, ok := out[0].Interface().(error); ok {
			return e
		}
	}
	return nil
}

// marshalIndentCallable implements `json.MarshalIndent(v any, prefix, indent string) ([]byte, error)`.
func marshalIndentCallable(m *vm.Machine, args []vm.Value) []vm.Value {
	if len(args) != 3 {
		return bytesErrResult(nil, fmt.Errorf("json.MarshalIndent: expected 3 args, got %d", len(args)))
	}
	data, err := marshalValue(m, args[0])
	if err != nil {
		return bytesErrResult(nil, err)
	}
	prefix, _ := args[1].Reflect().Interface().(string)
	indent, _ := args[2].Reflect().Interface().(string)
	var buf bytes.Buffer
	if err := json.Indent(&buf, data, prefix, indent); err != nil {
		return bytesErrResult(nil, err)
	}
	return bytesErrResult(buf.Bytes(), nil)
}

// marshalValue encodes a single parscan Value as JSON bytes. It is the
// top-level entry point; recursion writes directly into the shared
// buffer via encodeTo to avoid per-level []byte allocations.
func marshalValue(m *vm.Machine, val vm.Value) ([]byte, error) {
	var buf bytes.Buffer
	if err := encodeTo(&buf, m, val); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// encodeTo writes the JSON encoding of val into buf.
func encodeTo(buf *bytes.Buffer, m *vm.Machine, val vm.Value) error {
	if !val.IsValid() {
		buf.WriteString("null")
		return nil
	}
	if val.IsIface() {
		return encodeIfaceTo(buf, m, val.IfaceVal())
	}
	return nativeMarshal(buf, val.Reflect().Interface())
}

// encodeIfaceTo writes the JSON encoding of ifc into buf.
func encodeIfaceTo(buf *bytes.Buffer, m *vm.Machine, ifc vm.Iface) error {
	if ifc.Typ == nil {
		rv := ifc.Val.Reflect()
		if !rv.IsValid() {
			buf.WriteString("null")
			return nil
		}
		return nativeMarshal(buf, rv.Interface())
	}
	if data, ok, err := callMarshalJSON(m, ifc); ok {
		if err != nil {
			return err
		}
		buf.Write(data)
		return nil
	}
	rv := ifc.Val.Reflect()
	if !rv.IsValid() {
		buf.WriteString("null")
		return nil
	}
	switch ifc.Typ.Rtype.Kind() {
	case reflect.Struct:
		return encodeStructTo(buf, m, rv, ifc.Typ)
	case reflect.Pointer:
		if rv.IsNil() {
			buf.WriteString("null")
			return nil
		}
		return encodeIfaceTo(buf, m, vm.Iface{Typ: ifc.Typ.ElemType, Val: vm.FromReflect(rv.Elem())})
	case reflect.Slice, reflect.Array:
		return encodeSliceTo(buf, m, rv, ifc.Typ)
	case reflect.Map:
		return encodeMapTo(buf, m, rv, ifc.Typ)
	case reflect.Interface:
		if rv.IsNil() {
			buf.WriteString("null")
			return nil
		}
		inner := rv.Elem()
		if inner.Type() == ifaceRtype {
			return encodeIfaceTo(buf, m, inner.Interface().(vm.Iface))
		}
		return nativeMarshal(buf, inner.Interface())
	default:
		return nativeMarshal(buf, rv.Interface())
	}
}

// nativeMarshal calls stdlib json.Marshal on v and appends the result
// to buf.
func nativeMarshal(buf *bytes.Buffer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	buf.Write(data)
	return nil
}

// callMarshalJSON dispatches a parscan MarshalJSON method on ifc, if
// one is registered. ok=false means no such method; err propagates the
// method's own error return.
func callMarshalJSON(m *vm.Machine, ifc vm.Iface) (data []byte, ok bool, err error) {
	method, found := m.MethodByName(ifc.Typ, "MarshalJSON")
	if !found {
		return nil, false, nil
	}
	fval := m.MakeMethodCallable(ifc, method)
	fnType := reflect.TypeOf((func() ([]byte, error))(nil))
	out, err := m.CallFunc(fval, fnType, nil)
	if err != nil {
		return nil, true, err
	}
	if len(out) != 2 {
		return nil, true, fmt.Errorf("MarshalJSON: expected 2 returns, got %d", len(out))
	}
	if out[0].IsValid() && !out[0].IsZero() {
		data = out[0].Bytes()
	}
	if out[1].IsValid() && !out[1].IsNil() {
		if e, eok := out[1].Interface().(error); eok {
			err = e
		}
	}
	return data, true, err
}

// encodeStructTo writes a struct as a JSON object into buf, honouring
// json:"name,opts" tags and promoting anonymous struct fields per
// encoding/json semantics.
func encodeStructTo(buf *bytes.Buffer, m *vm.Machine, rv reflect.Value, t *vm.Type) error {
	buf.WriteByte('{')
	first := true
	if err := writeStructFields(buf, &first, m, rv, t); err != nil {
		return err
	}
	buf.WriteByte('}')
	return nil
}

// writeStructFields emits the key-value pairs of a struct into buf
// without surrounding braces, recursing into anonymous struct fields
// whose parscan type has no MarshalJSON of its own.
func writeStructFields(buf *bytes.Buffer, first *bool, m *vm.Machine, rv reflect.Value, t *vm.Type) error {
	rtype := t.Rtype
	embSet := embeddedFieldSet(t)
	for i := range rtype.NumField() {
		sf := rtype.Field(i)
		tag := sf.Tag.Get("json")
		name, opts := parseJSONTag(tag)
		if name == "-" {
			continue
		}
		fv := rv.Field(i)
		var fieldTyp *vm.Type
		if i < len(t.Fields) {
			fieldTyp = t.Fields[i]
		}
		if tag == "" && (sf.Anonymous || embSet[i]) {
			inner, innerTyp, ok := embeddedStructValue(fv, fieldTyp)
			if ok {
				if _, hasMarshal := m.MethodByName(innerTyp, "MarshalJSON"); !hasMarshal {
					if err := writeStructFields(buf, first, m, inner, innerTyp); err != nil {
						return err
					}
					continue
				}
			}
		}
		if !sf.IsExported() {
			continue
		}
		if name == "" {
			name = sf.Name
		}
		if opts.omitempty && isEmptyValue(fv) {
			continue
		}
		if !*first {
			buf.WriteByte(',')
		}
		*first = false
		nameJSON, _ := json.Marshal(name)
		buf.Write(nameJSON)
		buf.WriteByte(':')
		if err := encodeTo(buf, m, fieldValueForMarshal(fv, fieldTyp)); err != nil {
			return err
		}
	}
	return nil
}

// embeddedFieldSet returns the set of field indices marked embedded
// by parscan. Needed because reflect.StructField.Anonymous may be
// false for some embeds (e.g. when the embedded type has methods -
// see vm.StructOf comments).
func embeddedFieldSet(t *vm.Type) map[int]bool {
	if len(t.Embedded) == 0 {
		return nil
	}
	s := make(map[int]bool, len(t.Embedded))
	for _, e := range t.Embedded {
		s[e.FieldIdx] = true
	}
	return s
}

// embeddedStructValue follows a single pointer indirection (like
// encoding/json) and returns the struct value + parscan type when the
// embedded field is a struct (possibly through a pointer).
func embeddedStructValue(fv reflect.Value, typ *vm.Type) (reflect.Value, *vm.Type, bool) {
	if typ == nil {
		return fv, typ, false
	}
	t := typ
	if t.Rtype.Kind() == reflect.Pointer {
		if !fv.IsValid() || fv.IsNil() {
			return fv, t, false
		}
		fv = fv.Elem()
		t = t.ElemType
		if t == nil {
			return fv, nil, false
		}
	}
	if t.Rtype.Kind() != reflect.Struct {
		return fv, t, false
	}
	return fv, t, true
}

// encodeSliceTo writes a slice or array into buf.
func encodeSliceTo(buf *bytes.Buffer, m *vm.Machine, rv reflect.Value, t *vm.Type) error {
	// []byte is encoded as base64 string per encoding/json semantics.
	if t.Rtype.Elem().Kind() == reflect.Uint8 {
		return nativeMarshal(buf, rv.Bytes())
	}
	if rv.Kind() == reflect.Slice && rv.IsNil() {
		buf.WriteString("null")
		return nil
	}
	buf.WriteByte('[')
	n := rv.Len()
	for i := range n {
		if i > 0 {
			buf.WriteByte(',')
		}
		if err := encodeTo(buf, m, fieldValueForMarshal(rv.Index(i), t.ElemType)); err != nil {
			return err
		}
	}
	buf.WriteByte(']')
	return nil
}

// encodeMapTo writes a map into buf. Only string-keyed maps are handled
// directly; other key kinds defer to the native encoder (which goes
// through encoding.TextMarshaler).
func encodeMapTo(buf *bytes.Buffer, m *vm.Machine, rv reflect.Value, t *vm.Type) error {
	if rv.IsNil() {
		buf.WriteString("null")
		return nil
	}
	if t.Rtype.Key().Kind() != reflect.String {
		return nativeMarshal(buf, rv.Interface())
	}
	keys := rv.MapKeys()
	sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
	buf.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		nameJSON, _ := json.Marshal(k.String())
		buf.Write(nameJSON)
		buf.WriteByte(':')
		if err := encodeTo(buf, m, fieldValueForMarshal(rv.MapIndex(k), t.ElemType)); err != nil {
			return err
		}
	}
	buf.WriteByte('}')
	return nil
}

// fieldValueForMarshal wraps a reflect.Value into a parscan Value,
// attaching the parscan type info when available so recursive
// marshalValue can dispatch methods on the field's type.
func fieldValueForMarshal(rv reflect.Value, typ *vm.Type) vm.Value {
	if typ == nil {
		return vm.FromReflect(rv)
	}
	// Iface-typed slot already holds an Iface - pass through.
	if rv.IsValid() && rv.Type() == ifaceRtype {
		return vm.FromReflect(rv)
	}
	// Re-box the field with its known parscan type so downstream
	// method dispatch works even for StructOf-built types.
	return vm.FromReflect(reflect.ValueOf(vm.Iface{Typ: typ, Val: vm.FromReflect(rv)}))
}

// --- helpers ---

type jsonOpts struct {
	omitempty bool
	asString  bool
}

func parseJSONTag(tag string) (name string, opts jsonOpts) {
	if tag == "" {
		return "", opts
	}
	parts := strings.Split(tag, ",")
	name = parts[0]
	for _, p := range parts[1:] {
		switch p {
		case "omitempty":
			opts.omitempty = true
		case "string":
			opts.asString = true
		}
	}
	return name, opts
}

func isEmptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Interface, reflect.Pointer:
		return v.IsNil()
	}
	return false
}

var (
	errIfaceType = reflect.TypeOf((*error)(nil)).Elem()
	bytesType    = reflect.TypeOf([]byte{})
	ifaceRtype   = reflect.TypeOf(vm.Iface{})
	zeroBytesV   = vm.FromReflect(reflect.Zero(bytesType))
	zeroErrorV   = vm.FromReflect(reflect.Zero(errIfaceType))
)

// errValue returns a vm.Value representing an error return slot.
func errValue(err error) vm.Value {
	if err == nil {
		return zeroErrorV
	}
	return vm.FromReflect(reflect.ValueOf(&err).Elem())
}

// bytesErrResult packs (data, err) as the two-value result of
// Marshal-style callables.
func bytesErrResult(data []byte, err error) []vm.Value {
	bVal := zeroBytesV
	if data != nil {
		bVal = vm.FromReflect(reflect.ValueOf(data))
	}
	return []vm.Value{bVal, errValue(err)}
}
