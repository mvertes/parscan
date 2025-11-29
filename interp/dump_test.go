package interp_test

import (
	"testing"

	"github.com/mvertes/parscan/interp"
	"github.com/mvertes/parscan/lang/golang"
)

func TestDump(t *testing.T) {
	initProgram := "var a int = 2+1; a"
	intp := interp.NewInterpreter(golang.GoSpec)
	r, e := intp.Eval(initProgram)
	t.Log(r, e)
	if e != nil {
		t.Fatal(e)
	}

	r, e = intp.Eval("a = 100")
	t.Log(r, e)
	if e != nil {
		t.Fatal(e)
	}

	d := intp.Dump()
	t.Log(d)

	intp = interp.NewInterpreter(golang.GoSpec)
	r, e = intp.Eval(initProgram)
	t.Log(r, e)
	if e != nil {
		t.Fatal(e)
	}

	e = intp.ApplyDump(d)
	if e != nil {
		t.Fatal(e)
	}

	r, e = intp.Eval("a = a + 1;a")
	t.Log(r, e)
	if e != nil {
		t.Fatal(e)
	}

	if r.Interface() != int(101) {
		t.Fatalf("unexpected result: %v", r)
	}
}
