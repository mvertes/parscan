package parser

import (
	"errors"
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

	funcScope     string
	framelen      map[string]int // length of function frames indexed by funcScope
	labelCount    map[string]int
	breakLabel    string
	continueLabel string
}

func (p *Parser) Scan(s string, endSemi bool) (Tokens, error) {
	return p.Scanner.Scan(s, endSemi)
}

func (p *Parser) Parse(src string) (out Tokens, err error) {
	log.Printf("Parse src: %#v\n", src)
	in, err := p.Scan(src, true)
	if err != nil {
		return out, err
	}
	return p.ParseStmts(in)
}

func (p *Parser) ParseStmts(in Tokens) (out Tokens, err error) {
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
	log.Println("ParseStmt in:", in, len(in))
	if len(in) == 0 {
		return nil, nil
	}
	switch t := in[0]; t.Id {
	case lang.Break:
		return p.ParseBreak(in)
	case lang.Continue:
		return p.ParseContinue(in)
	case lang.Const:
		return p.ParseConst(in)
	case lang.For:
		return p.ParseFor(in)
	case lang.Func:
		return p.ParseFunc(in)
	case lang.Goto:
		return p.ParseGoto(in)
	case lang.If:
		return p.ParseIf(in)
	case lang.Return:
		return p.ParseReturn(in)
	case lang.Switch:
		return p.ParseSwitch(in)
	case lang.Type:
		return p.ParseType(in)
	case lang.Var:
		return p.ParseVar(in)
	case lang.Ident:
		if len(in) == 2 && in[1].Id == lang.Colon {
			return p.ParseLabel(in)
		}
		fallthrough
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
		// TODO: check validity of user provided label
		label = in[1].Str
	default:
		return nil, fmt.Errorf("invalid break statement")
	}
	out = Tokens{{Id: lang.Goto, Str: "goto " + label}}
	return out, err
}

func (p *Parser) ParseContinue(in Tokens) (out Tokens, err error) {
	var label string
	switch len(in) {
	case 1:
		label = p.continueLabel
	case 2:
		if in[1].Id != lang.Ident {
			return nil, fmt.Errorf("invalid continue statement")
		}
		// TODO: check validity of user provided label
		label = in[1].Str
	default:
		return nil, fmt.Errorf("invalid continue statement")
	}
	out = Tokens{{Id: lang.Goto, Str: "goto " + label}}
	return out, err
}

