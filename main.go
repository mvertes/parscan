// The parscan command interprets programs.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/mvertes/parscan/interp"
	"github.com/mvertes/parscan/lang/golang"
	"github.com/mvertes/parscan/stdlib"
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
	i.ImportPackageValues(stdlib.Values)
	if str != "" {
		_, err := i.Eval("m:"+str, str)
		return err
	}
	if len(args) == 0 {
		return i.Repl(os.Stdin)
	}
	fpath := filepath.Clean(args[0])
	buf, err := os.ReadFile(fpath)
	if err != nil {
		return err
	}
	_, err = i.Eval("f:"+fpath, string(buf))
	return err
}
