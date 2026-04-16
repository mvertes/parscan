package goparser

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/mvertes/parscan/lang"
	"github.com/mvertes/parscan/scan"
	"github.com/mvertes/parscan/symbol"
	"github.com/mvertes/parscan/vm"
)

// typeParam represents a single generic type parameter.
type typeParam struct {
	name       string // e.g. "T", "K", "V"
	constraint string // e.g. "any", "comparable", or an interface name
}

// genericTemplate stores a generic function or type definition.
type genericTemplate struct {
	name       string      // original name (e.g. "Max", "Set")
	typeParams []typeParam // ordered type parameter list
	rawTokens  Tokens      // entire declaration tokens (func or type)
	isFunc     bool        // true for generic functions, false for generic types
}

func (p *Parser) parseTypeParamList(bt scan.Token) ([]typeParam, error) {
	toks, err := p.ScanBlock(bt, false)
	if err != nil {
		return nil, err
	}
	var params []typeParam
	for _, seg := range toks.Split(lang.Comma) {
		if len(seg) == 0 {
			continue
		}
		if len(seg) < 2 || seg[0].Tok != lang.Ident {
			return nil, ErrSyntax
		}
		// Disambiguate from array size expressions like [N + 1].
		if seg[1].Tok != lang.Ident && seg[1].Tok != lang.Interface {
			return nil, ErrSyntax
		}
		// Constraint is everything after the param name. For simple constraints
		// like "any" or "comparable", this is a single ident. Collect as string.
		var parts []string
		for _, t := range seg[1:] {
			parts = append(parts, t.Str)
		}
		params = append(params, typeParam{
			name:       seg[0].Str,
			constraint: strings.Join(parts, ""),
		})
	}
	if len(params) == 0 {
		return nil, ErrSyntax
	}
	return params, nil
}

func (p *Parser) checkConstraints(tmpl *genericTemplate, typeArgs []*vm.Type) error {
	for i, tp := range tmpl.typeParams {
		if err := p.checkOneConstraint(tp.constraint, typeArgs[i]); err != nil {
			return err
		}
	}
	return nil
}

// checkOneConstraint checks whether arg satisfies constraint.
// Unknown constraint forms (union types, ~T) silently pass.
func (p *Parser) checkOneConstraint(constraint string, arg *vm.Type) error {
	switch constraint {
	case "any", "interface{}":
		return nil
	case "comparable":
		if !arg.Rtype.Comparable() {
			return fmt.Errorf("type %s does not satisfy constraint comparable", arg.Rtype)
		}
		return nil
	}

	ifaceType := p.resolveConstraintType(constraint)
	if ifaceType == nil || !ifaceType.IsInterface() {
		return nil
	}
	if !arg.Rtype.Implements(ifaceType.Rtype) {
		return fmt.Errorf("type %s does not satisfy constraint %s", arg.Rtype, constraint)
	}
	return nil
}

// resolveConstraintType resolves a constraint name to a type.
// Handles both unqualified ("error") and package-qualified ("fmt.Stringer") names.
func (p *Parser) resolveConstraintType(name string) *vm.Type {
	if s, _, ok := p.Symbols.Get(name, p.scope); ok && s.Kind == symbol.Type && s.Type != nil {
		return s.Type
	}
	idx := strings.Index(name, ".")
	if idx <= 0 {
		return nil
	}
	ps, _, ok := p.Symbols.Get(name[:idx], p.scope)
	if !ok || ps.Kind != symbol.Pkg {
		return nil
	}
	typ, err := p.resolvePkgType(ps, name[idx+1:])
	if err != nil {
		return nil
	}
	return typ
}

// resolveTypeArgs parses the contents of a bracket block as concrete type arguments.
// E.g. "[int, string]" -> []*vm.Type{intType, stringType}.
func (p *Parser) resolveTypeArgs(bt scan.Token) ([]*vm.Type, error) {
	toks, err := p.ScanBlock(bt, false)
	if err != nil {
		return nil, err
	}
	var types []*vm.Type
	for _, seg := range toks.Split(lang.Comma) {
		if len(seg) == 0 {
			continue
		}
		typ, _, err := p.parseTypeExpr(seg)
		if err != nil {
			return nil, err
		}
		types = append(types, typ)
	}
	return types, nil
}

