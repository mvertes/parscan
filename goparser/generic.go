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

// elemKind classifies a single constraint element.
type elemKind int

const (
	elemAny          elemKind = iota // any / interface{}
	elemComparable                   // built-in comparable
	elemExact                        // arg.Rtype must equal typ.Rtype
	elemInterface                    // arg must Implement typ (method-set interface)
	elemApprox                       // ~T: arg.Rtype.Kind() must match typ.Rtype.Kind()
	elemTypeParamRef                 // arg must equal typeArgs[paramRef]
)

// constraintElem is one leaf of a constraint's disjunction.
type constraintElem struct {
	kind     elemKind
	typ      *vm.Type // Exact, Interface, Approx
	paramRef int      // TypeParamRef
}

// tpConstraint is a resolved generic type-parameter constraint. An argument
// satisfies the constraint if it matches any element in elems — a flat
// disjunction. Nested unions (including those embedded inside constraint
// interfaces like cmp.Ordered) are flattened at resolution time.
type tpConstraint struct {
	elems []constraintElem
	pos   int // byte offset of the first constraint token; resolved via p.Sources at error time
}

// typeParam represents a single generic type parameter.
type typeParam struct {
	name       string       // e.g. "T", "K", "V"
	constraint tpConstraint // resolved constraint (kind + payload)
}

// genericTemplate stores a generic function or type definition.
type genericTemplate struct {
	name       string             // original name (e.g. "Max", "Set")
	typeParams []typeParam        // ordered type parameter list
	rawTokens  Tokens             // entire declaration tokens (func or type)
	isFunc     bool               // true for generic functions, false for generic types
	methods    []*genericTemplate // methods defined on this generic type
}

func (p *Parser) parseTypeParamList(bt scan.Token) ([]typeParam, error) {
	toks, err := p.ScanBlock(bt, false)
	if err != nil {
		return nil, err
	}
	type rawPar struct {
		name  string
		ctoks Tokens
	}
	var raws []rawPar
	for _, seg := range toks.Split(lang.Comma) {
		if len(seg) == 0 {
			continue
		}
		if seg[0].Tok != lang.Ident {
			return nil, ErrSyntax
		}
		if len(seg) == 1 {
			// Bare ident shares the constraint with the next segment.
			// Go syntax: [K, V any] means K any, V any.
			raws = append(raws, rawPar{name: seg[0].Str})
			continue
		}
		// Disambiguate from array size expressions like [N + 1].
		if seg[1].Tok != lang.Ident && seg[1].Tok != lang.Interface && seg[1].Tok != lang.Tilde {
			return nil, ErrSyntax
		}
		raws = append(raws, rawPar{name: seg[0].Str, ctoks: seg[1:]})
	}
	if len(raws) == 0 {
		return nil, ErrSyntax
	}
	// The last param must have an explicit constraint. A bare ident like [d]
	// is not a valid type parameter list (it's an array size expression).
	if raws[len(raws)-1].ctoks == nil {
		return nil, ErrSyntax
	}
	// Propagate constraints backwards for shared-constraint syntax: [K, V any].
	for i := len(raws) - 2; i >= 0; i-- {
		if raws[i].ctoks == nil {
			raws[i].ctoks = raws[i+1].ctoks
		}
	}

	// Build type-param index so constraints referencing other params resolve
	// to a TypeParamRef rather than attempting to lookup the name as a type.
	tpIdx := make(map[string]int, len(raws))
	for i, r := range raws {
		tpIdx[r.name] = i
	}

	// Temporarily install placeholders for each type-param name so that
	// parseTypeExpr can resolve references to them inside composite
	// constraints like "~[]E". Restore the prior symbol on exit.
	saved := make(map[string]*symbol.Symbol, len(raws))
	for _, r := range raws {
		saved[r.name] = p.Symbols[r.name]
		p.Symbols[r.name] = &symbol.Symbol{
			Kind: symbol.Type, Name: r.name,
			Type: &vm.Type{Name: r.name, Rtype: vm.AnyRtype},
		}
	}
	defer func() {
		for k, v := range saved {
			if v == nil {
				delete(p.Symbols, k)
			} else {
				p.Symbols[k] = v
			}
		}
	}()

	params := make([]typeParam, len(raws))
	for i, r := range raws {
		c, err := p.resolveConstraint(r.ctoks, tpIdx)
		if err != nil {
			return nil, err
		}
		params[i] = typeParam{name: r.name, constraint: c}
	}
	return params, nil
}

