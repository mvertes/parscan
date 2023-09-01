package parser

import (
	"log"
	"os"
	"testing"

	"github.com/gnolang/parscan/scanner"
)

var GoScanner = &scanner.Scanner{
	CharProp: [scanner.ASCIILen]uint{
		'\t': scanner.CharSep,
		'\n': scanner.CharLineSep,
		' ':  scanner.CharSep,
		'!':  scanner.CharOp,
		'"':  scanner.CharStr,
		'%':  scanner.CharOp,
		'&':  scanner.CharOp,
		'\'': scanner.CharStr,
		'(':  scanner.CharBlock,
		'*':  scanner.CharOp,
		'+':  scanner.CharOp,
		',':  scanner.CharGroupSep,
		'-':  scanner.CharOp,
		'.':  scanner.CharOp,
		'/':  scanner.CharOp,
		':':  scanner.CharOp,
		';':  scanner.CharGroupSep,
		'<':  scanner.CharOp,
		'=':  scanner.CharOp,
		'>':  scanner.CharOp,
		'[':  scanner.CharBlock,
		'^':  scanner.CharOp,
		'{':  scanner.CharBlock,
		'|':  scanner.CharOp,
		'~':  scanner.CharOp,
	},
	End: map[string]string{
		"(":  ")",
		"{":  "}",
		"[":  "]",
		"/*": "*/",
		`"`:  `"`,
		"'":  "'",
		"`":  "`",
		"//": "\n",
	},
	BlockProp: map[string]uint{
		"(":  scanner.CharBlock,
		"{":  scanner.CharBlock,
		"[":  scanner.CharBlock,
		`"`:  scanner.CharStr | scanner.StrEsc | scanner.StrNonl,
		"`":  scanner.CharStr,
		"'":  scanner.CharStr | scanner.StrEsc,
		"/*": scanner.CharStr,
		"//": scanner.CharStr | scanner.ExcludeEnd | scanner.EosValidEnd,
	},
}

var GoParser = &Parser{
	Scanner: GoScanner,
	Spec: map[string]NodeSpec{
		".":      {Kind: OpDot, Flags: Call, Order: 3},
		"*":      {Kind: OpMultiply, Order: 4},
		"+":      {Kind: OpAdd, Order: 5},
		"-":      {Kind: OpSubtract, Order: 5},
		"<":      {Kind: OpInferior, Order: 6},
		":=":     {Kind: OpDefine, Order: 7},
		"=":      {Kind: OpAssign, Order: 7},
		"if":     {Kind: StmtIf, Flags: Stmt | ExprSep},
		"func":   {Kind: DeclFunc, Flags: Decl | Call},
		"return": {Kind: StmtReturn, Flags: Stmt},
		"{..}":   {Kind: BlockStmt, Flags: ExprSep},
		"(..)":   {Kind: BlockParen, Flags: Call},
		"//..":   {Kind: Comment},
		"/*..":   {Kind: Comment},
	},
}

func init() {
	GoParser.Init()
	log.SetFlags(log.Lshortfile)
}

func TestParse(t *testing.T) {
	for _, test := range goTests {
		test := test
		t.Run("", func(t *testing.T) {
			var err error
			errStr := ""
			n := &Node{}
			if n.Child, err = GoParser.Parse(test.src); err != nil {
				errStr = err.Error()
			}
			if errStr != test.err {
				t.Errorf("got error %#v, want error %#v", errStr, test.err)
			}
			if dot := n.Sdot(""); dot != test.dot {
				t.Errorf("got %#v, want %#v", dot, test.dot)
			}
			t.Log(test.src)
			t.Log(n.Sdot(""))
			if dotCmd := os.Getenv("DOT"); dotCmd != "" {
				n.Dot(dotCmd, "")
			}
		})
	}
}

