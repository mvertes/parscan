package parser

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mvertes/parscan/lang"
	"github.com/mvertes/parscan/vm"
)

type typeFlag int

const (
	parseTypeIn typeFlag = iota
	parseTypeOut
	parseTypeVar
	parseTypeType
)

var (
	InvalidTypeErr        = errors.New("invalid type")
	MissingTypeErr        = errors.New("missing type")
	SyntaxErr             = errors.New("syntax error")
	TypeNotImplementedErr = errors.New("not implemented")
)

// ParseTypeExpr parses a list of tokens defining a type expresssion and returns
// the corresponding runtime type or an error.
func (p *Parser) ParseTypeExpr(in Tokens) (typ *vm.Type, err error) {
	switch in[0].Id {
	case lang.BracketBlock:
		typ, err := p.ParseTypeExpr(in[1:])
		if err != nil {
			return nil, err
		}
		if b := in[0].Block(); len(b) > 0 {
			x, err := p.Scan(b, false)
			if err != nil {
				return nil, err
			}
			cval, _, err := p.evalConstExpr(x)
			if err != nil {
				return nil, err
			}
			size, ok := constValue(cval).(int)
			if !ok {
				return nil, fmt.Errorf("invalid size")
			}
			return vm.ArrayOf(size, typ), nil
		}
		return vm.SliceOf(typ), nil

	case lang.Mul:
		typ, err := p.ParseTypeExpr(in[1:])
		if err != nil {
			return nil, err
		}
		return vm.PointerTo(typ), nil

	case lang.Func:
		// Get argument and return token positions depending on function pattern:
		// method with receiver, named function or anonymous closure.
		// TODO: handle variadics
		var out Tokens
		var indexArgs int
		switch l, in1 := len(in), in[1]; {
		case l >= 4 && in1.Id == lang.ParenBlock && in[2].Id == lang.Ident:
			indexArgs, out = 3, in[4:]
		case l >= 3 && in1.Id == lang.Ident:
			indexArgs, out = 2, in[3:]
		case l >= 2 && in1.Id == lang.ParenBlock:
			indexArgs, out = 1, in[2:]
		default:
			return nil, fmt.Errorf("invalid func signature")
		}

		// We can now parse function input and output parameter types.
		// Input parameters are always enclosed by parenthesis.
		iargs, err := p.Scan(in[indexArgs].Block(), false)
		if err != nil {
			return nil, err
		}
		arg, _, err := p.parseParamTypes(iargs, parseTypeIn)
		if err != nil {
			return nil, err
		}
		// Output parameters may be empty, or enclosed or not by parenthesis.
		if len(out) == 1 && out[0].Id == lang.ParenBlock {
			if out, err = p.Scan(out[0].Block(), false); err != nil {
				return nil, err
			}
		}
		ret, _, err := p.parseParamTypes(out, parseTypeOut)
		if err != nil {
			return nil, err
		}
		return vm.FuncOf(arg, ret, false), nil

	case lang.Ident:
		// TODO: selector expression (pkg.type)
		s, _, ok := p.getSym(in[0].Str, p.scope)
		if !ok || s.kind != symType {
			return nil, fmt.Errorf("%w: %s", InvalidTypeErr, in[0].Str)
		}
		return s.Type, nil

	case lang.Struct:
		if len(in) != 2 || in[1].Id != lang.BraceBlock {
			return nil, fmt.Errorf("%w: %v", SyntaxErr, in)
		}
		if in, err = p.Scan(in[1].Block(), false); err != nil {
			return nil, err
		}
		var fields []*vm.Type
		for _, lt := range in.Split(lang.Semicolon) {
			types, names, err := p.parseParamTypes(lt, parseTypeType)
			if err != nil {
				return nil, err
			}
			for i, name := range names {
				fields = append(fields, &vm.Type{Name: name, Rtype: types[i].Rtype})
				// TODO: handle embedded fields
			}
		}
		return vm.StructOf(fields), nil

	default:
		return nil, fmt.Errorf("%w: %v", TypeNotImplementedErr, in[0].Name())
	}
}

// parseParamTypes parses a list of comma separated typed parameters and returns a list of
// runtime types. Implicit parameter names and types are supported.
func (p *Parser) parseParamTypes(in Tokens, flag typeFlag) (types []*vm.Type, vars []string, err error) {
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
			param = strings.TrimPrefix(p.scope+"/"+t[0].Str, "/")
			t = t[1:]
			if len(t) == 0 {
				if len(types) == 0 {
					return nil, nil, MissingTypeErr
				}
				// Type was omitted, apply the previous one from the right.
				types = append([]*vm.Type{types[0]}, types...)
				p.addSymVar(i, param, types[0], flag, local)
				vars = append(vars, param)
				continue
			}
		}
		typ, err := p.ParseTypeExpr(t)
		if err != nil {
			return nil, nil, err
		}
		if param != "" {
			p.addSymVar(i, param, typ, flag, local)
		}
		types = append([]*vm.Type{typ}, types...)
		vars = append(vars, param)
	}
	return types, vars, err
}

func (p *Parser) addSymVar(index int, name string, typ *vm.Type, flag typeFlag, local bool) {
	zv := vm.NewValue(typ)
	switch flag {
	case parseTypeIn:
		p.addSym(-index-2, name, zv, symVar, typ, true)
	case parseTypeOut:
		p.addSym(p.framelen[p.funcScope], name, zv, symVar, typ, true)
		p.framelen[p.funcScope]++
	case parseTypeVar:
		if !local {
			p.addSym(unsetAddr, name, zv, symVar, typ, local)
			break
		}
		p.addSym(p.framelen[p.funcScope], name, zv, symVar, typ, local)
		p.framelen[p.funcScope]++
	}
}

// hasFirstParam returns true if the first token of a list is a parameter name.
func (p *Parser) hasFirstParam(in Tokens) bool {
	if in[0].Id != lang.Ident {
		return false
	}
	s, _, ok := p.getSym(in[0].Str, p.scope)
	return !ok || s.kind != symType
}
