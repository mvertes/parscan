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
	// instruction effect on stack: values consumed -- values produced
	Nop          = iota // --
	Add                 // n1 n2 -- sum ; sum = n1+n2
	Assign              // val -- ; mem[$1] = val
	Fassign             // val -- ; mem[$1] = val
	Vassign             // val dest -- ; dest.Set(val)
	Call                // f [a1 .. ai] -- [r1 .. rj] ; r1, ... = prog[f](a1, ...)
	Calli               // f [a1 .. ai] -- [r1 .. rj] ; r1, ... = prog[f](a1, ...)
	CallX               // f [a1 .. ai] -- [r1 .. rj] ; r1, ... = mem[f](a1, ...)
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
	Assign:       "Assign",
	Call:         "Call",
	Calli:        "Calli",
	CallX:        "CallX",
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
	Not:          "Not",
	Pop:          "Pop",
	Push:         "Push",
	Return:       "Return",
	Sub:          "Sub",
	Subi:         "Subi",
	Vassign:      "Vassign",
}

type Code [][]int64

// Machine represents a virtual machine.
type Machine struct {
	code   Code   // code to execute
	mem    []any  // memory, as a stack
	ip, fp int    // instruction and frame pointer
	ic     uint64 // instruction counter, incremented at each instruction executed
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
		log.Printf("ip:%-4d sp:%-4d fp:%-4d op:[%-12s %-4s %-4s] mem:%v\n", ip, sp, fp, strop[c[1]], op2, op3, mem)
	}

	for {
		sp = len(mem) // stack pointer
		trace()
		ic++
		switch op := code[ip]; op[1] {
		case Add:
			mem[sp-2] = mem[sp-2].(int) + mem[sp-1].(int)
			mem = mem[:sp-1]
		case Mul:
			mem[sp-2] = mem[sp-2].(int) * mem[sp-1].(int)
			mem = mem[:sp-1]
		case Assign:
			mem[op[2]] = mem[sp-1]
			mem = mem[:sp-1]
		case Fassign:
			mem[fp+int(op[2])-1] = mem[sp-1]
			mem = mem[:sp-1]
		case Call:
			nip := mem[sp-1].(int)
			mem = append(mem[:sp-1], ip+1, fp)
			ip = nip
			fp = sp + 1
			continue
		case Calli:
			mem = append(mem, ip+1, fp)
			fp = sp + 2
			ip += int(op[2])
			continue
		case CallX: // Should be made optional.
			l := int(op[2])
			in := make([]reflect.Value, l)
			for i := range in {
				in[i] = reflect.ValueOf(mem[sp-2-i])
			}
			f := reflect.ValueOf(mem[sp-1])
			mem = mem[:sp-l-1]
			for _, v := range f.Call(in) {
				mem = append(mem, v.Interface())
			}
		case Dup:
			mem = append(mem, mem[int(op[2])])
		case Equal:
			mem[sp-2] = mem[sp-2].(int) == mem[sp-1].(int)
			mem = mem[:sp-1]
		case EqualSet:
			if mem[sp-2].(int) == mem[sp-1].(int) {
				// If equal then lhs and rhs are popped, replaced by test result, as in Equal.
				mem[sp-2] = true
				mem = mem[:sp-1]
			} else {
				// If not equal then the lhs is let on stack for further processing.
				// This is used to simplify bytecode in case clauses of switch statments.
				mem[sp-1] = false
			}
		case Exit:
			return err
		case Fdup:
			mem = append(mem, mem[int(op[2])+fp-1])
		case Field:
			mem[sp-1] = mem[sp-1].(reflect.Value).FieldByIndex(slint(op[2:]))
		case Jump:
			ip += int(op[2])
			continue
		case JumpTrue:
			cond := mem[sp-1].(bool)
			mem = mem[:sp-1]
			if cond {
				ip += int(op[2])
				continue
			}
		case JumpFalse:
			cond := mem[sp-1].(bool)
			mem = mem[:sp-1]
			if !cond {
				ip += int(op[2])
				continue
			}
		case JumpSetTrue:
			cond := mem[sp-1].(bool)
			if cond {
				ip += int(op[2])
				// Note that stack is not modified if cond is true
				continue
			}
			mem = mem[:sp-1]
		case JumpSetFalse:
			cond := mem[sp-1].(bool)
			if !cond {
				ip += int(op[2])
				// Note that stack is not modified if cond is false
				continue
			}
			mem = mem[:sp-1]
		case Greater:
			mem[sp-2] = mem[sp-1].(int) > mem[sp-2].(int)
			mem = mem[:sp-1]
		case Lower:
			mem[sp-2] = mem[sp-1].(int) < mem[sp-2].(int)
			mem = mem[:sp-1]
		case Loweri:
			mem[sp-1] = mem[sp-1].(int) < int(op[2])
		case Not:
			mem[sp-1] = !mem[sp-1].(bool)
		case Pop:
			mem = mem[:sp-int(op[2])]
		case Push:
			mem = append(mem, int(op[2]))
		case Grow:
			mem = append(mem, make([]any, op[2])...)
		case Return:
			ip = mem[fp-2].(int)
			ofp := fp
			fp = mem[fp-1].(int)
			mem = append(mem[:ofp-int(op[2])-int(op[3])-1], mem[sp-int(op[2]):]...)
			continue
		case Sub:
			mem[sp-2] = mem[sp-1].(int) - mem[sp-2].(int)
			mem = mem[:sp-1]
		case Subi:
			mem[sp-1] = mem[sp-1].(int) - int(op[2])
		case Index:
			mem[sp-2] = mem[sp-1].(reflect.Value).Index(mem[sp-2].(int))
			mem = mem[:sp-1]
		case Vassign:
			mem[sp-1].(reflect.Value).Set(reflect.ValueOf(mem[sp-2]))
			mem = mem[:sp-2]
		}
		ip++
	}
}

func (m *Machine) PushCode(code ...[]int64) (p int) {
	p = len(m.code)
	m.code = append(m.code, code...)
	return p
}

func (m *Machine) SetIP(ip int)          { m.ip = ip }
func (m *Machine) Push(v ...any) (l int) { l = len(m.mem); m.mem = append(m.mem, v...); return l }
func (m *Machine) Pop() (v any)          { l := len(m.mem) - 1; v = m.mem[l]; m.mem = m.mem[:l]; return v }
func (m *Machine) Top() (v any) {
	if l := len(m.mem); l > 0 {
		v = m.mem[l-1]
	}
	return v
}

func (m *Machine) PopExit() {
	if l := len(m.code); l > 0 && m.code[l-1][1] == Exit {
		m.code = m.code[:l-1]
	}
}

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
