package parser

import (
	"fmt"
	"log"
	"os"
	"path"
	"reflect"
	"runtime"
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
		Parser:  NewParser(scanner, true),
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

	emit := func(t scanner.Token, op vm.Op, arg ...int) {
		_, file, line, _ := runtime.Caller(1)
		fmt.Fprintf(os.Stderr, "%s:%d: %v emit %v %v\n", path.Base(file), line, t, op, arg)
		c.Code = append(c.Code, vm.Instruction{Pos: vm.Pos(t.Pos), Op: op, Arg: arg})
	}
	push := func(s *symbol) { stack = append(stack, s) }
	pop := func() *symbol { l := len(stack) - 1; s := stack[l]; stack = stack[:l]; return s }

	for i, t := range tokens {
		switch t.Tok {
		case lang.Int:
			n, err := strconv.Atoi(t.Str)
			if err != nil {
				return err
			}
			push(&symbol{kind: symConst, value: vm.ValueOf(n), typ: vm.TypeOf(0)})
			emit(t, vm.Push, n)

		case lang.String:
			s := t.Block()
			v := vm.Value{Data: reflect.ValueOf(s), Type: vm.TypeOf(s)}
			i, ok := c.strings[s]
			if !ok {
				i = len(c.Data)
				c.Data = append(c.Data, v)
				c.strings[s] = i
			}
			push(&symbol{kind: symConst, value: v})
			emit(t, vm.Dup, i)

		case lang.Add:
			push(&symbol{typ: arithmeticOpType(pop(), pop())})
			emit(t, vm.Add)

		case lang.Mul:
			push(&symbol{typ: arithmeticOpType(pop(), pop())})
			emit(t, vm.Mul)

		case lang.Sub:
			push(&symbol{typ: arithmeticOpType(pop(), pop())})
			emit(t, vm.Sub)

		case lang.Minus:
			emit(t, vm.Push, 0)
			emit(t, vm.Sub)

		case lang.Not:
			emit(t, vm.Not)

		case lang.Plus:
			// Unary '+' is idempotent. Nothing to do.

		case lang.Addr:
			push(&symbol{typ: vm.PointerTo(pop().typ)})
			emit(t, vm.Addr)

		case lang.Deref:
			push(&symbol{typ: pop().typ.Elem()})
			emit(t, vm.Deref)

		case lang.Index:
			push(&symbol{typ: pop().typ.Elem()})
			emit(t, vm.Index)

		case lang.Greater:
			push(&symbol{typ: booleanOpType(pop(), pop())})
			emit(t, vm.Greater)

		case lang.Less:
			push(&symbol{typ: booleanOpType(pop(), pop())})
			emit(t, vm.Lower)

		case lang.Call:
			s := pop()
			if s.kind != symValue {
				typ := s.typ
				// TODO: pop input types (careful with variadic function).
				for i := 0; i < typ.Rtype.NumOut(); i++ {
					push(&symbol{typ: typ.Out(i)})
				}
				emit(t, vm.Call)
				break
			}
			push(s)
			fallthrough // A symValue must be called through callX.

		case lang.CallX:
			rtyp := pop().value.Data.Type()
			// TODO: pop input types (careful with variadic function).
			for i := 0; i < rtyp.NumOut(); i++ {
				push(&symbol{typ: &vm.Type{Rtype: rtyp.Out(i)}})
			}
			emit(t, vm.CallX, t.Beg)

		case lang.Composite:
			log.Println("COMPOSITE")
			/*
				d := pop()
				switch d.typ.Rtype.Kind() {
				case reflect.Struct:
					// nf := d.typ.Rtype.NumField()
					// emit(t.Pos, vm.New, d.index, c.typeSym(d.typ).index)
					emit(t, vm.Field, 0)
					emit(t, vm.Vassign)
					emit(t, vm.Fdup, 2)
					emit(t, vm.Field, 1)
					emit(t, vm.Vassign)
					emit(t, vm.Pop, 1)
					// emit(t, vm.Fdup, 2)
					// Assume an element list with no keys, one per struct field in order
				}
			*/

		case lang.Grow:
			emit(t, vm.Grow, t.Beg)

		case lang.Define:
			// TODO: support assignment to local, composite objects.
			st := tokens[i-1]
			l := len(c.Data)
			d := pop()
			typ := d.typ
			if typ == nil {
				typ = d.value.Type
			}
			v := vm.NewValue(typ)
			c.addSym(l, st.Str, v, symVar, typ, false)
			c.Data = append(c.Data, v)
			emit(t, vm.Assign, l)

		case lang.Assign:
			st := tokens[i-1]
			if st.Tok == lang.Period || st.Tok == lang.Index {
				emit(t, vm.Vassign)
				break
			}
			s, ok := c.symbols[st.Str]
			if !ok {
				return fmt.Errorf("symbol not found: %s", st.Str)
			}
			d := pop()
			typ := d.typ
			if typ == nil {
				typ = d.value.Type
			}
			if s.typ == nil {
				s.typ = typ
				s.value = vm.NewValue(typ)
			}
			if s.local {
				if !s.used {
					emit(st, vm.New, s.index, c.typeSym(s.typ).index)
					s.used = true
				}
				emit(st, vm.Fassign, s.index)
				break
			}
			if s.index == unsetAddr {
				s.index = len(c.Data)
				c.Data = append(c.Data, s.value)
			}
			emit(st, vm.Assign, s.index)

		case lang.Equal:
			push(&symbol{typ: booleanOpType(pop(), pop())})
			emit(t, vm.Equal)

		case lang.EqualSet:
			push(&symbol{typ: booleanOpType(pop(), pop())})
			emit(t, vm.EqualSet)

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
				emit(t, vm.Fdup, s.index)
			} else {
				if s.index == unsetAddr {
					s.index = len(c.Data)
					c.Data = append(c.Data, s.value)
				}
				log.Println(t, ": emit(", t.Pos, vm.Dup, s.index, ")")
				emit(t, vm.Dup, s.index)
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
			var i int
			if s, ok := c.symbols[t.Str]; !ok {
				// t.Beg contains the position in code which needs to be fixed.
				t.Beg = len(c.Code)
				fixList = append(fixList, t)
			} else {
				i = int(s.value.Data.Int()) - len(c.Code)
			}
			emit(t, vm.JumpFalse, i)

		case lang.JumpSetFalse:
			var i int
			if s, ok := c.symbols[t.Str]; !ok {
				// t.Beg contains the position in code which needs to be fixed.
				t.Beg = len(c.Code)
				fixList = append(fixList, t)
			} else {
				i = int(s.value.Data.Int()) - len(c.Code)
			}
			emit(t, vm.JumpSetFalse, i)

		case lang.JumpSetTrue:
			var i int
			if s, ok := c.symbols[t.Str]; !ok {
				// t.Beg contains the position in code which needs to be fixed.
				t.Beg = len(c.Code)
				fixList = append(fixList, t)
			} else {
				i = int(s.value.Data.Int()) - len(c.Code)
			}
			emit(t, vm.JumpSetTrue, i)

		case lang.Goto:
			var i int
			if s, ok := c.symbols[t.Str]; !ok {
				t.Beg = len(c.Code)
				fixList = append(fixList, t)
			} else {
				i = int(s.value.Data.Int()) - len(c.Code)
			}
			emit(t, vm.Jump, i)

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
				emit(t, vm.Dup, l)
			default:
				if f, ok := s.typ.Rtype.FieldByName(t.Str[1:]); ok {
					emit(t, vm.Field, f.Index...)
					break
				}
				return fmt.Errorf("field or method not found: %s", t.Str[1:])
			}

		case lang.Return:
			emit(t, vm.Return, t.Beg, t.End)

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
		c.Code[t.Beg].Arg[0] = int(s.value.Data.Int()) - t.Beg

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
		switch l.Op {
		case vm.Jump, vm.JumpFalse, vm.JumpTrue, vm.JumpSetFalse, vm.JumpSetTrue, vm.Calli:
			if d, ok := labels[i+l.Arg[0]]; ok {
				extra = "// " + d[0]
			}
		case vm.Dup, vm.Assign, vm.Fdup, vm.Fassign:
			if d, ok := data[l.Arg[0]]; ok {
				extra = "// " + d
			}
		}
		fmt.Fprintf(os.Stderr, "%4d %-14v %v\n", i, l, extra)
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
			e.local,
			e.index,
			e.kind,
			e.typ,
			e.value,
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
	dict := c.symbolsByIndex()
	dv := make([]*DumpValue, len(c.Data))
	for i, d := range c.Data {
		e := dict[i]
		dv[i] = &DumpValue{
			Index: e.index,
			Name:  e.name,
			Kind:  int(e.kind),
			Type:  e.typ.Name,
			Value: d.Data.Interface(),
		}
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
			dv.Type != e.typ.Name ||
			dv.Kind != int(e.kind) {
			return fmt.Errorf("entry with index %d does not match with provided entry. "+
				"dumpValue: %s, %s, %d. memoryValue: %s, %s, %d",
				dv.Index,
				dv.Name, dv.Type, dv.Kind,
				e.name, e.typ, e.kind)
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
		tsym = &symbol{index: unsetAddr, kind: symType, typ: t}
		c.symbols[t.Rtype.String()] = tsym
	}
	if tsym.index == unsetAddr {
		tsym.index = len(c.Data)
		c.Data = append(c.Data, vm.NewValue(t))
	}
	return tsym
}
