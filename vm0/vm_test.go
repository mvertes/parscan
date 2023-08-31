package vm0

import (
	"os"
	"testing"

	"github.com/gnolang/parscan/lang/golang"
)

func TestEval(t *testing.T) {
	i := New(golang.GoParser)
	t.Logf("%#v\n", i.Parser)
	//i.Eval("println(2*5)")
	//n, _ := i.Parse("println(2*5)")
	//n, _ := i.Parse(`a := 2 + 5`)
	src := `a := 2`
	nodes, err := i.Parse(src)
	if err != nil {
		t.Errorf("error %v", err)
	}
	i.Adot(nodes, os.Getenv("DOT"))
	for _, n := range nodes {
		err := i.Run(n, "")
		t.Log(err)
	}
}
