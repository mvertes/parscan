package codegen

import (
	"fmt"
	"strings"

	"github.com/gnolang/parscan/parser"
	"github.com/gnolang/parscan/vm1"
)

type symbol struct {
	index int  // address of symbol in frame
	local bool // if true address is relative to local frame, otherwise global
}

type Compiler struct {
	Code  [][]int64 // produced code, to fill VM with
	Data  []any     // produced data, will be at the bottom of VM stack
	Entry int

	symbols map[string]symbol
}

func New() *Compiler { return &Compiler{symbols: map[string]symbol{}, Entry: -1} }

type nodedata struct {
	ipstart, ipend, symind, fsp int // CFG and symbol node annotations
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
		case parser.FuncDecl:
			fname := n.Child[0].Content()
			i := c.Emit(n, vm1.Enter)
			c.AddSym(i, scope+fname, false)
			scope = pushScope(scope, fname)
			frameNode = append(frameNode, n)
			fnote = notes[n]
			for j, child := range n.Child[1].Child {
				vname := child.Content()
				c.AddSym(-j-2, scope+vname, true)
				fnote.fsp++
			}

		case parser.StmtBloc:
			nd.ipstart = len(c.Code)
			if a != nil && a.Kind == parser.IfStmt && k == 1 {
				c.Emit(n, vm1.JumpFalse, 0) // location to be updated in post IfStmt
			}
		}
		return true
	}, func(n, a *parser.Node, k int) (ok bool) {
		// Node post-order processing callback.
		nd := notes[n]

		switch n.Kind {
		case parser.AddOp:
			c.Emit(n, vm1.Add)

		case parser.CallExpr:
			if c.isExternalSymbol(n.Child[0].Content()) {
				// External call, using absolute addr in symtable
				c.Emit(n, vm1.CallX, int64(len(n.Child[1].Child)))
				break
			}
			// Internal call is always relative to instruction pointer.
			i, ok := c.symInt(n.Child[0].Content())
			if !ok {
				err = fmt.Errorf("invalid symbol %s", n.Child[0].Content())
			}
			c.Emit(n, vm1.Call, int64(i-len(c.Code)))

		case parser.DefOp:
			// Define operation, global vars only. TODO: on local frame too
			l := c.AddSym(nil, n.Child[0].Content(), false)
			c.Emit(n, vm1.Assign, int64(l))

		case parser.FuncDecl:
			scope = popScope(scope)
			fnote = notes[frameNode[len(frameNode)-1]]

		case parser.Ident:
			ident := n.Content()
			if len(n.Child) > 0 || a.Kind == parser.FuncDecl {
				break
			}
			if s, _, ok := c.getSym(ident, scope); ok {
				if s.local {
					c.Emit(n, vm1.Fdup, int64(s.index))
				} else if a != nil && a.Kind == parser.AssignOp {
					c.Emit(n, vm1.Push, int64(s.index))
				} else if c.isExternalSymbol(ident) {
					c.Emit(n, vm1.Dup, int64(s.index))
				}
			}

		case parser.IfStmt:
			ifBodyStart := notes[n.Child[1]].ipstart
			ifBodyEnd := notes[n.Child[1]].ipend
			c.Code[ifBodyStart][2] = int64(ifBodyEnd - ifBodyStart)
			// TODO: handle 'else'

		case parser.NumberLit:
			// A literal number can be a float or an integer, or a big number
			switch v := n.Value().(type) {
			case int64:
				c.Emit(n, vm1.Push, v)
			case error:
				err = v
				return false
			default:
				err = fmt.Errorf("type not supported: %T\n", v)
				return false
			}

		case parser.ReturnStmt:
			c.Emit(n, vm1.Return, int64(len(n.Child)))

		case parser.StmtBloc:
			nd.ipend = len(c.Code)

		case parser.StringLit:
			p := len(c.Data)
			c.Data = append(c.Data, n.Block())
			c.Emit(n, vm1.Dup, int64(p))

		case parser.InfOp:
			c.Emit(n, vm1.Lower)

		case parser.SubOp:
			c.Emit(n, vm1.Sub)
		}

		// TODO: Fix this temporary hack to compute an entry point
		if c.Entry < 0 && len(scope) == 0 && n.Kind != parser.FuncDecl {
			c.Entry = len(c.Code) - 1
			if c.Code[c.Entry][1] == vm1.Return {
				c.Entry++
			}
		}
		return true
	})
	return
}

func (c *Compiler) AddSym(v any, name string, local bool) int {
	l := len(c.Data)
	if local {
		l = v.(int)
	} else {
		c.Data = append(c.Data, v)
	}
	c.symbols[name] = symbol{index: l, local: local}
	return l
}

func (c *Compiler) Emit(n *parser.Node, op ...int64) int {
	op = append([]int64{int64(n.Pos())}, op...)
	l := len(c.Code)
	c.Code = append(c.Code, op)
	return l
}

func (c *Compiler) isExternalSymbol(name string) bool {
	s, ok := c.symbols[name]
	if !ok {
		return false
	}
	_, isInt := c.Data[s.index].(int)
	return !isInt
}

func (c *Compiler) symInt(name string) (int, bool) {
	s, ok := c.symbols[name]
	if !ok {
		return 0, false
	}
	j, ok := c.Data[s.index].(int)
	if !ok {
		return 0, false
	}
	return j, true
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
func (c *Compiler) getSym(name, scope string) (sym symbol, sc string, ok bool) {
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
