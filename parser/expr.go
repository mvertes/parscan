package parser

import (
	"errors"
	"log"
	"strconv"

	"github.com/mvertes/parscan/lang"
	"github.com/mvertes/parscan/scanner"
	"github.com/mvertes/parscan/symbol"
	"github.com/mvertes/parscan/vm"
)

// parseExpr transform an infix expression into a postfix notation.
func (p *Parser) parseExpr(in Tokens, typeStr string) (out Tokens, err error) {
	log.Println("parseExpr in:", in)
	var ops Tokens
	var ctype string

	popop := func() Token {
		l := len(ops) - 1
		t := ops[l]
		ops = ops[:l]
		if t.Tok.IsLogicalOp() {
			t.Tok = lang.Label // Implement conditional branching directly.
		}
		return t
	}

	// addop adds an operator to the operator stack.
	addop := func(t Token) {
		// Operators on stack with a lower precedence are poped out and output first.
		for len(ops) > 0 && p.precedence(t) < p.precedence(ops[len(ops)-1]) {
			out = append(out, popop())
		}
		ops = append(ops, t)
	}

	lin := len(in)
	for i := 0; i < lin; i++ {
		switch t := in[i]; t.Tok {
		case lang.Int, lang.String:
			out = append(out, t)

		case lang.Func:
			// Function as value (i.e closure).
			if out, err = p.parseFunc(in); err != nil {
				return out, err
			}
			// Get function label and use it as a symbol ident.
			fid := out[1]
			fid.Tok = lang.Ident
			out = append(out, fid)
			return out, err

		case lang.Period:
			// TODO: fail if next is not an ident.
			t.Str += in[i+1].Str // Hardwire selector argument.
			addop(t)
			i++ // Skip over next ident.

		case lang.Next:
			out = append(out, t)

		case lang.Range:
			ops = ops[:len(ops)-1] // Suppress previous assign or define.
			addop(t)

		case lang.Colon:
			t.Str = typeStr
			addop(t)

		case lang.Add, lang.And, lang.Assign, lang.Define, lang.Equal, lang.Greater, lang.Less, lang.Mul, lang.Not, lang.Sub, lang.Shl, lang.Shr:
			if i == 0 || in[i-1].Tok.IsOperator() {
				// An operator preceded by an operator or no token is unary.
				t.Tok = lang.UnaryOp[t.Tok]
				// FIXME: parsetype for composite if & or *
			}
			addop(t)

		case lang.Land:
			addop(t)
			xp := strconv.Itoa(p.labelCount[p.scope])
			p.labelCount[p.scope]++
			out = append(out, newJumpSetFalse(p.scope+"x"+xp, t.Pos))
			ops[len(ops)-1].Str = p.scope + "x" + xp

		case lang.Lor:
			addop(t)
			xp := strconv.Itoa(p.labelCount[p.scope])
			p.labelCount[p.scope]++
			out = append(out, newJumpSetTrue(p.scope+"x"+xp, t.Pos))
			ops[len(ops)-1].Str = p.scope + "x" + xp

		case lang.Ident:
			s, sc, ok := p.Symbols.Get(t.Str, p.scope)
			if ok && sc != "" {
				t.Str = sc + "/" + t.Str
			}
			if s != nil && s.Kind == symbol.Type {
				ctype = s.Type.String()
			}
			out = append(out, t)
			if i+1 < len(in) && in[i+1].Tok == lang.Define {
				// Ident is to be assigned next. Define it as a var.
				p.Symbols.Add(symbol.UnsetAddr, t.Str, vm.Value{}, symbol.Var, nil, false)
			}

		case lang.ParenBlock:
			toks, err := p.parseBlock(t, typeStr)
			if err != nil {
				return out, err
			}
			if i == 0 || in[i-1].Tok.IsOperator() {
				out = append(out, toks...)
			} else {
				prec := p.precedence(newCall(0))
				for len(ops) > 0 && prec < p.precedence(ops[len(ops)-1]) {
					out = append(out, popop())
				}
				// func call: ensure that the func token in on the top of the stack, after args.
				ops = append(ops, newCall(t.Pos, p.numItems(t.Block(), lang.Comma)))
				out = append(out, toks...)
			}

		case lang.BraceBlock:
			if ctype == "" {
				// Infer composite inner type from passed typeStr
				typ := p.Symbols[typeStr].Type.Elem()
				ctype = typ.String()
				p.Symbols.Add(symbol.UnsetAddr, ctype, vm.NewValue(typ), symbol.Type, typ, p.funcScope != "")
				out = append(out, newIdent(ctype, t.Pos))
			}
			toks, err := p.parseComposite(t.Block(), ctype)
			out = append(out, toks...)
			if err != nil {
				return out, err
			}
			ops = append(ops, newComposite(t.Pos))

		case lang.BracketBlock:
			if i == 0 || in[i-1].Tok.IsOperator() {
				// array or slice type expression
				typ, n, err := p.parseTypeExpr(in[i:])
				if err != nil {
					return out, err
				}
				ctype = typ.String()
				// p.Symbols.Add(symbol.UnsetAddr, ctype, vm.NewValue(typ), symbol.Type, typ, p.funcScope != "")
				p.Symbols.Add(symbol.UnsetAddr, ctype, vm.NewValue(typ), symbol.Type, typ, false)
				out = append(out, newIdent(ctype, t.Pos))
				i += n - 1
				break
			}
			toks, err := p.parseBlock(t, typeStr)
			if err != nil {
				return out, err
			}
			if len(toks) == 0 {
				break
			}
			out = append(out, toks...)
			if i < len(in)-2 && in[i+1].Tok == lang.Assign {
				// A bracket block followed by assign implies an IndexAssign token,
				// as assignement to a map element cannot be implemented through a normal Assign.
				ops = append(ops, newIndexAssign(t.Pos))
				i++
			} else if toks[len(toks)-1].Tok != lang.Slice {
				ops = append(ops, newIndex(t.Pos))
			}

		case lang.Struct:
			typ, _, err := p.parseTypeExpr(in[i : i+2])
			if err != nil {
				return out, err
			}
			ctype = typ.String()
			p.Symbols.Add(symbol.UnsetAddr, ctype, vm.NewValue(typ), symbol.Type, typ, p.funcScope != "")
			// out = append(out, Token{Token: scanner.Token{Tok: lang.Ident, Pos: t.Pos, Str: ctype}})
			out = append(out, Token{Token: scanner.Token{Tok: lang.Ident, Pos: t.Pos, Str: ctype}})
			i++

		case lang.Map:
			typ, n, err := p.parseTypeExpr(in[i:])
			if err != nil {
				return out, err
			}
			ctype = typ.String()
			p.Symbols.Add(symbol.UnsetAddr, ctype, vm.NewValue(typ), symbol.Type, typ, p.funcScope != "")
			out = append(out, newIdent(ctype, t.Pos))
			i += n - 1

		case lang.Comment:

		default:
			log.Println("unexpected token:", t)
		}
	}
	for len(ops) > 0 {
		out = append(out, popop())
	}
	log.Println("Final out:", out)
	return out, err
}

