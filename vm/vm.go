// Package vm implement a stack based virtual machine.
package vm

import (
	"fmt" // for tracing only
	"iter"
	"log"     // for tracing only
	"math"    // for float arithmetic
	"reflect" // for optional CallX only
	"strings"
	"unsafe" // to allow setting unexported struct fields //nolint:depguard
)

const debug = false

// Op is a VM opcode (bytecode instruction).
type Op int

//go:generate stringer -type=Op

// Closure bundles a function code address with its captured variables.
type Closure struct {
	Code int      // code address (same as the plain-int function value)
	Env  []*Value // heap-allocated cells, one per captured variable
}

// Byte-code instruction set.
const (
	// Instruction effect on stack: values consumed -- values produced.
	Nop          Op = iota // --
	Add                    // n1 n2 -- sum ; sum = n1+n2
	Addr                   // a -- &a ;
	Call                   // f [a1 .. ai] -- [r1 .. rj] ; r1, ... = prog[f](a1, ...)
	CallX                  // f [a1 .. ai] -- [r1 .. rj] ; r1, ... = mem[f](a1, ...)
	Deref                  // x -- *x ;
	Get                    // addr -- value ; value = mem[addr]
	Fnew                   // -- x; x = new mem[$1]
	FnewE                  // -- x; x = new mem[$1].Elem()
	Equal                  // n1 n2 -- cond ; cond = n1 == n2
	EqualSet               // n1 n2 -- n1 cond ; cond = n1 == n2
	Exit                   // -- ;
	Field                  // s -- f ; f = s.FieldIndex($1, ...)
	FieldSet               // s d -- s ; s.FieldIndex($1, ...) = d
	FieldFset              // s i v -- s; s.FieldIndex(i) = v
	Greater                // n1 n2 -- cond; cond = n1 > n2
	Grow                   // -- ; sp += $1
	Index                  // a i -- a[i] ;
	IndexSet               // a i v -- a; a[i] = v
	Jump                   // -- ; ip += $1
	JumpTrue               // cond -- ; if cond { ip += $1 }
	JumpFalse              // cond -- ; if cond { ip += $1 }
	JumpSetTrue            //
	JumpSetFalse           //
	Len                    // -- x; x = mem[sp-$1]
	Lower                  // n1 n2 -- cond ; cond = n1 < n2
	MapIndex               // a i -- a[i]
	MapSet                 // a i v -- a; a[i] = v
	Mul                    // n1 n2 -- prod ; prod = n1*n2
	New                    // -- x; mem[fp+$1] = new mem[$2]
	Negate                 // -- ; - mem[fp]
	Next                   // -- ; iterator next, set K
	Next2                  // -- ; iterator next, set K V
	Not                    // c -- r ; r = !c
	Pop                    // v --
	Push                   // -- v
	Pull                   // a -- a s n; pull iterator next and stop function
	Pull2                  // a -- a s n; pull iterator next and stop function
	Return                 // [r1 .. ri] -- ; exit frame: sp = fp, fp = pop
	Set                    // v --  ; mem[$1,$2] = v
	SetS                   // dest val -- ; dest.Set(val)
	Slice                  // a l h -- a; a = a [l:h]
	Slice3                 // a l h m -- a; a = a[l:h:m]
	Stop                   // -- iterator stop
	Sub                    // n1 n2 -- diff ; diff = n1 - n2
	Swap                   // --
	HAlloc                 // -- &cell ; cell = new(Value), push its pointer
	HGet                   // -- v    ; v = *State.Env[$1]
	HSet                   // v --    ; *State.Env[$1] = v
	HPtr                   // -- &cell ; push State.Env[$1] itself (transitive capture)
	MkClosure              // code [&c0..&cn-1] -- clo ; clo = Closure{code, env}
	Convert                // v -- v' ; v' = convert(v, type at mem[$1])
	IfaceWrap              // v -- iface ; wrap v in Iface{type at $1, v}
	IfaceCall              // iface -- closure ; dynamic dispatch method $1 on iface
	TypeAssert             // iface -- v [ok] ; assert iface holds type at mem[$1]; $2=0 panics, $2=1 ok form

	// Per-type numeric opcodes. Each block of NumTypes (12) opcodes follows the
	// order: Int, Int8, Int16, Int32, Int64, Uint, Uint8, Uint16, Uint32, Uint64, Float32, Float64.
	// The compiler computes: baseOp + Op(NumKindOffset[kind]).

	AddInt // n1 n2 -- sum
	AddInt8
	AddInt16
	AddInt32
	AddInt64
	AddUint
	AddUint8
	AddUint16
	AddUint32
	AddUint64
	AddFloat32
	AddFloat64

	SubInt // n1 n2 -- diff
	SubInt8
	SubInt16
	SubInt32
	SubInt64
	SubUint
	SubUint8
	SubUint16
	SubUint32
	SubUint64
	SubFloat32
	SubFloat64

	MulInt // n1 n2 -- prod
	MulInt8
	MulInt16
	MulInt32
	MulInt64
	MulUint
	MulUint8
	MulUint16
	MulUint32
	MulUint64
	MulFloat32
	MulFloat64

	NegateInt // n -- -n
	NegateInt8
	NegateInt16
	NegateInt32
	NegateInt64
	NegateUint
	NegateUint8
	NegateUint16
	NegateUint32
	NegateUint64
	NegateFloat32
	NegateFloat64

	GreaterInt // n1 n2 -- cond
	GreaterInt8
	GreaterInt16
	GreaterInt32
	GreaterInt64
	GreaterUint
	GreaterUint8
	GreaterUint16
	GreaterUint32
	GreaterUint64
	GreaterFloat32
	GreaterFloat64

	LowerInt // n1 n2 -- cond
	LowerInt8
	LowerInt16
	LowerInt32
	LowerInt64
	LowerUint
	LowerUint8
	LowerUint16
	LowerUint32
	LowerUint64
	LowerFloat32
	LowerFloat64

	DivInt // n1 n2 -- quot
	DivInt8
	DivInt16
	DivInt32
	DivInt64
	DivUint
	DivUint8
	DivUint16
	DivUint32
	DivUint64
	DivFloat32
	DivFloat64

	RemInt // n1 n2 -- rem (integer only)
	RemInt8
	RemInt16
	RemInt32
	RemInt64
	RemUint
	RemUint8
	RemUint16
	RemUint32
	RemUint64
	RemFloat32 // unused, but keeps NumTypes alignment
	RemFloat64 // unused, but keeps NumTypes alignment

	// Bitwise opcodes (generic, operate on raw uint64 bits).
	BitAnd    // n1 n2 -- n1 & n2
	BitOr     // n1 n2 -- n1 | n2
	BitXor    // n1 n2 -- n1 ^ n2
	BitAndNot // n1 n2 -- n1 &^ n2
	BitShl    // n1 n2 -- n1 << n2
	BitShr    // n1 n2 -- n1 >> n2 (arithmetic for signed)
	BitComp   // n -- ^n
)

