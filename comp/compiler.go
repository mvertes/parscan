// Package comp implements a byte code generator targeting the vm.
package comp

import (
	"errors"
	"fmt"
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

const debug = false

var builtinDeferOp = map[string]vm.Op{
	"print":   vm.Print,
	"println": vm.Println,
	"close":   vm.ChanClose,
	"delete":  vm.DeleteMap,
	"copy":    vm.CopySlice,
}

// Compiler represents the state of a compiler.
type Compiler struct {
	*goparser.Parser
	vm.Code            // produced code, to fill VM with
	Data    []vm.Value // produced data, will be at the bottom of VM stack
	Entry   int        // offset in Code to start execution from

	strings   map[string]int                  // locations of strings in Data
	methodIDs map[string]int                  // global method ID by method name
	typeIdxs  map[*vm.Type]int                // dedup cache for typeIndex, keyed by parscan type pointer
	typeSyms  map[reflect.Type]*symbol.Symbol // dedup cache for typeSym, keyed by reflect.Type
}

// NewCompiler returns a new compiler state for a given scanner.
func NewCompiler(spec *lang.Spec) *Compiler {
	return &Compiler{
		Parser:    goparser.NewParser(spec, true),
		Entry:     -1,
		strings:   map[string]int{},
		methodIDs: map[string]int{},
		typeIdxs:  map[*vm.Type]int{},
		typeSyms:  map[reflect.Type]*symbol.Symbol{},
	}
}

// Compile parses src and generates code and data, or returns a non-nil error.
// Code and data are added incrementally in c.Code and C.Data.
// name identifies the source ("m:<content>" for inline, "f:<path>" for file).
func (c *Compiler) Compile(name, src string) error {
	remaining, err := c.ParseAll(name, src)
	if err != nil {
		return err
	}
	c.allocGlobalSlots()
	var rest []goparser.Tokens
	for _, decl := range remaining {
		if len(decl) > 0 && decl[0].Tok == lang.Var {
			if err := c.compileDecl(decl); err != nil {
				return err
			}
		} else {
			rest = append(rest, decl)
		}
	}
	for _, decl := range rest {
		if err := c.compileDecl(decl); err != nil {
			return err
		}
	}
	return nil
}

func (c *Compiler) compileDecl(decl goparser.Tokens) error {
	toks, err := c.ParseOneStmt(decl)
	if err != nil {
		return err
	}
	return c.generate(toks)
}

func (c *Compiler) allocGlobalSlots() {
	for _, s := range c.Symbols {
		if s.Index != symbol.UnsetAddr {
			continue
		}
		switch s.Kind {
		case symbol.Func:
			s.Index = len(c.Data)
			c.Data = append(c.Data, s.Value)
		case symbol.Var:
			s.Index = len(c.Data)
			v := s.Value
			if !v.IsValid() && s.Type != nil {
				v = vm.NewValue(s.Type.Rtype)
			}
			c.Data = append(c.Data, v)
		}
	}
}

func (c *Compiler) methodID(name string) int {
	if id, ok := c.methodIDs[name]; ok {
		return id
	}
	id := len(c.methodIDs)
	c.methodIDs[name] = id
	return id
}

// MethodNames returns the reverse mapping of global method IDs to names.
func (c *Compiler) MethodNames() []string {
	names := make([]string, len(c.methodIDs))
	for name, id := range c.methodIDs {
		names[id] = name
	}
	return names
}

func (c *Compiler) typeIndex(typ *vm.Type) int {
	if i, ok := c.typeIdxs[typ]; ok {
		return i
	}
	i := len(c.Data)
	c.Data = append(c.Data, vm.ValueOf(typ))
	c.typeIdxs[typ] = i
	return i
}

func (c *Compiler) findTypeSym(rtype reflect.Type) *vm.Type {
	for _, sym := range c.Symbols {
		if sym.Kind == symbol.Type && sym.Type != nil && sym.Type.Rtype == rtype {
			return sym.Type
		}
	}
	return nil
}

func (c *Compiler) registerMethods(iface, typ *vm.Type) {
	isPtr := typ.Rtype.Kind() == reflect.Pointer
	lookupTyp := typ
	if isPtr {
		if t := c.findTypeSym(typ.Rtype.Elem()); t != nil {
			lookupTyp = t
		}
	}
	for _, im := range iface.IfaceMethods {
		id := c.methodID(im.Name)
		if id < len(typ.Methods) && (typ.Methods[id].Index >= 0 || typ.Methods[id].EmbedIface) {
			continue // already registered directly or through embedded interface
		}
		s := &symbol.Symbol{Kind: symbol.Var, Name: lookupTyp.Name, Type: lookupTyp}
		m, fieldPath := c.Symbols.MethodByName(s, im.Name)
		if m == nil {
			// MethodByName only finds concrete function symbols; interface methods have none.
			for _, emb := range lookupTyp.Embedded {
				embType := emb.Type
				if embType == nil || !embType.IsInterface() {
					continue
				}
				for _, embIM := range embType.IfaceMethods {
					if embIM.Name != im.Name {
						continue
					}
					for len(typ.Methods) <= id {
						typ.Methods = append(typ.Methods, vm.Method{Index: -1})
					}
					typ.Methods[id] = vm.Method{Index: -1, Path: []int{emb.FieldIdx}, EmbedIface: true}
					break
				}
			}
			continue
		}
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
	if debug {
		_, file, line, _ := runtime.Caller(1)
		fmt.Fprintf(os.Stderr, "%s%d: showstack: %d\n", path.Base(file), line, len(stack))
		for i, s := range stack {
			fmt.Fprintf(os.Stderr, "  stack[%d]: %v\n", i, s)
		}
	}
}

func (c *Compiler) emit(t goparser.Token, op vm.Op, arg ...int) {
	if debug {
		_, file, line, _ := runtime.Caller(1)
		fmt.Fprintf(os.Stderr, "%s:%d: %v emit %v %v\n", path.Base(file), line, t, op, arg)
	}
	inst := vm.Instruction{Op: op, Pos: vm.Pos(t.Pos + c.PosBase)} //nolint:gosec
	if len(arg) > 0 {
		inst.A = int32(arg[0]) //nolint:gosec
	}
	if len(arg) > 1 {
		inst.B = int32(arg[1]) //nolint:gosec
	}
	// Field/FieldSet encode a variable-length field index path in A, B.
	// Unused trailing B must be -1 so the VM can distinguish path length.
	if op == vm.Field || op == vm.FieldSet {
		if len(arg) < 2 {
			inst.B = -1
		}
	}
	c.Code = append(c.Code, inst)
}

func (c *Compiler) emitField(t goparser.Token, path []int) {
	for len(path) > 2 {
		c.emit(t, vm.Field, path[0], path[1])
		path = path[2:]
	}
	c.emit(t, vm.Field, path...)
}

func (c *Compiler) emitIfaceWrap(t goparser.Token, ifaceTyp, concreteTyp *vm.Type) {
	c.emitIfaceWrapAt(t, ifaceTyp, concreteTyp, 0)
}

func (c *Compiler) emitIfaceWrapAt(t goparser.Token, ifaceTyp, concreteTyp *vm.Type, depth int) {
	if ifaceTyp == nil || !ifaceTyp.IsInterface() || concreteTyp == nil || concreteTyp.IsInterface() {
		return
	}
	c.registerMethods(ifaceTyp, concreteTyp)
	c.emit(t, vm.IfaceWrap, c.typeIndex(concreteTyp), depth)
}

// emitTypeOrGlobal emits Fnew (or FnewE) for type symbols, or GetGlobal for values.
func (c *Compiler) emitTypeOrGlobal(t goparser.Token, sym *symbol.Symbol, index int) {
	if sym.Kind == symbol.Type {
		switch sym.Type.Rtype.Kind() {
		case reflect.Slice:
			c.emit(t, vm.Fnew, index, 0)
		case reflect.Pointer:
			c.emit(t, vm.FnewE, index, 1)
		default:
			c.emit(t, vm.Fnew, index, 1)
		}
	} else {
		c.emit(t, vm.GetGlobal, index)
	}
}

// generate generates vm code and data from parsed tokens, or returns an error.
func (c *Compiler) generate(tokens goparser.Tokens) (err error) {
	fixList := goparser.Tokens{}  // list of tokens to fix after all necessary information is gathered
	stack := []*symbol.Symbol{}   // for symbolic evaluation and type checking
	flen := []int{}               // stack length according to function scopes
	funcStack := []string{}       // names of functions currently being compiled
	jumpDepth := map[string]int{} // expected compile-stack depth at short-circuit merge labels
	exprBase := -1                // compile-stack depth at start of current expression statement (-1 = not in expr stmt)
	growPos := []int{}            // code positions of Grow instructions per function scope
	maxExprDepth := []int{}       // max expression depth above locals per function scope
	hasDefer := []bool{}          // whether current function scope uses defer

	push := func(s *symbol.Symbol) {
		stack = append(stack, s)
		if len(maxExprDepth) > 0 {
			if d := len(stack) - flen[len(flen)-1]; d > maxExprDepth[len(maxExprDepth)-1] {
				maxExprDepth[len(maxExprDepth)-1] = d
			}
		}
	}
	top := func() *symbol.Symbol { return stack[len(stack)-1] }
	pop := func() *symbol.Symbol { l := len(stack) - 1; s := stack[l]; stack = stack[:l]; return s }
	// checkTopN returns ErrUndefined if any of the top n stack entries is an unresolved
	// identifier (Unset with a non-empty Name). Anonymous Unset entries (Name=="") are
	// legitimate intermediate values (e.g. field-access results) and are not checked.
	checkTopN := func(n int) error {
		for j := 0; j < n; j++ {
			if i := len(stack) - 1 - j; i >= 0 && stack[i].Kind == symbol.Unset && stack[i].Name != "" {
				return goparser.ErrUndefined{Name: stack[i].Name}
			}
		}
		return nil
	}
	popflen := func() int { le := len(flen) - 1; l := flen[le]; flen = flen[:le]; return l }
	curFunc := func() string {
		if n := len(funcStack); n > 0 {
			return funcStack[n-1]
		}
		return ""
	}
	// isCallable reports whether sym can be the target of a function call.
	isCallable := func(sym *symbol.Symbol) bool {
		if sym.Kind == symbol.Func || sym.Kind == symbol.Builtin {
			return true
		}
		if sym.Type != nil {
			return sym.Type.Rtype.Kind() == reflect.Func
		}
		rv := sym.Value.Reflect()
		return rv.IsValid() && rv.Kind() == reflect.Func
	}

	for _, t := range tokens {
		switch t.Tok {
		case lang.Int:
			n64, err := strconv.ParseInt(t.Str, 0, 64)
			if err != nil {
				// Try unsigned parse for large literals (e.g. MaxUint64).
				u64, uerr := strconv.ParseUint(t.Str, 0, 64)
				if uerr != nil {
					return err
				}
				n64 = int64(u64) //nolint:gosec
			}
			n := int(n64)
			push(&symbol.Symbol{Kind: symbol.Const, Value: vm.ValueOf(n), Type: c.Symbols["int"].Type})
			if n >= -1<<31 && n < 1<<31 {
				c.emit(t, vm.Push, n)
			} else {
				// Large constant: store in data segment and load via Get.
				di := len(c.Data)
				c.Data = append(c.Data, vm.ValueOf(n))
				c.emit(t, vm.GetGlobal, di)
			}

		case lang.Float:
			f, err := strconv.ParseFloat(t.Str, 64)
			if err != nil {
				return err
			}
			v := vm.ValueOf(f)
			di := len(c.Data)
			c.Data = append(c.Data, v)
			push(&symbol.Symbol{Kind: symbol.Const, Value: v, Type: c.Symbols["float64"].Type})
			c.emit(t, vm.GetGlobal, di)

		case lang.String:
			if t.Prefix() == "'" {
				r, _, _, err2 := strconv.UnquoteChar(t.Block(), '\'')
				if err2 != nil {
					return err2
				}
				push(&symbol.Symbol{Kind: symbol.Const, Value: vm.ValueOf(r), Type: c.Symbols["rune"].Type})
				c.emit(t, vm.Push, int(r))
				break
			}
			s, err2 := strconv.Unquote(t.Str)
			if err2 != nil {
				return err2
			}
			push(&symbol.Symbol{Kind: symbol.Const, Value: vm.ValueOf(s), Type: c.Symbols["string"].Type})
			c.emit(t, vm.GetGlobal, c.stringIndex(s))

		case lang.Add, lang.Mul, lang.Sub, lang.Quo, lang.Rem:
			if err := checkTopN(2); err != nil {
				return err
			}
			right, left := pop(), pop()
			typ := arithmeticOpType(right, left)
			c.emitConstConvert(t, right, typ, 0)
			c.emitConstConvert(t, left, typ, 1)
			push(&symbol.Symbol{Kind: constKind(right, left), Type: typ})
			switch t.Tok {
			case lang.Add:
				c.emitArithmeticOp(t, right, typ, vm.AddInt, vm.AddIntImm, vm.GetLocalAddIntImm, vm.AddStr)
			case lang.Mul:
				c.emitArithmeticOp(t, right, typ, vm.MulInt, vm.MulIntImm, vm.GetLocalMulIntImm, 0)
			case lang.Sub:
				c.emitArithmeticOp(t, right, typ, vm.SubInt, vm.SubIntImm, vm.GetLocalSubIntImm, 0)
			case lang.Quo:
				c.emitArithmeticOp(t, right, typ, vm.DivInt, 0, 0, 0)
			case lang.Rem:
				c.emitArithmeticOp(t, right, typ, vm.RemInt, 0, 0, 0)
			}

		case lang.Minus:
			if err := checkTopN(1); err != nil {
				return err
			}
			typ := symbol.Vtype(top())
			c.emit(t, numericOp(vm.NegInt, typ))

		case lang.Not:
			if err := checkTopN(1); err != nil {
				return err
			}
			c.emit(t, vm.Not)

		case lang.Plus:
			// Unary '+' is idempotent. Nothing to do.

		case lang.Addr:
			if err := checkTopN(1); err != nil {
				return err
			}
			push(&symbol.Symbol{Kind: symbol.Value, Type: vm.PointerTo(pop().Type)})
			if n := len(c.Code); n > 0 && c.Code[n-1].Op == vm.Index {
				c.Code[n-1].Op = vm.IndexAddr
			} else {
				c.emit(t, vm.Addr)
			}

		case lang.Deref:
			if err := checkTopN(1); err != nil {
				return err
			}
			s := pop()
			if s.Type == nil {
				return goparser.ErrUndefined{Name: s.Name}
			}
			if !s.Type.IsPtr() {
				return errorf("cannot dereference non-pointer type %v", s.Type)
			}
			push(&symbol.Symbol{Kind: symbol.Value, Type: s.Type.Elem()})
			c.emit(t, vm.Deref)

		case lang.TypeAssert:
			if err := checkTopN(1); err != nil {
				return err
			}
			okForm := t.Arg[0].(int)
			typ := t.Arg[1].(*vm.Type)
			if typ.IsInterface() && len(typ.IfaceMethods) > 0 && typ.IfaceMethods[0].ID < 0 {
				for i, im := range typ.IfaceMethods {
					typ.IfaceMethods[i].ID = c.methodID(im.Name)
				}
			}
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
			c.emit(t, vm.TypeBranch, c.resolveLabel(t, &fixList), typeIdx)

		case lang.Index:
			if err := checkTopN(2); err != nil {
				return err
			}
			okForm := len(t.Arg) > 0 && t.Arg[0].(int) == 1
			pop()
			s := pop()
			vt := symbol.Vtype(s)
			if vt == nil {
				return goparser.ErrUndefined{Name: s.Name}
			}
			if vt.IsPtr() {
				vt = vt.Elem()
			}
			var elemType *vm.Type
			switch vt.Rtype.Kind() {
			case reflect.Map:
				elemType = vt.Elem()
				if okForm {
					c.emit(t, vm.MapIndexOk)
					push(&symbol.Symbol{Kind: symbol.Value, Type: elemType})
					push(&symbol.Symbol{Kind: symbol.Value, Type: c.Symbols["bool"].Type})
				} else {
					c.emit(t, vm.MapIndex)
					push(&symbol.Symbol{Kind: symbol.Value, Type: elemType})
				}
			case reflect.String:
				c.emit(t, vm.Index)
				elemType = c.Symbols["uint8"].Type
				push(&symbol.Symbol{Kind: symbol.Value, Type: elemType})
			default:
				c.emit(t, vm.Index)
				elemType = vt.Elem()
				push(&symbol.Symbol{Kind: symbol.Value, Type: elemType})
			}

		case lang.Greater, lang.Less, lang.GreaterEqual, lang.LessEqual:
			if err := checkTopN(2); err != nil {
				return err
			}
			s2, s1 := pop(), pop()
			typ := symbol.Vtype(s1)
			push(&symbol.Symbol{Kind: symbol.Value, Type: booleanOpType(s2, s1)})
			switch t.Tok {
			case lang.Greater:
				c.emitComparisonOp(t, s2, typ, vm.GreaterInt,
					vm.GreaterIntImm, vm.GreaterUintImm,
					vm.GetLocalGreaterIntImm, vm.GetLocalGreaterUintImm, false)
			case lang.Less:
				c.emitComparisonOp(t, s2, typ, vm.LowerInt,
					vm.LowerIntImm, vm.LowerUintImm,
					vm.GetLocalLowerIntImm, vm.GetLocalLowerUintImm, false)
			case lang.GreaterEqual:
				c.emitComparisonOp(t, s2, typ, vm.LowerInt,
					vm.LowerIntImm, vm.LowerUintImm, 0, 0, true)
			case lang.LessEqual:
				c.emitComparisonOp(t, s2, typ, vm.GreaterInt,
					vm.GreaterIntImm, vm.GreaterUintImm, 0, 0, true)
			}

		case lang.NotEqual:
			if err := checkTopN(2); err != nil {
				return err
			}
			push(&symbol.Symbol{Type: booleanOpType(pop(), pop())})
			c.emit(t, vm.Equal)
			c.emit(t, vm.Not)

		case lang.And:
			if err := checkTopN(2); err != nil {
				return err
			}
			typ := arithmeticOpType(pop(), pop())
			push(&symbol.Symbol{Kind: symbol.Value, Type: typ})
			c.emit(t, vm.BitAnd)

		case lang.Or:
			if err := checkTopN(2); err != nil {
				return err
			}
			typ := arithmeticOpType(pop(), pop())
			push(&symbol.Symbol{Kind: symbol.Value, Type: typ})
			c.emit(t, vm.BitOr)

		case lang.Xor:
			if err := checkTopN(2); err != nil {
				return err
			}
			typ := arithmeticOpType(pop(), pop())
			push(&symbol.Symbol{Kind: symbol.Value, Type: typ})
			c.emit(t, vm.BitXor)

		case lang.AndNot:
			if err := checkTopN(2); err != nil {
				return err
			}
			typ := arithmeticOpType(pop(), pop())
			push(&symbol.Symbol{Kind: symbol.Value, Type: typ})
			c.emit(t, vm.BitAndNot)

		case lang.Shl:
			if err := checkTopN(2); err != nil {
				return err
			}
			pop()         // shift amount
			left := pop() // left operand
			leftTyp := shiftLeftType(left, c.Symbols["int"].Type)
			c.emitConstConvert(t, left, leftTyp, 1)
			push(&symbol.Symbol{Kind: symbol.Value, Type: leftTyp})
			c.emit(t, vm.BitShl)

		case lang.Shr:
			if err := checkTopN(2); err != nil {
				return err
			}
			pop()         // shift amount
			left := pop() // left operand
			leftTyp := shiftLeftType(left, c.Symbols["int"].Type)
			c.emitConstConvert(t, left, leftTyp, 1)
			push(&symbol.Symbol{Kind: symbol.Value, Type: leftTyp})
			c.emit(t, vm.BitShr)

		case lang.BitComp:
			if err := checkTopN(1); err != nil {
				return err
			}
			c.emit(t, vm.BitComp)

		case lang.Arrow: // unary channel receive: <-ch
			if err := checkTopN(1); err != nil {
				return err
			}
			okForm := 0
			if len(t.Arg) > 0 {
				okForm = t.Arg[0].(int)
			}
			ch := pop()
			if ch.Type.Rtype.Kind() != reflect.Chan {
				return errorf("invalid channel receive: not a channel type")
			}
			elemType := ch.Type.Elem()
			push(&symbol.Symbol{Kind: symbol.Value, Type: elemType})
			if okForm == 1 {
				push(&symbol.Symbol{Kind: symbol.Value, Type: c.Symbols["bool"].Type})
			}
			c.emit(t, vm.ChanRecv, okForm)

		case lang.Call:
			narg := t.Arg[0].(int)
			spread := len(t.Arg) > 1 && t.Arg[1].(int) != 0
			if err := checkTopN(narg); err != nil {
				return err
			}
			s := stack[len(stack)-1-narg]
			// If s is a non-callable plain Value, arguments may have been expanded
			// from a multi-return call (e.g. g(f()) where f returns 2 values).
			// Only Value symbols (not Type, Func, etc.) indicate expansion.
			// Search backward for the real function symbol.
			if s.Kind == symbol.Value && !isCallable(s) {
				for i := narg + 1; i < len(stack); i++ {
					if candidate := stack[len(stack)-1-i]; isCallable(candidate) {
						s = candidate
						narg = i
						break
					}
				}
			}
			if ok, err := c.compileBuiltin(s, narg, t, &stack, push, pop, top); ok {
				if err != nil {
					return err
				}
				break
			}
			if ok, err := c.compileIntrinsic(s, narg, t, push, pop); ok {
				if err != nil {
					return err
				}
				break
			}
			if s.Kind == symbol.Type {
				if narg != 1 {
					return errorf("type conversion requires exactly one argument")
				}
				c.removeFnew(s.Index)
				arg := pop() // argument (top of stack)
				pop()        // type symbol
				push(&symbol.Symbol{Kind: symbol.Value, Type: s.Type})
				if s.Type.IsInterface() {
					c.emitIfaceWrap(t, s.Type, arg.Type)
				} else {
					c.emit(t, vm.Convert, s.Index)
				}
				break
			}
			if s.Kind != symbol.Value {
				typ := s.Type
				if typ == nil {
					return goparser.ErrUndefined{Name: s.Name}
				}
				// Wrap concrete args in Iface when the parameter expects an interface type.
				// Use parscan-level Params types (which carry IfaceMethods) when available.
				nIn := typ.Rtype.NumIn()
				for k := 0; k < narg && k < nIn; k++ {
					argSym := stack[len(stack)-narg+k]
					if argSym.Type == nil || argSym.Type.IsInterface() {
						continue
					}
					var ifaceTyp *vm.Type
					if k < len(typ.Params) {
						ifaceTyp = typ.Params[k]
					} else {
						ifaceTyp = &vm.Type{Rtype: typ.Rtype.In(k)}
					}
					depth := narg - 1 - k
					c.emitIfaceWrapAt(t, ifaceTyp, argSym.Type, depth)
					if !ifaceTyp.IsInterface() {
						c.emitConstConvert(t, argSym, ifaceTyp, depth)
					}
				}
				// Type switches on variadic slice elements require Iface wrapping at the call site.
				// For spread calls (f(s...)), the slice is pre-built; skip per-element wrapping.
				if typ.Rtype.IsVariadic() && !spread {
					nFixed := typ.Rtype.NumIn() - 1
					elemType := typ.Rtype.In(nFixed).Elem()
					if elemType.Kind() == reflect.Interface {
						elemTyp := &vm.Type{Rtype: elemType}
						for k := nFixed; k < narg; k++ {
							argSym := stack[len(stack)-narg+k]
							if argSym.Type == nil || argSym.Type.IsInterface() {
								continue
							}
							c.emitIfaceWrapAt(t, elemTyp, argSym.Type, narg-1-k)
						}
					}
				}
				// Pop function and input arg symbols, push return value symbols.
				pop()
				for i := 0; i < narg; i++ {
					pop()
				}
				nret := typ.Rtype.NumOut()
				for i := 0; i < nret; i++ {
					push(&symbol.Symbol{Kind: symbol.Value, Type: typ.ReturnType(i)})
				}
				callNarg := narg
				if typ.Rtype.IsVariadic() {
					nFixed := typ.Rtype.NumIn() - 1
					if !spread {
						// Pack trailing arguments into a slice for the variadic parameter.
						nExtra := narg - nFixed
						elemType := typ.Rtype.In(nFixed).Elem()
						elemIdx := c.typeSym(&vm.Type{Rtype: elemType}).Index
						c.emit(t, vm.MkSlice, nExtra, elemIdx)
					}
					callNarg = nFixed + 1
				}
				// Direct call to a declared function (no closure): use CallImm
				// to avoid loading the func value and skip type dispatch at runtime.
				if s.Kind == symbol.Func && len(s.FreeVars) == 0 && c.removeGetGlobal(s.Index) {
					c.emit(t, vm.CallImm, s.Index, callNarg<<16|nret)
				} else {
					c.emit(t, vm.Call, callNarg, nret)
				}
				break
			}
			// s.Kind == symbol.Value: function value on stack (native Go func or returned parscan closure).
			var rtyp reflect.Type
			if rv := s.Value.Reflect(); rv.IsValid() {
				rtyp = rv.Type()
			} else if s.Type != nil {
				rtyp = s.Type.Rtype
			}
			// Wrap concrete args in Iface when the parameter expects an interface type.
			if rtyp != nil && rtyp.Kind() == reflect.Func {
				nIn := rtyp.NumIn()
				for k := 0; k < narg && k < nIn; k++ {
					argSym := stack[len(stack)-narg+k]
					if argSym.Type == nil || argSym.Type.IsInterface() {
						continue
					}
					ifaceTyp := &vm.Type{Rtype: rtyp.In(k)}
					depth := narg - 1 - k
					c.emitIfaceWrapAt(t, ifaceTyp, argSym.Type, depth)
					if !ifaceTyp.IsInterface() {
						c.emitConstConvert(t, argSym, ifaceTyp, depth)
					}
				}
				if rtyp.IsVariadic() && !spread {
					nFixed := nIn - 1
					elemType := rtyp.In(nFixed).Elem()
					if elemType.Kind() == reflect.Interface {
						elemTyp := &vm.Type{Rtype: elemType}
						for k := nFixed; k < narg; k++ {
							argSym := stack[len(stack)-narg+k]
							if argSym.Type == nil || argSym.Type.IsInterface() {
								continue
							}
							c.emitIfaceWrapAt(t, elemTyp, argSym.Type, narg-1-k)
						}
					}
				}
			}
			// Pop function and input arg symbols, push return value symbols.
			for i := 0; i < narg+1; i++ {
				pop()
			}
			nret := 0
			if rtyp != nil {
				nret = rtyp.NumOut()
				for i := 0; i < nret; i++ {
					var retType *vm.Type
					if s.Type != nil {
						retType = s.Type.ReturnType(i)
					} else {
						retType = &vm.Type{Rtype: rtyp.Out(i)}
					}
					push(&symbol.Symbol{Kind: symbol.Value, Type: retType})
				}
			}
			c.emit(t, vm.Call, narg, nret)

		case lang.Colon:
			vs := pop() // value
			ks := pop() // key or index
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
						fieldIdx := int(ks.Value.Int()) //nolint:gosec
						if fieldIdx < len(ts.Type.Fields) {
							ft := ts.Type.Fields[fieldIdx]
							if ft != nil && ft.Rtype.Kind() == reflect.Func {
								c.emit(t, vm.WrapFunc, c.typeIndex(ft))
							}
							c.emitIfaceWrap(t, ft, vs.Type)
						}
						c.emit(t, vm.FieldFset)
					}
				case reflect.Array, reflect.Slice:
					if ts.Type.Elem().IsPtr() && vs.Kind == symbol.Type {
						c.emit(t, vm.Addr)
					}
					c.emitIfaceWrap(t, ts.Type.Elem(), vs.Type)
					c.emit(t, vm.IndexSet)
				case reflect.Map:
					elemTyp := ts.Type.Elem()
					if elemTyp.IsPtr() && vs.Kind == symbol.Type {
						c.emit(t, vm.Addr)
					}
					c.emitIfaceWrap(t, elemTyp, vs.Type)
					c.emit(t, vm.MapSet)
				}

			case symbol.Type, symbol.Unset:
				fieldName := ks.Name
				if ks.Kind == symbol.Type {
					// Field name matches a type name: Ident emitted a spurious Fnew for it.
					if ts.Type.Rtype.Kind() != reflect.Struct || ks.Type == nil {
						break
					}
					fieldName = ks.Type.Name
				}
				j, ft := ts.Type.FieldLookup(fieldName)
				if j == nil {
					break
				}
				if ks.Kind == symbol.Type {
					c.removeFnew(ks.Index)
				}
				if ft != nil && ft.Rtype.Kind() == reflect.Func {
					c.emit(t, vm.WrapFunc, c.typeIndex(ft))
				}
				c.emitIfaceWrap(t, ft, vs.Type)
				c.emit(t, vm.FieldSet, j...)

			case symbol.LocalVar, symbol.Var:
				if ts.Type == nil || ts.Type.Rtype.Kind() != reflect.Struct {
					break
				}
				fieldName := ks.Name
				if j := strings.LastIndex(fieldName, "/"); j >= 0 {
					fieldName = fieldName[j+1:]
				}
				j, ft := ts.Type.FieldLookup(fieldName)
				if j == nil {
					break
				}
				if ks.Kind == symbol.LocalVar {
					c.removeGetLocal(ks.Index)
				} else {
					c.removeGetGlobal(ks.Index)
				}
				if ft != nil && ft.Rtype.Kind() == reflect.Func {
					c.emit(t, vm.WrapFunc, c.typeIndex(ft))
				}
				c.emitIfaceWrap(t, ft, vs.Type)
				c.emit(t, vm.FieldSet, j...)

			case symbol.Value:
				if ts.Type != nil && ts.Type.Rtype.Kind() == reflect.Map {
					elemTyp := ts.Type.Elem()
					if elemTyp.IsPtr() && vs.Kind == symbol.Type {
						c.emit(t, vm.Addr)
					}
					c.emitIfaceWrap(t, elemTyp, vs.Type)
					c.emit(t, vm.MapSet)
				}
			}

		case lang.Composite:
			sliceLen := t.Arg[0].(int)
			if sliceLen > 0 {
				idx := int32(c.Symbols[t.Str].Index) //nolint:gosec
				for i := len(c.Code) - 1; i >= 0; i-- {
					if c.Code[i].Op == vm.Fnew && c.Code[i].A == idx {
						c.Code[i].B = int32(sliceLen) //nolint:gosec
						break
					}
				}
			}

		case lang.Grow:
			growPos = append(growPos, len(c.Code))
			maxExprDepth = append(maxExprDepth, 0)
			hasDefer = append(hasDefer, false)
			c.emit(t, vm.Grow, t.Arg[0].(int))

		case lang.Define:
			showStack(stack)
			n := t.Arg[0].(int)
			if err := checkTopN(n); err != nil {
				return err
			}
			l := len(stack)
			rhs := stack[l-n:]
			stack = stack[:l-n]
			l = len(stack)
			lhs := stack[l-n:]
			stack = stack[:l-n]
			showStack(stack)
			// Local define: initialize local slots and assign via Set.
			if n > 0 && lhs[0].Kind == symbol.LocalVar {
				for i, r := range rhs {
					typ := r.Type
					if typ == nil {
						if !r.Value.Reflect().IsValid() {
							return goparser.ErrUndefined{Name: lhs[i].Name}
						}
						typ = vm.TypeOf(r.Value.Interface())
					}
					lhs[i].Type = typ
					if !lhs[i].NeedsCell() {
						c.emit(t, vm.New, lhs[i].Index, c.typeSym(typ).Index)
					}
					lhs[i].Used = true
				}
				for i := n - 1; i >= 0; i-- {
					if lhs[i].NeedsCell() {
						c.emit(t, vm.HeapAlloc)
						lhs[i].CellSlot = true
					}
					c.emit(t, vm.SetLocal, lhs[i].Index, 0)
				}
				c.emit(t, vm.Pop, n)
				break
			}
			for i, r := range rhs {
				// Propage type of rhs to lhs.
				typ := r.Type
				if typ == nil {
					if !r.Value.Reflect().IsValid() {
						return goparser.ErrUndefined{Name: lhs[i].Name}
					}
					typ = vm.TypeOf(r.Value.Interface())
				}
				// If lhs has an interface type, keep it and wrap the concrete value.
				if lhs[i].Type != nil && lhs[i].Type.IsInterface() && !typ.IsInterface() {
					c.emitIfaceWrap(t, lhs[i].Type, typ)
					c.Data[lhs[i].Index] = vm.NewValue(lhs[i].Type.Rtype)
				} else {
					lhs[i].Type = typ
					c.Data[lhs[i].Index] = vm.NewValue(typ.Rtype)
				}
			}
			c.emit(t, vm.SetS, n)

		case lang.Assign:
			n := t.Arg[0].(int)
			if err := checkTopN(n); err != nil { // check rhs values (top n items)
				return err
			}
			if n > 1 {
				// Batched multi-assign: compiler stack has [lhs0..lhs_(n-1), rhs0..rhs_(n-1)].
				// All RHS were pushed before any assignment, so swaps like a,b=b,a work correctly.
				l := len(stack)
				rhss := stack[l-n:]
				stack = stack[:l-n]
				lhss := stack[len(stack)-n:]
				stack = stack[:len(stack)-n]
				// Process from top of stack (rhs[n-1]) down to rhs[0].
				// Blank idents (Kind=Unset) have no slot on the VM stack; just discard their rhs.
				slotCount, namedAbove := 0, 0
				for i := n - 1; i >= 0; i-- {
					if lhss[i].Kind == symbol.Unset {
						c.emit(t, vm.Pop, 1) // discard rhs for blank ident
						continue
					}
					c.emitIfaceWrap(t, lhss[i].Type, rhss[i].Type)
					switch {
					case lhss[i].Kind == symbol.LocalVar:
						if lhss[i].CellSlot {
							c.emit(t, vm.CellSet, lhss[i].Index)
						} else {
							c.emit(t, vm.SetLocal, lhss[i].Index, 0)
						}
						slotCount++
						namedAbove++
					case lhss[i].Index != symbol.UnsetAddr:
						c.emit(t, vm.SetGlobal, lhss[i].Index, 0)
						slotCount++
						namedAbove++
					default:
						// Struct-field lhs (Index==UnsetAddr): field reflect.Value is on the VM
						// stack at depth D = namedAbove + i + 1 below the rhs at the top.
						// Bubble it to sp-2 via D-1 Swaps, then SetS(1) assigns and pops both.
						d := namedAbove + i + 1
						for j := 0; j < d-1; j++ {
							c.emit(t, vm.Swap, d-j, d-j-1)
						}
						c.emit(t, vm.SetS, 1)
					}
				}
				if slotCount > 0 {
					c.emit(t, vm.Pop, slotCount) // pop lhs copies for local/global vars
				}
				break
			}
			rhs := pop()
			lhs := pop()
			if lhs.Kind == symbol.Unset {
				c.emit(t, vm.Pop, 1)
				break
			}
			if lhs.Kind == symbol.LocalVar {
				// Captured variable write inside closure body: use HeapSet.
				if cf := curFunc(); cf != "" {
					if cloSym := c.Symbols[cf]; cloSym != nil {
						if idx := cloSym.FreeVarIndex(lhs.Name); idx >= 0 {
							c.emit(t, vm.HeapSet, idx)
							c.emit(t, vm.Pop, 1) // pop stale value pushed by HeapGet in Ident
							break
						}
					}
				}
				if !lhs.Used {
					if !lhs.NeedsCell() {
						c.emit(t, vm.New, lhs.Index, c.typeSym(lhs.Type).Index)
					}
					lhs.Used = true
				}
				// Wrap concrete value in Iface when assigning to interface local.
				c.emitIfaceWrap(t, lhs.Type, rhs.Type)
				switch {
				case lhs.CellSlot:
					c.emit(t, vm.CellSet, lhs.Index)
				case lhs.NeedsCell() && !lhs.CellSlot:
					c.emit(t, vm.HeapAlloc)
					lhs.CellSlot = true
					c.emit(t, vm.SetLocal, lhs.Index, 0)
				default:
					c.emit(t, vm.SetLocal, lhs.Index, 0)
				}
				c.emit(t, vm.Pop, 1) // pop stale lhs value left by Ident's Get
				break
			}
			// TODO check source type against var type
			if lhs.Index != symbol.UnsetAddr {
				if v := c.Data[lhs.Index]; !v.IsValid() && rhs.Type != nil {
					c.Data[lhs.Index] = vm.NewValue(rhs.Type.Rtype)
					if sym := c.Symbols[lhs.Name]; sym != nil {
						sym.Type = rhs.Type
					}
				}
			}
			// Wrap concrete value in Iface when assigning to interface variable.
			c.emitIfaceWrap(t, lhs.Type, rhs.Type)
			c.emit(t, vm.SetS, n)

		case lang.DerefAssign:
			if err := checkTopN(2); err != nil { // check rhs and pointer target
				return err
			}
			pop() // rhs
			pop() // lhs (pointer, not yet dereferenced)
			c.emit(t, vm.DerefSet)

		case lang.IndexAssign:
			if err := checkTopN(3); err != nil { // check container, index, and value
				return err
			}
			s := stack[len(stack)-3]
			typ := s.Type
			if typ.IsPtr() {
				typ = typ.Elem()
			}
			switch typ.Rtype.Kind() {
			case reflect.Array, reflect.Slice:
				c.emit(t, vm.IndexSet)
			case reflect.Map:
				c.emit(t, vm.MapSet)
			default:
				return errorf("not a map or array: %s", s.Name)
			}
			c.emit(t, vm.Pop, 1)
			stack = stack[:len(stack)-3]

		case lang.Equal:
			if err := checkTopN(2); err != nil {
				return err
			}
			push(&symbol.Symbol{Type: booleanOpType(pop(), pop())})
			c.emit(t, vm.Equal)

		case lang.EqualSet:
			if err := checkTopN(2); err != nil {
				return err
			}
			push(&symbol.Symbol{Type: booleanOpType(pop(), pop())})
			c.emit(t, vm.EqualSet)

		case lang.Ident:
			s, ok := c.Symbols[t.Str]
			if !ok {
				// It could be either an undefined symbol or a key ident in a literal composite expr.
				s = &symbol.Symbol{Name: t.Str}
			}
			push(s)
			if s.Kind == symbol.Pkg || s.Kind == symbol.Unset || s.Kind == symbol.Builtin {
				break
			}
			// Closure creation: emit code address + captured cell pointers + MkClosure.
			if s.Kind == symbol.Func && len(s.FreeVars) > 0 {
				c.emit(t, vm.GetGlobal, s.Index)
				// Determine the current function's FreeVars for transitive capture.
				var outerCloSym *symbol.Symbol
				if cf := curFunc(); cf != "" {
					outerCloSym = c.Symbols[cf]
				}
				for _, fvName := range s.FreeVars {
					fvSym := c.Symbols[fvName]
					if fvSym == nil {
						return goparser.ErrUndefined{Name: fvName}
					}
					if outerCloSym != nil {
						if idx := outerCloSym.FreeVarIndex(fvName); idx >= 0 {
							// The free variable is already captured in the enclosing closure's Heap.
							// Use HeapPtr to push the existing cell pointer (transitive capture).
							c.emit(t, vm.HeapPtr, idx)
							continue
						}
					}
					if fvSym.Kind == symbol.LocalVar {
						c.emit(t, vm.GetLocal, fvSym.Index)
						if !fvSym.CellSlot {
							c.emit(t, vm.HeapAlloc) // snapshot: not promoted to cell
						}
					} else {
						c.emit(t, vm.GetGlobal, fvSym.Index)
						c.emit(t, vm.HeapAlloc)
					}
				}
				c.emit(t, vm.MkClosure, len(s.FreeVars))
				break
			}
			// Captured variable read inside a closure body: use HeapGet.
			if cf := curFunc(); cf != "" {
				if cloSym := c.Symbols[cf]; cloSym != nil {
					if idx := cloSym.FreeVarIndex(t.Str); idx >= 0 {
						c.emit(t, vm.HeapGet, idx)
						break
					}
				}
			}
			if s.Kind == symbol.LocalVar && s.CellSlot {
				c.emit(t, vm.CellGet, s.Index)
				break
			}
			// Regular local or global access.
			// Type symbols are always in global Data.
			if s.Kind == symbol.LocalVar {
				if !c.fuseGetLocal(vm.GetLocal2, s.Index) {
					c.emit(t, vm.GetLocal, s.Index)
				}
			} else {
				if s.Index == symbol.UnsetAddr {
					// Type or value symbol discovered during Phase 2 code generation.
					s.Index = len(c.Data)
					if s.Kind == symbol.Type {
						c.Data = append(c.Data, vm.NewValue(s.Type.Rtype))
					} else {
						c.Data = append(c.Data, s.Value)
					}
				}
				c.emitTypeOrGlobal(t, s, s.Index)
			}

		case lang.Label:
			if expected, ok := jumpDepth[t.Str]; ok && len(stack) != expected {
				return fmt.Errorf("stack depth mismatch at label %s: got %d, want %d", t.Str, len(stack), expected)
			}
			lc := len(c.Code)
			if s, ok := c.Symbols[t.Str]; ok {
				s.Value = vm.ValueOf(lc)
				if s.Kind == symbol.Func {
					// Label is a function entry point, update its code address in data.
					if s.Index == symbol.UnsetAddr {
						// Method registered during Phase 2 func body parsing.
						s.Index = len(c.Data)
						c.Data = append(c.Data, s.Value)
					} else {
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
							ts.Type.Methods[id] = vm.Method{Index: s.Index, PtrRecv: strings.HasPrefix(parts[0], "*")}
						}
					}
				} else {
					if s.Index == symbol.UnsetAddr {
						s.Index = len(c.Data)
						c.Data = append(c.Data, s.Value)
					} else {
						c.Data[s.Index] = s.Value
					}
				}
			} else {
				if strings.HasSuffix(t.Str, "_end") {
					if s, ok = c.Symbols[strings.TrimSuffix(t.Str, "_end")]; ok && s.Kind == symbol.Func {
						// Patch the Grow instruction with max expression depth for bounds-check-free GetLocal.
						if len(growPos) > 0 {
							gp := growPos[len(growPos)-1]
							c.Code[gp].B = int32(maxExprDepth[len(maxExprDepth)-1]) //nolint:gosec
							growPos = growPos[:len(growPos)-1]
							maxExprDepth = maxExprDepth[:len(maxExprDepth)-1]
							hasDefer = hasDefer[:len(hasDefer)-1]
						}
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
			if err := checkTopN(1); err != nil {
				return err
			}
			if c.fuseCmpJump(t, &fixList, vm.LowerIntImm, vm.LowerIntImmJumpFalse,
				vm.GetLocalLowerIntImm, vm.GetLocalLowerIntImmJumpFalse, 0) ||
				c.fuseCmpJump(t, &fixList, vm.GreaterIntImm, vm.LowerIntImmJumpTrue,
					vm.GetLocalGreaterIntImm, vm.GetLocalLowerIntImmJumpTrue, 1) {
				break
			}
			c.emitJump(t, &fixList, vm.JumpFalse)

		case lang.JumpSetFalse, lang.JumpSetTrue:
			if err := checkTopN(1); err != nil {
				return err
			}
			pop()                             // LHS result: consumed on the non-jumping path; both paths leave one value at label.
			jumpDepth[t.Str] = len(stack) + 1 // one value (LHS or RHS) arrives at the merge label
			op := vm.JumpSetFalse
			if t.Tok == lang.JumpSetTrue {
				op = vm.JumpSetTrue
			}
			c.emitJump(t, &fixList, op)

		case lang.Goto:
			c.emitJump(t, &fixList, vm.Jump)

		case lang.PopExpr:
			if t.Arg[0].(int) == 0 {
				// Mark: save the compile-time stack depth before the expression.
				exprBase = len(stack)
			} else {
				// Pop unused return values left by the expression statement.
				if exprBase >= 0 && len(stack) > exprBase {
					excess := len(stack) - exprBase
					for range excess {
						pop()
					}
					c.emit(t, vm.Pop, excess)
				}
				exprBase = -1
			}

		case lang.Period:
			if len(stack) < 1 {
				return errorf("missing symbol")
			}
			if err := checkTopN(1); err != nil {
				return err
			}
			s := pop()
			switch s.Kind {
			case symbol.Pkg:
				p, ok := c.Packages[s.PkgPath]
				if !ok {
					return fmt.Errorf("package not found: %s", s.PkgPath)
				}
				v, ok := p.Values[t.Str[1:]]
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
					if v.Kind() == reflect.Pointer && v.Reflect().IsNil() {
						// Stdlib wrappers encode exported types as (*T)(nil); extract T.
						rtype := v.Type().Elem()
						nv := vm.NewValue(rtype)
						c.Data = append(c.Data, nv)
						c.SymAdd(l, name, nv, symbol.Type, &vm.Type{Name: rtype.Name(), Rtype: rtype})
					} else {
						c.Data = append(c.Data, v)
						c.SymAdd(l, name, v, symbol.Value, vm.TypeOf(v.Interface()))
					}
					sym = c.Symbols[name]
				}
				push(sym)
				c.emitTypeOrGlobal(t, sym, l)
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
						// For interface types, Method.Type does not include the receiver.
						rm, ok := s.Type.Rtype.MethodByName(methodName)
						if !ok {
							return goparser.ErrUndefined{Name: methodName}
						}
						// Use Kind=Value so the Call handler emits a regular Call
						// (not CallImm which would incorrectly remove a GetGlobal).
						methodSym = &symbol.Symbol{Kind: symbol.Value, Type: &vm.Type{Rtype: rm.Type}}
					}
					push(methodSym)
					c.emit(t, vm.IfaceCall, c.methodID(methodName))
					break
				}
				if m, fieldPath := c.Symbols.MethodByName(s, t.Str[1:]); m != nil {
					push(m)
					// Extract embedded receiver if method is promoted through embedded fields.
					if len(fieldPath) > 0 {
						c.emitField(t, fieldPath)
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
					// HeapAlloc: wrap receiver in a heap cell.
					// Get Global m.Index: push method code address above the cell.
					// Swap 0 1: put code addr below cell (MkClosure convention: code at sp-n-1).
					// MkClosure 1: produce Closure{code, [receiver_cell]}.
					c.emit(t, vm.HeapAlloc)
					c.emit(t, vm.GetGlobal, m.Index)
					c.emit(t, vm.Swap, 0, 1)
					c.emit(t, vm.MkClosure, 1)
					break
				}
				if s.Type == nil {
					return goparser.ErrUndefined{Name: s.Name}
				}
				typ := s.Type.Rtype
				isPtr := typ.Kind() == reflect.Pointer
				if isPtr {
					typ = typ.Elem()
				}
				if f, ok := typ.FieldByName(t.Str[1:]); ok {
					// Look up struct type in symbol table to get parscan-level Fields/Params info.
					structType := c.findTypeSym(typ)
					if structType == nil {
						if isPtr {
							structType = s.Type.Elem()
						} else {
							structType = s.Type
						}
					}
					push(&symbol.Symbol{Kind: symbol.Var, Index: symbol.UnsetAddr, Type: structType.FieldType(t.Str[1:])})
					c.emitField(t, f.Index)
					break
				}
				// Native method on concrete reflect type: use IfaceCall for
				// reflect-based dispatch at runtime.
				methodName := t.Str[1:]
				rtype := s.Type.Rtype
				rm, ok := rtype.MethodByName(methodName)
				needAddr := false
				if !ok && rtype.Kind() != reflect.Pointer {
					rm, ok = reflect.PointerTo(rtype).MethodByName(methodName)
					needAddr = true
				}
				if ok {
					// Build bound method signature (without receiver) so the
					// Call handler sees the correct parameter/return types.
					mt := rm.Type
					in := make([]reflect.Type, mt.NumIn()-1)
					for i := range in {
						in[i] = mt.In(i + 1)
					}
					out := make([]reflect.Type, mt.NumOut())
					for i := range out {
						out[i] = mt.Out(i)
					}
					boundType := reflect.FuncOf(in, out, mt.IsVariadic())
					push(&symbol.Symbol{Kind: symbol.Value, Type: &vm.Type{Rtype: boundType}})
					if needAddr {
						c.emit(t, vm.Addr)
					}
					c.emit(t, vm.IfaceCall, c.methodID(methodName))
					break
				}
				return goparser.ErrUndefined{Name: t.Str[1:]}
			}

		case lang.Next:
			showStack(stack)
			n := t.Arg[0].(int)
			i := c.resolveLabel(t, &fixList)
			lf := func(s *symbol.Symbol) int {
				if s.Kind == symbol.LocalVar {
					return vm.Local
				}
				return vm.Global
			}
			switch n {
			case 0:
				c.emit(t, vm.Next0, i)
			case 1:
				k := stack[len(stack)-2]
				if lf(k) == vm.Local {
					c.emit(t, vm.NextLocal, i, k.Index)
				} else {
					c.emit(t, vm.Next, i, k.Index)
				}
			case 2:
				v := stack[len(stack)-2]
				k := stack[len(stack)-3]
				// Pack kAddr (low 16) and vAddr (high 16) into one int.
				packed := k.Index | (v.Index << 16)
				if lf(k) == vm.Local {
					c.emit(t, vm.Next2Local, i, packed)
				} else {
					c.emit(t, vm.Next2, i, packed)
				}
			}

		case lang.Range:
			n := t.Arg[0].(int)
			// FIXME: handle all iterator types.
			// set the correct type to the iterator variables.
			topSym := top()
			vt := symbol.Vtype(topSym)
			var rangeKind reflect.Kind
			if vt != nil {
				rangeKind = vt.Rtype.Kind()
			}
			// Go spec: range over an array iterates a copy; range over
			// a pointer-to-array or a slice uses the original.
			var copyArray int
			if rangeKind == reflect.Array {
				copyArray = 1
			}
			if rangeKind == reflect.Pointer {
				vt = vt.Elem()
				rangeKind = vt.Rtype.Kind()
				c.emit(t, vm.Deref)
			}
			initRangeVar := func(s *symbol.Symbol, typ *vm.Type) {
				s.Type = typ
				if s.Kind == symbol.LocalVar {
					c.emit(t, vm.New, s.Index, c.typeSym(s.Type).Index)
				} else {
					c.Data[s.Index] = vm.NewValue(s.Type.Rtype)
				}
			}
			switch rangeKind {
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
				reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
				if n > 0 {
					initRangeVar(stack[len(stack)-2], c.Symbols["int"].Type)
				}
				c.emit(t, vm.Pull)
			case reflect.Array, reflect.Slice, reflect.String:
				var vType *vm.Type
				if rangeKind == reflect.String {
					vType = c.Symbols["rune"].Type
				} else {
					vType = vt.Elem()
				}
				switch n {
				case 0:
					c.emit(t, vm.Pull, copyArray)
				case 1:
					initRangeVar(stack[len(stack)-2], c.Symbols["int"].Type)
					c.emit(t, vm.Pull, copyArray)
				case 2:
					k, v := stack[len(stack)-3], stack[len(stack)-2]
					initRangeVar(k, c.Symbols["int"].Type)
					initRangeVar(v, vType)
					c.emit(t, vm.Pull2, copyArray)
				}
			case reflect.Map:
				keyType := vt.Key()
				switch n {
				case 0:
					c.emit(t, vm.Pull)
				case 1:
					initRangeVar(stack[len(stack)-2], keyType)
					c.emit(t, vm.Pull)
				case 2:
					k, v := stack[len(stack)-3], stack[len(stack)-2]
					initRangeVar(k, keyType)
					initRangeVar(v, vt.Elem())
					c.emit(t, vm.Pull2)
				}
			case reflect.Chan:
				switch n {
				case 0:
					c.emit(t, vm.Pull)
				case 1:
					initRangeVar(stack[len(stack)-2], vt.Elem())
					c.emit(t, vm.Pull)
				}
			default:
				// Unhandled range type (e.g. struct element type from empty composite literal).
				if n == 0 {
					c.emit(t, vm.Pop, 1)
					c.emit(t, vm.Push, 0)
					c.emit(t, vm.Pull)
				}
			}

		case lang.Stop:
			c.emit(t, vm.Stop, t.Arg[0].(int))

		case lang.Defer:
			if len(hasDefer) > 0 {
				hasDefer[len(hasDefer)-1] = true
			}
			narg := t.Arg[0].(int)
			s := stack[len(stack)-1-narg]
			isX := 0
			switch s.Kind {
			case symbol.Type:
				return errorf("cannot defer a type conversion")
			case symbol.Value:
				isX = 1
			case symbol.Builtin:
				// Builtin functions (print, println, close, ...) have no VM-callable
				// representation. Push the opcode number as funcVal (on top of
				// already-emitted args), then use isX=2 so DeferPush rotates it into
				// position and Return dispatches the opcode directly.
				op, ok := builtinDeferOp[s.Name]
				if !ok {
					return errorf("cannot defer builtin %s", s.Name)
				}
				c.emit(t, vm.Push, int(op))
				isX = 2
			}
			pop() // function
			for i := 0; i < narg; i++ {
				pop()
			}
			c.emit(t, vm.DeferPush, narg, isX)

		case lang.Go:
			narg := t.Arg[0].(int)
			s := stack[len(stack)-1-narg]
			if s.Kind == symbol.Type {
				return errorf("cannot use a type conversion as a goroutine")
			}
			pop() // function
			for i := 0; i < narg; i++ {
				pop()
			}
			if s.Kind == symbol.Func && len(s.FreeVars) == 0 && c.removeGetGlobal(s.Index) {
				c.emit(t, vm.GoCallImm, s.Index, narg)
			} else {
				c.emit(t, vm.GoCall, narg)
			}

		case lang.ChanSend:
			pop() // value
			pop() // channel
			c.emit(t, vm.ChanSend)

		case lang.Return:
			numOut := t.Arg[0].(int)
			if err := checkTopN(numOut); err != nil {
				return err
			}
			// Wrap concrete return values in Iface when the function return type is an interface.
			if funcType, ok := t.Arg[1].(*vm.Type); ok {
				for i := 0; i < numOut; i++ {
					stackSym := stack[len(stack)-numOut+i]
					c.emitIfaceWrapAt(t, funcType.ReturnType(i), stackSym.Type, numOut-1-i)
				}
			}
			if len(hasDefer) == 0 || hasDefer[len(hasDefer)-1] || !c.fuseGetLocal(vm.GetLocalReturn, 0) {
				c.emit(t, vm.Return)
			}

		case lang.Slice:
			var coll *symbol.Symbol
			if stack[len(stack)-3].IsInt() {
				coll = stack[len(stack)-4]
				c.emit(t, vm.Slice3)
				stack = stack[:len(stack)-4]
			} else {
				coll = stack[len(stack)-3]
				c.emit(t, vm.Slice)
				stack = stack[:len(stack)-3]
			}
			rtype := coll.Type.Rtype
			if rtype.Kind() == reflect.Ptr && rtype.Elem().Kind() == reflect.Array {
				rtype = rtype.Elem()
			}
			if rtype.Kind() == reflect.Array {
				rtype = reflect.SliceOf(rtype.Elem())
			}
			push(&symbol.Symbol{Kind: symbol.Value, Type: &vm.Type{Rtype: rtype}})

		case lang.Select:
			descs := t.Arg[0].([]goparser.SelectCaseDesc)
			meta := &vm.SelectMeta{Cases: make([]vm.SelectCaseInfo, len(descs))}
			// initSlot initializes a variable slot and returns its index.
			initSlot := func(name string, typ *vm.Type) int {
				s := c.Symbols[name]
				s.Type = typ
				switch {
				case s.Kind == symbol.LocalVar:
					c.emit(t, vm.New, s.Index, c.typeSym(typ).Index)
				case s.Index == symbol.UnsetAddr:
					s.Index = len(c.Data)
					c.Data = append(c.Data, vm.NewValue(typ.Rtype))
				default:
					c.Data[s.Index] = vm.NewValue(typ.Rtype)
				}
				return s.Index
			}
			// Pop stack entries in reverse (LIFO) to collect channel element types.
			chanTypes := make([]*vm.Type, len(descs))
			for i := len(descs) - 1; i >= 0; i-- {
				switch descs[i].Dir {
				case reflect.SelectSend:
					pop() // value
					pop() // channel
				case reflect.SelectRecv:
					chanTypes[i] = pop().Type.Elem()
				}
			}
			for i, d := range descs {
				ci := vm.SelectCaseInfo{Dir: d.Dir, Slot: -1, OkSlot: -1}
				switch d.Dir {
				case reflect.SelectRecv:
					if d.ValName != "" {
						ci.Local = c.Symbols[d.ValName].Kind == symbol.LocalVar
						ci.Slot = initSlot(d.ValName, chanTypes[i])
					}
					if d.OkName != "" {
						ci.OkSlot = initSlot(d.OkName, c.Symbols["bool"].Type)
					}
				case reflect.SelectSend:
					meta.TotalPop += 2
				}
				if d.Dir == reflect.SelectRecv {
					meta.TotalPop++
				}
				meta.Cases[i] = ci
			}
			metaIdx := len(c.Data)
			c.Data = append(c.Data, vm.ValueOf(meta))
			push(&symbol.Symbol{Kind: symbol.Value, Type: c.Symbols["int"].Type})
			c.emit(t, vm.SelectExec, metaIdx, len(descs))

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
		c.Code[loc].A = int32(int(s.Value.Int()) - loc) //nolint:gosec // relative code position
	}
	return err
}

