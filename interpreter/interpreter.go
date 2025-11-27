package interpreter

import (
	"reflect"

	"github.com/mvertes/parscan/compiler"
	"github.com/mvertes/parscan/scanner"
	"github.com/mvertes/parscan/vm"
)

const debug = true

// Interpreter represents the state of an interpreter.
type Interpreter struct {
	*compiler.Compiler
	*vm.Machine
}

// NewInterpreter returns a new interpreter state.
func NewInterpreter(s *scanner.Scanner) *Interpreter {
	return &Interpreter{compiler.NewCompiler(s), &vm.Machine{}}
}

// Eval interprets a src program and return the last produced value if any, or an error.
func (i *Interpreter) Eval(src string) (res reflect.Value, err error) {
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
	if err = i.Codegen(t); err != nil {
		return res, err
	}
	i.Push(i.Data[dataOffset:]...)
	i.PushCode(i.Code[codeOffset:]...)
	if s, ok := i.Symbols["main"]; ok {
		i.PushCode(vm.Instruction{Op: vm.Calli, Arg: []int{int(i.Data[s.Index].Data.Int())}})
	}
	i.PushCode(vm.Instruction{Op: vm.Exit})
	i.SetIP(max(codeOffset, i.Entry))
	if debug {
		i.PrintData()
		i.PrintCode()
	}
	err = i.Run()
	return i.Top().Data, err
}
