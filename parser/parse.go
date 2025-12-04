// Package parser implements a parser and compiler.
package parser

import (
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/mvertes/parscan/lang"
	"github.com/mvertes/parscan/scanner"
	"github.com/mvertes/parscan/symbol"
)

// Parser represents the state of a parser.
type Parser struct {
	*scanner.Scanner

	Symbols  symbol.SymMap
	function *symbol.Symbol
	scope    string
	fname    string
	pkgName  string // current package name
	noPkg    bool   // true if package statement is not mandatory (test, repl).

	funcScope     string
	framelen      map[string]int // length of function frames indexed by funcScope
	labelCount    map[string]int
	breakLabel    string
	continueLabel string
	clonum        int // closure instance number
}

// Parser errors.
var (
	ErrBody     = errors.New("missign body")
	ErrBreak    = errors.New("invalid break statement")
	ErrContinue = errors.New("invalid continue statement")
	ErrFor      = errors.New("invalid for statement")
	ErrGoto     = errors.New("invalid goto statement")
)

// NewParser returns a new parser.
func NewParser(spec *lang.Spec, noPkg bool) *Parser {
	p := &Parser{
		Scanner:    scanner.NewScanner(spec),
		Symbols:    symbol.SymMap{},
		noPkg:      noPkg,
		framelen:   map[string]int{},
		labelCount: map[string]int{},
	}
	p.Symbols.Init()
	return p
}

// Scan performs lexical analysis on s and returns Tokens or an error.
func (p *Parser) Scan(s string, endSemi bool) (Tokens, error) {
	return p.Scanner.Scan(s, endSemi)
}

// Parse performs syntax analysis on s and return Tokens or an error.
func (p *Parser) Parse(src string) (out Tokens, err error) {
	in, err := p.Scan(src, true)
	if err != nil {
		return out, err
	}
	log.Printf("Parse src: %#v\n", src)
	return p.parseStmts(in)
}

func (p *Parser) parseStmts(in Tokens) (out Tokens, err error) {
	for len(in) > 0 {
		endstmt := in.Index(lang.Semicolon)
		if endstmt == -1 {
			return out, scanner.ErrBlock
		}
		// Skip over simple init statements for some tokens (if, for, ...)
		if lang.HasInit[in[0].Tok] {
			for in[endstmt-1].Tok != lang.BraceBlock {
				e2 := in[endstmt+1:].Index(lang.Semicolon)
				if e2 == -1 {
					return out, scanner.ErrBlock
				}
				endstmt += 1 + e2
			}
		}
		o, err := p.parseStmt(in[:endstmt])
		if err != nil {
			return out, err
		}
		out = append(out, o...)
		in = in[endstmt+1:]
	}
	return out, err
}

func (p *Parser) parseStmt(in Tokens) (out Tokens, err error) {
	if len(in) == 0 {
		return nil, nil
	}
	log.Println("parseStmt in:", in)
	// Preliminary: make sure that a pkgName is defined (or about to be).
	if in[0].Tok != lang.Package && p.pkgName == "" {
		if !p.noPkg {
			return out, errors.New("no package defined")
		}
		p.pkgName = "main"
	}

	switch t := in[0]; t.Tok {
	case lang.Break:
		return p.parseBreak(in)
	case lang.Continue:
		return p.parseContinue(in)
	case lang.Const:
		return p.parseConst(in)
	case lang.For:
		return p.parseFor(in)
	case lang.Func:
		return p.parseFunc(in)
	case lang.Defer, lang.Go, lang.Fallthrough, lang.Select:
		return out, fmt.Errorf("not yet implemented: %v", t.Tok)
	case lang.Goto:
		return p.parseGoto(in)
	case lang.If:
		return p.parseIf(in)
	case lang.Import:
		return p.parseImports(in)
	case lang.Package:
		return p.parsePackage(in)
	case lang.Return:
		return p.parseReturn(in)
	case lang.Switch:
		return p.parseSwitch(in)
	case lang.Type:
		return p.parseType(in)
	case lang.Var:
		return p.parseVar(in)
	case lang.Ident:
		if len(in) == 2 && in[1].Tok == lang.Colon {
			return p.parseLabel(in)
		}
		fallthrough
	default:
		return p.parseExpr(in)
	}
}

func (p *Parser) parseBreak(in Tokens) (out Tokens, err error) {
	var label string
	switch len(in) {
	case 1:
		label = p.breakLabel
	case 2:
		if in[1].Tok != lang.Ident {
			return nil, ErrBreak
		}
		// TODO: check validity of user provided label
		label = in[1].Str
	default:
		return nil, ErrBreak
	}
	out = Tokens{{Tok: lang.Goto, Str: label}}
	return out, err
}

