// Package goparser implements a structured parser for Go.
package goparser

import (
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/mvertes/parscan/lang"
	"github.com/mvertes/parscan/scan"
	"github.com/mvertes/parscan/symbol"
	"github.com/mvertes/parscan/vm"
)

// Parser represents the state of a parser.
type Parser struct {
	*scan.Scanner

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
	clonum        int      // closure instance number
	namedOut      []string // scoped names of named return vars for current function
	SymTracker    []string // accumulates newly-added symbol keys during a checkpoint window; nil = not tracking
}

// SymSet inserts sym at key in the symbol table, recording the key for potential rollback.
func (p *Parser) SymSet(key string, sym *symbol.Symbol) {
	if p.SymTracker != nil {
		p.SymTracker = append(p.SymTracker, key)
	}
	p.Symbols[key] = sym
}

// SymAdd adds a new named symbol, recording the key for potential rollback.
func (p *Parser) SymAdd(i int, name string, v vm.Value, k symbol.Kind, t *vm.Type) {
	name = strings.TrimPrefix(name, "/")
	if p.SymTracker != nil {
		p.SymTracker = append(p.SymTracker, name)
	}
	p.Symbols[name] = &symbol.Symbol{Kind: k, Name: name, Index: i, Value: v, Type: t}
}

// scopedName returns name qualified by the current scope (e.g. "main/foo").
func (p *Parser) scopedName(name string) string {
	return strings.TrimPrefix(p.scope+"/"+name, "/")
}

// addLocalVar registers a new local variable in the current function frame
// and returns its scoped name for token fixup.
func (p *Parser) addLocalVar(name string) string {
	scoped := p.scopedName(name)
	p.SymAdd(p.framelen[p.funcScope], scoped, vm.Value{}, symbol.LocalVar, nil)
	p.framelen[p.funcScope]++
	return scoped
}

// addGlobalVar registers a new global variable in the current scope
// and returns its scoped name for token fixup.
func (p *Parser) addGlobalVar(name string) string {
	scoped := p.scopedName(name)
	p.SymAdd(symbol.UnsetAddr, scoped, vm.Value{}, symbol.Var, nil)
	return scoped
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
		Scanner:    scan.NewScanner(spec),
		Symbols:    symbol.SymMap{},
		noPkg:      noPkg,
		framelen:   map[string]int{},
		labelCount: map[string]int{},
	}
	p.Symbols.Init()
	return p
}

// Scan performs lexical analysis on s and returns Tokens or an error.
func (p *Parser) Scan(s string, endSemi bool) (out Tokens, err error) {
	toks, err := p.Scanner.Scan(s, endSemi)
	if err != nil {
		return out, err
	}
	for _, t := range toks {
		out = append(out, Token{Token: t})
	}
	return out, err
}

// Parse performs syntax analysis on s and returns Tokens or an error.
func (p *Parser) Parse(src string) (out Tokens, err error) {
	in, err := p.Scan(src, true)
	if err != nil {
		return out, err
	}
	return p.parseStmts(in)
}

// stmtEnd returns the index of the semicolon ending the first statement in toks,
// accounting for HasInit tokens (if, for, switch) which contain internal semicolons.
func (p *Parser) stmtEnd(toks Tokens) (int, error) {
	end := toks.Index(lang.Semicolon)
	if end == -1 {
		return -1, scan.ErrBlock
	}
	if p.TokenProps[toks[0].Tok].HasInit {
		for toks[end-1].Tok != lang.BraceBlock {
			e2 := toks[end+1:].Index(lang.Semicolon)
			if e2 == -1 {
				return -1, scan.ErrBlock
			}
			end += 1 + e2
		}
	}
	return end, nil
}

func (p *Parser) parseStmts(in Tokens) (out Tokens, err error) {
	for len(in) > 0 {
		end, err := p.stmtEnd(in)
		if err != nil {
			return out, err
		}
		o, err := p.parseStmt(in[:end])
		if err != nil {
			return out, err
		}
		out = append(out, o...)
		in = in[end+1:]
	}
	return out, err
}

// ScanDecls scans src and returns its top-level statements as individual token slices,
// without parsing them. Used by the lazy fixpoint evaluation loop.
func (p *Parser) ScanDecls(src string) ([]Tokens, error) {
	toks, err := p.Scan(src, true)
	if err != nil {
		return nil, err
	}
	var decls []Tokens
	for len(toks) > 0 {
		end, err := p.stmtEnd(toks)
		if err != nil {
			return nil, err
		}
		decls = append(decls, toks[:end])
		toks = toks[end+1:]
	}
	return decls, nil
}

