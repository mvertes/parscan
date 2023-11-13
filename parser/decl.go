package parser

import (
	"errors"
	"log"
	"strings"

	"github.com/gnolang/parscan/lang"
	"github.com/gnolang/parscan/scanner"
)

func (p *Parser) ParseType(in Tokens) (out Tokens, err error) {
	if len(in) < 2 {
		return out, missingTypeError
	}
	if in[1].Id != lang.ParenBlock {
		return p.parseTypeLine(in[1:])
	}
	if in, err = p.Scan(in[1].Block(), false); err != nil {
		return out, err
	}
	for _, lt := range in.Split(lang.Semicolon) {
		ot, err := p.parseTypeLine(lt)
		if err != nil {
			return out, err
		}
		out = append(out, ot...)
	}
	return out, err
}

func (p *Parser) parseTypeLine(in Tokens) (out Tokens, err error) {
	if len(in) < 2 {
		return out, missingTypeError
	}
	if in[0].Id != lang.Ident {
		return out, errors.New("not an ident")
	}
	isAlias := in[1].Id == lang.Assign
	toks := in[1:]
	if isAlias {
		toks = toks[1:]
	}
	typ, err := p.ParseTypeExpr(toks)
	if err != nil {
		return out, err
	}
	p.addSym(unsetAddr, in[0].Str, nil, symType, typ, p.funcScope != "")
	return out, err
}

func (p *Parser) ParseVar(in Tokens) (out Tokens, err error) {
	if len(in) < 2 {
		return out, errors.New("missing expression")
	}
	if in[1].Id != lang.ParenBlock {
		return p.parseVarLine(in[1:])
	}
	if in, err = p.Scan(in[1].Block(), false); err != nil {
		return out, err
	}
	for _, lt := range in.Split(lang.Semicolon) {
		if lt, err = p.parseVarLine(lt); err != nil {
			return out, err
		}
		out = append(out, lt...)
	}
	return out, err
}

func (p *Parser) parseVarLine(in Tokens) (out Tokens, err error) {
	decl := in
	var assign Tokens
	if i := decl.Index(lang.Assign); i >= 0 {
		assign = decl[i+1:]
		decl = decl[:i]
	}
	var vars []string
	if _, vars, err = p.parseParamTypes(decl, parseTypeVar); err != nil {
		if errors.Is(err, missingTypeError) {
			for _, lt := range decl.Split(lang.Comma) {
				vars = append(vars, lt[0].Str)
				// TODO: compute type from rhs
				p.addSym(unsetAddr, strings.TrimPrefix(p.scope+"/"+lt[0].Str, "/"), nil, symVar, nil, false)
			}
		} else {
			return out, err
		}
	}
	values := assign.Split(lang.Comma)
	if len(values) == 1 && len(values[0]) == 0 {
		values = nil
	}
	log.Println("ParseVar:", vars, values, len(values))
	for i, v := range values {
		if v, err = p.ParseExpr(v); err != nil {
			return out, err
		}
		out = append(out, v...)
		out = append(out,
			scanner.Token{Id: lang.Ident, Str: vars[i]},
			scanner.Token{Id: lang.Assign})
	}
	return out, err
}
