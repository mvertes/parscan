// Package interp implements an interpreter.
package interp

import (
	"fmt"
	"os"
	"reflect"

	"github.com/mvertes/parscan/comp"
	"github.com/mvertes/parscan/lang"
	"github.com/mvertes/parscan/vm"
)

var debug = os.Getenv("PARSCAN_DEBUG") != ""

// Interp represents the state of an interpreter.
type Interp struct {
	*comp.Compiler
	*vm.Machine
	fmtPatched bool
}

// NewInterpreter returns a new interpreter.
func NewInterpreter(s *lang.Spec) *Interp {
	i := &Interp{Compiler: comp.NewCompiler(s), Machine: vm.NewMachine()}
	return i
}

// Eval evaluates code string and return the last produced value if any, or an error.
// name identifies the source ("m:<content>" for inline, "f:<path>" for file).
func (i *Interp) Eval(name, src string) (res reflect.Value, err error) {
	codeOffset := len(i.Code)
	dataOffset := 0
	if codeOffset > 0 {
		// All data must be copied to the VM the first time only (re-entrance).
		dataOffset = len(i.Data)
	}
	i.PopExit() // Remove last exit from previous run (re-entrance).
	initsBefore := len(i.InitFuncs)

	if !i.fmtPatched {
		i.patchFmtBindings()
		i.fmtPatched = true
	}

	if err = i.Compile(name, src); err != nil {
		return res, err
	}

	i.Machine.MethodNames = i.Compiler.MethodNames()

	i.TrimStack()
	i.Push(i.Data[dataOffset:]...)
	i.PushCode(i.Code[codeOffset:]...)
	emitCall := func(fn string) {
		if s, ok := i.Symbols[fn]; ok {
			i.PushCode(vm.Instruction{Op: vm.Push, A: int32(i.Data[s.Index].Int())}) //nolint:gosec
			i.PushCode(vm.Instruction{Op: vm.Call})
		}
	}
	for _, fn := range i.InitFuncs[initsBefore:] {
		emitCall(fn)
	}
	emitCall("main")
	i.PushCode(vm.Instruction{Op: vm.Exit})
	i.SetIP(max(codeOffset, i.Entry))
	i.SetDebugInfo(func() *vm.DebugInfo { return i.BuildDebugInfo() })
	if debug {
		i.PrintData()
		i.PrintCode()
	}
	err = i.Run()
	return i.Top().Reflect(), err
}

// patchFmtBindings overrides fmt.Print, Printf, Println with versions
// that write to the machine's configured output writer instead of os.Stdout.
func (i *Interp) patchFmtBindings() {
	pkg, ok := i.Packages["fmt"]
	if !ok {
		return
	}
	m := i.Machine
	pkg.Values["Print"] = vm.FromReflect(reflect.ValueOf(func(a ...any) (int, error) {
		return fmt.Fprint(m.Out(), a...)
	}))
	pkg.Values["Printf"] = vm.FromReflect(reflect.ValueOf(func(format string, a ...any) (int, error) {
		return fmt.Fprintf(m.Out(), format, a...)
	}))
	pkg.Values["Println"] = vm.FromReflect(reflect.ValueOf(func(a ...any) (int, error) {
		return fmt.Fprintln(m.Out(), a...)
	}))

	// Also export the Stringer type so interpreted code can reference it.
	if _, ok := pkg.Values["Stringer"]; !ok {
		pkg.Values["Stringer"] = vm.FromReflect(reflect.ValueOf((*fmt.Stringer)(nil)))
	}
}
