// Package comp implements a byte code generator targeting the vm.
package comp

import (
	"fmt"
	"log"
	"os"
	"path"
	"reflect"
	"runtime"
	"strconv"
	"strings"

	"github.com/mvertes/parscan/lang"
	"github.com/mvertes/parscan/spar"
	"github.com/mvertes/parscan/symbol"
	"github.com/mvertes/parscan/vm"
)

// Compiler represents the state of a compiler.
type Compiler struct {
	*spar.Parser
	vm.Code            // produced code, to fill VM with
	Data    []vm.Value // produced data, will be at the bottom of VM stack
	Entry   int        // offset in Code to start execution from (skip function defintions)

	strings map[string]int // locations of strings in Data
}

// NewCompiler returns a new compiler state for a given scanner.
func NewCompiler(spec *lang.Spec) *Compiler {
	return &Compiler{
		Parser:  spar.NewParser(spec, true),
		Entry:   -1,
		strings: map[string]int{},
	}
}

func errorf(format string, v ...any) error {
	_, file, line, _ := runtime.Caller(1)
	loc := fmt.Sprintf("%s:%d: ", path.Base(file), line)
	return fmt.Errorf(loc+format, v...)
}

func showStack(stack []*symbol.Symbol) {
	_, file, line, _ := runtime.Caller(1)
	fmt.Fprintf(os.Stderr, "%s%d: showstack: %d\n", path.Base(file), line, len(stack))
	for i, s := range stack {
		fmt.Fprintf(os.Stderr, "  stack[%d]: %v\n", i, s)
	}
}

func (c *Compiler) emit(t spar.Token, op vm.Op, arg ...int) {
	_, file, line, _ := runtime.Caller(1)
	fmt.Fprintf(os.Stderr, "%s:%d: %v emit %v %v\n", path.Base(file), line, t, op, arg)
	c.Code = append(c.Code, vm.Instruction{Pos: vm.Pos(t.Pos), Op: op, Arg: arg})
}

