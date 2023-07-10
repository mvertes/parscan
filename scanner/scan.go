package scanner

import (
	"errors"
	"fmt"
	"strings"
)

// Kind is the token type kind.
type Kind uint

const (
	Undefined Kind = iota
	Identifier
	Number
	Operator
	Separator
	String
	Block
	Custom
)

const (
	CharOp = 1 << iota
	CharNum
	CharAlpha
	CharSep
	CharLineSep
	CharGroupSep
	CharStr
	CharBlock
	StrEsc
	StrNonl
)

var ErrBlock = errors.New("block not terminated")

// Token defines a scanner token.
type Token struct {
	pos     int
	kind    Kind
	content string
	start   int
	end     int
}

func (t *Token) Kind() Kind      { return t.kind }
func (t *Token) Content() string { return t.content }
func (t *Token) Start() int      { return t.start }
func (t *Token) End() int        { return t.end }
func (t *Token) Pos() int        { return t.pos }
func (t *Token) Block() string   { return t.content[t.start : len(t.content)-t.end] }
func (t *Token) Prefix() string  { return t.content[:t.start] }

func (t *Token) Name() string {
	name := t.content
	if t.start > 0 {
		name = name[:t.start] + ".." + name[len(name)-t.end:]
	}
	return name
}

func NewToken(content string, pos int) Token {
	return Token{pos, Custom, content, 0, 0}
}

const ASCIILen = 1 << 7 // 128

// Scanner contains the scanner rules for a language.
type Scanner struct {
	CharProp [ASCIILen]uint    // Special Character properties.
	End      map[string]string // End delimiters.
	DotNum   bool              // True if a number can start with '.'.
	IdAscii  bool              // True if an identifier can be in ASCII only.
	Num_     bool              // True if a number can contain _ character.
}

func (sc *Scanner) HasProp(r rune, p uint) bool {
	if r >= ASCIILen {
		return false
	}
	return sc.CharProp[r]&p != 0
}

func (sc *Scanner) IsOp(r rune) bool       { return sc.HasProp(r, CharOp) }
func (sc *Scanner) IsSep(r rune) bool      { return sc.HasProp(r, CharSep) }
func (sc *Scanner) IsLineSep(r rune) bool  { return sc.HasProp(r, CharLineSep) }
func (sc *Scanner) IsGroupSep(r rune) bool { return sc.HasProp(r, CharGroupSep) }
func (sc *Scanner) IsStr(r rune) bool      { return sc.HasProp(r, CharStr) }
func (sc *Scanner) IsBlock(r rune) bool    { return sc.HasProp(r, CharBlock) }
func (sc *Scanner) IsId(r rune) bool {
	return !sc.HasProp(r, CharOp|CharSep|CharLineSep|CharGroupSep|CharStr|CharBlock)
}

func IsNum(r rune) bool { return '0' <= r && r <= '9' }

func (sc *Scanner) Scan(src string) (tokens []Token, err error) {
	offset := 0
	s := src
	for len(s) > 0 {
		t, err := sc.Next(s)
		if err != nil {
			//return nil, fmt.Errorf("%s: %w '%s'", loc(src, offset+t.pos), err, t.delim)
			return nil, fmt.Errorf("%s: %w", loc(src, offset+t.pos), err)
		}
		if t.kind == Undefined {
			break
		}
		nr := t.pos + len(t.content)
		s = s[nr:]
		t.pos += offset
		offset += nr
		tokens = append(tokens, t)
	}
	return tokens, nil
}

func loc(s string, p int) string {
	s = s[:p]
	l := strings.Count(s, "\n")
	li := strings.LastIndex(s, "\n")
	if li < 0 {
		li = 0
	}
	return fmt.Sprintf("%d:%d", 1+l, 1+len(s)-li)
}

// Next returns the next token in string.
func (sc *Scanner) Next(src string) (tok Token, err error) {
	p := 0

	// Skip initial separators.
	for i, r := range src {
		p = i
		if !sc.IsSep(r) {
			break
		}
	}
	src = src[p:]

	// Get token according to its first characters.
	for i, r := range src {
		switch {
		case sc.IsSep(r):
			return Token{}, nil
		case sc.IsGroupSep(r):
			// TODO: handle group separators.
			return Token{kind: Separator, pos: p + i, content: string(r)}, nil
		case sc.IsLineSep(r):
			return Token{kind: Separator, pos: p + i, content: " "}, nil
		case sc.IsStr(r):
			s, ok := sc.GetStr(src[i:])
			if !ok {
				err = ErrBlock
			}
			return Token{kind: String, pos: p + i, content: s, start: 1, end: 1}, err
		case sc.IsBlock(r):
			b, ok := sc.GetBlock(src[i:])
			if !ok {
				err = ErrBlock
			}
			return Token{kind: Block, pos: p + i, content: b, start: 1, end: 1}, err
		case sc.IsOp(r):
			return Token{kind: Operator, pos: p + i, content: sc.GetOp(src[i:])}, nil
		case IsNum(r):
			return Token{kind: Number, pos: p + i, content: sc.GetNum(src[i:])}, nil
		default:
			return Token{kind: Identifier, pos: p + i, content: sc.GetId(src[i:])}, nil
		}
	}
	return Token{}, nil
}

func (sc *Scanner) GetId(src string) (s string) {
	for _, r := range src {
		if !sc.IsId(r) {
			break
		}
		s += string(r)
	}
	return s
}

func (sc *Scanner) GetOp(src string) (s string) {
	for _, r := range src {
		if !sc.IsOp(r) {
			break
		}
		s += string(r)
	}
	return s
}

func (sc *Scanner) GetNum(src string) (s string) {
	// TODO: handle hexa, binary, octal, float and eng notations.
	for _, r := range src {
		if !IsNum(r) {
			break
		}
		s += string(r)
	}
	return s
}

func (sc *Scanner) GetGroupSep(src string) (s string) {
	for _, r := range src {
		return string(r)
	}
	return s
}

func (sc *Scanner) GetStr(src string) (s string, ok bool) {
	// TODO: handle long delimiters.
	var delim rune
	var esc, canEscape, nonl bool
	for i, r := range src {
		s += string(r)
		if i == 0 {
			delim = r
			canEscape = sc.HasProp(r, StrEsc)
			nonl = sc.HasProp(r, StrNonl)
			continue
		}
		if r == '\n' && nonl {
			return
		}
		if r == delim && !(esc && canEscape) {
			return s, true
		}
		esc = r == '\\' && !esc
	}
	return
}

func (sc *Scanner) GetBlock(src string) (s string, ok bool) {
	// TODO: handle long and word delimiters.
	var start, end rune
	skip := 0
	n := 1
	for i, r := range src {
		s += string(r)
		if i == 0 {
			start = r
			end = rune(sc.End[string(r)][0]) // FIXME: not robust.
			continue
		}
		if i < skip {
			continue
		} else if r == start {
			n++
		} else if r == end {
			n--
		} else if sc.IsStr(r) {
			str, ok := sc.GetStr(src[i:])
			if !ok {
				return s, false
			}
			skip = i + len(str)
		}
		if n == 0 {
			break
		}
	}
	return s, n == 0
}