func arithmeticOpType(right, left *symbol.Symbol) *vm.Type {
	// Untyped constants take their type from the other operand (Go spec Operators).
	if right.Kind == symbol.Const && left.Kind != symbol.Const {
		return symbol.Vtype(left)
	}
	return symbol.Vtype(right)
}

func constKind(right, left *symbol.Symbol) symbol.Kind {
	if right.Kind == symbol.Const && left.Kind == symbol.Const {
		return symbol.Const
	}
	return symbol.Value
}

func (c *Compiler) emitConstConvert(t goparser.Token, s *symbol.Symbol, typ *vm.Type, depth int) {
	if s.Kind != symbol.Const || typ == nil {
		return
	}
	styp := symbol.Vtype(s)
	if styp == nil || styp.Rtype == typ.Rtype {
		return
	}
	sk, dk := styp.Rtype.Kind(), typ.Rtype.Kind()
	if sk >= reflect.Int && sk <= reflect.Float64 && dk >= reflect.Int && dk <= reflect.Float64 {
		c.emit(t, vm.Convert, c.typeSym(typ).Index, depth)
	}
}

func booleanOpType(_, _ *symbol.Symbol) *vm.Type { return vm.TypeOf(true) }

func shiftLeftType(left *symbol.Symbol, intTyp *vm.Type) *vm.Type {
	vt := symbol.Vtype(left)
	if left.Kind == symbol.Const && vt != nil {
		if k := vt.Rtype.Kind(); k == reflect.Float32 || k == reflect.Float64 {
			return intTyp
		}
	}
	return vt
}

