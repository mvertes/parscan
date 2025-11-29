// Package vm implement a stack based virtual machine.
package vm

import (
	"fmt"     // for tracing only
	"log"     // for tracing only
	"reflect" // for optional CallX only
	"strings"
	"unsafe" // to allow setting unexported struct fields
)

const debug = true

// Op is a VM opcode (bytecode instruction).
type Op int

//go:generate stringer -type=Op

// Byte-code instruction set.
const (
	// Instruction effect on stack: values consumed -- values produced.
	Nop          Op = iota // --
	Add                    // n1 n2 -- sum ; sum = n1+n2
	Addr                   // a -- &a ;
	Assign                 // val -- ; mem[$1] = val
	Fassign                // val -- ; mem[$1] = val
	Vassign                // val dest -- ; dest.Set(val)
	Call                   // f [a1 .. ai] -- [r1 .. rj] ; r1, ... = prog[f](a1, ...)
	Calli                  // f [a1 .. ai] -- [r1 .. rj] ; r1, ... = prog[f](a1, ...)
	CallX                  // f [a1 .. ai] -- [r1 .. rj] ; r1, ... = mem[f](a1, ...)
	Deref                  // x -- *x ;
	Dup                    // addr -- value ; value = mem[addr]
	Fdup                   // addr -- value ; value = mem[addr]
	Equal                  // n1 n2 -- cond ; cond = n1 == n2
	EqualSet               // n1 n2 -- n1 cond ; cond = n1 == n2
	Exit                   // -- ;
	Field                  // s -- f ; f = s.FieldIndex($1, ...)
	Greater                // n1 n2 -- cond; cond = n1 > n2
	Grow                   // -- ; sp += $1
	Index                  // a i -- a[i] ;
	Jump                   // -- ; ip += $1
	JumpTrue               // cond -- ; if cond { ip += $1 }
	JumpFalse              // cond -- ; if cond { ip += $1 }
	JumpSetTrue            //
	JumpSetFalse           //
	Lower                  // n1 n2 -- cond ; cond = n1 < n2
	Loweri                 // n1 -- cond ; cond = n1 < $1
	Mul                    // n1 n2 -- prod ; prod = n1*n2
	New                    // -- x; mem[fp+$1] = new mem[$2]
	Not                    // c -- r ; r = !c
	Pop                    // v --
	Push                   // -- v
	Return                 // [r1 .. ri] -- ; exit frame: sp = fp, fp = pop
	Sub                    // n1 n2 -- diff ; diff = n1 - n2
	Subi                   // n1 -- diff ; diff = n1 - $1
	Swap                   // --
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
	s = fmt.Sprintf("%4d: %v", i.Pos, i.Op)
	var sb strings.Builder
	for _, a := range i.Arg {
		sb.WriteString(fmt.Sprintf(" %v", a))
	}
	s += sb.String()
	return s
}

// Code represents the virtual machine byte code.
type Code []Instruction

// Machine represents a virtual machine.
type Machine struct {
	code   Code    // code to execute
	mem    []Value // memory, as a stack
	ip, fp int     // instruction pointer and frame pointer
	ic     uint64  // instruction counter, incremented at each instruction executed
	// flags  uint      // to set options such as restrict CallX, etc...
}

