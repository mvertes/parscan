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
			t.Parallel()
			runFile(t, filepath.Join(baseDir, file.Name()))
		})
	}
}

func runFile(t *testing.T, p string) {
	t.Helper()
	buf, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	want, isErr, skip := commentData(p, buf)
	if skip {
		t.Skip()
	}

	var stdout, stderr bytes.Buffer

	i := NewInterpreter(golang.GoSpec)
	i.SetIO(os.Stdin, &stdout, &stderr)

	_, err = i.Eval("f:"+p, string(buf))
	if isErr {
		if err == nil {
			t.Fatalf("got nil error, want: %q", want)
		}
		if res := strings.TrimSpace(err.Error()); !strings.Contains(res, want) {
			t.Errorf("got: %q, want: %q", res, want)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if res := stdout.String(); res != want {
		t.Errorf("\ngot:  %q,\nwant: %q", res, want)
	}
}

func commentData(p string, buf []byte) (text string, isErr, skip bool) {
	fset := token.NewFileSet()
	f, _ := parser.ParseFile(fset, p, buf, parser.ParseComments)
	if len(f.Comments) == 0 {
		return
	}
	text = f.Comments[len(f.Comments)-1].Text()
	switch {
	case strings.HasPrefix(text, "skip:"):
		return "", false, true
	case strings.HasPrefix(text, "Error:\n"):
		return strings.TrimPrefix(text, "Error:\n"), true, false
	case strings.HasPrefix(text, "Output:\n"):
		return strings.TrimPrefix(text, "Output:\n"), false, false
	}
	return
}