func (c *Compiler) fuseGetLocal(op vm.Op, imm int) bool {
	if len(c.Code) == 0 || c.Code[len(c.Code)-1].Op != vm.GetLocal {
		return false
	}
	c.Code[len(c.Code)-1].Op = op
	c.Code[len(c.Code)-1].B = int32(imm) //nolint:gosec
	return true
}

func (c *Compiler) fuseCmpJump(t goparser.Token, fixList *goparser.Tokens,
	cmpOp, fusedOp, getLocalCmpOp, getLocalFusedOp vm.Op, immAdj int32,
) bool {
	if len(c.Code) == 0 {
		return false
	}
	prev := &c.Code[len(c.Code)-1]
	var fused vm.Op
	var newB int32
	switch prev.Op {
	case cmpOp:
		fused = fusedOp
		newB = prev.A + immAdj // immediate moves to B; A will hold jump offset
	case getLocalCmpOp:
		imm := prev.B + immAdj
		if imm < -32768 || imm > 32767 {
			return false // immediate doesn't fit in int16 after adjustment
		}
		fused = getLocalFusedOp
		newB = (prev.A << 16) | (imm & 0xFFFF) // pack localOff<<16 | imm
	default:
		return false
	}
	loc := len(c.Code) - 1
	var jumpOff int32
	if s, ok := c.Symbols[t.Str]; !ok {
		t.Arg = []any{loc} // fixup at the fused instruction's position
		*fixList = append(*fixList, t)
	} else {
		jumpOff = int32(int(s.Value.Int()) - loc) //nolint:gosec
	}
	prev.Op = fused
	prev.A = jumpOff
	prev.B = newB
	return true
}

