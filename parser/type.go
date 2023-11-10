package parser

import (
	"errors"
	"fmt"
	"log"
	"reflect"
	"strings"

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

type typeFlag int

const (
	parseTypeIn typeFlag = iota
	parseTypeOut
	parseTypeVar
)

var missingTypeError = errors.New("Missing type")

// parseParamTypes parses a list of comma separated typed parameters and returns a list of
// runtime types. Implicit parameter names and types are supported.
func (p *Parser) parseParamTypes(in Tokens, flag typeFlag) (types []reflect.Type, vars []string, err error) {
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
					return nil, nil, missingTypeError
				}
				// Type was ommitted, apply the previous one from the right.
				types = append([]reflect.Type{types[0]}, types...)
				zv := reflect.New(types[0]).Elem().Interface()
				switch flag {
				case parseTypeIn:
					p.addSym(-i-2, param, zv, symVar, types[0], true)
				case parseTypeOut:
					p.addSym(p.framelen[p.funcScope], param, zv, symVar, types[0], true)
					p.framelen[p.funcScope]++
				case parseTypeVar:
					if local {
						p.addSym(p.framelen[p.funcScope], param, zv, symVar, types[0], local)
						p.framelen[p.funcScope]++
					} else {
						p.addSym(unsetAddr, param, zv, symVar, types[0], local)
					}
				}
				vars = append(vars, param)
				continue
			}
		}
		typ, err := p.ParseType(t)
		if err != nil {
			return nil, nil, err
		}
		if param != "" {
			zv := reflect.New(typ).Elem().Interface()
			switch flag {
			case parseTypeIn:
				p.addSym(-i-2, param, zv, symVar, typ, true)
			case parseTypeOut:
				p.addSym(p.framelen[p.funcScope], param, zv, symVar, typ, true)
				p.framelen[p.funcScope]++
			case parseTypeVar:
				if local {
					p.addSym(p.framelen[p.funcScope], param, zv, symVar, typ, local)
					p.framelen[p.funcScope]++
				} else {
					p.addSym(unsetAddr, param, zv, symVar, typ, local)
				}
			}
		} else if flag == parseTypeOut {
			p.framelen[p.funcScope]++
		}
		types = append([]reflect.Type{typ}, types...)
		vars = append(vars, param)
	}
	return types, vars, err
}

// hasFirstParam returns true if the first token of a list is a parameter name.
func (p *Parser) hasFirstParam(in Tokens) bool {
	if in[0].Id != lang.Ident {
		return false
	}
	s, _, ok := p.getSym(in[0].Str, p.scope)
	return !ok || s.kind != symType
}
