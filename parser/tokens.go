package parser

import (
	"strconv"
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

func newToken(tok lang.Token, str string, pos int, arg ...any) Token {
	return Token{Token: scanner.Token{Tok: tok, Str: str, Pos: pos}, Arg: arg}
}

func newIdent(name string, pos int, arg ...any) Token { return newToken(lang.Ident, name, pos, arg...) }
func newCall(pos int, arg ...any) Token               { return newToken(lang.Call, "", pos, arg...) }
func newGoto(label string, pos int) Token             { return newToken(lang.Goto, label, pos) }
func newLabel(label string, pos int) Token            { return newToken(lang.Label, label, pos) }
func newJumpFalse(label string, pos int) Token        { return newToken(lang.JumpFalse, label, pos) }
func newNext(label string, pos int) Token             { return newToken(lang.Next, label, pos) }
func newStop(pos int) Token                           { return newToken(lang.Stop, "", pos) }
func newGrow(size, pos int) Token                     { return newToken(lang.Grow, "", pos, size) }
func newSemicolon(pos int) Token                      { return newToken(lang.Semicolon, "", pos) }
func newEqualSet(pos int) Token                       { return newToken(lang.EqualSet, "", pos) }
func newReturn(pos int) Token                         { return newToken(lang.Return, "", pos) }
func newJumpSetFalse(label string, pos int) Token     { return newToken(lang.JumpSetFalse, label, pos) }
func newJumpSetTrue(label string, pos int) Token      { return newToken(lang.JumpSetTrue, label, pos) }
func newComposite(pos int) Token                      { return newToken(lang.Composite, "", pos) }
func newIndexAssign(pos int) Token                    { return newToken(lang.IndexAssign, "", pos) }
func newIndex(pos int) Token                          { return newToken(lang.Index, "", pos) }
func newInt(i, pos int) Token                         { return newToken(lang.Int, strconv.Itoa(i), pos) }
func newColon(pos int) Token                          { return newToken(lang.Colon, "", pos) }
func newLen(i, pos int) Token                         { return newToken(lang.Len, "", pos, i) }
func newSlice(pos int) Token                          { return newToken(lang.Slice, "", pos) }
