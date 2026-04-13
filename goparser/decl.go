package goparser

import (
	"errors"
	"fmt"
	"go/constant"
	"go/token"
	"path"
	"reflect"

	"github.com/mvertes/parscan/lang"
	"github.com/mvertes/parscan/symbol"
	"github.com/mvertes/parscan/vm"
)

var nilValue = vm.ValueOf(nil)

func (p *Parser) parseConst(in Tokens) (out Tokens, err error) {
	if len(in) < 2 {
		return out, errors.New("missing expression")
	}
	if in[1].Tok != lang.ParenBlock {
		return p.parseConstLine(in[1:])
	}
	if in, err = p.Scan(in[1].Block(), false); err != nil {
		return out, err
	}

	lines := in.Split(lang.Semicolon)

	// Build expanded lines (apply iota implicit repetition) and record iota values.
	type constLine struct {
		toks Tokens
		iota int64
	}
	pending := make([]constLine, 0, len(lines))
	var prev Tokens
	var iotaIdx int
	for _, lt := range lines {
		// Strip trailing comments and skip comment-only lines.
		for len(lt) > 0 && lt[len(lt)-1].Tok == lang.Comment {
			lt = lt[:len(lt)-1]
		}
		if len(lt) == 0 {
			continue
		}
		if len(lt) == 1 && iotaIdx > 0 {
			lt = append(Tokens{lt[0]}, prev...)
		}
		pending = append(pending, constLine{toks: lt, iota: int64(iotaIdx)})
		if len(lt) > 1 {
			prev = lt[1:]
		}
		iotaIdx++
	}

	// Retry until no undefined const remains, or no progress is made.
	for len(pending) > 0 {
		var retry []constLine
		var firstErr error
		for _, cl := range pending {
			p.Symbols["iota"].Cval = constant.Make(cl.iota)
			ot, err := p.parseConstLine(cl.toks)
			if err != nil {
				var eu ErrUndefined
				if errors.As(err, &eu) {
					retry = append(retry, cl)
					if firstErr == nil {
						firstErr = err
					}
					continue
				}
				return out, err
			}
			out = append(out, ot...)
		}
		if len(retry) == len(pending) {
			return out, firstErr
		}
		pending = retry
	}
	return out, nil
}

