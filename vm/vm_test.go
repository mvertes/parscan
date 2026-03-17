package vm

import (
	"fmt"
	"log"
	"testing"
)

func init() {
	log.SetFlags(log.Lshortfile)
}

func TestVM(t *testing.T) {
	for _, test := range tests {
		test := test
		t.Run("", func(t *testing.T) {
			m := &Machine{}
			for _, v := range test.sym {
				m.Push(v)
			}
			m.PushCode(test.code...)
			if err := m.Run(); err != nil {
				t.Errorf("run error: %v", err)
			}
			t.Log(Vstring(m.mem))
			if r := Vstring(m.mem[test.start:test.end]); r != test.mem {
				t.Errorf("got %v, want %v", r, test.mem)
			}
		})
	}
}

func BenchmarkVM(b *testing.B) {
	for _, test := range tests {
		test := test
		b.Run("", func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				m := &Machine{}
				m.PushCode(test.code...)
				b.StartTimer()

				if err := m.Run(); err != nil {
					b.Errorf("run error: %v", err)
				}
			}
		})
	}
}

// fibTypedCode is fib(20) hand-written with per-type opcodes (no Imm).
// fib function at addr 1, call site at addr 19.
var fibTypedCode = []Instruction{
	{Op: Jump, Arg: []int{19}},     // 0: skip to call site
	{Op: Get, Arg: []int{1, -3}},   // 1: push i
	{Op: Push, Arg: []int{2}},      // 2
	{Op: LowerInt},                 // 3: i < 2
	{Op: JumpTrue, Arg: []int{13}}, // 4: if i<2 goto 17
	{Op: Push, Arg: []int{1}},      // 5: fib addr
	{Op: Get, Arg: []int{1, -3}},   // 6: push i
	{Op: Push, Arg: []int{2}},      // 7
	{Op: SubInt},                   // 8: i-2
	{Op: Call, Arg: []int{1, 1}},   // 9: fib(i-2)
	{Op: Push, Arg: []int{1}},      // 10: fib addr
	{Op: Get, Arg: []int{1, -3}},   // 11: push i
	{Op: Push, Arg: []int{1}},      // 12
	{Op: SubInt},                   // 13: i-1
	{Op: Call, Arg: []int{1, 1}},   // 14: fib(i-1)
	{Op: AddInt},                   // 15: sum
	{Op: Return, Arg: []int{1, 1}}, // 16: return (recursive)
	{Op: Get, Arg: []int{1, -3}},   // 17: base case
	{Op: Return, Arg: []int{1, 1}}, // 18: return i
	{Op: Push, Arg: []int{1}},      // 19: call site
	{Op: Push, Arg: []int{20}},     // 20: n=20
	{Op: Call, Arg: []int{1, 1}},   // 21
	{Op: Exit},                     // 22
}

// fibImmCode is fib(20) rewritten with immediate-operand opcodes.
// Saves 3 Push instructions from the function body.
// fib function at addr 1, call site at addr 16.
var fibImmCode = []Instruction{
	{Op: Jump, Arg: []int{16}},       // 0: skip to call site
	{Op: Get, Arg: []int{1, -3}},     // 1: push i
	{Op: LowerIntImm, Arg: []int{2}}, // 2: i < 2 (was: Push 2; LowerInt)
	{Op: JumpTrue, Arg: []int{11}},   // 3: if i<2 goto 14
	{Op: Push, Arg: []int{1}},        // 4: fib addr
	{Op: Get, Arg: []int{1, -3}},     // 5: push i
	{Op: SubIntImm, Arg: []int{2}},   // 6: i-2 (was: Push 2; SubInt)
	{Op: Call, Arg: []int{1, 1}},     // 7: fib(i-2)
	{Op: Push, Arg: []int{1}},        // 8: fib addr
	{Op: Get, Arg: []int{1, -3}},     // 9: push i
	{Op: SubIntImm, Arg: []int{1}},   // 10: i-1 (was: Push 1; SubInt)
	{Op: Call, Arg: []int{1, 1}},     // 11: fib(i-1)
	{Op: AddInt},                     // 12: sum
	{Op: Return, Arg: []int{1, 1}},   // 13: return (recursive)
	{Op: Get, Arg: []int{1, -3}},     // 14: base case
	{Op: Return, Arg: []int{1, 1}},   // 15: return i
	{Op: Push, Arg: []int{1}},        // 16: call site
	{Op: Push, Arg: []int{20}},       // 17: n=20
	{Op: Call, Arg: []int{1, 1}},     // 18
	{Op: Exit},                       // 19
}

