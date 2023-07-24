package main

import (
	"fmt"
	"log"
	"os"

	"github.com/gnolang/parscan/codegen"
	"github.com/gnolang/parscan/lang/golang"
	"github.com/gnolang/parscan/parser"
	"github.com/gnolang/parscan/vm0"
	"github.com/gnolang/parscan/vm1"
)

func main() {
	log.SetFlags(log.Lshortfile)
	buf, err := os.ReadFile("/dev/stdin")
	if err != nil {
		log.Fatal(err)
	}
	if len(os.Args) > 1 {
		if err := run1(string(buf)); err != nil {
			log.Fatal(err)
		}
	} else {
		if err := run0(string(buf)); err != nil {
			log.Fatal(err)
		}
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
	m := &vm1.Machine{}
	c := &codegen.Compiler{Symbols: map[string]int{}}
	c.AddSym(fmt.Println, "println")
	n := &parser.Node{}
	if n.Child, err = golang.GoParser.Parse(src); err != nil {
		return err
	}
	n.Dot(os.Getenv("DOT"), "")
	if err = c.CodeGen(n); err != nil {
		return err
	}
	c.Emit(n, vm1.Exit)
	log.Println("data:", c.Data)
	log.Println("code:", vm1.Disas(c.Code))
	for _, v := range c.Data {
		m.Push(v)
	}
	m.PushCode(c.Code)
	m.Run()
	return
}
