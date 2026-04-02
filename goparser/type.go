package goparser

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode"

	"github.com/mvertes/parscan/lang"
	"github.com/mvertes/parscan/symbol"
	"github.com/mvertes/parscan/vm"
)

type typeFlag int

const (
	parseTypeIn typeFlag = iota
	parseTypeOut
	parseTypeVar
	parseTypeType
	parseTypeRecv // method receiver: Heap[0], not a stack param
)

// Type parsing error definitions.
var (
	ErrEllipsisArray  = errors.New("[...] array")
	ErrFuncType       = errors.New("invalid function type")
	ErrInvalidType    = errors.New("invalid type")
	ErrMissingType    = errors.New("missing type")
	ErrSize           = errors.New("invalid size")
	ErrSyntax         = errors.New("syntax error")
	ErrNotImplemented = errors.New("not implemented")
)

// ErrUndefined is returned during parsing when a referenced symbol is not yet defined.
// It is retryable: the lazy fixpoint loop in interp.Eval defers the declaration and retries
// after other declarations have been processed.
type ErrUndefined struct{ Name string }

func (e ErrUndefined) Error() string { return "undefined: " + e.Name }

// resolveEllipsisArray resolves [...]T by counting elements in the following BraceBlock.
func (p *Parser) resolveEllipsisArray(elemTyp *vm.Type, toks Tokens, braceIdx int) (*vm.Type, error) {
	if braceIdx >= len(toks) || toks[braceIdx].Tok != lang.BraceBlock {
		return nil, errors.New("[...] requires a composite literal")
	}
	size, err := p.numItems(toks[braceIdx].Block(), lang.Comma)
	if err != nil {
		return nil, err
	}
	return vm.ArrayOf(size, elemTyp), nil
}