func (p *Parser) ParseGoto(in Tokens) (out Tokens, err error) {
	if len(in) != 2 || in[1].Id != lang.Ident {
		return nil, fmt.Errorf("invalid goto statement")
	}
	// TODO: check validity of user provided label
	return Tokens{{Id: lang.Goto, Str: "goto " + p.funcScope + "/" + in[1].Str}}, nil
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
	breakLabel, continueLabel := p.breakLabel, p.continueLabel
	p.pushScope("for" + fc)
	p.breakLabel, p.continueLabel = p.scope+"e", p.scope+"b"
	defer func() {
		p.breakLabel, p.continueLabel = breakLabel, continueLabel
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
	funcScope := p.funcScope
	s, _, ok := p.getSym(fname, p.scope)
	if !ok {
		s = &symbol{used: true}
		p.symbols[p.scope+fname] = s
	}
	p.pushScope(fname)
	p.funcScope = p.scope
	defer func() {
		p.fname = ofname // TODO remove if favor of function.
		p.function = ofunc
		p.funcScope = funcScope
		p.popScope()
	}()

	out = Tokens{
		{Id: lang.Goto, Str: "goto " + fname + "_end"}, // Skip function definition.
		{Id: lang.Label, Pos: in[0].Pos, Str: fname}}

	bi := in.Index(lang.BraceBlock)
	if bi < 0 {
		return out, fmt.Errorf("no function body")
	}
	typ, err := p.ParseTypeExpr(in[:bi])
	if err != nil {
		return out, err
	}
	s.kind = symFunc
	s.Type = typ
	p.function = s

	toks, err := p.Parse(in[len(in)-1].Block())
	if err != nil {
		return out, err
	}
	if l := p.framelen[p.funcScope] - 1; l > 0 {
		out = append(out, scanner.Token{Id: lang.Grow, Beg: l})
	}
	out = append(out, toks...)
	out = append(out, scanner.Token{Id: lang.Label, Str: fname + "_end"})
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

func (p *Parser) ParseSwitch(in Tokens) (out Tokens, err error) {
	var init, cond, clauses Tokens
	initcond := in[1 : len(in)-1]
	if ii := initcond.Index(lang.Semicolon); ii < 0 {
		cond = initcond
	} else {
		init = initcond[:ii]
		cond = initcond[ii+1:]
	}
	label := "switch" + strconv.Itoa(p.labelCount[p.scope])
	p.labelCount[p.scope]++
	breakLabel := p.breakLabel
	p.pushScope(label)
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
	condSwitch := false
	if len(cond) > 0 {
		if cond, err = p.ParseExpr(cond); err != nil {
			return nil, err
		}
		out = append(out, cond...)
		condSwitch = true
	}
	// Split switch body into  case clauses.
	clauses, err = p.Scan(in[len(in)-1].Block(), true)
	sc := clauses.SplitStart(lang.Case)
	// Make sure that the default clause is the last.
	lsc := len(sc) - 1
	for i, cl := range sc {
		if cl[1].Id == lang.Colon && i != lsc {
			sc[i], sc[lsc] = sc[lsc], sc[i]
			break
		}
	}
	// Process each clause.
	nc := len(sc) - 1
	for i, cl := range sc {
		co, err := p.ParseCaseClause(cl, i, nc, condSwitch)
		if err != nil {
			return nil, err
		}
		out = append(out, co...)
	}
	out = append(out, scanner.Token{Id: lang.Label, Str: p.breakLabel})
	return out, err
}

func (p *Parser) ParseCaseClause(in Tokens, index, max int, condSwitch bool) (out Tokens, err error) {
	in = append(in, scanner.Token{Id: lang.Semicolon}) // Force a ';' at the end of body clause.
	var conds, body Tokens
	tl := in.Split(lang.Colon)
	if len(tl) != 2 {
		return nil, errors.New("invalid case clause")
	}
	conds = tl[0][1:]
	if body, err = p.ParseStmts(tl[1]); err != nil {
		return out, err
	}
	lcond := conds.Split(lang.Comma)
	for i, cond := range lcond {
		if cond, err = p.ParseExpr(cond); err != nil {
			return out, err
		}
		txt := fmt.Sprintf("%sc%d.%d", p.scope, index, i)
		next := ""
		if i == len(lcond)-1 { // End of cond: next, go to next clause or exit
			if index < max {
				next = fmt.Sprintf("%sc%d.%d", p.scope, index+1, 0)
			} else {
				next = p.scope + "e"
			}
		} else {
			next = fmt.Sprintf("%sc%d.%d", p.scope, index, i+1)
		}
		out = append(out, scanner.Token{Id: lang.Label, Str: txt})
		if len(cond) > 0 {
			out = append(out, cond...)
			if condSwitch {
				out = append(out, scanner.Token{Id: lang.EqualSet})
			}
			out = append(out, scanner.Token{Id: lang.JumpFalse, Str: "JumpFalse " + next})
		}
		out = append(out, body...)
		if i != len(lcond)-1 || index != max {
			out = append(out, scanner.Token{Id: lang.Goto, Str: "Goto " + p.scope + "e"})
		}
	}
	return out, err
}

func (p *Parser) ParseLabel(in Tokens) (out Tokens, err error) {
	return Tokens{{Id: lang.Label, Str: p.funcScope + "/" + in[0].Str}}, nil
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
	out = append(out, in[0])
	return out, err
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

func (p *Parser) precedence(t scanner.Token) int {
	return p.TokenProps[t.Str].Precedence
}
