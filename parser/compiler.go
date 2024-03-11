package parser

import (
	"fmt"
	"log"
	"strconv"

	"github.com/mvertes/parscan/lang"
	"github.com/mvertes/parscan/scanner"
	"github.com/mvertes/parscan/vm"
)

type Compiler struct {
	*Parser
	vm.Code            // produced code, to fill VM with
	Data    []vm.Value // produced data, will be at the bottom of VM stack
	Entry   int        // offset in Code to start execution from (skip function defintions)

	strings map[string]int // locations of strings in Data
}

func NewCompiler(scanner *scanner.Scanner) *Compiler {
	return &Compiler{
		Parser:  &Parser{Scanner: scanner, symbols: initUniverse(), framelen: map[string]int{}, labelCount: map[string]int{}},
		Entry:   -1,
		strings: map[string]int{},
	}
}

func (c *Compiler) AddSym(name string, value vm.Value) int {
	p := len(c.Data)
	c.Data = append(c.Data, value)
	c.Parser.AddSym(p, name, value)
	return p
}

func (c *Compiler) Codegen(tokens Tokens) (err error) {
	log.Println("Codegen tokens:", tokens)
	fixList := Tokens{}  // list of tokens to fix after we gathered all necessary information
	stack := []*symbol{} // for symbolic evaluation, type checking, etc

	emit := func(op ...int64) { c.Code = append(c.Code, op) }
	push := func(s *symbol) { stack = append(stack, s) }
	pop := func() *symbol { l := len(stack) - 1; s := stack[l]; stack = stack[:l]; return s }

	for i, t := range tokens {
		switch t.Id {
		case lang.Int:
			n, err := strconv.Atoi(t.Str)
			if err != nil {
				return err
			}
			push(&symbol{kind: symConst, value: vm.ValueOf(n), Type: vm.TypeOf(0)})
			emit(int64(t.Pos), vm.Push, int64(n))

		case lang.String:
			s := t.Block()
			i, ok := c.strings[s]
			if !ok {
				i = len(c.Data)
				c.Data = append(c.Data, vm.ValueOf(s))
				c.strings[s] = i
			}
			push(&symbol{kind: symConst, value: vm.ValueOf(s)})
			emit(int64(t.Pos), vm.Dup, int64(i))

		case lang.Add:
			push(&symbol{Type: arithmeticOpType(pop(), pop())})
			emit(int64(t.Pos), vm.Add)

		case lang.Mul:
			push(&symbol{Type: arithmeticOpType(pop(), pop())})
			emit(int64(t.Pos), vm.Mul)

		case lang.Sub:
			push(&symbol{Type: arithmeticOpType(pop(), pop())})
			emit(int64(t.Pos), vm.Sub)

		case lang.Minus:
			emit(int64(t.Pos), vm.Push, 0)
			emit(int64(t.Pos), vm.Sub)

		case lang.Not:
			emit(int64(t.Pos), vm.Not)

		case lang.Plus:
			// Nothing to do.

		case lang.Addr:
			push(&symbol{Type: vm.PointerTo(pop().Type)})
			emit(int64(t.Pos), vm.Addr)

		case lang.Deref:
			push(&symbol{Type: pop().Type.Elem()})
			emit(int64(t.Pos), vm.Deref)

		case lang.Index:
			push(&symbol{Type: pop().Type.Elem()})
			emit(int64(t.Pos), vm.Index)

		case lang.Greater:
			push(&symbol{Type: booleanOpType(pop(), pop())})
			emit(int64(t.Pos), vm.Greater)

		case lang.Less:
			push(&symbol{Type: booleanOpType(pop(), pop())})
			emit(int64(t.Pos), vm.Lower)

		case lang.Call:
			typ := pop().Type
			// TODO: pop input types (careful with variadic function)
			for i := 0; i < typ.Rtype.NumOut(); i++ {
				push(&symbol{Type: typ.Out(i)})
			}
			emit(int64(t.Pos), vm.Call)

		case lang.CallX:
			rtyp := pop().value.Data.Type()
			// TODO: pop input types (careful with variadic function)
			for i := 0; i < rtyp.NumOut(); i++ {
				push(&symbol{Type: &vm.Type{Rtype: rtyp.Out(i)}})
			}
			emit(int64(t.Pos), vm.CallX, int64(t.Beg))

		case lang.Grow:
			emit(int64(t.Pos), vm.Grow, int64(t.Beg))

		case lang.Define:
			// TODO: support assignment to local, composite objects
			st := tokens[i-1]
			l := len(c.Data)
			typ := pop().Type
			v := vm.NewValue(typ)
			c.addSym(l, st.Str, v, symVar, typ, false)
			c.Data = append(c.Data, v)
			emit(int64(st.Pos), vm.Assign, int64(l))

		case lang.Assign:
			st := tokens[i-1]
			if st.Id == lang.Period || st.Id == lang.Index {
				emit(int64(t.Pos), vm.Vassign)
				break
			}
			s, ok := c.symbols[st.Str]
			if !ok {
				return fmt.Errorf("symbol not found: %s", st.Str)
			}
			typ := pop().Type
			if s.Type == nil {
				s.Type = typ
				s.value = vm.NewValue(typ)
			}
			if s.local {
				if !s.used {
					emit(int64(st.Pos), vm.New, int64(s.index), int64(c.typeSym(s.Type).index))
					s.used = true
				}
				emit(int64(st.Pos), vm.Fassign, int64(s.index))
				break
			}
			if s.index == unsetAddr {
				s.index = len(c.Data)
				c.Data = append(c.Data, s.value)
			}
			emit(int64(st.Pos), vm.Assign, int64(s.index))

		case lang.Equal:
			push(&symbol{Type: booleanOpType(pop(), pop())})
			emit(int64(t.Pos), vm.Equal)

		case lang.EqualSet:
			push(&symbol{Type: booleanOpType(pop(), pop())})
			emit(int64(t.Pos), vm.EqualSet)

		case lang.Ident:
			if i < len(tokens)-1 {
				switch t1 := tokens[i+1]; t1.Id {
				case lang.Define, lang.Assign, lang.Colon:
					continue
				}
			}
			s, ok := c.symbols[t.Str]
			if !ok {
				return fmt.Errorf("symbol not found: %s", t.Str)
			}
			push(s)
			if s.local {
				emit(int64(t.Pos), vm.Fdup, int64(s.index))
			} else {
				if s.index == unsetAddr {
					s.index = len(c.Data)
					c.Data = append(c.Data, s.value)
				}
				emit(int64(t.Pos), vm.Dup, int64(s.index))
			}

		case lang.Label:
			lc := len(c.Code)
			s, ok := c.symbols[t.Str]
			if ok {
				s.value = vm.ValueOf(lc)
				if s.kind == symFunc {
					// label is a function entry point, register its code address in data.
					s.index = len(c.Data)
					c.Data = append(c.Data, s.value)
				} else {
					c.Data[s.index] = s.value
				}
			} else {
				c.symbols[t.Str] = &symbol{kind: symLabel, value: vm.ValueOf(lc)}
			}

		case lang.JumpFalse:
			label := t.Str[10:]
			var i int64
			if s, ok := c.symbols[label]; !ok {
				// t.Beg contains the position in code which needs to be fixed.
				t.Beg = len(c.Code)
				fixList = append(fixList, t)
			} else {
				i = s.value.Data.Int() - int64(len(c.Code))
			}
			emit(int64(t.Pos), vm.JumpFalse, i)

		case lang.JumpSetFalse:
			label := t.Str[13:]
			var i int64
			if s, ok := c.symbols[label]; !ok {
				// t.Beg contains the position in code which needs to be fixed.
				t.Beg = len(c.Code)
				fixList = append(fixList, t)
			} else {
				i = s.value.Data.Int() - int64(len(c.Code))
			}
			emit(int64(t.Pos), vm.JumpSetFalse, i)

		case lang.JumpSetTrue:
			label := t.Str[12:]
			var i int64
			if s, ok := c.symbols[label]; !ok {
				// t.Beg contains the position in code which needs to be fixed.
				t.Beg = len(c.Code)
				fixList = append(fixList, t)
			} else {
				i = s.value.Data.Int() - int64(len(c.Code))
			}
			emit(int64(t.Pos), vm.JumpSetTrue, int64(i))

		case lang.Goto:
			label := t.Str[5:]
			var i int64
			if s, ok := c.symbols[label]; !ok {
				t.Beg = len(c.Code)
				fixList = append(fixList, t)
			} else {
				i = s.value.Data.Int() - int64(len(c.Code))
			}
			emit(int64(t.Pos), vm.Jump, i)

		case lang.Period:
			if f, ok := pop().Type.Rtype.FieldByName("X" + t.Str[1:]); ok {
				emit(append([]int64{int64(t.Pos), vm.Field}, slint64(f.Index)...)...)
				break
			}
			return fmt.Errorf("field or method not found: %s", t.Str[1:])

		case lang.Return:
			emit(int64(t.Pos), vm.Return, int64(t.Beg), int64(t.End))

		default:
			return fmt.Errorf("Codegen: unsupported token %v", t)
		}
	}

	// Finally we fix unresolved labels for jump destinations.
	for _, t := range fixList {
		var label string
		// TODO: this could be simplified.
		switch t.Id {
		case lang.Goto:
			label = t.Str[5:]
		case lang.JumpFalse:
			label = t.Str[10:]
		case lang.JumpSetFalse:
			label = t.Str[13:]
		case lang.JumpSetTrue:
			label = t.Str[12:]
		}
		s, ok := c.symbols[label]
		if !ok {
			return fmt.Errorf("label not found: %q", label)
		}
		c.Code[t.Beg][2] = s.value.Data.Int() - int64(t.Beg)

	}
	return err
}