// isSimpleIdent reports whether s is a plain Go identifier (letters, digits, underscore).
func isSimpleIdent(s string) bool {
	for _, r := range s {
		if r != '_' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return len(s) > 0
}

// typeArgName returns the source-level name for a concrete type argument.
// For pointer types, PointerTo stores just the element name (e.g. "int"
// for *int), so we prepend "*" to produce the correct name.
func typeArgName(t *vm.Type) string {
	name := t.Name
	if name == "" {
		return t.Rtype.String()
	}
	if t.IsPtr() {
		return "*" + name
	}
	return name
}

// mangledName returns the mangled name for a generic instantiation.
// E.g. mangledName("Max", [int]) -> "Max#int".
func mangledName(base string, typeArgs []*vm.Type) string {
	var sb strings.Builder
	sb.WriteString(base)
	for _, t := range typeArgs {
		sb.WriteByte('#')
		sb.WriteString(typeArgName(t))
	}
	return sb.String()
}

// instantiate creates a concrete (monomorphized) version of a generic template
// by substituting type parameter names with concrete type names in the token stream.
// It returns the rewritten tokens and the mangled name.
func (p *Parser) instantiate(tmpl *genericTemplate, typeArgs []*vm.Type) (Tokens, string, error) {
	if len(typeArgs) != len(tmpl.typeParams) {
		return nil, "", ErrSyntax
	}

	mname := mangledName(tmpl.name, typeArgs)
	if s, _, ok := p.Symbols.Get(mname, ""); ok && s.Type != nil {
		return nil, mname, nil // Already instantiated.
	}

	if err := p.checkConstraints(tmpl, typeArgs); err != nil {
		return nil, "", err
	}

	// Build substitution map: type param name -> concrete type name string.
	sub := make(map[string]string, len(tmpl.typeParams))
	for i, tp := range tmpl.typeParams {
		sub[tp.name] = typeArgName(typeArgs[i])
	}

	// Token index offset: func tokens have a leading `func` keyword.
	offset := 0
	if tmpl.isFunc {
		offset = 1
	}

	raw := tmpl.rawTokens
	out := make(Tokens, 0, len(raw))
	for i, t := range raw {
		t2 := t // shallow copy

		switch {
		case i == offset && t.Tok == lang.Ident && t.Str == tmpl.name:
			t2.Str = mname
		case i == offset+1 && t.Tok == lang.BracketBlock:
			continue
		case t.Tok == lang.Ident:
			if repl, ok := sub[t.Str]; ok {
				// Compound types (e.g. "*int", "[]byte", "map[K]V")
				// must be re-scanned into proper tokens.
				if !isSimpleIdent(repl) {
					expanded, err := p.Scanner.Scan(repl, false)
					if err == nil && len(expanded) > 0 {
						for _, et := range expanded {
							et.Pos = t.Pos
							out = append(out, Token{Token: et})
						}
						continue
					}
				}
				t2.Str = repl
			}
		}

		if t.Tok.IsBlock() {
			t2.Str = p.substituteBlock(t.Str, sub)
		}

		out = append(out, t2)
	}
	return out, mname, nil
}

// ensureTypeInstantiated resolves type arguments from a bracket block and
// instantiates the generic type template, registering the concrete type.
func (p *Parser) ensureTypeInstantiated(tmpl *genericTemplate, bt scan.Token) (string, error) {
	typeArgs, err := p.resolveTypeArgs(bt)
	if err != nil {
		return "", err
	}
	instToks, mname, err := p.instantiate(tmpl, typeArgs)
	if err != nil {
		return "", err
	}
	if instToks != nil {
		savedScope := p.scope
		p.scope = ""
		_, err = p.parseTypeLine(instToks)
		p.scope = savedScope
		if err != nil {
			return "", err
		}
	}
	return mname, nil
}

// inferTypeArgs infers concrete type arguments for a generic function call
// by examining the call argument expressions and matching them against the
// template's type parameters through the function signature (stored in genSym.Type).
func (p *Parser) inferTypeArgs(tmpl *genericTemplate, genSym *symbol.Symbol, callArgs scan.Token) ([]*vm.Type, error) {
	argToks, err := p.ScanBlock(callArgs, false)
	if err != nil {
		return nil, err
	}
	args := argToks.Split(lang.Comma)

	// Build set of type parameter names.
	tpNames := make(map[string]bool, len(tmpl.typeParams))
	for _, tp := range tmpl.typeParams {
		tpNames[tp.name] = true
	}

	// Match each argument to the corresponding parameter type from the parsed signature.
	// If the parameter type name is a type parameter, infer it from the argument.
	params := genSym.Type.Params
	inferred := make(map[string]*vm.Type, len(tmpl.typeParams))
	for i, argExpr := range args {
		if len(argExpr) == 0 || i >= len(params) {
			continue
		}
		pName := params[i].Name
		if !tpNames[pName] {
			continue
		}
		if _, done := inferred[pName]; done {
			continue
		}
		typ := p.inferExprType(argExpr)
		if typ == nil {
			return nil, fmt.Errorf("cannot infer type for argument %d", i)
		}
		inferred[pName] = typ
	}

	// Build ordered type args matching tmpl.typeParams.
	typeArgs := make([]*vm.Type, len(tmpl.typeParams))
	for i, tp := range tmpl.typeParams {
		t, ok := inferred[tp.name]
		if !ok {
			return nil, fmt.Errorf("cannot infer type parameter %s", tp.name)
		}
		typeArgs[i] = t
	}
	return typeArgs, nil
}

// inferExprType determines the type of an infix token expression by first
// parsing it into postfix form (reusing parseExpr), then walking the postfix
// tokens right-to-left following the same pattern as evalConstExpr.
func (p *Parser) inferExprType(toks Tokens) *vm.Type {
	postfix, err := p.parseExpr(toks, "")
	if err != nil || len(postfix) == 0 {
		return nil
	}
	typ, _ := p.postfixType(postfix)
	return typ
}

// postfixType walks postfix tokens right-to-left (like evalConstExpr) and
// returns the result type and the number of tokens consumed.
func (p *Parser) postfixType(in Tokens) (*vm.Type, int) {
	l := len(in) - 1
	if l < 0 {
		return nil, 0
	}
	t := in[l]
	id := t.Tok

	switch {
	case id == lang.Period:
		// Field selector: result type is the field type.
		leftTyp, ln := p.postfixType(in[:l])
		if leftTyp == nil {
			return nil, 0
		}
		fieldName := t.Str[1:] // Strip leading ".".
		if ft := leftTyp.FieldType(fieldName); ft != nil {
			return ft, 1 + ln
		}
		// Method: look up in symbol table.
		if ms, _ := p.Symbols.MethodByName(&symbol.Symbol{Kind: symbol.Type, Name: leftTyp.Name, Type: leftTyp}, fieldName); ms != nil {
			return ms.Type, 1 + ln
		}
		return nil, 0

	case id.IsBinaryOp():
		typ2, l2 := p.postfixType(in[:l])
		typ1, l1 := p.postfixType(in[:l-l2])
		if id.IsBoolOp() {
			return p.Symbols["bool"].Type, 1 + l1 + l2
		}
		// Arithmetic / bitwise: result type follows from operands.
		if typ1 != nil {
			return typ1, 1 + l1 + l2
		}
		return typ2, 1 + l1 + l2

	case id.IsUnaryOp():
		inner, ln := p.postfixType(in[:l])
		if inner == nil {
			return nil, 0
		}
		switch id {
		case lang.Not:
			return p.Symbols["bool"].Type, 1 + ln
		case lang.Addr:
			return vm.PointerTo(inner), 1 + ln
		case lang.Deref:
			if inner.Rtype.Kind() == reflect.Pointer {
				return inner.Elem(), 1 + ln
			}
		case lang.Arrow:
			if inner.Rtype.Kind() == reflect.Chan {
				return inner.Elem(), 1 + ln
			}
		}
		return inner, 1 + ln

	case id.IsLiteral():
		switch id {
		case lang.Int:
			return p.Symbols["int"].Type, 1
		case lang.Float:
			return p.Symbols["float64"].Type, 1
		case lang.String:
			return p.Symbols["string"].Type, 1
		case lang.Char:
			return p.Symbols["int32"].Type, 1
		}
		return nil, 1

	case id == lang.Ident:
		s, _, ok := p.Symbols.Get(t.Str, p.scope)
		if !ok {
			return nil, 1
		}
		return symbol.Vtype(s), 1

	case id == lang.Call:
		narg := t.Arg[0].(int)
		rest := in[:l]
		totalLen := 1
		for range narg {
			_, al := p.postfixType(rest)
			if al == 0 {
				return nil, 0
			}
			totalLen += al
			rest = rest[:len(rest)-al]
		}
		// The function/type token precedes the arguments.
		if len(rest) == 0 {
			return nil, 0
		}
		fnTok := rest[len(rest)-1]
		totalLen++
		if fnTok.Tok == lang.Ident {
			s, _, ok := p.Symbols.Get(fnTok.Str, p.scope)
			if !ok {
				return nil, 0
			}
			if s.Kind == symbol.Type {
				return s.Type, totalLen
			}
			if s.Type != nil {
				return funcReturnType(s.Type), totalLen
			}
		}
		return nil, totalLen

	case id == lang.Index:
		_, il := p.postfixType(in[:l]) // index expression
		containerTyp, cl := p.postfixType(in[:l-il])
		if containerTyp == nil {
			return nil, 0
		}
		switch containerTyp.Rtype.Kind() {
		case reflect.Slice, reflect.Array, reflect.Map:
			return containerTyp.Elem(), 1 + il + cl
		case reflect.String:
			return p.Symbols["uint8"].Type, 1 + il + cl
		}
		return nil, 0

	case id == lang.TypeAssert:
		// Type assertion: x.(T). The asserted type is stored in Arg[1].
		_, el := p.postfixType(in[:l]) // consume the expression being asserted
		if typ, ok := t.Arg[1].(*vm.Type); ok {
			return typ, 1 + el
		}
		return nil, 0

	case id == lang.Composite:
		// Composite literal: type is encoded in the token.
		typeName := t.Str
		if s, _, ok := p.Symbols.Get(typeName, p.scope); ok && s.Type != nil {
			return s.Type, 1
		}
		return nil, 1
	}
	return nil, 0
}

// funcReturnType returns the first return type of a function type.
func funcReturnType(typ *vm.Type) *vm.Type {
	if len(typ.Returns) > 0 {
		return typ.Returns[0]
	}
	if typ.Rtype.Kind() == reflect.Func && typ.Rtype.NumOut() > 0 {
		out := typ.Rtype.Out(0)
		return &vm.Type{Name: out.Name(), Rtype: out}
	}
	return nil
}

func (p *Parser) substituteBlock(s string, sub map[string]string) string {
	toks, err := p.Scanner.Scan(s, false)
	if err != nil || len(toks) == 0 {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s))
	prev := 0
	for _, t := range toks {
		switch {
		case t.Tok == lang.Ident:
			if repl, ok := sub[t.Str]; ok {
				sb.WriteString(s[prev:t.Pos])
				sb.WriteString(repl)
				prev = t.Pos + len(t.Str)
			}
		case t.Tok.IsBlock():
			inner := t.Block()
			newInner := p.substituteBlock(inner, sub)
			if newInner != inner {
				sb.WriteString(s[prev : t.Pos+t.Beg])
				sb.WriteString(newInner)
				prev = t.Pos + len(t.Str) - t.End
			}
		}
	}
	if prev == 0 {
		return s
	}
	sb.WriteString(s[prev:])
	return sb.String()
}