// Generate generates vm code and data from parsed tokens.
func (c *Compiler) Generate(tokens spar.Tokens) (err error) {
	log.Println("Codegen tokens:", tokens)
	fixList := spar.Tokens{}    // list of tokens to fix after all necessary information is gathered
	stack := []*symbol.Symbol{} // for symbolic evaluation and type checking
	flen := []int{}             // stack length according to function scopes

	push := func(s *symbol.Symbol) { stack = append(stack, s) }
	top := func() *symbol.Symbol { return stack[len(stack)-1] }
	pop := func() *symbol.Symbol { l := len(stack) - 1; s := stack[l]; stack = stack[:l]; return s }
	popflen := func() int { le := len(flen) - 1; l := flen[le]; flen = flen[:le]; return l }

	for _, t := range tokens {
		switch t.Tok {
		case lang.Int:
			n, err := strconv.Atoi(t.Str)
			if err != nil {
				return err
			}
			push(&symbol.Symbol{Kind: symbol.Const, Value: vm.ValueOf(n), Type: vm.TypeOf(0)})
			c.emit(t, vm.Push, n)

		case lang.String:
			s := t.Block()
			v := vm.Value{Type: vm.TypeOf(s), Value: reflect.ValueOf(s)}
			i, ok := c.strings[s]
			if !ok {
				i = len(c.Data)
				c.Data = append(c.Data, v)
				c.strings[s] = i
			}
			push(&symbol.Symbol{Kind: symbol.Const, Value: v, Type: vm.TypeOf("")})
			c.emit(t, vm.Get, vm.Global, i)

		case lang.Add:
			push(&symbol.Symbol{Kind: symbol.Value, Type: arithmeticOpType(pop(), pop())})
			c.emit(t, vm.Add)

		case lang.Mul:
			push(&symbol.Symbol{Kind: symbol.Value, Type: arithmeticOpType(pop(), pop())})
			c.emit(t, vm.Mul)

		case lang.Sub:
			push(&symbol.Symbol{Kind: symbol.Value, Type: arithmeticOpType(pop(), pop())})
			c.emit(t, vm.Sub)

		case lang.Minus:
			c.emit(t, vm.Negate)

		case lang.Not:
			c.emit(t, vm.Not)

		case lang.Plus:
			// Unary '+' is idempotent. Nothing to do.

		case lang.Addr:
			push(&symbol.Symbol{Kind: symbol.Value, Type: vm.PointerTo(pop().Type)})
			c.emit(t, vm.Addr)

		case lang.Deref:
			push(&symbol.Symbol{Kind: symbol.Value, Type: pop().Type.Elem()})
			c.emit(t, vm.Deref)

		case lang.Index:
			pop()
			s := pop()
			if s.Type.Rtype.Kind() == reflect.Map {
				c.emit(t, vm.MapIndex)
			} else {
				c.emit(t, vm.Index)
			}
			push(&symbol.Symbol{Kind: symbol.Value, Type: s.Type.Elem()})

		case lang.Greater:
			push(&symbol.Symbol{Kind: symbol.Value, Type: booleanOpType(pop(), pop())})
			c.emit(t, vm.Greater)

		case lang.Less:
			push(&symbol.Symbol{Kind: symbol.Value, Type: booleanOpType(pop(), pop())})
			c.emit(t, vm.Lower)

		case lang.Call:
			narg := t.Arg[0].(int)
			s := stack[len(stack)-1-narg]
			if s.Kind != symbol.Value {
				typ := s.Type
				// TODO: pop input types (careful with variadic function).
				// Pop function and input arg symbols, push return value symbols.
				pop()
				for i := 0; i < narg; i++ {
					pop()
				}
				for i := 0; i < typ.Rtype.NumOut(); i++ {
					push(&symbol.Symbol{Kind: symbol.Value, Type: typ.Out(i)})
				}
				c.emit(t, vm.Call, narg)
				break
			}
			fallthrough // A symValue must be called through callX.

		case lang.CallX:
			narg := t.Arg[0].(int)
			s := stack[len(stack)-1-narg]
			rtyp := s.Value.Value.Type()
			// TODO: pop input types (careful with variadic function).
			for i := 0; i < rtyp.NumOut(); i++ {
				push(&symbol.Symbol{Kind: symbol.Value, Type: &vm.Type{Rtype: rtyp.Out(i)}})
			}
			c.emit(t, vm.CallX, narg)

		case lang.Colon:
			pop()
			ks := pop()
			ts := top()
			if ts.IsPtr() {
				// Resolve index on the element type
				ts = &symbol.Symbol{Kind: symbol.Value, Type: &vm.Type{Rtype: ts.Type.Rtype.Elem()}}
			}
			switch ks.Kind {
			case symbol.Const:
				switch ts.Type.Rtype.Kind() {
				case reflect.Struct:
					if v := ks.Value.Value; v.CanInt() {
						c.emit(t, vm.FieldFset)
					}
				case reflect.Slice:
					if ts.Type.Elem().IsPtr() {
						c.emit(t, vm.Addr)
					}
					c.emit(t, vm.IndexSet)
				case reflect.Map:
					c.emit(t, vm.MapSet)
				}
			case symbol.Unset:
				j := top().Type.FieldIndex(ks.Name)
				c.emit(t, vm.FieldSet, j...)
			}

		case lang.Composite:

		case lang.Grow:
			c.emit(t, vm.Grow, t.Arg[0].(int))

		case lang.Define:
			showStack(stack)
			n := t.Arg[0].(int)
			l := len(stack)
			rhs := stack[l-n:]
			stack = stack[:l-n]
			l = len(stack)
			lhs := stack[l-n:]
			stack = stack[:l-n]
			showStack(stack)
			for i, r := range rhs {
				// Propage type of rhs to lhs.
				typ := r.Type
				if typ == nil {
					typ = r.Value.Type
				}
				lhs[i].Type = typ
				c.Data[lhs[i].Index] = vm.NewValue(typ)
			}
			c.emit(t, vm.SetS, n)

		case lang.Assign:
			rhs := pop()
			lhs := pop()
			if lhs.Local {
				if !lhs.Used {
					c.emit(t, vm.New, lhs.Index, c.typeSym(lhs.Type).Index)
					lhs.Used = true
				}
				c.emit(t, vm.Set, 1, lhs.Index)
				break
			}
			// TODO check source type against var type
			if v := c.Data[lhs.Index]; !v.IsValid() {
				c.Data[lhs.Index] = vm.NewValue(rhs.Type)
				c.Symbols[lhs.Name].Type = rhs.Type
			}
			c.emit(t, vm.SetS, t.Arg[0].(int))

		case lang.IndexAssign:
			s := stack[len(stack)-3]
			switch s.Type.Rtype.Kind() {
			case reflect.Array, reflect.Slice:
				c.emit(t, vm.IndexSet)
			case reflect.Map:
				c.emit(t, vm.MapSet)
			default:
				return errorf("not a map or array: %s", s.Name)
			}
			stack = stack[:len(stack)-3]

		case lang.Equal:
			push(&symbol.Symbol{Type: booleanOpType(pop(), pop())})
			c.emit(t, vm.Equal)

		case lang.EqualSet:
			push(&symbol.Symbol{Type: booleanOpType(pop(), pop())})
			c.emit(t, vm.EqualSet)

		case lang.Ident:
			s, ok := c.Symbols[t.Str]
			if !ok {
				// It could be either an undefined symbol or a key ident in a literal composite expr.
				s = &symbol.Symbol{Name: t.Str}
			}
			push(s)
			if s.Kind == symbol.Pkg || s.Kind == symbol.Unset {
				break
			}
			if s.Local {
				c.emit(t, vm.Get, vm.Local, s.Index)
			} else {
				if s.Index == symbol.UnsetAddr {
					s.Index = len(c.Data)
					c.Data = append(c.Data, s.Value)
				}
				if s.Kind == symbol.Type {
					switch s.Type.Rtype.Kind() {
					case reflect.Slice:
						c.emit(t, vm.Fnew, s.Index, s.SliceLen)
					case reflect.Pointer:
						c.emit(t, vm.FnewE, s.Index, 1)
					default:
						c.emit(t, vm.Fnew, s.Index, 1)
					}
				} else {
					c.emit(t, vm.Get, vm.Global, s.Index)
				}
			}

		case lang.Label:
			lc := len(c.Code)
			if s, ok := c.Symbols[t.Str]; ok {
				s.Value = vm.ValueOf(lc)
				if s.Kind == symbol.Func {
					// Label is a function entry point, register its code address in data
					// and save caller stack length.
					s.Index = len(c.Data)
					c.Data = append(c.Data, s.Value)
					flen = append(flen, len(stack))
				} else {
					c.Data[s.Index] = s.Value
				}
			} else {
				if strings.HasSuffix(t.Str, "_end") {
					if s, ok = c.Symbols[strings.TrimSuffix(t.Str, "_end")]; ok && s.Kind == symbol.Func {
						// Exit function: restore caller stack
						l := popflen()
						stack = stack[:l]
					}
				}
				c.Symbols[t.Str] = &symbol.Symbol{Kind: symbol.Label, Value: vm.ValueOf(lc)}
			}

		case lang.Len:
			push(&symbol.Symbol{Type: c.Symbols["int"].Type})
			c.emit(t, vm.Len, t.Arg[0].(int))

		case lang.JumpFalse:
			var i int
			if s, ok := c.Symbols[t.Str]; !ok {
				t.Arg = []any{len(c.Code)} // current code location
				fixList = append(fixList, t)
			} else {
				i = int(s.Value.Int()) - len(c.Code)
			}
			c.emit(t, vm.JumpFalse, i)

		case lang.JumpSetFalse:
			var i int
			if s, ok := c.Symbols[t.Str]; !ok {
				t.Arg = []any{len(c.Code)} // current code location
				fixList = append(fixList, t)
			} else {
				i = int(s.Value.Int()) - len(c.Code)
			}
			c.emit(t, vm.JumpSetFalse, i)

		case lang.JumpSetTrue:
			var i int
			if s, ok := c.Symbols[t.Str]; !ok {
				t.Arg = []any{len(c.Code)} // current code location
				fixList = append(fixList, t)
			} else {
				i = int(s.Value.Int()) - len(c.Code)
			}
			c.emit(t, vm.JumpSetTrue, i)

		case lang.Goto:
			var i int
			if s, ok := c.Symbols[t.Str]; !ok {
				t.Arg = []any{len(c.Code)} // current code location
				fixList = append(fixList, t)
			} else {
				i = int(s.Value.Int()) - len(c.Code)
			}
			c.emit(t, vm.Jump, i)

		case lang.Period:
			if len(stack) < 1 {
				return errorf("missing symbol")
			}
			s := pop()
			switch s.Kind {
			case symbol.Pkg:
				p, ok := spar.Packages[s.PkgPath]
				if !ok {
					return fmt.Errorf("package not found: %s", s.PkgPath)
				}
				v, ok := p[t.Str[1:]]
				if !ok {
					return fmt.Errorf("symbol not found in package %s: %s", s.PkgPath, t.Str[1:])
				}
				name := s.PkgPath + t.Str
				var l int
				sym, _, ok := c.Symbols.Get(name, "")
				if ok {
					l = sym.Index
				} else {
					l = len(c.Data)
					c.Data = append(c.Data, v)
					c.Symbols.Add(l, name, v, symbol.Value, v.Type, false)
					sym = c.Symbols[name]
				}
				push(sym)
				c.emit(t, vm.Get, vm.Global, l)
			case symbol.Unset:
				return errorf("invalid symbol: %s", s.Name)
			default:
				if m := c.Symbols.MethodByName(s, t.Str[1:]); m != nil {
					push(m)
					c.emit(t, vm.Get, vm.Global, m.Index)
					break
				}
				typ := s.Type.Rtype
				isPtr := typ.Kind() == reflect.Pointer
				if isPtr {
					typ = typ.Elem()
				}
				if f, ok := typ.FieldByName(t.Str[1:]); ok {
					if isPtr {
						push(&symbol.Symbol{Type: s.Type.Elem().FieldType(t.Str[1:])})
					} else {
						push(&symbol.Symbol{Type: s.Type.FieldType(t.Str[1:])})
					}
					c.emit(t, vm.Field, f.Index...)
					break
				}
				return fmt.Errorf("field or method not found: %s", t.Str[1:])
			}

		case lang.Next:
			showStack(stack)
			n := t.Arg[0].(int)
			var i int
			if s, ok := c.Symbols[t.Str]; !ok {
				t.Arg = []any{len(c.Code)} // current code location
				fixList = append(fixList, t)
			} else {
				i = int(s.Value.Int()) - len(c.Code)
			}
			if n == 2 {
				v := stack[len(stack)-2]
				k := stack[len(stack)-3]
				c.emit(t, vm.Next2, i, k.Index, v.Index)
			} else {
				k := stack[len(stack)-2]
				c.emit(t, vm.Next, i, k.Index)
			}

		case lang.Range:
			n := t.Arg[0].(int)
			// FIXME: handle all iterator types.
			// set the correct type to the iterator variables.
			switch typ := top().Type; typ.Rtype.Kind() {
			case reflect.Slice:
				switch n {
				case 1:
					k := stack[len(stack)-2]
					k.Type = c.Symbols["int"].Type
					c.Data[k.Index] = vm.NewValue(k.Type)
					c.emit(t, vm.Pull)
				case 2:
					k, v := stack[len(stack)-3], stack[len(stack)-2]
					k.Type = c.Symbols["int"].Type
					v.Type = typ.Elem()
					c.Data[k.Index] = vm.NewValue(k.Type)
					c.Data[v.Index] = vm.NewValue(v.Type)
					c.emit(t, vm.Pull2)
				default:
				}
			case reflect.Map:
				// FIXME: handle map
			}

		case lang.Stop:
			c.emit(t, vm.Stop)

		case lang.Return:
			numOut, numIn := t.Arg[0].(int), t.Arg[1].(int)
			c.emit(t, vm.Return, numOut, numIn)

		case lang.Slice:
			if stack[len(stack)-3].IsInt() {
				c.emit(t, vm.Slice3)
				stack = stack[:len(stack)-4]
			} else {
				c.emit(t, vm.Slice)
				stack = stack[:len(stack)-3]
			}

		default:
			return fmt.Errorf("generate: unsupported token %v", t)
		}
	}

	// Finally we fix unresolved labels for jump destinations.
	for _, t := range fixList {
		s, ok := c.Symbols[t.Str]
		if !ok {
			return fmt.Errorf("label not found: %q", t.Str)
		}
		loc := t.Arg[0].(int)
		c.Code[loc].Arg[0] = int(s.Value.Int()) - loc // relative code position
	}
	return err
}