// parseTypeExpr returns the expression type from its tokens, the number of consumed tokens
// for the type and the parse error.
func (p *Parser) parseTypeExpr(in Tokens) (typ *vm.Type, n int, err error) {
	switch in[0].Tok {
	case lang.BracketBlock:
		typ, i, err := p.parseTypeExpr(in[1:])
		if err != nil {
			return nil, 0, err
		}
		if b := in[0].Block(); len(b) > 0 {
			x, err := p.Scan(b, false)
			if err != nil {
				return nil, 0, err
			}
			// [...]T syntax: size is resolved by the caller from the composite literal.
			if len(x) == 1 && x[0].Tok == lang.Ellipsis {
				return typ, 1 + i, ErrEllipsisArray
			}
			if x, err = p.parseExpr(x, ""); err != nil {
				return nil, 0, err
			}
			cval, _, err := p.evalConstExpr(x)
			if err != nil {
				return nil, 0, err
			}
			size, ok := constValue(cval).(int)
			if !ok {
				return nil, 0, ErrSize
			}
			return vm.ArrayOf(size, typ), 1 + i, nil
		}
		return vm.SliceOf(typ), 1 + i, nil

	case lang.Mul:
		typ, i, err := p.parseTypeExpr(in[1:])
		if err != nil {
			return nil, 0, err
		}
		return vm.PointerTo(typ), 1 + i, nil

	case lang.Func:
		// Get argument and return token positions depending on function pattern:
		// method with receiver, named function or anonymous closure.
		var out Tokens
		var indexArgs int
		var recvr string
		switch l, in1 := len(in), in[1]; {
		case l >= 4 && in1.Tok == lang.ParenBlock && in[2].Tok == lang.Ident:
			// TODO: make sure that it is not an anonymous closure output type.
			recvr = in1.Block()
			indexArgs, out = 3, in[4:]
		case l >= 3 && in1.Tok == lang.Ident:
			indexArgs, out = 2, in[3:]
		case l >= 2 && in1.Tok == lang.ParenBlock:
			indexArgs, out = 1, in[2:]
		default:
			return nil, 0, ErrFuncType
		}

		// We can now parse function input and output parameter types.
		// Input parameters are always enclosed by parenthesis.
		// For methods, parse the receiver separately as Heap[0] (not a stack param),
		// so explicit params get the correct frame indices (-2, -3, ...).
		if recvr != "" {
			recvrToks, err := p.Scan(recvr, false)
			if err != nil {
				return nil, 0, err
			}
			if _, _, _, err = p.parseParamTypes(recvrToks, parseTypeRecv); err != nil {
				return nil, 0, err
			}
		}
		typ, _, _, err := p.parseFuncParams(in[indexArgs], out)
		if err != nil {
			return nil, 0, err
		}
		// Count return type tokens so the caller can advance past them.
		// parseFuncParams consumes out as return types (unless out starts with a
		// BraceBlock, which is a function body rather than a return type).
		nRet := 0
		if len(out) > 0 && out[0].Tok != lang.BraceBlock {
			if out[0].Tok == lang.ParenBlock {
				nRet = 1 // parenthesized return list, e.g. (int, error)
			} else {
				// Bare return type: measure token count via parseTypeExpr.
				// Use typeOnly to avoid registering symbols as a side effect.
				save := p.typeOnly
				p.typeOnly = true
				_, nRet, _ = p.parseTypeExpr(out)
				p.typeOnly = save
			}
		}
		return typ, 1 + indexArgs + nRet, nil

	case lang.Ident:
		// TODO: selector expression (pkg.type)
		s, _, ok := p.Symbols.Get(in[0].Str, p.scope)
		if !ok {
			return nil, 0, ErrUndefined{in[0].Str}
		}
		if s.Kind != symbol.Type {
			return nil, 0, fmt.Errorf("%w: %s", ErrInvalidType, in[0].Str)
		}
		return s.Type, 1, nil

	case lang.Struct:
		typ, err := p.parseStructType(in)
		if err != nil {
			return nil, 0, err
		}
		return typ, 2, nil

	case lang.Arrow:
		// "<-chan T" is recv-only; require chan keyword next.
		if len(in) < 3 || in[1].Tok != lang.Chan {
			return nil, 0, fmt.Errorf("%w: %s", ErrInvalidType, in[0].Str)
		}
		elemTyp, i, err := p.parseTypeExpr(in[2:])
		if err != nil {
			return nil, 0, err
		}
		return vm.ChanOf(reflect.RecvDir, elemTyp), 2 + i, nil

	case lang.Chan:
		if len(in) < 2 {
			return nil, 0, fmt.Errorf("%w: %s", ErrInvalidType, in[0].Str)
		}
		dir := reflect.BothDir
		rest, skip := in[1:], 1
		if len(rest) > 0 && rest[0].Tok == lang.Arrow {
			dir, rest, skip = reflect.SendDir, rest[1:], 2 // chan<-: send-only, skip both chan and <- tokens
		}
		elemTyp, i, err := p.parseTypeExpr(rest)
		if err != nil {
			return nil, 0, err
		}
		return vm.ChanOf(dir, elemTyp), skip + i, nil

	case lang.Map:
		if len(in) < 3 || in[1].Tok != lang.BracketBlock {
			return nil, 0, fmt.Errorf("%w: %s", ErrInvalidType, in[0].Str)
		}
		kin, err := p.Scan(in[1].Block(), false)
		if err != nil {
			return nil, 0, err
		}
		ktyp, _, err := p.parseTypeExpr(kin) // Key type
		if err != nil {
			return nil, 0, err
		}
		etyp, i, err := p.parseTypeExpr(in[2:]) // Element type
		if err != nil {
			return nil, 0, err
		}
		return vm.MapOf(ktyp, etyp), 2 + i, nil

	case lang.Interface:
		if len(in) < 2 || in[1].Tok != lang.BraceBlock {
			return nil, 0, fmt.Errorf("%w: %v", ErrSyntax, in)
		}
		block := in[1].Block()
		if strings.TrimSpace(block) == "" {
			// Empty interface (equivalent to any).
			return &vm.Type{Rtype: reflect.TypeOf((*any)(nil)).Elem()}, 2, nil
		}
		toks, err := p.Scan(block, false)
		if err != nil {
			return nil, 0, err
		}
		var methods []vm.IfaceMethod
		for _, lt := range toks.Split(lang.Semicolon) {
			if len(lt) == 0 || lt[0].Tok == lang.Comment {
				continue
			}
			if lt[0].Tok != lang.Ident {
				return nil, 0, fmt.Errorf("%w: expected method name in interface", ErrSyntax)
			}
			if len(lt) == 1 || lt[1].Tok != lang.ParenBlock {
				s, _, ok := p.Symbols.Get(lt[0].Str, p.scope)
				if !ok {
					return nil, 0, ErrUndefined{lt[0].Str}
				}
				if s.Kind != symbol.Type || !s.Type.IsInterface() {
					return nil, 0, fmt.Errorf("%w: %s is not an interface", ErrSyntax, lt[0].Str)
				}
				if len(s.Type.IfaceMethods) > 0 {
					methods = append(methods, s.Type.IfaceMethods...)
				} else {
					// Builtin interface (e.g. error): synthesize from reflect method set.
					for j := 0; j < s.Type.Rtype.NumMethod(); j++ {
						m := s.Type.Rtype.Method(j)
						methods = append(methods, vm.IfaceMethod{Name: m.Name, ID: -1})
					}
				}
				continue
			}
			methods = append(methods, vm.IfaceMethod{Name: lt[0].Str, ID: -1})
		}
		// Use any as underlying reflect type; method set is tracked in IfaceMethods.
		return &vm.Type{
			Rtype:        reflect.TypeOf((*any)(nil)).Elem(),
			IfaceMethods: methods,
		}, 2, nil

	default:
		return nil, 0, fmt.Errorf("%w: %v", ErrNotImplemented, in[0].Name())
	}
}

