package interp_test

import (
	"fmt"
	"log"
	"strings"
	"testing"

	"github.com/mvertes/parscan/interp"
	"github.com/mvertes/parscan/lang/golang"
)

type etest struct {
	n, src, res, err string
	skip             bool
}

func init() {
	log.SetFlags(log.Lshortfile)
}

func gen(test etest) func(*testing.T) {
	return func(t *testing.T) {
		t.Parallel()
		if test.skip {
			t.Skip()
		}
		intp := interp.NewInterpreter(golang.GoSpec)
		errStr := ""
		r, e := intp.Eval(test.src)
		t.Log(r, e)
		if e != nil {
			errStr = e.Error()
		}
		if !strings.Contains(errStr, test.err) {
			t.Errorf("got error %#v, want error %#v", errStr, test.err)
		}
		if res := fmt.Sprintf("%v", r); test.err == "" && res != test.res {
			t.Errorf("got %#v, want %#v", res, test.res)
		}
	}
}

func run(t *testing.T, tests []etest) {
	for _, test := range tests {
		t.Run(test.n, gen(test))
	}
}

func TestExpr(t *testing.T) {
	run(t, []etest{
		{n: "#00", src: "", res: "<invalid reflect.Value>"},
		{n: "#01", src: "1+2", res: "3"},
		{n: "#02", src: "1+", err: "block not terminated"},
		{n: "#03", src: "a := 1 + 2; b := 0; a + 1", res: "4"},
		{n: "#04", src: "1+(2+3)", res: "6"},
		{n: "#05", src: "(1+2)+3", res: "6"},
		{n: "#06", src: "(6+(1+2)+3)+5", res: "17"},
		{n: "#07", src: "(6+(1+2+3)+5", err: "1:1: block not terminated"},
		{n: "#08", src: "a := 2; a = 3; a", res: "3"},
		{n: "#09", src: "2 * 3 + 1 == 7", res: "true"},
		{n: "#10", src: "7 == 2 * 3 + 1", res: "true"},
		{n: "#11", src: "1 + 3 * 2 == 2 * 3 + 1", res: "true"},
		{n: "#12", src: "a := 1 + 3 * 2 == 2 * 3 + 1; a", res: "true"},
		{n: "#13", src: "-2", res: "-2"},
		{n: "#14", src: "-2 + 5", res: "3"},
		{n: "#15", src: "5 + -2", res: "3"},
		{n: "#16", src: "!false", res: "true"},
		{n: "#17", src: `a := "hello"`, res: "hello"},
	})
}

func TestAssign(t *testing.T) {
	run(t, []etest{
		{n: "#00", src: "var a int = 1; a", res: "1"},
		{n: "#01", src: "var a, b int = 1, 2; b", res: "2"},
		{n: "#02", src: "var a, b int; a, b = 1, 2; b", res: "2"},
		{n: "#03", src: "a, b := 1, 2; b", res: "2"},
		{n: "#04", src: "func f() int {return 2}; a := f(); a", res: "2"},
		{n: "#05", src: "func f() (int, int) {return 2, 3}; a, b := f(); b", res: "3"},
		{n: "#06", src: "func f() (int, int) {return 2, 3}; var a, b = f(); b", res: "3"},
		{n: "#07", src: "func f() (int, int) {return 2, 3}; _, b := f(); b", res: "3"},
	})
}

func TestCompare(t *testing.T) {
	run(t, []etest{
		{n: "#00", src: "a := 1; a < 2", res: "true"},
	})
}

