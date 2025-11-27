package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/mvertes/parscan/interpreter"
	"github.com/mvertes/parscan/lang/golang"
)

func main() {
	log.SetFlags(log.Lshortfile)
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(arg []string) (err error) {
	var str string
	rflag := flag.NewFlagSet("run", flag.ContinueOnError)
	rflag.Usage = func() {
		fmt.Println("Usage: parscan run [options] [path] [args]")
		fmt.Println("Options:")
		rflag.PrintDefaults()
	}
	rflag.StringVar(&str, "e", "", "string to eval")
	if err = rflag.Parse(arg); err != nil {
		return err
	}
	args := rflag.Args()

	interp := interpreter.NewInterpreter(golang.GoSpec)

	var in io.Reader
	if str != "" {
		in = strings.NewReader(str)
	} else {
		in = os.Stdin
	}
	if len(args) > 0 {
		if in, err = os.Open(arg[0]); err != nil {
			return err
		}
		if i2, ok := in.(io.ReadCloser); ok {
			defer i2.Close()
		}
	}

	if isatty(in) {
		return interp.Repl(in)
	}

	buf, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	_, err = interp.Eval(string(buf))
	return err
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
