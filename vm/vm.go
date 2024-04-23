// Package vm implement a stack based virtual machine.
package vm

import (
	"fmt"     // for tracing only
	"log"     // for tracing only
	"reflect" // for optional CallX only
	"strconv" // for tracing only
)

const debug = true

// Byte-code instruction set.
const (
	// Instruction effect on stack: values consumed -- values produced.
	Nop          = iota // --
	Add                 // n1 n2 -- sum ; sum = n1+n2
	Addr                // a -- &a ;
	Assign              // val -- ; mem[$1] = val
	Fassign             // val -- ; mem[$1] = val
	Vassign             // val dest -- ; dest.Set(val)
	Call                // f [a1 .. ai] -- [r1 .. rj] ; r1, ... = prog[f](a1, ...)
	Calli               // f [a1 .. ai] -- [r1 .. rj] ; r1, ... = prog[f](a1, ...)
	CallX               // f [a1 .. ai] -- [r1 .. rj] ; r1, ... = mem[f](a1, ...)
	Deref               // x -- *x ;
	Dup                 // addr -- value ; value = mem[addr]
	Fdup                // addr -- value ; value = mem[addr]
	Equal               // n1 n2 -- cond ; cond = n1 == n2
	EqualSet            // n1 n2 -- n1 cond ; cond = n1 == n2
	Exit                // -- ;
	Field               // s -- f ; f = s.FieldIndex($1, ...)
	Greater             // n1 n2 -- cond; cond = n1 > n2
	Grow                // -- ; sp += $1
	Index               // a i -- a[i] ;
	Jump                // -- ; ip += $1
	JumpTrue            // cond -- ; if cond { ip += $1 }
	JumpFalse           // cond -- ; if cond { ip += $1 }
	JumpSetTrue         //
	JumpSetFalse        //
	Lower               // n1 n2 -- cond ; cond = n1 < n2
	Loweri              // n1 -- cond ; cond = n1 < $1
	Mul                 // n1 n2 -- prod ; prod = n1*n2
	New                 // -- x; mem[fp+$1] = new mem[$2]
	Not                 // c -- r ; r = !c
	Pop                 // v --
	Push                // -- v
	Return              // [r1 .. ri] -- ; exit frame: sp = fp, fp = pop
	Sub                 // n1 n2 -- diff ; diff = n1 - n2
	Subi                // n1 -- diff ; diff = n1 - $1
)

var strop = [...]string{ // for VM tracing.
	Nop:          "Nop",
	Add:          "Add",
	Addr:         "Addr",
	Assign:       "Assign",
	Call:         "Call",
	Calli:        "Calli",
	CallX:        "CallX",
	Deref:        "Deref",
	Dup:          "Dup",
	Equal:        "Equal",
	EqualSet:     "EqualSet",
	Exit:         "Exit",
	Fassign:      "Fassign",
	Fdup:         "Fdup",
	Field:        "Field",
	Greater:      "Greater",
	Grow:         "Grow",
	Index:        "Index",
	Jump:         "Jump",
	JumpTrue:     "JumpTrue",
	JumpFalse:    "JumpFalse",
	JumpSetTrue:  "JumpSetTrue",
	JumpSetFalse: "JumpSetFalse",
	Lower:        "Lower",
	Loweri:       "Loweri",
	Mul:          "Mul",
	New:          "New",
	Not:          "Not",
	Pop:          "Pop",
	Push:         "Push",
	Return:       "Return",
	Sub:          "Sub",
	Subi:         "Subi",
	Vassign:      "Vassign",
}

// Code represents the virtual machine byte code.
type Code [][]int64

// Machine represents a virtual machine.
type Machine struct {
	code   Code    // code to execute
	mem    []Value // memory, as a stack
	ip, fp int     // instruction and frame pointer
	ic     uint64  // instruction counter, incremented at each instruction executed
	// flags  uint      // to set options such as restrict CallX, etc...
}

