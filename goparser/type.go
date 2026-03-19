package goparser

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

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
	parseTypeRecv // method receiver: Env[0], not a stack param
)

// Type parsing error definitions.
var (
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
		// For methods, parse the receiver separately as Env[0] (not a stack param),
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
		iargs, err := p.Scan(in[indexArgs].Block(), false)
		if err != nil {
			return nil, 0, err
		}
		arg, _, isVariadic, err := p.parseParamTypes(iargs, parseTypeIn)
		if err != nil {
			return nil, 0, err
		}
		// Output parameters may be empty, or enclosed or not by parenthesis.
		if len(out) == 1 && out[0].Tok == lang.ParenBlock {
			if out, err = p.Scan(out[0].Block(), false); err != nil {
				return nil, 0, err
			}
		}
		ret, _, _, err := p.parseParamTypes(out, parseTypeOut)
		if err != nil {
			return nil, 0, err
		}
		return vm.FuncOf(arg, ret, isVariadic), 1 + indexArgs, nil

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
		if len(in) < 2 || in[1].Tok != lang.BraceBlock {
			return nil, 0, fmt.Errorf("%w: %v", ErrSyntax, in)
		}
		if in, err = p.Scan(in[1].Block(), false); err != nil {
			return nil, 0, err
		}
		var fields []*vm.Type
		var embedded []vm.EmbeddedField
		for _, lt := range in.Split(lang.Semicolon) {
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
				return nil, 0, err
			}
			for i, name := range names {
				fields = append(fields, &vm.Type{Name: name, PkgPath: p.pkgName, Rtype: types[i].Rtype})
			}
		}
		return vm.StructOf(fields, embedded), 2, nil

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
			if len(lt) == 0 {
				continue
			}
			if lt[0].Tok != lang.Ident {
				return nil, 0, fmt.Errorf("%w: expected method name in interface", ErrSyntax)
			}
			methods = append(methods, vm.IfaceMethod{Name: lt[0].Str})
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
		local := p.funcScope != ""
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
				p.addSymVar(i, len(list), param, types[0], flag, local)
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
			p.addSymVar(i, len(list), param, typ, flag, local)
		}
		types = append([]*vm.Type{typ}, types...)
		vars = append([]string{param}, vars...)
	}
	return types, vars, variadic, err
}

func (p *Parser) addSymVar(index, nparams int, name string, typ *vm.Type, flag typeFlag, local bool) {
	zv := vm.NewValue(typ.Rtype)
	switch flag {
	case parseTypeRecv:
		// Receiver lives in Env[0] of the method closure, not on the call stack.
		// Index is irrelevant; the compiler emits HGet 0 via FreeVars.
		p.SymSet(name, &symbol.Symbol{
			Kind: symbol.Var, Name: name, Index: symbol.UnsetAddr,
			Local: true, Captured: true, Used: true,
			Type: typ, Value: zv,
		})
	case parseTypeIn:
		p.SymAdd(index-nparams-2, name, zv, symbol.Var, typ, true)
	case parseTypeOut:
		p.SymAdd(p.framelen[p.funcScope], name, zv, symbol.Var, typ, true)
		p.framelen[p.funcScope]++
		if name != "" {
			p.namedOut = append(p.namedOut, name)
		}
	case parseTypeVar:
		if !local {
			p.SymAdd(symbol.UnsetAddr, name, zv, symbol.Var, typ, local)
			break
		}
		p.SymAdd(p.framelen[p.funcScope], name, zv, symbol.Var, typ, local)
		p.framelen[p.funcScope]++
	}
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
	rtyp := s.Type.Rtype
	if isPtr {
		rtyp = reflect.PointerTo(rtyp)
	}
	return &vm.Type{Name: toks[0].Str, Rtype: rtyp}, s.Type
}

// hasFirstParam returns true if the first token of a list is a parameter name.
func (p *Parser) hasFirstParam(in Tokens) bool {
	if in[0].Tok != lang.Ident {
		return false
	}
	s, _, ok := p.Symbols.Get(in[0].Str, p.scope)
	return !ok || s.Kind != symbol.Type
}