func (p *Parser) checkConstraints(tmpl *genericTemplate, typeArgs []*vm.Type) error {
	for i, tp := range tmpl.typeParams {
		if err := p.checkConstraint(tp.constraint, typeArgs[i], typeArgs); err != nil {
			return err
		}
	}
	return nil
}

func (p *Parser) constraintError(c tpConstraint, arg *vm.Type) error {
	if loc := p.Sources.FormatPos(c.pos); loc != "" {
		return fmt.Errorf("type %s does not satisfy constraint (%s)", arg.Rtype, loc)
	}
	return fmt.Errorf("type %s does not satisfy constraint", arg.Rtype)
}

// checkConstraint passes if any element in c.elems matches arg. typeArgs
// carries the full set of concrete type arguments for the current
// instantiation so that a TypeParamRef element can resolve to its target.
func (p *Parser) checkConstraint(c tpConstraint, arg *vm.Type, typeArgs []*vm.Type) error {
	for _, e := range c.elems {
		if checkConstraintElem(e, arg, typeArgs) {
			return nil
		}
	}
	return p.constraintError(c, arg)
}

// checkConstraintElem reports whether arg satisfies a single constraint element.
// For Approx with composite kinds (slice, map, array, …), only Kind is checked
// — tightening would require inter-param substitution.
func checkConstraintElem(e constraintElem, arg *vm.Type, typeArgs []*vm.Type) bool {
	switch e.kind {
	case elemAny:
		return true
	case elemComparable:
		return arg.Rtype.Comparable()
	case elemExact:
		return e.typ == nil || arg.Rtype == e.typ.Rtype
	case elemInterface:
		// Parscan-parsed interfaces have Rtype=any so Implements is trivially
		// true — acceptable because their type-element form is already flattened
		// into sibling elems at resolution time.
		return e.typ == nil || arg.Rtype.Implements(e.typ.Rtype)
	case elemApprox:
		return e.typ != nil && arg.Rtype.Kind() == e.typ.Rtype.Kind()
	case elemTypeParamRef:
		if e.paramRef < 0 || e.paramRef >= len(typeArgs) {
			return true
		}
		return arg.Rtype == typeArgs[e.paramRef].Rtype
	}
	return false
}

// resolveConstraint turns raw constraint tokens into a resolved constraint.
// tpIdx maps names of type parameters in the enclosing list to their index so
// that e.g. "~[]E" with "E" another type param resolves correctly.
func (p *Parser) resolveConstraint(toks Tokens, tpIdx map[string]int) (tpConstraint, error) {
	elems, err := p.resolveConstraintElems(toks, tpIdx)
	if err != nil {
		return tpConstraint{}, err
	}
	pos := 0
	if len(toks) > 0 {
		pos = toks[0].Pos
	}
	return tpConstraint{elems: elems, pos: pos}, nil
}

