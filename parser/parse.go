package parser

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/gnolang/parscan/lang"
	"github.com/gnolang/parscan/scanner"
)

type Parser struct {
	*scanner.Scanner

	symbols  map[string]*symbol
	function *symbol
	scope    string
	fname    string

	labelCount map[string]int
	breakLabel string
}

func (p *Parser) Scan(s string, endSemi bool) (Tokens, error) {
	return p.Scanner.Scan(s, endSemi)
}

type Tokens []scanner.Token

func (toks Tokens) String() (s string) {
	for _, t := range toks {
		s += fmt.Sprintf("%#v ", t.Str)
	}
	return s
}

func (toks Tokens) Index(id lang.TokenId) int {
	for i, t := range toks {
		if t.Id == id {
			return i
		}
	}
	return -1
}

func (toks Tokens) LastIndex(id lang.TokenId) int {
	for i := len(toks) - 1; i >= 0; i-- {
		if toks[i].Id == id {
			return i
		}
	}
	return -1
}

func (toks Tokens) Split(id lang.TokenId) (result []Tokens) {
	for {
		i := toks.Index(id)
		if i < 0 {
			return append(result, toks)
		}
		result = append(result, toks[:i])
		toks = toks[i+1:]
	}
}

func (p *Parser) Parse(src string) (out Tokens, err error) {
	log.Printf("Parse src: %#v\n", src)
	in, err := p.Scan(src, true)
	if err != nil {
		return out, err
	}
	log.Println("Parse in:", in)
	for len(in) > 0 {
		endstmt := in.Index(lang.Semicolon)
		if endstmt == -1 {
			return out, scanner.ErrBlock
		}

		// Skip over simple init statements for some tokens (if, for, ...)
		if lang.HasInit[in[0].Id] {
			for in[endstmt-1].Id != lang.BraceBlock {
				e2 := in[endstmt+1:].Index(lang.Semicolon)
				if e2 == -1 {
					return out, scanner.ErrBlock
				}
				endstmt += 1 + e2
			}
		}
		o, err := p.ParseStmt(in[:endstmt])
		if err != nil {
			return out, err
		}
		out = append(out, o...)
		in = in[endstmt+1:]
	}
	return out, err
}

func (p *Parser) ParseStmt(in Tokens) (out Tokens, err error) {
	log.Println("ParseStmt in:", in)
	if len(in) == 0 {
		return nil, nil
	}
	switch t := in[0]; t.Id {
	case lang.Break:
		return p.ParseBreak(in)
	case lang.For:
		return p.ParseFor(in)
	case lang.Func:
		return p.ParseFunc(in)
	case lang.If:
		return p.ParseIf(in)
	case lang.Return:
		return p.ParseReturn(in)
	default:
		return p.ParseExpr(in)
	}
}

func (p *Parser) ParseBreak(in Tokens) (out Tokens, err error) {
	var label string
	switch len(in) {
	case 1:
		label = p.breakLabel
	case 2:
		if in[1].Id != lang.Ident {
			return nil, fmt.Errorf("invalid break statement")
		}
		label = in[1].Str
	default:
		return nil, fmt.Errorf("invalid break statement")
	}
	out = Tokens{{Id: lang.Goto, Str: "goto " + label}}
	return out, err
}

func (p *Parser) ParseFor(in Tokens) (out Tokens, err error) {
	// TODO: detect invalid code.
	fc := strconv.Itoa(p.labelCount[p.scope])
	p.labelCount[p.scope]++
	var init, cond, post, body Tokens
	pre := in[1 : len(in)-1].Split(lang.Semicolon)
	switch len(pre) {
	case 1:
		cond = pre[0]
	case 3:
		init, cond, post = pre[0], pre[1], pre[2]
	default:
		return nil, fmt.Errorf("invalild for statement")
	}
	p.pushScope("for" + fc)
	breakLabel := p.breakLabel
	p.breakLabel = p.scope + "e"
	defer func() {
		p.breakLabel = breakLabel
		p.popScope()
	}()
	if len(init) > 0 {
		if init, err = p.ParseStmt(init); err != nil {
			return nil, err
		}
		out = init
	}
	out = append(out, scanner.Token{Id: lang.Label, Str: p.scope + "b"})
	if len(cond) > 0 {
		if cond, err = p.ParseExpr(cond); err != nil {
			return nil, err
		}
		out = append(out, cond...)
		out = append(out, scanner.Token{Id: lang.JumpFalse, Str: "JumpFalse " + p.scope + "e"})
	}
	if body, err = p.Parse(in[len(in)-1].Block()); err != nil {
		return nil, err
	}
	out = append(out, body...)
	if len(post) > 0 {
		if post, err = p.ParseStmt(post); err != nil {
			return nil, err
		}
		out = append(out, post...)
	}
	out = append(out,
		scanner.Token{Id: lang.Goto, Str: "goto " + p.scope + "b"},
		scanner.Token{Id: lang.Label, Str: p.scope + "e"})
	return out, err
}

