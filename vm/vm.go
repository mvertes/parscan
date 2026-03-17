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
	Neg                    // -- ; - mem[fp]
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
	TypeBranch             // iface -- ; pop iface; if iface doesn't hold type at mem[$2] (or $2==-1 for nil), ip += $1
	Panic                  // v -- ; pop value, start stack unwinding
	Recover                // -- v ; push recovered value (or nil if not panicking in a deferred call)
	DeferPush              // func [a0..an-1] -- func [a0..an-1] [packed prevHead retIP] ; register deferred call on stack; $0=narg, $1=1 if native
	DeferRet               // -- ; sentinel: restore outer frame after a deferred call returns

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

	NegInt // n -- -n
	NegInt8
	NegInt16
	NegInt32
	NegInt64
	NegUint
	NegUint8
	NegUint16
	NegUint32
	NegUint64
	NegFloat32
	NegFloat64

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

	// Immediate operand variants: fold Push+BinOp into one instruction.
	// Arg[0] holds the right-hand constant (int, sign-extended to int64).
	// Covers int and int64 (signed) or uint and uint64 (unsigned) only.
	AddIntImm      // n -- n+$1
	SubIntImm      // n -- n-$1
	MulIntImm      // n -- n*$1
	GreaterIntImm  // n -- n>$1  (signed)
	GreaterUintImm // n -- n>$1 (unsigned)
	LowerIntImm    // n -- n<$1  (signed)
	LowerUintImm   // n -- n<$1  (unsigned)
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

	panicking bool  // true while unwinding due to panic
	panicVal  Value // value passed to panic()
	frameInfo []int // per call frame: nret | (numIn << 16), parallel to captured
}

// deferSentinelIP is the ip value used as return address for deferred call frames.
// A negative ip is checked before m.code[ip] to dispatch the DeferRet handler.
const deferSentinelIP = -1

// panicUnwindIP is the ip sentinel used during panic stack unwinding.
// The main loop dispatches deferred calls and tears down frames when ip == panicUnwindIP.
const panicUnwindIP = -2

