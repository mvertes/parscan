package parser

import (
	"fmt"
	"log"
	"strconv"

	"github.com/mvertes/parscan/lang"
	"github.com/mvertes/parscan/scanner"
	"github.com/mvertes/parscan/vm"
)

func (p *Parser) parseExpr(in Tokens) (out Tokens, err error) {
	log.Println("parseExpr in:", in)
	var ops, selectors Tokens
	var vl int
	var selectorIndex string

	//
	// Process tokens from last to first, the goal is to reorder the tokens in
	// a stack machine processing order, so it can be directly interpreted.
	//
	if len(in) > 1 && in[0].Tok == lang.Func {
		// Function as value (i.e closure).
		if out, err = p.parseFunc(in); err != nil {
			return out, err
		}
		// Get function label and use it as a symbol ident.
		fid := out[1]
		fid.Tok = lang.Ident
		out = append(out, fid)
		return out, err
	}

	for i := len(in) - 1; i >= 0; i-- {
		t := in[i]
		// temporary assumptions: binary operators, returning 1 value
		switch t.Tok {
		case lang.Ident:
			if i > 0 && in[i-1].Tok == lang.Period {
				selectorIndex = t.Str
				continue
			}
			// resolve symbol if not a selector rhs.
			_, sc, ok := p.GetSym(t.Str, p.scope)
			if ok {
				if sc != "" {
					t.Str = sc + "/" + t.Str
				}
			}
			out = append(out, t)
			vl++

		case lang.Colon:
			// Make ':' a key-value operator for literal composite.
			ops = append(ops, t)

		case lang.Period:
			t.Str += selectorIndex
			selectors = append(Tokens{t}, selectors...)
			continue

		case lang.Int, lang.String:
			out = append(out, t)
			vl++

		case lang.Define, lang.Add, lang.Sub, lang.Assign, lang.Equal, lang.Greater, lang.Less,
			lang.Mul, lang.Land, lang.Lor, lang.Shl, lang.Shr, lang.Not, lang.And:
			if i == 0 || in[i-1].Tok.IsOperator() {
				// An operator preceded by an operator or no token is unary.
				t.Tok = lang.UnaryOp[t.Tok]
				j := len(out) - 1
				l := out[j]
				if p.precedence(l) > 0 {
					out = append(out[:j], t, l)
					break
				}
				out = append(out, t)
				break
			}
			if vl < 2 {
				ops = append(ops, t)
			}

		case lang.ParenBlock:
			// If the previous token is an arithmetic, logic or assign operator then
			// this parenthesis block is an enclosed expr, otherwise a call expr.
			if i == 0 || in[i-1].Tok.IsOperator() {
				out = append(out, t)
				vl++
				break
			}
			// The call expression can be a function call, a conversion,
			// a type assersion (including for type switch)
			// func call: push args and func address then call
			out = append(out, t)
			vl++
			ops = append(ops, scanner.Token{Tok: lang.Call, Pos: t.Pos, Beg: p.numItems(t.Block(), lang.Comma)})

		case lang.BraceBlock:
			// the block can be a func body or a composite type content.
			// In both cases it is preceded by a type definition.
			// We must determine the starting token of type def,
			// parse the type def, and substitute the type def by a single ident.
			// TODO: handle implicit type in composite expression.
			ti := p.typeStartIndex(in[:len(in)-1])
			if ti == -1 {
				return out, ErrInvalidType
			}
			typ, err := p.parseTypeExpr(in[ti : len(in)-1])
			if err != nil {
				return out, ErrInvalidType
			}
			p.AddSymbol(UnsetAddr, typ.String(), vm.NewValue(typ), SymType, typ, p.funcScope != "")
			out = append(out, t, scanner.Token{Tok: lang.Ident, Pos: t.Pos, Str: typ.String()})
			i = ti
			vl += 2
			ops = append(ops, scanner.Token{Tok: lang.Composite, Pos: t.Pos})

		case lang.BracketBlock:
			out = append(out, t)
			vl++
			ops = append(ops, scanner.Token{Tok: lang.Index, Pos: t.Pos})

		case lang.Comment:
			return out, nil

		default:
			return nil, fmt.Errorf("invalid expression: %v: %q", t.Tok, t.Str)
		}

		if len(selectors) > 0 {
			out = append(out, selectors...)
			selectors = nil
		}

		if lops, lout := len(ops), len(out); lops > 0 && vl > lops {
			op := ops[lops-1]
			ops = ops[:lops-1]
			// Reorder tokens according to operator precedence rules.
			if p.precedence(out[lout-2]) > p.precedence(op) {
				op, out[lout-1], out[lout-2] = out[lout-2], op, out[lout-1]
				if p.precedence(out[lout-3]) > p.precedence(out[lout-1]) {
					out[lout-1], out[lout-2], out[lout-3] = out[lout-3], out[lout-1], out[lout-2]
				}
			}
			out = append(out, op)
			vl--
		}
	}
	out = append(out, ops...)

	log.Println("parseExpr out:", out, "vl:", vl, "ops:", ops)
	// A logical operator (&&, ||) involves additional control flow operations.
	if out, err = p.parseLogical(out); err != nil {
		return out, err
	}

	if l := len(out) - 1; l >= 0 && (out[l].Tok == lang.Define || out[l].Tok == lang.Assign) {
		// Handle the assignment of a logical expression.
		s1 := p.subExprLen(out[:l])
		head, err := p.parseLogical(out[:l-s1])
		if err != nil {
			return out, err
		}
		out = append(head, out[l-s1:]...)
	}

	// The tokens are now properly ordered, process nested blocks.
	for i := len(out) - 1; i >= 0; i-- {
		t := out[i]
		var toks Tokens
		switch t.Tok {
		case lang.ParenBlock, lang.BracketBlock, lang.BraceBlock:
			if toks, err = p.parseExprStr(t.Block()); err != nil {
				return out, err
			}
		default:
			continue
		}

		// replace block token by its parsed result.
		log.Println("toks:", toks)
		out2 := append(Tokens{}, out[:i]...)
		out2 = append(out2, toks...)
		out = append(out2, out[i+1:]...)
	}

	log.Println("Final out:", out)
	return out, err
}

