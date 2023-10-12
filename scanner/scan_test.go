package scanner_test

import (
	"fmt"
	"log"
	"testing"

	"github.com/gnolang/parscan/lang/golang"
	"github.com/gnolang/parscan/scanner"
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
		s += fmt.Sprintf("%#v ", t.Str)
	}
	return
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
}, { // #29
	src: "\n\n\tif i < 1 {return 0}",
	tok: `"if" "i" "<" "1" "{return 0}" ";" `,
}}
