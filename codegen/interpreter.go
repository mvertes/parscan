package codegen

import (
	"os"

	"github.com/gnolang/parscan/parser"
	"github.com/gnolang/parscan/vm1"
)

const debug = true

type Interpreter struct {
	*parser.Parser
	*Compiler
	*vm1.Machine
}

func NewInterpreter(p *parser.Parser) *Interpreter {
	return &Interpreter{p, NewCompiler(), &vm1.Machine{}}
}

func (i *Interpreter) Eval(src string) (res any, err error) {
	n := &parser.Node{}
	if n.Child, err = i.Parse(src, n); err != nil {
		return res, err
	}
	if debug {
		n.Dot(os.Getenv("DOT"), "")
	}
	codeOffset := len(i.Code)
	dataOffset := 0
	if codeOffset > 0 {
		// All data must be copied to the VM the first time only (re-entrance).
		dataOffset = len(i.Data)
	}
	i.PopExit() // Remove last exit from previous run (re-entrance).
	if err = i.CodeGen(n); err != nil {
		return res, err
	}
	i.Push(i.Data[dataOffset:]...)
	i.PushCode(i.Code[codeOffset:]...)
	i.PushCode([]int64{0, vm1.Exit})
	i.SetIP(max(codeOffset, i.Entry))
	err = i.Run()
	return i.Top(), err
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
