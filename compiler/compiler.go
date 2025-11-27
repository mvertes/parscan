package compiler

import (
	"fmt"
	"log"
	"os"
	"path"
	"reflect"
	"runtime"
	"strconv"

	"github.com/mvertes/parscan/lang"
	"github.com/mvertes/parscan/parser"
	"github.com/mvertes/parscan/scanner"
	"github.com/mvertes/parscan/vm"
)

// Compiler represents the state of a compiler.
type Compiler struct {
	*parser.Parser
	vm.Code            // produced code, to fill VM with
	Data    []vm.Value // produced data, will be at the bottom of VM stack
	Entry   int        // offset in Code to start execution from (skip function defintions)

	strings map[string]int // locations of strings in Data
}

// NewCompiler returns a new compiler state for a given scanner.
func NewCompiler(spec *lang.Spec) *Compiler {
	return &Compiler{
		Parser:  parser.NewParser(spec, true),
		Entry:   -1,
		strings: map[string]int{},
	}
}

// AddSym adds a new named value to the compiler symbol table, and returns its index in memory.
func (c *Compiler) AddSym(name string, value vm.Value) int {
	p := len(c.Data)
	c.Data = append(c.Data, value)
	c.AddSymbol(p, name, value, parser.SymValue, nil, false)
	return p
}