// ParseOneStmt parses a single pre-scanned statement token slice.
func (p *Parser) ParseOneStmt(toks Tokens) (Tokens, error) {
	return p.parseStmt(toks)
}

// RegisterFunc parses and registers a named function's signature.
func (p *Parser) RegisterFunc(toks Tokens) error {
	if len(toks) < 3 || toks[0].Tok != lang.Func || toks[1].Tok != lang.Ident {
		return nil
	}
	fname := toks[1].Str
	bi := toks.Index(lang.BraceBlock)
	if bi < 0 {
		return nil
	}
	s, _, ok := p.Symbols.Get(fname, p.scope)
	if ok && s.Type != nil {
		return nil
	}
	if !ok {
		s = &symbol.Symbol{Used: true, Index: symbol.UnsetAddr}
		key := p.scopedName(fname)
		p.SymSet(key, s)
	}
	typ, _, err := p.parseTypeExpr(toks[:bi])
	if err != nil {
		return err
	}
	s.Kind = symbol.Func
	s.Type = typ
	return nil
}

func (p *Parser) parseStmt(in Tokens) (out Tokens, err error) {
	if len(in) == 0 {
		return nil, nil
	}
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
	case lang.Fallthrough:
		return out, errors.New("fallthrough statement out of place")
	case lang.Defer:
		return p.parseDefer(in)
	case lang.Go, lang.Select:
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
	case lang.Mul, lang.ParenBlock:
		if i := in.Index(lang.Assign); i > 0 {
			return p.parseAssign(in, i)
		}
		if op, i := indexCompoundAssign(in); i > 0 {
			return p.parseCompoundAssign(in, i, op)
		}
		return p.parseExpr(in, "")
	case lang.Ident:
		if in.Index(lang.Colon) == 1 {
			return p.parseLabel(in)
		}
		if i := in.Index(lang.Assign); i > 0 {
			return p.parseAssign(in, i)
		}
		if i := in.Index(lang.Define); i > 0 {
			return p.parseAssign(in, i)
		}
		if op, i := indexCompoundAssign(in); i > 0 {
			return p.parseCompoundAssign(in, i, op)
		}
		if l := len(in); l >= 2 && (in[l-1].Tok == lang.Inc || in[l-1].Tok == lang.Dec) {
			return p.parseIncDec(in)
		}
		fallthrough
	default:
		return p.parseExpr(in, "")
	}
}

