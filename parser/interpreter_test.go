package parser_test

import (
	"fmt"
	"log"
	"testing"

	"github.com/gnolang/parscan/lang/golang"
	"github.com/gnolang/parscan/parser"
	"github.com/gnolang/parscan/scanner"
)

type etest struct {
	src, res, err string
	skip          bool
}

var GoScanner *scanner.Scanner

func init() {
	log.SetFlags(log.Lshortfile)
	GoScanner = scanner.NewScanner(golang.GoSpec)
}

func gen(test etest) func(*testing.T) {
	return func(t *testing.T) {
		if test.skip {
			t.Skip()
		}
		interp := parser.NewInterpreter(GoScanner)
		errStr := ""
		r, e := interp.Eval(test.src)
		t.Log(r, e)
		if e != nil {
			errStr = e.Error()
		}
		if errStr != test.err {
			t.Errorf("got error %#v, want error %#v", errStr, test.err)
		}
		if res := fmt.Sprintf("%v", r); test.err == "" && res != test.res {
			t.Errorf("got %#v, want %#v", res, test.res)
		}
	}
}

func run(t *testing.T, tests []etest) {
	for _, test := range tests {
		test := test
		t.Run("", gen(test))
	}
}

func TestExpr(t *testing.T) {
	run(t, []etest{
		{src: "", res: "<invalid reflect.Value>"},
		{src: "1+2", res: "3"},
		{src: "1+", err: "block not terminated"},
		{src: "a := 1 + 2; b := 0; a + 1", res: "4"},
		{src: "1+(2+3)", res: "6"},
		{src: "(1+2)+3", res: "6"},
		{src: "(6+(1+2)+3)+5", res: "17"},
		{src: "(6+(1+2+3)+5", err: "1:1: block not terminated"},
		{src: "a := 2; a = 3; a", res: "3"},
		{src: "2 * 3 + 1 == 7", res: "true"},
		{src: "7 == 2 * 3 + 1", res: "true"},
		{src: "1 + 3 * 2 == 2 * 3 + 1", res: "true"},
		{src: "a := 1 + 3 * 2 == 2 * 3 + 1; a", res: "true"},
		{src: "-2", res: "-2"},
		{src: "-2 + 5", res: "3"},
		{src: "5 + -2", res: "3"},
		{src: "!false", res: "true"},
	})
}

func TestLogical(t *testing.T) {
	run(t, []etest{
		{src: "true && false", res: "false"},
		{src: "true && true", res: "true"},
		{src: "true && true && false", res: "false"},
		{src: "false || true && true", res: "true"},
		{src: "2 < 3 && 1 > 2 || 3 == 3", res: "true"},
		{src: "2 > 3 && 1 > 2 || 3 == 3", res: "true"},
		{src: "2 > 3 || 2 == 1+1 && 3>0", res: "true"},
		{src: "2 > 3 || 2 == 1+1 && 3>4 || 1<2", res: "true"},
		{src: "a := 1+1 < 3 && 4 == 2+2; a", res: "true"},
		{src: "a := 1+1 < 3 || 3 == 2+2; a", res: "true"},
	})
}

func TestFunc(t *testing.T) {
	run(t, []etest{
		{src: "func f() int {return 2}; a := f(); a", res: "2"},
		{src: "func f() int {return 2}; f()", res: "2"},
		{src: "func f(a int) int {return a+2}; f(3)", res: "5"},
		{src: "func f(a int) int {if a < 4 {a = 5}; return a}; f(3)", res: "5"},
		{src: "func f(a int) int {return a+2}; 7 - f(3)", res: "2"},
		{src: "func f(a int) int {return a+2}; f(5) - f(3)", res: "2"},
		{src: "func f(a int) int {return a+2}; f(3) - 2", res: "3"},
		{src: "func f(a, b, c int) int {return a+b-c} ; f(7, 1, 3)", res: "5"},
	})
}

func TestIf(t *testing.T) {
	run(t, []etest{
		{src: "a := 0; if a == 0 { a = 2 } else { a = 1 }; a", res: "2"},
		{src: "a := 0; if a == 1 { a = 2 } else { a = 1 }; a", res: "1"},
		{src: "a := 0; if a == 1 { a = 2 } else if a == 0 { a = 3 } else { a = 1 }; a", res: "3"},
		{src: "a := 0; if a == 1 { a = 2 } else if a == 2 { a = 3 } else { a = 1 }; a", res: "1"},
		{src: "a := 1; if a > 0 && a < 2 { a = 3 }; a", res: "3"},
		{src: "a := 1; if a < 0 || a < 2 { a = 3 }; a", res: "3"},
	})
}