// Run runs a program.
func (m *Machine) Run() (err error) {
	code, mem, ip, fp, sp, ic := m.code, m.mem, m.ip, m.fp, 0, m.ic

	defer func() { m.mem, m.ip, m.fp, m.ic = mem, ip, fp, ic }()

	trace := func() {
		if !debug {
			return
		}
		var op2, op3 string
		c := code[ip]
		if l := len(c); l > 2 {
			op2 = strconv.Itoa(int(c[2]))
			if l > 3 {
				op3 = strconv.Itoa(int(c[3]))
			}
		}
		log.Printf("ip:%-4d sp:%-4d fp:%-4d op:[%-12s %-4s %-4s] mem:%v\n", ip, sp, fp, strop[c[1]], op2, op3, Vstring(mem))
	}

	for {
		sp = len(mem) // stack pointer
		trace()
		ic++
		switch op := code[ip]; op[1] {
		case Add:
			mem[sp-2] = ValueOf(int(mem[sp-2].Data.Int() + mem[sp-1].Data.Int()))
			mem = mem[:sp-1]
		case Mul:
			mem[sp-2] = ValueOf(int(mem[sp-2].Data.Int() * mem[sp-1].Data.Int()))
			mem = mem[:sp-1]
		case Addr:
			mem[sp-1].Data = mem[sp-1].Data.Addr()
		case Assign:
			mem[op[2]].Data.Set(mem[sp-1].Data)
			mem = mem[:sp-1]
		case Fassign:
			mem[fp+int(op[2])-1].Data.Set(mem[sp-1].Data)
			mem = mem[:sp-1]
		case Call:
			nip := int(mem[sp-1].Data.Int())
			mem = append(mem[:sp-1], ValueOf(ip+1), ValueOf(fp))
			ip = nip
			fp = sp + 1
			continue
		case Calli:
			mem = append(mem, ValueOf(ip+1), ValueOf(fp))
			fp = sp + 2
			ip = int(op[2])
			continue
		case CallX: // Should be made optional.
			l := int(op[2])
			in := make([]reflect.Value, l)
			for i := range in {
				in[i] = mem[sp-2-i].Data
			}
			f := mem[sp-1].Data
			mem = mem[:sp-l-1]
			for _, v := range f.Call(in) {
				mem = append(mem, Value{Data: v})
			}
		case Deref:
			mem[sp-1].Data = mem[sp-1].Data.Elem()
		case Dup:
			mem = append(mem, mem[int(op[2])])
		case New:
			mem[int(op[2])+fp-1] = NewValue(mem[int(op[3])].Type)
		case Equal:
			mem[sp-2] = ValueOf(mem[sp-2].Data.Equal(mem[sp-1].Data))
			mem = mem[:sp-1]
		case EqualSet:
			if mem[sp-2].Data.Equal(mem[sp-1].Data) {
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
			mem = append(mem, mem[int(op[2])+fp-1])
		case Field:
			mem[sp-1].Data = mem[sp-1].Data.FieldByIndex(slint(op[2:]))
		case Jump:
			ip += int(op[2])
			continue
		case JumpTrue:
			cond := mem[sp-1].Data.Bool()
			mem = mem[:sp-1]
			if cond {
				ip += int(op[2])
				continue
			}
		case JumpFalse:
			cond := mem[sp-1].Data.Bool()
			mem = mem[:sp-1]
			if !cond {
				ip += int(op[2])
				continue
			}
		case JumpSetTrue:
			cond := mem[sp-1].Data.Bool()
			if cond {
				ip += int(op[2])
				// Note that stack is not modified if cond is true
				continue
			}
			mem = mem[:sp-1]
		case JumpSetFalse:
			cond := mem[sp-1].Data.Bool()
			if !cond {
				ip += int(op[2])
				// Note that stack is not modified if cond is false
				continue
			}
			mem = mem[:sp-1]
		case Greater:
			mem[sp-2] = ValueOf(mem[sp-1].Data.Int() > mem[sp-2].Data.Int())
			mem = mem[:sp-1]
		case Lower:
			mem[sp-2] = ValueOf(mem[sp-1].Data.Int() < mem[sp-2].Data.Int())
			mem = mem[:sp-1]
		case Loweri:
			mem[sp-1] = ValueOf(mem[sp-1].Data.Int() < op[2])
		case Not:
			mem[sp-1] = ValueOf(!mem[sp-1].Data.Bool())
		case Pop:
			mem = mem[:sp-int(op[2])]
		case Push:
			// mem = append(mem, reflect.ValueOf(int(op[2])))
			mem = append(mem, NewValue(TypeOf(0)))
			mem[sp].Data.SetInt(op[2])
		case Grow:
			mem = append(mem, make([]Value, op[2])...)
		case Return:
			ip = int(mem[fp-2].Data.Int())
			ofp := fp
			fp = int(mem[fp-1].Data.Int())
			mem = append(mem[:ofp-int(op[2])-int(op[3])-1], mem[sp-int(op[2]):]...)
			continue
		case Sub:
			mem[sp-2] = ValueOf(int(mem[sp-1].Data.Int() - mem[sp-2].Data.Int()))
			mem = mem[:sp-1]
		case Subi:
			mem[sp-1] = ValueOf(int(mem[sp-1].Data.Int() - op[2]))
		case Index:
			mem[sp-2].Data = mem[sp-1].Data.Index(int(mem[sp-2].Data.Int()))
			mem = mem[:sp-1]
		case Vassign:
			mem[sp-1].Data.Set(mem[sp-2].Data)
			mem = mem[:sp-2]
		}
		ip++
	}
}

// PushCode adds instructions to the machine code.
func (m *Machine) PushCode(code ...[]int64) (p int) {
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
func (m *Machine) Pop() (v Value) {
	l := len(m.mem) - 1
	v = m.mem[l]
	m.mem = m.mem[:l]
	return v
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
	if l := len(m.code); l > 0 && m.code[l-1][1] == Exit {
		m.code = m.code[:l-1]
	}
}

// CodeString returns the string representation of a machine code instruction.
func CodeString(op []int64) string {
	switch len(op) {
	case 2:
		return strop[op[1]]
	case 3:
		return fmt.Sprintf("%s %d", strop[op[1]], op[2])
	case 4:
		return fmt.Sprintf("%s %d %d", strop[op[1]], op[2], op[3])
	}
	return ""
}

// Disassemble returns the code as a readable string.
func Disassemble(code [][]int64) (asm string) {
	for _, op := range code {
		asm += CodeString(op) + "\n"
	}
	return asm
}

func slint(a []int64) []int {
	r := make([]int, len(a))
	for i, v := range a {
		r[i] = int(v)
	}
	return r
}

// Vstring returns the string repreentation of a list of values.
func Vstring(lv []Value) string {
	s := "["
	for _, v := range lv {
		if s != "[" {
			s += " "
		}
		s += fmt.Sprintf("%v", v.Data)
	}
	return s + "]"
}