func (p *Parser) parseAssign(in Tokens, aindex int) (out Tokens, err error) {
	rhs := in[aindex+1:].Split(lang.Comma)
	lhs := in[:aindex].Split(lang.Comma)
	define := in[aindex].Tok == lang.Define
	if len(rhs) == 1 {
		// Track positions of LHS tokens for local fixup (one entry per lhs element).
		lhsPositions := make([]int, len(lhs))
		for j, e := range lhs {
			lhsPositions[j] = len(out)
			toks, err := p.parseExpr(e, "")
			if err != nil {
				return out, err
			}
			out = append(out, toks...)
		}
		toks, err := p.parseExpr(rhs[0], "")
		if err != nil {
			return out, err
		}
		switch out[len(out)-1].Tok {
		case lang.Index:
			// Map elements cannot be assigned directly, but only through IndexAssign.
			out = out[:len(out)-1]
			out = append(out, toks...)
			out = append(out, newToken(lang.IndexAssign, "", in[aindex].Pos, len(lhs)))
		case lang.Deref:
			// Pointer deref cannot be assigned directly, use DerefAssign.
			out = out[:len(out)-1]
			out = append(out, toks...)
			out = append(out, newToken(lang.DerefAssign, "", in[aindex].Pos, len(lhs)))
		default:
			// Mark TypeAssert as ok form when LHS has exactly 2 targets.
			if len(lhs) == 2 && len(toks) > 0 && toks[len(toks)-1].Tok == lang.TypeAssert {
				toks[len(toks)-1].Arg[0] = 1
			}
			out = append(out, toks...)
			if out[len(out)-1].Tok == lang.Range {
				// Pass the the number of values to set to range.
				out[len(out)-1].Arg = []any{len(lhs)}
			} else {
				out = append(out, newToken(in[aindex].Tok, "", in[aindex].Pos, len(lhs)))
			}
		}
		// Register define symbols after parsing both LHS and RHS so that
		// the RHS can still reference outer-scope variables being shadowed.
		if define {
			for i, e := range lhs {
				if len(e) != 1 || e[0].Tok != lang.Ident {
					continue
				}
				if p.funcScope != "" {
					out[lhsPositions[i]].Str = p.addLocalVar(e[0].Str)
				} else {
					out[lhsPositions[i]].Str = p.addGlobalVar(e[0].Str)
				}
			}
		}
		return out, err
	}
	// Multiple values in right hand side.
	// For plain-ident non-define assignments (e.g. a, b = b, a), use a batched approach:
	// emit all LHS first, then all RHS, then one Assign(n). This ensures all RHS values
	// are captured before any assignment takes effect, preserving swap semantics.
	if !define {
		allSimple := true
		for _, e := range lhs {
			if len(e) != 1 || e[0].Tok != lang.Ident {
				allSimple = false
				break
			}
		}
		if allSimple {
			for _, e := range lhs {
				toks, err := p.parseExpr(e, "")
				if err != nil {
					return out, err
				}
				out = append(out, toks...)
			}
			for _, e := range rhs {
				toks, err := p.parseExpr(e, "")
				if err != nil {
					return out, err
				}
				out = append(out, toks...)
			}
			out = append(out, newToken(lang.Assign, "", in[aindex].Pos, len(lhs)))
			return out, err
		}
	}
	for i, e := range rhs {
		lhsPos := len(out)
		toks, err := p.parseExpr(lhs[i], "")
		if err != nil {
			return out, err
		}
		out = append(out, toks...)
		toks, err = p.parseExpr(e, "")
		if err != nil {
			return out, err
		}
		switch out[len(out)-1].Tok {
		case lang.Index:
			// Map elements cannot be assigned directly, but only through IndexAssign.
			out = out[:len(out)-1]
			out = append(out, toks...)
			out = append(out, newToken(lang.IndexAssign, "", in[aindex].Pos, 1))
		case lang.Deref:
			// Pointer deref cannot be assigned directly, use DerefAssign.
			out = out[:len(out)-1]
			out = append(out, toks...)
			out = append(out, newToken(lang.DerefAssign, "", in[aindex].Pos, 1))
		default:
			out = append(out, toks...)
			out = append(out, newToken(in[aindex].Tok, "", in[aindex].Pos, 1))
		}
		if define {
			lt := lhs[i]
			if len(lt) == 1 && lt[0].Tok == lang.Ident {
				if p.funcScope != "" {
					out[lhsPos].Str = p.addLocalVar(lt[0].Str)
				} else {
					out[lhsPos].Str = p.addGlobalVar(lt[0].Str)
				}
			}
		}
	}
	return out, err
}

// compoundAssignOp maps compound assignment tokens to their binary operator.
var compoundAssignOp = map[lang.Token]lang.Token{
	lang.AddAssign:    lang.Add,
	lang.SubAssign:    lang.Sub,
	lang.MulAssign:    lang.Mul,
	lang.QuoAssign:    lang.Quo,
	lang.RemAssign:    lang.Rem,
	lang.AndAssign:    lang.And,
	lang.OrAssign:     lang.Or,
	lang.XorAssign:    lang.Xor,
	lang.ShlAssign:    lang.Shl,
	lang.ShrAssign:    lang.Shr,
	lang.AndNotAssign: lang.AndNot,
}

// indexCompoundAssign returns the binary operator and position of the first compound
// assignment token in the token list, or (0, -1) if none found.
func indexCompoundAssign(in Tokens) (lang.Token, int) {
	for i, t := range in {
		if op, ok := compoundAssignOp[t.Tok]; ok {
			return op, i
		}
	}
	return 0, -1
}

// parseCompoundAssign transforms "a op= expr" into the equivalent of "a = a op (expr)".
func (p *Parser) parseCompoundAssign(in Tokens, aindex int, op lang.Token) (Tokens, error) {
	lhs := in[:aindex]
	rhs := in[aindex+1:]
	pos := in[aindex].Pos
	// Build: lhs = lhs op (rhs)
	newIn := make(Tokens, 0, len(lhs)*2+len(rhs)+2)
	newIn = append(newIn, lhs...)
	newIn = append(newIn, newToken(lang.Assign, "", pos, 1))
	newIn = append(newIn, lhs...)
	newIn = append(newIn, newToken(op, "", pos))
	if len(rhs) > 1 {
		// Wrap rhs in parens to preserve precedence.
		newIn = append(newIn, newToken(lang.ParenBlock, tokensToBlock(rhs), rhs[0].Pos))
	} else {
		newIn = append(newIn, rhs...)
	}
	return p.parseAssign(newIn, len(lhs))
}

