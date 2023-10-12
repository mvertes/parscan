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
	Code  [][]int64 // produced code, to fill VM with
	Data  []any     // produced data, will be at the bottom of VM stack
	Entry int       // offset in Code to start execution from (skip function defintions)

	strings map[string]int // locations of strings in Data
}

func NewCompiler(scanner *scanner.Scanner) *Compiler {
	return &Compiler{
		Parser:  &Parser{Scanner: scanner, symbols: initUniverse(), labelCount: map[string]int{}},
		Entry:   -1,
		strings: map[string]int{},
	}
}

func (c *Compiler) Emit(op ...int64) int {
	op = append([]int64{}, op...)
	l := len(c.Code)
	c.Code = append(c.Code, op)
	return l
}

func (c *Compiler) AddSym(name string, value any) int {
	p := len(c.Data)
	c.Data = append(c.Data, value)
	c.Parser.AddSym(p, name, value)
	return p
}

func (c *Compiler) Codegen(tokens Tokens) (err error) {
	fixList := Tokens{}
	function := []string{""}

	log.Println("Codegen tokens:", tokens)

	for i, t := range tokens {
		scope := function[len(function)-1]
		switch t.Id {
		case lang.Int:
			n, err := strconv.Atoi(t.Str)
			if err != nil {
				return err
			}
			c.Emit(int64(t.Pos), vm.Push, int64(n))

		case lang.String:
			s := t.Block()
			i, ok := c.strings[s]
			if !ok {
				i = len(c.Data)
				c.Data = append(c.Data, s)
				c.strings[s] = i
			}
			c.Emit(int64(t.Pos), vm.Dup, int64(i))

		case lang.Add:
			c.Emit(int64(t.Pos), vm.Add)

		case lang.Sub:
			c.Emit(int64(t.Pos), vm.Sub)

		case lang.Less:
			c.Emit(int64(t.Pos), vm.Lower)

		case lang.Call:
			c.Emit(int64(t.Pos), vm.Call)

		case lang.CallX:
			c.Emit(int64(t.Pos), vm.CallX, int64(t.Beg))

		case lang.Define:
			// TODO: support assignment to local, composite objects
			st := tokens[i-1]
			l := len(c.Data)
			c.Data = append(c.Data, nil)
			c.addSym(l, st.Str, nil, symVar, nil, false)
			c.Emit(int64(st.Pos), vm.Assign, int64(l))

		case lang.Enter:
			// TODO: merge with label ?
			function = append(function, t.Str[6:])

		case lang.Exit:
			function = function[:len(function)-1]

		case lang.Assign:
			st := tokens[i-1]
			s, _, ok := c.getSym(st.Str, scope)
			if !ok {
				return fmt.Errorf("symbol not found: %s", st.Str)
			}
			if s.local {
				c.Emit(int64(st.Pos), vm.Fassign, int64(s.index))
			} else {
				c.Emit(int64(st.Pos), vm.Assign, int64(s.index))
			}

		case lang.Equal:
			c.Emit(int64(t.Pos), vm.Equal)

		case lang.Ident:
			if i < len(tokens)-1 {
				switch t1 := tokens[i+1]; t1.Id {
				case lang.Define, lang.Assign, lang.Colon:
					continue
				}
			}
			s, _, ok := c.getSym(t.Str, scope)
			if !ok {
				return fmt.Errorf("symbol not found: %s", t.Str)
			}
			if s.local {
				c.Emit(int64(t.Pos), vm.Fdup, int64(s.index))
			} else {
				c.Emit(int64(t.Pos), vm.Dup, int64(s.index))
			}

		case lang.Label:
			// If the label is a function, the symbol already exists
			s, _, ok := c.getSym(t.Str, scope)
			lc := len(c.Code)
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
				ld := len(c.Data)
				c.Data = append(c.Data, lc)
				c.addSym(ld, t.Str, lc, symLabel, nil, false)
				//c.symbols[t.Str] = &symbol{kind: symLabel, value: lc}
			}

		case lang.JumpFalse:
			label := t.Str[10:]
			i := 0
			if s, _, ok := c.getSym(label, scope); !ok {
				// t.Beg contains the position in code which needs to be fixed.
				t.Beg = len(c.Code)
				fixList = append(fixList, t)
			} else {
				i = s.value.(int) - len(c.Code)
			}
			c.Emit(int64(t.Pos), vm.JumpFalse, int64(i))

		case lang.Goto:
			label := t.Str[5:]
			i := 0
			if s, _, ok := c.getSym(label, scope); !ok {
				t.Beg = len(c.Code)
				fixList = append(fixList, t)
			} else {
				i = s.value.(int) - len(c.Code)
			}
			c.Emit(int64(t.Pos), vm.Jump, int64(i))

		case lang.Return:
			c.Emit(int64(t.Pos), vm.Return, int64(t.Beg), int64(t.End))

		default:
			return fmt.Errorf("Codegen: unsupported token %v", t)
		}
	}

	// Finally we fix unresolved labels for jump destinations.
	for _, t := range fixList {
		var label string
		switch t.Id {
		case lang.Goto:
			label = t.Str[5:]
		case lang.JumpFalse:
			label = t.Str[10:]
		}
		s, _, ok := c.getSym(label, "")
		if !ok {
			return fmt.Errorf("label not found: %q", label)
		}
		c.Code[t.Beg][2] = int64(s.value.(int) - t.Beg)

	}
	return err
}

func (c *Compiler) PrintCode() {
	labels := map[int]string{}
	for name, sym := range c.symbols {
		if sym.kind == symLabel || sym.kind == symFunc {
			labels[sym.value.(int)] = name
		}
	}
	fmt.Println("# Code:")
	for i, l := range c.Code {
		if label, ok := labels[i]; ok {
			fmt.Println(label + ":")
		}
		extra := ""
		switch l[1] {
		case vm.Jump, vm.JumpFalse, vm.JumpTrue, vm.Calli:
			if d, ok := labels[i+(int)(l[2])]; ok {
				extra = "// " + d
			}
		}
		fmt.Printf("%4d %-14v %v\n", i, vm.CodeString(l), extra)
	}
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
