package parser

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
)

func (*Parser) Adot(nodes []*Node, c string) {
	if c == "" {
		return
	}
	n := &Node{Child: nodes}
	n.Dot(c, "")
}

func (n *Node) Dot(c, s string) {
	dw, cmd := dotWriter(c)
	n.astDot(dw, s)
	if cmd == nil {
		return
	}
	if err := cmd.Wait(); err != nil {
		log.Fatal(err)
	}
}

func (n *Node) Sdot(s string) string {
	var buf bytes.Buffer
	n.astDot(&buf, s)
	return buf.String()
}

// TODO: rewrite it using Walk2
func (n *Node) astDot(out io.Writer, label string) {
	fmt.Fprintf(out, "digraph ast { ")
	if label != "" {
		fmt.Fprintf(out, "labelloc=\"t\"; label=\"%s\";", label)
	}
	anc := map[*Node]*Node{}
	index := map[*Node]int{}
	count := 0
	n.Walk(func(nod *Node) bool {
		index[nod] = count
		count++

		for _, c := range nod.Child {
			anc[c] = nod
		}
		name := strings.ReplaceAll(nod.Name(), `"`, `\"`)
		fmt.Fprintf(out, "%d [label=\"%s\"]; ", index[nod], name)
		if anc[nod] != nil {
			fmt.Fprintf(out, "%d -> %d; ", index[anc[nod]], index[nod])
		}
		return true
	}, nil)
	fmt.Fprintf(out, "}")
	if c, ok := out.(io.Closer); ok {
		c.Close()
	}
}

type nopCloser struct {
	io.Writer
}

func (nopCloser) Close() error { return nil }

func dotWriter(dotCmd string) (io.WriteCloser, *exec.Cmd) {
	if dotCmd == "" {
		return nopCloser{io.Discard}, nil
	}
	fields := strings.Fields(dotCmd)
	cmd := exec.Command(fields[0], fields[1:]...)
	dotin, err := cmd.StdinPipe()
	if err != nil {
		log.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		log.Fatal(err)
	}
	return dotin, cmd
}
