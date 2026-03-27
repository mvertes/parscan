// Package vm implement a stack based virtual machine.
package vm

import (
	"fmt" // for tracing only
	"io"
	"iter"
	"log"  // for tracing only
	"math" // for float arithmetic
	"reflect"
	"strings"
	"unsafe" // to allow setting unexported struct fields //nolint:depguard
)

const debug = false

// Op is a VM opcode (bytecode instruction).
type Op int32

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
	Addr                   // a -- &a ;
	Call                   // f [a1 .. ai] -- [r1 .. rj] ; r1, ... = prog[f](a1, ...)
	Deref                  // x -- *x ;
	DerefSet               // ptr val -- ; *ptr = val
	Get                    // addr -- value ; value = mem[addr]
	Fnew                   // -- x; x = new mem[$1]
	FnewE                  // -- x; x = new mem[$1].Elem()
	Equal                  // n1 n2 -- cond ; cond = n1 == n2
	EqualSet               // n1 n2 -- n1 cond ; cond = n1 == n2
	Exit                   // -- ;
	Field                  // s -- f ; f = s.FieldIndex($1, ...)
	FieldSet               // s d -- s ; s.FieldIndex($1, ...) = d
	FieldFset              // s i v -- s; s.FieldIndex(i) = v
	Grow                   // -- ; sp += $1
	Index                  // a i -- a[i] ;
	IndexAddr              // a i -- &a[i] ; pointer to element
	IndexSet               // a i v -- a; a[i] = v
	Jump                   // -- ; ip += $1
	JumpTrue               // cond -- ; if cond { ip += $1 }
	JumpFalse              // cond -- ; if cond { ip += $1 }
	JumpSetTrue            //
	JumpSetFalse           //
	Len                    // -- x; x = mem[sp-$1]
	MapIndex               // a i -- a[i]
	MapIndexOk             // a i -- v ok ; v, ok = a[i]
	MapSet                 // a i v -- a; a[i] = v
	New                    // -- x; mem[fp+$1] = new mem[$2]
	Next                   // -- ; iterator next, set K
	Next0                  // -- ; iterator next, no variable
	Next2                  // -- ; iterator next, set K V
	Not                    // c -- r ; r = !c
	Pop                    // v --
	Push                   // -- v
	Pull                   // a -- a s n; pull iterator next and stop function
	Pull2                  // a -- a s n; pull iterator next and stop function
	Return                 // [r1 .. ri] -- ; exit frame, nret and callNarg from frames
	Set                    // v --  ; mem[$1,$2] = v
	SetS                   // dest val -- ; dest.Set(val)
	Slice                  // a l h -- a; a = a [l:h]
	Slice3                 // a l h m -- a; a = a[l:h:m]
	Stop                   // -- iterator stop
	Stop0                  // -- iterator stop, no variable
	Swap                   // --
	HAlloc                 // -- &cell ; cell = new(Value), push its pointer
	HGet                   // -- v    ; v = *State.Env[$1]
	HSet                   // v --    ; *State.Env[$1] = v
	HPtr                   // -- &cell ; push State.Env[$1] itself (transitive capture)
	MkClosure              // code [&c0..&cn-1] -- clo ; clo = Closure{code, env}
	Convert                // v -- v' ; v' = convert(v, type at mem[$1]); optional $2 = stack depth offset
	IfaceWrap              // v -- iface ; wrap v in Iface{type at $1, v}
	IfaceCall              // iface -- closure ; dynamic dispatch method $1 on iface
	TypeAssert             // iface -- v [ok] ; assert iface holds type at mem[$1]; $2=0 panics, $2=1 ok form
	TypeBranch             // iface -- ; pop iface; if iface doesn't hold type at mem[$2] (or $2==-1 for nil), ip += $1
	Panic                  // v -- ; pop value, start stack unwinding
	Recover                // -- v ; push recovered value (or nil if not panicking in a deferred call)
	DeferPush              // func [a0..an-1] -- func [a0..an-1] [packed prevHead retIP] ; register deferred call on stack; $0=narg, $1=1 if native
	DeferRet               // -- ; sentinel: restore outer frame after a deferred call returns
	MkSlice                // [v0..vn-1] -- slice ; collect $0 values into []T, elem type at mem[$1]
	MkMap                  // -- map ; create map[K]V, key type at mem[$0], val type at mem[$1]
	Append                 // slice [v0..vn-1] -- slice' ; append $0 values to slice
	CopySlice              // dst src -- n ; n = copy(dst, src)
	DeleteMap              // map key -- ; delete(map, key)
	Cap                    // -- x ; x = cap(mem[sp-$0])
	PtrNew                 // -- ptr ; ptr = new(T), type at mem[$0]
	Trap                   // -- ; pause VM execution and enter debug mode
	WrapFunc               // parscanFuncVal -- ParscanFunc ; wrap parscan func in reflect.MakeFunc for native callbacks; $0=typeIdx
	AddStr                 // s1 s2 -- s ; s = s1 + s2 (string concatenation)

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
	AddIntImm      // n -- n+$1
	SubIntImm      // n -- n-$1
	MulIntImm      // n -- n*$1
	GreaterIntImm  // n -- n>$1  (signed)
	GreaterUintImm // n -- n>$1 (unsigned)
	LowerIntImm    // n -- n<$1  (signed)
	LowerUintImm   // n -- n<$1  (unsigned)

	Next2Local // -- ; iterator next, set K V (local scope); like Next2 but scope is always Local
	GetLocal   // -- value ; value = mem[$1+fp-1] (local variable, no scope check)
	GetGlobal  // -- value ; value = mem[$1] (global variable, syncs num from ref if needed)
	NextLocal  // -- ; iterator next, set K (local scope); like Next but scope is always Local
)

// Memory attributes.
const (
	Global = 0
	Local  = 1
)

// frameOverhead is the number of bookkeeping slots in a call frame
// (deferHead, retIP, prevFP), between the arguments and locals.
const frameOverhead = 3

// Pos is the source code position of instruction.
type Pos int32

// Instruction represents a virtual machine bytecode instruction (16 bytes).
// Fields A, B hold up to 2 immediate operands (0 when unused).
type Instruction struct {
	Op   Op
	A, B int32
	Pos  Pos
}

func (i Instruction) String() (s string) {
	s = fmt.Sprintf("%3d: %v", i.Pos, i.Op)
	if i.A != 0 || i.B != 0 {
		s += fmt.Sprintf(" %v", i.A)
	}
	if i.B != 0 {
		s += fmt.Sprintf(" %v", i.B)
	}
	return s
}

// Code represents the virtual machine byte code.
type Code []Instruction

// Machine is a stack-based virtual machine that executes bytecode instructions.
type Machine struct {
	code    Code       // code to execute
	mem     []Value    // memory, as a stack
	ip, fp  int        // instruction pointer and frame pointer
	dataLen int        // number of global data slots in mem (set by Push)
	env     []*Value   // active closure's captured cells (nil for plain functions)
	frames  [][]*Value // saved caller envs (only for closure calls where env != nil)

	panicking bool  // true while unwinding due to panic
	panicVal  Value // value passed to panic()

	// funcFields maps struct func field addresses to parscan func values (int code addresses
	// or Closures). Parscan funcs cannot be stored directly in typed Go func fields via reflect.
	funcFields map[uintptr]Value
	// funcFieldsByFuncPtr is a stable fallback for funcFields when a struct containing func
	// fields is copied (e.g. via append). Keyed by the funcValue struct pointer, obtained by
	// dereferencing the field's memory address — unique per closure and stable across copies.
	// (reflect.Value.Pointer() on a Func returns only the code pointer, which reflect.MakeFunc
	// reuses for all closures, so it is not a suitable key.)
	funcFieldsByFuncPtr map[uintptr]Value

	debugInfoFn func() *DebugInfo // builds DebugInfo on demand (breaks vm->comp cycle)
	debugIn     io.Reader         // debug command input (nil = os.Stdin)
	debugOut    io.Writer         // debug output (nil = os.Stderr)
	stepping    bool              //nolint:unused // when true, trap after every instruction (planned)
	trapOrig    int               // ip to resume after Trap
}

