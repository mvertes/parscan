package parser

import "github.com/gnolang/parscan/scanner"

type Kind int

type Node struct {
	Child         []*Node // sub-tree nodes
	scanner.Token         // token at origin of the node
	Kind                  // Node kind, depends on the language spec

	unary bool // TODO(marc): get rid of it.
}

// TODO: remove it in favor of Walk2
func (n *Node) Walk(in, out func(*Node) bool) (stop bool) {
	if in != nil && !in(n) {
		return true
	}
	for _, child := range n.Child {
		if child.Walk(in, out) {
			return
		}
	}
	if out != nil {
		stop = !out(n)
	}
	return
}

// Idem to walk, but also propagates the ancestor of visited node and child index.
func (n *Node) Walk2(a *Node, i int, in, out func(*Node, *Node, int) bool) (stop bool) {
	if in != nil && !in(n, a, i) {
		return true
	}
	for j, child := range n.Child {
		if child.Walk2(n, j, in, out) {
			return
		}
	}
	if out != nil {
		stop = !out(n, a, i)
	}
	return
}
