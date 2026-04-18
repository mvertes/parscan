// The parscan command interprets programs.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/mvertes/parscan/interp"
	"github.com/mvertes/parscan/lang/golang"
	"github.com/mvertes/parscan/stdlib"
	_ "github.com/mvertes/parscan/stdlib/jsonx"
)

// newlineTracker wraps a writer and tracks whether the last byte written was a newline.
type newlineTracker struct {
	w       io.Writer
	written bool
	last    byte
}

func (t *newlineTracker) Write(p []byte) (int, error) {
	if len(p) > 0 {
		t.written = true
		t.last = p[len(p)-1]
	}
	return t.w.Write(p)
}

func main() {
	log.SetFlags(log.Lshortfile)
	if err := dispatch(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func dispatch(args []string) error {
	if len(args) == 0 {
		return runCmd(nil)
	}
	switch args[0] {
	case "-h", "--help", "help":
		usage(os.Stdout)
		return nil
	case "run":
		return runCmd(args[1:])
	}
	return runCmd(args)
}

func usage(w io.Writer) {
	_, _ = fmt.Fprintln(w, "Usage: parscan <command> [arguments]")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Commands:")
	_, _ = fmt.Fprintln(w, "  run    run a Go source file, evaluate an expression, or start the REPL")
	_, _ = fmt.Fprintln(w, "  help   show this help")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, `Use "parscan <command> -h" for details on a command.`)
}

func runCmd(arg []string) error {
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

	out := &newlineTracker{w: os.Stdout}
	i.SetIO(os.Stdin, out, os.Stderr)

	var err error
	switch {
	case str != "":
		_, err = i.Eval("m:"+str, str)
	case len(args) == 0:
		return i.Repl(os.Stdin)
	default:
		fpath := filepath.Clean(args[0])
		var buf []byte
		buf, err = os.ReadFile(fpath)
		if err != nil {
			return err
		}
		_, err = i.Eval("f:"+fpath, string(buf))
	}
	// Ensure output ends with a newline so the shell prompt is not overwritten.
	if out.written && out.last != '\n' {
		_, _ = fmt.Fprintln(os.Stdout)
	}
	return err
}