// SetDebugInfo registers a function that builds DebugInfo on demand.
func (m *Machine) SetDebugInfo(fn func() *DebugInfo) { m.debugInfoFn = fn }

// SetDebugIO sets the I/O streams for the interactive debug mode.
func (m *Machine) SetDebugIO(in io.Reader, out io.Writer) {
	m.debugIn = in
	m.debugOut = out
}

// deferSentinelIP is the ip value used as return address for deferred call frames.
// A negative ip is checked before m.code[ip] to dispatch the DeferRet handler.
const deferSentinelIP = -1

// deferSentinelBits is deferSentinelIP packed into the retIP slot's low 32 bits.
// High 32 bits are zero (nret=0, narg=0 for defer frames).
const deferSentinelBits = uint64(0xFFFFFFFF) // low 32 bits = -1, high 32 bits = 0

// envSavedFlag is set in the high bit of prevFP when the caller's env was saved to m.frames.
const envSavedFlag = uint64(1) << 63

// packRetIP packs the return IP, nret, and narg into a single uint64.
// Layout: [narg:16 | nret:16 | retIP:32].
func packRetIP(retIP, nret, narg int) uint64 {
	return uint64(uint32(retIP)) | uint64(nret)<<32 | uint64(narg)<<48 //nolint:gosec
}

// panicUnwindIP is the ip sentinel used during panic stack unwinding.
// The main loop dispatches deferred calls and tears down frames when ip == panicUnwindIP.
const panicUnwindIP = -2

// trapIP is the ip sentinel that triggers interactive debug mode.
const trapIP = -3

// growStack ensures mem has room for at least sp+1+need elements, where sp is
// the index of the current top-of-stack element.
// Returns the (possibly reallocated) slice extended to its full capacity.
func growStack(mem []Value, sp, need int) []Value {
	required := sp + 1 + need
	if required <= len(mem) {
		return mem
	}
	n := max(len(mem)*2, required+256)
	newMem := make([]Value, n)
	copy(newMem, mem[:sp+1])
	return newMem
}