func TestFor(t *testing.T) {
	run(t, []etest{
		{src: "a := 0; for i := 0; i < 3; i = i+1 {a = a+i}; a", res: "3"},
		{src: "func f() int {a := 0; for i := 0; i < 3; i = i+1 {a = a+i}; return a}; f()", res: "3"},
		{src: "a := 0; for {a = a+1; if a == 3 {break}}; a", res: "3"},
		{src: "func f() int {a := 0; for {a = a+1; if a == 3 {break}}; return a}; f()", res: "3"},
		{src: "func f() int {a := 0; for {a = a+1; if a < 3 {continue}; break}; return a}; f()", res: "3"},
	})
}

func TestGoto(t *testing.T) {
	run(t, []etest{
		{src: `
func f(a int) int {
	a = a+1
	goto end
	a = a+1
end:
	return a
}
f(3)`, res: "4"},
	})
}

func TestSwitch(t *testing.T) {
	src0 := `func f(a int) int {
	switch a {
	default:  a = 0
	case 1,2: a = a+1
	case 3:   a = a+2; break; a = 3
	case 4:   a = 10
	}
	return a
}
`
	src1 := `func f(a int) int {
	switch {
	case a < 3: return 2
	case a < 5: return 5
	default:  a = 0
	}
	return a
}
`
	run(t, []etest{
		{src: src0 + "f(1)", res: "2"},
		{src: src0 + "f(2)", res: "3"},
		{src: src0 + "f(3)", res: "5"},
		{src: src0 + "f(4)", res: "10"},
		{src: src0 + "f(5)", res: "0"},

		{src: src1 + "f(1)", res: "2"},
		{src: src1 + "f(4)", res: "5"},
		{src: src1 + "f(6)", res: "0"},
	})
}

func TestConst(t *testing.T) {
	src0 := `const (
	a = iota
	b
	c
)
`
	run(t, []etest{
		{src: "const a = 1+2; a", res: "3"},
		{src: "const a, b = 1, 2; a+b", res: "3"},
		{src: "const huge = 1 << 100; const four = huge >> 98; four", res: "4"},

		{src: src0 + "c", res: "2"},
	})
}

func TestArray(t *testing.T) {
	run(t, []etest{
		{src: "type T []int; var t T; t", res: "[]"},
		{src: "type T [3]int; var t T; t", res: "[0 0 0]"},
		{src: "type T [3]int; var t T; t[1] = 2; t", res: "[0 2 0]"},
	})
}

func TestPointer(t *testing.T) {
	run(t, []etest{
		{src: "var a *int; a", res: "<nil>"},
		{src: "var a int; var b *int = &a; *b", res: "0"},
		{src: "var a int = 2; var b *int = &a; *b", res: "2"},
	})
}

func TestStruct(t *testing.T) {
	run(t, []etest{
		{src: "type T struct {a string; b, c int}; var t T; t", res: "{ 0 0}"},
		{src: "type T struct {a int}; var t T; t.a", res: "0"},
		{src: "type T struct {a int}; var t T; t.a = 1; t.a", res: "1"},
	})
}

func TestType(t *testing.T) {
	src0 := `type(
	I int
	S string
)
`
	run(t, []etest{
		{src: "type t int; var a t = 1; a", res: "1"},
		{src: "type t = int; var a t = 1; a", res: "1"},
		{src: src0 + `var s S = "xx"; s`, res: "xx"},
	})
}

func TestVar(t *testing.T) {
	run(t, []etest{
		{src: "var a int; a", res: "0"},
		{src: "var a, b, c int; a", res: "0"},
		{src: "var a, b, c int; a + b", res: "0"},
		{src: "var a, b, c int; a + b + c", res: "0"},
		{src: "var a int = 2+1; a", res: "3"},
		{src: "var a, b int = 2, 5; a+b", res: "7"},
		{src: "var x = 5; x", res: "5"},
		{src: "var a = 1; func f() int { var a, b int = 3, 4; return a+b}; a+f()", res: "8"},
		{src: `var (
	a, b int = 4+1, 3
	c = 8
); a+b+c`, res: "16"},
	})
}
