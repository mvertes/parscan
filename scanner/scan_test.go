package scanner_test

import (
	"log"
	"testing"

	"github.com/mvertes/parscan/lang/golang"
	"github.com/mvertes/parscan/scanner"
)

var GoScanner *scanner.Scanner

func init() {
	log.SetFlags(log.Lshortfile)
	GoScanner = scanner.NewScanner(golang.GoSpec)
}

func TestScan(t *testing.T) {
	for _, test := range tests {
		test := test
		t.Run("", func(t *testing.T) {
			errStr := ""
			tokens, err := GoScanner.Scan(test.src, true)
			if err != nil {
				errStr = err.Error()
			}
			if errStr != test.err {
				t.Errorf("got error %v, want error %#v", errStr, test.err)
			}
			if result := tokStr(tokens); result != test.tok {
				t.Errorf("got %v, want %v", result, test.tok)
			}
		})
	}
}

func tokStr(tokens []scanner.Token) (s string) {
	for _, t := range tokens {
		s += t.String() + " "
	}
	return s
}

var tests = []struct {
	src, tok, err string
}{{ // #00
	src: "",
}, { // #01
	src: "   abc + 5",
	tok: `Ident"abc" Add Int"5" Semicolon `,
}, { // #02
	src: "abc0+5 ",
	tok: `Ident"abc0" Add Int"5" Semicolon `,
}, { // #03
	src: "a+5\na=x-4",
	tok: `Ident"a" Add Int"5" Semicolon Ident"a" Assign Ident"x" Sub Int"4" Semicolon `,
}, { // #04
	src: `return "hello world" + 4`,
	tok: `Return String"\"hello world\"" Add Int"4" Semicolon `,
}, { // #05
	src: `print(4 * (3+7))`,
	tok: `Ident"print" ParenBlock"(4 * (3+7))" Semicolon `,
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
	tok: `String"\"ab\\\\\"" Semicolon `,
}, { // #10
	src: `"ab\\\"`,
	err: "1:1: block not terminated",
}, { // #11
	src: `"ab\\\\"`,
	tok: `String"\"ab\\\\\\\\\"" Semicolon `,
}, { // #12
	src: `"abc
def"`,
	err: "1:1: block not terminated",
}, { // #13
	src: "`hello\nworld`",
	tok: "String\"`hello\\nworld`\" Semicolon ",
}, { // #14
	src: "2* (3+4",
	err: "1:4: block not terminated",
}, { // #15
	src: `("fo)o")+1`,
	tok: `ParenBlock"(\"fo)o\")" Add Int"1" Semicolon `,
}, { // #16
	src: `"foo""bar"`,
	tok: `String"\"foo\"" String"\"bar\"" Semicolon `,
}, { // #17
	src: "/* a comment */ a = 2",
	tok: `Comment"/* a comment */" Ident"a" Assign Int"2" Semicolon `,
}, { // #18
	src: "return // quit\nbegin",
	tok: `Return Comment"// quit" Semicolon Ident"begin" Semicolon `,
}, { // #19
	src: "return // quit",
	tok: `Return Comment"// quit" Semicolon `,
}, { // #20
	src: "println(3 /* argh ) */)",
	tok: `Ident"println" ParenBlock"(3 /* argh ) */)" Semicolon `,
}, { // #21
	src: `println("in f")`,
	tok: `Ident"println" ParenBlock"(\"in f\")" Semicolon `,
}, { // #22
	src: "a, b = 1, 2",
	tok: `Ident"a" Comma Ident"b" Assign Int"1" Comma Int"2" Semicolon `,
}, { // #23
	src: "1 + \n2 + 3",
	tok: `Int"1" Add Int"2" Add Int"3" Semicolon `,
}, { // #24
	src: "i++\n2 + 3",
	tok: `Ident"i" Inc Semicolon Int"2" Add Int"3" Semicolon `,
}, { // #25
	src: "return\na = 1",
	tok: `Return Semicolon Ident"a" Assign Int"1" Semicolon `,
}, { // #26
	src: "if\na == 2 { return }",
	tok: `If Ident"a" Equal Int"2" BraceBlock"{ return }" Semicolon `,
}, { // #27
	src: "f(4)\nreturn",
	tok: `Ident"f" ParenBlock"(4)" Semicolon Return Semicolon `,
}, { // #28
	src: "f(3).\nfield",
	tok: `Ident"f" ParenBlock"(3)" Period"." Ident"field" Semicolon `,
}, { // #29
	src: "\n\n\tif i < 1 {return 0}",
	tok: `If Ident"i" Less Int"1" BraceBlock"{return 0}" Semicolon `,
}}