// resolveConstraintElems returns the flat disjunction of leaf elements that
// satisfy the constraint expressed by toks. Nested unions — including those
// embedded inside constraint interfaces like cmp.Ordered — are flattened.
func (p *Parser) resolveConstraintElems(toks Tokens, tpIdx map[string]int) ([]constraintElem, error) {
	if len(toks) == 0 {
		return nil, fmt.Errorf("%w: empty constraint", ErrSyntax)
	}

	// Top-level union "A | B | C": concatenate each side's elements.
	if toks.Index(lang.Or) >= 0 {
		var out []constraintElem
		for _, seg := range toks.Split(lang.Or) {
			es, err := p.resolveConstraintElems(seg, tpIdx)
			if err != nil {
				return nil, err
			}
			out = append(out, es...)
		}
		return out, nil
	}

	// Approximate "~T": T must be a concrete type (single elemExact).
	if toks[0].Tok == lang.Tilde {
		inner, err := p.resolveConstraintElems(toks[1:], tpIdx)
		if err != nil {
			return nil, err
		}
		if len(inner) != 1 || inner[0].kind != elemExact {
			loc := p.Sources.FormatPos(toks[0].Pos)
			if loc == "" {
				return nil, fmt.Errorf("%w: ~ must prefix a type", ErrSyntax)
			}
			return nil, fmt.Errorf("%w: ~ must prefix a type (%s)", ErrSyntax, loc)
		}
		return []constraintElem{{kind: elemApprox, typ: inner[0].typ}}, nil
	}

	// Well-known identifier or type-param reference.
	if len(toks) == 1 && toks[0].Tok == lang.Ident {
		switch toks[0].Str {
		case "any":
			return []constraintElem{{kind: elemAny}}, nil
		case "comparable":
			return []constraintElem{{kind: elemComparable}}, nil
		}
		if idx, ok := tpIdx[toks[0].Str]; ok {
			return []constraintElem{{kind: elemTypeParamRef, paramRef: idx}}, nil
		}
	}

	// Type expression. A constraint interface with type elements (e.g.
	// cmp.Ordered) contributes one elem per member.
	typ, _, err := p.parseTypeExpr(toks)
	if err != nil {
		return nil, err
	}
	if typ.IsInterface() {
		if len(typ.TypeElems) > 0 {
			out := make([]constraintElem, len(typ.TypeElems))
			for i, e := range typ.TypeElems {
				kind := elemExact
				if e.Approx {
					kind = elemApprox
				}
				out[i] = constraintElem{kind: kind, typ: e.Type}
			}
			return out, nil
		}
		return []constraintElem{{kind: elemInterface, typ: typ}}, nil
	}
	return []constraintElem{{kind: elemExact, typ: typ}}, nil
}

// resolveTypeArgs parses the contents of a bracket block as concrete type arguments.
// E.g. "[int, string]" -> []*vm.Type{intType, stringType}.
// The second return value carries the source-level text of each segment
// (e.g. "netip.Prefix"), preserving package qualifiers lost in *vm.Type.Name.
func (p *Parser) resolveTypeArgs(bt scan.Token) ([]*vm.Type, []string, error) {
	toks, err := p.ScanBlock(bt, false)
	if err != nil {
		return nil, nil, err
	}
	var types []*vm.Type
	var sources []string
	for _, seg := range toks.Split(lang.Comma) {
		if len(seg) == 0 {
			continue
		}
		typ, _, err := p.parseTypeExpr(seg)
		if err != nil {
			return nil, nil, err
		}
		types = append(types, typ)
		sources = append(sources, tokensSource(seg))
	}
	return types, sources, nil
}