func (p *Parser) parseComposite(s, typ string) (Tokens, error) {
	tokens, err := p.Scan(s, false)
	if err != nil {
		return nil, err
	}

	noColon := len(tokens) > 0 && tokens.Index(lang.Colon) == -1
	var result Tokens
	var sliceLen int
	for i, sub := range tokens.Split(lang.Comma) {
		toks, err := p.parseExpr(sub, typ)
		if err != nil {
			return result, err
		}
		if noColon {
			// Insert a numeric index key and a colon operator.
			result = append(result, newInt(i, toks[0].Pos))
			result = append(result, toks...)
			result = append(result, newColon(toks[0].Pos))
			sliceLen++
		} else {
			result = append(result, toks...)
		}
	}
	p.Symbols[typ].SliceLen = sliceLen

	return result, nil
}

func (p *Parser) parseBlock(t Token, typ string) (result Tokens, err error) {
	tokens, err := p.Scan(t.Block(), false)
	if err != nil {
		return tokens, err
	}

	if tokens.Index(lang.Colon) >= 0 {
		// Slice expression, a[low : high] or a[low : high : max]
		for i, sub := range tokens.Split(lang.Colon) {
			if i > 2 {
				return nil, errors.New("expected ']', found ':'")
			}
			if len(sub) == 0 {
				if i == 0 {
					result = append(result, newInt(0, tokens[0].Pos))
					continue
				} else if i == 2 {
					return nil, errors.New("final index required in 3-index slice")
				}
				result = append(result, newLen(1, tokens[0].Pos))
				continue
			}
			toks, err := p.parseExpr(sub, typ)
			if err != nil {
				return result, err
			}
			result = append(result, toks...)
		}
		result = append(result, newSlice(t.Pos))
		return result, err
	}

	for _, sub := range tokens.Split(lang.Comma) {
		toks, err := p.parseExpr(sub, typ)
		if err != nil {
			return result, err
		}
		// Inverse sub list order (func call parameters)
		result = append(toks, result...)
	}

	return result, err
}
