// Package comp implements a byte code generator targeting the vm.
package comp

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"reflect"
	"runtime"
	"strconv"
	"strings"

	"github.com/mvertes/parscan/goparser"
	"github.com/mvertes/parscan/lang"
	"github.com/mvertes/parscan/symbol"
	"github.com/mvertes/parscan/vm"
)

// Compiler represents the state of a compiler.
type Compiler struct {
	*goparser.Parser
	vm.Code            // produced code, to fill VM with
	Data    []vm.Value // produced data, will be at the bottom of VM stack
	Entry   int        // offset in Code to start execution from

	strings   map[string]int // locations of strings in Data
	methodIDs map[string]int // global method ID by method name
}

// NewCompiler returns a new compiler state for a given scanner.
func NewCompiler(spec *lang.Spec) *Compiler {
	return &Compiler{
		Parser:    goparser.NewParser(spec, true),
		Entry:     -1,
		strings:   map[string]int{},
		methodIDs: map[string]int{},
	}
}

// Compile parses src and generates code and data, or returns a non-nil error.
// Code and data are added incrementally in c.Code and C.Data.
func (c *Compiler) Compile(src string) error {
	decls, err := c.ScanDecls(src)
	if err != nil {
		return err
	}

	// Register function signatures in advance (fix mutually recursive funcs).
	for _, decl := range decls {
		c.SymTracker = nil
		dataLen, codeLen := len(c.Data), len(c.Code)
		if err = c.RegisterFunc(decl); err != nil {
			c.rollback(dataLen, codeLen)
		}
	}

	// Retry until no undefined declaration remains, or stale, or other error.
	pending := decls
	for len(pending) > 0 {
		var retry []goparser.Tokens
		for _, decl := range pending {
			c.SymTracker = nil
			dataLen, codeLen := len(c.Data), len(c.Code)
			toks, parseErr := c.ParseOneStmt(decl)
			if parseErr == nil {
				parseErr = c.generate(toks)
			}
			if parseErr != nil {
				var eu goparser.ErrUndefined
				if errors.As(parseErr, &eu) {
					c.rollback(dataLen, codeLen)
					retry = append(retry, decl)
					continue
				}
				return parseErr
			}
		}
		if len(retry) == len(pending) {
			// No progress: return first error.
			_, err = c.ParseOneStmt(pending[0])
			return err
		}
		pending = retry
	}
	return nil
}

func (c *Compiler) rollback(dataLen, codeLen int) {
	for _, k := range c.SymTracker {
		delete(c.Symbols, k)
	}
	c.SymTracker = nil
	c.Data = c.Data[:dataLen]
	c.Code = c.Code[:codeLen]
}

func (c *Compiler) methodID(name string) int {
	if id, ok := c.methodIDs[name]; ok {
		return id
	}
	id := len(c.methodIDs)
	c.methodIDs[name] = id
	return id
}

func (c *Compiler) typeIndex(typ *vm.Type) int {
	i := len(c.Data)
	c.Data = append(c.Data, vm.ValueOf(typ))
	return i
}

// registerMethods registers promoted methods from embedded types into typ so that
// interface dispatch (IfaceCall) can find them. Called at IfaceWrap emission time when
// iface is the interface type being satisfied and typ is the concrete type.
func (c *Compiler) registerMethods(iface, typ *vm.Type) {
	// For *T, resolve T so we can look up value methods and embedded promotions.
	isPtr := typ.Rtype.Kind() == reflect.Pointer
	lookupTyp := typ
	if isPtr {
		elemRtype := typ.Rtype.Elem()
		for _, sym := range c.Symbols {
			if sym.Kind == symbol.Type && sym.Type != nil && sym.Type.Rtype == elemRtype {
				lookupTyp = sym.Type
				break
			}
		}
	}
	for _, im := range iface.IfaceMethods {
		id := c.methodID(im.Name)
		if id < len(typ.Methods) && typ.Methods[id].Index >= 0 {
			continue // already registered directly
		}
		// Find the method: value receiver, pointer receiver, or promoted.
		s := &symbol.Symbol{Kind: symbol.Var, Name: lookupTyp.Name, Type: lookupTyp}
		m, fieldPath := c.Symbols.MethodByName(s, im.Name)
		if m == nil {
			continue
		}
		// Path: nil = direct (no adjustment), []int{} = deref only, non-empty = field path.
		var mpath []int
		if len(fieldPath) > 0 {
			if isPtr {
				mpath = append([]int{}, fieldPath...)
			} else {
				mpath = fieldPath
			}
		} else if isPtr && !strings.HasPrefix(m.Name, "*") {
			mpath = []int{} // non-nil empty = deref only
		}
		for len(typ.Methods) <= id {
			typ.Methods = append(typ.Methods, vm.Method{Index: -1})
		}
		typ.Methods[id] = vm.Method{Index: m.Index, Path: mpath}
	}
}