// Codegen generates vm code from parsed tokens.
func (c *Compiler) Codegen(tokens parser.Tokens) (err error) {
	log.Println("Codegen tokens:", tokens)
	fixList := parser.Tokens{}  // list of tokens to fix after all necessary information is gathered
	stack := []*parser.Symbol{} // for symbolic evaluation, type checking, etc

	emit := func(t scanner.Token, op vm.Op, arg ...int) {
		_, file, line, _ := runtime.Caller(1)
		fmt.Fprintf(os.Stderr, "%s:%d: %v emit %v %v\n", path.Base(file), line, t, op, arg)
		c.Code = append(c.Code, vm.Instruction{Pos: vm.Pos(t.Pos), Op: op, Arg: arg})
	}
	push := func(s *parser.Symbol) { stack = append(stack, s) }
	pop := func() *parser.Symbol { l := len(stack) - 1; s := stack[l]; stack = stack[:l]; return s }

	for i, t := range tokens {
		switch t.Tok {
		case lang.Int:
			n, err := strconv.Atoi(t.Str)
			if err != nil {
				return err
			}
			push(&parser.Symbol{Kind: parser.SymConst, Value: vm.ValueOf(n), Type: vm.TypeOf(0)})
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
			push(&parser.Symbol{Kind: parser.SymConst, Value: v})
			emit(t, vm.Dup, i)

		case lang.Add:
			push(&parser.Symbol{Type: arithmeticOpType(pop(), pop())})
			emit(t, vm.Add)

		case lang.Mul:
			push(&parser.Symbol{Type: arithmeticOpType(pop(), pop())})
			emit(t, vm.Mul)

		case lang.Sub:
			push(&parser.Symbol{Type: arithmeticOpType(pop(), pop())})
			emit(t, vm.Sub)

		case lang.Minus:
			emit(t, vm.Push, 0)
			emit(t, vm.Sub)

		case lang.Not:
			emit(t, vm.Not)

		case lang.Plus:
			// Unary '+' is idempotent. Nothing to do.

		case lang.Addr:
			push(&parser.Symbol{Type: vm.PointerTo(pop().Type)})
			emit(t, vm.Addr)

		case lang.Deref:
			push(&parser.Symbol{Type: pop().Type.Elem()})
			emit(t, vm.Deref)

		case lang.Index:
			push(&parser.Symbol{Type: pop().Type.Elem()})
			emit(t, vm.Index)

		case lang.Greater:
			push(&parser.Symbol{Type: booleanOpType(pop(), pop())})
			emit(t, vm.Greater)

		case lang.Less:
			push(&parser.Symbol{Type: booleanOpType(pop(), pop())})
			emit(t, vm.Lower)

		case lang.Call:
			s := pop()
			if s.Kind != parser.SymValue {
				typ := s.Type
				// TODO: pop input types (careful with variadic function).
				for i := 0; i < typ.Rtype.NumOut(); i++ {
					push(&parser.Symbol{Type: typ.Out(i)})
				}
				emit(t, vm.Call)
				break
			}
			push(s)
			fallthrough // A symValue must be called through callX.

		case lang.CallX:
			rtyp := pop().Value.Data.Type()
			// TODO: pop input types (careful with variadic function).
			for i := 0; i < rtyp.NumOut(); i++ {
				push(&parser.Symbol{Type: &vm.Type{Rtype: rtyp.Out(i)}})
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
			typ := d.Type
			if typ == nil {
				typ = d.Value.Type
			}
			v := vm.NewValue(typ)
			c.AddSymbol(l, st.Str, v, parser.SymVar, typ, false)
			c.Data = append(c.Data, v)
			emit(t, vm.Assign, l)

		case lang.Assign:
			st := tokens[i-1]
			if st.Tok == lang.Period || st.Tok == lang.Index {
				emit(t, vm.Vassign)
				break
			}
			s, ok := c.Symbols[st.Str]
			if !ok {
				return fmt.Errorf("symbol not found: %s", st.Str)
			}
			d := pop()
			typ := d.Type
			if typ == nil {
				typ = d.Value.Type
			}
			if s.Type == nil {
				s.Type = typ
				s.Value = vm.NewValue(typ)
			}
			if s.Local {
				if !s.Used {
					emit(st, vm.New, s.Index, c.typeSym(s.Type).Index)
					s.Used = true
				}
				emit(st, vm.Fassign, s.Index)
				break
			}
			if s.Index == parser.UnsetAddr {
				s.Index = len(c.Data)
				c.Data = append(c.Data, s.Value)
			}
			emit(st, vm.Assign, s.Index)

		case lang.Equal:
			push(&parser.Symbol{Type: booleanOpType(pop(), pop())})
			emit(t, vm.Equal)

		case lang.EqualSet:
			push(&parser.Symbol{Type: booleanOpType(pop(), pop())})
			emit(t, vm.EqualSet)

		case lang.Ident:
			if i < len(tokens)-1 {
				switch t1 := tokens[i+1]; t1.Tok {
				case lang.Define, lang.Assign, lang.Colon:
					continue
				}
			}
			s, ok := c.Symbols[t.Str]
			if !ok {
				return fmt.Errorf("symbol not found: %s", t.Str)
			}
			push(s)
			if s.Kind == parser.SymPkg {
				break
			}
			if s.Local {
				emit(t, vm.Fdup, s.Index)
			} else {
				if s.Index == parser.UnsetAddr {
					s.Index = len(c.Data)
					c.Data = append(c.Data, s.Value)
				}
				emit(t, vm.Dup, s.Index)
			}

		case lang.Label:
			lc := len(c.Code)
			s, ok := c.Symbols[t.Str]
			if ok {
				s.Value = vm.ValueOf(lc)
				if s.Kind == parser.SymFunc {
					// label is a function entry point, register its code address in data.
					s.Index = len(c.Data)
					c.Data = append(c.Data, s.Value)
				} else {
					c.Data[s.Index] = s.Value
				}
			} else {
				c.Symbols[t.Str] = &parser.Symbol{Kind: parser.SymLabel, Value: vm.ValueOf(lc)}
			}

		case lang.JumpFalse:
			var i int
			if s, ok := c.Symbols[t.Str]; !ok {
				// t.Beg contains the position in code which needs to be fixed.
				t.Beg = len(c.Code)
				fixList = append(fixList, t)
			} else {
				i = int(s.Value.Data.Int()) - len(c.Code)
			}
			emit(t, vm.JumpFalse, i)

		case lang.JumpSetFalse:
			var i int
			if s, ok := c.Symbols[t.Str]; !ok {
				// t.Beg contains the position in code which needs to be fixed.
				t.Beg = len(c.Code)
				fixList = append(fixList, t)
			} else {
				i = int(s.Value.Data.Int()) - len(c.Code)
			}
			emit(t, vm.JumpSetFalse, i)

		case lang.JumpSetTrue:
			var i int
			if s, ok := c.Symbols[t.Str]; !ok {
				// t.Beg contains the position in code which needs to be fixed.
				t.Beg = len(c.Code)
				fixList = append(fixList, t)
			} else {
				i = int(s.Value.Data.Int()) - len(c.Code)
			}
			emit(t, vm.JumpSetTrue, i)

		case lang.Goto:
			var i int
			if s, ok := c.Symbols[t.Str]; !ok {
				t.Beg = len(c.Code)
				fixList = append(fixList, t)
			} else {
				i = int(s.Value.Data.Int()) - len(c.Code)
			}
			emit(t, vm.Jump, i)

		case lang.Period:
			s := pop()
			switch s.Kind {
			case parser.SymPkg:
				p, ok := parser.Packages[s.PkgPath]
				if !ok {
					return fmt.Errorf("package not found: %s", s.PkgPath)
				}
				v, ok := p[t.Str[1:]]
				if !ok {
					return fmt.Errorf("symbol not found in package %s: %s", s.PkgPath, t.Str[1:])
				}
				name := s.PkgPath + t.Str
				var l int
				sym, _, ok := c.GetSym(name, "")
				if ok {
					l = sym.Index
				} else {
					l = len(c.Data)
					c.Data = append(c.Data, v)
					c.AddSymbol(l, name, v, parser.SymValue, v.Type, false)
					sym = c.Symbols[name]
				}
				push(sym)
				emit(t, vm.Dup, l)
			default:
				if f, ok := s.Type.Rtype.FieldByName(t.Str[1:]); ok {
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
		s, ok := c.Symbols[t.Str]
		if !ok {
			return fmt.Errorf("label not found: %q", t.Str)
		}
		c.Code[t.Beg].Arg[0] = int(s.Value.Data.Int()) - t.Beg
	}
	return err
}
func arithmeticOpType(s1, _ *parser.Symbol) *vm.Type { return parser.SymbolType(s1) }
func booleanOpType(_, _ *parser.Symbol) *vm.Type     { return vm.TypeOf(true) }

// PrintCode pretty prints the generated code.
func (c *Compiler) PrintCode() {
	labels := map[int][]string{} // labels indexed by code location
	data := map[int]string{}     // data indexed by frame location

	for name, sym := range c.Symbols {
		if sym.Kind == parser.SymLabel || sym.Kind == parser.SymFunc {
			i := int(sym.Value.Data.Int())
			labels[i] = append(labels[i], name)
		}
		if sym.Used {
			data[sym.Index] = name
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
	*parser.Symbol
}

func (e entry) String() string {
	if e.Symbol != nil {
		return fmt.Sprintf("name: %s,local: %t, i: %d, k: %d, t: %s, v: %v",
			e.name,
			e.Local,
			e.Index,
			e.Kind,
			e.Type,
			e.Value,
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
	for name, sym := range c.Symbols {
		if sym.Index == parser.UnsetAddr {
			continue
		}
		dict[sym.Index] = entry{name, sym}
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
			Index: e.Index,
			Name:  e.name,
			Kind:  int(e.Kind),
			Type:  e.Type.Name,
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
			dv.Type != e.Type.Name ||
			dv.Kind != int(e.Kind) {
			return fmt.Errorf("entry with index %d does not match with provided entry. "+
				"dumpValue: %s, %s, %d. memoryValue: %s, %s, %d",
				dv.Index,
				dv.Name, dv.Type, dv.Kind,
				e.name, e.Type, e.Kind)
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

func (c *Compiler) typeSym(t *vm.Type) *parser.Symbol {
	tsym, ok := c.Symbols[t.Rtype.String()]
	if !ok {
		tsym = &parser.Symbol{Index: parser.UnsetAddr, Kind: parser.SymType, Type: t}
		c.Symbols[t.Rtype.String()] = tsym
	}
	if tsym.Index == parser.UnsetAddr {
		tsym.Index = len(c.Data)
		c.Data = append(c.Data, vm.NewValue(t))
	}
	return tsym
}