func (c *Compiler) retractPush(s *symbol.Symbol) (int, bool) {
	if s.Kind != symbol.Const || len(c.Code) == 0 || c.Code[len(c.Code)-1].Op != vm.Push {
		return 0, false
	}
	n := int(c.Code[len(c.Code)-1].A)
	c.Code = c.Code[:len(c.Code)-1]
	return n, true
}

func isInt64Kind(typ *vm.Type) bool {
	if typ == nil {
		return false
	}
	k := typ.Rtype.Kind()
	return k == reflect.Int || k == reflect.Int64
}

func isUint64Kind(typ *vm.Type) bool {
	if typ == nil {
		return false
	}
	k := typ.Rtype.Kind()
	return k == reflect.Uint || k == reflect.Uint64
}

func numericOp(base vm.Op, typ *vm.Type) vm.Op {
	if typ == nil {
		panic("numericOp: nil type")
	}
	k := typ.Rtype.Kind()
	if int(k) >= len(vm.NumKindOffset) || vm.NumKindOffset[k] < 0 { //nolint:gosec
		panic(fmt.Sprintf("numericOp: non-numeric kind %v", k))
	}
	return base + vm.Op(vm.NumKindOffset[k]) //nolint:gosec
}

func (c *Compiler) emitArithmeticOp(t goparser.Token, right *symbol.Symbol, typ *vm.Type, baseOp, immOp, fuseOp, strOp vm.Op) {
	if strOp != 0 && typ != nil && typ.Rtype.Kind() == reflect.String {
		c.emit(t, strOp)
		return
	}
	if immOp != 0 && (isInt64Kind(typ) || isUint64Kind(typ)) {
		if n, ok := c.retractPush(right); ok {
			if fuseOp == 0 || !c.fuseGetLocal(fuseOp, n) {
				c.emit(t, immOp, n)
			}
			return
		}
	}
	c.emit(t, numericOp(baseOp, typ))
}