func TestLogical(t *testing.T) {
	run(t, []etest{
		{n: "#00", src: "true && false", res: "false"},
		{n: "#01", src: "true && true", res: "true"},
		{n: "#02", src: "true && true && false", res: "false"},
		{n: "#03", src: "false || true && true", res: "true"},
		{n: "#04", src: "2 < 3 && 1 > 2 || 3 == 3", res: "true"},
		{n: "#05", src: "2 > 3 && 1 > 2 || 3 == 3", res: "true"},
		{n: "#06", src: "2 > 3 || 2 == 1+1 && 3>0", res: "true"},
		{n: "#07", src: "2 > 3 || 2 == 1+1 && 3>4 || 1<2", res: "true"},
		{n: "#08", src: "a := 1+1 < 3 && 4 == 2+2; a", res: "true"},
		{n: "#09", src: "a := 1+1 < 3 || 3 == 2+2; a", res: "true"},
		{n: "#10", src: "a := 1+1 < 3 && 4 == 2+2; a", res: "true"},
		{n: "#11", src: "a := 1+1 < 3 || 3 == 2+2; a", res: "true"},
	})
}

func TestFunc(t *testing.T) {
	run(t, []etest{
		{n: "#00", src: "func f() int {return 2}; a := f(); a", res: "2"},
		{n: "#01", src: "func f() int {return 2}; f()", res: "2"},
		{n: "#02", src: "func f(a int) int {return a+2}; f(3)", res: "5"},
		{n: "#03", src: "func f(a int) int {if a < 4 {a = 5}; return a}; f(3)", res: "5"},
		{n: "#04", src: "func f(a int) int {return a+2}; 7 - f(3)", res: "2"},
		{n: "#05", src: "func f(a int) int {return a+2}; f(5) - f(3)", res: "2"},
		{n: "#06", src: "func f(a int) int {return a+2}; f(3) - 2", res: "3"},
		{n: "#07", src: "func f(a, b, c int) int {return a+b-c} ; f(7, 1, 3)", res: "5"},
		{n: "#08", src: "var a int; func f() {a = a+2}; f(); a", res: "2"},
		{n: "#09", src: "var f = func(a int) int {return a+3}; f(2)", res: "5"},
		{n: "#10", src: "var a int; func f(a int) {a = a+2}; f(); a", res: "0"},
		{n: "#11", src: "func f(a int) {a = a+2}; a := 1; f(); a", res: "1"},
	})
}

func TestIf(t *testing.T) {
	run(t, []etest{
		{n: "#00", src: "a := 0; if a == 0 { a = 2 } else { a = 1 }; a", res: "2"},
		{n: "#01", src: "a := 0; if a == 1 { a = 2 } else { a = 1 }; a", res: "1"},
		{n: "#02", src: "a := 0; if a == 1 { a = 2 } else if a == 0 { a = 3 } else { a = 1 }; a", res: "3"},
		{n: "#03", src: "a := 0; if a == 1 { a = 2 } else if a == 2 { a = 3 } else { a = 1 }; a", res: "1"},
		{n: "#04", src: "a := 1; if a > 0 && a < 2 { a = 3 }; a", res: "3"},
		{n: "#05", src: "a := 1; if a < 0 || a < 2 { a = 3 }; a", res: "3"},
	})
}

func TestFor(t *testing.T) {
	run(t, []etest{
		{n: "#00", src: "a := 0; for i := 0; i < 3; i = i+1 {a = a+i}; a", res: "3"},
		{n: "#01", src: "func f() int {a := 0; for i := 0; i < 3; i = i+1 {a = a+i}; return a}; f()", res: "3"},
		{n: "#02", src: "a := 0; for {a = a+1; if a == 3 {break}}; a", res: "3"},
		{n: "#03", src: "func f() int {a := 0; for {a = a+1; if a == 3 {break}}; return a}; f()", res: "3"},
		{n: "#04", src: "func f() int {a := 0; for {a = a+1; if a < 3 {continue}; break}; return a}; f()", res: "3"},
		{n: "#05", src: "a := []int{1,2,3,4}; b := 0; for i := range a {b = b+i}; b", res: "6"},
		{n: "#06", src: "func f() int {a := []int{1,2,3,4}; b := 0; for i := range a {b = b+i}; return b}; f()", res: "6"},
		// {n: "#07", src: "a := []int{1,2,3,4}; b := 0; for i, e := range a {b = b+i+e}; b", res: "16"},
	})
}