// Memory attributes.
const (
	Global = 0
	Local  = 1
)

// Pos is the source code position of instruction.
type Pos int

// Instruction represents a virtual machine bytecode instruction.
type Instruction struct {
	Pos       // position in source
	Op        // opcode
	Arg []int // arguments
}

func (i Instruction) String() (s string) {
	s = fmt.Sprintf("%3d: %v", i.Pos, i.Op)
	var sb strings.Builder
	for _, a := range i.Arg {
		fmt.Fprintf(&sb, " %v", a)
	}
	s += sb.String()
	return s
}

// Code represents the virtual machine byte code.
type Code []Instruction

// Machine represents a virtual machine.
type Machine struct {
	code     Code       // code to execute
	mem      []Value    // memory, as a stack
	ip, fp   int        // instruction pointer and frame pointer
	ic       uint64     // instruction counter, incremented at each instruction executed
	env      []*Value   // active closure's captured cells (nil for plain functions)
	captured [][]*Value // saved env per call frame
	// flags  uint      // to set options such as restrict CallX, etc...
}

// Run runs a program.
func (m *Machine) Run() (err error) {
	mem, ip, fp, sp, ic := m.mem, m.ip, m.fp, 0, m.ic

	defer func() { m.mem, m.ip, m.fp, m.ic = mem, ip, fp, ic }()

	for {
		sp = len(mem)   // stack pointer
		c := m.code[ip] // current instruction
		if debug {
			log.Printf("ip:%-3d sp:%-3d fp:%-3d op:[%-20v] mem:%v\n", ip, sp, fp, c, Vstring(mem))
		}
		ic++
		switch c.Op {
		case Add:
			switch k := mem[sp-2].ref.Kind(); {
			case k == reflect.String:
				mem[sp-2] = ValueOf(mem[sp-2].ref.String() + mem[sp-1].ref.String())
			case isFloat(k):
				mem[sp-2].num = math.Float64bits(math.Float64frombits(mem[sp-2].num) + math.Float64frombits(mem[sp-1].num))
				resetNumRef(&mem[sp-2])
			default:
				mem[sp-2].num = uint64(int64(mem[sp-2].num) + int64(mem[sp-1].num)) //nolint:gosec
				resetNumRef(&mem[sp-2])
			}
			mem = mem[:sp-1]
		case Mul:
			if isFloat(mem[sp-2].ref.Kind()) {
				mem[sp-2].num = math.Float64bits(math.Float64frombits(mem[sp-2].num) * math.Float64frombits(mem[sp-1].num))
			} else {
				mem[sp-2].num = uint64(int64(mem[sp-2].num) * int64(mem[sp-1].num)) //nolint:gosec
			}
			resetNumRef(&mem[sp-2])
			mem = mem[:sp-1]
		case Addr:
			v := mem[sp-1]
			if v.ref.CanAddr() {
				mem[sp-1] = Value{ref: v.ref.Addr()}
			} else {
				// Materialize via Reflect() to get an addressable value, then take its address.
				mem[sp-1] = Value{ref: v.Reflect().Addr()}
			}
		case Set:
			assignSlot(&mem[c.Arg[0]*(fp-1)+c.Arg[1]], mem[sp-1])
			mem = mem[:sp-1]
		case Call:
			fval := mem[sp-1-c.Arg[0]]
			prevEnv := m.env
			var nip int
			if isNum(fval.ref.Kind()) {
				// Plain int code address stored inline in num.
				nip = int(fval.num) //nolint:gosec
				m.env = nil
			} else if clo, ok := fval.ref.Interface().(Closure); ok {
				nip = clo.Code
				m.env = clo.Env
			} else if iv, ok := fval.ref.Interface().(int); ok {
				// Function variable slot holds a plain code address boxed as interface{}.
				nip = iv
				m.env = nil
			} else {
				nip = int(fval.num) //nolint:gosec
				m.env = nil
			}
			m.captured = append(m.captured, prevEnv)
			mem = append(mem, ValueOf(ip+1), ValueOf(fp))
			ip = nip
			fp = sp + 2
			continue
		case CallX: // Should be made optional.
			in := make([]reflect.Value, c.Arg[0])
			for i := range in {
				in[i] = mem[sp-1-i].Reflect()
			}
			f := mem[sp-1-c.Arg[0]].ref
			mem = mem[:sp-c.Arg[0]-1]
			for _, v := range f.Call(in) {
				mem = append(mem, fromReflect(v))
			}
		case Deref:
			mem[sp-1] = Value{ref: mem[sp-1].ref.Elem()}
		case Get:
			v := mem[c.Arg[0]*(fp-1)+c.Arg[1]]
			if isNum(v.ref.Kind()) && v.ref.CanAddr() {
				v.num = numBits(v.ref)
			}
			mem = append(mem, v)
		case New:
			mem[c.Arg[0]+fp-1] = NewValue(mem[c.Arg[1]].ref.Type())
		case Equal:
			mem[sp-2] = ValueOf(mem[sp-2].Equal(mem[sp-1]))
			mem = mem[:sp-1]
		case EqualSet:
			if mem[sp-2].Equal(mem[sp-1]) {
				// If equal then lhs and rhs are popped, replaced by test result, as in Equal.
				mem[sp-2] = ValueOf(true)
				mem = mem[:sp-1]
			} else {
				// If not equal then the lhs is let on stack for further processing.
				// This is used to simplify bytecode in case clauses of switch statments.
				mem[sp-1] = ValueOf(false)
			}
		case Convert:
			v := mem[sp-1]
			dstType := mem[c.Arg[0]].ref.Type()
			srcKind := v.ref.Type().Kind()
			dstKind := dstType.Kind()

			switch {
			case isNum(srcKind) && isNum(dstKind):
				bits := v.num
				switch {
				case isFloat(srcKind) && isFloat(dstKind):
					// float32 -> float64 or float64 -> float32: re-precision.
					if srcKind != dstKind {
						f := math.Float64frombits(bits)
						if dstKind == reflect.Float32 {
							bits = math.Float64bits(float64(float32(f)))
						}
					}
				case isFloat(srcKind):
					// float -> int: truncate.
					f := math.Float64frombits(bits)
					bits = uint64(int64(f)) //nolint:gosec
				case isFloat(dstKind):
					// int -> float.
					if srcKind >= reflect.Uint && srcKind <= reflect.Uintptr {
						bits = math.Float64bits(float64(bits))
					} else {
						bits = math.Float64bits(float64(int64(bits))) //nolint:gosec
					}
				}
				// Truncate to target width for sub-word types.
				switch dstKind {
				case reflect.Int8:
					bits = uint64(int8(bits)) //nolint:gosec
				case reflect.Int16:
					bits = uint64(int16(bits)) //nolint:gosec
				case reflect.Int32:
					bits = uint64(int32(bits)) //nolint:gosec
				case reflect.Uint8:
					bits = uint64(uint8(bits)) //nolint:gosec
				case reflect.Uint16:
					bits = uint64(uint16(bits)) //nolint:gosec
				case reflect.Uint32:
					bits = uint64(uint32(bits)) //nolint:gosec
				case reflect.Float32:
					bits = math.Float64bits(float64(float32(math.Float64frombits(bits))))
				}
				off := NumKindOffset[dstKind]
				mem[sp-1] = Value{num: bits, ref: numZero[off]}

			case isNum(srcKind) && dstKind == reflect.String:
				// int/rune -> string (e.g. string(65) -> "A").
				mem[sp-1] = Value{ref: reflect.ValueOf(string(rune(int64(v.num))))} //nolint:gosec

			case srcKind == reflect.String && dstKind == reflect.Slice && dstType.Elem().Kind() == reflect.Uint8:
				// string -> []byte.
				mem[sp-1] = Value{ref: reflect.ValueOf([]byte(v.ref.String()))}

			case srcKind == reflect.Slice && v.ref.Type().Elem().Kind() == reflect.Uint8 && dstKind == reflect.String:
				// []byte -> string.
				mem[sp-1] = Value{ref: reflect.ValueOf(string(v.ref.Bytes()))}

			default:
				// Fallback: use reflect.
				mem[sp-1] = fromReflect(v.Reflect().Convert(dstType))
			}

		case IfaceWrap:
			typ := mem[c.Arg[0]].ref.Interface().(*Type)
			mem[sp-1] = Value{ref: reflect.ValueOf(Iface{Typ: typ, Val: mem[sp-1]})}

		case IfaceCall:
			ifc := mem[sp-1].IfaceVal()
			method := ifc.Typ.Methods[c.Arg[0]]
			codeAddr := int(mem[method.Index].num) //nolint:gosec
			// Build a closure with the concrete receiver as Env[0], replacing the
			// interface value on the stack. Same result as HAlloc+Get+Swap+MkClosure.
			// For promoted methods, extract the embedded field as receiver.
			cell := new(Value)
			*cell = ifc.Val
			if path := method.Path; path != nil {
				rv := reflect.Indirect(ifc.Val.Reflect())
				for _, idx := range path {
					if rv.Kind() == reflect.Pointer {
						rv = rv.Elem()
					}
					rv = rv.Field(idx)
				}
				*cell = fromReflect(rv)
			}
			mem[sp-1] = Value{ref: reflect.ValueOf(Closure{Code: codeAddr, Env: []*Value{cell}})}

		case TypeAssert:
			dstTyp := mem[c.Arg[0]].ref.Interface().(*Type)
			okForm := c.Arg[1] == 1
			ifc := mem[sp-1]
			if !ifc.IsIface() {
				if !okForm {
					// FIXME: to be replaced with a vm panic operator which stops the vm, returns
					// an error, but does not crash the program.
					panic(fmt.Sprintf("interface conversion: interface is nil, not %s", dstTyp))
				}
				mem[sp-1] = ValueOf(false)
				mem = append(mem, NewValue(dstTyp.Rtype))
				break
			}
			if concrete := ifc.IfaceVal(); concrete.Typ == dstTyp {
				if okForm {
					mem[sp-1] = ValueOf(true)
					mem = append(mem, concrete.Val)
				} else {
					mem[sp-1] = concrete.Val
				}
			} else {
				if !okForm {
					// FIXME: replace with a vm panic operator when ready.
					panic(fmt.Sprintf("interface conversion: interface value is %s, not %s", concrete.Typ, dstTyp))
				}
				mem[sp-1] = ValueOf(false)
				mem = append(mem, NewValue(dstTyp.Rtype))
			}

		case Exit:
			return err
		case Fnew:
			mem = append(mem, NewValue(mem[c.Arg[0]].ref.Type(), c.Arg[1:]...))
		case FnewE:
			mem = append(mem, NewValue(mem[c.Arg[0]].ref.Type().Elem(), c.Arg[1:]...))
		case Field:
			fv := forceSettable(reflect.Indirect(mem[sp-1].ref).FieldByIndex(c.Arg))
			if isNum(fv.Kind()) {
				// Preserve addressable ref for write-through on struct field mutations.
				mem[sp-1] = Value{num: numBits(fv), ref: fv}
			} else {
				mem[sp-1] = Value{ref: fv}
			}
		case FieldSet:
			forceSettable(mem[sp-2].ref.FieldByIndex(c.Arg)).Set(mem[sp-1].Reflect())
			mem = mem[:sp-1]
		case FieldFset:
			fv := forceSettable(mem[sp-3].ref.Field(int(mem[sp-2].num))) //nolint:gosec
			fv.Set(mem[sp-1].Reflect())
			mem = mem[:sp-2]
		case Jump:
			ip += c.Arg[0]
			continue
		case JumpTrue:
			cond := mem[sp-1].num != 0
			mem = mem[:sp-1]
			if cond {
				ip += c.Arg[0]
				continue
			}
		case JumpFalse:
			cond := mem[sp-1].num != 0
			mem = mem[:sp-1]
			if !cond {
				ip += c.Arg[0]
				continue
			}
		case JumpSetTrue:
			cond := mem[sp-1].num != 0
			if cond {
				ip += c.Arg[0]
				// Note that the stack is not modified if cond is true.
				continue
			}
			mem = mem[:sp-1]
		case JumpSetFalse:
			cond := mem[sp-1].num != 0
			if !cond {
				ip += c.Arg[0]
				// Note that the stack is not modified if cond is false.
				continue
			}
			mem = mem[:sp-1]
		case Greater:
			if isFloat(mem[sp-2].ref.Kind()) {
				mem[sp-2] = ValueOf(math.Float64frombits(mem[sp-2].num) > math.Float64frombits(mem[sp-1].num))
			} else {
				mem[sp-2] = ValueOf(int64(mem[sp-2].num) > int64(mem[sp-1].num)) //nolint:gosec
			}
			mem = mem[:sp-1]
		case Lower:
			if isFloat(mem[sp-2].ref.Kind()) {
				mem[sp-2] = ValueOf(math.Float64frombits(mem[sp-2].num) < math.Float64frombits(mem[sp-1].num))
			} else {
				mem[sp-2] = ValueOf(int64(mem[sp-2].num) < int64(mem[sp-1].num)) //nolint:gosec
			}
			mem = mem[:sp-1]
		case Len:
			mem = append(mem, ValueOf(mem[sp-1-c.Arg[0]].ref.Len()))
		case Negate:
			if isFloat(mem[sp-1].ref.Kind()) {
				mem[sp-1].num = math.Float64bits(-math.Float64frombits(mem[sp-1].num))
			} else {
				mem[sp-1].num = uint64(-int64(mem[sp-1].num)) //nolint:gosec
			}
			resetNumRef(&mem[sp-1])
		case Next:
			if k, ok := mem[sp-2].ref.Interface().(func() (reflect.Value, bool))(); ok {
				mem[c.Arg[1]].Set(k)
			} else {
				ip += c.Arg[0]
				continue
			}
		case Next2:
			if k, v, ok := mem[sp-2].ref.Interface().(func() (reflect.Value, reflect.Value, bool))(); ok {
				mem[c.Arg[1]].Set(k)
				mem[c.Arg[2]].Set(v)
			} else {
				ip += c.Arg[0]
				continue
			}
		case Not:
			if mem[sp-1].num != 0 {
				mem[sp-1].num = 0
			} else {
				mem[sp-1].num = 1
			}
			mem[sp-1].ref = zeroBool
		case Pop:
			mem = mem[:sp-c.Arg[0]]
		case Push:
			mem = append(mem, Value{num: uint64(c.Arg[0]), ref: numZero[0]}) //nolint:gosec
		case Pull:
			next, stop := iter.Pull(mem[sp-1].Seq())
			mem = append(mem, ValueOf(next), ValueOf(stop))
		case Pull2:
			next, stop := iter.Pull2(mem[sp-1].Seq2())
			mem = append(mem, ValueOf(next), ValueOf(stop))
		case Grow:
			mem = append(mem, make([]Value, c.Arg[0])...)
		case Return:
			ip = int(mem[fp-2].num) //nolint:gosec
			ofp := fp
			fp = int(mem[fp-1].num) //nolint:gosec
			nret := c.Arg[0]
			newBase := ofp - nret - c.Arg[1] - 2
			copy(mem[newBase:], mem[sp-nret:sp])
			newSP := newBase + nret
			for i := newSP; i < sp; i++ {
				mem[i] = Value{} // zero stale slots so GC can reclaim references
			}
			mem = mem[:newSP]
			if top := len(m.captured) - 1; top >= 0 {
				m.env = m.captured[top]
				m.captured[top] = nil // zero for GC
				m.captured = m.captured[:top]
			}
			continue
		case Slice:
			low := int(mem[sp-2].num)  //nolint:gosec
			high := int(mem[sp-1].num) //nolint:gosec
			mem[sp-3] = Value{ref: mem[sp-3].ref.Slice(low, high)}
			mem = mem[:sp-2]
		case Slice3:
			low := int(mem[sp-3].num)  //nolint:gosec
			high := int(mem[sp-2].num) //nolint:gosec
			hi := int(mem[sp-1].num)   //nolint:gosec
			mem[sp-4] = Value{ref: mem[sp-4].ref.Slice3(low, high, hi)}
			mem = mem[:sp-3]
		case Stop:
			mem[sp-1].ref.Interface().(func())()
			mem = mem[:sp-4]
		case Sub:
			if isFloat(mem[sp-2].ref.Kind()) {
				mem[sp-2].num = math.Float64bits(math.Float64frombits(mem[sp-2].num) - math.Float64frombits(mem[sp-1].num))
			} else {
				mem[sp-2].num = uint64(int64(mem[sp-2].num) - int64(mem[sp-1].num)) //nolint:gosec
			}
			resetNumRef(&mem[sp-2])
			mem = mem[:sp-1]

		// Generic bitwise.
		case BitAnd:
			mem[sp-2].num &= mem[sp-1].num
			resetNumRef(&mem[sp-2])
			mem = mem[:sp-1]
		case BitOr:
			mem[sp-2].num |= mem[sp-1].num
			resetNumRef(&mem[sp-2])
			mem = mem[:sp-1]
		case BitXor:
			mem[sp-2].num ^= mem[sp-1].num
			resetNumRef(&mem[sp-2])
			mem = mem[:sp-1]
		case BitAndNot:
			mem[sp-2].num &^= mem[sp-1].num
			resetNumRef(&mem[sp-2])
			mem = mem[:sp-1]
		case BitShl:
			mem[sp-2].num <<= mem[sp-1].num
			resetNumRef(&mem[sp-2])
			mem = mem[:sp-1]
		case BitShr:
			k := mem[sp-2].ref.Kind()
			if k >= reflect.Uint && k <= reflect.Uintptr {
				mem[sp-2].num >>= mem[sp-1].num
			} else {
				mem[sp-2].num = uint64(int64(mem[sp-2].num) >> mem[sp-1].num) //nolint:gosec
			}
			resetNumRef(&mem[sp-2])
			mem = mem[:sp-1]
		case BitComp:
			mem[sp-1].num = ^mem[sp-1].num
			resetNumRef(&mem[sp-1])

		case Swap:
			a, b := sp-c.Arg[0]-1, sp-c.Arg[1]-1
			mem[a], mem[b] = mem[b], mem[a]
		case HAlloc:
			cell := new(Value)
			*cell = mem[sp-1]         // initialise cell with top-of-stack value
			mem[sp-1] = ValueOf(cell) // replace value with cell pointer
		case HGet:
			mem = append(mem, *m.env[c.Arg[0]])
		case HSet:
			*m.env[c.Arg[0]] = mem[sp-1]
			mem = mem[:sp-1]
		case HPtr:
			mem = append(mem, ValueOf(m.env[c.Arg[0]]))
		case MkClosure:
			n := c.Arg[0]
			codeAddr := int(mem[sp-n-1].num) //nolint:gosec
			env := make([]*Value, n)
			for i := range n {
				env[i] = mem[sp-n+i].ref.Interface().(*Value)
			}
			clo := ValueOf(Closure{Code: codeAddr, Env: env})
			for i := sp - n - 1; i < sp; i++ {
				mem[i] = Value{} // zero code addr + cell ptr slots
			}
			mem = mem[:sp-n-1]
			mem = append(mem, clo)
		case Index:
			idx := int(mem[sp-1].num) //nolint:gosec
			mem[sp-2] = fromReflect(mem[sp-2].ref.Index(idx))
			mem = mem[:sp-1]
		case IndexSet:
			idx := int(mem[sp-2].num) //nolint:gosec
			mem[sp-3].ref.Index(idx).Set(mem[sp-1].Reflect())
			mem = mem[:sp-2]
		case MapIndex:
			rv := mem[sp-2].ref.MapIndex(mem[sp-1].Reflect())
			mem[sp-2] = fromReflect(rv)
			mem = mem[:sp-1]
		case MapSet:
			mem[sp-3].ref.SetMapIndex(mem[sp-2].Reflect(), mem[sp-1].Reflect())
			mem = mem[:sp-2]
		case SetS:
			n := c.Arg[0]
			for i := 0; i < n; i++ {
				assignSlot(&mem[sp-n-i-1], mem[sp-n+i])
			}
			mem = mem[:sp-n-1]

		// Per-type Add.
		case AddInt, AddInt64:
			mem[sp-2].num = uint64(int64(mem[sp-2].num) + int64(mem[sp-1].num)) //nolint:gosec
			mem[sp-2].ref = numZero[c.Op-AddInt]
			mem = mem[:sp-1]
		case AddInt8:
			mem[sp-2].num = uint64(int8(mem[sp-2].num) + int8(mem[sp-1].num)) //nolint:gosec
			mem[sp-2].ref = numZero[c.Op-AddInt]
			mem = mem[:sp-1]
		case AddInt16:
			mem[sp-2].num = uint64(int16(mem[sp-2].num) + int16(mem[sp-1].num)) //nolint:gosec
			mem[sp-2].ref = numZero[c.Op-AddInt]
			mem = mem[:sp-1]
		case AddInt32:
			mem[sp-2].num = uint64(int32(mem[sp-2].num) + int32(mem[sp-1].num)) //nolint:gosec
			mem[sp-2].ref = numZero[c.Op-AddInt]
			mem = mem[:sp-1]
		case AddUint, AddUint64:
			mem[sp-2].num += mem[sp-1].num
			mem[sp-2].ref = numZero[c.Op-AddInt]
			mem = mem[:sp-1]
		case AddUint8:
			mem[sp-2].num = uint64(uint8(mem[sp-2].num) + uint8(mem[sp-1].num)) //nolint:gosec
			mem[sp-2].ref = numZero[c.Op-AddInt]
			mem = mem[:sp-1]
		case AddUint16:
			mem[sp-2].num = uint64(uint16(mem[sp-2].num) + uint16(mem[sp-1].num)) //nolint:gosec
			mem[sp-2].ref = numZero[c.Op-AddInt]
			mem = mem[:sp-1]
		case AddUint32:
			mem[sp-2].num = uint64(uint32(mem[sp-2].num) + uint32(mem[sp-1].num)) //nolint:gosec
			mem[sp-2].ref = numZero[c.Op-AddInt]
			mem = mem[:sp-1]
		case AddFloat64:
			mem[sp-2].num = math.Float64bits(math.Float64frombits(mem[sp-2].num) + math.Float64frombits(mem[sp-1].num))
			mem[sp-2].ref = numZero[c.Op-AddInt]
			mem = mem[:sp-1]
		case AddFloat32:
			mem[sp-2].num = math.Float64bits(float64(float32(math.Float64frombits(mem[sp-2].num)) + float32(math.Float64frombits(mem[sp-1].num))))
			mem[sp-2].ref = numZero[c.Op-AddInt]
			mem = mem[:sp-1]

		// Per-type Sub.
		case SubInt, SubInt64:
			mem[sp-2].num = uint64(int64(mem[sp-2].num) - int64(mem[sp-1].num)) //nolint:gosec
			mem[sp-2].ref = numZero[c.Op-SubInt]
			mem = mem[:sp-1]
		case SubInt8:
			mem[sp-2].num = uint64(int8(mem[sp-2].num) - int8(mem[sp-1].num)) //nolint:gosec
			mem[sp-2].ref = numZero[c.Op-SubInt]
			mem = mem[:sp-1]
		case SubInt16:
			mem[sp-2].num = uint64(int16(mem[sp-2].num) - int16(mem[sp-1].num)) //nolint:gosec
			mem[sp-2].ref = numZero[c.Op-SubInt]
			mem = mem[:sp-1]
		case SubInt32:
			mem[sp-2].num = uint64(int32(mem[sp-2].num) - int32(mem[sp-1].num)) //nolint:gosec
			mem[sp-2].ref = numZero[c.Op-SubInt]
			mem = mem[:sp-1]
		case SubUint, SubUint64:
			mem[sp-2].num -= mem[sp-1].num
			mem[sp-2].ref = numZero[c.Op-SubInt]
			mem = mem[:sp-1]
		case SubUint8:
			mem[sp-2].num = uint64(uint8(mem[sp-2].num) - uint8(mem[sp-1].num)) //nolint:gosec
			mem[sp-2].ref = numZero[c.Op-SubInt]
			mem = mem[:sp-1]
		case SubUint16:
			mem[sp-2].num = uint64(uint16(mem[sp-2].num) - uint16(mem[sp-1].num)) //nolint:gosec
			mem[sp-2].ref = numZero[c.Op-SubInt]
			mem = mem[:sp-1]
		case SubUint32:
			mem[sp-2].num = uint64(uint32(mem[sp-2].num) - uint32(mem[sp-1].num)) //nolint:gosec
			mem[sp-2].ref = numZero[c.Op-SubInt]
			mem = mem[:sp-1]
		case SubFloat64:
			mem[sp-2].num = math.Float64bits(math.Float64frombits(mem[sp-2].num) - math.Float64frombits(mem[sp-1].num))
			mem[sp-2].ref = numZero[c.Op-SubInt]
			mem = mem[:sp-1]
		case SubFloat32:
			mem[sp-2].num = math.Float64bits(float64(float32(math.Float64frombits(mem[sp-2].num)) - float32(math.Float64frombits(mem[sp-1].num))))
			mem[sp-2].ref = numZero[c.Op-SubInt]
			mem = mem[:sp-1]

		// Per-type Mul.
		case MulInt, MulInt64:
			mem[sp-2].num = uint64(int64(mem[sp-2].num) * int64(mem[sp-1].num)) //nolint:gosec
			mem[sp-2].ref = numZero[c.Op-MulInt]
			mem = mem[:sp-1]
		case MulInt8:
			mem[sp-2].num = uint64(int8(mem[sp-2].num) * int8(mem[sp-1].num)) //nolint:gosec
			mem[sp-2].ref = numZero[c.Op-MulInt]
			mem = mem[:sp-1]
		case MulInt16:
			mem[sp-2].num = uint64(int16(mem[sp-2].num) * int16(mem[sp-1].num)) //nolint:gosec
			mem[sp-2].ref = numZero[c.Op-MulInt]
			mem = mem[:sp-1]
		case MulInt32:
			mem[sp-2].num = uint64(int32(mem[sp-2].num) * int32(mem[sp-1].num)) //nolint:gosec
			mem[sp-2].ref = numZero[c.Op-MulInt]
			mem = mem[:sp-1]
		case MulUint, MulUint64:
			mem[sp-2].num *= mem[sp-1].num
			mem[sp-2].ref = numZero[c.Op-MulInt]
			mem = mem[:sp-1]
		case MulUint8:
			mem[sp-2].num = uint64(uint8(mem[sp-2].num) * uint8(mem[sp-1].num)) //nolint:gosec
			mem[sp-2].ref = numZero[c.Op-MulInt]
			mem = mem[:sp-1]
		case MulUint16:
			mem[sp-2].num = uint64(uint16(mem[sp-2].num) * uint16(mem[sp-1].num)) //nolint:gosec
			mem[sp-2].ref = numZero[c.Op-MulInt]
			mem = mem[:sp-1]
		case MulUint32:
			mem[sp-2].num = uint64(uint32(mem[sp-2].num) * uint32(mem[sp-1].num)) //nolint:gosec
			mem[sp-2].ref = numZero[c.Op-MulInt]
			mem = mem[:sp-1]
		case MulFloat64:
			mem[sp-2].num = math.Float64bits(math.Float64frombits(mem[sp-2].num) * math.Float64frombits(mem[sp-1].num))
			mem[sp-2].ref = numZero[c.Op-MulInt]
			mem = mem[:sp-1]
		case MulFloat32:
			mem[sp-2].num = math.Float64bits(float64(float32(math.Float64frombits(mem[sp-2].num)) * float32(math.Float64frombits(mem[sp-1].num))))
			mem[sp-2].ref = numZero[c.Op-MulInt]
			mem = mem[:sp-1]

		// Per-type Negate.
		case NegateInt, NegateInt64:
			mem[sp-1].num = uint64(-int64(mem[sp-1].num)) //nolint:gosec
			mem[sp-1].ref = numZero[c.Op-NegateInt]
		case NegateInt8:
			mem[sp-1].num = uint64(-int8(mem[sp-1].num)) //nolint:gosec
			mem[sp-1].ref = numZero[c.Op-NegateInt]
		case NegateInt16:
			mem[sp-1].num = uint64(-int16(mem[sp-1].num)) //nolint:gosec
			mem[sp-1].ref = numZero[c.Op-NegateInt]
		case NegateInt32:
			mem[sp-1].num = uint64(-int32(mem[sp-1].num)) //nolint:gosec
			mem[sp-1].ref = numZero[c.Op-NegateInt]
		case NegateUint, NegateUint64:
			mem[sp-1].num = -mem[sp-1].num
			mem[sp-1].ref = numZero[c.Op-NegateInt]
		case NegateUint8:
			mem[sp-1].num = uint64(-uint8(mem[sp-1].num)) //nolint:gosec
			mem[sp-1].ref = numZero[c.Op-NegateInt]
		case NegateUint16:
			mem[sp-1].num = uint64(-uint16(mem[sp-1].num)) //nolint:gosec
			mem[sp-1].ref = numZero[c.Op-NegateInt]
		case NegateUint32:
			mem[sp-1].num = uint64(-uint32(mem[sp-1].num)) //nolint:gosec
			mem[sp-1].ref = numZero[c.Op-NegateInt]
		case NegateFloat64:
			mem[sp-1].num = math.Float64bits(-math.Float64frombits(mem[sp-1].num))
			mem[sp-1].ref = numZero[c.Op-NegateInt]
		case NegateFloat32:
			mem[sp-1].num = math.Float64bits(-float64(float32(math.Float64frombits(mem[sp-1].num))))
			mem[sp-1].ref = numZero[c.Op-NegateInt]

		// Per-type Greater.
		case GreaterInt, GreaterInt8, GreaterInt16, GreaterInt32, GreaterInt64:
			mem[sp-2] = ValueOf(int64(mem[sp-2].num) > int64(mem[sp-1].num)) //nolint:gosec
			mem = mem[:sp-1]
		case GreaterUint, GreaterUint8, GreaterUint16, GreaterUint32, GreaterUint64:
			mem[sp-2] = ValueOf(mem[sp-2].num > mem[sp-1].num)
			mem = mem[:sp-1]
		case GreaterFloat32, GreaterFloat64:
			mem[sp-2] = ValueOf(math.Float64frombits(mem[sp-2].num) > math.Float64frombits(mem[sp-1].num))
			mem = mem[:sp-1]

		// Per-type Lower.
		case LowerInt, LowerInt8, LowerInt16, LowerInt32, LowerInt64:
			mem[sp-2] = ValueOf(int64(mem[sp-2].num) < int64(mem[sp-1].num)) //nolint:gosec
			mem = mem[:sp-1]
		case LowerUint, LowerUint8, LowerUint16, LowerUint32, LowerUint64:
			mem[sp-2] = ValueOf(mem[sp-2].num < mem[sp-1].num)
			mem = mem[:sp-1]
		case LowerFloat32, LowerFloat64:
			mem[sp-2] = ValueOf(math.Float64frombits(mem[sp-2].num) < math.Float64frombits(mem[sp-1].num))
			mem = mem[:sp-1]

		// Per-type Div.
		case DivInt, DivInt64:
			mem[sp-2].num = uint64(int64(mem[sp-2].num) / int64(mem[sp-1].num)) //nolint:gosec
			mem[sp-2].ref = numZero[c.Op-DivInt]
			mem = mem[:sp-1]
		case DivInt8:
			mem[sp-2].num = uint64(int8(mem[sp-2].num) / int8(mem[sp-1].num)) //nolint:gosec
			mem[sp-2].ref = numZero[c.Op-DivInt]
			mem = mem[:sp-1]
		case DivInt16:
			mem[sp-2].num = uint64(int16(mem[sp-2].num) / int16(mem[sp-1].num)) //nolint:gosec
			mem[sp-2].ref = numZero[c.Op-DivInt]
			mem = mem[:sp-1]
		case DivInt32:
			mem[sp-2].num = uint64(int32(mem[sp-2].num) / int32(mem[sp-1].num)) //nolint:gosec
			mem[sp-2].ref = numZero[c.Op-DivInt]
			mem = mem[:sp-1]
		case DivUint, DivUint64:
			mem[sp-2].num /= mem[sp-1].num
			mem[sp-2].ref = numZero[c.Op-DivInt]
			mem = mem[:sp-1]
		case DivUint8:
			mem[sp-2].num = uint64(uint8(mem[sp-2].num) / uint8(mem[sp-1].num)) //nolint:gosec
			mem[sp-2].ref = numZero[c.Op-DivInt]
			mem = mem[:sp-1]
		case DivUint16:
			mem[sp-2].num = uint64(uint16(mem[sp-2].num) / uint16(mem[sp-1].num)) //nolint:gosec
			mem[sp-2].ref = numZero[c.Op-DivInt]
			mem = mem[:sp-1]
		case DivUint32:
			mem[sp-2].num = uint64(uint32(mem[sp-2].num) / uint32(mem[sp-1].num)) //nolint:gosec
			mem[sp-2].ref = numZero[c.Op-DivInt]
			mem = mem[:sp-1]
		case DivFloat64:
			mem[sp-2].num = math.Float64bits(math.Float64frombits(mem[sp-2].num) / math.Float64frombits(mem[sp-1].num))
			mem[sp-2].ref = numZero[c.Op-DivInt]
			mem = mem[:sp-1]
		case DivFloat32:
			mem[sp-2].num = math.Float64bits(float64(float32(math.Float64frombits(mem[sp-2].num)) / float32(math.Float64frombits(mem[sp-1].num))))
			mem[sp-2].ref = numZero[c.Op-DivInt]
			mem = mem[:sp-1]

		// Per-type Rem (integer only).
		case RemInt, RemInt64:
			mem[sp-2].num = uint64(int64(mem[sp-2].num) % int64(mem[sp-1].num)) //nolint:gosec
			mem[sp-2].ref = numZero[c.Op-RemInt]
			mem = mem[:sp-1]
		case RemInt8:
			mem[sp-2].num = uint64(int8(mem[sp-2].num) % int8(mem[sp-1].num)) //nolint:gosec
			mem[sp-2].ref = numZero[c.Op-RemInt]
			mem = mem[:sp-1]
		case RemInt16:
			mem[sp-2].num = uint64(int16(mem[sp-2].num) % int16(mem[sp-1].num)) //nolint:gosec
			mem[sp-2].ref = numZero[c.Op-RemInt]
			mem = mem[:sp-1]
		case RemInt32:
			mem[sp-2].num = uint64(int32(mem[sp-2].num) % int32(mem[sp-1].num)) //nolint:gosec
			mem[sp-2].ref = numZero[c.Op-RemInt]
			mem = mem[:sp-1]
		case RemUint, RemUint64:
			mem[sp-2].num %= mem[sp-1].num
			mem[sp-2].ref = numZero[c.Op-RemInt]
			mem = mem[:sp-1]
		case RemUint8:
			mem[sp-2].num = uint64(uint8(mem[sp-2].num) % uint8(mem[sp-1].num)) //nolint:gosec
			mem[sp-2].ref = numZero[c.Op-RemInt]
			mem = mem[:sp-1]
		case RemUint16:
			mem[sp-2].num = uint64(uint16(mem[sp-2].num) % uint16(mem[sp-1].num)) //nolint:gosec
			mem[sp-2].ref = numZero[c.Op-RemInt]
			mem = mem[:sp-1]
		case RemUint32:
			mem[sp-2].num = uint64(uint32(mem[sp-2].num) % uint32(mem[sp-1].num)) //nolint:gosec
			mem[sp-2].ref = numZero[c.Op-RemInt]
			mem = mem[:sp-1]
		}
		ip++
	}
}