// parseIncDec transforms "a++" into the equivalent of "a = a + 1" (or "a - 1" for "--").
func (p *Parser) parseIncDec(in Tokens) (Tokens, error) {
	last := in[len(in)-1]
	lhs := in[:len(in)-1]
	op := lang.Add
	if last.Tok == lang.Dec {
		op = lang.Sub
	}
	pos := last.Pos
	newIn := make(Tokens, 0, len(lhs)*2+3)
	newIn = append(newIn, lhs...)
	newIn = append(newIn, newToken(lang.Assign, "", pos, 1))
	newIn = append(newIn, lhs...)
	newIn = append(newIn, newToken(op, "", pos))
	newIn = append(newIn, newToken(lang.Int, "1", pos))
	return p.parseAssign(newIn, len(lhs))
}

// tokensToBlock serializes tokens back into a parenthesized expression string.
func tokensToBlock(toks Tokens) string {
	var sb strings.Builder
	for i, t := range toks {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(t.Str)
	}
	return sb.String()
}

func (p *Parser) parseDefer(in Tokens) (out Tokens, err error) {
	if len(in) < 2 {
		return nil, errors.New("invalid defer statement")
	}
	expr := in[1:]
	// The last token must be a ParenBlock containing the call arguments.
	// We split here rather than calling parseExpr on the whole expression
	// because the lang.Func case in parseExpr would consume a trailing '()'.
	last := len(expr) - 1
	if last < 0 || expr[last].Tok != lang.ParenBlock {
		return nil, errors.New("defer requires a function call")
	}
	callTok := expr[last]
	narg := p.numItems(callTok.Block(), lang.Comma)

	// Parse the function expression (tokens before the call paren).
	if out, err = p.parseExpr(expr[:last], ""); err != nil {
		return out, err
	}
	// Parse the argument list (reversed into LIFO push order, as in parseBlock).
	argToks, err := p.parseBlock(callTok, "")
	if err != nil {
		return out, err
	}
	out = append(out, argToks...)
	out = append(out, newDefer(callTok.Pos, narg))
	return out, nil
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
	out = append(out, newGoto(label, in[0].Pos))
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
	out = append(out, newGoto(label, in[0].Pos))
	return out, err
}

func (p *Parser) parseGoto(in Tokens) (out Tokens, err error) {
	if len(in) != 2 || in[1].Tok != lang.Ident {
		return nil, ErrGoto
	}
	// TODO: check validity of user provided label
	return Tokens{newGoto(p.funcScope+"/"+in[1].Str, in[0].Pos)}, nil
}

func (p *Parser) parseFor(in Tokens) (out Tokens, err error) {
	// TODO: detect invalid code.
	var init, cond, post, body, final Tokens
	hasRange := in.Index(lang.Range) >= 0
	fc := strconv.Itoa(p.labelCount[p.scope])
	p.labelCount[p.scope]++
	breakLabel, continueLabel := p.breakLabel, p.continueLabel
	p.pushScope("for" + fc)
	p.breakLabel, p.continueLabel = p.scope+"e", p.scope+"b"
	defer func() {
		p.breakLabel, p.continueLabel = breakLabel, continueLabel
		p.popScope()
	}()
	pre := in[1 : len(in)-1].Split(lang.Semicolon)
	// condLabel is the top of the loop (where Goto jumps back to).
	// For 3-clause for loops, continueLabel is set to the post-statement label
	// so that continue executes the post statement before re-checking the condition.
	condLabel := p.scope + "b"
	switch len(pre) {
	case 1:
		if hasRange {
			init = pre[0]
			if init, err = p.parseStmt(init); err != nil {
				return nil, err
			}
			out = init
			cond = Tokens{newNext(p.breakLabel, in[1].Pos, out[len(out)-1].Arg[0].(int))}
			final = Tokens{newStop(in[1].Pos)}
		} else {
			cond = pre[0]
		}
	case 3:
		init, cond, post = pre[0], pre[1], pre[2]
		if init, err = p.parseStmt(init); err != nil {
			return nil, err
		}
		out = init
		// continue must run the post statement before looping; use a separate label.
		p.continueLabel = p.scope + "p"
	default:
		return nil, ErrFor
	}
	out = append(out, newLabel(condLabel, in[0].Pos))
	if len(cond) > 0 {
		if cond, err = p.parseExpr(cond, ""); err != nil {
			return nil, err
		}
		out = append(out, cond...)
		if !hasRange {
			out = append(out, newJumpFalse(p.breakLabel, in[0].Pos))
		}
	}
	if body, err = p.Parse(in[len(in)-1].Block()); err != nil {
		return nil, err
	}
	out = append(out, body...)
	if len(post) > 0 {
		if post, err = p.parseStmt(post); err != nil {
			return nil, err
		}
		if p.continueLabel != condLabel {
			out = append(out, newLabel(p.continueLabel, in[0].Pos))
		}
		out = append(out, post...)
	}
	out = append(out,
		newGoto(condLabel, in[0].Pos),
		newLabel(p.breakLabel, in[0].Pos),
	)
	out = append(out, final...)
	return out, err
}

