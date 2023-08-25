package main

import (
	"fmt"
	"log"
	"os"

	"github.com/gnolang/parscan/codegen"
	"github.com/gnolang/parscan/lang/golang"
	"github.com/gnolang/parscan/vm0"
)

type Interpreter interface {
	Eval(string) error
}

func main() {
	log.SetFlags(log.Lshortfile)
	buf, err := os.ReadFile("/dev/stdin")
	if err != nil {
		log.Fatal(err)
	}
	var interp Interpreter = vm0.New(golang.GoParser)
	if len(os.Args) > 1 && os.Args[1] == "1" {
		interp = codegen.NewInterpreter(golang.GoParser)
		interp.(*codegen.Interpreter).AddSym(fmt.Println, "println", false)
	}
	if err := interp.Eval(string(buf)); err != nil {
		log.Fatal(err)
	}
}
