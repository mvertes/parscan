package vm

import (
	"reflect"
	"strconv"
	"sync/atomic"
	"unsafe" //nolint:depguard
)

var (
	// placeholderSeq ensures each placeholder gets a unique reflect type,
	// preventing reflect.StructOf from returning a cached (shared) rtype.
	placeholderSeq atomic.Uint64

	// structTypeSize is the byte size of reflect's internal structType.
	// Computed at init time by probing reflect internals.
	structTypeSize uintptr

	intRtype = reflect.TypeOf(0)
)

func init() {
	// Create a struct type with a distinctive field count and scan for
	// the Fields slice header to determine structType size.
	// structType layout: abi.Type (base) + abi.Name (*byte) + []structField.
	// The slice header is fixed-size, so structType has the same size
	// regardless of field count.
	const nfields = 7
	sf := make([]reflect.StructField, nfields)
	for i := range sf {
		sf[i] = reflect.StructField{Name: string(rune('A' + i)), Type: intRtype}
	}
	rt := reflect.StructOf(sf)
	data := rtypeData(rt)
	ws := unsafe.Sizeof(uintptr(0))
	for off := ws; off < 256; off += ws {
		lenp := (*int)(unsafe.Add(data, off+ws))
		capp := (*int)(unsafe.Add(data, off+2*ws))
		if *lenp == nfields && *capp >= nfields {
			structTypeSize = off + 3*ws
			return
		}
	}
	panic("vm: cannot determine reflect structType size")
}

// rtypeData extracts the *rtype data pointer from a reflect.Type interface.
func rtypeData(t reflect.Type) unsafe.Pointer {
	return (*[2]unsafe.Pointer)(unsafe.Pointer(&t))[1]
}

// patchRtype overwrites dst's internal rtype with src's rtype bytes,
// then clears the Str (nameOff) and PtrToThis (typeOff) fields.
//
// These 4-byte offsets (at byte offsets 40 and 44 in abi.Type) are registered
// in reflect's global offset map for the source rtype's heap address. After
// copying them into the destination (which has a different address), the
// runtime cannot resolve them and crashes with "nameOff/typeOff base pointer
// out of range". Zeroing them forces reflect to use safe slow paths.
func patchRtype(dst, src reflect.Type) {
	d := rtypeData(dst)
	s := rtypeData(src)
	for i := uintptr(0); i < structTypeSize; i++ {
		*(*byte)(unsafe.Add(d, i)) = *(*byte)(unsafe.Add(s, i))
	}
	*(*int32)(unsafe.Add(d, 40)) = 0 // Str (nameOff)
	*(*int32)(unsafe.Add(d, 44)) = 0 // PtrToThis (typeOff)
}

// NewStructType creates a forward-declared struct type.
// Register it in the symbol table, then call SetFields to finalize.
func NewStructType() *Type {
	// Each placeholder must have a unique field name to prevent reflect.StructOf
	// from returning a cached (shared) rtype, which would cause data races when
	// multiple struct types are patched concurrently.
	n := placeholderSeq.Add(1)
	sf := []reflect.StructField{{Name: "P" + strconv.FormatUint(n, 10), Type: intRtype}}
	return &Type{Rtype: reflect.StructOf(sf), Placeholder: true}
}

// SetFields finalizes a forward-declared struct type using src's definition.
// It patches the internal reflect.Type in place so that any derived types
// (e.g., pointer types created via PointerTo) automatically see the real layout.
func (t *Type) SetFields(src *Type) {
	patchRtype(t.Rtype, src.Rtype)
	t.Fields = src.Fields
	t.Embedded = src.Embedded
	t.Placeholder = false
}
