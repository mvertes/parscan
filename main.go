// The parscan command interprets programs.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/mvertes/parscan/interp"
	"github.com/mvertes/parscan/lang/golang"
)

func main() {
	log.SetFlags(log.Lshortfile)
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(arg []string) error {
	var str string
	rflag := flag.NewFlagSet("run", flag.ContinueOnError)
	rflag.Usage = func() {
		fmt.Println("Usage: parscan run [options] [path] [args]")
		fmt.Println("Options:")
		rflag.PrintDefaults()
	}
	rflag.StringVar(&str, "e", "", "string to eval")
	if err := rflag.Parse(arg); err != nil {
		return err
	}
	args := rflag.Args()

	i := interp.NewInterpreter(golang.GoSpec)
	if str != "" {
		return evalStr(i, str)
	}
	if len(args) == 0 {
		return i.Repl(os.Stdin)
	}
	buf, err := os.ReadFile(arg[0])
	if err != nil {
		return err
	}
	return evalStr(i, string(buf))
}

func evalStr(i *interp.Interp, s string) error {
	_, err := i.Eval(s)
	return err
}
