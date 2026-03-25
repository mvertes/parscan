package goparser

import (
	"errors"
	"log"
	"strconv"
	"strings"

	"github.com/mvertes/parscan/lang"
	"github.com/mvertes/parscan/symbol"
	"github.com/mvertes/parscan/vm"
)

// parseExpr transforms an infix expression into a postfix notation.
func (p *Parser) parseExpr(in Tokens, typeStr string) (out Tokens, err error) {
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

	flushops := func(minPrec int) {
		for len(ops) > 0 && p.precedence(ops[len(ops)-1]) >= minPrec {
			out = append(out, popop())
		}
	}

	addop := func(t Token) {
		flushops(p.precedence(t) + 1)
		ops = append(ops, t)
	}

	lin := len(in)
	for i := 0; i < lin; i++ {
		switch t := in[i]; t.Tok {
		case lang.Int, lang.Float, lang.String:
			out = append(out, t)

		case lang.Func:
			// Function as value (i.e closure).
			bi := in.LastIndex(lang.BraceBlock)
			if out, err = p.parseFunc(in); err != nil {
				return out, err
			}
			fid := out[1]
			fid.Tok = lang.Ident
			out = append(out, fid)
			i = bi // advance past body; loop will increment to bi+1 (e.g. IIFE call args)

		case lang.Period:
			if i+1 < lin && in[i+1].Tok == lang.ParenBlock {
				// Type assertion: x.(T).
				flushops(p.precedence(t))
				block := in[i+1].Block()
				btoks, err := p.Scan(block, false)
				if err != nil {
					return out, err
				}
				typ, _, err := p.parseTypeExpr(btoks)
				if err != nil {
					return out, err
				}
				out = append(out, newTypeAssert(typ, t.Pos, 0))
				i++ // Skip following ParenBlock.
			} else {
				// Normal field selector. Use left-associative flushing so that
				// postfix chains like foo().Name evaluate the call before the access.
				t.Str += in[i+1].Str
				flushops(p.precedence(t))
				ops = append(ops, t)
				i++ // Skip over next ident.
			}

		case lang.Next:
			out = append(out, t)

		case lang.Range:
			addop(t)

		case lang.Colon:
			t.Str = typeStr
			addop(t)

		case lang.Add, lang.And, lang.AndNot, lang.Equal, lang.Greater, lang.GreaterEqual, lang.Less, lang.LessEqual, lang.Mul, lang.Not, lang.NotEqual, lang.Or, lang.Quo, lang.Rem, lang.Sub, lang.Shl, lang.Shr, lang.Xor:
			if i == 0 || in[i-1].Tok.IsOperator() {
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
			// Free variable detection: defined in an enclosing function scope.
			// Exclude variables defined in sub-scopes of the current function (e.g. for loops).
			isInnerScope := sc == p.funcScope || strings.HasPrefix(sc, p.funcScope+"/")
			if ok && s != nil && s.Kind == symbol.LocalVar && sc != "" && p.fname != "" && !isInnerScope {
				if cloSym := p.Symbols[p.fname]; cloSym != nil {
					if cloSym.FreeVarIndex(t.Str) < 0 {
						cloSym.FreeVars = append(cloSym.FreeVars, t.Str)
						s.Captured = true
					}
				}
			}
			if s != nil && s.Kind == symbol.Type {
				ctype = s.Type.String()
			}
			out = append(out, t)

		case lang.ParenBlock:
			toks, err := p.parseBlock(t, typeStr)
			if err != nil {
				return out, err
			}
			if i == 0 || in[i-1].Tok.IsOperator() {
				out = append(out, toks...)
			} else {
				flushops(p.precedence(newCall(0)))
				// func call: ensure that the func token in on the top of the stack, after args.
				ops = append(ops, newCall(t.Pos, p.numItems(t.Block(), lang.Comma)))
				out = append(out, toks...)
			}

		case lang.BraceBlock:
			if ctype == "" {
				// Infer composite inner type from passed typeStr.
				sym := p.Symbols[typeStr]
				if sym == nil || sym.Type == nil {
					// Type not yet defined: look for preceding Ident in output.
					name := typeStr
					if len(out) > 0 && out[len(out)-1].Tok == lang.Ident {
						name = out[len(out)-1].Str
					}
					return out, ErrUndefined{Name: name}
				}
				typ := sym.Type.Elem()
				ctype = typ.String()
				p.SymAdd(symbol.UnsetAddr, ctype, vm.NewValue(typ.Rtype), symbol.Type, typ)
				out = append(out, newIdent(ctype, t.Pos))
			}
			toks, err := p.parseComposite(t.Block(), ctype)
			out = append(out, toks...)
			if err != nil {
				return out, err
			}
			ops = append(ops, newComposite(t.Pos))

		case lang.BracketBlock:
			if i == 0 || in[i-1].Tok.IsOperator() || in[i-1].Tok == lang.Range {
				// Array or slice type expression.
				elemTyp, n, err := p.parseTypeExpr(in[i:])
				if errors.Is(err, ErrEllipsisArray) {
					elemTyp, err = p.resolveEllipsisArray(elemTyp, in, i+n)
				}
				if err != nil {
					return out, err
				}
				ctype = elemTyp.String()
				p.SymAdd(symbol.UnsetAddr, ctype, vm.NewValue(elemTyp.Rtype), symbol.Type, elemTyp)
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
			if toks[len(toks)-1].Tok != lang.Slice {
				ops = append(ops, newIndex(t.Pos))
			}

		case lang.Struct:
			typ, _, err := p.parseTypeExpr(in[i : i+2])
			if err != nil {
				return out, err
			}
			ctype = typ.String()
			p.SymAdd(symbol.UnsetAddr, ctype, vm.NewValue(typ.Rtype), symbol.Type, typ)
			out = append(out, newIdent(ctype, t.Pos))
			i++

		case lang.Map:
			typ, n, err := p.parseTypeExpr(in[i:])
			if err != nil {
				return out, err
			}
			ctype = typ.String()
			p.SymAdd(symbol.UnsetAddr, ctype, vm.NewValue(typ.Rtype), symbol.Type, typ)
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
		if len(sub) == 0 {
			continue
		}
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
		// Slice expression, a[low : high] or a[low : high : max].
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
		result = append(result, toks...)
	}

	return result, err
}
