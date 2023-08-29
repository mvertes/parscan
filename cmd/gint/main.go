package main

import (
	"bufio"
	"errors"
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
	Eval(string) error
}

func main() {
	log.SetFlags(log.Lshortfile)
	var interp Interpreter = vm0.New(golang.GoParser)
	if len(os.Args) > 1 && os.Args[1] == "1" {
		interp = codegen.NewInterpreter(golang.GoParser)
		interp.(*codegen.Interpreter).AddSym(fmt.Println, "println", false)
	}
	in := os.Stdin

	if isatty(in) {
		// Provide an interactive line oriented Read Eval Print Loop (REPL).
		liner := bufio.NewScanner(in)
		text, prompt := "", "> "
		fmt.Printf(prompt)
		for liner.Scan() {
			text += liner.Text()
			err := interp.Eval(text + "\n")
			if err == nil {
				text, prompt = "", "> "
			} else if errors.Is(err, scanner.ErrBlock) {
				prompt = ">> "
			} else {
				text, prompt = "", "> "
				fmt.Println("Error:", err)
			}
			fmt.Printf(prompt)
		}
		return
	}

	buf, err := io.ReadAll(in)
	if err != nil {
		log.Fatal(err)
	}
	if err := interp.Eval(string(buf)); err != nil {
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
