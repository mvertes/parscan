package parser

import (
	"github.com/gnolang/parscan/scanner"
)

const (
	Stmt = 1 << iota
	ExprSep
	Call
	Index
	Decl
)

type NodeSpec struct {
	Kind       // AST node kind
	Flags uint // composable properties used for AST generation
	Order int  // operator precedence order
}

type Parser struct {
	*scanner.Scanner
	Spec map[string]NodeSpec
}

func (p *Parser) Parse(src string) (n []*Node, err error) {
	tokens, err := p.Scan(src)
	if err != nil {
		return
	}
	return p.ParseTokens(tokens)
}

func (p *Parser) ParseTokens(tokens []scanner.Token) (roots []*Node, err error) {
	// TODO: error handling.
	var root *Node              // current root node
	var expr *Node              // current expression root node
	var prev, c *Node           // previous and current nodes
	var lce *Node               // last complete expression node
	unaryOp := map[*Node]bool{} // unaryOp indicates if a node is an unary operator.

	for i, t := range tokens {
		prev = c
		c = &Node{
			Token: t,
			Kind:  p.Spec[t.Name()].Kind,
		}
		if c.Kind == Comment {
			continue
		}
		if t.IsOperator() && (i == 0 || tokens[i-1].IsOperator()) {
			unaryOp[c] = true
		}
		if c.Kind == Undefined {
			switch t.Kind() {
			case scanner.Number:
				c.Kind = LiteralNumber
			case scanner.Identifier:
				c.Kind = Ident
			}
		}

		if root == nil {
			if p.isSep(c) {
				continue
			}
			lce = nil
			root = c
			if p.isExpr(c) {
				expr = c
			}
			continue
		}

		if t.IsBlock() {
			if expr != nil {
				if p.hasProp(c, ExprSep) && p.isExprSep(root) {
					// A bracket block may end a previous expression.
					root.Child = append(root.Child, expr)
					expr = nil
				} else if p.hasProp(c, Call) && !p.hasProp(root, Decl) && p.canCallToken(tokens[i-1]) {
					// Handle (possibly nested) call expressions.
					if lce == nil || lce != expr { // TODO(marc): not general, fix it.
						lce = prev
					}
					lce.Child = []*Node{{Token: lce.Token, Child: lce.Child, Kind: lce.Kind}}
					lce.Token = scanner.NewToken("Call", c.Pos())
					lce.Kind = ExprCall
				}
			}
			tcont := t.Content()
			s := tcont[t.Start() : len(tcont)-t.End()]
			n2, err := p.Parse(s)
			if err != nil {
				return nil, err
			}
			c.Child = append(c.Child, n2...)
		}

		// Process the end of an expression or a statement.
		if t.IsSeparator() {
			if expr != nil && p.hasProp(root, Stmt) {
				root.Child = append(root.Child, expr)
				if p.hasProp(expr, ExprSep) {
					roots = append(roots, root)
					root = nil
				}
				expr = nil
			} else {
				if expr != nil {
					root = expr
				}
				roots = append(roots, root)
				expr = nil
				root = nil
			}
			continue
		}

		// We assume from now that current node is part of an expression subtree.
		if expr == nil {
			if p.isStatement(root) {
				expr = c
				continue
			}
			expr = root
		}

		// Update the expression subtree according to binary operator precedence rules.
		// - operators are binary infix by default.
		// - if an operator follows another, then it's unary prefix.
		// - if an expression starts by an operator, then it's unary prefix.
		// - non operator nodes have a default precedence of 0.
		// TODO: handle postfix unary (i.e. ++) and ternary (i.e. ?:)
		//
		ep := p.Spec[expr.Content()].Order
		cp := p.Spec[c.Content()].Order
		a := expr
		if unaryOp[c] {
			cp = 0
		}
		if cp != 0 {
			if cp > ep {
				// Perform an operator permutation at expr root as required by precedence.
				// TODO(marc): maybe it can be generalized in below else branch.
				expr, c = c, expr
				a = expr // Temporary ancestor: its children may have to be permuted.
			} else {
				// Findout if an operator permutation is necessary in subtree.
				c1 := expr
				for {
					a = c1
					if unaryOp[c1] {
						c1, c = c, c1
						a = c1
						if c == expr {
							expr = a
						}
						break
					}
					if len(c1.Child) < 2 {
						break
					}
					c1 = c1.Child[1]
					if !c1.IsOperator() || unaryOp[c1] || cp > p.Spec[c1.Content()].Order {
						break
					}
				}
				// No permutation occured. Append current to last visited ancestor.
				if len(a.Child) > 1 {
					a.Child = a.Child[:1]
					c.Child = append(c.Child, c1)
				}
			}
		} else if ep != 0 {
			for len(a.Child) > 1 {
				a = a.Child[1]
			}
		}
		a.Child = append(a.Child, c)
		if p.hasProp(a, Call) {
			lce = a
		}
	}
	if root != nil && p.isStatement(root) {
		if expr != nil {
			root.Child = append(root.Child, expr)
		}
	} else if expr != nil {
		root = expr
	}
	if root != nil {
		roots = append(roots, root)
	}
	return roots, err
}

func (p *Parser) hasProp(n *Node, prop uint) bool { return p.Spec[n.Name()].Flags&prop != 0 }
func (p *Parser) isStatement(n *Node) bool        { return p.Spec[n.Content()].Flags&Stmt != 0 }
func (p *Parser) isExprSep(n *Node) bool          { return p.Spec[n.Content()].Flags&ExprSep != 0 }
func (p *Parser) isExpr(n *Node) bool             { return !p.isStatement(n) && !p.isExprSep(n) }
func (p *Parser) isSep(n *Node) bool              { return n.Token.Kind() == scanner.Separator }
func (p *Parser) IsBlock(n *Node) bool            { return n.Token.Kind() == scanner.Block }

func (p *Parser) precedenceToken(t scanner.Token) int {
	s := t.Content()
	if l := t.Start(); l > 0 {
		s = s[:l]
	}
	return p.Spec[s].Order
}

func (p *Parser) canCallToken(t scanner.Token) bool {
	return p.precedenceToken(t) == 0 || p.Spec[t.Name()].Flags&Call != 0
}
