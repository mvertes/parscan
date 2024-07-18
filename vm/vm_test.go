package vm

import (
	"fmt"
	"log"
	"reflect"
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

var tests = []struct {
	sym        []Value   // initial memory values
	code       [][]int64 // bytecode to execute
	start, end int       //
	mem        string    // expected memory content
}{{ // #00 -- A simple addition.
	code: [][]int64{
		{0, Push, 1},
		{0, Push, 2},
		{0, Add},
		{0, Exit},
	},
	start: 0, end: 1, mem: "[3]",
}, { // #01 -- A simple subtraction.
	code: [][]int64{
		{0, Push, 2},
		{0, Push, 3},
		{0, Sub},
		{0, Exit},
	},
	start: 0, end: 1, mem: "[1]",
}, { // #02 -- A simple multiplication.
	code: [][]int64{
		{0, Push, 3},
		{0, Push, 2},
		{0, Mul},
		{0, Exit},
	},
	start: 0, end: 1, mem: "[6]",
}, { // #03 -- lower.
	code: [][]int64{
		{0, Push, 3},
		{0, Push, 2},
		{0, Lower},
		{0, Exit},
	},
	start: 0, end: 1, mem: "[true]",
}, { // #04 -- greater.
	code: [][]int64{
		{0, Push, 2},
		{0, Push, 3},
		{0, Greater},
		{0, Exit},
	},
	start: 0, end: 1, mem: "[true]",
}, { // #05 -- equal.
	code: [][]int64{
		{0, Push, 2},
		{0, Push, 3},
		{0, Equal},
		{0, Exit},
	},
	start: 0, end: 1, mem: "[false]",
}, { // #06 -- equalSet.
	code: [][]int64{
		{0, Push, 2},
		{0, Push, 3},
		{0, EqualSet},
		{0, Exit},
	},
	start: 0, end: 2, mem: "[2 false]",
}, { // #07 -- equalSet.
	code: [][]int64{
		{0, Push, 3},
		{0, Push, 3},
		{0, EqualSet},
		{0, Exit},
	},
	start: 0, end: 1, mem: "[true]",
}, { // #08 not.
	code: [][]int64{
		{0, Push, 3},
		{0, Push, 3},
		{0, Equal},
		{0, Not},
		{0, Exit},
	},
	start: 0, end: 1, mem: "[false]",
}, { // #09 pop.
	code: [][]int64{
		{0, Push, 3},
		{0, Push, 2},
		{0, Pop, 1},
		{0, Exit},
	},
	start: 0, end: 1, mem: "[3]",
}, { // #10 -- Assign a variable.
	sym: []Value{{Type: TypeOf(0), Data: reflect.ValueOf(0)}},
	code: [][]int64{
		{0, Grow, 1},
		{0, New, 2, 0},
		{0, Push, 2},
		{0, Assign, 1},
		{0, Exit},
	},
	start: 1, end: 2, mem: "[2]",
}, { // #11 -- Calling a function defined outside the VM.
	sym: []Value{ValueOf(fmt.Println), ValueOf("Hello")},
	code: [][]int64{
		{0, Dup, 0},
		{0, CallX, 1},
		{0, Exit},
	},
	start: 1, end: 3, mem: "[6 <nil>]",
}, { // #12 -- Defining and calling a function in VM.
	code: [][]int64{
		{0, Jump, 3},      // 0
		{0, Push, 3},      // 1
		{0, Return, 1, 1}, // 2
		{0, Push, 1},      // 3
		{0, Calli, 1},     // 4
		{0, Exit},         // 5
	},
	start: 0, end: 1, mem: "[3]",
}, { // #13 -- Defining and calling a function in VM.
	code: [][]int64{
		{0, Jump, 3},      // 0
		{0, Push, 3},      // 1
		{0, Return, 1, 1}, // 2
		{0, Push, 1},      // 3
		{0, Push, 1},      // 4
		{0, Call},         // 5
		{0, Exit},         // 6
	},
	start: 0, end: 1, mem: "[3]",
}, { // #14 -- Defining and calling a function in VM.
	code: [][]int64{
		{0, Jump, 5},      // 0
		{0, Push, 3},      // 1
		{0, Fassign, -2},  // 2
		{0, Fdup, -2},     // 3
		{0, Return, 1, 1}, // 4
		{0, Push, 1},      // 5
		{0, Push, 1},      // 6
		{0, Call},         // 7
		{0, Exit},         // 8
	},
	start: 0, end: 1, mem: "[3]",
}, { // #15 -- Fibonacci numbers, hand written. Showcase recursivity.
	code: [][]int64{
		{0, Jump, 19},     // 0
		{0, Push, 2},      // 2  [2]
		{0, Fdup, -2},     // 1  [2 i]
		{0, Lower},        // 3  [true/false]
		{0, JumpTrue, 13}, // 4  [], goto 17
		{0, Push, 2},      // 5  [i 2]
		{0, Fdup, -2},     // 6  [i]
		{0, Sub},          // 7  [(i-2)]
		{0, Push, 1},      // 8
		{0, Call},         // 9  [fib(i-2)]
		{0, Push, 1},      // 10 [(i-2) i 1]
		{0, Fdup, -2},     // 11 [fib(i-2) i]
		{0, Sub},          // 12 [(i-2) (i-1)]
		{0, Push, 1},      // 13
		{0, Call},         // 14 [fib(i-2) fib(i-1)]
		{0, Add},          // 15 [fib(i-2)+fib(i-1)]
		{0, Return, 1, 1}, // 16 return i
		{0, Fdup, -2},     // 17 [i]
		{0, Return, 1, 1}, // 18 return i
		{0, Push, 6},      // 19 [1]
		{0, Push, 1},      // 20
		{0, Call},         // 21 [fib(*1)]
		{0, Exit},         // 22
	},
	start: 0, end: 1, mem: "[8]",
}, { // #16 -- Fibonacci with some immediate instructions.
	code: [][]int64{
		{0, Jump, 14},     // 0
		{0, Fdup, -2},     // 1  [i]
		{0, Loweri, 2},    // 2  [true/false]
		{0, JumpTrue, 9},  // 3  [], goto 12
		{0, Fdup, -2},     // 4  [i]
		{0, Subi, 2},      // 5  [(i-2)]
		{0, Calli, 1},     // 6  [fib(i-2)]
		{0, Fdup, -2},     // 7  [fib(i-2) i]
		{0, Subi, 1},      // 8  [(i-2) (i-1)]
		{0, Calli, 1},     // 9  [fib(i-2) fib(i-1)], call 1
		{0, Add},          // 10 [fib(i-2)+fib(i-1)]
		{0, Return, 1, 1}, // 11 return i
		{0, Fdup, -2},     // 12 [i]
		{0, Return, 1, 1}, // 13 return i
		{0, Push, 6},      // 14 [1]
		{0, Calli, 1},     // 15 [fib(*1)], call 1
		{0, Exit},         // 16
	},
	start: 0, end: 1, mem: "[8]",
}}