func (c *Compiler) emitComparisonOp(t goparser.Token, s2 *symbol.Symbol, typ *vm.Type, baseOp, intImm, uintImm, fuseInt, fuseUint vm.Op, negate bool) {
	var immOp, fuseOp vm.Op
	if isInt64Kind(typ) {
		immOp, fuseOp = intImm, fuseInt
	} else if isUint64Kind(typ) {
		immOp, fuseOp = uintImm, fuseUint
	}
	if immOp != 0 {
		if n, ok := c.retractPush(s2); ok {
			if fuseOp == 0 || !c.fuseGetLocal(fuseOp, n) {
				c.emit(t, immOp, n)
			}
			if negate {
				c.emit(t, vm.Not)
			}
			return
		}
	}
	c.emit(t, numericOp(baseOp, typ))
	if negate {
		c.emit(t, vm.Not)
	}
}

func (c *Compiler) resolveLabel(t goparser.Token, fixList *goparser.Tokens) int {
	if s, ok := c.Symbols[t.Str]; ok {
		return int(s.Value.Int()) - len(c.Code)
	}
	t.Arg = []any{len(c.Code)}
	*fixList = append(*fixList, t)
	return 0
}

func (c *Compiler) emitJump(t goparser.Token, fixList *goparser.Tokens, op vm.Op) {
	c.emit(t, op, c.resolveLabel(t, fixList))
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
			if d, ok := labels[i+int(l.A)]; ok {
				extra = "// " + d[0]
			}
		case vm.Get, vm.GetLocal, vm.GetGlobal, vm.SetLocal, vm.SetGlobal, vm.CallImm, vm.CellGet, vm.CellSet:
			if d, ok := data[int(l.A)]; ok {
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

// BuildDebugInfo constructs a DebugInfo from the compiler's symbol table
// and source registry. The result can be passed to DumpFrame/DumpCallStack.
func (c *Compiler) BuildDebugInfo() *vm.DebugInfo {
	di := vm.NewDebugInfo()
	di.Sources = c.Sources

	for name, sym := range c.Symbols {
		switch {
		case sym.Kind == symbol.Func:
			if !sym.Value.IsValid() {
				continue
			}
			addr := int(sym.Value.Int())
			// Prefer shorter (less-scoped) names when multiple funcs share an address.
			if existing, ok := di.Labels[addr]; !ok || len(name) < len(existing) {
				di.Labels[addr] = name
			}

		case sym.Kind == symbol.LocalVar && sym.Used && sym.Index != symbol.UnsetAddr:
			// Extract function scope and short variable name from scoped name.
			// Scoped name format: "main/foo/for0/x" -> funcScope = closest Func ancestor.
			shortName := name
			if i := strings.LastIndex(name, "/"); i >= 0 {
				shortName = name[i+1:]
			}
			// Walk up the scope to find the enclosing function.
			funcName := enclosingFunc(name, c.Symbols)
			di.Locals[funcName] = append(di.Locals[funcName], vm.LocalVar{
				Offset: sym.Index,
				Name:   shortName,
			})
		}
	}
	for idx, e := range c.symbolsByIndex() {
		if e.Kind != symbol.LocalVar {
			di.Globals[idx] = e.name
		}
	}
	return di
}

func enclosingFunc(scopedName string, syms symbol.SymMap) string {
	scope := scopedName
	for {
		i := strings.LastIndex(scope, "/")
		if i < 0 {
			return ""
		}
		scope = scope[:i]
		if s, ok := syms[scope]; ok && s.Kind == symbol.Func {
			return scope
		}
	}
}

func (c *Compiler) removeFnew(index int) {
	for i := len(c.Code) - 1; i >= 0; i-- {
		op := c.Code[i].Op
		if (op == vm.Fnew || op == vm.FnewE) && int(c.Code[i].A) == index {
			copy(c.Code[i:], c.Code[i+1:])
			c.Code = c.Code[:len(c.Code)-1]
			return
		}
	}
}

func (c *Compiler) removeGetLocal(index int) {
	for i := len(c.Code) - 1; i >= 0; i-- {
		op := c.Code[i].Op
		if (op == vm.GetLocal || op == vm.CellGet) && int(c.Code[i].A) == index {
			copy(c.Code[i:], c.Code[i+1:])
			c.Code = c.Code[:len(c.Code)-1]
			return
		}
		if op == vm.GetLocal2 && int(c.Code[i].A) == index {
			// Key is at A; value is at B. Unfuse: keep only GetLocal(B).
			c.Code[i].Op = vm.GetLocal
			c.Code[i].A = c.Code[i].B
			c.Code[i].B = 0
			return
		}
	}
}

func (c *Compiler) removeGetGlobal(index int) bool {
	for i := len(c.Code) - 1; i >= 0; i-- {
		if c.Code[i].Op == vm.GetGlobal && int(c.Code[i].A) == index {
			// Skip if followed by Swap (method dispatch: GetGlobal + Swap + MkClosure).
			if i+1 < len(c.Code) && c.Code[i+1].Op == vm.Swap {
				return false
			}
			copy(c.Code[i:], c.Code[i+1:])
			c.Code = c.Code[:len(c.Code)-1]
			return true
		}
	}
	return false
}

func (c *Compiler) compileBuiltin(
	s *symbol.Symbol, narg int, t goparser.Token,
	stack *[]*symbol.Symbol, push func(*symbol.Symbol), pop func() *symbol.Symbol, _ func() *symbol.Symbol,
) (bool, error) {
	switch s.Name {
	case "trap":
		if narg != 0 {
			return true, errors.New("too many arguments to trap")
		}
		pop() // trap symbol
		c.emit(t, vm.Trap)
		return true, nil

	case "panic":
		if narg != 1 {
			return true, errors.New("too many arguments to panic")
		}
		pop() // argument
		pop() // panic symbol
		c.emit(t, vm.Panic)
		return true, nil

	case "recover":
		if narg != 0 {
			return true, errors.New("too many arguments to recover")
		}
		pop() // recover symbol
		push(&symbol.Symbol{Type: c.Symbols["any"].Type})
		c.emit(t, vm.Recover)
		return true, nil

	case "len", "cap":
		if narg != 1 {
			return true, fmt.Errorf("invalid argument count for %s", s.Name)
		}
		pop() // argument
		pop() // builtin symbol
		push(&symbol.Symbol{Type: c.Symbols["int"].Type})
		op := vm.Len
		if s.Name == "cap" {
			op = vm.Cap
		}
		c.emit(t, op, 0)
		c.emit(t, vm.Swap, 0, 1)
		c.emit(t, vm.Pop, 1)
		return true, nil

	case "append":
		if narg < 2 {
			return true, errors.New("missing arguments to append")
		}
		nvals := narg - 1 // number of values to append
		for range nvals {
			pop()
		}
		sliceSym := pop() // slice argument
		pop()             // append symbol
		push(sliceSym)    // result is same slice type
		elemType := sliceSym.Type.Rtype.Elem()
		elemIdx := c.typeSym(&vm.Type{Rtype: elemType}).Index
		if elemType.Kind() == reflect.Func && nvals > 1 {
			// Pre-wrap func values so AppendSlice can extract ParscanFunc.GF without
			// calling wrapForFunc at runtime. Not needed for nvals==1; Append handles it.
			funcTypeIdx := c.typeIndex(&vm.Type{Rtype: elemType})
			for i := range nvals {
				c.emit(t, vm.WrapFunc, funcTypeIdx, nvals-1-i)
			}
		}
		switch {
		case len(t.Arg) > 1 && t.Arg[1].(int) != 0:
			c.emit(t, vm.AppendSlice, 0, elemIdx) // 0 signals spread mode
		case nvals == 1:
			c.emit(t, vm.Append, 1, elemIdx)
		default:
			c.emit(t, vm.AppendSlice, nvals, elemIdx)
		}
		return true, nil

	case "copy":
		if narg != 2 {
			return true, errors.New("invalid argument count for copy")
		}
		pop() // src
		pop() // dst
		pop() // copy symbol
		push(&symbol.Symbol{Type: c.Symbols["int"].Type})
		c.emit(t, vm.CopySlice)
		return true, nil

	case "delete":
		if narg != 2 {
			return true, errors.New("invalid argument count for delete")
		}
		pop() // key
		pop() // map
		pop() // delete symbol
		c.emit(t, vm.DeleteMap)
		c.emit(t, vm.Pop, 1) // delete is void; discard stale map value
		return true, nil

	case "new":
		if narg != 1 {
			return true, errors.New("invalid argument count for new")
		}
		typeSym := (*stack)[len(*stack)-1]
		if typeSym.Kind != symbol.Type {
			return true, errors.New("first argument to new must be a type")
		}
		c.removeFnew(typeSym.Index)
		pop() // type arg
		pop() // new symbol
		push(&symbol.Symbol{Kind: symbol.Value, Type: vm.PointerTo(typeSym.Type)})
		c.emit(t, vm.PtrNew, typeSym.Index)
		return true, nil

	case "make":
		if narg < 1 || narg > 3 {
			return true, errors.New("invalid argument count for make")
		}
		typeSym := (*stack)[len(*stack)-narg]
		if typeSym.Kind != symbol.Type {
			return true, errors.New("first argument to make must be a type")
		}
		c.removeFnew(typeSym.Index)
		for range narg {
			pop()
		}
		pop() // make symbol
		push(&symbol.Symbol{Kind: symbol.Value, Type: typeSym.Type})
		switch typeSym.Type.Rtype.Kind() {
		case reflect.Slice:
			// make([]T, len) or make([]T, len, cap)
			elemType := typeSym.Type.Rtype.Elem()
			elemIdx := c.typeSym(&vm.Type{Rtype: elemType}).Index
			c.emit(t, vm.MkSlice, -(narg - 1), elemIdx)
		case reflect.Map:
			keyType := typeSym.Type.Rtype.Key()
			keyIdx := c.typeSym(&vm.Type{Rtype: keyType}).Index
			valType := typeSym.Type.Rtype.Elem()
			valIdx := c.typeSym(&vm.Type{Rtype: valType}).Index
			c.emit(t, vm.MkMap, keyIdx, valIdx)
		case reflect.Chan:
			elemIdx := c.typeSym(typeSym.Type.ElemType).Index
			if narg == 2 {
				// make(chan T, bufSize): buffer size is already on stack
				c.emit(t, vm.MkChan, elemIdx, -1)
			} else {
				// make(chan T): unbuffered
				c.emit(t, vm.MkChan, elemIdx, 0)
			}
		default:
			return true, fmt.Errorf("cannot make type %s", typeSym.Type.Rtype)
		}
		return true, nil

	case "close":
		if narg != 1 {
			return true, errors.New("invalid argument count for close")
		}
		pop() // channel
		pop() // close symbol
		c.emit(t, vm.ChanClose)
		return true, nil

	case "print", "println":
		for range narg {
			pop()
		}
		pop() // builtin symbol
		op := vm.Print
		if s.Name == "println" {
			op = vm.Println
		}
		c.emit(t, op, narg)
		return true, nil
	}

	return false, nil
}

func (c *Compiler) typeSym(t *vm.Type) *symbol.Symbol {
	// Use c.typeSyms (keyed by reflect.Type pointer equality) instead of
	// c.Symbols[t.Rtype.String()]: calling String() on heap-allocated rtypes
	// created by reflect.StructOf and then patched in-place by patchRtype
	// crashes in resolveNameOff because the rtype address is not in any
	// module's type section.
	tsym, ok := c.typeSyms[t.Rtype]
	if !ok {
		tsym = &symbol.Symbol{Index: symbol.UnsetAddr, Kind: symbol.Type, Type: t}
		c.typeSyms[t.Rtype] = tsym
	}
	if tsym.Index == symbol.UnsetAddr {
		tsym.Index = len(c.Data)
		c.Data = append(c.Data, vm.TypeValue(t.Rtype))
	}
	return tsym
}

// intrinsicInfo describes a VM intrinsic that replaces a native function call.
type intrinsicInfo struct {
	op   vm.Op
	narg int
}

// intrinsicOp maps "pkgPath.funcName" to a VM opcode and its arity.
var intrinsicOp = map[string]intrinsicInfo{
	// math: float64 unary.
	"math.Abs":         {vm.AbsFloat64, 1},
	"math.Sqrt":        {vm.SqrtFloat64, 1},
	"math.Ceil":        {vm.CeilFloat64, 1},
	"math.Floor":       {vm.FloorFloat64, 1},
	"math.Trunc":       {vm.TruncFloat64, 1},
	"math.RoundToEven": {vm.NearestFloat64, 1},
	// math: float64 binary.
	"math.Min":      {vm.MinFloat64, 2},
	"math.Max":      {vm.MaxFloat64, 2},
	"math.Copysign": {vm.CopysignFloat64, 2},
	// math/bits: leading/trailing zeros.
	"math/bits.LeadingZeros":    {vm.Clz64, 1},
	"math/bits.LeadingZeros32":  {vm.Clz32, 1},
	"math/bits.LeadingZeros64":  {vm.Clz64, 1},
	"math/bits.TrailingZeros":   {vm.Ctz64, 1},
	"math/bits.TrailingZeros32": {vm.Ctz32, 1},
	"math/bits.TrailingZeros64": {vm.Ctz64, 1},
	// math/bits: population count.
	"math/bits.OnesCount":   {vm.Popcnt64, 1},
	"math/bits.OnesCount32": {vm.Popcnt32, 1},
	"math/bits.OnesCount64": {vm.Popcnt64, 1},
	// math/bits: rotate.
	"math/bits.RotateLeft":   {vm.Rotl64, 2},
	"math/bits.RotateLeft32": {vm.Rotl32, 2},
	"math/bits.RotateLeft64": {vm.Rotl64, 2},
}

// compileIntrinsic replaces known native function calls with direct VM opcodes,
// avoiding the overhead of reflection-based calls.
func (c *Compiler) compileIntrinsic(
	s *symbol.Symbol, narg int, t goparser.Token,
	push func(*symbol.Symbol), pop func() *symbol.Symbol,
) (bool, error) {
	if s.Kind != symbol.Value {
		return false, nil
	}
	info, ok := intrinsicOp[s.Name]
	if !ok {
		return false, nil
	}
	if narg != info.narg {
		return false, nil
	}
	// Remove the GetGlobal that loaded the function value onto the stack.
	if !c.removeGetGlobal(s.Index) {
		return false, nil
	}
	// Pop function symbol and argument symbols, push return type.
	for i := 0; i < narg; i++ {
		pop()
	}
	pop() // function symbol
	// Determine the return type from the native function's reflect type.
	rv := s.Value.Reflect()
	if rv.IsValid() && rv.Type().Kind() == reflect.Func && rv.Type().NumOut() > 0 {
		push(&symbol.Symbol{Kind: symbol.Value, Type: &vm.Type{Rtype: rv.Type().Out(0)}})
	}
	c.emit(t, info.op)
	return true, nil
}
