package parser_test

import (
	"testing"

	"github.com/mvertes/parscan/parser"
)

func TestDump(t *testing.T) {
	initProgram := "var a int = 2+1; a"
	interp := parser.NewInterpreter(GoScanner)
	r, e := interp.Eval(initProgram)
	t.Log(r, e)
	if e != nil {
		t.Fatal(e)
	}

	r, e = interp.Eval("a = 100")
	t.Log(r, e)
	if e != nil {
		t.Fatal(e)
	}

	d := interp.Dump()
	t.Log(d)

	interp = parser.NewInterpreter(GoScanner)
	r, e = interp.Eval(initProgram)
	t.Log(r, e)
	if e != nil {
		t.Fatal(e)
	}

	e = interp.ApplyDump(d)
	if e != nil {
		t.Fatal(e)
	}

	r, e = interp.Eval("a = a + 1;a")
	t.Log(r, e)
	if e != nil {
		t.Fatal(e)
	}

	if r.Interface() != int(101) {
		t.Fatalf("unexpected result: %v", r)
	}
}
