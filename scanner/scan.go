package scanner

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/mvertes/parscan/lang"
)

var (
	ErrBlock   = errors.New("block not terminated")
	ErrIllegal = errors.New("illegal token")
)

// Token defines a scanner token.
type Token struct {
	Id  lang.TokenId // token identificator
	Pos int          // position in source
	Str string       // string in source
	Beg int          // length of begin delimiter (block, string)
	End int          // length of end delimiter (block, string)
}

func (t *Token) Block() string  { return t.Str[t.Beg : len(t.Str)-t.End] }
func (t *Token) Prefix() string { return t.Str[:t.Beg] }

func (t *Token) Name() string {
	name := t.Str
	if t.Beg > 1 {
		return name[:t.Beg] + ".."
	}
	if t.Beg > 0 {
		return name[:t.Beg] + ".." + name[len(name)-t.End:]
	}
	return name
}

// Scanner contains the scanner rules for a language.
type Scanner struct {
	*lang.Spec

	sdre *regexp.Regexp // string delimiters regular expression
}

func NewScanner(spec *lang.Spec) *Scanner {
	sc := &Scanner{Spec: spec}

	// TODO: Mark unset ASCII char other than alphanum illegal

	// Build a regular expression to match all string start delimiters at once.
	re := "("
	for s, p := range sc.BlockProp {
		if p&lang.CharStr == 0 {
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

	return sc
}

func (sc *Scanner) HasProp(r rune, p uint) bool {
	if r >= lang.ASCIILen {
		return false
	}
	return sc.CharProp[r]&p != 0
}

func (sc *Scanner) isOp(r rune) bool       { return sc.HasProp(r, lang.CharOp) }
func (sc *Scanner) isSep(r rune) bool      { return sc.HasProp(r, lang.CharSep) }
func (sc *Scanner) isLineSep(r rune) bool  { return sc.HasProp(r, lang.CharLineSep) }
func (sc *Scanner) isGroupSep(r rune) bool { return sc.HasProp(r, lang.CharGroupSep) }
func (sc *Scanner) isStr(r rune) bool      { return sc.HasProp(r, lang.CharStr) }
func (sc *Scanner) isBlock(r rune) bool    { return sc.HasProp(r, lang.CharBlock) }
func (sc *Scanner) isDir(r rune) bool {
	return !sc.HasProp(r, lang.CharOp|lang.CharSep|lang.CharLineSep|lang.CharGroupSep|lang.CharStr|lang.CharBlock)
}

func isNum(r rune) bool { return '0' <= r && r <= '9' }

func (sc *Scanner) Scan(src string, semiEOF bool) (tokens []Token, err error) {
	offset := 0
	s := strings.TrimSpace(src)
	for len(s) > 0 {
		t, err := sc.Next(s)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", loc(src, offset+t.Pos), err)
		}
		if t.Id == lang.Illegal && t.Str == "" {
			break
		}
		skip := false
		if len(tokens) > 0 && t.Str == "\n" {
			// Check for automatic semi-colon insertion after newline.
			last := tokens[len(tokens)-1]
			if last.Id.IsKeyword() && sc.TokenProps[last.Str].SkipSemi ||
				last.Id.IsOperator() && !sc.TokenProps[last.Str].SkipSemi {
				skip = true
			} else {
				t.Id = lang.Semicolon
				t.Str = ";"
			}
		}
		nr := t.Pos + len(t.Str)
		s = s[nr:]
		t.Pos += offset
		offset += nr
		if !skip {
			tokens = append(tokens, t)
		}
	}
	// Optional insertion of semi-colon at the end of the token stream.
	if semiEOF && len(tokens) > 0 {
		last := tokens[len(tokens)-1]
		if last.Str == ";" {
			return tokens, nil
		}
		if !(last.Id == lang.Ident && sc.TokenProps[last.Str].SkipSemi ||
			last.Id.IsOperator() && !sc.TokenProps[last.Str].SkipSemi) {
			tokens = append(tokens, Token{Id: lang.Semicolon, Str: ";"})
		}
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
		if !sc.isSep(r) {
			break
		}
	}
	src = src[p:]

	// Get token according to its first characters.
	for i, r := range src {
		switch {
		case sc.isSep(r):
			return Token{}, nil
		case sc.isGroupSep(r):
			// TODO: handle group separators.
			return Token{Id: sc.TokenProps[string(r)].TokenId, Pos: p + i, Str: string(r)}, nil
		case sc.isLineSep(r):
			return Token{Pos: p + i, Str: "\n"}, nil
		case sc.isStr(r):
			s, ok := sc.getStr(src[i:], 1)
			if !ok {
				err = ErrBlock
			}
			return Token{Id: lang.String, Pos: p + i, Str: s, Beg: 1, End: 1}, err
		case sc.isBlock(r):
			b, ok := sc.getBlock(src[i:], 1)
			if !ok {
				err = ErrBlock
			}
			tok := Token{Pos: p + i, Str: b, Beg: 1, End: 1}
			tok.Id = sc.TokenProps[tok.Name()].TokenId
			return tok, err
		case sc.isOp(r):
			op, isOp := sc.getOp(src[i:])
			if isOp {
				id := sc.TokenProps[op].TokenId
				if id == lang.Illegal {
					err = fmt.Errorf("%w: %s", ErrIllegal, op)
				}
				return Token{Id: id, Pos: p + i, Str: op}, err
			}
			flag := sc.BlockProp[op]
			if flag&lang.CharStr != 0 {
				s, ok := sc.getStr(src[i:], len(op))
				if !ok {
					err = ErrBlock
				}
				return Token{Id: lang.Comment, Pos: p + i, Str: s, Beg: len(op), End: len(op)}, err
			}
		case isNum(r):
			return Token{Id: lang.Int, Pos: p + i, Str: sc.getNum(src[i:])}, nil
		default:
			id, isId := sc.getId(src[i:])
			if isId {
				ident := sc.TokenProps[id].TokenId
				if ident == lang.Illegal {
					ident = lang.Ident
				}
				return Token{Id: ident, Pos: p + i, Str: id}, nil
			}
			flag := sc.BlockProp[id]
			if flag&lang.CharBlock != 0 {
				s, ok := sc.getBlock(src[i:], len(id))
				if !ok {
					err = ErrBlock
				}
				return Token{Pos: p + i, Str: s, Beg: len(id), End: len(id)}, err
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
		if !sc.isDir(r) {
			break
		}
		s = src[:i+1]
	}
	return s
}

func (sc *Scanner) getOp(src string) (s string, isOp bool) {
	for i, r := range src {
		if !sc.isOp(r) {
			break
		}
		s = src[:i+1]
		if _, match := sc.BlockProp[s]; match {
			return s, false
		}
	}
	return s, true
}

func (sc *Scanner) getNum(src string) (s string) {
	// TODO: handle hexa, binary, octal, float and eng notations.
	for i, r := range src {
		if !isNum(r) {
			break
		}
		s = src[:i+1]
	}
	return s
}

func (sc *Scanner) getStr(src string, nstart int) (s string, ok bool) {
	start := src[:nstart]
	end := sc.End[start]
	prop := sc.BlockProp[start]
	canEscape := prop&lang.StrEsc != 0
	nonl := prop&lang.StrNonl != 0
	excludeEnd := prop&lang.ExcludeEnd != 0
	var esc bool

	for i, r := range src[nstart:] {
		s = src[:nstart+i+1]
		if r == '\n' && nonl {
			return s, ok
		}
		if strings.HasSuffix(s, end) && !esc {
			if excludeEnd {
				s = s[:len(s)-len(end)]
			}
			return s, true
		}
		esc = canEscape && r == '\\' && !esc
	}
	ok = prop&lang.EosValidEnd != 0
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
			l1 := len(m[1])
			str, ok := sc.getStr(src[nstart+i+1-l1:], l1)
			if !ok {
				return s, false
			}
			skip = nstart + i + len(str) - l1
		}
		if n == 0 {
			if prop&lang.ExcludeEnd != 0 {
				s = s[:len(s)-len(end)]
			}
			return s, true
		}
	}
	ok = prop&lang.EosValidEnd != 0
	return s, ok
}
