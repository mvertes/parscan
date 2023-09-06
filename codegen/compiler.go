package codegen

import (
	"fmt"
	"log"
	"strings"

	"github.com/gnolang/parscan/parser"
	"github.com/gnolang/parscan/vm1"
)

type symbol struct {
	index int          // address of symbol in frame
	local bool         // if true address is relative to local frame, otherwise global
	node  *parser.Node // symbol definition in AST
}

type Compiler struct {
	Code  [][]int64 // produced code, to fill VM with
	Data  []any     // produced data, will be at the bottom of VM stack
	Entry int       // offset in Code to start execution from (skip function defintions)

	symbols map[string]*symbol
}

func NewCompiler() *Compiler { return &Compiler{symbols: map[string]*symbol{}, Entry: -1} }

type nodedata struct {
	ipstart, ipend, fsp int // CFG and symbol node annotations
}

type extNode struct {
	*Compiler
	*parser.Node
	anc  *parser.Node // node ancestor
	rank int          // node rank in ancestor's children
}

func (c *Compiler) CodeGen(node *parser.Node) (err error) {
	notes := map[*parser.Node]*nodedata{} // AST node annotations for CFG, symbols, ...
	scope := ""
	frameNode := []*parser.Node{node}
	fnote := notes[node]

	node.Walk2(nil, 0, func(n, a *parser.Node, k int) (ok bool) {
		// Node pre-order processing callback.
		notes[n] = &nodedata{}
		nd := notes[n]

		switch n.Kind {
		case parser.DeclFunc:
			fname := n.Child[0].Content()
			c.addSym(len(c.Code), scope+fname, false, n)
			scope = pushScope(scope, fname)
			frameNode = append(frameNode, n)
			fnote = notes[n]
			for j, child := range n.Child[1].Child {
				vname := child.Content()
				c.addSym(-j-2, scope+vname, true, child)
				fnote.fsp++
			}

		case parser.BlockStmt:
			nd.ipstart = len(c.Code)
			if a != nil && a.Kind == parser.StmtIf && k == 1 {
				c.Emit(n, vm1.JumpFalse, 0) // location to be updated in post IfStmt
			}
		}
		return true
	}, func(n, a *parser.Node, k int) (ok bool) {
		// Node post-order processing callback.
		nd := notes[n]
		x := extNode{c, n, a, k}

		switch n.Kind {
		case parser.OpAdd:
			c.Emit(n, vm1.Add)

		case parser.ExprCall:
			err = postCallExpr(x)

		case parser.OpDefine:
			// Define operation, global vars only. TODO: on local frame too
			l := c.addSym(nil, n.Child[0].Content(), false, n)
			c.Emit(n, vm1.Assign, int64(l))

		case parser.DeclFunc:
			fun := frameNode[len(frameNode)-1]
			if len(fun.Child) == 3 { // no return values
				if c.Code[len(c.Code)-1][1] != vm1.Return {
					c.Emit(n, vm1.Return, 0, int64(len(fun.Child[1].Child)))
				}
			}
			scope = popScope(scope)
			fnote = notes[fun]

		case parser.Ident:
			ident := n.Content()
			if len(n.Child) > 0 || a.Kind == parser.DeclFunc {
				break
			}
			if s, _, ok := c.getSym(ident, scope); ok {
				if s.local {
					c.Emit(n, vm1.Fdup, int64(s.index))
				} else if a != nil && a.Kind == parser.OpAssign {
					c.Emit(n, vm1.Push, int64(s.index))
				} else if _, ok := c.Data[s.index].(int); !ok {
					c.Emit(n, vm1.Dup, int64(s.index))
				}
			}

		case parser.StmtIf:
			ifBodyStart := notes[n.Child[1]].ipstart
			ifBodyEnd := notes[n.Child[1]].ipend
			c.Code[ifBodyStart][2] = int64(ifBodyEnd - ifBodyStart)
			// TODO: handle 'else'

		case parser.LiteralNumber:
			// A literal number can be a float or an integer, or a big number
			switch v := n.Value().(type) {
			case int64:
				c.Emit(n, vm1.Push, v)
			case error:
				err = v
			default:
				err = fmt.Errorf("type not supported: %T\n", v)
			}

		case parser.StmtReturn:
			fun := frameNode[len(frameNode)-1]
			nret := 0
			if len(fun.Child) > 3 {
				if ret := fun.Child[2]; ret.Kind == parser.BlockParen {
					nret = len(ret.Child)
				} else {
					nret = 1
				}
			}
			c.Emit(n, vm1.Return, int64(nret), int64(len(fun.Child[1].Child)))

		case parser.BlockStmt:
			nd.ipend = len(c.Code)

		case parser.LiteralString:
			p := len(c.Data)
			c.Data = append(c.Data, n.Block())
			c.Emit(n, vm1.Dup, int64(p))

		case parser.OpInferior:
			c.Emit(n, vm1.Lower)

		case parser.OpSubtract:
			c.Emit(n, vm1.Sub)
		}

		if err != nil {
			return false
		}

		// TODO: Fix this temporary hack to compute an entry point
		if c.Entry < 0 && len(scope) == 0 && n.Kind != parser.DeclFunc {
			c.Entry = len(c.Code) - 1
			if c.Entry >= 0 && len(c.Code) > c.Entry && c.Code[c.Entry][1] == vm1.Return {
				c.Entry++
			}
		}
		return true
	})

	if s, _, ok := c.getSym("main", ""); ok {
		if i, ok := c.codeIndex(s); ok {
			// Internal call is always relative to instruction pointer.
			c.Emit(nil, vm1.Call, int64(i-len(c.Code)))
			c.Entry = len(c.Code) - 1
		}
		log.Println(vm1.Disassemble(c.Code))
	}

	return
}

func (c *Compiler) AddSym(v any, name string) int { return c.addSym(v, name, false, nil) }

func (c *Compiler) addSym(v any, name string, local bool, n *parser.Node) int {
	l := len(c.Data)
	if local {
		l = v.(int)
	} else {
		c.Data = append(c.Data, v)
	}
	c.symbols[name] = &symbol{index: l, local: local, node: n}
	return l
}

func (c *Compiler) Emit(n *parser.Node, op ...int64) int {
	var pos int64
	if n != nil {
		pos = int64(n.Pos())
	}
	op = append([]int64{pos}, op...)
	l := len(c.Code)
	c.Code = append(c.Code, op)
	return l
}

func (c *Compiler) codeIndex(s *symbol) (i int, ok bool) {
	i, ok = c.Data[s.index].(int)
	return
}

func pushScope(scope, name string) string {
	return strings.TrimPrefix(scope+"/"+name+"/", "/")
}

func popScope(scope string) string {
	scope = strings.TrimSuffix(scope, "/")
	j := strings.LastIndex(scope, "/")
	if j == -1 {
		return ""
	}
	return scope[:j]
}

// getSym searches for an existing symbol starting from the deepest scope.
func (c *Compiler) getSym(name, scope string) (sym *symbol, sc string, ok bool) {
	for {
		if sym, ok = c.symbols[scope+name]; ok {
			return sym, scope, ok
		}
		scope = strings.TrimSuffix(scope, "/")
		i := strings.LastIndex(scope, "/")
		if i == -1 {
			scope = ""
			break
		}
		if scope = scope[:i]; scope == "" {
			break
		}
	}
	sym, ok = c.symbols[name]
	return sym, scope, ok
}