// tokensSource reconstructs the original source text from tokens; used to
// preserve package qualifiers (e.g. "netip.Prefix") that *vm.Type.Name drops.
func tokensSource(toks Tokens) string {
	if len(toks) == 1 {
		return toks[0].Str
	}
	var sb strings.Builder
	for _, t := range toks {
		sb.WriteString(t.Str)
	}
	return sb.String()
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

// typeArgSubst returns the text used to substitute a type parameter in the
// template body. Prefers source (preserves package qualifiers like
// "netip.Prefix"); falls back to typeArgName when source is empty.
func typeArgSubst(t *vm.Type, source string) string {
	if source != "" {
		return source
	}
	return typeArgName(t)
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

// substituteTokens rewrites tokens by replacing type parameter identifiers
// with concrete type names and substituting inside block strings.
// Struct body BraceBlocks get field-name-aware substitution to avoid
// renaming field names that shadow type parameters.
func (p *Parser) substituteTokens(raw Tokens, sub map[string]string) Tokens {
	out := make(Tokens, 0, len(raw))
	for i, t := range raw {
		t2 := t // shallow copy

		if t.Tok == lang.Ident {
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
			if t.Tok == lang.BraceBlock && i > 0 && raw[i-1].Tok == lang.Struct {
				t2.Str = p.substituteStructBody(t.Str, sub)
			} else {
				t2.Str = p.substituteBlock(t.Str, sub)
			}
		}

		out = append(out, t2)
	}
	return out
}

// instantiate creates a concrete (monomorphized) version of a generic template
// by substituting type parameter names with concrete type names in the token stream.
// It returns the rewritten tokens and the mangled name.
// typeArgSources, when non-nil, provides source-level text for each type
// argument (e.g. "netip.Prefix") so package qualifiers are preserved in the
// substituted body. If nil, the short Name from *vm.Type is used.
func (p *Parser) instantiate(tmpl *genericTemplate, typeArgs []*vm.Type, typeArgSources []string) (Tokens, string, error) {
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
		var src string
		if i < len(typeArgSources) {
			src = typeArgSources[i]
		}
		sub[tp.name] = typeArgSubst(typeArgs[i], src)
	}

	out := p.substituteTokens(tmpl.rawTokens, sub)

	// Rename the declaration and remove the type parameter bracket block.
	// Token index offset: func tokens have a leading `func` keyword.
	offset := 0
	if tmpl.isFunc {
		offset = 1
	}
	for i := range out {
		if i == offset && out[i].Tok == lang.Ident && out[i].Str == tmpl.name {
			out[i].Str = mname
		}
	}
	// Remove the BracketBlock at offset+1.
	if offset+1 < len(out) && out[offset+1].Tok == lang.BracketBlock {
		out = append(out[:offset+1], out[offset+2:]...)
	}

	return out, mname, nil
}

// ensureTypeInstantiated resolves type arguments from a bracket block and
// instantiates the generic type template, registering the concrete type.
// Methods on the generic type are also instantiated; their compiled tokens
// are stored in p.pendingMethodDefs for later emission.
func (p *Parser) ensureTypeInstantiated(tmpl *genericTemplate, bt scan.Token) (string, error) {
	typeArgs, typeArgSources, err := p.resolveTypeArgs(bt)
	if err != nil {
		return "", err
	}
	instToks, mname, err := p.instantiate(tmpl, typeArgs, typeArgSources)
	if err != nil {
		return "", err
	}
	if instToks != nil {
		savedScope := p.scope
		p.scope = ""
		_, err = p.parseTypeLine(instToks)
		if err != nil {
			p.scope = savedScope
			return "", err
		}
		// Instantiate methods on this generic type.
		for _, methTmpl := range tmpl.methods {
			methToks, err := p.instantiateMethod(tmpl, methTmpl, typeArgs, typeArgSources)
			if err != nil {
				p.scope = savedScope
				return "", err
			}
			if methToks == nil {
				continue // Already instantiated.
			}
			if _, err := p.registerFunc(methToks); err != nil {
				p.scope = savedScope
				return "", err
			}
			fout, err := p.parseFunc(methToks)
			if err != nil {
				p.scope = savedScope
				return "", err
			}
			p.pendingMethodDefs = append(p.pendingMethodDefs, fout...)
		}
		p.scope = savedScope
	}
	return mname, nil
}

// recvGenericBaseName checks whether the receiver tokens contain type
// parameters (a BracketBlock). If so, it returns the base type name
// (the Ident immediately preceding the BracketBlock).
func recvGenericBaseName(recvr Tokens) (string, bool) {
	for i, t := range recvr {
		if t.Tok == lang.BracketBlock && i > 0 && recvr[i-1].Tok == lang.Ident {
			return recvr[i-1].Str, true
		}
	}
	return "", false
}

// instantiateMethod creates a concrete version of a generic method template
// by substituting type parameter names with concrete types in the token stream.
// The receiver block is rewritten from e.g. (b Box[T]) to (b Box#int).
// Returns nil if the method is already instantiated.
func (p *Parser) instantiateMethod(typeTmpl, methTmpl *genericTemplate, typeArgs []*vm.Type, typeArgSources []string) (Tokens, error) {
	mTypeName := mangledName(typeTmpl.name, typeArgs)
	methFullName := mTypeName + "." + methTmpl.name

	// Guard: already instantiated.
	if _, _, ok := p.Symbols.Get(methFullName, ""); ok {
		return nil, nil
	}

	sub := make(map[string]string, len(typeTmpl.typeParams))
	for i, tp := range typeTmpl.typeParams {
		var src string
		if i < len(typeArgSources) {
			src = typeArgSources[i]
		}
		sub[tp.name] = typeArgSubst(typeArgs[i], src)
	}

	out := p.substituteTokens(methTmpl.rawTokens, sub)

	// Collapse TypeName[Args] into the mangled name in the receiver ParenBlock
	// (the first ParenBlock, at index 1 after the func keyword).
	if len(out) > 1 && out[1].Tok == lang.ParenBlock {
		out[1].Str = p.stripRecvTypeParams(out[1].Str, typeTmpl.name, mTypeName)
	}

	return out, nil
}

// stripRecvTypeParams rewrites a receiver block string by replacing
// TypeName[...] with the mangled name. For example:
//
//	"(b Box[int])"   -> "(b Box#int)"
//	"(b *Box[int])"  -> "(b *Box#int)"
func (p *Parser) stripRecvTypeParams(blockStr, origName, mangledName string) string {
	// Scan the full block string — expect a single ParenBlock.
	outerToks, err := p.Scanner.Scan(blockStr, false)
	if err != nil || len(outerToks) != 1 || outerToks[0].Tok != lang.ParenBlock {
		return blockStr
	}

	paren := outerToks[0]
	inner := paren.Block()

	innerToks, err := p.Scanner.Scan(inner, false)
	if err != nil {
		return blockStr
	}

	// Find origName Ident followed by BracketBlock and replace.
	var sb strings.Builder
	prev := 0
	for i, t := range innerToks {
		if t.Tok == lang.Ident && t.Str == origName && i+1 < len(innerToks) && innerToks[i+1].Tok == lang.BracketBlock {
			sb.WriteString(inner[prev:t.Pos])
			sb.WriteString(mangledName)
			bracketTok := innerToks[i+1]
			prev = bracketTok.Pos + len(bracketTok.Str)
		}
	}
	if prev == 0 {
		return blockStr // No change needed.
	}
	sb.WriteString(inner[prev:])

	// Reconstruct with outer parens.
	return blockStr[:paren.Beg] + sb.String() + blockStr[len(blockStr)-paren.End:]
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

	// Generic templates whose signature failed to parse cleanly leave Type nil;
	// surface an inference error rather than nil-deref.
	if genSym.Type == nil {
		return nil, fmt.Errorf("cannot infer type parameters for %s: signature unresolved", tmpl.name)
	}

	// Match each argument to the corresponding parameter type from the parsed signature.
	// If the parameter type name is a type parameter, infer it from the argument.
	params := genSym.Type.Params
	isVariadic := genSym.Type.Rtype.IsVariadic() && len(params) > 0
	inferred := make(map[string]*vm.Type, len(tmpl.typeParams))
	for i, argExpr := range args {
		if len(argExpr) == 0 {
			continue
		}
		var pType *vm.Type
		switch {
		case i < len(params)-1, !isVariadic && i < len(params):
			pType = params[i]
		case isVariadic:
			// Variadic slot: the argument is a single element, not the whole
			// slice — descend into ElemType so unification sees `T`, not `[]T`.
			pType = params[len(params)-1]
			if pType.ElemType != nil {
				pType = pType.ElemType
			}
		default:
			continue
		}
		// Skip when every type param in pType is already bound: avoids a
		// redundant inferExprType (which can fail legitimately on later
		// args — e.g. slices.Equal's second []E with a shape inferExprType
		// can't currently type).
		if !hasUnboundTypeParam(pType, tpNames, inferred) {
			continue
		}
		argTyp := p.inferExprType(argExpr)
		if argTyp == nil {
			return nil, fmt.Errorf("cannot infer type for argument %d", i)
		}
		unifyTypeParam(pType, argTyp, tpNames, inferred)
	}

	// Second pass: for type parameters that never appear directly as a
	// parameter type (e.g. E in Equal[S ~[]E, E comparable](s1, s2 S)),
	// unpack any sibling's composite approx-constraint (~[]E, ~map[K]V)
	// against its inferred concrete type. Iterated to a fixed point so
	// that chains like [A ~[]B, B ~[]C, C any] resolve.
	for progress := len(inferred) < len(tmpl.typeParams); progress; {
		progress = false
		for _, tp := range tmpl.typeParams {
			if _, done := inferred[tp.name]; done {
				continue
			}
			for _, other := range tmpl.typeParams {
				if other.name == tp.name {
					continue
				}
				ot, ok := inferred[other.name]
				if !ok {
					continue
				}
				if t := unpackConstraint(other.constraint, tp.name, ot); t != nil {
					inferred[tp.name] = t
					progress = true
					break
				}
			}
		}
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

// hasUnboundTypeParam reports whether t mentions any type parameter in tpNames
// that isn't yet in inferred, at any depth (pointer/slice/array/chan/map).
func hasUnboundTypeParam(t *vm.Type, tpNames map[string]bool, inferred map[string]*vm.Type) bool {
	if t == nil {
		return false
	}
	switch t.Rtype.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Chan:
		return hasUnboundTypeParam(t.ElemType, tpNames, inferred)
	case reflect.Map:
		return hasUnboundTypeParam(t.KeyType, tpNames, inferred) || hasUnboundTypeParam(t.ElemType, tpNames, inferred)
	}
	if !tpNames[t.Name] {
		return false
	}
	_, ok := inferred[t.Name]
	return !ok
}

// unifyTypeParam walks pType (from a generic signature, possibly containing
// type-param idents) and argType in parallel, binding each encountered
// type-param ident to the corresponding sub-type of argType. Returns false
// if the shapes don't match. First-seen binding wins (no conflict checking).
func unifyTypeParam(pType, argType *vm.Type, tpNames map[string]bool, inferred map[string]*vm.Type) bool {
	if pType == nil || argType == nil {
		return false
	}
	// Recurse through composite constructors first: Name may be inherited from
	// the element (PointerTo propagates Name), so we must not leaf-match on
	// Name for a compound shape.
	switch pType.Rtype.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Chan:
		if argType.Rtype.Kind() != pType.Rtype.Kind() {
			return false
		}
		return unifyTypeParam(pType.ElemType, argType.ElemType, tpNames, inferred)
	case reflect.Map:
		if argType.Rtype.Kind() != reflect.Map {
			return false
		}
		if !unifyTypeParam(pType.KeyType, argType.KeyType, tpNames, inferred) {
			return false
		}
		return unifyTypeParam(pType.ElemType, argType.ElemType, tpNames, inferred)
	}
	// Leaf: bind if this is a type-param ident; otherwise a concrete leaf
	// with no binding to make.
	if tpNames[pType.Name] {
		if _, ok := inferred[pType.Name]; !ok {
			inferred[pType.Name] = argType
		}
	}
	return true
}

// unpackConstraint tries to extract a concrete type for paramName by matching
// the inferred concrete type against the shape of one of c's approx/exact
// constraint elements. Returns nil if no element pins paramName.
func unpackConstraint(c tpConstraint, paramName string, concrete *vm.Type) *vm.Type {
	for _, e := range c.elems {
		if (e.kind != elemApprox && e.kind != elemExact) || e.typ == nil {
			continue
		}
		if t := extractFromShape(e.typ, concrete, paramName); t != nil {
			return t
		}
	}
	return nil
}

// extractFromShape walks `shape` in parallel with `concrete`, returning the
// sub-type of concrete at the position where shape names paramName. E.g.
// shape=[]E, concrete=[]int, paramName=E → int. Handles slice, array,
// pointer, chan (via ElemType), map (via KeyType + ElemType), and func
// (via Params + Returns). Decomposes before matching by name so that
// composite shapes whose outer Name happens to collide with paramName
// (e.g. PointerTo sets Name=ElemName) don't short-circuit.
func extractFromShape(shape, concrete *vm.Type, paramName string) *vm.Type {
	if shape.Rtype.Kind() == concrete.Rtype.Kind() {
		switch shape.Rtype.Kind() {
		case reflect.Map:
			if shape.KeyType != nil {
				if t := extractFromShape(shape.KeyType, concrete.Key(), paramName); t != nil {
					return t
				}
			}
			if shape.ElemType != nil {
				if t := extractFromShape(shape.ElemType, concrete.Elem(), paramName); t != nil {
					return t
				}
			}
		case reflect.Func:
			for i, p := range shape.Params {
				if i >= len(concrete.Params) {
					break
				}
				if t := extractFromShape(p, concrete.Params[i], paramName); t != nil {
					return t
				}
			}
			for i, r := range shape.Returns {
				if i >= len(concrete.Returns) {
					break
				}
				if t := extractFromShape(r, concrete.Returns[i], paramName); t != nil {
					return t
				}
			}
		default:
			if shape.ElemType != nil {
				if t := extractFromShape(shape.ElemType, concrete.Elem(), paramName); t != nil {
					return t
				}
			}
		}
	}
	if shape.Name == paramName && shape.ElemType == nil && shape.KeyType == nil && len(shape.Params) == 0 && len(shape.Returns) == 0 {
		return concrete
	}
	return nil
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
		// Auto-dereference pointer types for field access (Go: s.F works for *T).
		structTyp := leftTyp
		if structTyp.Rtype.Kind() == reflect.Pointer {
			structTyp = structTyp.Elem()
		}
		if structTyp.Rtype.Kind() == reflect.Struct {
			if ft := structTyp.FieldType(fieldName); ft != nil {
				return ft, 1 + ln
			}
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

// substituteStructBody substitutes type parameters in a struct body BraceBlock,
// preserving field names that shadow type parameters.
// In Go, struct fields follow the pattern: FieldName Type or EmbeddedType.
// Only type-position identifiers should be substituted.
func (p *Parser) substituteStructBody(blockStr string, sub map[string]string) string {
	outerToks, err := p.Scanner.Scan(blockStr, false)
	if err != nil || len(outerToks) != 1 || outerToks[0].Tok != lang.BraceBlock {
		return blockStr
	}
	brace := outerToks[0]
	inner := brace.Block()

	// Process each field declaration (separated by ; or \n).
	// Only start building the replacement string when a change is found.
	var sb strings.Builder
	prev := 0 // tracks position in inner where sb is caught up to
	start := 0
	for i := 0; i <= len(inner); i++ {
		if i < len(inner) && inner[i] != ';' && inner[i] != '\n' {
			continue
		}
		field := inner[start:i]
		newField := p.substituteStructField(field, sub)
		if newField != field {
			sb.WriteString(inner[prev:start])
			sb.WriteString(newField)
			prev = i
		}
		start = i + 1
	}
	if prev == 0 {
		return blockStr
	}
	sb.WriteString(inner[prev:])
	return blockStr[:brace.Beg] + sb.String() + blockStr[len(blockStr)-brace.End:]
}

// substituteStructField substitutes type params in a single struct field
// declaration, protecting field-name idents from substitution.
// E.g. "K K" with sub {K:string} → "K string" (first K is field name, kept).
func (p *Parser) substituteStructField(field string, sub map[string]string) string {
	fieldToks, err := p.Scanner.Scan(field, false)
	if err != nil || len(fieldToks) == 0 {
		return field
	}

	// Count consecutive leading idents. In "K K", nIdent=2 → first is field name.
	// In "*T" or "[]T", nIdent=0 → embedded type, no field names.
	// In "K", nIdent=1 → embedded type.
	nIdent := 0
	for _, ft := range fieldToks {
		if ft.Tok != lang.Ident {
			break
		}
		nIdent++
	}

	// If 2+ leading idents, the first N-1 are field names → protect them.
	protectCount := 0
	if nIdent >= 2 {
		protectCount = nIdent - 1
	}

	var sb strings.Builder
	prev := 0
	identIdx := 0
	for _, ft := range fieldToks {
		switch {
		case ft.Tok == lang.Ident:
			if identIdx >= protectCount {
				if repl, ok := sub[ft.Str]; ok {
					sb.WriteString(field[prev:ft.Pos])
					sb.WriteString(repl)
					prev = ft.Pos + len(ft.Str)
				}
			}
			identIdx++
		case ft.Tok.IsBlock():
			innerBlock := ft.Block()
			newInner := p.substituteBlock(innerBlock, sub)
			if newInner != innerBlock {
				sb.WriteString(field[prev : ft.Pos+ft.Beg])
				sb.WriteString(newInner)
				prev = ft.Pos + len(ft.Str) - ft.End
			}
		}
	}
	if prev == 0 {
		return field
	}
	sb.WriteString(field[prev:])
	return sb.String()
}

func (p *Parser) substituteBlock(s string, sub map[string]string) string {
	toks, err := p.Scanner.Scan(s, false)
	if err != nil || len(toks) == 0 {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s))
	prev := 0
	for i, t := range toks {
		switch {
		case t.Tok == lang.Ident:
			// Skip idents after a Period — they are field/member names, not type params.
			if i > 0 && toks[i-1].Tok == lang.Period {
				break
			}
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
