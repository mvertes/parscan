package interp

import (
	"bufio"
	"errors"
	"fmt"
	"io"

	"github.com/mvertes/parscan/scan"
)

// Repl executes an interactive line oriented Read Eval Print Loop (REPL).
func (i *Interp) Repl(in io.Reader) (err error) {
	liner := bufio.NewScanner(in)
	text, prompt := "", "> "
	fmt.Print(prompt)
	for liner.Scan() {
		text += liner.Text()
		res, err := i.Eval(text + "\n")
		switch {
		case err == nil:
			if res.IsValid() {
				fmt.Println(": ", res)
			}
			text, prompt = "", "> "
		case errors.Is(err, scan.ErrBlock):
			prompt = ">> "
		default:
			fmt.Println("Error:", err)
			text, prompt = "", "> "
		}
		fmt.Print(prompt)
	}
	return err
}