func (p *Parser) ParseFunc(in Tokens) (out Tokens, err error) {
	// TODO: handle anonymous functions (no function name)
	// TODO: handle receiver (methods)
	// TODO: handle parametric types (generics)
	// TODO: handle variadic parameters
	fname := in[1].Str
	ofname := p.fname
	p.fname = fname
	ofunc := p.function
	s, _, ok := p.getSym(fname, p.scope)
	if !ok {
		s = &symbol{}
		p.symbols[p.scope+fname] = s
	}
	p.pushScope(fname)
	defer func() {
		p.fname = ofname // TODO remove if favor of function.
		p.function = ofunc
		p.popScope()
	}()

	out = Tokens{
		{Id: lang.Enter, Str: "enter " + p.scope},
		{Id: lang.Goto, Str: "goto " + fname + "_end"}, // Skip function definition.
		{Id: lang.Label, Pos: in[0].Pos, Str: fname},
	}

	bi := in.Index(lang.BraceBlock)
	if bi < 0 {
		return out, fmt.Errorf("no function body")
	}
	typ, err := p.ParseType(in[:bi])
	if err != nil {
		return out, err
	}
	s.kind = symFunc
	s.Type = typ
	p.function = s

	log.Println("body:", in[len(in)-1].Block())
	toks, err := p.Parse(in[len(in)-1].Block())
	if err != nil {
		return out, err
	}
	out = append(out, toks...)
	out = append(out,
		scanner.Token{Id: lang.Label, Str: fname + "_end"},
		scanner.Token{Id: lang.Exit},
	)
	log.Println("symbols", p.symbols)
	return out, err
}

func (p *Parser) ParseIf(in Tokens) (out Tokens, err error) {
	label := "if" + strconv.Itoa(p.labelCount[p.scope])
	p.labelCount[p.scope]++
	p.pushScope(label)
	defer p.popScope()
	// We start from the end of the statement and examine tokens backward to
	// get the destination labels already computed when jumps are set.
	for sc, i := 0, len(in)-1; i > 0; sc++ {
		ssc := strconv.Itoa(sc)
		if in[i].Id != lang.BraceBlock {
			return nil, fmt.Errorf("expected '{', got %v", in[i])
		}
		pre, err := p.Parse(in[i].Block())
		if err != nil {
			return nil, err
		}
		if sc > 0 {
			pre = append(pre, scanner.Token{Id: lang.Goto, Str: "goto " + p.scope + "e0"})
		}
		pre = append(pre, scanner.Token{Id: lang.Label, Str: p.scope + "e" + ssc})
		out = append(pre, out...)
		i--

		if in[i].Id == lang.Else { // Step over final 'else'.
			i--
			continue
		}
		pre = Tokens{}
		var init, cond Tokens
		ifp := in[:i].LastIndex(lang.If)
		initcond := in[ifp+1 : i+1]
		if ii := initcond.Index(lang.Semicolon); ii < 0 {
			cond = initcond
		} else {
			init = initcond[:ii]
			cond = initcond[ii+1:]
		}
		if len(init) > 0 {
			if init, err = p.ParseStmt(init); err != nil {
				return nil, err
			}
			pre = append(pre, init...)
		}
		if cond, err = p.ParseExpr(cond); err != nil {
			return nil, err
		}
		pre = append(pre, cond...)
		pre = append(pre, scanner.Token{Id: lang.JumpFalse, Str: "JumpFalse " + p.scope + "e" + ssc})
		out = append(pre, out...)
		i = ifp
		if i > 1 && in[i].Id == lang.If && in[i-1].Id == lang.Else { // Step over 'else if'.
			i -= 2
		}
	}
	return out, err
}

func (p *Parser) ParseReturn(in Tokens) (out Tokens, err error) {
	if len(in) > 1 {
		if out, err = p.ParseExpr(in[1:]); err != nil {
			return out, err
		}
	}

	// TODO: the function symbol should be already present in the parser context.
	// otherwise no way to handle anonymous func.
	s := p.function
	in[0].Beg = s.Type.NumOut()
	in[0].End = s.Type.NumIn()
	log.Println("ParseReturn:", p.fname, in[0])
	out = append(out, in[0])
	return out, err
}

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
		case lang.Ident, lang.Int, lang.String:
			out = append(out, t)
			vl++
		case lang.Define, lang.Add, lang.Sub, lang.Assign, lang.Equal, lang.Less:
			// TODO: handle operator precedence to swap operators / operands if necessary
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
		if ol := len(ops); ol > 0 && vl > ol {
			op := ops[ol-1]
			ops = ops[:ol-1]
			out = append(out, op)
			vl--
		}
	}
	out = append(out, ops...) // TODO: verify that ops should be added in this order.

	log.Println("ParseExpr out:", out, "vl:", vl, "ops:", ops)
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

func (p *Parser) numItems(s string, sep lang.TokenId) int {
	tokens, err := p.Scan(s, false)
	if err != nil {
		return -1
	}
	r := 0
	for _, t := range tokens.Split(sep) {
		if len(t) == 0 {
			continue
		}
		r++
	}
	return r
}

func (p *Parser) pushScope(name string) {
	if p.scope != "" {
		p.scope += "/"
	}
	p.scope += name
}

func (p *Parser) popScope() {
	j := strings.LastIndex(p.scope, "/")
	if j == -1 {
		j = 0
	}
	p.scope = p.scope[:j]
}
