package parser

import (
	"errors"
	"go/constant"
	"go/token"
	"strings"

	"github.com/mvertes/parscan/lang"
	"github.com/mvertes/parscan/scanner"
	"github.com/mvertes/parscan/vm"
)

var nilValue = vm.ValueOf(nil)

func (p *Parser) ParseConst(in Tokens) (out Tokens, err error) {
	if len(in) < 2 {
		return out, errors.New("missing expression")
	}
	if in[1].Id != lang.ParenBlock {
		return p.parseConstLine(in[1:])
	}
	if in, err = p.Scan(in[1].Block(), false); err != nil {
		return out, err
	}
	var cnt int64
	p.symbols["iota"].cval = constant.Make(cnt)
	var prev Tokens
	for i, lt := range in.Split(lang.Semicolon) {
		if i > 0 && len(lt) == 1 {
			lt = append(Tokens{lt[0]}, prev...) // Handle implicit repetition of the previous expression.
		}
		ot, err := p.parseConstLine(lt)
		if err != nil {
			return out, err
		}
		out = append(out, ot...)
		prev = lt[1:]
		cnt++
		p.symbols["iota"].cval = constant.Make(cnt)
	}
	return out, err
}

func (p *Parser) parseConstLine(in Tokens) (out Tokens, err error) {
	decl := in
	var assign Tokens
	if i := decl.Index(lang.Assign); i >= 0 {
		assign = decl[i+1:]
		decl = decl[:i]
	}
	var vars []string
	if _, vars, err = p.parseParamTypes(decl, parseTypeVar); err != nil {
		if errors.Is(err, MissingTypeErr) {
			for _, lt := range decl.Split(lang.Comma) {
				vars = append(vars, lt[0].Str)
				name := strings.TrimPrefix(p.scope+"/"+lt[0].Str, "/")
				p.addSym(unsetAddr, name, nilValue, symConst, nil, false)
			}
		} else {
			return out, err
		}
	}
	values := assign.Split(lang.Comma)
	if len(values) == 1 && len(values[0]) == 0 {
		values = nil
	}
	for i, v := range values {
		if v, err = p.ParseExpr(v); err != nil {
			return out, err
		}
		cval, _, err := p.evalConstExpr(v)
		if err != nil {
			return out, err
		}
		name := strings.TrimPrefix(p.scope+"/"+vars[i], "/")
		p.symbols[name] = &symbol{
			kind:  symConst,
			index: unsetAddr,
			cval:  cval,
			value: vm.ValueOf(constValue(cval)),
			local: p.funcScope != "",
			used:  true,
		}
		// TODO: type conversion when applicable.
	}
	return out, err
}

func (p *Parser) evalConstExpr(in Tokens) (cval constant.Value, length int, err error) {
	l := len(in) - 1
	if l < 0 {
		return nil, 0, errors.New("missing argument")
	}
	t := in[l]
	id := t.Id
	switch {
	case id.IsBinaryOp():
		op1, l1, err := p.evalConstExpr(in[:l])
		if err != nil {
			return nil, 0, err
		}
		op2, l2, err := p.evalConstExpr(in[:l-l1])
		if err != nil {
			return nil, 0, err
		}
		length = 1 + l1 + l2
		tok := gotok[id]
		if id.IsBoolOp() {
			return constant.MakeBool(constant.Compare(op1, tok, op2)), length, err
		}
		if id == lang.Shl || id == lang.Shr {
			s, ok := constant.Uint64Val(op2)
			if !ok {
				return nil, 0, errors.New("invalid shift parameter")
			}
			return constant.Shift(op1, tok, uint(s)), length, err
		}
		if tok == token.QUO && op1.Kind() == constant.Int && op2.Kind() == constant.Int {
			tok = token.QUO_ASSIGN // Force int result, see https://pkg.go.dev/go/constant#BinaryOp
		}
		return constant.BinaryOp(op1, tok, op2), length, err
	case id.IsUnaryOp():
		op1, l1, err := p.evalConstExpr(in[:l])
		if err != nil {
			return nil, 0, err
		}
		return constant.UnaryOp(gotok[id], op1, 0), 1 + l1, err
	case id.IsLiteral():
		return constant.MakeFromLiteral(t.Str, gotok[id], 0), 1, err
	case id == lang.Ident:
		s, _, ok := p.getSym(t.Str, p.scope)
		if !ok {
			return nil, 0, errors.New("symbol not found")
		}
		if s.kind != symConst {
			return nil, 0, errors.New("symbol is not a constant")
		}
		return s.cval, 1, err
	case id == lang.Call:
		// TODO: implement support for type conversions and builtin calls.
		panic("not implemented yet")
	default:
		return nil, 0, errors.New("invalid constant expression")
	}
}

func constValue(c constant.Value) any {
	switch c.Kind() {
	case constant.Bool:
		return constant.BoolVal(c)
	case constant.String:
		return constant.StringVal(c)
	case constant.Int:
		v, _ := constant.Int64Val(c)
		return int(v)
	case constant.Float:
		v, _ := constant.Float64Val(c)
		return v
	}
	return nil
}

var gotok = map[lang.TokenId]token.Token{
	lang.Char:         token.CHAR,
	lang.Imag:         token.IMAG,
	lang.Int:          token.INT,
	lang.Float:        token.FLOAT,
	lang.Add:          token.ADD,
	lang.Sub:          token.SUB,
	lang.Mul:          token.MUL,
	lang.Quo:          token.QUO,
	lang.Rem:          token.REM,
	lang.And:          token.AND,
	lang.Or:           token.OR,
	lang.Xor:          token.XOR,
	lang.Shl:          token.SHL,
	lang.Shr:          token.SHR,
	lang.AndNot:       token.AND_NOT,
	lang.Equal:        token.EQL,
	lang.Greater:      token.GTR,
	lang.Less:         token.LSS,
	lang.GreaterEqual: token.GEQ,
	lang.LessEqual:    token.LEQ,
	lang.NotEqual:     token.NEQ,
	lang.Plus:         token.ADD,
	lang.Minus:        token.SUB,
	lang.BitComp:      token.XOR,
	lang.Not:          token.NOT,
}

func (p *Parser) ParseType(in Tokens) (out Tokens, err error) {
	if len(in) < 2 {
		return out, MissingTypeErr
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
		return out, MissingTypeErr
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
	p.addSym(unsetAddr, in[0].Str, vm.NewValue(typ), symType, typ, p.funcScope != "")
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
		if errors.Is(err, MissingTypeErr) {
			for _, lt := range decl.Split(lang.Comma) {
				vars = append(vars, lt[0].Str)
				name := strings.TrimPrefix(p.scope+"/"+lt[0].Str, "/")
				if p.funcScope == "" {
					p.addSym(unsetAddr, name, nilValue, symVar, nil, false)
					continue
				}
				p.addSym(p.framelen[p.funcScope], name, nilValue, symVar, nil, false)
				p.framelen[p.funcScope]++
			}
		} else {
			return out, err
		}
	}
	values := assign.Split(lang.Comma)
	if len(values) == 1 && len(values[0]) == 0 {
		values = nil
	}
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
