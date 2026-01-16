// Package interp implements an interpreter.
package interp

import (
	"reflect"

	"github.com/mvertes/parscan/comp"
	"github.com/mvertes/parscan/lang"
	"github.com/mvertes/parscan/vm"
)

const debug = true

// Interp represents the state of an interpreter.
type Interp struct {
	*comp.Compiler
	*vm.Machine
}

// NewInterpreter returns a new interpreter.
func NewInterpreter(s *lang.Spec) *Interp {
	return &Interp{comp.NewCompiler(s), &vm.Machine{}}
}

// Eval evaluates code string and return the last produced value if any, or an error.
func (i *Interp) Eval(src string) (res reflect.Value, err error) {
	codeOffset := len(i.Code)
	dataOffset := 0
	if codeOffset > 0 {
		// All data must be copied to the VM the first time only (re-entrance).
		dataOffset = len(i.Data)
	}
	i.PopExit() // Remove last exit from previous run (re-entrance).

	t, err := i.Parse(src)
	if err != nil {
		return res, err
	}
	if err = i.Generate(t); err != nil {
		return res, err
	}
	i.Push(i.Data[dataOffset:]...)
	i.PushCode(i.Code[codeOffset:]...)
	if s, ok := i.Symbols["main"]; ok {
		i.PushCode(vm.Instruction{Op: vm.Push, Arg: []int{int(i.Data[s.Index].Int())}})
		i.PushCode(vm.Instruction{Op: vm.Call, Arg: []int{0}})
	}
	i.PushCode(vm.Instruction{Op: vm.Exit})
	i.SetIP(max(codeOffset, i.Entry))
	if debug {
		i.PrintData()
		i.PrintCode()
	}
	err = i.Run()
	return i.Top().Value, err
}
