package main

import (
	"fmt"
	"log"
	"os"

	"github.com/gnolang/parscan/codegen"
	"github.com/gnolang/parscan/lang/golang"
	"github.com/gnolang/parscan/vm0"
)

func main() {
	log.SetFlags(log.Lshortfile)
	buf, err := os.ReadFile("/dev/stdin")
	if err != nil {
		log.Fatal(err)
	}
	run := run0
	if len(os.Args) > 1 {
		v := "vm" + os.Args[1]
		switch v {
		case "vm0":
		case "vm1":
			run = run1
		default:
			log.Fatal("invalid argument", os.Args[1])
		}
	}
	if err := run(string(buf)); err != nil {
		log.Fatal(err)
	}
}

func run0(src string) error {
	i := vm0.New(golang.GoParser)
	nodes, err := i.Parse(src)
	if err != nil {
		return err
	}
	i.Adot(nodes, os.Getenv("DOT"))
	for _, n := range nodes {
		if _, err := i.Run(n, ""); err != nil {
			return err
		}
	}
	return nil
}

func run1(src string) (err error) {
	i := codegen.NewInterpreter(golang.GoParser)
	i.AddSym(fmt.Println, "println", false)
	return i.Eval(src)
}