func (p *Parser) parseConstLine(in Tokens) (out Tokens, err error) {
	decl := in
	var assign Tokens
	if i := decl.Index(lang.Assign); i >= 0 {
		assign = decl[i+1:]
		decl = decl[:i]
	}
	var vars []string
	var types []*vm.Type
	if types, vars, _, err = p.parseParamTypes(decl, parseTypeType); err != nil {
		if errors.Is(err, ErrMissingType) {
			for _, lt := range decl.Split(lang.Comma) {
				vars = append(vars, lt[0].Str)
				name := p.scopedName(lt[0].Str)
				p.SymAdd(symbol.UnsetAddr, name, nilValue, symbol.Const, nil)
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
		if v, err = p.parseExpr(v, ""); err != nil {
			return out, err
		}
		cval, ctyp, _, err := p.evalConstExpr(v)
		if err != nil {
			return out, err
		}
		name := p.scopedName(vars[i])
		var typ *vm.Type
		if i < len(types) {
			typ = types[i]
			cval = constConvert(cval, typ)
		} else if ctyp != nil {
			typ = ctyp
		}
		p.SymSet(name, &symbol.Symbol{
			Kind:  symbol.Const,
			Index: symbol.UnsetAddr,
			Type:  typ,
			Cval:  cval,
			Value: vm.ValueOf(typedConstValue(cval, typ)),
			Used:  true,
		})
	}
	return out, err
}

func (p *Parser) evalConstExpr(in Tokens) (cval constant.Value, ctyp *vm.Type, length int, err error) {
	l := len(in) - 1
	if l < 0 {
		return nil, nil, 0, errors.New("missing argument")
	}
	t := in[l]
	id := t.Tok
	switch {
	case id == lang.Period:
		if l < 1 || in[l-1].Tok != lang.Ident {
			return nil, nil, 0, errors.New("invalid package selector")
		}
		pkgName := in[l-1].Str
		s, _, ok := p.Symbols.Get(pkgName, p.scope)
		if !ok || s.Kind != symbol.Pkg {
			return nil, nil, 0, ErrUndefined{pkgName}
		}
		pkg, ok := p.Packages[s.PkgPath]
		if !ok {
			return nil, nil, 0, fmt.Errorf("package not found: %s", s.PkgPath)
		}
		v, ok := pkg.Values[t.Str[1:]]
		if !ok {
			return nil, nil, 0, fmt.Errorf("symbol not found in package %s: %s", s.PkgPath, t.Str[1:])
		}
		cv, err := vmValueToConst(v)
		if err != nil {
			return nil, nil, 0, err
		}
		return cv, nil, 2, nil // consumes Ident (pkg) + Period
	case id.IsBinaryOp():
		op2, typ2, l2, err := p.evalConstExpr(in[:l])
		if err != nil {
			return nil, nil, 0, err
		}
		op1, typ1, l1, err := p.evalConstExpr(in[:l-l2])
		if err != nil {
			return nil, nil, 0, err
		}
		length = 1 + l1 + l2
		tok := gotok[id]
		if id.IsBoolOp() {
			return constant.MakeBool(constant.Compare(op1, tok, op2)), nil, length, err
		}
		if id == lang.Shl || id == lang.Shr {
			s, ok := constant.Uint64Val(op2)
			if !ok {
				return nil, nil, 0, errors.New("invalid shift parameter")
			}
			cv := constant.Shift(op1, tok, uint(s))
			// go/constant uses arithmetic right-shift, which sign-extends negative
			// values produced by unary ^ on unsigned constants. Reinterpret as unsigned.
			if id == lang.Shr && typ1 != nil && isUnsignedKind(typ1.Rtype.Kind()) {
				v, _ := constant.Int64Val(cv)
				cv = constant.MakeUint64(uint64(v)) //nolint:gosec // reinterpret signed bits as unsigned
			}
			return cv, typ1, length, err
		}
		resTyp := typ1
		if resTyp == nil {
			resTyp = typ2
		}
		if tok == token.QUO && op1.Kind() == constant.Int && op2.Kind() == constant.Int {
			tok = token.QUO_ASSIGN // Force int result, see https://pkg.go.dev/go/constant#BinaryOp
		}
		return constant.BinaryOp(op1, tok, op2), resTyp, length, err
	case id.IsUnaryOp():
		op1, typ1, l1, err := p.evalConstExpr(in[:l])
		if err != nil {
			return nil, nil, 0, err
		}
		cv := constant.UnaryOp(gotok[id], op1, 0)
		// go/constant has no unsigned integer kind: ^ on 0 gives -1 (arbitrary
		// precision), not the width-limited complement Go requires for typed
		// unsigned constants. Recompute using the correct bit width.
		if id == lang.BitComp && typ1 != nil && isUnsignedKind(typ1.Rtype.Kind()) {
			v, _ := constant.Uint64Val(op1)
			bits := typ1.Rtype.Size() * 8
			mask := ^uint64(0) >> (64 - bits)
			cv = constant.MakeUint64(^v & mask)
		}
		return cv, typ1, 1 + l1, err
	case id.IsLiteral():
		tok := gotok[id]
		if id == lang.String && len(t.Str) > 0 && t.Str[0] == '\'' {
			tok = token.CHAR
		}
		return constant.MakeFromLiteral(t.Str, tok, 0), nil, 1, err
	case id == lang.Ident:
		s, _, ok := p.Symbols.Get(t.Str, p.scope)
		if !ok {
			return nil, nil, 0, ErrUndefined{t.Str}
		}
		if s.Kind != symbol.Const {
			return nil, nil, 0, errors.New("symbol is not a constant")
		}
		if s.Cval == nil {
			return nil, nil, 0, ErrUndefined{t.Str}
		}
		return s.Cval, s.Type, 1, err
	case id == lang.Call:
		narg := t.Arg[0].(int)
		// len/cap of an array or *array variable (bare or field access) is constant per Go spec.
		if narg == 1 {
			var fname string
			var rt reflect.Type
			var n int
			switch {
			case l >= 2 && in[l-1].Tok == lang.Ident && in[l-2].Tok == lang.Ident:
				if s, _, ok := p.Symbols.Get(in[l-1].Str, p.scope); ok && s.Type != nil {
					fname, rt, n = in[l-2].Str, s.Type.Rtype, 3
				}
			case l >= 3 && in[l-1].Tok == lang.Period && in[l-2].Tok == lang.Ident && in[l-3].Tok == lang.Ident:
				if s, _, ok := p.Symbols.Get(in[l-2].Str, p.scope); ok && s.Type != nil {
					bt := s.Type.Rtype
					if bt.Kind() == reflect.Ptr {
						bt = bt.Elem()
					}
					if bt.Kind() == reflect.Struct {
						if f, ok2 := bt.FieldByName(in[l-1].Str[1:]); ok2 {
							fname, rt, n = in[l-3].Str, f.Type, 4
						}
					}
				}
			}
			if rt != nil && (fname == "len" || fname == "cap") {
				if rt.Kind() == reflect.Ptr {
					rt = rt.Elem()
				}
				if rt.Kind() == reflect.Array {
					return constant.MakeInt64(int64(rt.Len())), nil, n, nil
				}
			}
		}
		args := make([]constant.Value, narg)
		rest := in[:l]
		totalLen := 1 // Call token
		for i := narg - 1; i >= 0; i-- {
			av, _, al, err := p.evalConstExpr(rest)
			if err != nil {
				return nil, nil, 0, err
			}
			args[i] = av
			totalLen += al
			rest = rest[:len(rest)-al]
		}
		if len(rest) == 0 || rest[len(rest)-1].Tok != lang.Ident {
			return nil, nil, 0, errors.New("unsupported constant call expression")
		}
		fname := rest[len(rest)-1].Str
		totalLen++
		// Handle builtins before symbol lookup to avoid scope-walk overhead.
		if fname == "len" {
			if narg != 1 {
				return nil, nil, 0, errors.New("len: wrong number of arguments")
			}
			if args[0] != nil && args[0].Kind() == constant.String {
				return constant.MakeInt64(int64(len(constant.StringVal(args[0])))), nil, totalLen, nil
			}
			return nil, nil, 0, errors.New("len: unsupported constant argument type")
		}
		if s, _, ok := p.Symbols.Get(fname, p.scope); ok && s.Kind == symbol.Type {
			if narg != 1 {
				return nil, nil, 0, errors.New("type conversion requires exactly one argument")
			}
			return constConvert(args[0], s.Type), s.Type, totalLen, nil
		}
		return nil, nil, 0, fmt.Errorf("unsupported constant call: %s", fname)
	default:
		return nil, nil, 0, errors.New("invalid constant expression")
	}
}

func isUnsignedKind(k reflect.Kind) bool {
	return k >= reflect.Uint && k <= reflect.Uintptr
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

// typedConstValue converts a constant value to a Go value with the given type.
// If typ is nil, it falls back to constValue (untyped).
func typedConstValue(c constant.Value, typ *vm.Type) any {
	v := constValue(c)
	if typ == nil || v == nil {
		return v
	}
	return reflect.ValueOf(v).Convert(typ.Rtype).Interface()
}

// constConvert converts a constant value to the target type, as in Go type conversions.
func constConvert(cv constant.Value, typ *vm.Type) constant.Value {
	rt := typ.Rtype
	switch {
	case rt.Kind() >= reflect.Int && rt.Kind() <= reflect.Int64:
		if cv.Kind() == constant.Float {
			f, _ := constant.Float64Val(cv)
			return constant.MakeInt64(int64(f))
		}
		return constant.ToInt(cv)
	case isUnsignedKind(rt.Kind()):
		if cv.Kind() == constant.Float {
			f, _ := constant.Float64Val(cv)
			return constant.MakeUint64(uint64(f))
		}
		// go/constant has no ToUint; extract int64 bits for correct wraparound.
		v, _ := constant.Int64Val(constant.ToInt(cv))
		return constant.MakeUint64(uint64(v)) //nolint:gosec // intentional wraparound
	case rt.Kind() == reflect.Float32 || rt.Kind() == reflect.Float64:
		return constant.ToFloat(cv)
	case rt.Kind() == reflect.String:
		if cv.Kind() == constant.Int {
			v, _ := constant.Int64Val(cv)
			return constant.MakeString(string(rune(v))) //nolint:gosec // intentional int-to-rune conversion
		}
		return cv
	}
	return cv
}

// vmValueToConst converts a vm.Value to a constant.Value for compile-time evaluation.
func vmValueToConst(v vm.Value) (constant.Value, error) {
	k := v.Kind()
	switch {
	case k == reflect.Bool:
		return constant.MakeBool(v.Bool()), nil
	case k >= reflect.Int && k <= reflect.Int64:
		return constant.MakeInt64(v.Int()), nil
	case isUnsignedKind(k):
		return constant.MakeUint64(v.Uint()), nil
	case k == reflect.Float32 || k == reflect.Float64:
		return constant.MakeFloat64(v.Float()), nil
	case k == reflect.String:
		return constant.MakeString(v.Reflect().String()), nil
	}
	return nil, fmt.Errorf("cannot use package value of kind %s as constant", k)
}

// Correspondence between language independent parscan tokens and Go stdlib tokens,
// To enable the use of the Go constant expression evaluator.
var gotok = map[lang.Token]token.Token{
	lang.Char:         token.CHAR,
	lang.Imag:         token.IMAG,
	lang.Int:          token.INT,
	lang.Float:        token.FLOAT,
	lang.String:       token.STRING,
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

func (p *Parser) parseImports(in Tokens) (out Tokens, err error) {
	if p.fname != "" {
		return out, errors.New("unexpected import")
	}
	if len(in) < 2 {
		return out, errors.New("missing expression")
	}
	if in[1].Tok != lang.ParenBlock {
		return p.parseImportLine(in[1:])
	}
	if in, err = p.Scan(in[1].Block(), false); err != nil {
		return out, err
	}
	for _, li := range in.Split(lang.Semicolon) {
		ot, err := p.parseImportLine(li)
		if err != nil {
			return out, err
		}
		out = append(out, ot...)
	}
	return out, err
}

func (p *Parser) parseImportLine(in Tokens) (out Tokens, err error) {
	l := len(in)
	if l == 0 {
		return out, errors.New("invalid number of arguments")
	}
	// Find the import path string, ignoring trailing tokens (e.g. comments).
	si := l - 1
	for si >= 0 && in[si].Tok != lang.String {
		si--
	}
	if si < 0 {
		return out, fmt.Errorf("invalid argument %v", in[0])
	}
	l = si + 1 // effective length up to and including the string token
	pp := in[si].Block()
	pkg, ok := p.Packages[pp]
	if !ok {
		if err = p.importSrc(pp); err != nil {
			return out, err
		}
		pkg = p.Packages[pp]
	}
	n := in[0].Str
	if l == 1 {
		// Derive package name from package path.
		d, f := path.Split(pp)
		n = f
		if ok, _ := path.Match(f, "v[0-9]*"); d != "" && ok {
			n = path.Base(d)
		}
	}
	if n == "." {
		// Import package symbols in the current scope.
		for k, v := range pkg.Values {
			p.SymSet(k, &symbol.Symbol{Index: symbol.UnsetAddr, Name: k, Kind: symbol.Value, PkgPath: pp, Value: v})
		}
	} else {
		p.SymSet(n, &symbol.Symbol{Kind: symbol.Pkg, PkgPath: pp, Index: symbol.UnsetAddr, Name: n})
	}
	return out, err
}

func (p *Parser) parsePackageDecl(in Tokens) (out Tokens, err error) {
	if len(in) != 2 {
		return out, errors.New("invalid number of arguments")
	}
	if in[1].Tok != lang.Ident {
		return out, errors.New("not an ident")
	}
	if p.pkgName != "" && p.pkgName != in[1].Str {
		return out, fmt.Errorf("package %s; expected %s", in[1].Str, p.pkgName)
	}
	p.pkgName = in[1].Str
	return out, err
}

func (p *Parser) parseType(in Tokens) (out Tokens, err error) {
	if len(in) < 2 {
		return out, ErrMissingType
	}
	if in[1].Tok != lang.ParenBlock {
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
		return out, ErrMissingType
	}
	if in[0].Tok != lang.Ident {
		return out, errors.New("not an ident")
	}
	isAlias := in[1].Tok == lang.Assign
	toks := in[1:]
	if isAlias {
		toks = toks[1:]
	}

	// For struct types, use a forward-declared placeholder to enable
	// self-references (*Node) and mutual references between types.
	name := p.scopedName(in[0].Str)
	var placeholder *vm.Type
	if !isAlias && len(toks) > 0 && toks[0].Tok == lang.Struct {
		if s, ok := p.Symbols[name]; ok && s.Kind == symbol.Type {
			// Reuse placeholder pre-registered by the compiler.
			placeholder = s.Type
		} else {
			placeholder = vm.NewStructType()
			placeholder.Name = in[0].Str
			p.SymAdd(symbol.UnsetAddr, name, vm.NewValue(placeholder.Rtype), symbol.Type, placeholder)
		}
	}

	typ, _, err := p.parseTypeExpr(toks)
	if err != nil {
		return out, err
	}

	if placeholder != nil {
		// Finalize: patches the internal reflect type in place so derived
		// types (e.g., *Node via PointerTo) see the real struct layout.
		placeholder.SetFields(typ)
		if s, ok := p.Symbols[name]; ok {
			s.Value = vm.NewValue(placeholder.Rtype)
		}
	} else {
		typ.Name = in[0].Str
		p.SymAdd(symbol.UnsetAddr, name, vm.NewValue(typ.Rtype), symbol.Type, typ)
	}
	return out, err
}

func (p *Parser) parseVar(in Tokens) (out Tokens, err error) {
	lines, err := p.varLines(in)
	if err != nil {
		return out, err
	}
	for _, lt := range lines {
		if lt, err = p.parseVarLine(lt); err != nil {
			return out, err
		}
		out = append(out, lt...)
	}
	return out, err
}

// zeroInitLocals emits Assign tokens that zero-initialize typed local variables.
// Each var gets [Ident(var), Ident(type), Assign(1)] so the compiler emits New+Set.
func (p *Parser) zeroInitLocals(vars []string, types []*vm.Type) (out Tokens) {
	for i, v := range vars {
		typ := types[i]
		typName := typ.Name
		if typName == "" {
			typName = typ.Rtype.String()
		}
		// Resolve type symbol key, honouring scope (e.g. "f/T" vs global "T").
		typKey := typName
		if sym, sc, ok := p.Symbols.Get(typName, p.scope); ok && sym.Kind == symbol.Type {
			if sc != "" {
				typKey = sc + "/" + typName
			}
		} else if !ok {
			// Anonymous type not yet in the symbol table; register it globally now.
			p.SymAdd(symbol.UnsetAddr, typKey, vm.NewValue(typ.Rtype), symbol.Type, typ)
		}
		out = append(out, newIdent(v, 0))
		out = append(out, newIdent(typKey, 0))
		out = append(out, newToken(lang.Assign, "", 0, 1))
	}
	return out
}

func (p *Parser) parseVarLine(in Tokens) (out Tokens, err error) {
	decl := in
	var assign Tokens
	if i := decl.Index(lang.Assign); i >= 0 {
		assign = decl[i+1:]
		decl = decl[:i]
	}
	var vars []string
	var types []*vm.Type
	var undefinedType bool
	if types, vars, _, err = p.parseParamTypes(decl, parseTypeVar); err != nil {
		if errors.Is(err, ErrMissingType) {
			undefinedType = true
			for _, lt := range decl.Split(lang.Comma) {
				rawName := lt[0].Str
				if rawName == "_" {
					rawName = p.blankName()
				}
				name := p.scopedName(rawName)
				vars = append(vars, name)
				if p.funcScope == "" {
					if s, _, ok := p.Symbols.Get(lt[0].Str, p.scope); !ok || s.Index == symbol.UnsetAddr {
						p.SymAdd(symbol.UnsetAddr, name, nilValue, symbol.Var, nil)
					}
					continue
				}
				p.SymAdd(p.framelen[p.funcScope], name, nilValue, symbol.LocalVar, nil)
				p.framelen[p.funcScope]++
			}
		} else {
			return out, err
		}
	}
	values := assign.Split(lang.Comma)
	if len(values) == 1 {
		if len(values[0]) == 0 {
			// No initializer: emit zero-init for typed local vars.
			if !undefinedType && p.funcScope != "" {
				out = append(out, p.zeroInitLocals(vars, types)...)
			}
			return out, err
		}
		for _, v := range vars {
			out = append(out, newIdent(v, 0))
		}
		toks, err := p.parseExpr(values[0], "")
		if err != nil {
			return out, err
		}
		out = append(out, toks...)
		if undefinedType {
			out = append(out, newToken(lang.Define, "", 0, len(vars)))
		} else {
			out = append(out, newToken(lang.Assign, "", 0, len(vars)))
		}
		return out, err
	}
	for i, v := range values {
		if v, err = p.parseExpr(v, ""); err != nil {
			return out, err
		}
		out = append(out, newIdent(vars[i], 0))
		out = append(out, v...)
		if undefinedType {
			out = append(out, newToken(lang.Define, "", 0, 1))
		} else {
			out = append(out, newToken(lang.Assign, "", 0, 1))
		}
	}
	return out, err
}