func (p *Parser) parseContinue(in Tokens) (out Tokens, err error) {
	var label string
	switch len(in) {
	case 1:
		label = p.continueLabel
	case 2:
		if in[1].Tok != lang.Ident {
			return nil, ErrContinue
		}
		// TODO: check validity of user provided label
		label = in[1].Str
	default:
		return nil, ErrContinue
	}
	out = Tokens{{Tok: lang.Goto, Str: label}}
	return out, err
}

func (p *Parser) parseGoto(in Tokens) (out Tokens, err error) {
	if len(in) != 2 || in[1].Tok != lang.Ident {
		return nil, ErrGoto
	}
	// TODO: check validity of user provided label
	return Tokens{{Tok: lang.Goto, Str: p.funcScope + "/" + in[1].Str}}, nil
}

func (p *Parser) parseFor(in Tokens) (out Tokens, err error) {
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
		return nil, ErrFor
	}
	breakLabel, continueLabel := p.breakLabel, p.continueLabel
	p.pushScope("for" + fc)
	p.breakLabel, p.continueLabel = p.scope+"e", p.scope+"b"
	defer func() {
		p.breakLabel, p.continueLabel = breakLabel, continueLabel
		p.popScope()
	}()
	if len(init) > 0 {
		if init, err = p.parseStmt(init); err != nil {
			return nil, err
		}
		out = init
	}
	out = append(out, scanner.Token{Tok: lang.Label, Str: p.scope + "b"})
	if len(cond) > 0 {
		if cond, err = p.parseExpr(cond); err != nil {
			return nil, err
		}
		out = append(out, cond...)
		out = append(out, scanner.Token{Tok: lang.JumpFalse, Str: p.scope + "e"})
	}
	if body, err = p.Parse(in[len(in)-1].Block()); err != nil {
		return nil, err
	}
	out = append(out, body...)
	if len(post) > 0 {
		if post, err = p.parseStmt(post); err != nil {
			return nil, err
		}
		out = append(out, post...)
	}
	out = append(out,
		scanner.Token{Tok: lang.Goto, Str: p.scope + "b"},
		scanner.Token{Tok: lang.Label, Str: p.scope + "e"})
	return out, err
}

func (p *Parser) parseFunc(in Tokens) (out Tokens, err error) {
	// TODO: handle anonymous functions (no function name)
	// TODO: handle receiver (methods)
	// TODO: handle parametric types (generics)
	// TODO: handle variadic parameters
	var fname string
	if in[1].Tok == lang.Ident {
		fname = in[1].Str
	} else {
		fname = "#f" + strconv.Itoa(p.clonum)
		p.clonum++
	}
	ofname := p.fname
	p.fname = fname
	ofunc := p.function
	funcScope := p.funcScope
	s, _, ok := p.Symbols.Get(fname, p.scope)
	if !ok {
		s = &symbol.Symbol{Used: true}
		p.Symbols[p.scope+fname] = s
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
		{Tok: lang.Goto, Str: fname + "_end"}, // Skip function definition.
		{Tok: lang.Label, Pos: in[0].Pos, Str: fname},
	}

	bi := in.Index(lang.BraceBlock)
	if bi < 0 {
		return out, ErrBody
	}
	typ, err := p.parseTypeExpr(in[:bi])
	if err != nil {
		return out, err
	}
	s.Kind = symbol.Func
	s.Type = typ
	p.function = s

	toks, err := p.Parse(in[len(in)-1].Block())
	if err != nil {
		return out, err
	}
	if l := p.framelen[p.funcScope] - 1; l > 0 {
		out = append(out, scanner.Token{Tok: lang.Grow, Beg: l})
	}
	out = append(out, toks...)
	if out[len(out)-1].Tok != lang.Return {
		// Ensure that a return statement is always added at end of function.
		// TODO: detect missing or wrong returns.
		x, err := p.parseReturn(nil)
		if err != nil {
			return out, err
		}
		out = append(out, x...)
	}
	out = append(out, scanner.Token{Tok: lang.Label, Str: fname + "_end"})
	return out, err
}

