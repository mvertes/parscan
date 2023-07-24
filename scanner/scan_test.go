package scanner

import (
	"fmt"
	"testing"
)

var GoScanner = &Scanner{
	CharProp: [ASCIILen]uint{
		'\t': CharSep,
		'\n': CharLineSep,
		' ':  CharSep,
		'!':  CharOp,
		'"':  CharStr | StrEsc | StrNonl,
		'%':  CharOp,
		'&':  CharOp,
		'\'': CharStr | StrEsc,
		'(':  CharBlock,
		'*':  CharOp,
		'+':  CharOp,
		',':  CharGroupSep,
		'-':  CharOp,
		'`':  CharStr,
		'.':  CharOp,
		'/':  CharOp,
		':':  CharOp,
		';':  CharGroupSep,
		'<':  CharOp,
		'=':  CharOp,
		'>':  CharOp,
		'[':  CharBlock,
		'^':  CharOp,
		'{':  CharBlock,
		'|':  CharOp,
		'~':  CharOp,
	},
	End: map[string]string{
		"(":  ")",
		"{":  "}",
		"[":  "]",
		"/*": "*/",
		`"`:  `"`,
		"'":  "'",
		"`":  "`",
		"//": "\n",
	},
}

func TestScan(t *testing.T) {
	tests := []struct{ src, result, errStr string }{
		// Simple tokens: separators, identifiers, numbers, operators.
		{"", "[]", ""},
		{"   abc + 5", "[{3 1 abc 0 0 <nil>} {7 3 + 0 0 <nil>} {9 2 5 0 0 5}]", ""},
		{"abc0+5 ", "[{0 1 abc0 0 0 <nil>} {4 3 + 0 0 <nil>} {5 2 5 0 0 5}]", ""},
		{"a+5\na=x-4", "[{0 1 a 0 0 <nil>} {1 3 + 0 0 <nil>} {2 2 5 0 0 5} {3 4   0 0 <nil>} {4 1 a 0 0 <nil>} {5 3 = 0 0 <nil>} {6 1 x 0 0 <nil>} {7 3 - 0 0 <nil>} {8 2 4 0 0 4}]", ""},

		// Strings.
		{`return "hello world" + 4`, `[{0 1 return 0 0 <nil>} {7 5 "hello world" 1 1 <nil>} {21 3 + 0 0 <nil>} {23 2 4 0 0 4}]`, ""},
		{`print(4 * (3+7))`, "[{0 1 print 0 0 <nil>} {5 6 (4 * (3+7)) 1 1 <nil>}]", ""},
		{`"foo`, "[]", "1:1: block not terminated"},
		{`abc
def "foo truc`, "[]", "2:6: block not terminated"},
		{`"ab\"`, "[]", "1:1: block not terminated"},
		{`"ab\\"`, `[{0 5 "ab\\" 1 1 <nil>}]`, ""},
		{`"ab\\\"`, "[]", "1:1: block not terminated"},
		{`"ab\\\\"`, `[{0 5 "ab\\\\" 1 1 <nil>}]`, ""},
		{`"abc
def"`, "[]", "1:1: block not terminated"},
		{"`hello\nworld`", "[{0 5 `hello\nworld` 1 1 <nil>}]", ""},

		// Nested blocks.
		// {`f("a)bc")+1, 3)`, "[{0 1 f } {1 6 (\"a)bc\", 3) (}]", ""},
		{"2* (3+4", "[]", "1:4: block not terminated"},
		{`("fo)o")+1`, "[{0 6 (\"fo)o\") 1 1 <nil>} {8 3 + 0 0 <nil>} {9 2 1 0 0 1}]", ""},
		{`"foo""bar"`, "[{0 5 \"foo\" 1 1 <nil>} {5 5 \"bar\" 1 1 <nil>}]", ""},
	}

	for _, test := range tests {
		t.Run("", func(t *testing.T) {
			errStr := ""
			token, err := GoScanner.Scan(test.src)
			if err != nil {
				errStr = err.Error()
			}
			if errStr != test.errStr {
				t.Errorf("got error %#v, want error %#v", errStr, test.errStr)
			}
			result := fmt.Sprintf("%v", token)
			t.Logf("%#v\n%v %v\n", test.src, result, errStr)
			if result != test.result {
				t.Errorf("got %#v, want %#v", result, test.result)
			}
		})
	}
}
