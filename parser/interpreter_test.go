package parser_test

import (
	"fmt"
	"log"
	"testing"

	"github.com/gnolang/parscan/lang/golang"
	"github.com/gnolang/parscan/parser"
	"github.com/gnolang/parscan/scanner"
)

type etest struct{ src, res, err string }

var GoScanner *scanner.Scanner

func init() {
	log.SetFlags(log.Lshortfile)
	GoScanner = scanner.NewScanner(golang.GoSpec)
}

func run(t *testing.T, tests []etest) {
	for _, test := range tests {
		test := test
		t.Run("", func(t *testing.T) {
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
		})
	}
}

func TestExpr(t *testing.T) {
	run(t, []etest{
		{src: "", res: "<nil>"},
		{src: "1+2", res: "3"},
		{src: "1+", err: "block not terminated"},
		{src: "a := 1 + 2; b := 0; a + 1", res: "4"},
		{src: "1+(2+3)", res: "6"},
		{src: "(1+2)+3", res: "6"},
		{src: "(6+(1+2)+3)+5", res: "17"},
		{src: "(6+(1+2+3)+5", err: "1:1: block not terminated"},
		{src: "a := 2; a = 3; a", res: "3"},
	})
}

func TestFunc(t *testing.T) {
	run(t, []etest{
		{src: "func f() int {return 2}; a := f(); a", res: "2"},
		{src: "func f() int {return 2}; f()", res: "2"},
		{src: "func f(a int) int {return a+2}; f(3)", res: "5"},
		{src: "func f(a int) int {if a < 4 {a = 5}; return a }; f(3)", res: "5"},
		{src: "func f(a int) int {return a+2}; 7 - f(3)", res: "2"},
		{src: "func f(a int) int {return a+2}; f(5) - f(3)", res: "2"},
		{src: "func f(a int) int {return a+2}; f(3) - 2", res: "3"},
		{src: "func f(a int, b int, c int) int {return a+b-c} ; f(7, 1, 3)", res: "5"},
	})
}

func TestIf(t *testing.T) {
	run(t, []etest{
		{src: "a := 0; if a == 0 { a = 2 } else { a = 1 }; a", res: "2"},
		{src: "a := 0; if a == 1 { a = 2 } else { a = 1 }; a", res: "1"},
		{src: "a := 0; if a == 1 { a = 2 } else if a == 0 { a = 3 } else { a = 1 }; a", res: "3"},
		{src: "a := 0; if a == 1 { a = 2 } else if a == 2 { a = 3 } else { a = 1 }; a", res: "1"},
	})
}

func TestFor(t *testing.T) {
	run(t, []etest{
		{src: "a := 0; for i := 0; i < 3; i = i+1 {a = a+i}; a", res: "3"},
		{src: "func f() int {a := 0; for i := 0; i < 3; i = i+1 {a = a+i}; return a}; f()", res: "3"},
		{src: "a := 0; for {a = a+1; if a == 3 {break}}; a", res: "3"},
		{src: "func f() int {a := 0; for {a = a+1; if a == 3 {break}}; return a}; f()", res: "3"},
	})
}
