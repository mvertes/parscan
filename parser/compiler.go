package parser

import (
	"fmt"
	"log"
	"os"
	"reflect"
	"strconv"

	"github.com/mvertes/parscan/lang"
	"github.com/mvertes/parscan/scanner"
	"github.com/mvertes/parscan/vm"
)

// Compiler represents the state of a compiler.
type Compiler struct {
	*Parser
	vm.Code            // produced code, to fill VM with
	Data    []vm.Value // produced data, will be at the bottom of VM stack
	Entry   int        // offset in Code to start execution from (skip function defintions)

	strings map[string]int // locations of strings in Data
}

// NewCompiler returns a new compiler state for a given scanner.
func NewCompiler(scanner *scanner.Scanner) *Compiler {
	return &Compiler{
		Parser:  &Parser{Scanner: scanner, symbols: initUniverse(), framelen: map[string]int{}, labelCount: map[string]int{}},
		Entry:   -1,
		strings: map[string]int{},
	}
}

// AddSym adds a new named value to the compiler symbol table, and returns its index in memory.
func (c *Compiler) AddSym(name string, value vm.Value) int {
	p := len(c.Data)
	c.Data = append(c.Data, value)
	c.Parser.AddSym(p, name, value)
	return p
}

// Codegen generates vm code from parsed tokens.
func (c *Compiler) Codegen(tokens Tokens) (err error) {
	log.Println("Codegen tokens:", tokens)
	fixList := Tokens{}  // list of tokens to fix after we gathered all necessary information
	stack := []*symbol{} // for symbolic evaluation, type checking, etc

	emit := func(op ...int64) { c.Code = append(c.Code, op) }
	push := func(s *symbol) { stack = append(stack, s) }
	pop := func() *symbol { l := len(stack) - 1; s := stack[l]; stack = stack[:l]; return s }

	for i, t := range tokens {
		switch t.Tok {
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
			// Unary '+' is idempotent. Nothing to do.

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
			s := pop()
			if s.kind != symValue {
				typ := s.Type
				// TODO: pop input types (careful with variadic function).
				for i := 0; i < typ.Rtype.NumOut(); i++ {
					push(&symbol{Type: typ.Out(i)})
				}
				emit(int64(t.Pos), vm.Call)
				break
			}
			push(s)
			fallthrough // A symValue must be called through callX.

		case lang.CallX:
			rtyp := pop().value.Data.Type()
			// TODO: pop input types (careful with variadic function).
			for i := 0; i < rtyp.NumOut(); i++ {
				push(&symbol{Type: &vm.Type{Rtype: rtyp.Out(i)}})
			}
			emit(int64(t.Pos), vm.CallX, int64(t.Beg))

		case lang.Composite:
			log.Println("COMPOSITE")

		case lang.Grow:
			emit(int64(t.Pos), vm.Grow, int64(t.Beg))

		case lang.Define:
			// TODO: support assignment to local, composite objects.
			st := tokens[i-1]
			l := len(c.Data)
			typ := pop().Type
			v := vm.NewValue(typ)
			c.addSym(l, st.Str, v, symVar, typ, false)
			c.Data = append(c.Data, v)
			emit(int64(st.Pos), vm.Assign, int64(l))

		case lang.Assign:
			st := tokens[i-1]
			if st.Tok == lang.Period || st.Tok == lang.Index {
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
				switch t1 := tokens[i+1]; t1.Tok {
				case lang.Define, lang.Assign, lang.Colon:
					continue
				}
			}
			s, ok := c.symbols[t.Str]
			if !ok {
				return fmt.Errorf("symbol not found: %s", t.Str)
			}
			push(s)
			if s.kind == symPkg {
				break
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
			var i int64
			if s, ok := c.symbols[t.Str]; !ok {
				// t.Beg contains the position in code which needs to be fixed.
				t.Beg = len(c.Code)
				fixList = append(fixList, t)
			} else {
				i = s.value.Data.Int() - int64(len(c.Code))
			}
			emit(int64(t.Pos), vm.JumpFalse, i)

		case lang.JumpSetFalse:
			var i int64
			if s, ok := c.symbols[t.Str]; !ok {
				// t.Beg contains the position in code which needs to be fixed.
				t.Beg = len(c.Code)
				fixList = append(fixList, t)
			} else {
				i = s.value.Data.Int() - int64(len(c.Code))
			}
			emit(int64(t.Pos), vm.JumpSetFalse, i)

		case lang.JumpSetTrue:
			var i int64
			if s, ok := c.symbols[t.Str]; !ok {
				// t.Beg contains the position in code which needs to be fixed.
				t.Beg = len(c.Code)
				fixList = append(fixList, t)
			} else {
				i = s.value.Data.Int() - int64(len(c.Code))
			}
			emit(int64(t.Pos), vm.JumpSetTrue, int64(i))

		case lang.Goto:
			var i int64
			if s, ok := c.symbols[t.Str]; !ok {
				t.Beg = len(c.Code)
				fixList = append(fixList, t)
			} else {
				i = s.value.Data.Int() - int64(len(c.Code))
			}
			emit(int64(t.Pos), vm.Jump, i)

		case lang.Period:
			s := pop()
			switch s.kind {
			case symPkg:
				p, ok := packages[s.pkgPath]
				if !ok {
					return fmt.Errorf("package not found: %s", s.pkgPath)
				}
				v, ok := p[t.Str[1:]]
				if !ok {
					return fmt.Errorf("symbol not found in package %s: %s", s.pkgPath, t.Str[1:])
				}
				name := s.pkgPath + t.Str
				var l int
				sym, _, ok := c.getSym(name, "")
				if ok {
					l = sym.index
				} else {
					l = len(c.Data)
					c.Data = append(c.Data, v)
					c.addSym(l, name, v, symValue, v.Type, false)
					sym = c.symbols[name]
				}
				push(sym)
				emit(int64(t.Pos), vm.Dup, int64(l))
			default:
				if f, ok := s.Type.Rtype.FieldByName("X" + t.Str[1:]); ok {
					emit(append([]int64{int64(t.Pos), vm.Field}, slint64(f.Index)...)...)
					break
				}
				return fmt.Errorf("field or method not found: %s", t.Str[1:])
			}

		case lang.Return:
			emit(int64(t.Pos), vm.Return, int64(t.Beg), int64(t.End))

		default:
			return fmt.Errorf("Codegen: unsupported token %v", t)
		}
	}

	// Finally we fix unresolved labels for jump destinations.
	for _, t := range fixList {
		s, ok := c.symbols[t.Str]
		if !ok {
			return fmt.Errorf("label not found: %q", t.Str)
		}
		c.Code[t.Beg][2] = s.value.Data.Int() - int64(t.Beg)

	}
	return err
}

func arithmeticOpType(s1, _ *symbol) *vm.Type { return symtype(s1) }
func booleanOpType(_, _ *symbol) *vm.Type     { return vm.TypeOf(true) }

// PrintCode pretty prints the generated code in compiler.
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

	fmt.Fprintln(os.Stderr, "# Code:")
	for i, l := range c.Code {
		for _, label := range labels[i] {
			fmt.Fprintln(os.Stderr, label+":")
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
		fmt.Fprintf(os.Stderr, "%4d %-14v %v\n", i, vm.CodeString(l), extra)
	}

	for _, label := range labels[len(c.Code)] {
		fmt.Fprintln(os.Stderr, label+":")
	}
	fmt.Fprintln(os.Stderr, "# End code")
}

type entry struct {
	name string
	*symbol
}

func (e entry) String() string {
	if e.symbol != nil {
		return fmt.Sprintf("name: %s,local: %t, i: %d, k: %d, t: %s, v: %v",
			e.name,
			e.symbol.local,
			e.symbol.index,
			e.symbol.kind,
			e.symbol.Type,
			e.symbol.value,
		)
	}

	return e.name
}

// PrintData pretty prints the generated global data symbols in compiler.
func (c *Compiler) PrintData() {
	dict := c.symbolsByIndex()

	fmt.Fprintln(os.Stderr, "# Data:")
	for i, d := range c.Data {
		fmt.Fprintf(os.Stderr, "%4d %T %v %v\n", i, d.Data.Interface(), d.Data, dict[i])
	}
}

func (c *Compiler) symbolsByIndex() map[int]entry {
	dict := map[int]entry{}
	for name, sym := range c.symbols {
		if sym.index == unsetAddr {
			continue
		}
		dict[sym.index] = entry{name, sym}
	}

	return dict
}

// Dump represents the state of a data dump.
type Dump struct {
	Values []*DumpValue
}

// DumpValue is a value of a dump state.
type DumpValue struct {
	Index int
	Name  string
	Kind  int
	Type  string
	Value any
}

// Dump creates a snapshot of the execution state of global variables.
// This method is specifically implemented in the Compiler to minimize the coupling between
// the dump format and other components. By situating the dump logic in the Compiler,
// it relies solely on the program being executed and the indexing algorithm used for ordering variables
// (currently, this is an integer that corresponds to the order of variables in the program).
// This design choice allows the Virtual Machine (VM) to evolve its memory management strategies
// without compromising backward compatibility with dumps generated by previous versions.
func (c *Compiler) Dump() *Dump {
	var dv []*DumpValue
	dict := c.symbolsByIndex()
	for i, d := range c.Data {
		e := dict[i]
		dv = append(dv, &DumpValue{
			Index: e.index,
			Name:  e.name,
			Kind:  int(e.kind),
			Type:  e.Type.Name,
			Value: d.Data.Interface(),
		})
	}

	return &Dump{Values: dv}
}

// ApplyDump sets previously saved dump, restoring the state of global variables.
func (c *Compiler) ApplyDump(d *Dump) error {
	dict := c.symbolsByIndex()
	for _, dv := range d.Values {
		// do all the checks to be sure we are applying the correct values
		e, ok := dict[dv.Index]
		if !ok {
			return fmt.Errorf("entry not found on index %d", dv.Index)
		}

		if dv.Name != e.name ||
			dv.Type != e.Type.Name ||
			dv.Kind != int(e.kind) {
			return fmt.Errorf("entry with index %d does not match with provided entry. "+
				"dumpValue: %s, %s, %d. memoryValue: %s, %s, %d",
				dv.Index,
				dv.Name, dv.Type, dv.Kind,
				e.name, e.Type, e.kind)
		}

		if dv.Index >= len(c.Data) {
			return fmt.Errorf("index (%d) bigger than memory (%d)", dv.Index, len(c.Data))
		}

		if !c.Data[dv.Index].Data.CanSet() {
			return fmt.Errorf("value %v cannot be set", dv.Value)
		}

		c.Data[dv.Index].Data.Set(reflect.ValueOf(dv.Value))
	}

	return nil
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