func (c *Compiler) stringIndex(s string) int {
	i, ok := c.strings[s]
	if !ok {
		i = len(c.Data)
		c.Data = append(c.Data, vm.ValueOf(s))
		c.strings[s] = i
	}
	return i
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

func (c *Compiler) emit(t goparser.Token, op vm.Op, arg ...int) {
	_, file, line, _ := runtime.Caller(1)
	fmt.Fprintf(os.Stderr, "%s:%d: %v emit %v %v\n", path.Base(file), line, t, op, arg)
	c.Code = append(c.Code, vm.Instruction{Pos: vm.Pos(t.Pos), Op: op, Arg: arg})
}

// generate generates vm code and data from parsed tokens, or returns an error.
func (c *Compiler) generate(tokens goparser.Tokens) (err error) {
	log.Println("Codegen tokens:", tokens)
	fixList := goparser.Tokens{} // list of tokens to fix after all necessary information is gathered
	stack := []*symbol.Symbol{}  // for symbolic evaluation and type checking
	flen := []int{}              // stack length according to function scopes
	funcStack := []string{}      // names of functions currently being compiled

	push := func(s *symbol.Symbol) { stack = append(stack, s) }
	top := func() *symbol.Symbol { return stack[len(stack)-1] }
	pop := func() *symbol.Symbol { l := len(stack) - 1; s := stack[l]; stack = stack[:l]; return s }
	popflen := func() int { le := len(flen) - 1; l := flen[le]; flen = flen[:le]; return l }
	curFunc := func() string {
		if n := len(funcStack); n > 0 {
			return funcStack[n-1]
		}
		return ""
	}

	for _, t := range tokens {
		switch t.Tok {
		case lang.Int:
			n64, err := strconv.ParseInt(t.Str, 0, 64)
			if err != nil {
				return err
			}
			n := int(n64)
			push(&symbol.Symbol{Kind: symbol.Const, Value: vm.ValueOf(n), Type: c.Symbols["int"].Type})
			c.emit(t, vm.Push, n)

		case lang.Float:
			f, err := strconv.ParseFloat(t.Str, 64)
			if err != nil {
				return err
			}
			v := vm.ValueOf(f)
			di := len(c.Data)
			c.Data = append(c.Data, v)
			push(&symbol.Symbol{Kind: symbol.Const, Value: v, Type: c.Symbols["float64"].Type})
			c.emit(t, vm.Get, vm.Global, di)

		case lang.String:
			s := t.Block()
			push(&symbol.Symbol{Kind: symbol.Const, Value: vm.ValueOf(s), Type: c.Symbols["string"].Type})
			c.emit(t, vm.Get, vm.Global, c.stringIndex(s))

		case lang.Add:
			typ := arithmeticOpType(pop(), pop())
			push(&symbol.Symbol{Kind: symbol.Value, Type: typ})
			c.emit(t, numericOp(vm.AddInt, vm.Add, typ))

		case lang.Mul:
			typ := arithmeticOpType(pop(), pop())
			push(&symbol.Symbol{Kind: symbol.Value, Type: typ})
			c.emit(t, numericOp(vm.MulInt, vm.Mul, typ))

		case lang.Sub:
			typ := arithmeticOpType(pop(), pop())
			push(&symbol.Symbol{Kind: symbol.Value, Type: typ})
			c.emit(t, numericOp(vm.SubInt, vm.Sub, typ))

		case lang.Quo:
			typ := arithmeticOpType(pop(), pop())
			push(&symbol.Symbol{Kind: symbol.Value, Type: typ})
			c.emit(t, numericOp(vm.DivInt, vm.DivInt, typ))

		case lang.Rem:
			typ := arithmeticOpType(pop(), pop())
			push(&symbol.Symbol{Kind: symbol.Value, Type: typ})
			c.emit(t, numericOp(vm.RemInt, vm.RemInt, typ))

		case lang.Minus:
			typ := symbol.Vtype(top())
			c.emit(t, numericOp(vm.NegInt, vm.Neg, typ))

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

		case lang.TypeAssert:
			okForm := t.Arg[0].(int)
			typ := t.Arg[1].(*vm.Type)
			pop() // interface value
			push(&symbol.Symbol{Kind: symbol.Value, Type: typ})
			if okForm == 1 {
				push(&symbol.Symbol{Kind: symbol.Value, Type: vm.TypeOf(false)})
			}
			c.emit(t, vm.TypeAssert, c.typeIndex(typ), okForm)

		case lang.TypeSwitchJump:
			var typ *vm.Type
			if t.Arg[0] != nil {
				typ = t.Arg[0].(*vm.Type)
			}
			pop() // consume iface_sym from compiler stack
			typeIdx := -1
			if typ != nil {
				typeIdx = c.typeIndex(typ)
			}
			var offset int
			if s, ok := c.Symbols[t.Str]; !ok {
				t.Arg = []any{len(c.Code)} // store code location for fixup
				fixList = append(fixList, t)
			} else {
				offset = int(s.Value.Int()) - len(c.Code)
			}
			c.emit(t, vm.TypeBranch, offset, typeIdx) // Arg[0]=offset, Arg[1]=typeIdx

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
			s2, s1 := pop(), pop()
			typ := symbol.Vtype(s1)
			push(&symbol.Symbol{Kind: symbol.Value, Type: booleanOpType(s2, s1)})
			c.emit(t, numericOp(vm.GreaterInt, vm.Greater, typ))

		case lang.Less:
			s2, s1 := pop(), pop()
			typ := symbol.Vtype(s1)
			push(&symbol.Symbol{Kind: symbol.Value, Type: booleanOpType(s2, s1)})
			c.emit(t, numericOp(vm.LowerInt, vm.Lower, typ))

		case lang.GreaterEqual:
			s2, s1 := pop(), pop()
			typ := symbol.Vtype(s1)
			push(&symbol.Symbol{Kind: symbol.Value, Type: booleanOpType(s2, s1)})
			c.emit(t, numericOp(vm.LowerInt, vm.Lower, typ))
			c.emit(t, vm.Not)

		case lang.LessEqual:
			s2, s1 := pop(), pop()
			typ := symbol.Vtype(s1)
			push(&symbol.Symbol{Kind: symbol.Value, Type: booleanOpType(s2, s1)})
			c.emit(t, numericOp(vm.GreaterInt, vm.Greater, typ))
			c.emit(t, vm.Not)

		case lang.NotEqual:
			push(&symbol.Symbol{Type: booleanOpType(pop(), pop())})
			c.emit(t, vm.Equal)
			c.emit(t, vm.Not)

		case lang.And:
			typ := arithmeticOpType(pop(), pop())
			push(&symbol.Symbol{Kind: symbol.Value, Type: typ})
			c.emit(t, vm.BitAnd)

		case lang.Or:
			typ := arithmeticOpType(pop(), pop())
			push(&symbol.Symbol{Kind: symbol.Value, Type: typ})
			c.emit(t, vm.BitOr)

		case lang.Xor:
			typ := arithmeticOpType(pop(), pop())
			push(&symbol.Symbol{Kind: symbol.Value, Type: typ})
			c.emit(t, vm.BitXor)

		case lang.AndNot:
			typ := arithmeticOpType(pop(), pop())
			push(&symbol.Symbol{Kind: symbol.Value, Type: typ})
			c.emit(t, vm.BitAndNot)

		case lang.Shl:
			pop() // shift amount
			// Result type is the type of the left operand.
			push(&symbol.Symbol{Kind: symbol.Value, Type: symbol.Vtype(top())})
			c.emit(t, vm.BitShl)

		case lang.Shr:
			pop() // shift amount
			push(&symbol.Symbol{Kind: symbol.Value, Type: symbol.Vtype(top())})
			c.emit(t, vm.BitShr)

		case lang.BitComp:
			c.emit(t, vm.BitComp)

		case lang.Call:
			narg := t.Arg[0].(int)
			s := stack[len(stack)-1-narg]
			if s.Kind == symbol.Type {
				if narg != 1 {
					return errorf("type conversion requires exactly one argument")
				}
				// Find and remove the Fnew/FnewE instruction emitted by the Ident
				// handler for the type. It precedes the argument instructions.
				for i := len(c.Code) - 1; i >= 0; i-- {
					op := c.Code[i].Op
					if (op == vm.Fnew || op == vm.FnewE) && c.Code[i].Arg[0] == s.Index {
						copy(c.Code[i:], c.Code[i+1:])
						c.Code = c.Code[:len(c.Code)-1]
						break
					}
				}
				pop() // type symbol
				pop() // argument
				push(&symbol.Symbol{Kind: symbol.Value, Type: s.Type})
				c.emit(t, vm.Convert, s.Index)
				break
			}
			if s.Kind != symbol.Value {
				typ := s.Type
				if typ == nil {
					return goparser.ErrUndefined{Name: s.Name}
				}
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
				if typ.Rtype.NumOut() == 0 && narg >= typ.Rtype.NumIn() {
					c.emit(t, vm.Pop, 1) // pop stale func value left by Return for void calls
				}
				break
			}
			fallthrough // A symValue must be called through callX.

		case lang.CallX:
			narg := t.Arg[0].(int)
			s := stack[len(stack)-1-narg]
			rtyp := s.Value.Reflect().Type()
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
					if ks.Value.CanInt() {
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
					typ = vm.TypeOf(r.Value.Interface())
				}
				// If lhs has an interface type, keep it and wrap the concrete value.
				if lhs[i].Type != nil && lhs[i].Type.IsInterface() && !typ.IsInterface() {
					c.registerMethods(lhs[i].Type, typ)
					c.emit(t, vm.IfaceWrap, c.typeIndex(typ))
					c.Data[lhs[i].Index] = vm.NewValue(lhs[i].Type.Rtype)
				} else {
					lhs[i].Type = typ
					c.Data[lhs[i].Index] = vm.NewValue(typ.Rtype)
				}
			}
			c.emit(t, vm.SetS, n)

		case lang.Assign:
			rhs := pop()
			lhs := pop()
			if lhs.Local {
				// Captured variable write inside closure body: use HSet.
				if cf := curFunc(); cf != "" {
					if cloSym := c.Symbols[cf]; cloSym != nil {
						if idx, captOK := cloSym.CapturedAs[lhs.Name]; captOK {
							c.emit(t, vm.HSet, idx)
							c.emit(t, vm.Pop, 1) // pop stale value pushed by HGet in Ident
							break
						}
					}
				}
				if !lhs.Used {
					c.emit(t, vm.New, lhs.Index, c.typeSym(lhs.Type).Index)
					lhs.Used = true
				}
				c.emit(t, vm.Set, 1, lhs.Index)
				c.emit(t, vm.Pop, 1) // pop stale lhs value left by Ident's Get
				break
			}
			// TODO check source type against var type
			if v := c.Data[lhs.Index]; !v.IsValid() {
				c.Data[lhs.Index] = vm.NewValue(rhs.Type.Rtype)
				c.Symbols[lhs.Name].Type = rhs.Type
			}
			// Wrap concrete value in Iface when assigning to interface variable.
			if lhs.Type != nil && lhs.Type.IsInterface() && rhs.Type != nil && !rhs.Type.IsInterface() {
				c.registerMethods(lhs.Type, rhs.Type)
				c.emit(t, vm.IfaceWrap, c.typeIndex(rhs.Type))
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
			// Closure creation: emit code address + captured cell pointers + MkClosure.
			if s.Kind == symbol.Func && len(s.FreeVars) > 0 {
				c.emit(t, vm.Get, vm.Global, s.Index)
				// Determine the current function's CapturedAs map for transitive capture.
				var outerCapturedAs map[string]int
				if cf := curFunc(); cf != "" {
					if cloSym := c.Symbols[cf]; cloSym != nil {
						outerCapturedAs = cloSym.CapturedAs
					}
				}
				for _, fvName := range s.FreeVars {
					fvSym := c.Symbols[fvName]
					if fvSym == nil {
						return errorf("free variable not found: %s", fvName)
					}
					if idx, ok := outerCapturedAs[fvName]; ok {
						// The free variable is already captured in the enclosing closure's Env.
						// Use HPtr to push the existing cell pointer (transitive capture).
						c.emit(t, vm.HPtr, idx)
					} else if fvSym.Local {
						c.emit(t, vm.Get, vm.Local, fvSym.Index)
						c.emit(t, vm.HAlloc)
					} else {
						c.emit(t, vm.Get, vm.Global, fvSym.Index)
						c.emit(t, vm.HAlloc)
					}
				}
				c.emit(t, vm.MkClosure, len(s.FreeVars))
				break
			}
			// Captured variable read inside a closure body: use HGet.
			if cf := curFunc(); cf != "" {
				if cloSym := c.Symbols[cf]; cloSym != nil {
					if idx, captOK := cloSym.CapturedAs[t.Str]; captOK {
						c.emit(t, vm.HGet, idx)
						break
					}
				}
			}
			// Regular local or global access.
			if s.Local {
				c.emit(t, vm.Get, vm.Local, s.Index)
			} else {
				if s.Index == symbol.UnsetAddr {
					s.Index = len(c.Data)
					if s.Kind == symbol.Type {
						c.Data = append(c.Data, vm.NewValue(s.Type.Rtype))
					} else {
						c.Data = append(c.Data, s.Value)
					}
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
					if s.Index == symbol.UnsetAddr {
						s.Index = len(c.Data)
						c.Data = append(c.Data, s.Value)
					} else {
						// Slot was pre-allocated by an Ident reference before this Label:
						// update in place so all Get Global N instructions already emitted
						// load the correct code address at runtime.
						c.Data[s.Index] = s.Value
					}
					flen = append(flen, len(stack))
					funcStack = append(funcStack, t.Str)
					// Register method in its receiver type's method table.
					if parts := strings.SplitN(t.Str, ".", 2); len(parts) == 2 {
						typeName := strings.TrimPrefix(parts[0], "*")
						if ts, ok := c.Symbols[typeName]; ok && ts.Kind == symbol.Type {
							id := c.methodID(parts[1])
							for len(ts.Type.Methods) <= id {
								ts.Type.Methods = append(ts.Type.Methods, vm.Method{Index: -1})
							}
							ts.Type.Methods[id] = vm.Method{Index: s.Index}
						}
					}
				} else {
					c.Data[s.Index] = s.Value
				}
			} else {
				if strings.HasSuffix(t.Str, "_end") {
					if s, ok = c.Symbols[strings.TrimSuffix(t.Str, "_end")]; ok && s.Kind == symbol.Func {
						// Exit function: restore caller stack and function name tracking.
						l := popflen()
						stack = stack[:l]
						funcStack = funcStack[:len(funcStack)-1]
					}
				}
				c.SymSet(t.Str, &symbol.Symbol{Kind: symbol.Label, Value: vm.ValueOf(lc)})
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
				p, ok := goparser.Packages[s.PkgPath]
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
					c.SymAdd(l, name, v, symbol.Value, vm.TypeOf(v.Interface()), false)
					sym = c.Symbols[name]
				}
				push(sym)
				c.emit(t, vm.Get, vm.Global, l)
			case symbol.Unset:
				return errorf("invalid symbol: %s", s.Name)
			default:
				// Dynamic dispatch for interface receiver.
				if s.Type != nil && s.Type.IsInterface() {
					methodName := t.Str[1:]
					// Find the method signature from a concrete implementation.
					// Look up any "TypeName.methodName" symbol in the table.
					var methodSym *symbol.Symbol
					for k, sym := range c.Symbols {
						if strings.HasSuffix(k, "."+methodName) && sym.Kind == symbol.Func {
							methodSym = sym
							break
						}
					}
					if methodSym == nil {
						return fmt.Errorf("method not found: %s", methodName)
					}
					push(methodSym)
					c.emit(t, vm.IfaceCall, c.methodID(methodName))
					break
				}
				if m, fieldPath := c.Symbols.MethodByName(s, t.Str[1:]); m != nil {
					push(m)
					// Extract embedded receiver if method is promoted through embedded fields.
					if len(fieldPath) > 0 {
						c.emit(t, vm.Field, fieldPath...)
					}
					// Determine if auto-deref or auto-addr is needed.
					methodWantsPtr := strings.HasPrefix(m.Name, "*")
					recvRtype := s.Type.Rtype
					if len(fieldPath) > 0 {
						for _, idx := range fieldPath {
							if recvRtype.Kind() == reflect.Pointer {
								recvRtype = recvRtype.Elem()
							}
							recvRtype = recvRtype.Field(idx).Type
						}
					}
					recvIsPtr := recvRtype.Kind() == reflect.Pointer
					switch {
					case methodWantsPtr && !recvIsPtr:
						c.emit(t, vm.Addr)
					case !methodWantsPtr && recvIsPtr:
						c.emit(t, vm.Deref)
					}
					// Closure-based method dispatch.
					// VM stack before Period: [..., receiver_value]
					// HAlloc: wrap receiver in a heap cell.
					// Get Global m.Index: push method code address above the cell.
					// Swap 0 1: put code addr below cell (MkClosure convention: code at sp-n-1).
					// MkClosure 1: produce Closure{code, [receiver_cell]}.
					c.emit(t, vm.HAlloc)
					c.emit(t, vm.Get, vm.Global, m.Index)
					c.emit(t, vm.Swap, 0, 1)
					c.emit(t, vm.MkClosure, 1)
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
					c.Data[k.Index] = vm.NewValue(k.Type.Rtype)
					c.emit(t, vm.Pull)
				case 2:
					k, v := stack[len(stack)-3], stack[len(stack)-2]
					k.Type = c.Symbols["int"].Type
					v.Type = typ.Elem()
					c.Data[k.Index] = vm.NewValue(k.Type.Rtype)
					c.Data[v.Index] = vm.NewValue(v.Type.Rtype)
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

// numericOp returns a per-type opcode computed as base + type offset.
// If the type is not a numeric type, it returns the fallback opcode.
func numericOp(base, fallback vm.Op, typ *vm.Type) vm.Op {
	if typ == nil || int(typ.Rtype.Kind()) >= len(vm.NumKindOffset) { //nolint:gosec
		return fallback
	}
	off := vm.NumKindOffset[typ.Rtype.Kind()]
	if off < 0 {
		return fallback
	}
	return base + vm.Op(off)
}

// PrintCode pretty prints the generated code.
func (c *Compiler) PrintCode() {
	labels := map[int][]string{} // labels indexed by code location
	data := map[int]string{}     // data indexed by frame location

	for name, sym := range c.Symbols {
		if sym.Kind == symbol.Label || sym.Kind == symbol.Func {
			if !sym.Value.IsValid() {
				continue
			}
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
			fmt.Fprintf(os.Stderr, "%4d %T %v, Symbol: %v\n", i, d.Interface(), d.Reflect(), dict[i])
		} else {
			fmt.Fprintf(os.Stderr, "%4d %v %v\n", i, d.Reflect(), dict[i])
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
		c.SymSet(t.Rtype.String(), tsym)
	}
	if tsym.Index == symbol.UnsetAddr {
		tsym.Index = len(c.Data)
		c.Data = append(c.Data, vm.NewValue(t.Rtype))
	}
	return tsym
}