// parseParamTypes parses a list of comma separated typed parameters and returns a list of
// runtime types. Implicit parameter names and types are supported.
func (p *Parser) parseParamTypes(in Tokens, flag typeFlag) (types []*vm.Type, vars []string, variadic bool, err error) {
	// Parse from right to left, to allow multiple comma separated parameters of the same type.
	list := in.Split(lang.Comma)
	for i := len(list) - 1; i >= 0; i-- {
		t := list[i]
		if len(t) == 0 {
			continue
		}
		param := ""
		if p.hasFirstParam(t) {
			origName := t[0].Str
			param = p.scopedName(origName)
			t = t[1:]
			if len(t) == 0 {
				if len(types) == 0 {
					// No type seen from the right yet. For output params, a lone ident
					// that is not in the symbol table might be a forward-declared type;
					// return ErrUndefined so the lazy fixpoint can retry.
					if flag == parseTypeOut {
						if _, _, ok := p.Symbols.Get(origName, p.scope); !ok {
							return nil, nil, false, ErrUndefined{origName}
						}
					}
					return nil, nil, false, ErrMissingType
				}
				// Type was omitted, apply the previous one from the right.
				types = append([]*vm.Type{types[0]}, types...)
				p.addSymVar(i, len(list), param, types[0], flag)
				vars = append([]string{param}, vars...)
				continue
			}
		}
		// Detect variadic parameter: ...T becomes []T.
		if len(t) > 0 && t[0].Tok == lang.Ellipsis {
			variadic = true
			t = t[1:]
		}
		typ, _, err := p.parseTypeExpr(t)
		if err != nil {
			return nil, nil, false, err
		}
		if variadic && i == len(list)-1 {
			typ = vm.SliceOf(typ)
		}
		if param != "" {
			p.addSymVar(i, len(list), param, typ, flag)
		}
		types = append([]*vm.Type{typ}, types...)
		vars = append([]string{param}, vars...)
	}
	return types, vars, variadic, err
}

