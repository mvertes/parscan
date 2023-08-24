package vm1

import (
	"fmt"
	"testing"
)

func TestVM(t *testing.T) {
	for _, test := range tests {
		test := test
		t.Run("", func(t *testing.T) {
			m := &Machine{}
			for _, v := range test.sym {
				m.Push(v)
			}
			m.PushCode(test.code)
			m.Run()
			t.Log(m.mem)
			r := fmt.Sprintf("%v", m.mem[test.start:test.end])
			if r != test.mem {
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
				m.PushCode(test.code)
				b.StartTimer()

				m.Run()
			}
		})
	}
}

var tests = []struct {
	sym        []any     // initial memory values
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
}, { // #01 -- Calling a function defined outside the VM.
	sym: []any{fmt.Println, "Hello"},
	code: [][]int64{
		{0, CallX, 1},
		{0, Exit},
	},
	start: 0, end: 2, mem: "[6 <nil>]",
}, { // #02 -- Defining and calling a function in VM.
	code: [][]int64{
		{0, Jump, 4},   // 0
		{0, Enter},     // 1
		{0, Push, 3},   // 2
		{0, Return, 1}, // 3
		{0, Push, 1},   // 4
		{0, Call, -4},  // 5
		{0, Exit},      // 6
	},
	start: 0, end: 1, mem: "[3]",
}, { // #03 -- Fibonacci numbers, hand written. Showcase recursivity.
	code: [][]int64{
		{0, Jump, 18},     // 0, goto 18
		{0, Enter},        // 1,
		{0, Fdup, -2},     // 2, [i]
		{0, Push, 2},      // 3, [i 2]
		{0, Lower},        // 4, [true/false]
		{0, JumpTrue, 11}, // 5, [], goto 16
		{0, Fdup, -2},     // 6  [i]
		{0, Push, 2},      // 7  [i 2]
		{0, Sub},          // 8  [(i-2)]
		{0, Call, -8},     // 9  [fib(i-2)]
		{0, Fdup, -2},     // 10 [fib(i-2) i]
		{0, Push, 1},      // 11 [(i-2) i 1]
		{0, Sub},          // 12 [(i-2) (i-1)]
		{0, Call, -12},    // 13 [fib(i-2) fib(i-1)]
		{0, Add},          // 14 [fib(i-2)+fib(i-1)]
		{0, Return, 1},    // 15 return i
		{0, Fdup, -2},     // 16 [i]
		{0, Return, 1},    // 17 return i
		{0, Push, 6},      // 18 [1]
		{0, Call, -18},    // 19 [fib(*1)]
		{0, Exit},         // 20
	},
	start: 0, end: 1, mem: "[8]",
}, { // #04 -- Fibonacci with some immediate instructions.
	code: [][]int64{
		{0, Jump, 15},    // 0, goto 15
		{0, Enter},       // 1,
		{0, Fdup, -2},    // 2, [i]
		{0, Loweri, 2},   // 3, [true/false]
		{0, JumpTrue, 9}, // 4, [], goto 13
		{0, Fdup, -2},    // 5  [i]
		{0, Subi, 2},     // 6  [(i-2)]
		{0, Call, -6},    // 7  [fib(i-2)]
		{0, Fdup, -2},    // 8  [fib(i-2) i]
		{0, Subi, 1},     // 9  [(i-2) (i-1)]
		{0, Call, -9},    // 10 [fib(i-2) fib(i-1)], call 1
		{0, Add},         // 11 [fib(i-2)+fib(i-1)]
		{0, Return, 1},   // 12 return i
		{0, Fdup, -2},    // 13 [i]
		{0, Return, 1},   // 14 return i
		{0, Push, 6},     // 15 [1]
		{0, Call, -15},   // 16 [fib(*1)], call 1
		{0, Exit},        // 17
	},
	start: 0, end: 1, mem: "[8]",
}}
