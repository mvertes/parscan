// Package goparser implements a structured parser for Go.
package goparser

import (
	"errors"
	"fmt"
	"reflect"
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
	Packages map[string]*symbol.Package
	function *symbol.Symbol // current function
	scope    string         // current scope
	fname    string         // current function name
	pkgName  string         // current package name
	noPkg    bool           // true if package statement is not mandatory (test, repl).

	funcScope     string
	framelen      map[string]int // length of function frames indexed by funcScope
	labelCount    map[string]int
	breakLabel    string
	continueLabel string
	pendingLabel  string               // user label preceding the current for/switch statement
	labeledJump   map[string][2]string // maps user label to [continueLabel, breakLabel]
	clonum        int                  // closure instance number
	blankSeq      int                  // counter for unique blank identifier names
	namedOut      []string             // scoped names of named return vars for current function
	SymTracker    []string             // accumulates newly-added symbol keys during a checkpoint window; nil = not tracking
	typeOnly      bool                 // when true, addSymVar is a no-op (Phase 1 signature-only parse)
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

// ImportPackageValues populates packages with values.
func (p *Parser) ImportPackageValues(m map[string]map[string]reflect.Value) {
	for k, v := range m {
		p.Packages[k] = symbol.BinPkg(v, k)
	}
}

// scopedName returns name qualified by the current scope (e.g. "main/foo").
func (p *Parser) scopedName(name string) string {
	return strings.TrimPrefix(p.scope+"/"+name, "/")
}

// labelName returns name qualified by the current function scope, for labels and gotos.
func (p *Parser) labelName(name string) string { return p.funcScope + "/" + name }

// takePendingLabel returns any pending user label and clears it.
func (p *Parser) takePendingLabel() string {
	l := p.pendingLabel
	p.pendingLabel = ""
	return l
}

// blankName returns a unique internal name for the blank identifier "_", so
// that multiple "_" in the same declaration each get their own symbol entry.
func (p *Parser) blankName() string {
	n := "_" + strconv.Itoa(p.blankSeq)
	p.blankSeq++
	return n
}

func (p *Parser) addLocalVar(name string) string {
	if name == "_" {
		name = p.blankName()
	}
	scoped := p.scopedName(name)
	p.SymAdd(p.framelen[p.funcScope], scoped, vm.Value{}, symbol.LocalVar, nil)
	p.framelen[p.funcScope]++
	return scoped
}

func (p *Parser) inferDefineType(rhs Tokens, scopedName string) {
	sym := p.Symbols[scopedName]
	if sym == nil || sym.Type != nil {
		return // not found, or type already set
	}
	n := len(rhs)
	if n == 0 {
		return
	}
	// Check for &T{} (Addr at end, Composite before it) or T{} (Composite at end).
	hasAddr := rhs[n-1].Tok == lang.Addr
	compositeIdx := n - 1
	if hasAddr {
		compositeIdx = n - 2
	}
	if compositeIdx < 0 || rhs[compositeIdx].Tok != lang.Composite || rhs[compositeIdx].Str == "" {
		return
	}
	s, _, ok := p.Symbols.Get(rhs[compositeIdx].Str, p.scope)
	if !ok || s.Kind != symbol.Type || s.Type == nil {
		return
	}
	if hasAddr {
		sym.Type = vm.PointerTo(s.Type)
	} else {
		sym.Type = s.Type
	}
}

func (p *Parser) addGlobalVar(name string) string {
	if name == "_" {
		name = p.blankName()
	}
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

func caseLabel(scope string, index, sub int) string {
	return fmt.Sprintf("%sc%d.%d", scope, index, sub)
}

func caseBodyLabel(scope string, index int) string {
	return fmt.Sprintf("%sc%d_body", scope, index)
}

func moveDefaultLast(clauses []Tokens) {
	last := len(clauses) - 1
	for i, cl := range clauses {
		if cl[1].Tok == lang.Colon && i != last {
			clauses[i], clauses[last] = clauses[last], clauses[i]
			return
		}
	}
}

// NewParser returns a new parser.
func NewParser(spec *lang.Spec, noPkg bool) *Parser {
	p := &Parser{
		Scanner:     scan.NewScanner(spec),
		Symbols:     symbol.SymMap{},
		Packages:    map[string]*symbol.Package{},
		noPkg:       noPkg,
		framelen:    map[string]int{},
		labelCount:  map[string]int{},
		labeledJump: map[string][2]string{},
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
// Labeled statements (Label: for/switch ...) are treated as a single statement.
func (p *Parser) stmtEnd(toks Tokens) (int, error) {
	end := toks.Index(lang.Semicolon)
	if end == -1 {
		return -1, scan.ErrBlock
	}
	firstTok := toks[0].Tok
	// A label "Ident :" followed by a HasInit statement is treated as one statement.
	if firstTok == lang.Ident && len(toks) > 2 && toks[1].Tok == lang.Colon {
		firstTok = toks[2].Tok
	}
	if p.TokenProps[firstTok].HasInit {
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

// SplitAndSortVarDecls splits var(...) blocks into individual declarations,
// topologically sorts them by dependency, and returns the reordered list.
// Funcs and other statements keep their original relative positions; only
// var declarations are extracted, sorted, and placed back into var slots.
func (p *Parser) SplitAndSortVarDecls(decls []Tokens) []Tokens {
	// Expand var blocks and identify var slot positions.
	type slot struct {
		pos  int    // position in expanded list
		decl Tokens // the var declaration
	}
	var expanded []Tokens
	var varSlots []slot
	for _, decl := range decls {
		if len(decl) == 0 {
			continue
		}
		switch decl[0].Tok {
		case lang.Var:
			for _, vd := range p.splitVarBlock(decl) {
				varSlots = append(varSlots, slot{pos: len(expanded), decl: vd})
				expanded = append(expanded, vd)
			}
		default:
			expanded = append(expanded, decl)
		}
	}
	if len(varSlots) <= 1 {
		return expanded
	}

	// Extract var declarations, sort by dependency, and place back.
	vars := make([]Tokens, len(varSlots))
	for i, s := range varSlots {
		vars[i] = s.decl
	}
	vars = p.sortByDeps(vars)
	for i, s := range varSlots {
		expanded[s.pos] = vars[i]
	}
	return expanded
}

func (p *Parser) varLines(toks Tokens) ([]Tokens, error) {
	if len(toks) < 2 {
		return nil, errors.New("missing expression")
	}
	if toks[1].Tok != lang.ParenBlock {
		return []Tokens{toks[1:]}, nil
	}
	inner, err := p.Scan(toks[1].Block(), false)
	if err != nil {
		return nil, err
	}
	return inner.Split(lang.Semicolon), nil
}

func (p *Parser) splitVarBlock(decl Tokens) []Tokens {
	lines, err := p.varLines(decl)
	if err != nil || len(lines) <= 1 {
		return []Tokens{decl}
	}
	result := make([]Tokens, 0, len(lines))
	for _, line := range lines {
		if len(line) > 0 {
			d := make(Tokens, 1, 1+len(line))
			d[0] = decl[0]
			result = append(result, append(d, line...))
		}
	}
	return result
}

func (p *Parser) sortByDeps(decls []Tokens) []Tokens {
	if len(decls) <= 1 {
		return decls
	}
	nameSet := map[string]int{}
	for i, decl := range decls {
		if len(decl) >= 2 && decl[1].Tok == lang.Ident {
			nameSet[decl[1].Str] = i
		}
	}
	if len(nameSet) == 0 {
		return decls
	}

	n := len(decls)
	rdeps := make([][]int, n)
	inDeg := make([]int, n)
	for i, decl := range decls {
		seen := map[int]bool{}
		rhs := decl[1:] // skip "var" keyword
		if j := rhs.Index(lang.Assign); j >= 0 {
			rhs = rhs[j+1:]
		}
		p.collectIdents(rhs, nameSet, seen)
		for dep := range seen {
			rdeps[dep] = append(rdeps[dep], i)
			inDeg[i]++
		}
	}

	queue := make([]int, 0, n)
	for i, d := range inDeg {
		if d == 0 {
			queue = append(queue, i)
		}
	}
	result := make([]Tokens, 0, n)
	for head := 0; head < len(queue); head++ {
		i := queue[head]
		result = append(result, decls[i])
		for _, j := range rdeps[i] {
			if inDeg[j]--; inDeg[j] == 0 {
				queue = append(queue, j)
			}
		}
	}
	for i, d := range inDeg {
		if d > 0 {
			result = append(result, decls[i])
		}
	}
	return result
}

func (p *Parser) collectIdents(toks Tokens, nameSet map[string]int, out map[int]bool) {
	for _, t := range toks {
		if t.Tok == lang.Ident {
			if dep, ok := nameSet[t.Str]; ok {
				out[dep] = true
			}
		} else if t.Tok.IsBlock() {
			if inner, err := p.Scan(t.Block(), false); err == nil {
				p.collectIdents(inner, nameSet, out)
			}
		}
	}
}

// ParseOneStmt parses a single pre-scanned statement token slice.
func (p *Parser) ParseOneStmt(toks Tokens) (Tokens, error) {
	return p.parseStmt(toks)
}

// ParseDecl resolves a declaration's symbols (Phase 1) without emitting code.
// Returns handled=true if fully resolved, false if code generation is needed.
func (p *Parser) ParseDecl(toks Tokens) (handled bool, err error) {
	for len(toks) > 0 && toks[len(toks)-1].Tok == lang.Comment {
		toks = toks[:len(toks)-1]
	}
	if len(toks) == 0 {
		return true, nil
	}
	if toks[0].Tok != lang.Package && p.pkgName == "" {
		if !p.noPkg {
			return false, errors.New("no package defined")
		}
		p.pkgName = "main"
	}
	switch toks[0].Tok {
	case lang.Package:
		_, err = p.parsePackage(toks)
		return true, err
	case lang.Import:
		_, err = p.parseImports(toks)
		return true, err
	case lang.Const:
		_, err = p.parseConst(toks)
		return true, err
	case lang.Type:
		_, err = p.parseType(toks)
		return true, err
	case lang.Func:
		if err := p.registerFunc(toks); err != nil {
			return false, err
		}
		return false, nil // Body still needs full parse + generate.
	case lang.Var:
		return p.parseVarDecl(toks)
	}
	return false, nil
}

func (p *Parser) registerFunc(toks Tokens) error {
	if len(toks) < 3 || toks[0].Tok != lang.Func {
		return nil
	}

	var fname string
	var recvVarName string // raw receiver variable name (for methods)
	var sigToks Tokens     // tokens to pass to parseTypeExpr (signature without receiver)

	bi := toks.LastIndex(lang.BraceBlock)
	if bi < 0 {
		return nil
	}

	switch {
	case toks[1].Tok == lang.Ident:
		// Plain function: func Name(params) rettype { ... }
		fname = toks[1].Str
		sigToks = toks[:bi]

	case toks[1].Tok == lang.ParenBlock && len(toks) > 2 && toks[2].Tok == lang.Ident:
		// Method or anonymous function. Disambiguate: if toks[2] is a known
		// type and toks[3] is not a ParenBlock (param list), this is an anonymous
		// func with a named return type (e.g. func(int) T {...}), not a method.
		if s, _, ok := p.Symbols.Get(toks[2].Str, p.scope); ok && s.IsType() {
			if len(toks) < 4 || toks[3].Tok != lang.ParenBlock {
				return nil
			}
		}
		// Method: func (recv) Name(params) rettype { ... }
		recvr, err := p.Scan(toks[1].Block(), false)
		if err != nil {
			return nil
		}
		typeName := recvTypeName(recvr)
		if typeName == "" {
			return nil
		}
		fname = typeName + "." + toks[2].Str
		if len(recvr) >= 2 && recvr[0].Tok == lang.Ident {
			recvVarName = recvr[0].Str
		}
		// Build signature tokens without receiver: [func, Name, params..., rettype].
		sigToks = make(Tokens, 0, 1+bi-2)
		sigToks = append(sigToks, toks[0])
		sigToks = append(sigToks, toks[2:bi]...)

	default:
		return nil // Anonymous function.
	}

	s, _, ok := p.Symbols.Get(fname, p.scope)
	if ok && s.Type != nil {
		return nil
	}
	if !ok {
		s = &symbol.Symbol{Name: fname, Used: true, Index: symbol.UnsetAddr}
		key := p.scopedName(fname)
		p.SymSet(key, s)
	}
	typ, inNames, outNames, err := p.parseFuncSig(sigToks)
	if err != nil {
		if !ok {
			delete(p.Symbols, p.scopedName(fname))
		}
		return err
	}
	s.Kind = symbol.Func
	s.Type = typ
	s.RecvName = recvVarName
	s.InNames = inNames
	s.OutNames = outNames
	return nil
}

func recvTypeName(recvr Tokens) string {
	// Walk backwards: last Ident is the type name, preceded by * for pointer receivers.
	for i := len(recvr) - 1; i >= 0; i-- {
		if recvr[i].Tok == lang.Ident {
			if i > 0 && recvr[i-1].Tok == lang.Mul {
				return "*" + recvr[i].Str
			}
			return recvr[i].Str
		}
	}
	return ""
}

func (p *Parser) parseVarDecl(toks Tokens) (handled bool, err error) {
	lines, err := p.varLines(toks)
	if err != nil {
		return true, err
	}
	hasInit := false
	for _, lt := range lines {
		if lt.Index(lang.Assign) >= 0 {
			hasInit = true
			break
		}
	}
	if hasInit {
		for _, lt := range lines {
			decl := lt
			var rhs Tokens
			if i := decl.Index(lang.Assign); i >= 0 {
				rhs = decl[i+1:]
				decl = decl[:i]
			}
			// Resolve type once for all names sharing this declaration.
			var rhsTyp *vm.Type
			if len(rhs) > 0 && rhs[0].Tok == lang.BracketBlock {
				elemTyp, n, err := p.parseTypeExpr(rhs)
				if errors.Is(err, ErrEllipsisArray) {
					rhsTyp, _ = p.resolveEllipsisArray(elemTyp, rhs, n)
				} else if err == nil {
					rhsTyp = elemTyp
				}
			}
			// Right-to-left so a trailing type applies to all names (Go grammar: "a, b int").
			parts := decl.Split(lang.Comma)
			var lastTyp *vm.Type
			for i := len(parts) - 1; i >= 0; i-- {
				ct := parts[i]
				if len(ct) == 0 {
					continue
				}
				if ct[0].Tok != lang.Ident {
					continue
				}
				rawName := ct[0].Str
				if rawName == "_" {
					rawName = p.blankName()
				}
				name := p.scopedName(rawName)
				if _, _, ok := p.Symbols.Get(rawName, p.scope); !ok {
					p.SymAdd(symbol.UnsetAddr, name, nilValue, symbol.Var, nil)
				}
				if len(ct) > 1 {
					if t, _, _ := p.parseTypeExpr(ct[1:]); t != nil {
						lastTyp = t
					}
				}
				typ := rhsTyp
				if lastTyp != nil {
					typ = lastTyp
				}
				if typ != nil {
					p.Symbols[name].Type = typ
				}
			}
		}
		return false, nil
	}
	// No initializer: full parse is just symbol registration.
	_, err = p.parseVar(toks)
	return true, err
}

func (p *Parser) parseStmt(in Tokens) (out Tokens, err error) {
	for len(in) > 0 && in[len(in)-1].Tok == lang.Comment {
		in = in[:len(in)-1]
	}
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
	case lang.Go:
		return p.parseGo(in)
	case lang.Select:
		return p.parseSelect(in)
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
	case lang.BraceBlock:
		label := "block" + strconv.Itoa(p.labelCount[p.scope])
		p.labelCount[p.scope]++
		p.pushScope(label)
		defer p.popScope()
		return p.Parse(in[0].Block())
	case lang.Mul, lang.ParenBlock:
		if i := in.Index(lang.Assign); i > 0 {
			return p.parseAssign(in, i)
		}
		if op, i := indexCompoundAssign(in); i > 0 {
			return p.parseCompoundAssign(in, i, op)
		}
		if l := len(in); l >= 2 && (in[l-1].Tok == lang.Inc || in[l-1].Tok == lang.Dec) {
			return p.parseIncDec(in)
		}
		return p.parseExpr(in, "")
	case lang.Ident:
		if in.Index(lang.Colon) == 1 {
			return p.parseLabel(in)
		}
		if i := in.Index(lang.Arrow); i > 0 {
			// Only a send statement (ch <- v) if the arrow precedes any assignment.
			defIdx := in.Index(lang.Define)
			assIdx := in.Index(lang.Assign)
			if (defIdx < 0 || i < defIdx) && (assIdx < 0 || i < assIdx) {
				return p.parseChanSend(in, i)
			}
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
			if len(lhs) == 2 && len(toks) > 0 {
				switch toks[len(toks)-1].Tok {
				case lang.TypeAssert:
					toks[len(toks)-1].Arg[0] = 1
				case lang.Index:
					toks[len(toks)-1].Arg = []any{1}
				case lang.Arrow:
					toks[len(toks)-1].Arg = []any{1}
				}
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
			if p.funcScope != "" && len(lhs) == 1 {
				p.inferDefineType(toks, out[lhsPositions[0]].Str)
			}
		}
		return out, err
	}
	return p.parseAssignMultiRHS(in, lhs, rhs, aindex, define)
}

func (p *Parser) parseAssignMultiRHS(in Tokens, lhs, rhs []Tokens, aindex int, define bool) (out Tokens, err error) {
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
			out = out[:len(out)-1]
			out = append(out, toks...)
			out = append(out, newToken(lang.IndexAssign, "", in[aindex].Pos, 1))
		case lang.Deref:
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

func indexCompoundAssign(in Tokens) (lang.Token, int) {
	for i, t := range in {
		if op, ok := compoundAssignOp[t.Tok]; ok {
			return op, i
		}
	}
	return 0, -1
}

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
	narg, err := p.numItems(callTok.Block(), lang.Comma)
	if err != nil {
		return nil, err
	}

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

func (p *Parser) parseGo(in Tokens) (out Tokens, err error) {
	if len(in) < 2 {
		return nil, errors.New("invalid go statement")
	}
	expr := in[1:]
	last := len(expr) - 1
	if last < 0 || expr[last].Tok != lang.ParenBlock {
		return nil, errors.New("go requires a function call")
	}
	callTok := expr[last]
	narg, err := p.numItems(callTok.Block(), lang.Comma)
	if err != nil {
		return nil, err
	}

	if out, err = p.parseExpr(expr[:last], ""); err != nil {
		return out, err
	}
	argToks, err := p.parseBlock(callTok, "")
	if err != nil {
		return out, err
	}
	out = append(out, argToks...)
	out = append(out, newGo(callTok.Pos, narg))
	return out, nil
}

func (p *Parser) parseChanSend(in Tokens, arrowIdx int) (out Tokens, err error) {
	if out, err = p.parseExpr(in[:arrowIdx], ""); err != nil {
		return out, err
	}
	val, err := p.parseExpr(in[arrowIdx+1:], "")
	if err != nil {
		return nil, err
	}
	out = append(out, val...)
	out = append(out, newChanSend(in[arrowIdx].Pos))
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
		j, ok := p.labeledJump[p.labelName(in[1].Str)]
		if !ok {
			return nil, ErrBreak
		}
		label = j[1]
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
		j, ok := p.labeledJump[p.labelName(in[1].Str)]
		if !ok || j[0] == "" {
			return nil, ErrContinue
		}
		label = j[0]
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
	return Tokens{newGoto(p.labelName(in[1].Str), in[0].Pos)}, nil
}

func (p *Parser) parseFor(in Tokens) (out Tokens, err error) {
	// TODO: detect invalid code.
	var init, cond, post, body, final Tokens
	hasRange := in.Index(lang.Range) >= 0
	pendingLabel := p.takePendingLabel()
	defer p.pushBreakScope("for", pendingLabel, true)()
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
			last := &out[len(out)-1]
			if len(last.Arg) == 0 {
				last.Arg = []any{0}
			}
			n, _ := last.Arg[0].(int)
			cond = Tokens{newNext(p.breakLabel, in[1].Pos, n)}
			final = Tokens{newToken(lang.Stop, "", in[1].Pos, n)}
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
	if pendingLabel != "" {
		p.labeledJump[pendingLabel] = [2]string{p.continueLabel, p.breakLabel}
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

func (p *Parser) registerParamsFromSym(s *symbol.Symbol) {
	nparams := len(s.Type.Params)
	for i, name := range s.InNames {
		if name == "" {
			continue
		}
		p.addSymVar(i, nparams, p.scopedName(name), s.Type.Params[i], parseTypeIn)
	}
	// Reverse order to match frame slot assignment in parseParamTypes.
	for i := len(s.OutNames) - 1; i >= 0; i-- {
		name := s.OutNames[i]
		if name == "" {
			continue
		}
		p.addSymVar(i, len(s.OutNames), p.scopedName(name), &vm.Type{Rtype: s.Type.Rtype.Out(i)}, parseTypeOut)
	}
}

func (p *Parser) parseFunc(in Tokens) (out Tokens, err error) {
	// TODO: handle parametric types (generics)
	var fname string

	switch in[1].Tok {
	case lang.Ident:
		fname = in[1].Str
	case lang.ParenBlock:
		// receiver, or anonymous function parameters.
		if t := in[2]; t.Tok == lang.Ident {
			// If in[2] is a known type and in[3] is not a ParenBlock (param list),
			// this is an anonymous func with a named return type (e.g. func(T) Ret{}).
			if s, _, ok := p.Symbols.Get(t.Str, p.scope); ok && s.IsType() {
				if len(in) < 4 || in[3].Tok != lang.ParenBlock {
					fname = "#f" + strconv.Itoa(p.clonum) // Generated closure symbol name.
					p.clonum++
					break
				}
			}
			// Method: derive fname from receiver type name.
			recvr, scanErr := p.Scan(in[1].Block(), false)
			if scanErr != nil {
				return nil, scanErr
			}
			fname = recvTypeName(recvr) + "." + in[2].Str
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

	// For methods, register the receiver directly at the function scope
	// using cached info from Phase 1.
	if s.RecvName != "" {
		recvScoped := p.scope + "/" + s.RecvName
		s.FreeVars = []string{recvScoped}
		recvTypName, _, _ := strings.Cut(fname, ".")
		lookupName := strings.TrimPrefix(recvTypName, "*")
		if recvTypSym, _, ok := p.Symbols.Get(lookupName, p.scope); ok && recvTypSym.IsType() {
			recvTyp := recvTypSym.Type
			if strings.HasPrefix(recvTypName, "*") {
				recvTyp = vm.PointerTo(recvTyp)
			}
			p.addSymVar(0, 1, recvScoped, recvTyp, parseTypeRecv)
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

	bi := in.LastIndex(lang.BraceBlock)
	if bi < 0 {
		return out, ErrBody
	}

	if s.Type != nil {
		p.registerParamsFromSym(s)
	} else {
		typ, _, err := p.parseTypeExpr(in[:bi])
		if err != nil {
			return out, err
		}
		if typ == nil {
			return out, errors.New("could not determine function type")
		}
		s.Kind = symbol.Func
		s.Type = typ
	}
	p.function = s

	toks, err := p.Parse(in[bi].Block())
	if err != nil {
		return out, err
	}
	l := p.framelen[p.funcScope] - 1
	if l < 0 {
		l = 0
	}
	out = append(out, newGrow(l, in[0].Pos))
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
	pendingLabel := p.takePendingLabel()
	defer p.pushBreakScope("switch", pendingLabel, false)()
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
	moveDefaultLast(sc)
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
	pendingLabel := p.takePendingLabel()
	defer p.pushBreakScope("switch", pendingLabel, false)()
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
	moveDefaultLast(sc)
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
		caseLabel := caseLabel(switchScope, index, 0)
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
	bodyLabel := caseBodyLabel(switchScope, index)

	for i, cond := range lcond {
		subLabel := caseLabel(switchScope, index, i)
		var nextLabel string
		switch {
		case i < len(lcond)-1:
			nextLabel = caseLabel(switchScope, index, i+1)
		case index < maximum:
			nextLabel = caseLabel(switchScope, index+1, 0)
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
	bodyLabel := caseBodyLabel(p.scope, index)
	miss := p.scope + "e"
	if index < maximum {
		miss = caseLabel(p.scope, index+1, 0)
	}
	for i, cond := range lcond {
		if cond, err = p.parseExpr(cond, ""); err != nil {
			return nil, false, err
		}
		out = append(out, newLabel(caseLabel(p.scope, index, i), 0))
		if len(cond) > 0 {
			out = append(out, cond...)
			if condSwitch {
				out = append(out, newEqualSet(cond[0].Pos))
			}
			if isMulti && i < len(lcond)-1 {
				out = append(out, newJumpFalse(caseLabel(p.scope, index, i+1), cond[len(cond)-1].Pos))
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
		out = append(out, newGoto(caseBodyLabel(p.scope, index+1), pos))
	} else if index != maximum {
		out = append(out, newGoto(p.scope+"e", 0))
	}
	return out, hasFallthrough, err
}

type selectCase struct {
	dir      reflect.SelectDir
	chanToks Tokens
	sendToks Tokens
	body     Tokens
	valName  string
	okName   string
}

func (p *Parser) parseSelectCase(cl Tokens, index int, pos int, ci *selectCase) error {
	tl := cl.Split(lang.Colon)
	if len(tl) != 2 {
		return errors.New("invalid select case clause")
	}
	header := tl[0][1:]
	bodyToks := append(Tokens{}, tl[1]...)
	bodyToks = append(bodyToks, newSemicolon(pos))

	if len(header) == 0 {
		ci.dir = reflect.SelectDefault
		var err error
		ci.body, err = p.parseStmts(bodyToks)
		return err
	}

	arrowIdx := header.Index(lang.Arrow)
	defIdx := header.Index(lang.Define)
	assIdx := header.Index(lang.Assign)

	if arrowIdx > 0 && (defIdx < 0 || arrowIdx < defIdx) && (assIdx < 0 || arrowIdx < assIdx) {
		ci.dir = reflect.SelectSend
		var err error
		if ci.chanToks, err = p.parseExpr(header[:arrowIdx], ""); err != nil {
			return err
		}
		if ci.sendToks, err = p.parseExpr(header[arrowIdx+1:], ""); err != nil {
			return err
		}
		ci.body, err = p.parseStmts(bodyToks)
		return err
	}

	ci.dir = reflect.SelectRecv
	assignIdx := defIdx
	if assignIdx < 0 {
		assignIdx = assIdx
	}
	var chExpr Tokens
	if assignIdx >= 0 {
		isDefine := defIdx >= 0
		// Per-case scope so each case's variables are independent.
		p.pushScope("c" + strconv.Itoa(index))
		lhsToks := header[:assignIdx].Split(lang.Comma)
		for j, lt := range lhsToks {
			if len(lt) != 1 || lt[0].Tok != lang.Ident {
				p.popScope()
				return errors.New("invalid select recv assignment")
			}
			name := lt[0].Str
			if name == "_" {
				continue
			}
			var scopedName string
			if isDefine {
				if p.funcScope != "" {
					scopedName = p.addLocalVar(name)
				} else {
					scopedName = p.addGlobalVar(name)
				}
			} else {
				if _, sn, ok := p.Symbols.Get(name, p.scope); ok && sn != "" {
					scopedName = sn + "/" + name
				} else {
					scopedName = p.scopedName(name)
				}
			}
			if j == 0 {
				ci.valName = scopedName
			} else {
				ci.okName = scopedName
			}
		}
		chExpr = header[assignIdx+1:]
	} else {
		chExpr = header
	}
	if len(chExpr) == 0 || chExpr[0].Tok != lang.Arrow {
		if assignIdx >= 0 {
			p.popScope()
		}
		return errors.New("select recv case requires <-")
	}
	var err error
	if ci.chanToks, err = p.parseExpr(chExpr[1:], ""); err != nil {
		if assignIdx >= 0 {
			p.popScope()
		}
		return err
	}
	// Parse body after creating recv variables so the body can reference them.
	ci.body, err = p.parseStmts(bodyToks)
	if assignIdx >= 0 {
		p.popScope()
	}
	return err
}

// SelectCaseDesc describes one case of a select statement for the compiler.
type SelectCaseDesc struct {
	Dir     reflect.SelectDir
	ValName string // scoped name of recv value var ("" if none)
	OkName  string // scoped name of recv ok var ("" if none)
}

// parseSelect parses a select statement: select { case <-ch: ... case ch <- v: ... default: ... }.
func (p *Parser) parseSelect(in Tokens) (out Tokens, err error) {
	if len(in) < 2 || in[len(in)-1].Tok != lang.BraceBlock {
		return nil, errors.New("invalid select statement")
	}
	pos := in[0].Pos

	pendingLabel := p.takePendingLabel()
	defer p.pushBreakScope("select", pendingLabel, false)()

	clauses, err := p.Scan(in[len(in)-1].Block(), true)
	if err != nil {
		return nil, err
	}
	caseClauses := clauses.SplitStart(lang.Case)

	moveDefaultLast(caseClauses)

	cases := make([]selectCase, len(caseClauses))
	for i, cl := range caseClauses {
		if err := p.parseSelectCase(cl, i, pos, &cases[i]); err != nil {
			return nil, err
		}
	}

	descs := make([]SelectCaseDesc, len(cases))
	for i := range cases {
		descs[i] = SelectCaseDesc{Dir: cases[i].dir, ValName: cases[i].valName, OkName: cases[i].okName}
		switch cases[i].dir {
		case reflect.SelectRecv:
			out = append(out, cases[i].chanToks...)
		case reflect.SelectSend:
			out = append(out, cases[i].chanToks...)
			out = append(out, cases[i].sendToks...)
		}
	}

	out = append(out, newToken(lang.Select, "", pos, descs))

	// Emit condSwitch-style dispatch on chosen index.
	nc := len(cases) - 1
	for i, c := range cases {
		out = append(out, newLabel(caseLabel(p.scope, i, 0), 0))
		if i < nc {
			out = append(out, newToken(lang.Int, strconv.Itoa(i), pos))
			out = append(out, newEqualSet(pos))
			out = append(out, newJumpFalse(caseLabel(p.scope, i+1, 0), pos))
		}
		out = append(out, c.body...)
		if i < nc {
			out = append(out, newGoto(p.breakLabel, 0))
		}
	}

	out = append(out, newLabel(p.breakLabel, pos))
	return out, nil
}

func (p *Parser) parseLabel(in Tokens) (out Tokens, err error) {
	p.pendingLabel = p.labelName(in[0].Str)
	out = Tokens{newLabel(p.pendingLabel, in[0].Pos)}
	if len(in) > 2 {
		// Label followed by a compound statement (for, switch, ...) on the same line.
		stmtOut, err := p.parseStmt(in[2:])
		if err != nil {
			return out, err
		}
		out = append(out, stmtOut...)
	}
	return out, nil
}

func (p *Parser) parseReturn(in Tokens) (out Tokens, err error) {
	if l := len(in); l > 1 {
		for _, val := range in[1:].Split(lang.Comma) {
			if len(val) == 0 {
				continue
			}
			toks, err := p.parseExpr(val, "")
			if err != nil {
				return out, err
			}
			out = append(out, toks...)
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

	s := p.function
	if s == nil || s.Type == nil {
		return nil, errors.New("return statement outside function")
	}
	in[0].Arg = []any{s.Type.Rtype.NumOut(), s.Type}
	out = append(out, in[0])
	return out, err
}

func (p *Parser) numItems(s string, sep lang.Token) (int, error) {
	tokens, err := p.Scan(s, false)
	if err != nil {
		return -1, err
	}
	r := 0
	for _, t := range tokens.Split(sep) {
		if len(t) == 0 {
			continue
		}
		r++
	}
	return r, nil
}

func (p *Parser) pushBreakScope(prefix, pendingLabel string, hasContinue bool) func() {
	label := prefix + strconv.Itoa(p.labelCount[p.scope])
	p.labelCount[p.scope]++
	savedBreak, savedContinue := p.breakLabel, p.continueLabel
	p.pushScope(label)
	p.breakLabel = p.scope + "e"
	if hasContinue {
		p.continueLabel = p.scope + "b"
	}
	if pendingLabel != "" {
		cont := ""
		if hasContinue {
			cont = p.continueLabel
		}
		p.labeledJump[pendingLabel] = [2]string{cont, p.breakLabel}
	}
	return func() {
		p.breakLabel, p.continueLabel = savedBreak, savedContinue
		p.popScope()
	}
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
