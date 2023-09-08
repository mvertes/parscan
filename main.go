package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/gnolang/parscan/codegen"
	"github.com/gnolang/parscan/lang/golang"
	"github.com/gnolang/parscan/scanner"
	"github.com/gnolang/parscan/vm0"
)

type Interpreter interface {
	Eval(string) (any, error)
}

func main() {
	log.SetFlags(log.Lshortfile)
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

// isatty returns true if the input stream is a tty (i.e. a character device).
func isatty(in io.Reader) bool {
	s, ok := in.(interface{ Stat() (os.FileInfo, error) })
	if !ok {
		return false
	}
	stat, err := s.Stat()
	return err == nil && stat.Mode()&os.ModeCharDevice != 0
}

// repl executes an interactive line oriented Read Eval Print Loop (REPL).
func repl(interp Interpreter, in io.Reader) (err error) {
	liner := bufio.NewScanner(in)
	text, prompt := "", "> "
	fmt.Print(prompt)
	for liner.Scan() {
		text += liner.Text()
		res, err := interp.Eval(text + "\n")
		if err == nil {
			if res != nil {
				fmt.Println(": ", res)
			}
			text, prompt = "", "> "
		} else if errors.Is(err, scanner.ErrBlock) {
			prompt = ">> "
		} else {
			fmt.Println("Error:", err)
			text, prompt = "", "> "
		}
		fmt.Print(prompt)
	}
	return
}

func run(arg []string) (err error) {
	var i int

	rflag := flag.NewFlagSet("run", flag.ContinueOnError)
	rflag.IntVar(&i, "i", 1, "set interpreter version for execution, possible values: 0, 1")
	rflag.Usage = func() {
		fmt.Println("Usage: parscan run [options] [path] [args]")
		fmt.Println("Options:")
		rflag.PrintDefaults()
	}
	if err = rflag.Parse(arg); err != nil {
		return err
	}
	args := rflag.Args()

	var interp Interpreter
	switch i {
	case 0:
		interp = vm0.New(golang.GoParser)
	case 1:
		interp = codegen.NewInterpreter(golang.GoParser)
		interp.(*codegen.Interpreter).AddSym(fmt.Println, "println")
	default:
		return fmt.Errorf("invalid interpreter version: %v", i)
	}

	log.Println("args:", args)
	in := os.Stdin
	if len(args) > 0 {
		if in, err = os.Open(arg[0]); err != nil {
			return err
		}
		defer in.Close()
	}

	if isatty(in) {
		return repl(interp, in)
	}

	buf, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	_, err = interp.Eval(string(buf))
	return err
}