func arithmeticOpType(s, _ *symbol.Symbol) *vm.Type { return symbol.Vtype(s) }
func booleanOpType(_, _ *symbol.Symbol) *vm.Type    { return vm.TypeOf(true) }

// PrintCode pretty prints the generated code.
func (c *Compiler) PrintCode() {
	labels := map[int][]string{} // labels indexed by code location
	data := map[int]string{}     // data indexed by frame location

	for name, sym := range c.Symbols {
		if sym.Kind == symbol.Label || sym.Kind == symbol.Func {
			i := int(sym.Value.Int())
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
		case vm.Jump, vm.JumpFalse, vm.JumpTrue, vm.JumpSetFalse, vm.JumpSetTrue:
			if d, ok := labels[i+l.Arg[0]]; ok {
				extra = "// " + d[0]
			}
		case vm.Get, vm.Set:
			if d, ok := data[l.Arg[0]]; ok {
				extra = "// " + d
			}
		}
		fmt.Fprintf(os.Stderr, "%4d %v %v\n", i, l, extra)
	}

	for _, label := range labels[len(c.Code)] {
		fmt.Fprintln(os.Stderr, label+":")
	}
	fmt.Fprintln(os.Stderr, "# End code")
}

type entry struct {
	name string
	*symbol.Symbol
}

