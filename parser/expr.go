package parser

import (
	"log"
	"strconv"

	"github.com/gnolang/parscan/lang"
	"github.com/gnolang/parscan/scanner"
)

func (p *Parser) ParseExpr(in Tokens) (out Tokens, err error) {
	log.Println("ParseExpr in:", in)
	var ops Tokens
	var vl int
	//
	// Process tokens from last to first, the goal is to reorder the tokens in
	// a stack machine processing order, so it can be directly interpreted.
	//
	for i := len(in) - 1; i >= 0; i-- {
		t := in[i]
		// temporary assumptions: binary operators, returning 1 value
		switch t.Id {
		case lang.Ident:
			// resolve symbol if not a selector rhs.
			// TODO: test for selector expr.
			_, sc, ok := p.getSym(t.Str, p.scope)
			if ok {
				if sc != "" {
					t.Str = sc + "/" + t.Str
				}
			}
			out = append(out, t)
			vl++
		case lang.Int, lang.String:
			out = append(out, t)
			vl++
		case lang.Define, lang.Add, lang.Sub, lang.Assign, lang.Equal, lang.Greater, lang.Less, lang.Mul, lang.Land, lang.Lor, lang.Shl, lang.Shr:
			if vl < 2 {
				ops = append(ops, t)
				break
			}
		case lang.ParenBlock:
			// If the previous token is an arithmetic, logic or assign operator then
			// this parenthesis block is an enclosed expr, otherwise a call expr.
			if i == 0 || in[i-1].Id.IsOperator() {
				out = append(out, t)
				vl++
				break
			}
			// The call expression can be a function call, a conversion,
			// a type assersion (including for type switch)
			// func call: push args and func address then call
			out = append(out, t)
			vl++
			if t2 := in[i-1]; t2.Id == lang.Ident {
				if s, sc, ok := p.getSym(t2.Str, p.scope); ok {
					log.Println("callExpr:", t2.Str, p.scope, s, ok, sc)
					if s.kind == symValue {
						// Store the number of input parameters in the token Beg field.
						ops = append(ops, scanner.Token{Str: "callX", Id: lang.CallX, Pos: t.Pos, Beg: p.numItems(t.Block(), lang.Comma)})
						break
					}
				}
			}
			ops = append(ops, scanner.Token{Str: "call", Id: lang.Call, Pos: t.Pos})
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

	log.Println("ParseExpr out:", out, "vl:", vl, "ops:", ops)
	// A logical operator (&&, ||) involves additional control flow operations.
	if out, err = p.ParseLogical(out); err != nil {
		return out, err
	}
	if l := len(out) - 1; l >= 0 && (out[l].Id == lang.Define || out[l].Id == lang.Assign) {
		// Handle the assignment of a logical expression.
		s1 := p.subExprLen(out[:l])
		head, err := p.ParseLogical(out[:l-s1])
		if err != nil {
			return out, err
		}
		out = append(head, out[l-s1:]...)
	}

	// The tokens are now properly ordered, process nested blocks.
	for i := len(out) - 1; i >= 0; i-- {
		t := out[i]
		var toks Tokens
		switch t.Id {
		case lang.ParenBlock, lang.BracketBlock:
			if toks, err = p.ParseExprStr(t.Block()); err != nil {
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

func (p *Parser) ParseExprStr(s string) (tokens Tokens, err error) {
	if tokens, err = p.Scan(s, false); err != nil {
		return
	}
	var result Tokens
	for _, sub := range tokens.Split(lang.Comma) {
		toks, err := p.ParseExpr(sub)
		if err != nil {
			return result, err
		}
		result = append(toks, result...)
	}
	return result, err
}

// ParseLogical handles logical expressions with control flow (&& and ||) by
// ensuring the left hand side is evaluated unconditionally first, then the
// right hand side can be skipped or not by inserting a conditional jump and label.
// If the last token is not a logical operator then the function is idempotent.
func (p *Parser) ParseLogical(in Tokens) (out Tokens, err error) {
	l := len(in) - 1
	if l < 0 || !in[l].Id.IsLogicalOp() {
		return in, nil
	}
	xp := strconv.Itoa(p.labelCount[p.scope])
	p.labelCount[p.scope]++
	rhsIndex := p.subExprLen(in[:l])
	lhs, err := p.ParseLogical(in[l-rhsIndex : l])
	if err != nil {
		return out, err
	}
	rhs, err := p.ParseLogical(in[:l-rhsIndex])
	if err != nil {
		return out, err
	}
	out = append(out, lhs...)
	if in[l].Id == lang.Lor {
		out = append(out, scanner.Token{Id: lang.JumpSetTrue, Str: "JumpSetTrue " + p.scope + "x" + xp})
	} else {
		out = append(out, scanner.Token{Id: lang.JumpSetFalse, Str: "JumpSetFalse " + p.scope + "x" + xp})
	}
	out = append(out, rhs...)
	out = append(out, scanner.Token{Id: lang.Label, Str: p.scope + "x" + xp})
	return out, err
}

// subExprLen returns the length of the first complete sub-expression starting from the input end.
func (p *Parser) subExprLen(in Tokens) int {
	l := len(in) - 1
	last := in[l]
	switch last.Id {
	case lang.Int, lang.Float, lang.String, lang.Char, lang.Ident, lang.ParenBlock, lang.BracketBlock:
		return 1
	case lang.Call:
		s1 := p.subExprLen(in[:l])
		return 1 + s1 + p.subExprLen(in[:l-s1])
		// TODO: add selector and index operators when ready
	}
	if last.Id.IsBinaryOp() {
		s1 := p.subExprLen(in[:l])
		return 1 + s1 + p.subExprLen(in[:l-s1])
	}
	if last.Id.IsUnaryOp() {
		return 1 + p.subExprLen(in[:l])
	}
	return 0 // should not occur. TODO: diplay some error here.
}