func TestGoto(t *testing.T) {
	run(t, []etest{
		{n: "#00", src: `
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
		{n: "#00", src: src0 + "f(1)", res: "2"},
		{n: "#01", src: src0 + "f(2)", res: "3"},
		{n: "#02", src: src0 + "f(3)", res: "5"},
		{n: "#03", src: src0 + "f(4)", res: "10"},
		{n: "#04", src: src0 + "f(5)", res: "0"},

		{n: "#05", src: src1 + "f(1)", res: "2"},
		{n: "#06", src: src1 + "f(4)", res: "5"},
		{n: "#07", src: src1 + "f(6)", res: "0"},
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
		{n: "#00", src: "const a = 1+2; a", res: "3"},
		{n: "#01", src: "const a, b = 1, 2; a+b", res: "3"},
		{n: "#02", src: "const huge = 1 << 100; const four = huge >> 98; four", res: "4"},

		{n: "#03", src: src0 + "c", res: "2"},
	})
}

func TestArray(t *testing.T) {
	run(t, []etest{
		{n: "#00", src: "type T []int; var t T; t", res: "[]"},
		{n: "#01", src: "type T [3]int; var t T; t", res: "[0 0 0]"},
		{n: "#02", src: "type T [3]int; var t T; t[1]", res: "0"},
		{n: "#03", src: "type T [3]int; var t T; t[1] = 2; t", res: "[0 2 0]"},
	})
}

func TestPointer(t *testing.T) {
	run(t, []etest{
		{n: "#00", src: "var a *int; a", res: "<nil>"},
		{n: "#01", src: "var a int; var b *int = &a; *b", res: "0"},
		{n: "#02", src: "var a int = 2; var b *int = &a; *b", res: "2"},
	})
}

func TestStruct(t *testing.T) {
	run(t, []etest{
		{n: "#00", src: "type T struct {a string; b, c int}; var t T; t", res: "{ 0 0}"},
		{n: "#01", src: "type T struct {a int}; var t T; t.a", res: "0"},
		{n: "#02", src: "type T struct {a int}; var t T; t.a = 1; t.a", res: "1"},
		{n: "#03", src: "type T struct {a int}; var t T = T{1}; t.a", res: "1"},
		{n: "#04", src: "type T struct {a int}; var t *T = &T{1}; t.a", res: "1"},
	})
}

func TestMap(t *testing.T) {
	src0 := `type M map[string]bool;`
	run(t, []etest{
		{n: "#00", src: src0 + `var m M; m`, res: `map[]`},
		{n: "#01", src: `m := map[string]bool{"foo": true}; m["foo"]`, res: `true`},
		{n: "#02", src: src0 + `m := M{"xx": true}; m`, res: `map[xx:true]`},
		{n: "#03", src: src0 + `var m = M{"xx": true}; m`, res: `map[xx:true]`},
		{n: "#04", src: src0 + `var m = M{"xx": true}; m["xx"] = false`, res: `map[xx:false]`},
	})
}

func TestSlice(t *testing.T) {
	src0 := `s := []int{0, 1, 2, 3};`
	run(t, []etest{
		{n: "#00", src: src0 + `s`, res: `[0 1 2 3]`},
		{n: "#01", src: src0 + `s[:]`, res: `[0 1 2 3]`},
		{n: "#02", src: src0 + `s[1:3]`, res: `[1 2]`},
		{n: "#03", src: src0 + `s[1:3:4]`, res: `[1 2]`},
		{n: "#04", src: src0 + `s[:3:4]`, res: `[0 1 2]`},
		{n: "#05", src: src0 + `s[:2:]`, err: `final index required in 3-index slice`},
		{n: "#06", src: src0 + `s[:3:4:]`, err: `expected ']', found ':'`},
		{n: "#07", src: src0 + `s[2:]`, res: `[2 3]`},
		{n: "#08", src: src0 + `s[:0]`, res: `[]`},
		{n: "#09", src: `"Hello"[1:3]`, res: `el`},
		{n: "#10", src: `s := "Hello"; s[1:3]`, res: `el`},
	})
}

func TestType(t *testing.T) {
	src0 := `type (
	I int
	S string
)
`
	run(t, []etest{
		{n: "#00", src: "type t int; var a t = 1; a", res: "1"},
		{n: "#01", src: "type t = int; var a t = 1; a", res: "1"},
		{n: "#02", src: src0 + `var s S = "xx"; s`, res: "xx"},
	})
}

func TestVar(t *testing.T) {
	run(t, []etest{
		{n: "#00", src: "var a int; a", res: "0"},
		{n: "#01", src: "var a, b, c int; a", res: "0"},
		{n: "#02", src: "var a, b, c int; a + b", res: "0"},
		{n: "#03", src: "var a, b, c int; a + b + c", res: "0"},
		{n: "#04", src: "var a int = 2+1; a", res: "3"},
		{n: "#05", src: "var a, b int = 2, 5; a+b", res: "7"},
		{n: "#06", src: "var x = 5; x", res: "5"},
		{n: "#07", src: "var a = 1; func f() int { var a, b int = 3, 4; return a+b}; a+f()", res: "8"},
		{n: "#08", src: `var a = "hello"; a`, res: "hello"},
		{n: "#09", src: `var ( a, b int = 4+1, 3; c = 8); a+b+c`, res: "16"},
	})
}

func TestImport(t *testing.T) {
	src0 := `import (
	"fmt"
)
`
	run(t, []etest{
		{n: "#00", src: "fmt.Println(4)", err: "invalid symbol: fmt"},
		{n: "#01", src: `import "xxx"`, err: "package not found: xxx"},
		{n: "#02", src: `import "fmt"; fmt.Println(4)`, res: "<nil>"},
		{n: "#03", src: src0 + "fmt.Println(4)", res: "<nil>"},
		{n: "#04", src: `func main() {import "fmt"; fmt.Println("hello")}`, err: "unexpected import"},
		{n: "#05", src: `import m "fmt"; m.Println(4)`, res: "<nil>"},
		{n: "#06", src: `import . "fmt"; Println(4)`, res: "<nil>"},
	})
}

func TestComposite(t *testing.T) {
	run(t, []etest{
		{n: "#00", src: "type T struct{}; t := T{}; t", res: "{}"},
		{n: "#01", src: "t := struct{}{}; t", res: "{}"},
		{n: "#02", src: `type T struct {}; var t T; t = T{}; t`, res: "{}"},
		{n: "#03", src: `type T struct{N int; S string}; var t T; t = T{2, "foo"}; t`, res: `{2 foo}`},
		{n: "#04", src: `type T struct{N int; S string}; t := T{2, "foo"}; t`, res: `{2 foo}`},
		{n: "#05", src: `type T struct{N int; S string}; t := T{S: "foo"}; t`, res: `{0 foo}`},
		{n: "#06", src: `a := []int{}`, res: `[]`},
		{n: "#07", src: `a := []int{1, 2, 3}; a`, res: `[1 2 3]`},
		{n: "#08", src: `m := map[string]bool{}`, res: `map[]`},
		{n: "#09", src: `m := map[string]bool{"hello": true}; m`, res: `map[hello:true]`},
		{n: "#10", src: `m := map[int]struct{b bool}{1:struct {b bool}{true}}; m`, res: `map[1:{true}]`},
		{n: "#11", src: `type T struct {b bool}; m := []T{T{true}}; m`, res: `[{true}]`},
		{n: "#12", src: `type T struct {b bool}; m := []T{{true}}; m`, res: `[{true}]`},
		{n: "#13", src: `m := []struct{b bool}{{true}}; m`, res: `[{true}]`},
		{n: "#14", src: `m := map[int]struct{b bool}{1:{true}}; m`, res: `map[1:{true}]`},
		{n: "#15", src: `type T *struct {b bool}; m := []T{{true}}; m[0]`, res: `&{true}`},
		{n: "#16", src: `type T *struct {b bool}; m := []T{{true}}; m[0].b`, res: `true`},
	})
}
