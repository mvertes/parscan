package parser

import (
	"fmt"
	"log"
	"reflect"

	"github.com/gnolang/parscan/lang"
)

// ParseType parses a list of tokens defining a type expresssion and returns
// the corresponding runtime type or an error.
func (p *Parser) ParseType(in Tokens) (typ reflect.Type, err error) {
	log.Println("ParseType", in)
	switch in[0].Id {
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
		arg, err := p.parseParamTypes(iargs, true)
		if err != nil {
			return nil, err
		}
		// Output parameters may be empty, or enclosed or not by parenthesis.
		if len(out) == 1 && out[0].Id == lang.ParenBlock {
			if out, err = p.Scan(out[0].Block(), false); err != nil {
				return nil, err
			}
		}
		ret, err := p.parseParamTypes(out, false)
		if err != nil {
			return nil, err
		}
		return reflect.FuncOf(arg, ret, false), nil

	case lang.Ident:
		// TODO: selector expression (pkg.type)
		s, _, ok := p.getSym(in[0].Str, p.scope)
		if !ok || s.kind != symType {
			return nil, fmt.Errorf("invalid type %s", in[0].Str)
		}
		return s.Type, nil
	}
	return typ, err
}

// parseParamTypes parses a list of comma separated typed parameters and returns a list of
// runtime types. Implicit parameter names and types are supported.
func (p *Parser) parseParamTypes(in Tokens, arg bool) (types []reflect.Type, err error) {
	// Parse from right to left, to allow multiple comma separated parameters of the same type.
	list := in.Split(lang.Comma)
	for i := len(list) - 1; i >= 0; i-- {
		t := list[i]
		if len(t) == 0 {
			continue
		}
		param := ""
		if p.hasFirstParam(t) {
			param = t[0].Str
			t = t[1:]
			if len(t) == 0 {
				if len(types) == 0 {
					return nil, fmt.Errorf("Invalid type %v", t[0])
				}
				// Type was ommitted, apply the previous one from the right.
				types = append([]reflect.Type{types[0]}, types...)
				if arg {
					p.addSym(-i-2, p.scope+param, nil, symVar, types[0], true)
				} else {
					p.addSym(i, p.scope+param, nil, symVar, types[0], true)
				}
				continue
			}
		}
		typ, err := p.ParseType(t)
		if err != nil {
			return nil, err
		}
		if param != "" {
			if arg {
				p.addSym(-i-2, p.scope+param, nil, symVar, typ, true)
			} else {
				p.addSym(i, p.scope+param, nil, symVar, typ, true)
			}
		}
		types = append([]reflect.Type{typ}, types...)
	}
	return types, err
}

// hasFirstParam returns true if the first token of a list is a parameter name.
func (p *Parser) hasFirstParam(in Tokens) bool {
	if in[0].Id != lang.Ident {
		return false
	}
	s, _, ok := p.getSym(in[0].Str, p.scope)
	return !ok || s.kind != symType
}
