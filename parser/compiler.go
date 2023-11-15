package parser

import (
	"fmt"
	"log"
	"reflect"
	"strconv"

	"github.com/gnolang/parscan/lang"
	"github.com/gnolang/parscan/scanner"
	"github.com/gnolang/parscan/vm"
)

type Compiler struct {
	*Parser
	vm.Code       // produced code, to fill VM with
	Data    []any // produced data, will be at the bottom of VM stack
	Entry   int   // offset in Code to start execution from (skip function defintions)

	strings map[string]int // locations of strings in Data
}

func NewCompiler(scanner *scanner.Scanner) *Compiler {
	return &Compiler{
		Parser:  &Parser{Scanner: scanner, symbols: initUniverse(), framelen: map[string]int{}, labelCount: map[string]int{}},
		Entry:   -1,
		strings: map[string]int{},
	}
}

func (c *Compiler) AddSym(name string, value any) int {
	p := len(c.Data)
	c.Data = append(c.Data, value)
	c.Parser.AddSym(p, name, value)
	return p
}

func (c *Compiler) Codegen(tokens Tokens) (err error) {
	fixList := Tokens{}
	log.Println("Codegen tokens:", tokens)

	emit := func(op ...int64) { c.Code = append(c.Code, op) }

	for i, t := range tokens {
		switch t.Id {
		case lang.Int:
			n, err := strconv.Atoi(t.Str)
			if err != nil {
				return err
			}
			emit(int64(t.Pos), vm.Push, int64(n))

		case lang.String:
			s := t.Block()
			i, ok := c.strings[s]
			if !ok {
				i = len(c.Data)
				c.Data = append(c.Data, s)
				c.strings[s] = i
			}
			emit(int64(t.Pos), vm.Dup, int64(i))

		case lang.Add:
			emit(int64(t.Pos), vm.Add)

		case lang.Mul:
			emit(int64(t.Pos), vm.Mul)

		case lang.Sub:
			emit(int64(t.Pos), vm.Sub)

		case lang.Greater:
			emit(int64(t.Pos), vm.Greater)

		case lang.Less:
			emit(int64(t.Pos), vm.Lower)

		case lang.Call:
			emit(int64(t.Pos), vm.Call)

		case lang.CallX:
			emit(int64(t.Pos), vm.CallX, int64(t.Beg))

		case lang.Grow:
			emit(int64(t.Pos), vm.Grow, int64(t.Beg))

		case lang.Define:
			// TODO: support assignment to local, composite objects
			st := tokens[i-1]
			l := len(c.Data)
			c.Data = append(c.Data, nil)
			// TODO: symbol should be added at parse, not here.
			c.addSym(l, st.Str, nil, symVar, nil, false)
			emit(int64(st.Pos), vm.Assign, int64(l))

		case lang.Assign:
			st := tokens[i-1]
			s, ok := c.symbols[st.Str]
			if !ok {
				return fmt.Errorf("symbol not found: %s", st.Str)
			}
			if s.local {
				emit(int64(st.Pos), vm.Fassign, int64(s.index))
			} else {
				if s.index == unsetAddr {
					s.index = len(c.Data)
					c.Data = append(c.Data, s.value)
				}
				emit(int64(st.Pos), vm.Assign, int64(s.index))
			}

		case lang.Equal:
			emit(int64(t.Pos), vm.Equal)

		case lang.EqualSet:
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
				s.value = lc
				if s.kind == symFunc {
					// label is a function entry point, register its code address in data.
					s.index = len(c.Data)
					c.Data = append(c.Data, lc)
				} else {
					c.Data[s.index] = lc
				}
			} else {
				c.symbols[t.Str] = &symbol{kind: symLabel, value: lc}
			}

		case lang.JumpFalse:
			label := t.Str[10:]
			i := 0
			if s, ok := c.symbols[label]; !ok {
				// t.Beg contains the position in code which needs to be fixed.
				t.Beg = len(c.Code)
				fixList = append(fixList, t)
			} else {
				i = s.value.(int) - len(c.Code)
			}
			emit(int64(t.Pos), vm.JumpFalse, int64(i))

		case lang.JumpSetFalse:
			label := t.Str[13:]
			i := 0
			if s, ok := c.symbols[label]; !ok {
				// t.Beg contains the position in code which needs to be fixed.
				t.Beg = len(c.Code)
				fixList = append(fixList, t)
			} else {
				i = s.value.(int) - len(c.Code)
			}
			emit(int64(t.Pos), vm.JumpSetFalse, int64(i))

		case lang.JumpSetTrue:
			label := t.Str[12:]
			i := 0
			if s, ok := c.symbols[label]; !ok {
				// t.Beg contains the position in code which needs to be fixed.
				t.Beg = len(c.Code)
				fixList = append(fixList, t)
			} else {
				i = s.value.(int) - len(c.Code)
			}
			emit(int64(t.Pos), vm.JumpSetTrue, int64(i))

		case lang.Goto:
			label := t.Str[5:]
			i := 0
			if s, ok := c.symbols[label]; !ok {
				t.Beg = len(c.Code)
				fixList = append(fixList, t)
			} else {
				i = s.value.(int) - len(c.Code)
			}
			emit(int64(t.Pos), vm.Jump, int64(i))

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
		c.Code[t.Beg][2] = int64(s.value.(int) - t.Beg)

	}
	return err
}

func (c *Compiler) PrintCode() {
	labels := map[int][]string{} // labels indexed by code location
	data := map[int]string{}     // data indexed by frame location

	for name, sym := range c.symbols {
		if sym.kind == symLabel || sym.kind == symFunc {
			i := sym.value.(int)
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
		fmt.Printf("%4d %T %v %v\n", i, d, d, dict[i])
	}
}

func (c *Compiler) NumIn(i int) (int, bool) {
	t := reflect.TypeOf(c.Data[i])
	if t.Kind() == reflect.Func {
		return t.NumIn(), t.IsVariadic()
	}
	return -1, false
}
