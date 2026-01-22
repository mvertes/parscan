package parser

import (
	"strings"

	"github.com/mvertes/parscan/lang"
	"github.com/mvertes/parscan/scanner"
)

// Token represents a parser token.
type Token struct {
	scanner.Token
	Arg []any
}

// Tokens represents slice of tokens.
type Tokens []Token

func (toks Tokens) String() (s string) {
	var sb strings.Builder
	for _, t := range toks {
		sb.WriteString(t.String() + " ")
	}
	s += sb.String()
	return s
}

// Index returns the index in toks of the first matching tok, or -1.
func (toks Tokens) Index(tok lang.Token) int {
	for i, t := range toks {
		if t.Tok == tok {
			return i
		}
	}
	return -1
}

// LastIndex returns the index in toks of the last matching tok, or -1.
func (toks Tokens) LastIndex(tok lang.Token) int {
	for i := len(toks) - 1; i >= 0; i-- {
		if toks[i].Tok == tok {
			return i
		}
	}
	return -1
}

// Split returns a slice of token arrays, separated by tok.
func (toks Tokens) Split(tok lang.Token) (result []Tokens) {
	for {
		i := toks.Index(tok)
		if i < 0 {
			return append(result, toks)
		}
		result = append(result, toks[:i])
		toks = toks[i+1:]
	}
}

// SplitStart is similar to Split, except the first token in toks is skipped.
func (toks Tokens) SplitStart(tok lang.Token) (result []Tokens) {
	for {
		i := toks[1:].Index(tok)
		if i < 0 {
			return append(result, toks)
		}
		result = append(result, toks[:i])
		toks = toks[i+1:]
	}
}

func newIdent(name string, pos int, arg ...any) Token {
	return Token{Token: scanner.Token{Tok: lang.Ident, Pos: pos, Str: name}, Arg: arg}
}

func newToken(tok lang.Token, pos int, arg ...any) Token {
	return Token{Token: scanner.Token{Tok: tok, Pos: pos}, Arg: arg}
}
