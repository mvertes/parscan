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

func (i *Interpreter) Eval(src string) (err error) {
	n := &parser.Node{}
	if n.Child, err = i.Parse(src); err != nil {
		return err
	}
	if debug {
		n.Dot(os.Getenv("DOT"), "")
	}
	offset := len(i.Code)
	i.PopExit() // Remove last exit from previous run.
	if err = i.CodeGen(n); err != nil {
		return err
	}
	i.Push(i.Data...)
	i.PushCode(i.Code[offset:]...)
	i.PushCode([]int64{0, vm1.Exit})
	i.SetIP(max(offset, i.Entry))
	return i.Run()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
