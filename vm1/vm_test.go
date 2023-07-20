package vm1

import (
	"fmt"
	"testing"
)

func TestVM(t *testing.T) {
	for _, test := range tests {
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

var tests = []struct {
	sym        []any     // initial memory values
	code       [][]int64 // bytecode to execute
	start, end int       //
	mem        string    // expected memory content
}{{ // #00 -- A simple addition.
	code: [][]int64{
		{Push, 1},
		{Push, 2},
		{Add},
		{Exit},
	},
	start: 0, end: 1, mem: "[3]",
}, { // #01 -- Calling a function defined outside the VM.
	sym: []any{fmt.Println, "Hello"},
	code: [][]int64{
		{CallX, 1},
		{Exit},
	},
	start: 0, end: 2, mem: "[6 <nil>]",
}, { // #02 -- Defining and calling a function in VM.
	code: [][]int64{
		{Jump, 4},   // 0
		{Enter},     // 1
		{Push, 3},   // 2
		{Return, 1}, // 3
		{Push, 1},   // 4
		{Call, -4},  // 5
		{Exit},      // 6
	},
	start: 0, end: 1, mem: "[3]",
}, { // #03 -- Fibonacci numbers, hand written. Showcase recursivity.
	code: [][]int64{
		{Jump, 18},     // 0, goto 18
		{Enter},        // 1,
		{Fdup, -2},     // 2, [i]
		{Push, 2},      // 3, [i 2]
		{Lower},        // 4, [true/false]
		{JumpTrue, 11}, // 5, [], goto 16
		{Fdup, -2},     // 6  [i]
		{Push, 2},      // 7  [i 2]
		{Sub},          // 8  [(i-2)]
		{Call, -8},     // 9  [fib(i-2)]
		{Fdup, -2},     // 10 [fib(i-2) i]
		{Push, 1},      // 11 [(i-2) i 1]
		{Sub},          // 12 [(i-2) (i-1)]
		{Call, -12},    // 13 [fib(i-2) fib(i-1)]
		{Add},          // 14 [fib(i-2)+fib(i-1)]
		{Return, 1},    // 15 return i
		{Fdup, -2},     // 16 [i]
		{Return, 1},    // 17 return i
		{Push, 6},      // 18 [1]
		{Call, -18},    // 19 [fib(*1)]
		{Exit},         // 20
	},
	start: 0, end: 1, mem: "[8]",
}}
