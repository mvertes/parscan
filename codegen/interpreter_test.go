package codegen

import (
	"fmt"
	"testing"

	"github.com/gnolang/parscan/lang/golang"
)

func TestEval(t *testing.T) {
	for _, test := range evalTests {
		test := test
		t.Run("", func(t *testing.T) {
			interp := NewInterpreter(golang.GoParser)
			errStr := ""
			r, e := interp.Eval(test.src)
			if e != nil {
				errStr = e.Error()
			}
			if errStr != test.err {
				t.Errorf("got error %#v, want error %#v", errStr, test.err)
			}
			res := fmt.Sprintf("%v", r)
			if res != test.res {
				t.Errorf("got %#v, want %#v", res, test.res)
			}
		})
	}
}

var evalTests = []struct {
	name, src, res, err string
}{{ // #00
	src: "1 + 2",
	res: "3",
}, { // #01
	src: "a := 2; a = a + 3",
	res: "5",
}, { // #02
	src: "func f(a int) int { return a + 1 }; f(5)",
	res: "6",
}, { // #03
	src: "func f(a int) (b int) { b = a + 1; return b }; f(5)",
	res: "6",
}, { // #04
	src: "func f(a int) (b int) { b = a + 1; return }; f(5)",
	res: "6",
}}
