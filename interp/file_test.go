package interp

import (
	"bytes"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvertes/parscan/lang/golang"
)

func TestFile(t *testing.T) {
	t.Skip("not ready")
	baseDir := filepath.Join("..", "_samples")
	files, err := os.ReadDir(baseDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if filepath.Ext(file.Name()) != ".go" {
			continue
		}
		t.Run(file.Name(), func(t *testing.T) {
			runFile(t, filepath.Join(baseDir, file.Name()))
		})
	}
}

func runFile(t *testing.T, p string) {
	buf, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	want, isErr := commentData(p, buf)
	t.Log("want:", isErr, want)

	i := NewInterpreter(golang.GoSpec)
	i.SetIO(os.Stdin, &stdout, &stderr)

	_, err = i.Eval("f:"+p, string(buf))
	t.Log("out:", stdout.String(), err)
}

func commentData(p string, buf []byte) (text string, isErr bool) {
	fset := token.NewFileSet()
	f, _ := parser.ParseFile(fset, p, buf, parser.ParseComments)
	if len(f.Comments) == 0 {
		return
	}
	text = f.Comments[len(f.Comments)-1].Text()
	if strings.HasPrefix(text, "Error:\n") {
		return strings.TrimPrefix(text, "Error:\n"), true
	} else if strings.HasPrefix(text, "Output:\n") {
		return strings.TrimPrefix(text, "Output:\n"), false
	}
	return
}
