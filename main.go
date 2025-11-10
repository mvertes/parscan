package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"reflect"

	"github.com/mvertes/parscan/lang/golang"
	"github.com/mvertes/parscan/parser"
	"github.com/mvertes/parscan/scanner"
)

type Interpreter interface {
	Eval(string) (reflect.Value, error)
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
		switch {
		case err == nil:
			if res.IsValid() {
				fmt.Println(": ", res)
			}
			text, prompt = "", "> "
		case errors.Is(err, scanner.ErrBlock):
			prompt = ">> "
		default:
			fmt.Println("Error:", err)
			text, prompt = "", "> "
		}
		fmt.Print(prompt)
	}
	return err
}

func run(arg []string) (err error) {
	rflag := flag.NewFlagSet("run", flag.ContinueOnError)
	rflag.Usage = func() {
		fmt.Println("Usage: parscan run [options] [path] [args]")
		// fmt.Println("Options:")
		// rflag.PrintDefaults()
	}
	if err = rflag.Parse(arg); err != nil {
		return err
	}
	args := rflag.Args()

	interp := parser.NewInterpreter(scanner.NewScanner(golang.GoSpec))

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