// PushCode adds instructions to the machine code.
func (m *Machine) PushCode(code ...Instruction) (p int) {
	p = len(m.code)
	m.code = append(m.code, code...)
	return p
}

// SetIP sets the value of machine instruction pointer to given index.
func (m *Machine) SetIP(ip int) { m.ip = ip }

// Push pushes data values on top of machine memory stack.
func (m *Machine) Push(v ...Value) (l int) {
	l = len(m.mem)
	m.mem = append(m.mem, v...)
	return l
}

// Top returns (but not remove)  the value on the top of machine stack.
func (m *Machine) Top() (v Value) {
	if l := len(m.mem); l > 0 {
		v = m.mem[l-1]
	}
	return v
}

// PopExit removes the last machine code instruction if is Exit.
func (m *Machine) PopExit() {
	if l := len(m.code); l > 0 && m.code[l-1].Op == Exit {
		m.code = m.code[:l-1]
	}
}

// Vstring returns the string representation of a list of values.
func Vstring(lv []Value) string {
	var sb strings.Builder
	sb.WriteByte('[')
	for i, v := range lv {
		if i > 0 {
			sb.WriteByte(' ')
		}
		fmt.Fprintf(&sb, "%v", v.Interface())
	}
	sb.WriteByte(']')
	return sb.String()
}

