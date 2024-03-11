package parser

import (
	"github.com/mvertes/parscan/scanner"
	"github.com/mvertes/parscan/vm"
)

const debug = true

type Interpreter struct {
	*Compiler
	*vm.Machine
}

func NewInterpreter(s *scanner.Scanner) *Interpreter {
	return &Interpreter{NewCompiler(s), &vm.Machine{}}
}

func (i *Interpreter) Eval(src string) (res any, err error) {
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
	i.PushCode([]int64{0, vm.Exit})
	i.SetIP(max(codeOffset, i.Entry))
	if debug {
		i.PrintData()
		i.PrintCode()
	}
	err = i.Run()
	return i.Top().Data, err
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