// Run runs a program.
func (m *Machine) Run() (err error) {
	mem, ip, fp, sp, ic := m.mem, m.ip, m.fp, 0, m.ic

	defer func() {
		m.mem, m.ip, m.fp, m.ic = mem, ip, fp, ic
	}()

	for {
		sp = len(mem) // stack pointer
		// Negative ip is a sentinel for special handlers.
		if ip < 0 {
			ic++
			if ip == panicUnwindIP {
				// Panic unwind: dispatch deferred calls in current frame, then tear down.
				dh := int(mem[fp-3].num) //nolint:gosec
				if dh != 0 {
					packed := mem[dh-2].num
					narg := int(packed >> 1) //nolint:gosec
					isX := packed&1 == 1
					prevHead := int(mem[dh-1].num) //nolint:gosec
					funcVal := mem[dh-narg-3]
					if isX {
						// Native defer: call via reflect, discard results.
						rin := make([]reflect.Value, narg)
						for i := range rin {
							rin[i] = mem[dh-narg-2+i].Reflect()
						}
						funcVal.ref.Call(rin)
						retBase := dh - narg - 3
						for i := retBase; i < sp; i++ {
							mem[i] = Value{}
						}
						mem = mem[:retBase]
						mem[fp-3].num = uint64(prevHead) //nolint:gosec
						continue
					}
					// VM defer: store panicUnwindIP as return address, push frame.
					top := len(m.frameInfo) - 1
					nret := m.frameInfo[top] & 0xFFFF
					pip := int32(panicUnwindIP)
					mem[dh].num = uint64(*(*uint32)(unsafe.Pointer(&pip))) | uint64(nret)<<32 //nolint:gosec
					prevEnv := m.env
					var nip int
					if isNum(funcVal.ref.Kind()) {
						nip = int(funcVal.num) //nolint:gosec
						m.env = nil
					} else if clo, ok := funcVal.ref.Interface().(Closure); ok {
						nip = clo.Code
						m.env = clo.Env
					} else if iv, ok := funcVal.ref.Interface().(int); ok {
						nip = iv
						m.env = nil
					} else {
						nip = int(funcVal.num) //nolint:gosec
						m.env = nil
					}
					base := len(mem)
					mem = append(mem, funcVal)
					mem = append(mem, mem[dh-narg-2:dh-2]...)
					mem = append(mem, Value{}, Value{num: ^uint64(0)}, Value{num: uint64(fp)}) //nolint:gosec
					m.captured = append(m.captured, prevEnv)
					m.frameInfo = append(m.frameInfo, 0)
					fp = base + 1 + narg + 3
					ip = nip
					continue
				}
				// No more defers in this frame.
				if !m.panicking {
					// Recovered: tear down frame, return zero values to caller.
					top := len(m.frameInfo) - 1
					info := m.frameInfo[top]
					nret := info & 0xFFFF
					numIn := info >> 16
					m.frameInfo = m.frameInfo[:top]
					ip = int(mem[fp-2].num) //nolint:gosec
					ofp := fp
					fp = int(mem[fp-1].num) //nolint:gosec
					newBase := ofp - nret - numIn - 3
					for i := 0; i < nret; i++ {
						mem[newBase+i] = Value{}
					}
					newSP := newBase + nret
					for i := newSP; i < len(mem); i++ {
						mem[i] = Value{}
					}
					mem = mem[:newSP]
					if top := len(m.captured) - 1; top >= 0 {
						m.env = m.captured[top]
						m.captured[top] = nil
						m.captured = m.captured[:top]
					}
					continue
				}
				// Still panicking: tear down frame, continue unwinding parent.
				top := len(m.frameInfo) - 1
				info := m.frameInfo[top]
				numIn := info >> 16
				m.frameInfo = m.frameInfo[:top]
				ofp := fp
				fp = int(mem[fp-1].num) //nolint:gosec
				if fp == 0 {
					// Top of stack: return panic as error.
					m.mem, m.ip, m.fp, m.ic = mem, 0, 0, ic
					return fmt.Errorf("panic: %v", m.panicVal.Interface())
				}
				newBase := ofp - numIn - 3 - 1 // below func slot
				for i := newBase; i < len(mem); i++ {
					mem[i] = Value{}
				}
				mem = mem[:newBase]
				if top := len(m.captured) - 1; top >= 0 {
					m.env = m.captured[top]
					m.captured[top] = nil
					m.captured = m.captured[:top]
				}
				continue
			}
			// ip == deferSentinelIP: restore outer frame after a deferred VM call returns.
			dh := int(mem[fp-3].num)        //nolint:gosec
			narg := int(mem[dh-2].num >> 1) //nolint:gosec
			val := mem[dh].num
			returnIP := int(int32(val & 0xFFFFFFFF)) //nolint:gosec
			nret := int(val >> 32)                   //nolint:gosec
			prevHead := int(mem[dh-1].num)           //nolint:gosec
			retBase := dh - narg - 3
			for i := 0; i < nret; i++ { // move return values down
				mem[retBase+i] = mem[dh+1+i]
			}
			for i := retBase + nret; i < sp; i++ { // zero stale slots
				mem[i] = Value{}
			}
			mem = mem[:retBase+nret]
			mem[fp-3].num = uint64(prevHead) //nolint:gosec
			ip = returnIP
			continue
		}
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
			narg := c.Arg[0]
			fval := mem[sp-1-narg]
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
			nret := c.Arg[1]
			m.captured = append(m.captured, prevEnv)
			m.frameInfo = append(m.frameInfo, nret|narg<<16)
			mem = append(mem, Value{}, Value{num: uint64(ip + 1)}, Value{num: uint64(fp)}) //nolint:gosec // deferHead, retIP, prevFP
			ip = nip
			fp = sp + 3
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
			mem[sp-2] = boolVal(mem[sp-2].Equal(mem[sp-1]))
			mem = mem[:sp-1]
		case EqualSet:
			if mem[sp-2].Equal(mem[sp-1]) {
				// If equal then lhs and rhs are popped, replaced by test result, as in Equal.
				mem[sp-2] = boolVal(true)
				mem = mem[:sp-1]
			} else {
				// If not equal then the lhs is let on stack for further processing.
				// This is used to simplify bytecode in case clauses of switch statments.
				mem[sp-1] = boolVal(false)
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
				mem[sp-1] = boolVal(false)
				mem = append(mem, NewValue(dstTyp.Rtype))
				break
			}
			if concrete := ifc.IfaceVal(); concrete.Typ.Rtype == dstTyp.Rtype && concrete.Typ.Name == dstTyp.Name {
				if okForm {
					mem[sp-1] = boolVal(true)
					mem = append(mem, concrete.Val)
				} else {
					mem[sp-1] = concrete.Val
				}
			} else {
				if !okForm {
					// FIXME: replace with a vm panic operator when ready.
					panic(fmt.Sprintf("interface conversion: interface value is %s, not %s", concrete.Typ, dstTyp))
				}
				mem[sp-1] = boolVal(false)
				mem = append(mem, NewValue(dstTyp.Rtype))
			}

		case TypeBranch: // Arg[0]=offset, Arg[1]=typeIdx (-1 for nil case)
			ifc := mem[sp-1]
			mem = mem[:sp-1]
			var matched bool
			if c.Arg[1] == -1 {
				matched = !ifc.IsIface()
			} else if ifc.IsIface() {
				matched = ifc.IfaceVal().Typ == mem[c.Arg[1]].ref.Interface().(*Type)
			}
			if !matched {
				ip += c.Arg[0]
				continue
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
				mem[sp-2] = boolVal(math.Float64frombits(mem[sp-2].num) > math.Float64frombits(mem[sp-1].num))
			} else {
				mem[sp-2] = boolVal(int64(mem[sp-2].num) > int64(mem[sp-1].num)) //nolint:gosec
			}
			mem = mem[:sp-1]
		case Lower:
			if isFloat(mem[sp-2].ref.Kind()) {
				mem[sp-2] = boolVal(math.Float64frombits(mem[sp-2].num) < math.Float64frombits(mem[sp-1].num))
			} else {
				mem[sp-2] = boolVal(int64(mem[sp-2].num) < int64(mem[sp-1].num)) //nolint:gosec
			}
			mem = mem[:sp-1]
		case Len:
			mem = append(mem, ValueOf(mem[sp-1-c.Arg[0]].ref.Len()))
		case Neg:
			if isFloat(mem[sp-1].ref.Kind()) {
				mem[sp-1].num = math.Float64bits(-math.Float64frombits(mem[sp-1].num))
			} else {
				mem[sp-1].num = uint64(-int64(mem[sp-1].num)) //nolint:gosec
			}
			resetNumRef(&mem[sp-1])
		case Next:
			if k, ok := mem[sp-2].ref.Interface().(func() (reflect.Value, bool))(); ok {
				addr := c.Arg[2]
				if c.Arg[1] == Local {
					addr += fp - 1
				}
				mem[addr].Set(k)
			} else {
				ip += c.Arg[0]
				continue
			}
		case Next2:
			if k, v, ok := mem[sp-2].ref.Interface().(func() (reflect.Value, reflect.Value, bool))(); ok {
				base := 0
				if c.Arg[1] == Local {
					base = fp - 1
				}
				mem[base+c.Arg[2]].Set(k)
				mem[base+c.Arg[3]].Set(v)
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
		case DeferPush:
			// Snapshot args in-place (detach addressable refs to prevent aliasing).
			narg := c.Arg[0]
			isX := c.Arg[1]
			for i := sp - narg; i < sp; i++ {
				if isNum(mem[i].ref.Kind()) && mem[i].ref.CanAddr() {
					mem[i].ref = reflect.Zero(mem[i].ref.Type())
				}
			}
			// Push 3-slot header: packed(narg/isX), prevHead link, returnIP placeholder.
			prevHead := int(mem[fp-3].num) //nolint:gosec
			mem = append(mem,
				Value{num: uint64(narg<<1 | isX)}, //nolint:gosec
				Value{num: uint64(prevHead)},      //nolint:gosec
				Value{},                           // returnIP placeholder, filled by Return
			)
			mem[fp-3].num = uint64(sp + 2) //nolint:gosec // dh = index of returnIP slot

		case Panic:
			m.panicking = true
			m.panicVal = mem[sp-1]
			mem = mem[:sp-1] // pop the panic argument
			ip = panicUnwindIP
			continue

		case Recover:
			if m.panicking && int(mem[fp-2].num) == deferSentinelIP { //nolint:gosec
				m.panicking = false
				pv := m.panicVal
				// Wrap in Iface so type assertions on the recovered value work.
				if pv.IsValid() && !pv.IsIface() {
					typ := &Type{Rtype: pv.Reflect().Type()}
					pv = Value{ref: reflect.ValueOf(Iface{Typ: typ, Val: pv})}
				}
				mem = append(mem, pv)
				m.panicVal = Value{}
			} else {
				mem = append(mem, Value{}) // nil
			}

		case Return:
			// If there are pending defers in this frame, dispatch the top one (LIFO).
			dh := int(mem[fp-3].num) //nolint:gosec
			if dh != 0 {
				packed := mem[dh-2].num
				narg := int(packed >> 1) //nolint:gosec
				isX := packed&1 == 1
				prevHead := int(mem[dh-1].num) //nolint:gosec
				funcVal := mem[dh-narg-3]
				nret := c.Arg[0]
				if isX {
					// Native function: call via reflect, discard results.
					rin := make([]reflect.Value, narg)
					for i := range rin {
						rin[i] = mem[dh-narg-2+i].Reflect()
					}
					funcVal.ref.Call(rin)
					// Move return values (at dh+1..dh+nret) down over the defer entry.
					retBase := dh - narg - 3
					for i := 0; i < nret; i++ {
						mem[retBase+i] = mem[dh+1+i]
					}
					for i := retBase + nret; i < sp; i++ {
						mem[i] = Value{}
					}
					mem = mem[:retBase+nret]
					mem[fp-3].num = uint64(prevHead) //nolint:gosec
					continue                         // re-check for more defers
				}
				// VM function: pack ip and nret into the returnIP slot, then call.
				mem[dh].num = uint64(ip) | uint64(nret)<<32 //nolint:gosec
				prevEnv := m.env
				var nip int
				if isNum(funcVal.ref.Kind()) {
					nip = int(funcVal.num) //nolint:gosec
					m.env = nil
				} else if clo, ok := funcVal.ref.Interface().(Closure); ok {
					nip = clo.Code
					m.env = clo.Env
				} else if iv, ok := funcVal.ref.Interface().(int); ok {
					nip = iv
					m.env = nil
				} else {
					nip = int(funcVal.num) //nolint:gosec
					m.env = nil
				}
				// Push func+args copy and 3-slot call frame (retIP, prevFP, deferHead=0).
				base := len(mem)
				mem = append(mem, funcVal)
				mem = append(mem, mem[dh-narg-2:dh-2]...)
				mem = append(mem, Value{}, Value{num: ^uint64(0)}, Value{num: uint64(fp)}) //nolint:gosec
				m.captured = append(m.captured, prevEnv)
				m.frameInfo = append(m.frameInfo, 0)
				fp = base + 1 + narg + 3
				ip = nip
				continue
			}
			// No pending defers: normal frame teardown.
			ip = int(mem[fp-2].num) //nolint:gosec
			ofp := fp
			fp = int(mem[fp-1].num) //nolint:gosec
			nret := c.Arg[0]
			newBase := ofp - nret - c.Arg[1] - 3
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
			if top := len(m.frameInfo) - 1; top >= 0 {
				m.frameInfo = m.frameInfo[:top]
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

		// Per-type Neg.
		case NegInt, NegInt64:
			mem[sp-1].num = uint64(-int64(mem[sp-1].num)) //nolint:gosec
			mem[sp-1].ref = numZero[c.Op-NegInt]
		case NegInt8:
			mem[sp-1].num = uint64(-int8(mem[sp-1].num)) //nolint:gosec
			mem[sp-1].ref = numZero[c.Op-NegInt]
		case NegInt16:
			mem[sp-1].num = uint64(-int16(mem[sp-1].num)) //nolint:gosec
			mem[sp-1].ref = numZero[c.Op-NegInt]
		case NegInt32:
			mem[sp-1].num = uint64(-int32(mem[sp-1].num)) //nolint:gosec
			mem[sp-1].ref = numZero[c.Op-NegInt]
		case NegUint, NegUint64:
			mem[sp-1].num = -mem[sp-1].num
			mem[sp-1].ref = numZero[c.Op-NegInt]
		case NegUint8:
			mem[sp-1].num = uint64(-uint8(mem[sp-1].num)) //nolint:gosec
			mem[sp-1].ref = numZero[c.Op-NegInt]
		case NegUint16:
			mem[sp-1].num = uint64(-uint16(mem[sp-1].num)) //nolint:gosec
			mem[sp-1].ref = numZero[c.Op-NegInt]
		case NegUint32:
			mem[sp-1].num = uint64(-uint32(mem[sp-1].num)) //nolint:gosec
			mem[sp-1].ref = numZero[c.Op-NegInt]
		case NegFloat64:
			mem[sp-1].num = math.Float64bits(-math.Float64frombits(mem[sp-1].num))
			mem[sp-1].ref = numZero[c.Op-NegInt]
		case NegFloat32:
			mem[sp-1].num = math.Float64bits(-float64(float32(math.Float64frombits(mem[sp-1].num))))
			mem[sp-1].ref = numZero[c.Op-NegInt]

		// Per-type Greater.
		case GreaterInt, GreaterInt8, GreaterInt16, GreaterInt32, GreaterInt64:
			mem[sp-2] = boolVal(int64(mem[sp-2].num) > int64(mem[sp-1].num)) //nolint:gosec
			mem = mem[:sp-1]
		case GreaterUint, GreaterUint8, GreaterUint16, GreaterUint32, GreaterUint64:
			mem[sp-2] = boolVal(mem[sp-2].num > mem[sp-1].num)
			mem = mem[:sp-1]
		case GreaterFloat32, GreaterFloat64:
			mem[sp-2] = boolVal(math.Float64frombits(mem[sp-2].num) > math.Float64frombits(mem[sp-1].num))
			mem = mem[:sp-1]

		// Per-type Lower.
		case LowerInt, LowerInt8, LowerInt16, LowerInt32, LowerInt64:
			mem[sp-2] = boolVal(int64(mem[sp-2].num) < int64(mem[sp-1].num)) //nolint:gosec
			mem = mem[:sp-1]
		case LowerUint, LowerUint8, LowerUint16, LowerUint32, LowerUint64:
			mem[sp-2] = boolVal(mem[sp-2].num < mem[sp-1].num)
			mem = mem[:sp-1]
		case LowerFloat32, LowerFloat64:
			mem[sp-2] = boolVal(math.Float64frombits(mem[sp-2].num) < math.Float64frombits(mem[sp-1].num))
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

		// Immediate operand ops: right-hand constant is in Arg[0].
		case AddIntImm:
			mem[sp-1].num = uint64(int64(mem[sp-1].num) + int64(c.Arg[0])) //nolint:gosec
			mem[sp-1].ref = numZero[0]
		case SubIntImm:
			mem[sp-1].num = uint64(int64(mem[sp-1].num) - int64(c.Arg[0])) //nolint:gosec
			mem[sp-1].ref = numZero[0]
		case MulIntImm:
			mem[sp-1].num = uint64(int64(mem[sp-1].num) * int64(c.Arg[0])) //nolint:gosec
			mem[sp-1].ref = numZero[0]
		case GreaterIntImm:
			mem[sp-1] = boolVal(int64(mem[sp-1].num) > int64(c.Arg[0])) //nolint:gosec
		case GreaterUintImm:
			mem[sp-1] = boolVal(mem[sp-1].num > uint64(c.Arg[0])) //nolint:gosec
		case LowerIntImm:
			mem[sp-1] = boolVal(int64(mem[sp-1].num) < int64(c.Arg[0])) //nolint:gosec
		case LowerUintImm:
			mem[sp-1] = boolVal(mem[sp-1].num < uint64(c.Arg[0])) //nolint:gosec
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