func (p *Parser) parseFunc(in Tokens) (out Tokens, err error) {
	// TODO: handle parametric types (generics)
	var fname string    // function name
	var recvName string // receiver variable name (non-empty for methods)

	switch in[1].Tok {
	case lang.Ident:
		fname = in[1].Str
	case lang.ParenBlock:
		// receiver, or anonymous function parameters.
		if t := in[2]; t.Tok == lang.Ident {
			if s, _, ok := p.Symbols.Get(t.Str, p.scope); ok && s.IsType() {
				fname = "#f" + strconv.Itoa(p.clonum) // Generated closure symbol name.
				p.clonum++
				break
			}
			// Parse receiver declaration: get type and variable name.
			if recvr, err := p.Scan(in[1].Block(), false); err != nil {
				return nil, err
			} else if rtyp, vars, _, err := p.parseParamTypes(recvr, parseTypeRecv); err == nil {
				// Extract the base type name from the receiver tokens (e.g. [t, *, T]).
				// reflect.Type.Name() is empty for dynamically created structs, so we
				// cannot rely on rtyp[0].Rtype.Elem().Name(); use the token stream instead.
				typeName := rtyp[0].String()
				isPtr := rtyp[0].IsPtr()
				for i := len(recvr) - 1; i >= 0; i-- {
					if recvr[i].Tok == lang.Ident {
						if isPtr {
							typeName = "*" + recvr[i].Str
						} else {
							typeName = recvr[i].Str
						}
						break
					}
				}
				fname = typeName + "." + in[2].Str // Method name prefixed by receiver type.
				if len(vars) > 0 && vars[0] != "" {
					recvName = path.Base(vars[0])
				}
			} else {
				return nil, err
			}
		}
		if fname == "" {
			// Anonymous function whose return type starts with a keyword (e.g. func() func() int {}).
			fname = "#f" + strconv.Itoa(p.clonum)
			p.clonum++
		}
	default:
		fname = "#f" + strconv.Itoa(p.clonum) // Generated closure symbol name.
		p.clonum++
	}

	ofname := p.fname
	p.fname = fname
	ofunc := p.function
	funcScope := p.funcScope
	onamedOut := p.namedOut
	p.namedOut = nil
	s, _, ok := p.Symbols.Get(fname, p.scope)
	if !ok {
		s = &symbol.Symbol{Name: fname, Used: true, Index: symbol.UnsetAddr}
		key := fname
		if !strings.HasPrefix(fname, "#") {
			key = p.scope + fname
		}
		p.SymSet(key, s)
	}
	p.pushScope(fname)
	p.funcScope = p.scope
	// Local variable indices start at 1; index 0 is the frame header (prevFP).
	p.framelen[p.funcScope] = 1
	// For methods, the receiver is Env[0] of the method closure.
	// Register it so the compiler emits HGet 0 for the receiver inside the body.
	if recvName != "" {
		recvScoped := p.scope + "/" + recvName
		s.FreeVars = []string{recvScoped}
		// Re-register receiver symbol at function scope so the compiler finds it
		// with the correct type when resolving the scoped identifier inside the body.
		// The outer-scoped key was set by addSymVar before pushScope.
		outerKey := strings.TrimPrefix(funcScope+"/"+recvName, "/")
		if outerSym := p.Symbols[outerKey]; outerSym != nil {
			p.SymSet(recvScoped, outerSym)
		}
	}
	defer func() {
		p.fname = ofname // TODO remove in favor of function.
		p.function = ofunc
		p.funcScope = funcScope
		p.namedOut = onamedOut
		p.popScope()
	}()

	out = Tokens{
		newGoto(fname+"_end", in[0].Pos), // Skip function definition.
		newLabel(fname, in[0].Pos),
	}

	bi := in.Index(lang.BraceBlock)
	if bi < 0 {
		return out, ErrBody
	}
	typ, _, err := p.parseTypeExpr(in[:bi])
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
		out = append(out, newGrow(l, in[0].Pos))
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
	out = append(out, newLabel(fname+"_end", in[0].Pos))
	return out, err
}