func BenchmarkFibTyped(b *testing.B) {
	for i := 0; i < b.N; i++ {
		m := &Machine{}
		m.PushCode(fibTypedCode...)
		if err := m.Run(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFibImm(b *testing.B) {
	for i := 0; i < b.N; i++ {
		m := &Machine{}
		m.PushCode(fibImmCode...)
		if err := m.Run(); err != nil {
			b.Fatal(err)
		}
	}
}

var tests = []struct {
	sym        []Value       // initial memory values
	code       []Instruction // bytecode to execute
	start, end int           //
	mem        string        // expected memory content
}{{ // #00 -- A simple addition.
	code: []Instruction{
		{Op: Push, Arg: []int{1}},
		{Op: Push, Arg: []int{2}},
		{Op: Add},
		{Op: Exit},
	},
	start: 0, end: 1, mem: "[3]",
}, { // #01 -- A simple subtraction.
	code: []Instruction{
		{Op: Push, Arg: []int{2}},
		{Op: Push, Arg: []int{3}},
		{Op: Sub},
		{Op: Exit},
	},
	start: 0, end: 1, mem: "[-1]",
}, { // #02 -- A simple multiplication.
	code: []Instruction{
		{Op: Push, Arg: []int{3}},
		{Op: Push, Arg: []int{2}},
		{Op: Mul},
		{Op: Exit},
	},
	start: 0, end: 1, mem: "[6]",
}, { // #03 -- lower.
	code: []Instruction{
		{Op: Push, Arg: []int{2}},
		{Op: Push, Arg: []int{3}},
		{Op: Lower},
		{Op: Exit},
	},
	start: 0, end: 1, mem: "[true]",
}, { // #04 -- greater.
	code: []Instruction{
		{Op: Push, Arg: []int{3}},
		{Op: Push, Arg: []int{2}},
		{Op: Greater},
		{Op: Exit},
	},
	start: 0, end: 1, mem: "[true]",
}, { // #05 -- equal.
	code: []Instruction{
		{Op: Push, Arg: []int{2}},
		{Op: Push, Arg: []int{3}},
		{Op: Equal},
		{Op: Exit},
	},
	start: 0, end: 1, mem: "[false]",
}, { // #06 -- equalSet.
	code: []Instruction{
		{Op: Push, Arg: []int{2}},
		{Op: Push, Arg: []int{3}},
		{Op: EqualSet},
		{Op: Exit},
	},
	start: 0, end: 2, mem: "[2 false]",
}, { // #07 -- equalSet.
	code: []Instruction{
		{Op: Push, Arg: []int{3}},
		{Op: Push, Arg: []int{3}},
		{Op: EqualSet},
		{Op: Exit},
	},
	start: 0, end: 1, mem: "[true]",
}, { // #08 not.
	code: []Instruction{
		{Op: Push, Arg: []int{3}},
		{Op: Push, Arg: []int{3}},
		{Op: Equal},
		{Op: Not},
		{Op: Exit},
	},
	start: 0, end: 1, mem: "[false]",
}, { // #09 pop.
	code: []Instruction{
		{Op: Push, Arg: []int{3}},
		{Op: Push, Arg: []int{2}},
		{Op: Pop, Arg: []int{1}},
		{Op: Exit},
	},
	start: 0, end: 1, mem: "[3]",
}, { // #10 -- Assign a variable.
	sym: []Value{ValueOf(0)},
	code: []Instruction{
		{Op: Grow, Arg: []int{1}},
		{Op: New, Arg: []int{2, 0}},
		{Op: Push, Arg: []int{2}},
		{Op: Set, Arg: []int{0, 1}},
		{Op: Exit},
	},
	start: 1, end: 2, mem: "[2]",
}, { // #11 -- Calling a function defined outside the VM.
	sym: []Value{ValueOf(fmt.Println), ValueOf("Hello")},
	code: []Instruction{
		{Op: Get, Arg: []int{0, 0}},
		{Op: Get, Arg: []int{0, 1}},
		{Op: CallX, Arg: []int{1}},
		{Op: Exit},
	},
	start: 2, end: 4, mem: "[6 <nil>]",
}, { // #12 -- Defining and calling a function in VM.
	code: []Instruction{
		{Op: Jump, Arg: []int{3}},      // 0
		{Op: Push, Arg: []int{3}},      // 1
		{Op: Return, Arg: []int{1, 1}}, // 2
		{Op: Push, Arg: []int{1}},      // 3
		{Op: Push, Arg: []int{1}},      // 4
		{Op: Call, Arg: []int{0}},      // 5
		{Op: Exit},                     // 6
	},
	start: 0, end: 1, mem: "[3]",
}, { // #13 -- Defining and calling a function in VM.
	code: []Instruction{
		{Op: Jump, Arg: []int{5}},      // 0
		{Op: Push, Arg: []int{3}},      // 1
		{Op: Set, Arg: []int{1, -3}},   // 2
		{Op: Get, Arg: []int{1, -3}},   // 3
		{Op: Return, Arg: []int{1, 1}}, // 4
		{Op: Push, Arg: []int{1}},      // 5
		{Op: Push, Arg: []int{1}},      // 6
		{Op: Call, Arg: []int{0}},      // 7
		{Op: Exit},                     // 8
	},
	start: 0, end: 1, mem: "[3]",
}, { // #14 -- Fibonacci numbers, hand written. Showcase recursivity.
	code: []Instruction{
		{Op: Jump, Arg: []int{19}},     // 0
		{Op: Get, Arg: []int{1, -3}},   // 1  [2 i]
		{Op: Push, Arg: []int{2}},      // 2  [2]
		{Op: Lower},                    // 3  [true/false]
		{Op: JumpTrue, Arg: []int{13}}, // 4  [], goto 17
		{Op: Push, Arg: []int{1}},      // 5
		{Op: Get, Arg: []int{1, -3}},   // 6  [i]
		{Op: Push, Arg: []int{2}},      // 7  [i 2]
		{Op: Sub},                      // 8  [(i-2)]
		{Op: Call, Arg: []int{1}},      // 9  [fib(i-2)]
		{Op: Push, Arg: []int{1}},      // 10
		{Op: Get, Arg: []int{1, -3}},   // 11 [fib(i-2) i]
		{Op: Push, Arg: []int{1}},      // 12 [(i-2) i 1]
		{Op: Sub},                      // 13 [(i-2) (i-1)]
		{Op: Call, Arg: []int{1}},      // 14 [fib(i-2) fib(i-1)]
		{Op: Add},                      // 15 [fib(i-2)+fib(i-1)]
		{Op: Return, Arg: []int{1, 1}}, // 16 return i
		{Op: Get, Arg: []int{1, -3}},   // 17 [i]
		{Op: Return, Arg: []int{1, 1}}, // 18 return i
		{Op: Push, Arg: []int{1}},      // 19
		{Op: Push, Arg: []int{6}},      // 20 [1]
		{Op: Call, Arg: []int{1}},      // 21 [fib(*1)]
		{Op: Exit},                     // 22
	},
	start: 0, end: 1, mem: "[8]",
}}
