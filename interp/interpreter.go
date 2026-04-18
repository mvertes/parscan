// Package interp implements an interpreter.
package interp

import (
	"fmt"
	"os"
	"reflect"

	"github.com/mvertes/parscan/comp"
	"github.com/mvertes/parscan/lang"
	"github.com/mvertes/parscan/stdlib"
	"github.com/mvertes/parscan/stdlib/jsonx"
	"github.com/mvertes/parscan/vm"
)

var debug = os.Getenv("PARSCAN_DEBUG") != ""

// Interp represents the state of an interpreter.
type Interp struct {
	*comp.Compiler
	*vm.Machine
	stdlibPatched bool
}

// NewInterpreter returns a new interpreter.
func NewInterpreter(s *lang.Spec) *Interp {
	i := &Interp{Compiler: comp.NewCompiler(s), Machine: vm.NewMachine()}
	i.SetStdlibFS(stdlib.SrcFS())
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

	if !i.stdlibPatched {
		i.patchStdlibOverrides()
		i.stdlibPatched = true
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

// patchStdlibOverrides installs parscan-aware replacements for stdlib
// bindings that cannot be satisfied by the reflection-based generated
// wrappers alone — either because the binding needs access to the
// machine (fmt I/O redirection) or because the package needs to
// dispatch parscan methods through the VM (encoding/json via jsonx).
func (i *Interp) patchStdlibOverrides() {
	i.patchFmtBindings()
	i.patchJSONBindings()
}

// patchJSONBindings installs jsonx as the runtime implementation of
// encoding/json.Marshal, MarshalIndent, and friends. The native
// stubs remain registered in stdlib.Values for compile-time type
// matching; at runtime the VM diverts calls into jsonx via
// Machine.RegisterParscanAware.
func (i *Interp) patchJSONBindings() {
	pkg, ok := i.Packages["encoding/json"]
	if !ok {
		return
	}
	jsonx.Register(i.Machine, pkg.Values)
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
