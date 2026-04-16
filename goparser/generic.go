package goparser

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mvertes/parscan/lang"
	"github.com/mvertes/parscan/scan"
	"github.com/mvertes/parscan/vm"
)

// typeParam represents a single type parameter in a generic definition (e.g. T any).
type typeParam struct {
	name       string // e.g. "T", "K", "V"
	constraint string // e.g. "any", "comparable", or an interface name
}

// genericTemplate stores a generic function or type definition for later instantiation.
type genericTemplate struct {
	name       string          // original name (e.g. "Max", "Set")
	typeParams []typeParam     // ordered type parameter list
	rawTokens  Tokens          // entire declaration tokens (func or type)
	isFunc     bool            // true for generic functions, false for generic types
	instances  map[string]bool // tracks already-instantiated mangled names
}

// parseTypeParamList parses the contents of a bracket block as a type parameter list.
// E.g. "[T any, K comparable]" -> [{name:"T", constraint:"any"}, {name:"K", constraint:"comparable"}].
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

// instantiateFunc creates a concrete (monomorphized) version of a generic function template
// by substituting type parameter names with concrete type names in the token stream.
// It returns the rewritten tokens and the mangled name.
func (p *Parser) instantiateFunc(tmpl *genericTemplate, typeArgs []*vm.Type) (Tokens, string, error) {
	if len(typeArgs) != len(tmpl.typeParams) {
		return nil, "", ErrSyntax
	}

	mname := mangledName(tmpl.name, typeArgs)
	if tmpl.instances[mname] {
		return nil, mname, nil // Already instantiated.
	}
	tmpl.instances[mname] = true

	// Build substitution map: type param name -> concrete type name string.
	sub := make(map[string]string, len(tmpl.typeParams))
	for i, tp := range tmpl.typeParams {
		name := typeArgs[i].Name
		if name == "" {
			name = typeArgs[i].Rtype.String()
		}
		sub[tp.name] = name
	}

	// Deep-copy and rewrite tokens.
	raw := tmpl.rawTokens
	out := make(Tokens, 0, len(raw))
	for i, t := range raw {
		t2 := t // shallow copy

		switch {
		case i == 1 && t.Tok == lang.Ident && t.Str == tmpl.name:
			// Replace function name with mangled name.
			t2.Str = mname
		case i == 2 && t.Tok == lang.BracketBlock:
			// Skip the type parameter bracket entirely.
			continue
		case t.Tok == lang.Ident:
			if repl, ok := sub[t.Str]; ok {
				t2.Str = repl
			}
		}

		// For block tokens, substitute inside the raw source text.
		if t.Tok.IsBlock() {
			t2.Str = substituteBlock(t.Str, sub)
		}

		out = append(out, t2)
	}
	return out, mname, nil
}

// substituteBlock replaces type parameter names inside a block token's raw source text
// using word-boundary-aware matching.
func substituteBlock(s string, sub map[string]string) string {
	changed := false
	for old, repl := range sub {
		if old == repl {
			continue
		}
		ns := replaceIdent(s, old, repl)
		if ns != s {
			s = ns
			changed = true
		}
	}
	if !changed {
		return s
	}
	return s
}

// replaceIdent replaces all occurrences of identifier old with repl in s,
// but only when old is at a word boundary (not part of a larger identifier).
func replaceIdent(s, old, repl string) string {
	if len(old) == 0 || !strings.Contains(s, old) {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s))
	i := 0
	for i < len(s) {
		j := strings.Index(s[i:], old)
		if j < 0 {
			sb.WriteString(s[i:])
			break
		}
		pos := i + j
		// Check left boundary: pos==0 or previous char is not an ident char.
		leftOk := pos == 0 || !isIdentChar(lastRune(s[:pos]))
		// Check right boundary: end==len(s) or next char is not an ident char.
		end := pos + len(old)
		rightOk := end == len(s) || !isIdentChar(firstRune(s[end:]))
		if leftOk && rightOk {
			sb.WriteString(s[i:pos])
			sb.WriteString(repl)
			i = end
		} else {
			sb.WriteString(s[i : pos+len(old)])
			i = pos + len(old)
		}
	}
	return sb.String()
}

func isIdentChar(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func firstRune(s string) rune {
	r, _ := utf8.DecodeRuneInString(s)
	return r
}

func lastRune(s string) rune {
	r, _ := utf8.DecodeLastRuneInString(s)
	return r
}
