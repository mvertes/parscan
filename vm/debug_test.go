package vm

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mvertes/parscan/scan"
)

func TestDumpFrame(t *testing.T) {
	// Simulate a frame: [func, arg0, arg1, deferHead, retIP, prevFP, local1, local2]
	//                     0      1     2      3          4       5       6       7
	mem := []Value{
		ValueOf(42),  // func (code address)
		ValueOf(10),  // arg 0
		ValueOf(20),  // arg 1
		{num: 0},     // deferHead
		{num: 15},    // retIP
		{num: 0},     // prevFP (top-level)
		ValueOf(100), // local 1
		ValueOf(200), // local 2
	}

	var buf bytes.Buffer
	DumpFrame(&buf, mem, nil, 6, 8, 2, 1, nil)
	out := buf.String()

	// Verify key elements are present.
	for _, want := range []string{"fp=6", "retIP=15", "prevFP=0", "narg=2", "nret=1", "func", "arg 0", "arg 1", "deferHead", "retIP", "prevFP", "local 1", "local 2"} {
		if !strings.Contains(out, want) {
			t.Errorf("DumpFrame output missing %q:\n%s", want, out)
		}
	}
}

func TestDumpFrame_Invalid(t *testing.T) {
	var buf bytes.Buffer
	DumpFrame(&buf, nil, nil, 0, 0, 0, 0, nil)
	if !strings.Contains(buf.String(), "invalid") {
		t.Errorf("expected 'invalid' message, got: %s", buf.String())
	}
}

func TestDumpFrame_WithDebugInfo(t *testing.T) {
	mem := []Value{
		ValueOf(42),  // func (code address 42)
		ValueOf(10),  // arg 0
		{num: 0},     // deferHead
		{num: 5},     // retIP
		{num: 0},     // prevFP
		ValueOf(100), // local 1
	}
	code := Code{
		{Pos: 0, Op: Push},
		{Pos: 0, Op: Push},
		{Pos: 0, Op: Push},
		{Pos: 0, Op: Push},
		{Pos: 0, Op: Push},
		{Pos: 10, Op: Call}, // retIP=5 points here
	}
	var sources scan.Sources
	sources.Add("m:test", "func foo(n int) int {\n\treturn n + 1\n}\n")
	di := &DebugInfo{
		Sources: sources,
		Labels:  map[int]string{42: "main/foo"},
		Locals: map[string][]LocalVar{
			"main/foo": {
				{Offset: 1, Name: "n"},
				{Offset: 2, Name: "result"},
			},
		},
	}

	var buf bytes.Buffer
	DumpFrame(&buf, mem, code, 5, 6, 1, 1, di)
	out := buf.String()

	for _, want := range []string{"main/foo", "// n", "// result"} {
		if !strings.Contains(out, want) {
			t.Errorf("DumpFrame with debug info missing %q:\n%s", want, out)
		}
	}
}

func TestDumpCallStack(t *testing.T) {
	// Build a two-frame scenario.
	// Frame 0 (outer): func at mem[0], 1 arg, fp=5
	// Frame 1 (inner): func at mem[6], 1 arg, fp=11
	mem := []Value{
		ValueOf(0),  // 0  frame 0: func
		ValueOf(10), // 1  frame 0: arg 0
		{num: 0},    // 2  frame 0: deferHead
		{num: 99},   // 3  frame 0: retIP
		{num: 0},    // 4  frame 0: prevFP (top-level)
		ValueOf(0),  // 5  frame 0: local 1 (fp=5)
		ValueOf(5),  // 6  frame 1: func
		ValueOf(30), // 7  frame 1: arg 0
		{num: 0},    // 8  frame 1: deferHead
		{num: 7},    // 9  frame 1: retIP
		{num: 5},    // 10 frame 1: prevFP -> frame 0 fp=5
		ValueOf(77), // 11 frame 1: local 1 (fp=11)
	}

	m := &Machine{
		mem:       mem,
		fp:        11,
		frames: []frame{{info: 1 | (1 << 16)}, {info: 0 | (1 << 16)}}, // [nret=1,narg=1], [nret=0,narg=1]
	}

	var buf bytes.Buffer
	m.DumpCallStack(&buf, nil)
	out := buf.String()

	if !strings.Contains(out, "Call Stack (2 frames)") {
		t.Errorf("expected 2 frames header, got:\n%s", out)
	}
	if strings.Count(out, "--- Frame") < 2 {
		t.Errorf("expected 2 frame dumps, got:\n%s", out)
	}
}

func TestDumpCallStack_NoFrames(t *testing.T) {
	m := &Machine{}
	var buf bytes.Buffer
	m.DumpCallStack(&buf, nil)
	if !strings.Contains(buf.String(), "no call frames") {
		t.Errorf("expected no frames message, got: %s", buf.String())
	}
}

func TestEnterDebug(t *testing.T) {
	// Build a single-frame scenario with a Trap instruction.
	// func foo(x int) { trap() } ; foo(10)
	code := Code{
		{Op: Push, Arg: []int{4}},      // 0: push func addr (ip=4)
		{Op: Push, Arg: []int{10}},     // 1: push arg
		{Op: Call, Arg: []int{1, 0}},   // 2: call(1 arg, 0 ret)
		{Op: Exit},                     // 3
		{Op: Trap},                     // 4: trap inside the function
		{Op: Return, Arg: []int{0, 1}}, // 5
	}

	var sources2 scan.Sources
	sources2.Add("m:test", "func foo(x int) { trap() }")
	di := &DebugInfo{
		Sources: sources2,
		Labels:  map[int]string{4: "foo"},
		Globals: map[int]string{},
		Locals:  map[string][]LocalVar{},
	}

	in := strings.NewReader("bt\ncont\n")
	var out bytes.Buffer

	m := &Machine{}
	m.SetDebugInfo(func() *DebugInfo { return di })
	m.SetDebugIO(in, &out)
	m.PushCode(code...)
	m.SetIP(0)

	if err := m.Run(); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	result := out.String()
	for _, want := range []string{"trap at ip=4", "debug> ", "Call Stack"} {
		if !strings.Contains(result, want) {
			t.Errorf("enterDebug output missing %q:\n%s", want, result)
		}
	}
}

func TestEnterDebugHelp(t *testing.T) {
	code := Code{
		{Op: Trap}, // 0
		{Op: Exit}, // 1
	}

	in := strings.NewReader("help\nc\n")
	var out bytes.Buffer

	m := &Machine{}
	m.SetDebugIO(in, &out)
	m.PushCode(code...)

	if err := m.Run(); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	result := out.String()
	for _, want := range []string{"stack, bt", "cont, c", "help, h"} {
		if !strings.Contains(result, want) {
			t.Errorf("help output missing %q:\n%s", want, result)
		}
	}
}

func TestDebugInfoPosToLine(t *testing.T) {
	var sources scan.Sources
	sources.Add("m:test", "line1\nline2\nline3\n")
	di := &DebugInfo{Sources: sources}

	tests := []struct {
		pos  Pos
		want string
	}{
		{0, "1:1"},
		{5, "1:6"},
		{6, "2:1"},
		{12, "3:1"},
	}
	for _, tt := range tests {
		got := di.PosToLine(tt.pos)
		if got != tt.want {
			t.Errorf("PosToLine(%d) = %q, want %q", tt.pos, got, tt.want)
		}
	}
}