// Run runs a program.
func (m *Machine) Run() (err error) {
	mem, ip, fp, sp, ic := m.mem, m.ip, m.fp, 0, m.ic

	defer func() { m.mem, m.ip, m.fp, m.ic = mem, ip, fp, ic }()

	for {
		sp = len(mem) // stack pointer
		c := m.code[ip]
		if debug {
			log.Printf("ip:%-4d sp:%-4d fp:%-4d op:[%-14v] mem:%v\n", ip, sp, fp, c, Vstring(mem))
		}
		ic++
		switch c.Op {
		case Add:
			mem[sp-2] = ValueOf(int(mem[sp-2].Int() + mem[sp-1].Int()))
			mem = mem[:sp-1]
		case Mul:
			mem[sp-2] = ValueOf(int(mem[sp-2].Int() * mem[sp-1].Int()))
			mem = mem[:sp-1]
		case Addr:
			mem[sp-1].Value = mem[sp-1].Addr()
		case Assign:
			mem[c.Arg[0]].Set(mem[sp-1].Value)
			mem = mem[:sp-1]
		case Fassign:
			mem[fp+c.Arg[0]-1].Set(mem[sp-1].Value)
			mem = mem[:sp-1]
		case Call:
			nip := int(mem[sp-1].Int())
			mem = append(mem[:sp-1], ValueOf(ip+1), ValueOf(fp))
			ip = nip
			fp = sp + 1
			continue
		case Calli:
			mem = append(mem, ValueOf(ip+1), ValueOf(fp))
			fp = sp + 2
			ip = c.Arg[0]
			continue
		case CallX: // Should be made optional.
			in := make([]reflect.Value, c.Arg[0])
			for i := range in {
				in[i] = mem[sp-2-i].Value
			}
			f := mem[sp-1].Value
			mem = mem[:sp-c.Arg[0]-1]
			for _, v := range f.Call(in) {
				mem = append(mem, Value{Value: v})
			}
		case Deref:
			mem[sp-1].Value = mem[sp-1].Value.Elem()
		case Dup:
			mem = append(mem, mem[c.Arg[0]])
		case New:
			mem[c.Arg[0]+fp-1] = NewValue(mem[c.Arg[1]].Type)
		case Equal:
			mem[sp-2] = ValueOf(mem[sp-2].Equal(mem[sp-1].Value))
			mem = mem[:sp-1]
		case EqualSet:
			if mem[sp-2].Equal(mem[sp-1].Value) {
				// If equal then lhs and rhs are popped, replaced by test result, as in Equal.
				mem[sp-2] = ValueOf(true)
				mem = mem[:sp-1]
			} else {
				// If not equal then the lhs is let on stack for further processing.
				// This is used to simplify bytecode in case clauses of switch statments.
				mem[sp-1] = ValueOf(false)
			}
		case Exit:
			return err
		case Fdup:
			mem = append(mem, mem[c.Arg[0]+fp-1])
		case Field:
			fv := mem[sp-1].FieldByIndex(c.Arg)
			if !fv.CanSet() {
				// Normally private fields can not bet set via reflect. Override this limitation.
				fv = reflect.NewAt(fv.Type(), unsafe.Pointer(fv.UnsafeAddr())).Elem()
			}
			mem[sp-1].Value = fv
		case Jump:
			ip += c.Arg[0]
			continue
		case JumpTrue:
			cond := mem[sp-1].Bool()
			mem = mem[:sp-1]
			if cond {
				ip += c.Arg[0]
				continue
			}
		case JumpFalse:
			cond := mem[sp-1].Bool()
			mem = mem[:sp-1]
			if !cond {
				ip += c.Arg[0]
				continue
			}
		case JumpSetTrue:
			cond := mem[sp-1].Bool()
			if cond {
				ip += c.Arg[0]
				// Note that the stack is not modified if cond is true.
				continue
			}
			mem = mem[:sp-1]
		case JumpSetFalse:
			cond := mem[sp-1].Bool()
			if !cond {
				ip += c.Arg[0]
				// Note that the stack is not modified if cond is false.
				continue
			}
			mem = mem[:sp-1]
		case Greater:
			mem[sp-2] = ValueOf(mem[sp-1].Int() > mem[sp-2].Int())
			mem = mem[:sp-1]
		case Lower:
			mem[sp-2] = ValueOf(mem[sp-1].Int() < mem[sp-2].Int())
			mem = mem[:sp-1]
		case Loweri:
			mem[sp-1] = ValueOf(mem[sp-1].Int() < int64(c.Arg[0]))
		case Not:
			mem[sp-1] = ValueOf(!mem[sp-1].Bool())
		case Pop:
			mem = mem[:sp-c.Arg[0]]
		case Push:
			mem = append(mem, NewValue(TypeOf(0)))
			mem[sp].SetInt(int64(c.Arg[0]))
		case Grow:
			mem = append(mem, make([]Value, c.Arg[0])...)
		case Return:
			ip = int(mem[fp-2].Int())
			ofp := fp
			fp = int(mem[fp-1].Int())
			mem = append(mem[:ofp-c.Arg[0]-c.Arg[1]-1], mem[sp-c.Arg[0]:]...)
			continue
		case Sub:
			mem[sp-2] = ValueOf(int(mem[sp-1].Int() - mem[sp-2].Int()))
			mem = mem[:sp-1]
		case Subi:
			mem[sp-1] = ValueOf(int(mem[sp-1].Int()) - c.Arg[0])
		case Swap:
			mem[sp-2], mem[sp-1] = mem[sp-1], mem[sp-2]
		case Index:
			mem[sp-2].Value = mem[sp-1].Index(int(mem[sp-2].Int()))
			mem = mem[:sp-1]
		case Vassign:
			mem[sp-1].Set(mem[sp-2].Value)
			mem = mem[:sp-2]
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

// Pop removes and returns the value on the top of machine stack.
// func (m *Machine) Pop() (v Value) {
// 	l := len(m.mem) - 1
// 	v = m.mem[l]
// 	m.mem = m.mem[:l]
// 	return v
// }

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

// Vstring returns the string repreentation of a list of values.
func Vstring(lv []Value) string {
	s := "["
	for _, v := range lv {
		if s != "[" {
			s += " "
		}
		s += fmt.Sprintf("%v", v.Value)
	}
	return s + "]"
}
