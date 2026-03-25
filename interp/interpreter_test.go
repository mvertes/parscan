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
		r, e := intp.Eval("m:test", test.src)
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

const fibSrc = `
func fib(i int) int {
	if i < 2 { return i }
	return fib(i-2) + fib(i-1)
}
`

func BenchmarkFib(b *testing.B) {
	intp := interp.NewInterpreter(golang.GoSpec)
	if _, err := intp.Eval("m:fib", fibSrc); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := intp.Eval("m:bench", "fib(20)"); err != nil {
			b.Fatal(err)
		}
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
		{n: "#08", src: "func f() (int, int, int) {return 1, 2, 3}; a, b, c := f(); a*100+b*10+c", res: "123"},
		{n: "#09", src: "func f(x int) (int, int) {return x, x+1}; a, b := f(5); a*10+b", res: "56"},
		{n: "#10", src: "func f() (int, int) {return 2, 3}; func g() int { a, b := f(); return a+b }; g()", res: "5"},
		{n: "#11", src: "a, b := 1, 2; a, b = b, a; 10*a+b", res: "21"},
		{n: "#12", src: "func f() int { a, b := 1, 2; a, b = b, a; return 10*a+b }; f()", res: "21"},
		{n: "#13", src: "var g int; func f() int { l := 1; g, l = l, g; return 10*g+l }; g = 2; f()", res: "12"},
		{n: "#14", src: "_ = 1+1; 42", res: "42"},
		{n: "#15", src: "func f() (int, int) {return 2, 3}; var a, b int = f(); a+b", res: "5"},
	})
}

func TestCompare(t *testing.T) {
	run(t, []etest{
		{n: "#00", src: "a := 1; a < 2", res: "true"},
		// nil comparisons for nilable composite types
		{n: "nil_map_decl", src: "var m map[string]string; m == nil", res: "true"},
		{n: "nil_map_explicit", src: "var m map[string]string = nil; m == nil", res: "true"},
		{n: "nil_map_nonnnil", src: "m := map[string]string{}; m == nil", res: "false"},
		{n: "nil_slice_decl", src: "var s []int; s == nil", res: "true"},
		{n: "nil_slice_explicit", src: "var s []int = nil; s == nil", res: "true"},
		{n: "nil_slice_nonnil", src: "s := []int{}; s == nil", res: "false"},
		{n: "nil_ptr_decl", src: "var p *int; p == nil", res: "true"},
		{n: "nil_ptr_nonnil", src: "a := 1; p := &a; p == nil", res: "false"},
		{n: "nil_lhs", src: "var m map[string]int; nil == m", res: "true"},
		{n: "nil_neq_map", src: "var m map[string]string; m != nil", res: "false"},
		{n: "nil_neq_slice", src: "s := []int{1}; s != nil", res: "true"},
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
		{n: "#12", src: "func f1() bool {return true}; func f2() bool {return false}; a := f1() && f2(); a", res: "false"},
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
		// local variables
		{n: "#12", src: "func f(a int) int { b := a + 1; return b }; f(3)", res: "4"},
		{n: "#13", src: "func f(a int) int { var b int = a + 1; return b }; f(3)", res: "4"},
		{n: "#14", src: "func f() int { a := 1; b := 2; c := 3; return a+b+c }; f()", res: "6"},
		{n: "#15", src: "func f(a int) int { b := 0; b = a + 1; return b }; f(4)", res: "5"},
		// input parameters are pass-by-value
		{n: "#16", src: "func inc(a int) { a = 100 }; x := 5; inc(x); x", res: "5"},
		// recursion (requires correct local frame isolation per call)
		{n: "#17", src: "func fib(n int) int { if n < 2 { return n }; return fib(n-1) + fib(n-2) }; fib(6)", res: "8"},
		{n: "#18", src: "var a int; func f() { a:=2 }; f(); a", res: "0"},
		// var declaration without explicit type inside a function (undefinedType path)
		{n: "#19", src: "func f() int {var x = 42; return x}; f()", res: "42"},
		{n: "#20", src: "func f() int {var a, b = 2, 3; return 10*a+b}; f()", res: "23"},
	})
}

func TestOutOfOrder(t *testing.T) {
	run(t, []etest{
		// function declared after use
		{n: "#00", src: "func f() int { return g() }; func g() int { return 2 }; f()", res: "2"},
		// mutual recursion: even and odd call each other, both declared before use
		{n: "#01", src: "func even(n int) bool { if n == 0 { return true }; return odd(n-1) }; func odd(n int) bool { if n == 0 { return false }; return even(n-1) }; even(4)", res: "true"},
		// f calls two functions declared after it
		{n: "#02", src: "func f() int { return g() + h() }; func g() int { return 3 }; func h() int { return 4 }; f()", res: "7"},
		// three-level chain: a depends on b, b depends on c
		{n: "#03", src: "func a() int { return b() }; func b() int { return c() }; func c() int { return 7 }; a()", res: "7"},
		{n: "#04", src: `type T1 T; func foo() T1 {return T1(T{"foo"})}; type T struct {Name string}; foo().Name`, res: "foo"},

		// Deref of a global pointer var declared after the function using it.
		// Exercises the s.Type==nil guard in lang.Deref.
		{n: "deref_fwd", src: `
func f() int { return *p }
var n int = 42
var p = &n
f()`, res: "42"},

		// Method call on a global var declared after the function using it.
		// Exercises checkTopN(1) in lang.Period and the s.Type==nil guards.
		{n: "method_on_fwd_var", src: `
func bar() bool { return obj.Foo() }
type T struct{}
func (t *T) Foo() bool { return t != nil }
var obj = &T{}
bar()`, res: "true"},

		// Type declared after the const that uses it in an array size.
		{n: "const_before_type", src: `
const size = 3
type Vec struct { data [size]int }
len(Vec{}.data)`, res: "3"},

		// var with initializer declared after the func that uses it.
		{n: "var_init_after_func", src: `
func get() int { return x }
var x = 10
get()`, res: "10"},

		// Forward reference between vars in a var block.
		{n: "var_block_fwd_ref", src: `
var (
	a = b
	b = "hello"
)
a`, res: "hello"},

		// Dependency chain with function calls in a var block.
		{n: "var_block_dep_chain", src: `
var (
	a = concat("hello", b)
	b = concat(" ", c, "!")
	c = d
	d = "world"
)
func concat(a ...string) string {
	var s string
	for _, ss := range a { s += ss }
	return s
}
a`, res: "hello world!"},

		// Dependency chain across separate var declarations.
		{n: "separate_var_dep_chain", src: `
var a = concat("hello", b)
var b = concat(" ", c, "!")
var c = d
var d = "world"
func concat(a ...string) string {
	var s string
	for _, ss := range a { s += ss }
	return s
}
a`, res: "hello world!"},
	})
}