func (p *Parser) parseIf(in Tokens) (out Tokens, err error) {
	label := "if" + strconv.Itoa(p.labelCount[p.scope])
	p.labelCount[p.scope]++
	p.pushScope(label)
	defer p.popScope()
	// We start from the end of the statement and examine tokens backward to
	// get the destination labels already computed when jumps are set.
	for sc, i := 0, len(in)-1; i > 0; sc++ {
		ssc := strconv.Itoa(sc)
		if in[i].Tok != lang.BraceBlock {
			return nil, fmt.Errorf("expected '{', got %v", in[i])
		}
		pre, err := p.Parse(in[i].Block())
		if err != nil {
			return nil, err
		}
		if sc > 0 {
			pre = append(pre, scanner.Token{Tok: lang.Goto, Str: p.scope + "e0"})
		}
		pre = append(pre, scanner.Token{Tok: lang.Label, Str: p.scope + "e" + ssc})
		out = append(pre, out...)
		i--

		if in[i].Tok == lang.Else { // Step over final 'else'.
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
			if init, err = p.parseStmt(init); err != nil {
				return nil, err
			}
			pre = append(pre, init...)
		}
		if cond, err = p.parseExpr(cond); err != nil {
			return nil, err
		}
		pre = append(pre, cond...)
		pre = append(pre, scanner.Token{Tok: lang.JumpFalse, Str: p.scope + "e" + ssc})
		out = append(pre, out...)
		i = ifp
		if i > 1 && in[i].Tok == lang.If && in[i-1].Tok == lang.Else { // Step over 'else if'.
			i -= 2
		}
	}
	return out, err
}

func (p *Parser) parseSwitch(in Tokens) (out Tokens, err error) {
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
		if init, err = p.parseStmt(init); err != nil {
			return nil, err
		}
		out = init
	}
	condSwitch := false
	if len(cond) > 0 {
		if cond, err = p.parseExpr(cond); err != nil {
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
		if cl[1].Tok == lang.Colon && i != lsc {
			sc[i], sc[lsc] = sc[lsc], sc[i]
			break
		}
	}
	// Process each clause.
	nc := len(sc) - 1
	for i, cl := range sc {
		co, err := p.parseCaseClause(cl, i, nc, condSwitch)
		if err != nil {
			return nil, err
		}
		out = append(out, co...)
	}
	out = append(out, scanner.Token{Tok: lang.Label, Str: p.breakLabel})
	return out, err
}

func (p *Parser) parseCaseClause(in Tokens, index, maximum int, condSwitch bool) (out Tokens, err error) {
	in = append(in, scanner.Token{Tok: lang.Semicolon}) // Force a ';' at the end of body clause.
	var conds, body Tokens
	tl := in.Split(lang.Colon)
	if len(tl) != 2 {
		return nil, errors.New("invalid case clause")
	}
	conds = tl[0][1:]
	if body, err = p.parseStmts(tl[1]); err != nil {
		return out, err
	}
	lcond := conds.Split(lang.Comma)
	for i, cond := range lcond {
		if cond, err = p.parseExpr(cond); err != nil {
			return out, err
		}
		txt := fmt.Sprintf("%sc%d.%d", p.scope, index, i)
		next := ""
		if i == len(lcond)-1 { // End of cond: next, go to next clause or exit
			if index < maximum {
				next = fmt.Sprintf("%sc%d.%d", p.scope, index+1, 0)
			} else {
				next = p.scope + "e"
			}
		} else {
			next = fmt.Sprintf("%sc%d.%d", p.scope, index, i+1)
		}
		out = append(out, scanner.Token{Tok: lang.Label, Str: txt})
		if len(cond) > 0 {
			out = append(out, cond...)
			if condSwitch {
				out = append(out, scanner.Token{Tok: lang.EqualSet})
			}
			out = append(out, scanner.Token{Tok: lang.JumpFalse, Str: next})
		}
		out = append(out, body...)
		if i != len(lcond)-1 || index != maximum {
			out = append(out, scanner.Token{Tok: lang.Goto, Str: p.scope + "e"})
		}
	}
	return out, err
}

func (p *Parser) parseLabel(in Tokens) (out Tokens, err error) {
	return Tokens{{Tok: lang.Label, Str: p.funcScope + "/" + in[0].Str}}, nil
}

func (p *Parser) parseReturn(in Tokens) (out Tokens, err error) {
	if l := len(in); l > 1 {
		if out, err = p.parseExpr(in[1:]); err != nil {
			return out, err
		}
	} else if l == 0 {
		in = Tokens{{Tok: lang.Return}} // Implicit return in functions with no return parameters.
	}

	// TODO: the function symbol should be already present in the parser context.
	// otherwise no way to handle anonymous func.
	s := p.function
	in[0].Beg = s.Type.Rtype.NumOut()
	in[0].End = s.Type.Rtype.NumIn()
	out = append(out, in[0])
	return out, err
}

func (p *Parser) numItems(s string, sep lang.Token) int {
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