func (p *Parser) addSymVar(index, nparams int, name string, typ *vm.Type, flag typeFlag) {
	if p.typeOnly {
		return
	}
	zv := vm.NewValue(typ.Rtype)
	switch flag {
	case parseTypeRecv:
		// Receiver lives in Heap[0] of the method closure, not on the call stack.
		// Index is irrelevant; the compiler emits HeapGet 0 via FreeVars.
		p.SymSet(name, &symbol.Symbol{
			Kind: symbol.LocalVar, Name: name, Index: symbol.UnsetAddr,
			Captured: true, Used: true,
			Type: typ, Value: zv,
		})
	case parseTypeIn:
		p.SymAdd(index-nparams-2, name, zv, symbol.LocalVar, typ)
	case parseTypeOut:
		p.SymAdd(p.framelen[p.funcScope], name, zv, symbol.LocalVar, typ)
		p.framelen[p.funcScope]++
		if name != "" {
			p.namedOut = append(p.namedOut, name)
		}
	case parseTypeVar:
		if p.funcScope == "" {
			if s, ok := p.Symbols[name]; ok && s.Index != symbol.UnsetAddr {
				// Preserve pre-assigned index from allocGlobalSlots.
				s.Type = typ
				s.Value = zv
			} else {
				p.SymAdd(symbol.UnsetAddr, name, zv, symbol.Var, typ)
			}
			break
		}
		p.SymAdd(p.framelen[p.funcScope], name, zv, symbol.LocalVar, typ)
		p.framelen[p.funcScope]++
	}
}

// parseFuncParams parses function input and output parameter lists.
// argBlock is the ParenBlock token containing input params; out contains
// output param tokens (may be empty, a single ParenBlock, or bare tokens).
// Returns the function type and raw parameter names.
func (p *Parser) parseFuncParams(argBlock Token, out Tokens) (typ *vm.Type, inNames, outNames []string, err error) {
	iargs, err := p.Scan(argBlock.Block(), false)
	if err != nil {
		return nil, nil, nil, err
	}
	arg, argNames, isVariadic, err := p.parseParamTypes(iargs, parseTypeIn)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(out) > 0 && out[0].Tok == lang.BraceBlock {
		// BraceBlock at start of out is a function body or composite literal, not a return type.
		out = nil
	} else if len(out) == 1 && out[0].Tok == lang.ParenBlock {
		if out, err = p.Scan(out[0].Block(), false); err != nil {
			return nil, nil, nil, err
		}
	}
	ret, retNames, _, err := p.parseParamTypes(out, parseTypeOut)
	if err != nil {
		return nil, nil, nil, err
	}
	return vm.FuncOf(arg, ret, isVariadic), baseNames(argNames), baseNames(retNames), nil
}

// parseFuncSig parses a named function signature (receiver must be pre-stripped)
// and returns its type along with raw parameter names. Suppresses addSymVar
// so Phase 1 does not register param symbols.
func (p *Parser) parseFuncSig(in Tokens) (typ *vm.Type, inNames, outNames []string, err error) {
	if len(in) == 0 || in[0].Tok != lang.Func {
		return nil, nil, nil, ErrFuncType
	}
	var out Tokens
	var indexArgs int
	switch l, in1 := len(in), in[1]; {
	case l >= 3 && in1.Tok == lang.Ident:
		indexArgs, out = 2, in[3:]
	case l >= 2 && in1.Tok == lang.ParenBlock:
		indexArgs, out = 1, in[2:]
	default:
		return nil, nil, nil, ErrFuncType
	}
	p.typeOnly = true
	typ, inNames, outNames, err = p.parseFuncParams(in[indexArgs], out)
	p.typeOnly = false
	return
}

// baseNames extracts raw (unscoped) names from scoped parameter names.
// Returns nil if all names are empty (unnamed parameters).
func baseNames(scoped []string) []string {
	var raw []string
	for i, s := range scoped {
		if s == "" {
			continue
		}
		if raw == nil {
			raw = make([]string, len(scoped))
		}
		if j := strings.LastIndex(s, "/"); j >= 0 {
			raw[i] = s[j+1:]
		} else {
			raw[i] = s
		}
	}
	return raw
}