func TestVariadic(t *testing.T) {
	run(t, []etest{
		// Variadic with multiple args.
		{n: "#00", src: "func sum(a ...int) int { s := 0; for _, v := range a { s = s + v }; return s }; sum(1, 2, 3)", res: "6"},
		// Variadic with zero args.
		{n: "#01", src: "func sum(a ...int) int { s := 0; for _, v := range a { s = s + v }; return s }; sum()", res: "0"},
		// Variadic with one arg.
		{n: "#02", src: "func sum(a ...int) int { s := 0; for _, v := range a { s = s + v }; return s }; sum(42)", res: "42"},
		// Fixed params before variadic.
		{n: "#03", src: "func add(x int, rest ...int) int { s := x; for _, v := range rest { s = s + v }; return s }; add(10, 1, 2, 3)", res: "16"},
		// Fixed param, no variadic args.
		{n: "#04", src: "func add(x int, rest ...int) int { s := x; for _, v := range rest { s = s + v }; return s }; add(10)", res: "10"},
		// Variadic void function.
		{n: "#05", src: "var r int; func f(a ...int) { for _, v := range a { r = r + v } }; f(1, 2, 3); r", res: "6"},
	})
}

func TestFuncNamedReturn(t *testing.T) {
	run(t, []etest{
		{n: "#00", src: "func f(a int) (r int) { r = a + 2; return }; f(3)", res: "5"},
		{n: "#01", src: "func f(a int) (r int) { r = a; r = r + 2; return }; f(3)", res: "5"},
		{n: "#02", src: "func f(a int) (x, y int) { x = a; y = a + 1; return }; a, b := f(3); a+b", res: "7"},
		{n: "#03", src: "func f(a int) (r int) { return a + 2 }; f(3)", res: "5"},
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
		{n: "#06", src: `func f() (int, error) { return 3, nil }; r := 0; if a, err := f(); err != nil { r = 1 } else { r = a }; r`, res: "3"},
		{n: "#07", src: `func f() (int, error) { return 0, nil }; func g() ([]int, error) { return []int{1,2}, nil }; r := 0; if a, err := f(); err != nil { r = a } else if _, err2 := g(); err2 != nil { r = 1 } else { r = 3 }; r`, res: "3"},
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
		{n: "#07", src: "a := []int{1,2,3,4}; b := 0; for i, e := range a {b = b+i+e}; b", res: "16"},
		{n: "#08", src: "a := [4]int{1,2,3,4}; b := 0; for i, e := range a {b = b+i+e}; b", res: "16"},
		{n: "#09", src: "a:= 0; for i := 0; i < 10; i++ { if i < 5 {a++; continue}}; a", res: "5"},
		{n: "#10", src: `a := 0; Outer: for i := 0; i < 3; i++ { for j := 0; j < 3; j++ { if j == 1 { continue Outer }; a++ } }; a`, res: "3"},
		{n: "#11", src: `a := 0; Outer: for i := 0; i < 3; i++ { switch i { case 1: continue Outer }; a++ }; a`, res: "2"},
		{n: "#12", src: `s := "abc"; b := 0; for i := range s { b += i }; b`, res: "3"},
		{n: "#13", src: `s := "abc"; n := 0; for _, r := range s { n += int(r) }; n`, res: "294"},
		{n: "#14", src: `const s = "ab"; b := 0; for i, r := range s { b += i + int(r) }; b`, res: "196"},
		{n: "#15", src: `s := "a1b"; n := 0; for i, r := range s { if r == '1' { n = i } }; n`, res: "1"},
		{n: "#16", src: `b := 0; for i := range 4 { b += i }; b`, res: "6"},
		{n: "#17", src: `func f() int { b := 0; for i := range 4 { b += i }; return b }; f()`, res: "6"},
		{n: "#21", src: `n := 0; for range []int{1,2,3} { n++ }; n`, res: "3"},
		{n: "#22", src: `for range []struct{}{} {}; true`, res: "true"},
		{n: "#23", src: `func f() bool { for range []struct{}{} {}; return true }; f()`, res: "true"},
		{n: "#24", src: `n := 0; for range 4 { n++ }; n`, res: "4"},
		{n: "#25", src: `n := 0; for range map[string]int{"a": 1, "b": 2} { n++ }; n`, res: "2"},
		{n: "#26", src: `m := map[string]int{"a": 1, "b": 2}; n := 0; for k := range m { n += len(k) }; n`, res: "2"},
		{n: "#27", src: `m := map[string]int{"a": 1, "b": 2}; n := 0; for _, v := range m { n += v }; n`, res: "3"},
		{n: "#18", src: `m := map[string]int{"a": 1}; v, ok := m["a"]; ok && v == 1`, res: "true"},
		{n: "#19", src: `m := map[string]int{"a": 1}; v, ok := m["b"]; !ok && v == 0`, res: "true"},
		{n: "#20", src: `
func f() string {
	s := make([]map[string]string, 0)
	m := make(map[string]string)
	m["m1"] = "m1"
	s = append(s, m)
	tmpStr := "start"
	for _, v := range s {
		tmpStr, _ := v["m1"]
		_ = tmpStr
	}
	return tmpStr
}
f()`, res: "start"},
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
	src2 := `func f(a int) int {
	switch a {
	case 1: a = 10; fallthrough
	case 2: a++
	case 3: a = 30
	}
	return a
}
`
	src3 := `func f(a int) int {
	switch a {
	case 1,2: fallthrough
	case 3:   a = 99
	case 4:   a = 0
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

		{n: "#08", src: src2 + "f(1)", res: "11"},
		{n: "#09", src: src2 + "f(2)", res: "3"},
		{n: "#10", src: src2 + "f(3)", res: "30"},

		{n: "#11", src: src3 + "f(1)", res: "99"},
		{n: "#12", src: src3 + "f(2)", res: "99"},
		{n: "#13", src: src3 + "f(3)", res: "99"},
		{n: "#14", src: src3 + "f(4)", res: "0"},
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
		{n: "#04", src: `func f() string {return a}; const a = "hello"; f()`, res: "hello"},

		// Forward references within a const block.
		{n: "fwd_in_block", src: `const (a = 2; b = c + d; c = 4; d = 5); b`, res: "9"},
		// Deep forward reference chain within a const block.
		{n: "fwd_deep_chain", src: `const (a = 2; b = c + d; c = a + d; d = e + f; e = 3; f = 4); b`, res: "16"},
		// Forward references across separate const blocks.
		{n: "fwd_cross_block", src: `const b = c + 1; const c = 5; b`, res: "6"},
		// Const used in array size, declared after the type.
		{n: "fwd_array_size", src: `
const maxN = 30
const bufSize = maxN + 2
type T struct { pos uint8; size uint8 }
type buf struct { data [bufSize]T }
len(buf{}.data)`, res: "32"},

		// Builtin len in const expression.
		{n: "len_string", src: `const n = len("hello"); n`, res: "5"},
		{n: "len_string_expr", src: `const n = len("hello") + 1; n`, res: "6"},

		// Type conversions in const expressions.
		{n: "conv_int", src: `const a = int(3.0); a`, res: "3"},
		{n: "conv_float", src: `const a = float64(3) + 0.5; a`, res: "3.5"},
		{n: "conv_string", src: `const a = string(65); a`, res: "A"},

		// len/cap on array-typed variables in const expressions.
		{n: "len_array_var", src: `var a = [3]int{1,2,3}; const n = len(a); n`, res: "3"},
		{n: "cap_array_var", src: `var a = [...]int{1,2,3}; const n = cap(a); n`, res: "3"},
	})
}

func TestArray(t *testing.T) {
	run(t, []etest{
		{n: "#00", src: "type T []int; var t T; t", res: "[]"},
		{n: "#01", src: "type T [3]int; var t T; t", res: "[0 0 0]"},
		{n: "#02", src: "type T [3]int; var t T; t[1]", res: "0"},
		{n: "#03", src: "type T [3]int; var t T; t[1] = 2; t", res: "[0 2 0]"},

		// [...] array syntax.
		{n: "ellipsis", src: `a := [...]int{10, 20, 30}; len(a)`, res: "3"},
	})
}

func TestPointer(t *testing.T) {
	run(t, []etest{
		{n: "#00", src: "var a *int; a", res: "<nil>"},
		{n: "#01", src: "var a int; var b *int = &a; *b", res: "0"},
		{n: "#02", src: "var a int = 2; var b *int = &a; *b", res: "2"},

		// DerefAssign: *p = value (simple).
		{n: "deref_assign_int", src: "var a int; p := &a; *p = 42; a", res: "42"},
		{n: "deref_assign_string", src: "var s string; p := &s; *p = \"hello\"; s", res: "hello"},

		// DerefAssign: *p = expr (RHS is an arithmetic expression).
		{n: "deref_assign_expr", src: "var a int; p := &a; *p = 3 + 4; a", res: "7"},

		// DerefAssign: *f() = value (pointer returned by function).
		{n: "deref_assign_func", src: `
var a int
func f() *int { return &a }
*f() = 99; a`, res: "99"},

		// DerefAssign: *s[i] = value (pointer in a slice).
		{n: "deref_assign_slice", src: `
var a, b int
s := []*int{&a, &b}
*s[1] = 10; b`, res: "10"},

		// Interior deref: (*p).field = value (deref is not outermost).
		{n: "deref_field_assign", src: `
type T struct { x int }
p := &T{0}
(*p).x = 5; p.x`, res: "5"},

		// Interior deref: (*p)[i] = value.
		{n: "deref_index_assign", src: `
s := []int{1, 2, 3}
p := &s
(*p)[1] = 20; s[1]`, res: "20"},

		// Auto-deref: p.field = value (Go implicit deref for field access).
		{n: "auto_deref_field", src: `
type T struct { x int }
p := &T{0}
p.x = 7; p.x`, res: "7"},

		// Double pointer deref: **pp = value.
		{n: "deref_double", src: `
var a int
p := &a
pp := &p
**pp = 33; a`, res: "33"},

		// DerefAssign with new().
		{n: "deref_assign_new", src: "p := new(int); *p = 5; *p", res: "5"},

		// Pointer inc/dec: *p++ and *p--.
		{n: "deref_inc", src: "a := 2; p := &a; *p++; a", res: "3"},
		{n: "deref_dec", src: "a := 2; p := &a; *p--; a", res: "1"},
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

func TestEmbeddedStruct(t *testing.T) {
	run(t, []etest{
		{n: "field", src: `
type Base struct { X int }
type T struct { Base; Y int }
var t T; t.X = 1; t.Y = 2; t.X`, res: "1"},

		{n: "literal", src: `
type Base struct { X int }
type T struct { Base; Y int }
t := T{Base{10}, 20}; t.X`, res: "10"},

		{n: "method", src: `
type Base struct { X int }
func (b Base) GetX() int { return b.X }
type T struct { Base; Y int }
t := T{Base{7}, 0}; t.GetX()`, res: "7"},

		{n: "iface", src: `
type Getter interface { GetX() int }
type Base struct { X int }
func (b Base) GetX() int { return b.X }
type T struct { Base }
var g Getter = T{Base{42}}
g.GetX()`, res: "42"},

		{n: "override", src: `
type Base struct { X int }
func (b Base) GetX() int { return b.X }
type T struct { Base }
func (t T) GetX() int { return t.X * 10 }
t := T{Base{3}}; t.GetX()`, res: "30"},

		{n: "nested", src: `
type A struct { V int }
type B struct { A }
type C struct { B }
c := C{B{A{99}}}; c.V`, res: "99"},

		{n: "ptr_field", src: `
type Base struct { X int }
type T struct { *Base }
t := T{&Base{5}}; t.X`, res: "5"},

		{n: "ptr_set", src: `
type Base struct { X int }
type T struct { *Base }
t := T{&Base{0}}; t.X = 42; t.X`, res: "42"},

		{n: "ptr_method", src: `
type Base struct { X int }
func (b Base) GetX() int { return b.X }
type T struct { *Base }
t := T{&Base{8}}; t.GetX()`, res: "8"},

		{n: "ptr_recv_method", src: `
type Base struct { X int }
func (b *Base) SetX(v int) { b.X = v }
type T struct { *Base }
t := T{&Base{0}}; t.SetX(99); t.X`, res: "99"},

		{n: "ptr_iface", src: `
type Getter interface { GetX() int }
type Base struct { X int }
func (b *Base) GetX() int { return b.X }
type T struct { *Base }
var g Getter = T{&Base{55}}
g.GetX()`, res: "55"},

		{n: "ptr_nested", src: `
type A struct { V int }
type B struct { *A }
type C struct { B }
c := C{B{&A{77}}}; c.V`, res: "77"},
	})
}

func TestMethodResolve(t *testing.T) {
	run(t, []etest{
		{n: "val_on_val", src: `
type T struct { X int }
func (t T) GetX() int { return t.X }
v := T{3}; v.GetX()`, res: "3"},

		{n: "val_on_ptr", src: `
type T struct { X int }
func (t T) GetX() int { return t.X }
p := &T{5}; p.GetX()`, res: "5"},

		{n: "ptr_on_ptr", src: `
type T struct { X int }
func (t *T) SetX(v int) { t.X = v }
p := &T{0}; p.SetX(7); p.X`, res: "7"},

		{n: "ptr_on_val", src: `
type T struct { X int }
func (t *T) SetX(v int) { t.X = v }
var v T; v.SetX(9); v.X`, res: "9"},

		{n: "both", src: `
type T struct { X int }
func (t T) GetX() int { return t.X }
func (t *T) Double() { t.X = t.X * 2 }
var v T; v.X = 4; v.Double(); v.GetX()`, res: "8"},

		{n: "iface_val_recv", src: `
type Getter interface { GetX() int }
type T struct { X int }
func (t T) GetX() int { return t.X }
var g Getter = &T{6}
g.GetX()`, res: "6"},

		{n: "iface_ptr_recv", src: `
type Setter interface { SetX(int) }
type T struct { X int }
func (t T) GetX() int { return t.X }
func (t *T) SetX(v int) { t.X = v }
var t0 = &T{0}
var s Setter = t0
s.SetX(11)
t0.GetX()`, res: "11"},

		{n: "named_val_on_val", src: `
type N int
func (n N) IsPos() bool { return int(n) > 0 }
v := N(5); v.IsPos()`, res: "true"},

		{n: "named_val_on_ptr", src: `
type N int
func (n N) IsPos() bool { return int(n) > 0 }
p := new(N); *p = N(3); p.IsPos()`, res: "true"},

		{n: "named_ptr_on_val", src: `
type N int
func (n *N) Inc() { *n = *n + 1 }
var v N = 10; v.Inc(); v`, res: "11"},

		{n: "local_var", src: `
type T struct { X int }
func (t T) GetX() int { return t.X }
func f() int { v := T{42}; return v.GetX() }
f()`, res: "42"},

		{n: "field_access", src: `
type Coord struct { x, y int }
func (c Coord) dist() int { return c.x*c.x + c.y*c.y }
type Point struct { Coord; z int }
o := Point{Coord{3, 4}, 5}
o.Coord.dist()`, res: "25"},

		{n: "slice_elem", src: `
type S struct { X int }
func (s S) GetX() int { return s.X }
a := []S{S{7}, S{9}}
a[0].GetX()`, res: "7"},
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
		{n: "#05", src: "var m map[string]int64; func f() {m = make(map[string]int64)}; f(); len(m)", res: "0"},
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
		{n: "#11", src: src0 + `z := s[1:3]; z`, res: `[1 2]`},
		{n: "#12", src: `s := "Hello"; z := s[1:3]; z`, res: `el`},
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
		{n: "named_arith", src: "type t int; var a, b t = 3, 4; a + b", res: "7"},
		{n: "named_conv", src: "type t int; t(42)", res: "42"},
		{n: "named_method", src: "type t int; func (v t) Double() int { return int(v) * 2 }; var a t = 5; a.Double()", res: "10"},
		{n: "const_len", src: `
type t1 uint8
const (
	n1 t1 = iota
	n2
)
type T struct { elem [n2 + 1]int }
len(T{}.elem)`, res: "2"},
		{n: "alias", src: "type Number = int; Number(1) < int(2)", res: "true"},
		{n: "local_shadow", src: `
type T struct { X int }
func f() int {
	type T struct { Y string }
	var v T
	v.Y = "hello"
	return len(v.Y)
}
f()`, res: "5"},
		{n: "local_shadow_outer", src: `
type T struct { X int }
func f() { type T struct { Y string }; var v T; v.Y = "ok" }
f()
var t T
t.X = 99
t.X`, res: "99"},

		// Struct field name shadows a builtin type (e.g. rune).
		{n: "field_shadows_type", src: `
type P struct { pos uint8; size uint8 }
type buf struct { rune [3]P }
len(buf{}.rune)`, res: "3"},
	})
}

func TestInterface(t *testing.T) {
	run(t, []etest{
		{n: "basic", src: `
type Stringer interface { String() string }
type T int
func (t T) String() string { return "hello" }
var s Stringer = T(1)
s.String()`, res: "hello"},

		{n: "recv_value", src: `
type Doubler interface { Double() int }
type N int
func (n N) Double() int { return int(n) * 2 }
var d Doubler = N(5)
d.Double()`, res: "10"},

		{n: "reassign", src: `
type Doubler interface { Double() int }
type N int
func (n N) Double() int { return int(n) * 2 }
var d Doubler = N(3)
d = N(7)
d.Double()`, res: "14"},

		{n: "empty_iface", src: "type I interface {}; var x I; x", res: "<nil>"},

		{n: "struct_recv", src: `
type Getter interface { Get() int }
type S struct { n int }
func (s S) Get() int { return s.n }
var g Getter = S{42}
g.Get()`, res: "42"},

		{n: "iface_method", src: `
type I interface { inI() }
type T struct {name string}
func (t *T) inI() {}
var i I = &T{name: "foo"}
var r = ""
if i, ok := i.(*T); ok { r = i.name }
r`, res: "foo"},

		{n: "any_set", src: "var a interface{} = 2 + 5; a.(int)", res: "7"},

		// interface{} as function return type (was: syntax error: Interface)
		{n: "iface_return", src: `
func f(x int) interface{} { return x }
f(42).(int)`, res: "42"},

		// return concrete value from interface{} func, then type-assert
		{n: "iface_return_cap", src: `
func f(a []int) interface{} { return cap(a) }
a := []int{1, 2, 3}
f(a).(int)`, res: "3"},

		{n: "iface_return_multi", src: `
func f(x int) (interface{}, int) { return x, x + 1 }
a, b := f(5)
a.(int) + b`, res: "11"},

		// nil error interface: short-circuit prevents call on nil receiver
		{n: "error_nil_shortcircuit", src: `
var a error = nil
r := ""
if a == nil || a.Error() == "nil" { r = "nil" }
r`, res: "nil"},
	})
}

func TestTypeAssert(t *testing.T) {
	run(t, []etest{
		{n: "simple", src: `var i any = 42; i.(int)`, res: "42"},
		{n: "string", src: `var i any = "hello"; i.(string)`, res: "hello"},
		{n: "arith", src: `var i any = 42; i.(int) + 1`, res: "43"},

		{n: "ok_true", src: `var i any = 42; v, ok := i.(int); ok`, res: "true"},
		{n: "ok_val", src: `var i any = 42; v, ok := i.(int); v + 1`, res: "43"},

		{n: "ok_false", src: `var i any = 42; _, ok := i.(string); ok`, res: "false"},

		{n: "iface_assert", src: `
type Getter interface { Get() int }
type S struct { n int }
func (s S) Get() int { return s.n }
var g Getter = S{7}
v, ok := g.(S)
v.Get()`, res: "7"},

		{n: "iface_assert_ok", src: `
type Getter interface { Get() int }
type S struct { n int }
func (s S) Get() int { return s.n }
var g Getter = S{7}
_, ok := g.(S)
ok`, res: "true"},

		{n: "iface_assert_fail", src: `
type Getter interface { Get() int }
type Other struct { n int }
type S struct { n int }
func (s S) Get() int { return s.n }
var g Getter = S{7}
_, ok := g.(Other)
ok`, res: "false"},

		{n: "iface_to_iface", src: `
type Root struct { Name string }
type One struct { Root }
type Hi interface { Hello() string }
type Hey interface { Hello() string }
func (r *Root) Hello() string { return "Hello " + r.Name }
var one Hey = &One{Root{Name: "test2"}}
one.(Hi).Hello()`, res: "Hello test2"},

		{n: "iface_to_iface_ok", src: `
type Root struct { Name string }
type One struct { Root }
type Hi interface { Hello() string }
type Hey interface { Hello() string }
func (r *Root) Hello() string { return "Hello " + r.Name }
var one Hey = &One{Root{Name: "test2"}}
_, ok := one.(Hi)
ok`, res: "true"},

		{n: "iface_to_iface_fail", src: `
type S struct{}
type A interface { Foo() }
type B interface { Bar() }
func (s S) Foo() {}
var a A = S{}
_, ok := a.(B)
ok`, res: "false"},

		{
			n: "nil_panic", src: `var i any; i.(int)`,
			err: "panic: interface conversion: interface is nil, not int",
		},

		{n: "nil_recover", src: `
r := 0
func f() {
	defer func() { recover(); r = 1 }()
	var i any
	i.(int)
}
f()
r`, res: "1"},

		{
			n: "wrong_type_panic", src: `var i any = "hello"; i.(int)`,
			err: "panic: interface conversion: interface value is string, not int",
		},

		{n: "wrong_type_recover", src: `
r := 0
func f() {
	defer func() { recover(); r = 1 }()
	var i any = "hello"
	i.(int)
}
f()
r`, res: "1"},

		{n: "int64_return", src: `
func f1(a int) interface{} { return a + 1 }
func f2(a int64) interface{} { return a + 1 }
v1 := f1(3).(int)
v2 := f2(3).(int64)
v1 + int(v2)`, res: "8"},
	})
}

func TestTypeSwitch(t *testing.T) {
	run(t, []etest{
		{n: "no_bind_int", src: `var i any = 42; var r int; switch i.(type) { case int: r = 1 }; r`, res: "1"},
		{n: "no_bind_str", src: `var i any = "hi"; var r int; switch i.(type) { case int: r = 1; case string: r = 2 }; r`, res: "2"},
		{n: "no_bind_default", src: `var i any = true; var r int; switch i.(type) { case int: r = 1; default: r = 9 }; r`, res: "9"},

		{n: "bind_int", src: `var i any = 42; switch v := i.(type) { case int: v + 1 }`, res: "43"},
		{n: "bind_str", src: `var i any = "hi"; switch v := i.(type) { case string: v }`, res: "hi"},
		{n: "bind_second", src: `var i any = "hi"; switch v := i.(type) { case int: v; case string: v }`, res: "hi"},
		{n: "bind_default", src: `var i any = true; var r int; switch i.(type) { case int: r = 1; default: r = 9 }; r`, res: "9"},

		{n: "multi_int", src: `var i any = 42; var r int; switch i.(type) { case int, string: r = 1; default: r = 2 }; r`, res: "1"},
		{n: "multi_str", src: `var i any = "hi"; var r int; switch i.(type) { case int, string: r = 1; default: r = 2 }; r`, res: "1"},
		{n: "multi_default", src: `var i any = true; var r int; switch i.(type) { case int, string: r = 1; default: r = 2 }; r`, res: "2"},

		{n: "nil_match", src: `var i any; var r int; switch i.(type) { case nil: r = 1; default: r = 2 }; r`, res: "1"},
		{n: "nil_no_match", src: `var i any = 42; var r int; switch i.(type) { case nil: r = 1; default: r = 2 }; r`, res: "2"},

		{n: "iface_guard", src: `
type Getter interface { Get() int }
type S struct { n int }
func (s S) Get() int { return s.n }
var g Getter = S{7}
switch v := g.(type) { case S: v.Get() }`, res: "7"},
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
		{n: "#00", src: "fmt.Println(4)", err: "undefined: fmt"},
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
		{n: "#17", src: `a := [3]int{1, 2, 3}; a`, res: `[1 2 3]`},
	})
}

func TestClosure(t *testing.T) {
	run(t, []etest{
		// Reading outer scope (module-level) variable.
		{n: "#00", src: `a := 10; f := func() int { return a }; f()`, res: "10"},
		// Mutating outer scope variable.
		{n: "#01", src: `a := 5; f := func() { a = 20 }; f(); a`, res: "20"},
		// Closure with own params, also captures outer var.
		{n: "#02", src: `x := 3; f := func(n int) int { return x + n }; f(4)`, res: "7"},
		// Closure returned from anonymous func (inner captures global).
		{n: "#03", src: `a := 1; makeInc := func() func() int { return func() int { a = a+1; return a } }; inc := makeInc(); inc(); inc()`, res: "3"},
		// Closure stored as var then called.
		{n: "#04", src: `var f func(int) int; f = func(n int) int { return n*2 }; f(6)`, res: "12"},
		// Two closures sharing the same outer var.
		{n: "#05", src: `n := 0; inc := func() { n = n+1 }; get := func() int { return n }; inc(); inc(); get()`, res: "2"},
		// Closure capturing param of enclosing named func.
		{n: "#06", src: `func makeAdder(x int) func(int) int { return func(n int) int { return x + n } }; add5 := makeAdder(5); add5(3)`, res: "8"},
		// Counter pattern: closure captures and mutates enclosing local.
		{n: "#07", src: `func makeCounter() func() int { n := 0; return func() int { n = n+1; return n } }; c := makeCounter(); c(); c()`, res: "2"},
		// Per-iteration capture: each closure in a loop captures its own snapshot of the loop
		// variable (no aliasing to the shared frame slot).
		{n: "#08", src: `func f() int { var fns []func() int; for i := 0; i < 3; i++ { a := i; fns = append(fns, func() int { return i*10 + a }) }; return fns[0]() + fns[1]() + fns[2]() }; f()`, res: "33"},
		// Closure in struct func field appended to slice: funcFields keyed by address must
		// survive the struct copy that append does. All three closures must see their own i/a.
		{n: "#09", src: `
type T struct{ F func() int }
func f() int {
	var foos []T
	for i := 0; i < 3; i++ {
		a := i
		foos = append(foos, T{func() int { return i*10 + a }})
	}
	return foos[0].F() + foos[1].F() + foos[2].F()
}
f()`, res: "33"},
		// Closures in for-range-int loop each capture their own snapshot.
		{n: "#10", src: `
func f() int {
	var foos []func() int
	for i := range 3 {
		a := i
		foos = append(foos, func() int { return i*10 + a })
	}
	return foos[0]() + foos[1]() + foos[2]()
}
f()`, res: "33"},
	})
}

func TestMethod(t *testing.T) {
	run(t, []etest{
		// Value receiver, direct call.
		{n: "#00", src: `type I int; func(i I) F(a int) int { return a+i }; var i I = 1; i.F(2)`, res: "3"},
		// Multiple params.
		{n: "#01", src: `type I int; func(i I) Add(a, b int) int { return a + b }; var i I = 0; i.Add(3, 4)`, res: "7"},

		// Read single field.
		{n: "#02", src: `type T struct{n int}; func(t T) N() int { return t.n }; x := T{5}; x.N()`, res: "5"},
		// Read field, add param.
		{n: "#03", src: `type T struct{n int}; func(t T) Add(a int) int { return t.n + a }; x := T{3}; x.Add(4)`, res: "7"},
		// Two fields.
		{n: "#04", src: `type T struct{a, b int}; func(t T) Sum() int { return t.a + t.b }; x := T{2, 3}; x.Sum()`, res: "5"},

		// Store method value, call later.
		{n: "#05", src: `type I int; func(i I) F(a int) int { return a+i }; var i I = 2; f := i.F; f(3)`, res: "5"},
		// Two independent method values from different receivers.
		{n: "#06", src: `type I int; func(i I) Val() I { return i }; var a I = 1; var b I = 2; fa := a.Val; fb := b.Val; fa() + fb()`, res: "3"},
		// Pass method value to higher-order function.
		{n: "#07", src: `type I int; func(i I) F(a int) int { return a+i }; apply := func(f func(int) int, n int) int { return f(n) }; var i I = 5; apply(i.F, 3)`, res: "8"},
		// Method value on struct receiver.
		{n: "#08", src: `type T struct{n int}; func(t T) Add(a int) int { return t.n + a }; x := T{3}; f := x.Add; f(4)`, res: "7"},

		// Pointer receiver increments field.
		{n: "#09", src: `type T struct{n int}; func(t *T) Inc() { t.n = t.n + 1 }; var x T; x.Inc(); x.Inc(); x.n`, res: "2"},
		// Pointer receiver method value.
		{n: "#10", src: `type T struct{n int}; func(t *T) Inc() { t.n = t.n + 1 }; var x T; f := x.Inc; f(); f(); x.n`, res: "2"},

		// Method returning a closure that captures the receiver.
		{n: "#11", src: `type T struct{n int}; func(t T) Adder() func(int) int { return func(a int) int { return t.n + a } }; x := T{3}; add := x.Adder(); add(4)`, res: "7"},
	})
}

func TestArithInt(t *testing.T) {
	run(t, []etest{
		{n: "add", src: "3 + 4", res: "7"},
		{n: "add_neg", src: "-3 + 4", res: "1"},
		{n: "add_zero", src: "0 + 0", res: "0"},

		{n: "sub", src: "10 - 3", res: "7"},
		{n: "sub_neg_result", src: "3 - 10", res: "-7"},

		{n: "mul", src: "6 * 7", res: "42"},
		{n: "mul_zero", src: "42 * 0", res: "0"},
		{n: "mul_neg", src: "-3 * 4", res: "-12"},

		{n: "div", src: "10 / 3", res: "3"},
		{n: "div_exact", src: "12 / 4", res: "3"},
		{n: "div_neg", src: "-7 / 2", res: "-3"},
		{n: "div_neg2", src: "7 / -2", res: "-3"},

		{n: "rem", src: "10 % 3", res: "1"},
		{n: "rem_neg", src: "-10 % 3", res: "-1"},
		{n: "rem_exact", src: "12 % 4", res: "0"},

		{n: "negate", src: "-42", res: "-42"},
		{n: "negate_neg", src: "a := -1; -a", res: "1"},

		{n: "gt_true", src: "3 > 2", res: "true"},
		{n: "gt_false", src: "2 > 3", res: "false"},
		{n: "lt_true", src: "2 < 3", res: "true"},
		{n: "lt_false", src: "3 < 2", res: "false"},
		{n: "eq_true", src: "3 == 3", res: "true"},
		{n: "eq_false", src: "3 == 4", res: "false"},

		{n: "ge_true", src: "3 >= 3", res: "true"},
		{n: "ge_true2", src: "4 >= 3", res: "true"},
		{n: "ge_false", src: "2 >= 3", res: "false"},
		{n: "le_true", src: "3 <= 3", res: "true"},
		{n: "le_true2", src: "2 <= 3", res: "true"},
		{n: "le_false", src: "4 <= 3", res: "false"},
		{n: "ne_true", src: "3 != 4", res: "true"},
		{n: "ne_false", src: "3 != 3", res: "false"},

		{n: "max_int", src: "var a int = 9223372036854775807; a", res: "9223372036854775807"},
		{n: "min_int", src: "var a int = -9223372036854775808; a", res: "-9223372036854775808"},

		{n: "inc", src: "a := 5; a++; a", res: "6"},
		{n: "dec", src: "a := 5; a--; a", res: "4"},

		{n: "add_assign", src: "a := 5; a += 3; a", res: "8"},
		{n: "sub_assign", src: "a := 5; a -= 3; a", res: "2"},
		{n: "mul_assign", src: "a := 5; a *= 3; a", res: "15"},
		{n: "div_assign", src: "a := 12; a /= 4; a", res: "3"},
		{n: "rem_assign", src: "a := 10; a %= 3; a", res: "1"},

		{n: "rem_float_const", src: "i := 102; i % -1e2", res: "2"},
		{n: "add_assign_float_const", src: "a := 4; a += 13/4.0; a", res: "7"},
	})
}

func TestBitwiseInt(t *testing.T) {
	run(t, []etest{
		{n: "and", src: "0xff & 0x0f", res: "15"},
		{n: "and_zero", src: "0xff & 0", res: "0"},

		{n: "or", src: "0xf0 | 0x0f", res: "255"},
		{n: "or_same", src: "0xff | 0xff", res: "255"},

		{n: "xor", src: "0xff ^ 0x0f", res: "240"},
		{n: "xor_self", src: "a := 42; a ^ a", res: "0"},

		{n: "andnot", src: "0xff &^ 0x0f", res: "240"},

		{n: "comp", src: "^0", res: "-1"},
		{n: "comp_neg1", src: "^-1", res: "0"},

		{n: "shl", src: "1 << 10", res: "1024"},
		{n: "shl_zero", src: "42 << 0", res: "42"},

		{n: "shr", src: "1024 >> 3", res: "128"},
		{n: "shr_neg", src: "-8 >> 1", res: "-4"},

		{n: "shl_var", src: "var u uint64 = 1; var v uint32 = 10; u << v", res: "1024"},
		{n: "shr_var", src: "var u uint64 = 1024; var v uint32 = 3; u >> v", res: "128"},
		{n: "shl_assign", src: "a := 1; a <<= 4; a", res: "16"},
		{n: "shr_assign", src: "a := 16; a >>= 4; a", res: "1"},

		// Untyped float constant as left operand of shift (Go spec: treated as int).
		{n: "shl_float_const", src: "const a = 1.0; a << 2", res: "4"},
		{n: "shl_float_const_expr", src: "const a = 1.0; const b = a + 3; b << 1", res: "8"},
		{n: "shr_float_const", src: "const a = 8.0; a >> 1", res: "4"},

		{n: "and_assign", src: "a := 0xff; a &= 0x0f; a", res: "15"},
		{n: "or_assign", src: "a := 0xf0; a |= 0x0f; a", res: "255"},
		{n: "xor_assign", src: "a := 0xff; a ^= 0x0f; a", res: "240"},
		{n: "andnot_assign", src: "a := 0xff; a &^= 0x0f; a", res: "240"},
	})
}

func TestString(t *testing.T) {
	run(t, []etest{
		{n: "concat", src: `"hello" + " " + "world"`, res: "hello world"},
		{n: "concat_var", src: `a := "foo"; b := "bar"; a + b`, res: "foobar"},
		{n: "concat_empty", src: `"hello" + ""`, res: "hello"},

		{n: "add_assign", src: `a := "hello"; a += " world"; a`, res: "hello world"},

		{n: "slice", src: `a := "hello world"; a[0:5]`, res: "hello"},
		{n: "slice_mid", src: `a := "hello world"; a[6:11]`, res: "world"},
		{n: "slice_open_high", src: `a := "hello"; a[1:]`, res: "ello"},
		{n: "slice_open_low", src: `a := "hello"; a[:3]`, res: "hel"},

		{n: "index_var", src: `a := "hello"; a[1]`, res: "101"},
		{n: "index_const", src: `const s = "hello"; s[1]`, res: "101"},

		{n: "rune_lit", src: `'a'`, res: "97"},
		{n: "rune_lit_escape", src: `'\n'`, res: "10"},
		{n: "rune_compare", src: `var r rune = 97; r == 'a'`, res: "true"},
	})
}

func TestArithUint(t *testing.T) {
	run(t, []etest{
		{n: "add", src: "var a, b uint = 3, 4; a + b", res: "7"},
		{n: "sub", src: "var a, b uint = 10, 3; a - b", res: "7"},
		{n: "mul", src: "var a, b uint = 6, 7; a * b", res: "42"},
		{n: "div", src: "var a, b uint = 10, 3; a / b", res: "3"},
		{n: "rem", src: "var a, b uint = 10, 3; a % b", res: "1"},

		{n: "gt_large", src: "var a uint = 18446744073709551615; var b uint = 0; a > b", res: "true"},
		{n: "lt_large", src: "var a uint = 0; var b uint = 18446744073709551615; a < b", res: "true"},

		{n: "max_uint", src: "var a uint = 18446744073709551615; a", res: "18446744073709551615"},

		{n: "uint8_max", src: "var a uint8 = 255; a", res: "255"},
		{n: "uint8_add_wrap", src: "var a uint8 = 255; var b uint8 = 1; a + b", res: "0"},

		{n: "uint16_max", src: "var a uint16 = 65535; a", res: "65535"},
		{n: "uint32_max", src: "var a uint32 = 4294967295; a", res: "4294967295"},

		{n: "shr_logical", src: "var a uint = 18446744073709551615; a >> 60", res: "15"},
	})
}

func TestArithFloat(t *testing.T) {
	run(t, []etest{
		{n: "add", src: "var a, b float64 = 1.5, 2.5; a + b", res: "4"},
		{n: "sub", src: "var a, b float64 = 5.5, 2.0; a - b", res: "3.5"},
		{n: "mul", src: "var a, b float64 = 2.5, 4.0; a * b", res: "10"},
		{n: "div", src: "var a, b float64 = 7.0, 2.0; a / b", res: "3.5"},
		{n: "negate", src: "var a float64 = 3.14; -a", res: "-3.14"},

		{n: "gt_true", src: "var a, b float64 = 3.14, 2.71; a > b", res: "true"},
		{n: "gt_false", src: "var a, b float64 = 2.71, 3.14; a > b", res: "false"},
		{n: "lt_true", src: "var a, b float64 = 2.71, 3.14; a < b", res: "true"},
		{n: "eq_true", src: "var a, b float64 = 3.14, 3.14; a == b", res: "true"},
		{n: "ne_true", src: "var a, b float64 = 3.14, 2.71; a != b", res: "true"},
		{n: "ge_true", src: "var a, b float64 = 3.14, 3.14; a >= b", res: "true"},
		{n: "le_true", src: "var a, b float64 = 2.71, 3.14; a <= b", res: "true"},

		{n: "lit_add", src: "1.5 + 2.5", res: "4"},
		{n: "lit_sub", src: "5.0 - 1.5", res: "3.5"},
		{n: "lit_mul", src: "2.5 * 4.0", res: "10"},
		{n: "lit_div", src: "7.0 / 2.0", res: "3.5"},
		{n: "lit_neg", src: "-3.14", res: "-3.14"},

		{n: "int_div_float_const", src: "13/4.0", res: "3.25"},

		{n: "f32_add", src: "var a, b float32 = 1.5, 2.5; a + b", res: "4"},

		{n: "div_zero_pos", src: "var a, b float64 = 1.0, 0.0; a / b", res: "+Inf"},
		{n: "div_zero_neg", src: "var a, b float64 = -1.0, 0.0; a / b", res: "-Inf"},

		{n: "add_assign", src: "var a float64 = 1.5; a += 2.5; a", res: "4"},
		{n: "sub_assign", src: "var a float64 = 5.0; a -= 1.5; a", res: "3.5"},
		{n: "mul_assign", src: "var a float64 = 2.5; a *= 4.0; a", res: "10"},
		{n: "div_assign", src: "var a float64 = 7.0; a /= 2.0; a", res: "3.5"},
	})
}

func TestConvert(t *testing.T) {
	run(t, []etest{
		{n: "float64_to_int", src: "var a float64 = 3.14; int(a)", res: "3"},
		{n: "float64_to_int_neg", src: "var a float64 = -3.14; int(a)", res: "-3"},

		{n: "int_to_float64", src: "var a int = 42; float64(a)", res: "42"},
		{n: "int_to_int8", src: "var a int = 200; int8(a)", res: "-56"},
		{n: "int_to_uint8", src: "var a int = 256; uint8(a)", res: "0"},
		{n: "int_to_int16", src: "var a int = 40000; int16(a)", res: "-25536"},
		{n: "int_to_string", src: `string(65)`, res: "A"},
		{n: "int_to_int64", src: "var a int = 42; int64(a)", res: "42"},
		{n: "int_to_int", src: "var a int = 42; int(a)", res: "42"},

		{n: "uint_to_int", src: "var a uint = 5; int(a)", res: "5"},

		{n: "float32_to_float64", src: "var a float32 = 1.5; float64(a)", res: "1.5"},
		{n: "float64_to_float32", src: "var a float64 = 1.5; float32(a)", res: "1.5"},

		{n: "conv_in_expr", src: "var a float64 = 3.14; int(a) + 1", res: "4"},
	})
}

func TestArithTypedInt(t *testing.T) {
	run(t, []etest{
		{n: "int8_add", src: "var a, b int8 = 100, 20; a + b", res: "120"},
		{n: "int8_max", src: "var a int8 = 127; a", res: "127"},
		{n: "int8_min", src: "var a int8 = -128; a", res: "-128"},
		{n: "int8_wrap", src: "var a int8 = 127; var b int8 = 1; a + b", res: "-128"},

		{n: "int16_add", src: "var a, b int16 = 1000, 2000; a + b", res: "3000"},
		{n: "int16_max", src: "var a int16 = 32767; a", res: "32767"},
		{n: "int16_min", src: "var a int16 = -32768; a", res: "-32768"},

		{n: "int32_add", src: "var a, b int32 = 100000, 200000; a + b", res: "300000"},
		{n: "int32_max", src: "var a int32 = 2147483647; a", res: "2147483647"},

		{n: "int64_add", src: "var a, b int64 = 100, 200; a + b", res: "300"},
		{n: "int64_max", src: "var a int64 = 9223372036854775807; a", res: "9223372036854775807"},

		{n: "int8_mul", src: "var a, b int8 = 10, 12; a * b", res: "120"},
		{n: "int16_mul", src: "var a, b int16 = 200, 100; a * b", res: "20000"},
		{n: "int32_div", src: "var a, b int32 = 100, 3; a / b", res: "33"},
		{n: "int64_rem", src: "var a, b int64 = 100, 7; a % b", res: "2"},
	})
}

func TestDefer(t *testing.T) {
	run(t, []etest{
		{n: "#00", src: `
			a := 0
			func f() { defer func() { a = 1 }() }
			f()
			a`, res: "1"},
		{n: "#01", src: `
			// Multiple defers run LIFO.
			s := ""
			func f() {
				defer func() { s = s + "a" }()
				defer func() { s = s + "b" }()
				defer func() { s = s + "c" }()
			}
			f()
			s`, res: "cba"},
		{n: "#02", src: `
			// Args evaluated at defer time, not call time.
			x := 0
			func add(a, b int) { x = a + b }
			func f() {
				i := 1
				defer add(i, 2)
				i = 10
			}
			f()
			x`, res: "3"},
		{n: "#03", src: `
			// Args evaluated at defer time in a loop (not call time).
			s := 0
			func add(n int) { s = s + n }
			func f() {
				for i := 0; i < 3; i++ {
					defer add(i)
				}
			}
			f()
			s`, res: "3"},
		{n: "#04", src: `
			// Defer runs after return value is computed.
			a := 0
			func f() int {
				defer func() { a = 1 }()
				return 42
			}
			r := f()
			r`, res: "42"},
	})
}

func TestPanic(t *testing.T) {
	run(t, []etest{
		{n: "#00", src: `
			// Unrecovered panic propagates as error.
			func f() { panic("boom") }
			f()`, err: "panic: boom"},
		{n: "#01", src: `
			// Recover in deferred function stops the panic.
			a := 0
			func f() {
				defer func() { recover(); a = 1 }()
				panic("boom")
			}
			f()
			a`, res: "1"},
		{n: "#02", src: `
			// Recover returns the panic value.
			s := ""
			func f() {
				defer func() {
					r := recover()
					s = r.(string)
				}()
				panic("hello")
			}
			f()
			s`, res: "hello"},
		{n: "#03", src: `
			// Unrecovered panic still runs defers, but propagates error.
			s := ""
			func f() {
				defer func() { s = s + "a" }()
				defer func() { s = s + "b" }()
				panic("x")
			}
			f()
			s`, err: "panic: x"},
		{n: "#04", src: `
			// Recover outside panic returns nil (as empty value).
			func f() {
				defer func() { recover() }()
			}
			f()
			0`, res: "0"},
		{n: "#05", src: `
			// Panic with int value.
			n := 0
			func f() {
				defer func() {
					r := recover()
					n = r.(int)
				}()
				panic(42)
			}
			f()
			n`, res: "42"},
		{n: "#06", src: `
			// Panic propagates through multiple frames.
			s := ""
			func g() { panic("deep") }
			func f() {
				defer func() {
					r := recover()
					s = r.(string)
				}()
				g()
			}
			f()
			s`, res: "deep"},
		{n: "#07", src: `
			// Code after panic does not execute.
			a := 1
			func f() {
				defer func() { recover() }()
				panic("x")
				a = 2
			}
			f()
			a`, res: "1"},
		{n: "#08", src: `
			// Panic with native deferred function.
			x := 0
			func add(n int) { x = x + n }
			func f() {
				defer add(10)
				panic("boom")
			}
			f()
			x`, err: "panic: boom"},
		{n: "#09", src: `
			// Multiple defers: first recovers, rest still run.
			s := ""
			func f() {
				defer func() { s = s + "a" }()
				defer func() { recover(); s = s + "b" }()
				defer func() { s = s + "c" }()
				panic("x")
			}
			f()
			s`, res: "cba"},
	})
}

func TestStructFuncField(t *testing.T) {
	run(t, []etest{
		{n: "assign_call", src: `
type S struct { F func(int) int }
var s S
s.F = func(n int) int { return n * 2 }
s.F(7)`, res: "14"},

		{n: "literal", skip: true, src: `
type S struct { F func(int) int }
s := S{F: func(n int) int { return n + 1 }}
s.F(10)`, res: "11"},

		{n: "closure_capture", src: `
type S struct { F func() int }
x := 42
var s S
s.F = func() int { return x }
s.F()`, res: "42"},

		{n: "reassign", src: `
type S struct { F func() int }
var s S
s.F = func() int { return 1 }
s.F = func() int { return 2 }
s.F()`, res: "2"},

		{n: "iface_param", src: `
type I interface { Hello() string }
type T struct{ name string }
func (t T) Hello() string { return t.name }
type S struct { Handler func(I) string }
var s S
s.Handler = func(i I) string { return i.Hello() }
s.Handler(T{name: "world"})`, res: "world"},

		{n: "native_call", src: `
type S struct { F func(int) int }
var s S
s.F = func(n int) int { return n * 3 }
s.F(5)`, res: "15"},

		// Closure in struct func field survives append (struct copy to new backing array).
		{n: "append_copy", src: `
type T struct{ F func() int }
func g() int {
	var foos []T
	for i := 0; i < 3; i++ {
		a := i
		foos = append(foos, T{func() int { return a }})
	}
	return foos[0].F() + foos[1].F()*10 + foos[2].F()*100
}
g()`, res: "210"},
	})
}

func TestBuiltin(t *testing.T) {
	run(t, []etest{
		{n: "len_slice", src: `a := []int{1, 2, 3}; len(a)`, res: "3"},
		{n: "len_string", src: `len("hello")`, res: "5"},
		{n: "cap_slice", src: `a := make([]int, 2, 5); cap(a)`, res: "5"},
		{n: "make_slice", src: `a := make([]int, 3); len(a)`, res: "3"},
		{n: "make_slice_cap", src: `a := make([]int, 2, 10); cap(a)`, res: "10"},
		{n: "make_map", src: `m := make(map[string]int); m["x"] = 5; m["x"]`, res: "5"},
		{n: "append_basic", src: `a := []int{1, 2}; a = append(a, 3); a`, res: "[1 2 3]"},
		{n: "append_multi", src: `a := []int{1}; a = append(a, 2, 3, 4); a`, res: "[1 2 3 4]"},
		{n: "copy_basic", src: `a := []int{1, 2, 3}; b := make([]int, 2); copy(b, a); b`, res: "[1 2]"},
		{n: "copy_retval", src: `a := []int{1, 2, 3}; b := make([]int, 5); n := copy(b, a); n`, res: "3"},
		{n: "delete_map", src: `m := map[string]int{"a": 1, "b": 2}; delete(m, "a"); len(m)`, res: "1"},
		{n: "new_int", src: `p := new(int); *p`, res: "0"},
		{n: "new_string", src: `p := new(string); *p`, res: ""},
	})
}

// TestRepl exercises the re-entrant interpreter (REPL mode), where a single
// Interp is used across multiple sequential Eval calls.
func TestRepl(t *testing.T) {
	// Global data from a prior Eval must not occupy the same slots as new
	// constants from subsequent Evals.
	t.Run("stale_data", func(t *testing.T) {
		intp := interp.NewInterpreter(golang.GoSpec)
		if _, err := intp.Eval("m:1", "12/5.1"); err != nil {
			t.Fatal(err)
		}
		r, err := intp.Eval("m:2", "13/4.0")
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%v", r); got != "3.25" {
			t.Errorf("got %v, want 3.25", got)
		}
	})
}
