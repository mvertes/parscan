package codegen

import (
	"fmt"
	"log"
	"testing"

	"github.com/gnolang/parscan/lang/golang"
	"github.com/gnolang/parscan/parser"
	"github.com/gnolang/parscan/vm1"
)

func TestCodeGen(t *testing.T) {
	log.SetFlags(log.Lshortfile)
	for _, test := range tests {
		test := test
		t.Run("", func(t *testing.T) {
			c := New()
			c.AddSym(fmt.Println, "println", false)
			n := &parser.Node{}
			var err error
			if n.Child, err = golang.GoParser.Parse(test.src); err != nil {
				t.Error(err)
			}
			errStr := ""
			if err = c.CodeGen(n); err != nil {
				errStr = err.Error()
			}
			if errStr != test.err {
				t.Errorf("got error %#v, want error %#v", errStr, test.err)
			}
			t.Log("data:", c.Data)
			t.Log("code:", vm1.Disassemble(c.Code))
			if s := vm1.Disassemble(c.Code); s != test.asm {
				t.Errorf("got error %#v, want error %#v", s, test.asm)
			}
		})
	}
}

var tests = []struct {
	src, asm, sym, err string
}{{ // #00
	src: "1+2",
	asm: "Push 1\nPush 2\nAdd\n",
}, { // #01
	src: `println("Hello")`,
	asm: "Dup 0\nDup 1\nCallX 1\n",
}, { // #02
	src: `a := 2; println(a)`,
	asm: "Push 2\nAssign 1\nDup 0\nDup 1\nCallX 1\n",
}, { // #03
	src: `a := 2; if a < 3 {println(a)}; println("bye")`,
	asm: "Push 2\nAssign 1\nDup 1\nPush 3\nLower\nJumpFalse 4\nDup 0\nDup 1\nCallX 1\nDup 0\nDup 2\nCallX 1\n",
}, { // #04
	src: "func add(a int, b int) int { return a + b }",
	asm: "Fdup -2\nFdup -3\nAdd\nReturn 1 2\n",
}, {}}
