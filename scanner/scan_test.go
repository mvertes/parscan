package scanner

import (
	"fmt"
	"log"
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
	BlockProp: map[string]uint{
		"(":  CharBlock,
		"{":  CharBlock,
		"[":  CharBlock,
		`"`:  CharStr | StrEsc | StrNonl,
		"`":  CharStr,
		"'":  CharStr | StrEsc,
		"/*": CharStr,
		"//": CharStr | ExcludeEnd | EosValidEnd,
	},
}

func TestScan(t *testing.T) {
	log.SetFlags(log.Lshortfile)
	GoScanner.Init()
	for _, test := range tests {
		test := test
		t.Run("", func(t *testing.T) {
			errStr := ""
			token, err := GoScanner.Scan(test.src)
			if err != nil {
				errStr = err.Error()
			}
			if errStr != test.err {
				t.Errorf("got error %#v, want error %#v", errStr, test.err)
			}
			t.Logf("%#v\n%v %v\n", test.src, token, errStr)
			if result := tokStr(token); result != test.tok {
				t.Errorf("got %v, want %v", result, test.tok)
			}
		})
	}
}

func tokStr(tokens []Token) (s string) {
	for _, t := range tokens {
		s += fmt.Sprintf("%#v ", t.content)
	}
	return s
}

var tests = []struct {
	src, tok, err string
}{{ // #00
	src: "",
}, { // #01
	src: "   abc + 5",
	tok: `"abc" "+" "5" `,
}, { // #02
	src: "abc0+5 ",
	tok: `"abc0" "+" "5" `,
}, { // #03
	src: "a+5\na=x-4",
	tok: `"a" "+" "5" " " "a" "=" "x" "-" "4" `,
}, { // #04
	src: `return "hello world" + 4`,
	tok: `"return" "\"hello world\"" "+" "4" `,
}, { // #05
	src: `print(4 * (3+7))`,
	tok: `"print" "(4 * (3+7))" `,
}, { // #06
	src: `"foo`,
	err: "1:1: block not terminated",
}, { // #07
	src: `abc
def "foo truc`,
	err: "2:6: block not terminated",
}, { // #08
	src: `"ab\"`,
	err: "1:1: block not terminated",
}, { // #09
	src: `"ab\\"`,
	tok: `"\"ab\\\\\"" `,
}, { // #10
	src: `"ab\\\"`,
	err: "1:1: block not terminated",
}, { // #11
	src: `"ab\\\\"`,
	tok: `"\"ab\\\\\\\\\"" `,
}, { // #12
	src: `"abc
def"`,
	err: "1:1: block not terminated",
}, { // #13
	src: "`hello\nworld`",
	tok: "\"`hello\\nworld`\" ",
}, { // #14
	src: "2* (3+4",
	err: "1:4: block not terminated",
}, { // #15
	src: `("fo)o")+1`,
	tok: `"(\"fo)o\")" "+" "1" `,
}, { // #16
	src: `"foo""bar"`,
	tok: `"\"foo\"" "\"bar\"" `,
}, { // #17
	src: "/* a comment */ a = 2",
	tok: `"/* a comment */" "a" "=" "2" `,
}, { // #18
	src: "return // quit\nbegin",
	tok: `"return" "// quit" " " "begin" `,
}, { // #19
	src: "return // quit",
	tok: `"return" "// quit" `,
}, { // #20
	src: "println(3 /* argh ) */)",
	tok: `"println" "(3 /* argh ) */)" `,
}}
