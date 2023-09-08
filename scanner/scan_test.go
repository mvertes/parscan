package scanner

import (
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
	SkipSemi: map[string]bool{
		"++":        true,
		"--":        true,
		"case":      true,
		"chan":      true,
		"const":     true,
		"default":   true,
		"defer":     true,
		"else":      true,
		"for":       true,
		"func":      true,
		"go":        true,
		"goto":      true,
		"if":        true,
		"import":    true,
		"interface": true,
		"map":       true,
		"package":   true,
		"range":     true,
		"select":    true,
		"struct":    true,
		"switch":    true,
		"type":      true,
		"var":       true,
	},
}

func TestScan(t *testing.T) {
	log.SetFlags(log.Lshortfile)
	GoScanner.Init()
	for _, test := range tests {
		test := test
		t.Run("", func(t *testing.T) {
			errStr := ""
			tokens, err := GoScanner.Scan(test.src)
			if err != nil {
				errStr = err.Error()
			}
			if errStr != test.err {
				t.Errorf("got error %v, want error %#v", errStr, test.err)
			}
			if result := tokens.String(); result != test.tok {
				t.Errorf("got %v, want %v", result, test.tok)
			}
		})
	}
}

var tests = []struct {
	src, tok, err string
}{{ // #00
	src: "",
}, { // #01
	src: "   abc + 5",
	tok: `"abc" "+" "5" ";" `,
}, { // #02
	src: "abc0+5 ",
	tok: `"abc0" "+" "5" ";" `,
}, { // #03
	src: "a+5\na=x-4",
	tok: `"a" "+" "5" ";" "a" "=" "x" "-" "4" ";" `,
}, { // #04
	src: `return "hello world" + 4`,
	tok: `"return" "\"hello world\"" "+" "4" ";" `,
}, { // #05
	src: `print(4 * (3+7))`,
	tok: `"print" "(4 * (3+7))" ";" `,
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
	tok: `"\"ab\\\\\"" ";" `,
}, { // #10
	src: `"ab\\\"`,
	err: "1:1: block not terminated",
}, { // #11
	src: `"ab\\\\"`,
	tok: `"\"ab\\\\\\\\\"" ";" `,
}, { // #12
	src: `"abc
def"`,
	err: "1:1: block not terminated",
}, { // #13
	src: "`hello\nworld`",
	tok: "\"`hello\\nworld`\" \";\" ",
}, { // #14
	src: "2* (3+4",
	err: "1:4: block not terminated",
}, { // #15
	src: `("fo)o")+1`,
	tok: `"(\"fo)o\")" "+" "1" ";" `,
}, { // #16
	src: `"foo""bar"`,
	tok: `"\"foo\"" "\"bar\"" ";" `,
}, { // #17
	src: "/* a comment */ a = 2",
	tok: `"/* a comment */" "a" "=" "2" ";" `,
}, { // #18
	src: "return // quit\nbegin",
	tok: `"return" "// quit" ";" "begin" ";" `,
}, { // #19
	src: "return // quit",
	tok: `"return" "// quit" ";" `,
}, { // #20
	src: "println(3 /* argh ) */)",
	tok: `"println" "(3 /* argh ) */)" ";" `,
}, { // #21
	src: `println("in f")`,
	tok: `"println" "(\"in f\")" ";" `,
}, { // #22
	src: "a, b = 1, 2",
	tok: `"a" "," "b" "=" "1" "," "2" ";" `,
}, { // #23
	src: "1 + \n2 + 3",
	tok: `"1" "+" "2" "+" "3" ";" `,
}, { // #24
	src: "i++\n2 + 3",
	tok: `"i" "++" ";" "2" "+" "3" ";" `,
}, { // #25
	src: "return\na = 1",
	tok: `"return" ";" "a" "=" "1" ";" `,
}, { // #26
	src: "if\na == 2 { return }",
	tok: `"if" "a" "==" "2" "{ return }" ";" `,
}, { // #27
	src: "f(4)\nreturn",
	tok: `"f" "(4)" ";" "return" ";" `,
}, { // #28
	src: "f(3).\nfield",
	tok: `"f" "(3)" "." "field" ";" `,
}}
