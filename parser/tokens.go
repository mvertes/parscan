package parser

import (
	"fmt"

	"github.com/gnolang/parscan/lang"
	"github.com/gnolang/parscan/scanner"
)

type Tokens []scanner.Token

func (toks Tokens) String() (s string) {
	for _, t := range toks {
		s += fmt.Sprintf("%#v ", t.Str)
	}
	return s
}

func (toks Tokens) Index(id lang.TokenId) int {
	for i, t := range toks {
		if t.Id == id {
			return i
		}
	}
	return -1
}

func (toks Tokens) LastIndex(id lang.TokenId) int {
	for i := len(toks) - 1; i >= 0; i-- {
		if toks[i].Id == id {
			return i
		}
	}
	return -1
}

func (toks Tokens) Split(id lang.TokenId) (result []Tokens) {
	for {
		i := toks.Index(id)
		if i < 0 {
			return append(result, toks)
		}
		result = append(result, toks[:i])
		toks = toks[i+1:]
	}
}

func (toks Tokens) SplitStart(id lang.TokenId) (result []Tokens) {
	for {
		i := toks[1:].Index(id)
		if i < 0 {
			return append(result, toks)
		}
		result = append(result, toks[:i])
		toks = toks[i+1:]
	}
}