// parseEmbeddedField checks if lt matches an embedded (anonymous) struct field pattern
// (`TypeName` or `*TypeName`). Returns the field type and the symbol-table type, or nil, nil.
func (p *Parser) parseEmbeddedField(lt Tokens) (fieldType, origType *vm.Type) {
	isPtr := false
	toks := lt
	if len(toks) >= 2 && toks[0].Tok == lang.Mul {
		isPtr = true
		toks = toks[1:]
	}
	if len(toks) != 1 || toks[0].Tok != lang.Ident {
		return nil, nil
	}
	s, _, ok := p.Symbols.Get(toks[0].Str, p.scope)
	if !ok || s.Kind != symbol.Type {
		return nil, nil
	}
	ft := *s.Type
	ft.Name = toks[0].Str
	if isPtr {
		return vm.PointerTo(&ft), s.Type
	}
	return &ft, s.Type
}

// hasFirstParam returns true if the first token of a list is a parameter name.
func (p *Parser) hasFirstParam(in Tokens) bool {
	if in[0].Tok != lang.Ident {
		return false
	}
	s, _, ok := p.Symbols.Get(in[0].Str, p.scope)
	if !ok || s.Kind != symbol.Type {
		return true
	}
	// The first ident is a known type name. If followed by tokens that start
	// a type expression, treat the ident as a field/param name (e.g. rune [N]T).
	if len(in) > 1 {
		switch in[1].Tok {
		case lang.BracketBlock, lang.Mul, lang.Func, lang.Ident, lang.Struct, lang.Map, lang.Interface, lang.Chan:
			return true
		}
	}
	return false
}

// parseStructType parses a struct type expression: struct { fields... }.
// in[0] must be lang.Struct and in[1] must be lang.BraceBlock.
func (p *Parser) parseStructType(in Tokens) (*vm.Type, error) {
	if len(in) < 2 || in[1].Tok != lang.BraceBlock {
		return nil, fmt.Errorf("%w: %v", ErrSyntax, in)
	}
	fieldToks, err := p.Scan(in[1].Block(), false)
	if err != nil {
		return nil, err
	}
	var fields []*vm.Type
	var embedded []vm.EmbeddedField
	for _, lt := range fieldToks.Split(lang.Semicolon) {
		if len(lt) == 0 {
			continue
		}
		if f, origType := p.parseEmbeddedField(lt); f != nil {
			embedded = append(embedded, vm.EmbeddedField{FieldIdx: len(fields), Type: origType})
			fields = append(fields, f)
			continue
		}
		types, names, _, err := p.parseParamTypes(lt, parseTypeType)
		if err != nil {
			// A lone ident that failed embedded-field lookup and param-type
			// parsing is likely a forward-declared type. Return ErrUndefined
			// so the lazy fixpoint loop can retry after the type is defined.
			if errors.Is(err, ErrMissingType) && len(lt) == 1 && lt[0].Tok == lang.Ident {
				return nil, ErrUndefined{lt[0].Str}
			}
			return nil, err
		}
		for i, name := range names {
			if j := strings.LastIndex(name, "/"); j >= 0 {
				name = name[j+1:]
			}
			pkgPath := ""
			if len(name) > 0 && unicode.IsLower(rune(name[0])) {
				pkgPath = p.pkgName
			}
			// A struct field whose type is a placeholder (not yet finalized via SetFields)
			// means the containing struct's size cannot be computed yet. Return ErrUndefined
			// so the retry loop defers this declaration until the placeholder is finalized.
			if types[i].Rtype.Kind() == reflect.Struct && types[i].Placeholder {
				return nil, ErrUndefined{types[i].Name}
			}
			// Copy parscan-level type (preserving Params, IfaceMethods, etc.) and set field name.
			ft := *types[i]
			ft.Name = name
			ft.PkgPath = pkgPath
			fields = append(fields, &ft)
		}
	}
	return vm.StructOf(fields, embedded), nil
}