func (p *Parser) parseExprStr(s string) (tokens Tokens, err error) {
	if tokens, err = p.Scan(s, false); err != nil {
		return tokens, err
	}

	var result Tokens
	for _, sub := range tokens.Split(lang.Comma) {
		toks, err := p.parseExpr(sub)
		if err != nil {
			return result, err
		}
		result = append(toks, result...)
	}

	return result, err
}

// parseLogical handles logical expressions with control flow (&& and ||) by
// ensuring the left hand side is evaluated unconditionally first, then the
// right hand side can be skipped or not by inserting a conditional jump and label.
// If the last token is not a logical operator then the function is idempotent.
func (p *Parser) parseLogical(in Tokens) (out Tokens, err error) {
	l := len(in) - 1
	if l < 0 || !in[l].Tok.IsLogicalOp() {
		return in, nil
	}

	xp := strconv.Itoa(p.labelCount[p.scope])
	p.labelCount[p.scope]++
	rhsIndex := p.subExprLen(in[:l])

	lhs, err := p.parseLogical(in[l-rhsIndex : l])
	if err != nil {
		return out, err
	}

	rhs, err := p.parseLogical(in[:l-rhsIndex])
	if err != nil {
		return out, err
	}
	out = append(out, lhs...)

	if in[l].Tok == lang.Lor {
		out = append(out, scanner.Token{Tok: lang.JumpSetTrue, Str: p.scope + "x" + xp})
	} else {
		out = append(out, scanner.Token{Tok: lang.JumpSetFalse, Str: p.scope + "x" + xp})
	}

	out = append(out, rhs...)
	out = append(out, scanner.Token{Tok: lang.Label, Str: p.scope + "x" + xp})
	return out, err
}

// subExprLen returns the length of the first complete sub-expression starting from the input end.
func (p *Parser) subExprLen(in Tokens) int {
	l := len(in) - 1
	last := in[l]

	switch last.Tok {
	case lang.Int, lang.Float, lang.String, lang.Char, lang.Ident, lang.ParenBlock, lang.BracketBlock:
		return 1

	case lang.Call:
		s1 := p.subExprLen(in[:l])
		return 1 + s1 + p.subExprLen(in[:l-s1])
		// TODO: add selector and index operators when ready
	}

	if last.Tok.IsBinaryOp() {
		s1 := p.subExprLen(in[:l])
		return 1 + s1 + p.subExprLen(in[:l-s1])
	}

	if last.Tok.IsUnaryOp() {
		return 1 + p.subExprLen(in[:l])
	}

	return 0 // should not occur. TODO: diplay some error here.
}