// Run runs a program.
func (m *Machine) Run() (err error) {
	mem, ip, fp := m.mem, m.ip, m.fp
	sp := len(mem) - 1
	// Extend mem to full capacity so all writes up to cap are in bounds.
	mem = mem[:cap(mem)]

	defer func() {
		m.mem, m.ip, m.fp = mem[:sp+1], ip, fp
	}()

	for {
		for ip >= 0 {
			c := m.code[ip] // current instruction
			if debug {
				log.Printf("ip:%-3d sp:%-3d fp:%-3d op:[%-20v] mem:%v\n", ip, sp, fp, c, Vstring(mem[:sp+1]))
			}
			switch c.Op {
			case Addr:
				v := mem[sp]
				if v.ref.CanAddr() {
					mem[sp] = Value{ref: v.ref.Addr()}
				} else {
					// Materialize via Reflect() to get an addressable value, then take its address.
					mem[sp] = Value{ref: v.Reflect().Addr()}
				}
			case Set:
				m.assignSlot(&mem[int(c.A)*(fp-1)+int(c.B)], mem[sp])
				sp--
			case Call:
				narg := int(c.A)
				fval := mem[sp-narg]
				// Inline fast path: only call resolveFuncField for addressable Func fields.
				if fval.ref.Kind() == reflect.Func && fval.ref.CanAddr() {
					fval = m.resolveFuncField(fval)
				}
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
					rv := fval.ref
					if rv.Kind() == reflect.Interface && !rv.IsNil() {
						rv = rv.Elem()
					}
					if rv.Kind() == reflect.Func {
						in := make([]reflect.Value, narg)
						for i := range in {
							in[i] = mem[sp-narg+1+i].Reflect()
						}
						sp -= narg + 1
						for _, v := range rv.Call(in) {
							if sp+1 >= len(mem) {
								mem = growStack(mem, sp, 1)
							}
							sp++
							mem[sp] = fromReflect(v)
						}
						break
					}
					nip = int(fval.num) //nolint:gosec
					m.env = nil
				}
				nret := int(c.B)
				fpVal := uint64(fp) //nolint:gosec
				if prevEnv != nil {
					m.frames = append(m.frames, prevEnv)
					fpVal |= envSavedFlag
				}
				if sp+3 >= len(mem) {
					mem = growStack(mem, sp, 3)
				}
				mem[sp+1] = Value{}
				mem[sp+2] = Value{num: packRetIP(ip+1, nret, narg)}
				mem[sp+3] = Value{num: fpVal}
				sp += 3 // deferHead, retIP+info, prevFP+envFlag
				ip = nip
				fp = sp + 1
				continue
			case Deref:
				r := mem[sp].ref.Elem()
				v := Value{ref: r}
				if isNum(r.Kind()) {
					v.num = numBits(r)
				}
				mem[sp] = v
			case DerefSet:
				ptr := mem[sp-1]
				val := mem[sp]
				numSet(ptr.ref.Elem(), val)
				sp -= 2
			case GetLocal:
				if sp+1 >= len(mem) {
					mem = growStack(mem, sp, 1)
				}
				sp++
				mem[sp] = mem[int(c.A)+fp-1]
			case GetGlobal:
				// Global slots written via SetS update ref through a shared pointer without
				// updating num in the original slot; sync num from ref before copying.
				v := mem[int(c.A)]
				if isNum(v.ref.Kind()) && v.ref.CanAddr() {
					v.num = numBits(v.ref)
				}
				if sp+1 >= len(mem) {
					mem = growStack(mem, sp, 1)
				}
				sp++
				mem[sp] = v
			case Get:
				if int(c.A) == Local {
					if sp+1 >= len(mem) {
						mem = growStack(mem, sp, 1)
					}
					sp++
					mem[sp] = mem[int(c.B)+fp-1]
				} else {
					v := mem[int(c.B)]
					if isNum(v.ref.Kind()) && v.ref.CanAddr() {
						v.num = numBits(v.ref)
					}
					if sp+1 >= len(mem) {
						mem = growStack(mem, sp, 1)
					}
					sp++
					mem[sp] = v
				}
			case New:
				mem[int(c.A)+fp-1] = NewValue(mem[int(c.B)].ref.Type())
			case Equal:
				mem[sp-1] = boolVal(mem[sp-1].Equal(mem[sp]))
				sp--
			case EqualSet:
				if mem[sp-1].Equal(mem[sp]) {
					// If equal then lhs and rhs are popped, replaced by test result, as in Equal.
					mem[sp-1] = boolVal(true)
					sp--
				} else {
					// If not equal then the lhs is let on stack for further processing.
					// This is used to simplify bytecode in case clauses of switch statments.
					mem[sp] = boolVal(false)
				}
			case Convert:
				idx := sp - int(c.B)
				v := mem[idx]
				dstType := mem[int(c.A)].ref.Type()
				dstKind := dstType.Kind()
				if !v.ref.IsValid() {
					// nil source: zero value of destination type.
					if dstKind != reflect.Interface {
						mem[idx] = fromReflect(reflect.Zero(dstType))
					}
					break
				}
				srcKind := v.ref.Type().Kind()

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
					case reflect.Int:
						mem[idx] = Value{num: bits, ref: zint}
					case reflect.Int8:
						mem[idx] = Value{num: uint64(int8(bits)), ref: zint8} //nolint:gosec
					case reflect.Int16:
						mem[idx] = Value{num: uint64(int16(bits)), ref: zint16} //nolint:gosec
					case reflect.Int32:
						mem[idx] = Value{num: uint64(int32(bits)), ref: zint32} //nolint:gosec
					case reflect.Int64:
						mem[idx] = Value{num: bits, ref: zint64}
					case reflect.Uint:
						mem[idx] = Value{num: bits, ref: zuint}
					case reflect.Uint8:
						mem[idx] = Value{num: uint64(uint8(bits)), ref: zuint8} //nolint:gosec
					case reflect.Uint16:
						mem[idx] = Value{num: uint64(uint16(bits)), ref: zuint16} //nolint:gosec
					case reflect.Uint32:
						mem[idx] = Value{num: uint64(uint32(bits)), ref: zuint32} //nolint:gosec
					case reflect.Uint64:
						mem[idx] = Value{num: bits, ref: zuint64}
					case reflect.Float32:
						mem[idx] = Value{num: math.Float64bits(float64(float32(math.Float64frombits(bits)))), ref: zfloat32}
					case reflect.Float64:
						mem[idx] = Value{num: bits, ref: zfloat64}
					}

				case isNum(srcKind) && dstKind == reflect.String:
					// int/rune -> string (e.g. string(65) -> "A").
					mem[idx] = Value{ref: reflect.ValueOf(string(rune(int64(v.num))))} //nolint:gosec

				case srcKind == reflect.String && dstKind == reflect.Slice && dstType.Elem().Kind() == reflect.Uint8:
					// string -> []byte.
					mem[idx] = Value{ref: reflect.ValueOf([]byte(v.ref.String()))}

				case srcKind == reflect.Slice && v.ref.Type().Elem().Kind() == reflect.Uint8 && dstKind == reflect.String:
					// []byte -> string.
					mem[idx] = Value{ref: reflect.ValueOf(string(v.ref.Bytes()))}

				default:
					// Fallback: use reflect.
					mem[idx] = fromReflect(v.Reflect().Convert(dstType))
				}

			case IfaceWrap:
				typ := mem[int(c.A)].ref.Interface().(*Type)
				idx := sp - int(c.B)
				mem[idx] = Value{ref: reflect.ValueOf(Iface{Typ: typ, Val: mem[idx]})}

			case IfaceCall:
				ifc := mem[sp].IfaceVal()
				method := ifc.Typ.Methods[int(c.A)]
				// The concrete type inside an embedded interface field is only known at runtime.
				for method.EmbedIface {
					rv := ifc.Val.Reflect()
					if rv.Kind() == reflect.Pointer {
						rv = rv.Elem()
					}
					for _, fi := range method.Path {
						rv = rv.Field(fi)
					}
					ifc = fromReflect(rv).IfaceVal()
					method = ifc.Typ.Methods[int(c.A)]
				}
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
				mem[sp] = Value{ref: reflect.ValueOf(Closure{Code: codeAddr, Env: []*Value{cell}})}

			case TypeAssert:
				dstTyp := mem[int(c.A)].ref.Interface().(*Type)
				okForm := int(c.B) == 1
				ifc := mem[sp]
				if !ifc.IsIface() {
					if !okForm {
						m.panicking = true
						m.panicVal = Value{ref: reflect.ValueOf(fmt.Sprintf("interface conversion: interface is nil, not %s", dstTyp))}
						sp--
						ip = panicUnwindIP
						continue
					}
					mem[sp] = NewValue(dstTyp.Rtype)
					if sp+1 >= len(mem) {
						mem = growStack(mem, sp, 1)
					}
					sp++
					mem[sp] = boolVal(false)
					break
				}
				concrete := ifc.IfaceVal()
				var matched bool
				dstIsIface := dstTyp.IsInterface()
				if dstIsIface {
					matched = concrete.Typ.Implements(dstTyp)
				} else {
					matched = concrete.Typ.SameAs(dstTyp)
				}
				if matched {
					// For interface targets, keep the Iface wrapping so IfaceCall still works.
					result := concrete.Val
					if dstIsIface {
						result = ifc
					}
					if okForm {
						mem[sp] = result
						if sp+1 >= len(mem) {
							mem = growStack(mem, sp, 1)
						}
						sp++
						mem[sp] = boolVal(true)
					} else {
						mem[sp] = result
					}
				} else {
					if !okForm {
						m.panicking = true
						m.panicVal = Value{ref: reflect.ValueOf(fmt.Sprintf("interface conversion: interface value is %s, not %s", concrete.Typ, dstTyp))}
						sp--
						ip = panicUnwindIP
						continue
					}
					mem[sp] = NewValue(dstTyp.Rtype)
					if sp+1 >= len(mem) {
						mem = growStack(mem, sp, 1)
					}
					sp++
					mem[sp] = boolVal(false)
				}

			case TypeBranch: // Arg[0]=offset, Arg[1]=typeIdx (-1 for nil case)
				ifc := mem[sp]
				sp--
				var matched bool
				if int(c.B) == -1 {
					matched = !ifc.IsIface()
				} else if ifc.IsIface() {
					ctyp := ifc.IfaceVal().Typ
					dtyp := mem[int(c.B)].ref.Interface().(*Type)
					if dtyp.IsInterface() {
						matched = ctyp.Implements(dtyp)
					} else {
						matched = ctyp.SameAs(dtyp)
					}
				}
				if !matched {
					ip += int(c.A)
					continue
				}

			case Exit:
				return err
			case Fnew:
				if sp+1 >= len(mem) {
					mem = growStack(mem, sp, 1)
				}
				sp++
				mem[sp] = NewValue(mem[int(c.A)].ref.Type(), int(c.B))
			case FnewE:
				if sp+1 >= len(mem) {
					mem = growStack(mem, sp, 1)
				}
				sp++
				mem[sp] = NewValue(mem[int(c.A)].ref.Type().Elem(), int(c.B))
			case Field:
				fv := forceSettable(fieldByAB(reflect.Indirect(mem[sp].ref), int(c.A), int(c.B)))
				switch {
				case isNum(fv.Kind()):
					// Preserve addressable ref for write-through on struct field mutations.
					mem[sp] = Value{num: numBits(fv), ref: fv}
				case fv.Kind() == reflect.Func && fv.CanAddr():
					// Always return addressable ref so SetS can update funcFields on reassignment.
					// Call checks funcFields for fast parscan dispatch.
					mem[sp] = Value{ref: fv}
				default:
					mem[sp] = Value{ref: fv}
				}
			case FieldSet:
				m.setFuncField(forceSettable(fieldByAB(mem[sp-1].ref, int(c.A), int(c.B))), mem[sp])
				sp--
			case FieldFset:
				m.setFuncField(forceSettable(mem[sp-2].ref.Field(int(mem[sp-1].num))), mem[sp]) //nolint:gosec
				sp -= 2
			case Jump:
				ip += int(c.A)
				continue
			case JumpTrue:
				cond := mem[sp].num != 0
				sp--
				if cond {
					ip += int(c.A)
					continue
				}
			case JumpFalse:
				cond := mem[sp].num != 0
				sp--
				if !cond {
					ip += int(c.A)
					continue
				}
			case JumpSetTrue:
				cond := mem[sp].num != 0
				if cond {
					ip += int(c.A)
					// Note that the stack is not modified if cond is true.
					continue
				}
				sp--
			case JumpSetFalse:
				cond := mem[sp].num != 0
				if !cond {
					ip += int(c.A)
					// Note that the stack is not modified if cond is false.
					continue
				}
				sp--
			case Len:
				if sp+1 >= len(mem) {
					mem = growStack(mem, sp, 1)
				}
				sp++
				mem[sp] = ValueOf(mem[sp-1-int(c.A)].ref.Len())
			case Next:
				if k, ok := mem[sp-1].ref.Interface().(func() (reflect.Value, bool))(); ok {
					m.assignSlot(&mem[int(c.B)], fromReflect(k))
				} else {
					ip += int(c.A)
					continue
				}
			case NextLocal:
				if k, ok := mem[sp-1].ref.Interface().(func() (reflect.Value, bool))(); ok {
					m.assignSlot(&mem[fp-1+int(c.B)], fromReflect(k))
				} else {
					ip += int(c.A)
					continue
				}
			case Next0:
				if _, ok := mem[sp-1].ref.Interface().(func() (reflect.Value, bool))(); !ok {
					ip += int(c.A)
					continue
				}
			case Next2:
				if k, v, ok := mem[sp-1].ref.Interface().(func() (reflect.Value, reflect.Value, bool))(); ok {
					kAddr, vAddr := int(int16(c.B)), int(int16(c.B>>16)) //nolint:gosec
					m.assignSlot(&mem[kAddr], fromReflect(k))
					m.assignSlot(&mem[vAddr], fromReflect(v))
				} else {
					ip += int(c.A)
					continue
				}
			case Next2Local:
				if k, v, ok := mem[sp-1].ref.Interface().(func() (reflect.Value, reflect.Value, bool))(); ok {
					kAddr, vAddr := int(int16(c.B)), int(int16(c.B>>16)) //nolint:gosec
					m.assignSlot(&mem[fp-1+kAddr], fromReflect(k))
					m.assignSlot(&mem[fp-1+vAddr], fromReflect(v))
				} else {
					ip += int(c.A)
					continue
				}
			case Not:
				if mem[sp].num != 0 {
					mem[sp].num = 0
				} else {
					mem[sp].num = 1
				}
				mem[sp].ref = zbool
			case Pop:
				sp -= int(c.A)
			case Push:
				if sp+1 >= len(mem) {
					mem = growStack(mem, sp, 1)
				}
				sp++
				mem[sp] = Value{num: uint64(int(c.A)), ref: zint} //nolint:gosec
			case Pull:
				next, stop := iter.Pull(mem[sp].Seq())
				if sp+2 >= len(mem) {
					mem = growStack(mem, sp, 2)
				}
				mem[sp+1] = ValueOf(next)
				mem[sp+2] = ValueOf(stop)
				sp += 2
			case Pull2:
				next, stop := iter.Pull2(mem[sp].Seq2())
				if sp+2 >= len(mem) {
					mem = growStack(mem, sp, 2)
				}
				mem[sp+1] = ValueOf(next)
				mem[sp+2] = ValueOf(stop)
				sp += 2
			case Grow:
				if n := int(c.A); sp+n >= len(mem) {
					mem = growStack(mem, sp, n)
				}
				sp += int(c.A)
			case DeferPush:
				// Snapshot args in-place (detach addressable refs to prevent aliasing).
				narg := int(c.A)
				isX := int(c.B)
				for i := sp - narg + 1; i <= sp; i++ {
					if isNum(mem[i].ref.Kind()) && mem[i].ref.CanAddr() {
						mem[i].ref = reflect.Zero(mem[i].ref.Type())
					}
				}
				// Push 3-slot header: packed(narg/isX), prevHead link, returnIP placeholder.
				prevHead := int(mem[fp-3].num) //nolint:gosec
				if sp+3 >= len(mem) {
					mem = growStack(mem, sp, 3)
				}
				mem[sp+1] = Value{num: uint64(narg<<1 | isX)} //nolint:gosec
				mem[sp+2] = Value{num: uint64(prevHead)}      //nolint:gosec
				mem[sp+3] = Value{}                           // returnIP placeholder, filled by Return
				sp += 3
				mem[fp-3].num = uint64(sp) //nolint:gosec // dh = index of returnIP slot

			case WrapFunc:
				// Wrap the parscan func value on the stack in a reflect.MakeFunc for native Go callbacks.
				// The original parscan func is preserved in ParscanFunc.Val for fast in-VM dispatch.
				// CallFunc is re-entrant for single-threaded synchronous callbacks; concurrent goroutine
				// calls to different wrapped functions on the same Machine are NOT safe.
				typ := mem[int(c.A)].ref.Interface().(*Type)
				fval := mem[sp]
				mem[sp] = Value{ref: reflect.ValueOf(ParscanFunc{Val: fval, GF: m.wrapForFunc(fval, typ.Rtype)})}

			case Trap:
				m.trapOrig = ip + 1 // resume ip after Trap instruction
				ip = trapIP
				continue

			case Panic:
				m.panicking = true
				m.panicVal = mem[sp]
				sp-- // pop the panic argument
				ip = panicUnwindIP
				continue

			case Recover:
				if m.panicking && int(int32(mem[fp-2].num)) == deferSentinelIP { //nolint:gosec
					m.panicking = false
					pv := m.panicVal
					// Wrap in Iface so type assertions on the recovered value work.
					if pv.IsValid() && !pv.IsIface() {
						rt := pv.Reflect().Type()
						typ := &Type{Name: rt.Name(), Rtype: rt}
						pv = Value{ref: reflect.ValueOf(Iface{Typ: typ, Val: pv})}
					}
					if sp+1 >= len(mem) {
						mem = growStack(mem, sp, 1)
					}
					sp++
					mem[sp] = pv
					m.panicVal = Value{}
				} else {
					if sp+1 >= len(mem) {
						mem = growStack(mem, sp, 1)
					}
					sp++
					mem[sp] = Value{} // nil
				}

			case Return:
				// Read nret and callNarg from the packed retIP slot.
				retIPInfo := mem[fp-2].num
				nret := int((retIPInfo >> 32) & 0xFFFF)
				callNarg := int(retIPInfo >> 48)
				// If there are pending defers in this frame, dispatch the top one (LIFO).
				dh := int(mem[fp-3].num) //nolint:gosec
				if dh != 0 {
					packed := mem[dh-2].num
					narg := int(packed >> 1) //nolint:gosec
					isX := packed&1 == 1
					prevHead := int(mem[dh-1].num) //nolint:gosec
					funcVal := mem[dh-narg-3]
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
						clear(mem[retBase+nret : sp+1])
						sp = retBase + nret - 1
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
					base := sp
					if sp+1 >= len(mem) {
						mem = growStack(mem, sp, 1)
					}
					sp++
					mem[sp] = funcVal
					{
						n := (dh - 2) - (dh - narg - 2)
						if sp+n >= len(mem) {
							mem = growStack(mem, sp, n)
						}
						copy(mem[sp+1:], mem[dh-narg-2:dh-2])
						sp += n
					}
					defFPVal := uint64(fp) //nolint:gosec
					if prevEnv != nil {
						m.frames = append(m.frames, prevEnv)
						defFPVal |= envSavedFlag
					}
					if sp+3 >= len(mem) {
						mem = growStack(mem, sp, 3)
					}
					mem[sp+1] = Value{}
					mem[sp+2] = Value{num: deferSentinelBits}
					mem[sp+3] = Value{num: defFPVal}
					sp += 3
					fp = base + 1 + narg + 3 + 1
					ip = nip
					continue
				}
				// No pending defers: normal frame teardown.
				ip = int(int32(retIPInfo)) //nolint:gosec
				ofp := fp
				fpVal := mem[fp-1].num
				if fpVal&envSavedFlag != 0 {
					fp = int(fpVal &^ envSavedFlag) //nolint:gosec
					top := len(m.frames) - 1
					m.env = m.frames[top]
					m.frames[top] = nil // clear for GC
					m.frames = m.frames[:top]
				} else {
					fp = int(fpVal) //nolint:gosec
					m.env = nil
				}
				newBase := ofp - callNarg - 4
				// Inline copy for common small nret to avoid runtime.typedslicecopy.
				switch nret {
				case 0:
					// nothing to copy
				case 1:
					mem[newBase] = mem[sp]
				default:
					copy(mem[newBase:], mem[sp-nret+1:sp+1])
				}
				newSP := newBase + nret - 1
				// Scalar clear for small frames to avoid runtime.memclrHasPointers call.
				if n := sp - newSP; n <= 8 {
					for i := newSP + 1; i <= sp; i++ {
						mem[i] = Value{}
					}
				} else {
					clear(mem[newSP+1 : sp+1])
				}
				sp = newSP
				continue
			case Slice:
				low := int(mem[sp-1].num) //nolint:gosec
				high := int(mem[sp].num)  //nolint:gosec
				mem[sp-2] = Value{ref: mem[sp-2].ref.Slice(low, high)}
				sp -= 2
			case Slice3:
				low := int(mem[sp-2].num)  //nolint:gosec
				high := int(mem[sp-1].num) //nolint:gosec
				hi := int(mem[sp].num)     //nolint:gosec
				mem[sp-3] = Value{ref: mem[sp-3].ref.Slice3(low, high, hi)}
				sp -= 3
			case Stop:
				mem[sp].ref.Interface().(func())()
				sp -= 4
			case Stop0:
				mem[sp].ref.Interface().(func())()
				sp -= 3
			// Generic bitwise.
			case BitAnd:
				mem[sp-1].num &= mem[sp].num
				resetNumRef(&mem[sp-1])
				sp--
			case BitOr:
				mem[sp-1].num |= mem[sp].num
				resetNumRef(&mem[sp-1])
				sp--
			case BitXor:
				mem[sp-1].num ^= mem[sp].num
				resetNumRef(&mem[sp-1])
				sp--
			case BitAndNot:
				mem[sp-1].num &^= mem[sp].num
				resetNumRef(&mem[sp-1])
				sp--
			case BitShl:
				mem[sp-1].num <<= mem[sp].num
				resetNumRef(&mem[sp-1])
				sp--
			case BitShr:
				k := mem[sp-1].ref.Kind()
				if k >= reflect.Uint && k <= reflect.Uintptr {
					mem[sp-1].num >>= mem[sp].num
				} else {
					mem[sp-1].num = uint64(int64(mem[sp-1].num) >> mem[sp].num) //nolint:gosec
				}
				resetNumRef(&mem[sp-1])
				sp--
			case BitComp:
				mem[sp].num = ^mem[sp].num
				resetNumRef(&mem[sp])

			case Swap:
				a, b := sp-int(c.A), sp-int(c.B)
				mem[a], mem[b] = mem[b], mem[a]
			case HAlloc:
				cell := new(Value)
				*cell = mem[sp] // initialise cell with top-of-stack value
				// Detach addressable refs to prevent aliasing: numeric values may share
				// the underlying memory of the source frame slot via their ref field.
				// Allocate a fresh reflect.Value (not reflect.Zero) so that Reflect() returns
				// the correct captured value via cell.ref.
				if isNum(cell.ref.Kind()) && cell.ref.CanAddr() {
					rv := reflect.New(cell.ref.Type()).Elem()
					setNumReflect(rv, cell.num)
					cell.ref = rv
				}
				mem[sp] = ValueOf(cell) // replace value with cell pointer
			case HGet:
				if sp+1 >= len(mem) {
					mem = growStack(mem, sp, 1)
				}
				sp++
				mem[sp] = *m.env[int(c.A)]
			case HSet:
				*m.env[int(c.A)] = mem[sp]
				sp--
			case HPtr:
				if sp+1 >= len(mem) {
					mem = growStack(mem, sp, 1)
				}
				sp++
				mem[sp] = ValueOf(m.env[int(c.A)])
			case MkClosure:
				n := int(c.A)
				codeAddr := int(mem[sp-n].num) //nolint:gosec
				env := make([]*Value, n)
				for i := range n {
					env[i] = mem[sp-n+1+i].ref.Interface().(*Value)
				}
				clo := ValueOf(Closure{Code: codeAddr, Env: env})
				clear(mem[sp-n : sp+1]) // clear code addr + cell ptr slots
				sp -= n
				mem[sp] = clo
			case MkSlice:
				n := int(c.A)
				elemType := mem[int(c.B)].ref.Type()
				sliceType := reflect.SliceOf(elemType)
				switch {
				case n < 0:
					// make([]T, len[, cap]): size args are on the stack.
					nSizeArgs := -n
					sLen := int(mem[sp-nSizeArgs+1].num) //nolint:gosec
					sCap := sLen
					if nSizeArgs == 2 {
						sCap = int(mem[sp].num) //nolint:gosec
					}
					sp -= nSizeArgs - 1
					mem[sp] = Value{ref: reflect.MakeSlice(sliceType, sLen, sCap)}
				case n == 0:
					if sp+1 >= len(mem) {
						mem = growStack(mem, sp, 1)
					}
					sp++
					mem[sp] = Value{ref: reflect.Zero(sliceType)}
				default:
					slice := reflect.MakeSlice(sliceType, n, n)
					for i := range n {
						numSet(slice.Index(i), mem[sp-n+1+i])
					}
					mem[sp-n+1] = Value{ref: slice}
					sp -= n - 1
				}
			case MkMap:
				keyType := mem[int(c.A)].ref.Type()
				valType := mem[int(c.B)].ref.Type()
				mapType := reflect.MapOf(keyType, valType)
				if sp+1 >= len(mem) {
					mem = growStack(mem, sp, 1)
				}
				sp++
				mem[sp] = Value{ref: reflect.MakeMap(mapType)}
			case Append:
				n := int(c.A)
				result := mem[sp-n].ref
				elemType := result.Type().Elem()
				for i := range n {
					result = reflect.Append(result, m.wrapForFunc(mem[sp-n+1+i], elemType))
				}
				mem[sp-n] = Value{ref: result}
				sp -= n
			case CopySlice:
				dst := mem[sp-1].ref
				src := mem[sp].ref
				n := reflect.Copy(dst, src)
				mem[sp-1] = ValueOf(n)
				sp--
			case DeleteMap:
				mem[sp-1].ref.SetMapIndex(mem[sp].Reflect(), reflect.Value{})
				sp--
			case Cap:
				if sp+1 >= len(mem) {
					mem = growStack(mem, sp, 1)
				}
				sp++
				mem[sp] = ValueOf(mem[sp-1-int(c.A)].ref.Cap())
			case PtrNew:
				typ := mem[int(c.A)].ref.Type()
				if sp+1 >= len(mem) {
					mem = growStack(mem, sp, 1)
				}
				sp++
				mem[sp] = Value{ref: reflect.New(typ)}
			case Index:
				idx := int(mem[sp].num) //nolint:gosec
				ref := reflect.Indirect(mem[sp-1].ref)
				if ref.Kind() == reflect.String {
					mem[sp-1] = Value{num: uint64(ref.String()[idx]), ref: zuint8}
				} else {
					mem[sp-1] = fromReflect(ref.Index(idx))
				}
				sp--
			case IndexAddr:
				idx := int(mem[sp].num) //nolint:gosec
				ref := reflect.Indirect(mem[sp-1].ref)
				mem[sp-1] = Value{ref: ref.Index(idx).Addr()}
				sp--
			case IndexSet:
				idx := int(mem[sp-1].num) //nolint:gosec
				slot := reflect.Indirect(mem[sp-2].ref).Index(idx)
				slot.Set(m.wrapForFunc(mem[sp], slot.Type()))
				sp -= 2
			case MapIndex:
				rv := mem[sp-1].ref.MapIndex(mem[sp].Reflect())
				mem[sp-1] = fromReflect(rv)
				sp--
			case MapIndexOk:
				mapVal := mem[sp-1].ref
				rv := mapVal.MapIndex(mem[sp].Reflect())
				ok := rv.IsValid()
				if !ok {
					rv = reflect.Zero(mapVal.Type().Elem())
				}
				mem[sp-1] = fromReflect(rv)
				mem[sp] = boolVal(ok)
			case MapSet:
				mapVal := mem[sp-2].ref
				mt := mapVal.Type()
				mapVal.SetMapIndex(numReflect(mt.Key(), mem[sp-1]), m.wrapForFunc(mem[sp], mt.Elem()))
				sp -= 2
			case SetS:
				n := int(c.A)
				for i := 0; i < n; i++ {
					m.assignSlot(&mem[sp-2*n+1+i], mem[sp-n+1+i])
				}
				sp -= 2 * n

			case AddStr:
				mem[sp-1] = Value{ref: reflect.ValueOf(mem[sp-1].ref.String() + mem[sp].ref.String())}
				sp--

			// Per-type Add.
			case AddInt:
				mem[sp-1].num = add[int](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zint
				sp--
			case AddInt8:
				mem[sp-1].num = add[int8](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zint8
				sp--
			case AddInt16:
				mem[sp-1].num = add[int16](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zint16
				sp--
			case AddInt32:
				mem[sp-1].num = add[int32](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zint32
				sp--
			case AddInt64:
				mem[sp-1].num = add[int64](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zint64
				sp--
			case AddUint:
				mem[sp-1].num = add[uint](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zuint
				sp--
			case AddUint8:
				mem[sp-1].num = add[uint8](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zuint8
				sp--
			case AddUint16:
				mem[sp-1].num = add[uint16](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zuint16
				sp--
			case AddUint32:
				mem[sp-1].num = add[uint32](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zuint32
				sp--
			case AddUint64:
				mem[sp-1].num = add[uint64](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zuint64
				sp--
			case AddFloat64:
				mem[sp-1].num = addf[float64](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zfloat64
				sp--
			case AddFloat32:
				mem[sp-1].num = addf[float32](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zfloat32
				sp--

				// Per-type Sub.
			case SubInt:
				mem[sp-1].num = sub[int](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zint
				sp--
			case SubInt8:
				mem[sp-1].num = sub[int8](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zint8
				sp--
			case SubInt16:
				mem[sp-1].num = sub[int16](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zint16
				sp--
			case SubInt32:
				mem[sp-1].num = sub[int32](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zint32
				sp--
			case SubInt64:
				mem[sp-1].num = sub[int64](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zint64
				sp--
			case SubUint:
				mem[sp-1].num = sub[uint](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zuint
				sp--
			case SubUint8:
				mem[sp-1].num = sub[uint8](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zuint8
				sp--
			case SubUint16:
				mem[sp-1].num = sub[uint16](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zuint16
				sp--
			case SubUint32:
				mem[sp-1].num = sub[uint32](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zuint32
				sp--
			case SubUint64:
				mem[sp-1].num = sub[uint64](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zuint64
				sp--
			case SubFloat64:
				mem[sp-1].num = subf[float64](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zfloat64
				sp--
			case SubFloat32:
				mem[sp-1].num = subf[float32](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zfloat32
				sp--

				// Per-type Mul.
			case MulInt:
				mem[sp-1].num = mul[int](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zint
				sp--
			case MulInt8:
				mem[sp-1].num = mul[int8](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zint8
				sp--
			case MulInt16:
				mem[sp-1].num = mul[int16](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zint16
				sp--
			case MulInt32:
				mem[sp-1].num = mul[int32](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zint32
				sp--
			case MulInt64:
				mem[sp-1].num = mul[int64](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zint64
				sp--
			case MulUint:
				mem[sp-1].num = mul[uint](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zuint
				sp--
			case MulUint8:
				mem[sp-1].num = mul[uint8](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zuint8
				sp--
			case MulUint16:
				mem[sp-1].num = mul[uint16](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zuint16
				sp--
			case MulUint32:
				mem[sp-1].num = mul[uint32](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zuint32
				sp--
			case MulUint64:
				mem[sp-1].num = mul[uint64](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zuint64
				sp--
			case MulFloat64:
				mem[sp-1].num = mulf[float64](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zfloat64
				sp--
			case MulFloat32:
				mem[sp-1].num = mulf[float32](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zfloat32
				sp--

				// Per-type Neg.
			case NegInt:
				mem[sp].num = neg[int](mem[sp].num)
				mem[sp].ref = zint
			case NegInt8:
				mem[sp].num = neg[int8](mem[sp].num)
				mem[sp].ref = zint8
			case NegInt16:
				mem[sp].num = neg[int16](mem[sp].num)
				mem[sp].ref = zint16
			case NegInt32:
				mem[sp].num = neg[int32](mem[sp].num)
				mem[sp].ref = zint32
			case NegInt64:
				mem[sp].num = neg[int64](mem[sp].num)
				mem[sp].ref = zint64
			case NegUint:
				mem[sp].num = neg[uint](mem[sp].num)
				mem[sp].ref = zuint
			case NegUint8:
				mem[sp].num = neg[uint8](mem[sp].num)
				mem[sp].ref = zuint8
			case NegUint16:
				mem[sp].num = neg[uint16](mem[sp].num)
				mem[sp].ref = zuint16
			case NegUint32:
				mem[sp].num = neg[uint32](mem[sp].num)
				mem[sp].ref = zuint32
			case NegUint64:
				mem[sp].num = neg[uint64](mem[sp].num)
				mem[sp].ref = zuint64
			case NegFloat64:
				mem[sp].num = negf[float64](mem[sp].num)
				mem[sp].ref = zfloat64
			case NegFloat32:
				mem[sp].num = negf[float32](mem[sp].num)
				mem[sp].ref = zfloat32

			// Per-type Greater.
			case GreaterInt, GreaterInt8, GreaterInt16, GreaterInt32, GreaterInt64:
				mem[sp-1] = boolVal(int64(mem[sp-1].num) > int64(mem[sp].num)) //nolint:gosec
				sp--
			case GreaterUint, GreaterUint8, GreaterUint16, GreaterUint32, GreaterUint64:
				mem[sp-1] = boolVal(mem[sp-1].num > mem[sp].num)
				sp--
			case GreaterFloat32, GreaterFloat64:
				mem[sp-1] = boolVal(math.Float64frombits(mem[sp-1].num) > math.Float64frombits(mem[sp].num))
				sp--

			// Per-type Lower.
			case LowerInt, LowerInt8, LowerInt16, LowerInt32, LowerInt64:
				mem[sp-1] = boolVal(int64(mem[sp-1].num) < int64(mem[sp].num)) //nolint:gosec
				sp--
			case LowerUint, LowerUint8, LowerUint16, LowerUint32, LowerUint64:
				mem[sp-1] = boolVal(mem[sp-1].num < mem[sp].num)
				sp--
			case LowerFloat32, LowerFloat64:
				mem[sp-1] = boolVal(math.Float64frombits(mem[sp-1].num) < math.Float64frombits(mem[sp].num))
				sp--

				// Per-type Div.
			case DivInt:
				mem[sp-1].num = div[int](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zint
				sp--
			case DivInt8:
				mem[sp-1].num = div[int8](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zint8
				sp--
			case DivInt16:
				mem[sp-1].num = div[int16](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zint16
				sp--
			case DivInt32:
				mem[sp-1].num = div[int32](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zint32
				sp--
			case DivInt64:
				mem[sp-1].num = div[int64](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zint64
				sp--
			case DivUint:
				mem[sp-1].num = div[uint](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zuint
				sp--
			case DivUint8:
				mem[sp-1].num = div[uint8](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zuint8
				sp--
			case DivUint16:
				mem[sp-1].num = div[uint16](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zuint16
				sp--
			case DivUint32:
				mem[sp-1].num = div[uint32](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zuint32
				sp--
			case DivUint64:
				mem[sp-1].num = div[uint64](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zuint64
			case DivFloat64:
				mem[sp-1].num = divf[float64](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zfloat64
				sp--
			case DivFloat32:
				mem[sp-1].num = divf[float32](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zfloat32
				sp--

				// Per-type Rem (integer only).
			case RemInt:
				mem[sp-1].num = rem[int](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zint
				sp--
			case RemInt8:
				mem[sp-1].num = rem[int8](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zint8
				sp--
			case RemInt16:
				mem[sp-1].num = rem[int16](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zint16
				sp--
			case RemInt32:
				mem[sp-1].num = rem[int32](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zint32
				sp--
			case RemInt64:
				mem[sp-1].num = rem[int64](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zint64
				sp--
			case RemUint:
				mem[sp-1].num = rem[uint](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zuint
				sp--
			case RemUint8:
				mem[sp-1].num = rem[uint8](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zuint8
				sp--
			case RemUint16:
				mem[sp-1].num = rem[uint16](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zuint16
				sp--
			case RemUint32:
				mem[sp-1].num = rem[uint32](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zuint32
				sp--
			case RemUint64:
				mem[sp-1].num = rem[uint64](mem[sp-1].num, mem[sp].num)
				mem[sp-1].ref = zuint64
				sp--

			// Immediate operand ops: right-hand constant is in Arg[0].
			case AddIntImm:
				mem[sp].num = uint64(int(mem[sp].num) + int(c.A)) //nolint:gosec
				mem[sp].ref = zint
			case SubIntImm:
				mem[sp].num = uint64(int(mem[sp].num) - int(c.A)) //nolint:gosec
				mem[sp].ref = zint
			case MulIntImm:
				mem[sp].num = uint64(int(mem[sp].num) * int(c.A)) //nolint:gosec
				mem[sp].ref = zint
			case GreaterIntImm:
				mem[sp] = boolVal(int(mem[sp].num) > int(c.A)) //nolint:gosec
			case GreaterUintImm:
				mem[sp] = boolVal(uint(mem[sp].num) > uint(int(c.A))) //nolint:gosec
			case LowerIntImm:
				mem[sp] = boolVal(int(mem[sp].num) < int(c.A)) //nolint:gosec
			case LowerUintImm:
				mem[sp] = boolVal(uint(mem[sp].num) < uint(int(c.A))) //nolint:gosec
			}
			ip++
		}
		// Shrink mem to sp+1 for sentinel handlers that still use append / slice.
		mem = mem[:sp+1]
		// Negative ip is a sentinel for special handlers.
		if ip == panicUnwindIP {
			// Panic unwind: dispatch deferred calls in current frame, then tear down.
			if fp == 0 {
				// Top-level panic: no call frame to unwind.
				m.mem, m.ip, m.fp = mem, 0, 0
				return fmt.Errorf("panic: %v", m.panicVal.Interface())
			}
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
					clear(mem[retBase:])
					mem = mem[:retBase]
					mem[fp-3].num = uint64(prevHead) //nolint:gosec
					sp = len(mem) - 1
					mem = mem[:cap(mem)]
					continue
				}
				// VM defer: store panicUnwindIP as return address, push frame.
				retIPInfo := mem[fp-2].num
				nret := int((retIPInfo >> 32) & 0xFFFF)
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
				defFPVal := uint64(fp) //nolint:gosec
				if prevEnv != nil {
					m.frames = append(m.frames, prevEnv)
					defFPVal |= envSavedFlag
				}
				mem = append(mem, Value{}, Value{num: deferSentinelBits}, Value{num: defFPVal})
				fp = base + 1 + narg + 3
				ip = nip
				sp = len(mem) - 1
				mem = mem[:cap(mem)]
				continue
			}
			// No more defers in this frame.
			if !m.panicking {
				// Recovered: tear down frame, return zero values to caller.
				retIPInfo := mem[fp-2].num
				nret := int((retIPInfo >> 32) & 0xFFFF)
				numIn := int(retIPInfo >> 48)
				ip = int(int32(retIPInfo)) //nolint:gosec
				ofp := fp
				fpVal := mem[fp-1].num
				if fpVal&envSavedFlag != 0 {
					fp = int(fpVal &^ envSavedFlag) //nolint:gosec
					top := len(m.frames) - 1
					m.env = m.frames[top]
					m.frames[top] = nil
					m.frames = m.frames[:top]
				} else {
					fp = int(fpVal) //nolint:gosec
					m.env = nil
				}
				newBase := ofp - numIn - 4
				newSP := newBase + nret
				clear(mem[newBase:newSP]) // clear return slots
				clear(mem[newSP:])        // clear stale slots
				mem = mem[:newSP]
				sp = len(mem) - 1
				mem = mem[:cap(mem)]
				continue
			}
			// Still panicking: tear down frame, continue unwinding parent.
			numIn := int(mem[fp-2].num >> 48)
			ofp := fp
			fpVal := mem[fp-1].num
			if fpVal&envSavedFlag != 0 {
				fp = int(fpVal &^ envSavedFlag) //nolint:gosec
				top := len(m.frames) - 1
				m.env = m.frames[top]
				m.frames[top] = nil
				m.frames = m.frames[:top]
			} else {
				fp = int(fpVal) //nolint:gosec
				m.env = nil
			}
			if fp == 0 {
				// Top of stack: return panic as error.
				m.mem, m.ip, m.fp = mem, 0, 0
				return fmt.Errorf("panic: %v", m.panicVal.Interface())
			}
			newBase := ofp - numIn - 3 - 1 // below func slot
			clear(mem[newBase:])
			mem = mem[:newBase]
			sp = len(mem) - 1
			mem = mem[:cap(mem)]
			continue
		}
		if ip == trapIP {
			m.mem, m.ip, m.fp = mem, m.trapOrig, fp
			m.enterDebug()
			mem, ip, fp = m.mem, m.ip, m.fp
			sp = len(mem) - 1
			mem = mem[:cap(mem)]
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
		clear(mem[retBase+nret:]) // clear stale slots
		mem = mem[:retBase+nret]
		mem[fp-3].num = uint64(prevHead) //nolint:gosec
		ip = returnIP
		sp = len(mem) - 1
		mem = mem[:cap(mem)]
		continue
	}
}

// fieldByABC reconstructs a FieldByIndex path from fixed A, B, C args.
// B < 0 means single-level; C < 0 means two-level; otherwise three-level.
// fieldByAB accesses a struct field using the A, B encoding:
//
//	B == -1: single field at index A
//	B >= 0:  two-level path [A, B]
func fieldByAB(v reflect.Value, a, b int) reflect.Value {
	if b < 0 {
		return v.Field(a)
	}
	return v.FieldByIndex([]int{a, b})
}

// PushCode adds instructions to the machine code (with zero source positions).
func (m *Machine) PushCode(code ...Instruction) (p int) {
	p = len(m.code)
	m.code = append(m.code, code...)
	return p
}

// SetIP sets the value of machine instruction pointer to given index.
func (m *Machine) SetIP(ip int) { m.ip = ip }

// Push pushes data values on top of machine memory stack.
// It also records the new length as the global data boundary (dataLen),
// since globals are always loaded via Push before Run is called.
func (m *Machine) Push(v ...Value) (l int) {
	l = len(m.mem)
	m.mem = append(m.mem, v...)
	m.dataLen = len(m.mem)
	return l
}

// wrapForFunc returns a reflect.Value suitable for storing into a Go container
// of the given type. For func types, parscan func values are wrapped as proper Go funcs.
// For non-func types, numeric type mismatches are resolved via raw-bits reinterpretation.
func (m *Machine) wrapForFunc(val Value, funcType reflect.Type) reflect.Value {
	if funcType.Kind() != reflect.Func {
		return numReflect(funcType, val)
	}
	rv := val.Reflect()
	if !rv.IsValid() {
		return rv
	}
	if rv.Kind() == reflect.Func {
		if pf, ok := rv.Interface().(ParscanFunc); ok {
			return pf.GF
		}
		return rv // already a proper Go func
	}
	// Closure, code address, or other parscan func value.
	fv := val
	return reflect.MakeFunc(funcType, func(args []reflect.Value) []reflect.Value {
		out, err := m.CallFunc(fv, funcType, args)
		if err != nil {
			panic(err)
		}
		return out
	})
}

// TrimStack removes leftover stack values from a previous Run, restoring mem
// to the global data boundary. Call before pushing new global data on re-entry.
func (m *Machine) TrimStack() {
	if len(m.mem) > m.dataLen {
		m.mem = m.mem[:m.dataLen]
	}
}

// CallFunc executes a parscan function value with the given arguments and returns the results.
// It saves and restores all execution state so it can be called from native Go callbacks
// (reflect.MakeFunc wrappers) even while Run is in progress (single-threaded re-entrancy).
func (m *Machine) CallFunc(fval Value, funcType reflect.Type, args []reflect.Value) ([]reflect.Value, error) {
	// Save all volatile execution state.
	savedMem := m.mem
	savedIP := m.ip
	savedFP := m.fp
	savedEnv := m.env
	savedFrames := m.frames
	savedPanicking := m.panicking
	savedPanicVal := m.panicVal
	savedCodeLen := len(m.code)

	defer func() {
		m.mem = savedMem
		m.ip = savedIP
		m.fp = savedFP
		m.env = savedEnv
		m.frames = savedFrames
		m.panicking = savedPanicking
		m.panicVal = savedPanicVal
		m.code = m.code[:savedCodeLen]
	}()

	// Reset per-call state.
	m.env = nil
	m.frames = nil
	m.panicking = false
	m.panicVal = Value{}

	// Fresh stack: copy globals to a new backing array so the outer Run's mem is unaffected.
	m.mem = append([]Value(nil), m.mem[:m.dataLen]...)

	// Push func value and args.
	m.mem = append(m.mem, fval)
	for _, a := range args {
		m.mem = append(m.mem, fromReflect(a))
	}

	// Temporarily append Call + Exit to drive the function to completion.
	narg := funcType.NumIn()
	nret := funcType.NumOut()
	callIP := len(m.code)
	m.code = append(m.code, Instruction{Op: Call, A: int32(narg), B: int32(nret)}) //nolint:gosec
	m.code = append(m.code, Instruction{Op: Exit})
	m.ip = callIP
	m.fp = 0

	if err := m.Run(); err != nil {
		return nil, err
	}

	// Return values land at m.mem[dataLen:dataLen+nret] after the call frame tears down.
	out := make([]reflect.Value, nret)
	for i := range out {
		out[i] = m.mem[m.dataLen+i].Reflect()
	}
	return out, nil
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
		if !v.ref.IsValid() {
			fmt.Fprintf(&sb, "<%d>", v.num)
		} else {
			fmt.Fprintf(&sb, "%v", v.Interface())
		}
	}
	sb.WriteByte(']')
	return sb.String()
}

// funcValuePtr returns the funcValue struct pointer stored in an addressable func field fv.
// reflect.MakeFunc reuses the same code pointer for all closures, so Pointer() is not unique;
// the funcValue struct pointer obtained by dereferencing the field address is.
func funcValuePtr(fv reflect.Value) uintptr {
	return *(*uintptr)(fv.Addr().UnsafePointer()) //nolint:gosec
}

// forceSettable returns fv as-is if settable, or makes it settable via unsafe.
// Use it only on unexported struct fields. If the value is not addressable,
// it is returned as-is (e.g. field of a temporary struct is readable but not settable).
func forceSettable(fv reflect.Value) reflect.Value {
	if !fv.CanSet() && fv.CanAddr() {
		fv = reflect.NewAt(fv.Type(), unsafe.Pointer(fv.UnsafeAddr())).Elem()
	}
	return fv
}

// resolveFuncField returns the parscan Value for v if v is an addressable struct func
// field registered in funcFields or funcFieldsByFuncPtr; otherwise returns v unchanged.
func (m *Machine) resolveFuncField(v Value) Value {
	if v.ref.Kind() == reflect.Func && v.ref.CanAddr() {
		if pf, ok := m.funcFields[v.ref.UnsafeAddr()]; ok {
			return pf
		}
		if !v.ref.IsNil() && m.funcFieldsByFuncPtr != nil {
			if pf, ok := m.funcFieldsByFuncPtr[funcValuePtr(v.ref)]; ok {
				return pf
			}
		}
	}
	return v
}

// setFuncField stores val into a settable struct field fv.
// If val is a ParscanFunc (from WrapFunc), the original parscan func goes into the
// funcFields side table (for in-VM dispatch) and the Go func is set on the field
// (for native Go callbacks). Handles both addressable and plain reflect.Func fields.
func (m *Machine) setFuncField(fv reflect.Value, val Value) {
	if pf, ok := val.ref.Interface().(ParscanFunc); ok && fv.CanAddr() {
		if m.funcFields == nil {
			m.funcFields = make(map[uintptr]Value)
		}
		m.funcFields[fv.UnsafeAddr()] = pf.Val
		fv.Set(pf.GF)
		// Also register by funcValue ptr so Call can find it after a struct copy (e.g. append).
		if ptr := funcValuePtr(fv); ptr != 0 {
			if m.funcFieldsByFuncPtr == nil {
				m.funcFieldsByFuncPtr = make(map[uintptr]Value)
			}
			m.funcFieldsByFuncPtr[ptr] = pf.Val
		}
		return
	}
	if fv.Kind() == reflect.Func && fv.CanAddr() {
		if m.funcFields == nil {
			m.funcFields = make(map[uintptr]Value)
		}
		m.funcFields[fv.UnsafeAddr()] = val
		return
	}
	if isNum(fv.Kind()) && isNum(val.ref.Kind()) {
		// Avoid reflect.Set type-mismatch when field and value are different numeric kinds
		// (e.g. uint field, int value from untyped const).
		setNumReflect(fv, val.num)
		return
	}
	fv.Set(val.Reflect())
}

// assignSlot writes src into the memory slot dst, updating both num and ref
// for numeric types to maintain the dual-storage invariant.
func (m *Machine) assignSlot(dst *Value, src Value) {
	// Struct func field → func var: resolve to the parscan value so Call can dispatch
	// it directly (interface{} slots are transparent to the Func/CanAddr check in Call).
	if pf := m.resolveFuncField(src); pf != src {
		*dst = pf
		return
	}
	// Struct func fields can't hold parscan func values (int code addresses or Closures)
	// via reflect.Set. Store them in a side table keyed by the field's memory address.
	// funcFieldsByFuncPtr is not updated here: src is a raw Closure/int (not a ParscanFunc),
	// so no Go func is stored in the field and the funcValuePtr fallback is not applicable.
	if dst.ref.Kind() == reflect.Func && dst.ref.CanAddr() {
		if m.funcFields == nil {
			m.funcFields = make(map[uintptr]Value)
		}
		m.funcFields[dst.ref.UnsafeAddr()] = src
		dst.num = src.num
		return
	}
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
		if dst.ref.CanSet() {
			s := src.ref
			if !s.IsValid() {
				s = reflect.Zero(dst.ref.Type())
			}
			dst.ref.Set(s)
		} else {
			dst.ref = src.ref
		}
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

// numSet assigns src into the settable reflect.Value dst, handling different-kind numeric pairs
// (e.g. int value into uint slot from untyped const) that reflect.Set would reject.
func numSet(dst reflect.Value, src Value) {
	if isNum(dst.Kind()) && isNum(src.ref.Kind()) {
		setNumReflect(dst, src.num)
	} else {
		dst.Set(src.Reflect())
	}
}

// numReflect returns src as a reflect.Value of type t, handling different-kind numeric pairs
// (e.g. int value for uint map key) that reflect.SetMapIndex would reject.
func numReflect(t reflect.Type, src Value) reflect.Value {
	if isNum(t.Kind()) && isNum(src.ref.Kind()) {
		r := reflect.New(t).Elem()
		setNumReflect(r, src.num)
		return r
	}
	return src.Reflect()
}