var goTests = []struct {
	src, dot, err string
	skip          bool
}{{ // #00
	src: "",
	dot: `digraph ast { 0 [label=""]; }`,
}, { // #01
	src: "12",
	dot: `digraph ast { 0 [label=""]; 1 [label="12"]; 0 -> 1; }`,
}, { // #02
	src: "1 + 2",
	dot: `digraph ast { 0 [label=""]; 1 [label="+"]; 0 -> 1; 2 [label="1"]; 1 -> 2; 3 [label="2"]; 1 -> 3; }`,
}, { // #03
	src: "1 + 2 * 3",
	dot: `digraph ast { 0 [label=""]; 1 [label="+"]; 0 -> 1; 2 [label="1"]; 1 -> 2; 3 [label="*"]; 1 -> 3; 4 [label="2"]; 3 -> 4; 5 [label="3"]; 3 -> 5; }`,
}, { // #04
	src: "1 * 2 + 3",
	dot: `digraph ast { 0 [label=""]; 1 [label="+"]; 0 -> 1; 2 [label="*"]; 1 -> 2; 3 [label="1"]; 2 -> 3; 4 [label="2"]; 2 -> 4; 5 [label="3"]; 1 -> 5; }`,
}, { // #05
	src: "a := 2 + 5",
	dot: `digraph ast { 0 [label=""]; 1 [label=":="]; 0 -> 1; 2 [label="a"]; 1 -> 2; 3 [label="+"]; 1 -> 3; 4 [label="2"]; 3 -> 4; 5 [label="5"]; 3 -> 5; }`,
}, { // #06
	src: "a := 1 * 2 + 3",
	dot: `digraph ast { 0 [label=""]; 1 [label=":="]; 0 -> 1; 2 [label="a"]; 1 -> 2; 3 [label="+"]; 1 -> 3; 4 [label="*"]; 3 -> 4; 5 [label="1"]; 4 -> 5; 6 [label="2"]; 4 -> 6; 7 [label="3"]; 3 -> 7; }`,
}, { // #07
	src: "a := 1 + 2 * 3",
	dot: `digraph ast { 0 [label=""]; 1 [label=":="]; 0 -> 1; 2 [label="a"]; 1 -> 2; 3 [label="+"]; 1 -> 3; 4 [label="1"]; 3 -> 4; 5 [label="*"]; 3 -> 5; 6 [label="2"]; 5 -> 6; 7 [label="3"]; 5 -> 7; }`,
}, { // #08
	src: "a := 1 + 2 * 3 + 4",
	dot: `digraph ast { 0 [label=""]; 1 [label=":="]; 0 -> 1; 2 [label="a"]; 1 -> 2; 3 [label="+"]; 1 -> 3; 4 [label="1"]; 3 -> 4; 5 [label="+"]; 3 -> 5; 6 [label="*"]; 5 -> 6; 7 [label="2"]; 6 -> 7; 8 [label="3"]; 6 -> 8; 9 [label="4"]; 5 -> 9; }`,
}, { // #09
	src: "a := 1 + 2 + 3 * 4",
	dot: `digraph ast { 0 [label=""]; 1 [label=":="]; 0 -> 1; 2 [label="a"]; 1 -> 2; 3 [label="+"]; 1 -> 3; 4 [label="1"]; 3 -> 4; 5 [label="+"]; 3 -> 5; 6 [label="2"]; 5 -> 6; 7 [label="*"]; 5 -> 7; 8 [label="3"]; 7 -> 8; 9 [label="4"]; 7 -> 9; }`,
}, { // #10
	src: "a := 1 * 2 + 3 * 4",
	dot: `digraph ast { 0 [label=""]; 1 [label=":="]; 0 -> 1; 2 [label="a"]; 1 -> 2; 3 [label="+"]; 1 -> 3; 4 [label="*"]; 3 -> 4; 5 [label="1"]; 4 -> 5; 6 [label="2"]; 4 -> 6; 7 [label="*"]; 3 -> 7; 8 [label="3"]; 7 -> 8; 9 [label="4"]; 7 -> 9; }`,
}, { // #11
	src: "a := (1 + 2) * 3",
	dot: `digraph ast { 0 [label=""]; 1 [label=":="]; 0 -> 1; 2 [label="a"]; 1 -> 2; 3 [label="*"]; 1 -> 3; 4 [label="(..)"]; 3 -> 4; 5 [label="+"]; 4 -> 5; 6 [label="1"]; 5 -> 6; 7 [label="2"]; 5 -> 7; 8 [label="3"]; 3 -> 8; }`,
}, { // #12
	src: "a := 2 * -3",
	dot: `digraph ast { 0 [label=""]; 1 [label=":="]; 0 -> 1; 2 [label="a"]; 1 -> 2; 3 [label="*"]; 1 -> 3; 4 [label="2"]; 3 -> 4; 5 [label="-"]; 3 -> 5; 6 [label="3"]; 5 -> 6; }`,
}, { // #13
	src: "-5 + 4",
	dot: `digraph ast { 0 [label=""]; 1 [label="+"]; 0 -> 1; 2 [label="-"]; 1 -> 2; 3 [label="5"]; 2 -> 3; 4 [label="4"]; 1 -> 4; }`,
}, { // #14
	src: "-5 + -4",
	dot: `digraph ast { 0 [label=""]; 1 [label="+"]; 0 -> 1; 2 [label="-"]; 1 -> 2; 3 [label="5"]; 2 -> 3; 4 [label="-"]; 1 -> 4; 5 [label="4"]; 4 -> 5; }`,
}, { // #15
	src: "a := -5 + -4",
	dot: `digraph ast { 0 [label=""]; 1 [label=":="]; 0 -> 1; 2 [label="a"]; 1 -> 2; 3 [label="+"]; 1 -> 3; 4 [label="-"]; 3 -> 4; 5 [label="5"]; 4 -> 5; 6 [label="-"]; 3 -> 6; 7 [label="4"]; 6 -> 7; }`,
}, { // #16
	src: "*a := 5 * -3",
	dot: `digraph ast { 0 [label=""]; 1 [label=":="]; 0 -> 1; 2 [label="*"]; 1 -> 2; 3 [label="a"]; 2 -> 3; 4 [label="*"]; 1 -> 4; 5 [label="5"]; 4 -> 5; 6 [label="-"]; 4 -> 6; 7 [label="3"]; 6 -> 7; }`,
}, { // #17
	src: "*a := -5 * 3",
	dot: `digraph ast { 0 [label=""]; 1 [label=":="]; 0 -> 1; 2 [label="*"]; 1 -> 2; 3 [label="a"]; 2 -> 3; 4 [label="*"]; 1 -> 4; 5 [label="-"]; 4 -> 5; 6 [label="5"]; 5 -> 6; 7 [label="3"]; 4 -> 7; }`,
}, { // #18
	src: "1+2\n3-4",
	dot: `digraph ast { 0 [label=""]; 1 [label="+"]; 0 -> 1; 2 [label="1"]; 1 -> 2; 3 [label="2"]; 1 -> 3; 4 [label="-"]; 0 -> 4; 5 [label="3"]; 4 -> 5; 6 [label="4"]; 4 -> 6; }`,
}, { // #19
	src: "i = i+1",
	dot: `digraph ast { 0 [label=""]; 1 [label="="]; 0 -> 1; 2 [label="i"]; 1 -> 2; 3 [label="+"]; 1 -> 3; 4 [label="i"]; 3 -> 4; 5 [label="1"]; 3 -> 5; }`,
}, { // #20
	src: "a[12] = 5",
	dot: `digraph ast { 0 [label=""]; 1 [label="="]; 0 -> 1; 2 [label="a"]; 1 -> 2; 3 [label="[..]"]; 2 -> 3; 4 [label="12"]; 3 -> 4; 5 [label="5"]; 1 -> 5; }`,
}, { // #21
	src: "a[12][0] = 3",
	dot: `digraph ast { 0 [label=""]; 1 [label="="]; 0 -> 1; 2 [label="a"]; 1 -> 2; 3 [label="[..]"]; 2 -> 3; 4 [label="12"]; 3 -> 4; 5 [label="[..]"]; 2 -> 5; 6 [label="0"]; 5 -> 6; 7 [label="3"]; 1 -> 7; }`,
}, { // #22
	src: "a.b = 34",
	dot: `digraph ast { 0 [label=""]; 1 [label="="]; 0 -> 1; 2 [label="."]; 1 -> 2; 3 [label="a"]; 2 -> 3; 4 [label="b"]; 2 -> 4; 5 [label="34"]; 1 -> 5; }`,
}, { // #23
	src: "if i < 2 { return j }",
	dot: `digraph ast { 0 [label=""]; 1 [label="if"]; 0 -> 1; 2 [label="<"]; 1 -> 2; 3 [label="i"]; 2 -> 3; 4 [label="2"]; 2 -> 4; 5 [label="{..}"]; 1 -> 5; 6 [label="return"]; 5 -> 6; 7 [label="j"]; 6 -> 7; }`,
}, { // #24
	src: "if i:=1; i < 2 { return j }",
	dot: `digraph ast { 0 [label=""]; 1 [label="if"]; 0 -> 1; 2 [label=":="]; 1 -> 2; 3 [label="i"]; 2 -> 3; 4 [label="1"]; 2 -> 4; 5 [label="<"]; 1 -> 5; 6 [label="i"]; 5 -> 6; 7 [label="2"]; 5 -> 7; 8 [label="{..}"]; 1 -> 8; 9 [label="return"]; 8 -> 9; 10 [label="j"]; 9 -> 10; }`,
}, { // #25
	src: "f(i) + f(j)",
	dot: `digraph ast { 0 [label=""]; 1 [label="+"]; 0 -> 1; 2 [label="Call"]; 1 -> 2; 3 [label="f"]; 2 -> 3; 4 [label="(..)"]; 2 -> 4; 5 [label="i"]; 4 -> 5; 6 [label="Call"]; 1 -> 6; 7 [label="f"]; 6 -> 7; 8 [label="(..)"]; 6 -> 8; 9 [label="j"]; 8 -> 9; }`,
}, { // #26
	src: "a := 1 // This is a comment",
	dot: `digraph ast { 0 [label=""]; 1 [label=":="]; 0 -> 1; 2 [label="a"]; 1 -> 2; 3 [label="1"]; 1 -> 3; }`,
	//src: "f(i) + f(j)(4)", // not ok
	/*
	   }, { // #26
	   	src: "if i < 2 {return i}; return f(i-2) + f(i-1)",
	   }, { // #27
	   	src: "for i < 2 { println(i) }",
	   }, { // #28
	   	src: "func f(i int) (int) { if i < 2 { return i}; return f(i-2) + f(i-1) }",
	   }, { // #29
	   	src: "a := []int{3, 4}",
	   }, { // #30
	   	//src: "a := struct{int}",
	   	src: "a, b = c, d",
	   }, { // #31
	   	//src: "a := [2]int{3, 4}",
	   	src: `fmt.Println("Hello")`,
	   	//src: "(1 + 2) * (3 - 4)",
	   	//src: "1 + (1 + 2)",
	   }, { // #32
	   	//src: `a(3)(4)`,
	   	//src: `3 + 2 * a(3) + 5`,
	   	//src: `3 + 2 * a(3)(4) + (5)`,
	   	//src: `(a(3))(4)`,
	   	src: `a(3)(4)`,
	   	dot: `digraph ast { 0 [label=""]; 1 [label="Call"]; 0 -> 1; 2 [label="Call"]; 1 -> 2; 3 [label="a"]; 2 -> 3; 4 [label="(..)"]; 2 -> 4; 5 [label="3"]; 4 -> 5; 6 [label="(..)"]; 1 -> 6; 7 [label="4"]; 6 -> 7; }`,
	   	//src: `println("Hello")`,
	   	//src: `a.b.c + 3`,
	   }, { // #33
	   	src: `func f(a int, b int) {return a + b}; f(1+2)`,
	   }, { // #34
	   	src: `if a == 1 {
	   		println(2)
	   	}
	   	println("bye")`,
	*/
}}
