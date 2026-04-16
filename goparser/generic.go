package goparser

import (
	"strings"

	"github.com/mvertes/parscan/lang"
	"github.com/mvertes/parscan/scan"
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

// mangledName returns the mangled name for a generic instantiation.
// E.g. mangledName("Max", [int]) -> "Max#int".
func mangledName(base string, typeArgs []*vm.Type) string {
	var sb strings.Builder
	sb.WriteString(base)
	for _, t := range typeArgs {
		sb.WriteByte('#')
		name := t.Name
		if name == "" {
			name = t.Rtype.String()
		}
		sb.WriteString(name)
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

	// Build substitution map: type param name -> concrete type name string.
	sub := make(map[string]string, len(tmpl.typeParams))
	for i, tp := range tmpl.typeParams {
		name := typeArgs[i].Name
		if name == "" {
			name = typeArgs[i].Rtype.String()
		}
		sub[tp.name] = name
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