func arithmeticOpType(s1, s2 *symbol) *vm.Type { return symtype(s1) }
func booleanOpType(s1, s2 *symbol) *vm.Type    { return vm.TypeOf(true) }

func (c *Compiler) PrintCode() {
	labels := map[int][]string{} // labels indexed by code location
	data := map[int]string{}     // data indexed by frame location

	for name, sym := range c.symbols {
		if sym.kind == symLabel || sym.kind == symFunc {
			i := int(sym.value.Data.Int())
			labels[i] = append(labels[i], name)
		}
		if sym.used {
			data[sym.index] = name
		}
	}

	fmt.Println("# Code:")
	for i, l := range c.Code {
		for _, label := range labels[i] {
			fmt.Println(label + ":")
		}
		extra := ""
		switch l[1] {
		case vm.Jump, vm.JumpFalse, vm.JumpTrue, vm.JumpSetFalse, vm.JumpSetTrue, vm.Calli:
			if d, ok := labels[i+(int)(l[2])]; ok {
				extra = "// " + d[0]
			}
		case vm.Dup, vm.Assign, vm.Fdup, vm.Fassign:
			if d, ok := data[int(l[2])]; ok {
				extra = "// " + d
			}
		}
		fmt.Printf("%4d %-14v %v\n", i, vm.CodeString(l), extra)
	}

	for _, label := range labels[len(c.Code)] {
		fmt.Println(label + ":")
	}
	fmt.Println("# End code")
}

type entry struct {
	name string
	*symbol
}

func (c *Compiler) PrintData() {
	dict := map[int]entry{}
	for name, sym := range c.symbols {
		if !sym.used || sym.local || sym.kind == symLabel {
			continue
		}
		dict[sym.index] = entry{name, sym}
	}
	fmt.Println("# Data:")
	for i, d := range c.Data {
		fmt.Printf("%4d %T %v %v\n", i, d.Data.Interface(), d.Data, dict[i])
	}
}

func (c *Compiler) typeSym(t *vm.Type) *symbol {
	tsym, ok := c.symbols[t.Rtype.String()]
	if !ok {
		tsym = &symbol{index: unsetAddr, kind: symType, Type: t}
		c.symbols[t.Rtype.String()] = tsym
	}
	if tsym.index == unsetAddr {
		tsym.index = len(c.Data)
		c.Data = append(c.Data, vm.NewValue(t))
	}
	return tsym
}

func slint64(a []int) []int64 {
	r := make([]int64, len(a))
	for i, v := range a {
		r[i] = int64(v)
	}
	return r
}