func (p *Parser) parseIf(in Tokens) (out Tokens, err error) {
	label := "if" + strconv.Itoa(p.labelCount[p.scope])
	p.labelCount[p.scope]++
	p.pushScope(label)
	defer p.popScope()
	endLabel := p.scope + "e0"
	elseCount := 1 // counter for intermediate else-branch labels: "e1", "e2", ...

	// Parse the if-else chain forward. Init is parsed before the body so that
	// variables defined in the init are visible inside the body block.
	i := 1 // skip initial 'if'
	for i < len(in) {
		clauseStart := i
		// Find the body block ({...}) for this clause.
		for i < len(in) && in[i].Tok != lang.BraceBlock {
			i++
		}
		if i >= len(in) {
			return nil, errors.New("expected '{', got end of input")
		}
		bodyIdx := i
		hasCondition := bodyIdx > clauseStart

		// Parse init and/or condition before the body.
		if hasCondition {
			initcond := in[clauseStart:bodyIdx]
			var init, cond Tokens
			if si := initcond.Index(lang.Semicolon); si >= 0 {
				init = initcond[:si]
				cond = initcond[si+1:]
			} else {
				cond = initcond
			}
			// Parse init first so that variables defined here are in scope for the body.
			if len(init) > 0 {
				if init, err = p.parseStmt(init); err != nil {
					return nil, err
				}
				out = append(out, init...)
			}
			if cond, err = p.parseExpr(cond, ""); err != nil {
				return nil, err
			}
			out = append(out, cond...)
		}

		i++ // move past body block
		hasMore := i < len(in) && in[i].Tok == lang.Else

		// Emit conditional jump to the next else clause or to the end.
		elseLabel := endLabel
		if hasMore {
			elseLabel = p.scope + "e" + strconv.Itoa(elseCount)
		}
		if hasCondition {
			out = append(out, newJumpFalse(elseLabel, in[bodyIdx].Pos))
		}

		// Parse body.
		body, err := p.Parse(in[bodyIdx].Block())
		if err != nil {
			return nil, err
		}
		out = append(out, body...)

		if hasMore {
			out = append(out, newGoto(endLabel, in[bodyIdx].Pos))
			out = append(out, newLabel(elseLabel, in[bodyIdx].Pos))
			elseCount++
			i++ // skip 'else'
			if i < len(in) && in[i].Tok == lang.If {
				i++ // skip 'if' in 'else if'
			}
		} else {
			break
		}
	}

	out = append(out, newLabel(endLabel, in[0].Pos))
	return out, err
}

func (p *Parser) parseSwitch(in Tokens) (out Tokens, err error) {
	var init, cond Tokens
	initcond := in[1 : len(in)-1]
	if ii := initcond.Index(lang.Semicolon); ii < 0 {
		cond = initcond
	} else {
		init = initcond[:ii]
		cond = initcond[ii+1:]
	}
	// Detect type switch: cond contains ".(type)".
	for i, t := range cond {
		if t.Tok == lang.Period && i+1 < len(cond) && cond[i+1].Tok == lang.ParenBlock {
			if strings.TrimSpace(cond[i+1].Block()) == "type" {
				return p.parseTypeSwitch(in, init, cond, i)
			}
		}
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
		if cond, err = p.parseExpr(cond, ""); err != nil {
			return nil, err
		}
		out = append(out, cond...)
		condSwitch = true
	}
	// Split switch body into case clauses.
	clauses, err := p.Scan(in[len(in)-1].Block(), true)
	if err != nil {
		return nil, err
	}
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
	prevFallthrough := false
	for i, cl := range sc {
		co, hasFallthrough, err := p.parseCaseClause(cl, i, nc, condSwitch, prevFallthrough)
		if err != nil {
			return nil, err
		}
		out = append(out, co...)
		prevFallthrough = hasFallthrough
	}
	out = append(out, newLabel(p.breakLabel, in[len(in)-1].Pos))
	return out, err
}

