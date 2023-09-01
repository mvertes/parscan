package scanner

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
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
	ExcludeEnd  // exclude end delimiter from content
	EosValidEnd // end of input string terminates block or string token
)

var ErrBlock = errors.New("block not terminated")

// Token defines a scanner token.
type Token struct {
	pos     int
	kind    Kind
	content string
	start   int
	end     int
	value   any
}

func (t *Token) Kind() Kind        { return t.kind }
func (t *Token) Content() string   { return t.content }
func (t *Token) Start() int        { return t.start }
func (t *Token) End() int          { return t.end }
func (t *Token) Pos() int          { return t.pos }
func (t *Token) Block() string     { return t.content[t.start : len(t.content)-t.end] }
func (t *Token) Prefix() string    { return t.content[:t.start] }
func (t *Token) Value() any        { return t.value }
func (t *Token) IsBlock() bool     { return t.kind == Block }
func (t *Token) IsOperator() bool  { return t.kind == Operator }
func (t *Token) IsSeparator() bool { return t.kind == Separator }

func (t *Token) Name() string {
	name := t.content
	if t.start > 1 {
		return name[:t.start] + ".."
	}
	if t.start > 0 {
		return name[:t.start] + ".." + name[len(name)-t.end:]
	}
	return name
}

func NewToken(content string, pos int) Token {
	return Token{pos, Custom, content, 0, 0, nil}
}

const ASCIILen = 1 << 7 // 128

// Scanner contains the scanner rules for a language.
type Scanner struct {
	CharProp  [ASCIILen]uint    // special Character properties
	End       map[string]string // end delimiters, indexed by start
	BlockProp map[string]uint   // block properties
	DotNum    bool              // true if a number can start with '.'
	IdAscii   bool              // true if an identifier can be in ASCII only
	Num_      bool              // true if a number can contain _ character

	sdre *regexp.Regexp // string delimiters regular expression
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

func (sc *Scanner) Init() {
	// Build a regular expression to match all string delimiters.
	re := "("
	for s, p := range sc.BlockProp {
		if p&CharStr == 0 {
			continue
		}
		// TODO: sort keys in decreasing length order.
		for _, b := range []byte(s) {
			re += fmt.Sprintf("\\x%02x", b)
		}
		re += "|"
	}
	re = strings.TrimSuffix(re, "|") + ")$"
	sc.sdre = regexp.MustCompile(re)
}

func IsNum(r rune) bool { return '0' <= r && r <= '9' }

func (sc *Scanner) Scan(src string) (tokens []Token, err error) {
	offset := 0
	s := src
	for len(s) > 0 {
		t, err := sc.Next(s)
		if err != nil {
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
			s, ok := sc.getStr(src[i:], 1)
			if !ok {
				err = ErrBlock
			}
			return Token{kind: String, pos: p + i, content: s, start: 1, end: 1}, err
		case sc.IsBlock(r):
			b, ok := sc.getBlock(src[i:], 1)
			if !ok {
				err = ErrBlock
			}
			return Token{kind: Block, pos: p + i, content: b, start: 1, end: 1}, err
		case sc.IsOp(r):
			op, isOp := sc.getOp(src[i:])
			if isOp {
				return Token{kind: Operator, pos: p + i, content: op}, nil
			}
			flag := sc.BlockProp[op]
			if flag&CharStr != 0 {
				s, ok := sc.getStr(src[i:], len(op))
				if !ok {
					err = ErrBlock
				}
				return Token{kind: String, pos: p + i, content: s, start: len(op), end: len(op)}, err
			}
		case IsNum(r):
			c, v := sc.getNum(src[i:])
			return Token{kind: Number, pos: p + i, content: c, value: v}, nil
		default:
			id, isId := sc.getId(src[i:])
			if isId {
				return Token{kind: Identifier, pos: p + i, content: id}, nil
			}
			flag := sc.BlockProp[id]
			if flag&CharBlock != 0 {
				s, ok := sc.getBlock(src[i:], len(id))
				if !ok {
					err = ErrBlock
				}
				return Token{kind: Block, pos: p + i, content: s, start: len(id), end: len(id)}, err
			}
		}
	}
	return Token{}, nil
}

func (sc *Scanner) getId(src string) (s string, isId bool) {
	s = sc.nextId(src)
	if _, match := sc.BlockProp[s]; match {
		return s, false
	}
	return s, true
}

func (sc *Scanner) nextId(src string) (s string) {
	for i, r := range src {
		if !sc.IsId(r) {
			break
		}
		s = src[:i+1]
	}
	return s
}

func (sc *Scanner) getOp(src string) (s string, isOp bool) {
	for i, r := range src {
		if !sc.IsOp(r) {
			break
		}
		s = src[:i+1]
		if _, match := sc.BlockProp[s]; match {
			return s, false
		}
	}
	return s, true
}

func (sc *Scanner) getNum(src string) (s string, v any) {
	// TODO: handle hexa, binary, octal, float and eng notations.
	for i, r := range src {
		if !IsNum(r) {
			break
		}
		s = src[:i+1]
	}
	var err error
	if strings.ContainsRune(s, '.') {
		v, err = strconv.ParseFloat(s, 64)
	} else {
		v, err = strconv.ParseInt(s, 0, 64)
	}
	if err != nil {
		v = err
	}
	return s, v
}

func (sc *Scanner) getStr(src string, nstart int) (s string, ok bool) {
	start := src[:nstart]
	end := sc.End[start]
	prop := sc.BlockProp[start]
	canEscape := prop&StrEsc != 0
	nonl := prop&StrNonl != 0
	excludeEnd := prop&ExcludeEnd != 0
	var esc bool

	for i, r := range src[nstart:] {
		s = src[:nstart+i+1]
		if r == '\n' && nonl {
			return
		}
		if strings.HasSuffix(s, end) && !esc {
			if excludeEnd {
				s = s[:len(s)-len(end)]
			}
			return s, true
		}
		esc = canEscape && r == '\\' && !esc
	}
	ok = prop&EosValidEnd != 0
	return s, ok
}

func (sc *Scanner) getBlock(src string, nstart int) (s string, ok bool) {
	start := src[:nstart]
	end := sc.End[start]
	prop := sc.BlockProp[start]

	skip := 0
	n := 1

	for i := range src[nstart:] {
		s = src[:nstart+i+1]
		if i < skip {
			continue
		}
		if strings.HasSuffix(s, end) {
			n--
		} else if strings.HasSuffix(s, start) {
			n++
		} else if m := sc.sdre.FindStringSubmatch(s); len(m) > 1 {
			str, ok := sc.getStr(src[i:], len(m[1]))
			if !ok {
				return s, false
			}
			skip = i + len(str) - 1
		}
		if n == 0 {
			if prop&ExcludeEnd != 0 {
				s = s[:len(s)-len(end)]
			}
			return s, true
		}
	}
	ok = prop&EosValidEnd != 0
	return s, ok
}