// forceSettable returns fv as-is if settable, or makes it settable via unsafe.
// Use it only on unexported struct fields.
func forceSettable(fv reflect.Value) reflect.Value {
	if !fv.CanSet() {
		fv = reflect.NewAt(fv.Type(), unsafe.Pointer(fv.UnsafeAddr())).Elem()
	}
	return fv
}

// assignSlot writes src into the memory slot dst, updating both num and ref
// for numeric types to maintain the dual-storage invariant.
func assignSlot(dst *Value, src Value) {
	if isNum(src.ref.Kind()) {
		dst.num = src.num
		if dst.ref.CanSet() {
			if isNum(dst.ref.Kind()) {
				setNumReflect(dst.ref, src.num)
			} else {
				dst.ref.Set(src.Reflect())
			}
		}
	} else {
		dst.ref.Set(src.ref)
	}
}

// setNumReflect writes the raw bits from num into a settable numeric reflect.Value,
// handling cross-type assignment (e.g. int literal into uint16 slot).
func setNumReflect(rv reflect.Value, num uint64) {
	switch rv.Kind() {
	case reflect.Bool:
		rv.SetBool(num != 0)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		rv.SetInt(int64(num)) //nolint:gosec
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		rv.SetUint(num)
	case reflect.Float32, reflect.Float64:
		rv.SetFloat(math.Float64frombits(num))
	}
}