// parseTypeSwitch handles switch v := x.(type) { ... } statements.
// periodIdx is the index of ".(type)" in cond.
func (p *Parser) parseTypeSwitch(in, init, cond Tokens, periodIdx int) (out Tokens, err error) {
	pos := in[0].Pos
	guardToks := cond[:periodIdx]
	varName := ""
	if defPos := guardToks.Index(lang.Define); defPos >= 0 {
		if defPos == 1 {
			varName = guardToks[0].Str
		}
		guardToks = guardToks[defPos+1:]
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
	tsName := p.scope + "/_ts"
	p.SymAdd(symbol.UnsetAddr, tsName, vm.Value{}, symbol.Var, nil)
	guardParsed, err := p.parseExpr(guardToks, "")
	if err != nil {
		return nil, err
	}
	out = append(out, newIdent(tsName, pos))
	out = append(out, guardParsed...)
	out = append(out, newToken(lang.Define, "", pos, 1))
	// Split switch body into case clauses.
	clauses, err := p.Scan(in[len(in)-1].Block(), true)
	if err != nil {
		return nil, err
	}
	sc := clauses.SplitStart(lang.Case)
	// Move default to last position.
	lsc := len(sc) - 1
	for i, cl := range sc {
		if cl[1].Tok == lang.Colon && i != lsc {
			sc[i], sc[lsc] = sc[lsc], sc[i]
			break
		}
	}
	nc := len(sc) - 1
	for i, cl := range sc {
		co, err := p.parseTypeSwitchClause(cl, i, nc, tsName, varName)
		if err != nil {
			return nil, err
		}
		out = append(out, co...)
	}
	out = append(out, newLabel(p.breakLabel, in[len(in)-1].Pos))
	return out, nil
}

// parseTypeSwitchClause generates code for one case clause of a type switch.
func (p *Parser) parseTypeSwitchClause(in Tokens, index, maximum int, tsName, varName string) (out Tokens, err error) {
	in = append(in, newSemicolon(in[len(in)-1].Pos))
	tl := in.Split(lang.Colon)
	if len(tl) != 2 {
		return nil, errors.New("invalid type switch case clause")
	}
	conds := tl[0][1:] // tokens after 'case' keyword
	pos := in[0].Pos
	switchScope := p.scope // e.g. "main/switch0"

	// Push a case-specific scope so that v has a unique symbol per case.
	caseScopeName := "c" + strconv.Itoa(index)
	p.pushScope(caseScopeName)

	// Declare v in this case scope.
	var vScoped string
	if varName != "" {
		vScoped = p.scope + "/" + varName
		p.SymAdd(symbol.UnsetAddr, vScoped, vm.Value{}, symbol.Var, nil)
	}
	body, err := p.parseStmts(tl[1])
	p.popScope() // back to switchScope
	if err != nil {
		return nil, err
	}

	endLabel := p.breakLabel // e.g. "main/switch0e"

	// Check for default clause (no types between 'case' and ':').
	if len(conds) == 0 || (len(conds) == 1 && conds[0].Tok == lang.Semicolon) {
		caseLabel := fmt.Sprintf("%sc%d.0", switchScope, index)
		out = append(out, newLabel(caseLabel, pos))
		if varName != "" {
			// v = iface (interface type); compiler infers type from rhs.
			out = append(out, newIdent(vScoped, pos))
			out = append(out, newIdent(tsName, pos))
			out = append(out, newToken(lang.Assign, "", pos, 1))
		}
		out = append(out, body...)
		return out, nil
	}

	lcond := conds.Split(lang.Comma)
	isMulti := len(lcond) > 1
	bodyLabel := fmt.Sprintf("%sc%d_body", switchScope, index)

	for i, cond := range lcond {
		subLabel := fmt.Sprintf("%sc%d.%d", switchScope, index, i)
		var nextLabel string
		switch {
		case i < len(lcond)-1:
			nextLabel = fmt.Sprintf("%sc%d.%d", switchScope, index, i+1)
		case index < maximum:
			nextLabel = fmt.Sprintf("%sc%d.0", switchScope, index+1)
		default:
			nextLabel = endLabel
		}

		out = append(out, newLabel(subLabel, pos))

		var typ *vm.Type
		isNilCase := len(cond) == 1 && cond[0].Tok == lang.Ident && cond[0].Str == "nil"
		if !isNilCase {
			typ, _, err = p.parseTypeExpr(cond)
			if err != nil {
				return nil, err
			}
		}

		out = append(out, newIdent(tsName, pos))
		out = append(out, newToken(lang.TypeSwitchJump, nextLabel, pos, typ))

		if varName != "" {
			out = append(out, newIdent(vScoped, pos))
			out = append(out, newIdent(tsName, pos))
			if isMulti || isNilCase {
				out = append(out, newToken(lang.Assign, "", pos, 1))
			} else {
				out = append(out, newTypeAssert(typ, pos, 0))
				out = append(out, newToken(lang.Assign, "", pos, 1))
			}
		}

		if isMulti && i < len(lcond)-1 {
			out = append(out, newGoto(bodyLabel, pos))
		}
	}

	if isMulti {
		out = append(out, newLabel(bodyLabel, pos))
	}
	out = append(out, body...)
	if index != maximum {
		out = append(out, newGoto(endLabel, pos))
	}
	return out, nil
}

func (p *Parser) parseCaseClause(in Tokens, index, maximum int, condSwitch, prevFallthrough bool) (out Tokens, hasFallthrough bool, err error) {
	in = append(in, newSemicolon(in[len(in)-1].Pos)) // Force a ';' at the end of body clause.
	var conds, body Tokens
	tl := in.Split(lang.Colon)
	if len(tl) != 2 {
		return nil, false, errors.New("invalid case clause")
	}
	conds = tl[0][1:]
	pos := in[0].Pos

	// Pre-scan raw body for fallthrough before parsing statements.
	bodyRaw := tl[1]
	if fi := bodyRaw.Index(lang.Fallthrough); fi >= 0 {
		if index == maximum {
			return nil, false, errors.New("cannot fallthrough final case in switch")
		}
		if fi+2 < len(bodyRaw) {
			return nil, false, errors.New("fallthrough statement out of place")
		}
		hasFallthrough = true
		bodyRaw = bodyRaw[:fi]
	}
	if body, err = p.parseStmts(bodyRaw); err != nil {
		return nil, false, err
	}

	lcond := conds.Split(lang.Comma)
	isMulti := len(lcond) > 1
	bodyLabel := fmt.Sprintf("%sc%d_body", p.scope, index)
	miss := p.scope + "e"
	if index < maximum {
		miss = fmt.Sprintf("%sc%d.0", p.scope, index+1)
	}
	for i, cond := range lcond {
		if cond, err = p.parseExpr(cond, ""); err != nil {
			return nil, false, err
		}
		out = append(out, newLabel(fmt.Sprintf("%sc%d.%d", p.scope, index, i), 0))
		if len(cond) > 0 {
			out = append(out, cond...)
			if condSwitch {
				out = append(out, newEqualSet(cond[0].Pos))
			}
			if isMulti && i < len(lcond)-1 {
				out = append(out, newJumpFalse(fmt.Sprintf("%sc%d.%d", p.scope, index, i+1), cond[len(cond)-1].Pos))
				out = append(out, newGoto(bodyLabel, pos))
				continue
			}
			out = append(out, newJumpFalse(miss, cond[len(cond)-1].Pos))
		}
	}
	if isMulti || prevFallthrough {
		out = append(out, newLabel(bodyLabel, pos))
	}
	out = append(out, body...)
	if hasFallthrough {
		out = append(out, newGoto(fmt.Sprintf("%sc%d_body", p.scope, index+1), pos))
	} else if index != maximum {
		out = append(out, newGoto(p.scope+"e", 0))
	}
	return out, hasFallthrough, err
}

func (p *Parser) parseLabel(in Tokens) (out Tokens, err error) {
	return Tokens{newLabel(p.funcScope+"/"+in[0].Str, in[0].Pos)}, nil
}

func (p *Parser) parseReturn(in Tokens) (out Tokens, err error) {
	if l := len(in); l > 1 {
		if out, err = p.parseExpr(in[1:], ""); err != nil {
			return out, err
		}
	} else {
		if l == 0 {
			in = Tokens{newReturn(0)} // Implicit return in functions with no return parameters.
		}
		// Bare return: push named return vars in declaration order (reverse of namedOut).
		for i := len(p.namedOut) - 1; i >= 0; i-- {
			out = append(out, newIdent(p.namedOut[i], in[0].Pos))
		}
	}

	// TODO: the function symbol should be already present in the parser context.
	// otherwise no way to handle anonymous func.
	s := p.function
	in[0].Arg = []any{s.Type.Rtype.NumOut(), s.Type.Rtype.NumIn()}
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

func (p *Parser) precedence(t Token) int {
	return p.TokenProps[t.Tok].Precedence
}