func (e entry) String() string { return fmt.Sprintf("name: %s, sym: %v", e.name, e.Symbol) }

// PrintData pretty prints the generated global data symbols in compiler.
func (c *Compiler) PrintData() {
	dict := c.symbolsByIndex()

	fmt.Fprintln(os.Stderr, "# Data:")
	for i, d := range c.Data {
		if d.IsValid() {
			fmt.Fprintf(os.Stderr, "%4d %T %v, Symbol: %v\n", i, d.Interface(), d.Value, dict[i])
		} else {
			fmt.Fprintf(os.Stderr, "%4d %v %v\n", i, d.Value, dict[i])
		}
	}
}

func (c *Compiler) symbolsByIndex() map[int]entry {
	dict := map[int]entry{}
	for name, sym := range c.Symbols {
		if sym.Index == symbol.UnsetAddr {
			continue
		}
		dict[sym.Index] = entry{name, sym}
	}
	return dict
}

func (c *Compiler) typeSym(t *vm.Type) *symbol.Symbol {
	tsym, ok := c.Symbols[t.Rtype.String()]
	if !ok {
		tsym = &symbol.Symbol{Index: symbol.UnsetAddr, Kind: symbol.Type, Type: t}
		c.Symbols[t.Rtype.String()] = tsym
	}
	if tsym.Index == symbol.UnsetAddr {
		tsym.Index = len(c.Data)
		c.Data = append(c.Data, vm.NewValue(t))
	}
	return tsym
}
