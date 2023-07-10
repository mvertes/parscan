package vm0

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gnolang/parscan/lang/golang"
	"github.com/gnolang/parscan/parser"
)

type Interp struct {
	*parser.Parser
	stack []any          // stack memory space
	fp    int            // frame pointer: index of current frame in stack
	sym   map[string]int // symbol table, maps scoped identifiers to offsets relative to fp
}

func New(p *parser.Parser) (i *Interp) {
	i = &Interp{Parser: p, stack: []any{}, sym: map[string]int{}}
	i.sym["println"] = i.push(fmt.Println)
	return i
}

func (i *Interp) Eval(src string) (r []any, err error) {
	n, err := i.Parse(src)
	if err != nil {
		return nil, err
	}
	for _, nod := range n {
		r, err = i.Run(nod, "")
		if err != nil {
			break
		}
	}
	return
}

// Run implements a stack based virtual machine which directly walks the AST.
func (i *Interp) Run(node *parser.Node, scope string) ([]any, error) {
	stop := false

	node.Walk2(nil, 0, func(n, a *parser.Node, k int) (ok bool) {
		// Node pre-order processing.
		switch n.Kind {
		case golang.StmtBloc:
			if a != nil && a.Kind == golang.IfStmt {
				// Control-flow in 'if' sub-tree
				if k == 1 {
					// 'if' first body branch, evaluated when condition is true.
					if len(a.Child) > 2 {
						return i.peek().(bool) // keep condition on stack for else branch
					}
					return i.pop().(bool)
				}
				// 'else' body branch, evaluated when condition is false.
				return !i.pop().(bool)
			}
		case golang.FuncDecl:
			i.declareFunc(n, scope)
			return false
		}
		return true
	}, func(n, a *parser.Node, k int) (ok bool) {
		// Node post-order processing.
		if stop {
			return false
		}
		l := len(i.stack)
		switch n.Kind {
		case golang.NumberLit:
			num, _ := strconv.Atoi(n.Content()) // TODO(marc): compute num value at scanning.
			i.push(num)
		case golang.StringLit:
			i.push(n.Block())
		case golang.InfOp:
			i.stack[l-2] = i.stack[l-2].(int) < i.stack[l-1].(int)
			i.stack = i.stack[:l-1]
		case golang.AddOp:
			i.stack[l-2] = i.stack[l-2].(int) + i.stack[l-1].(int)
			i.stack = i.stack[:l-1]
		case golang.SubOp:
			i.stack[l-2] = i.stack[l-2].(int) - i.stack[l-1].(int)
			i.stack = i.stack[:l-1]
		case golang.MulOp:
			i.stack[l-2] = i.stack[l-2].(int) * i.stack[l-1].(int)
			i.stack = i.stack[:l-1]
		case golang.AssignOp, golang.DefOp:
			i.stack[i.stack[l-2].(int)] = i.stack[l-1]
			i.stack = i.stack[:l-2]
		case golang.ReturnStmt:
			stop = true
			return false
		case golang.CallExpr:
			i.push(len(n.Child[1].Child)) // number of arguments to call
			i.callFunc(n)
		case golang.Ident:
			name := n.Content()
			v, sc, ok := i.getSym(name, scope)
			fp := i.fp
			if sc == "" {
				fp = 0
			}
			if ok {
				if a.Content() == ":=" {
					i.push(fp + v) // reference for assign (absolute index)
					break
				}
				i.push(i.stack[fp+v]) // value
				break
			}
			if a.Content() != ":=" {
				fmt.Println("error: undefined:", name, "scope:", scope)
			}
			v = i.push(any(nil)) - i.fp // v is the address of new value, relative to frame pointer
			i.sym[scope+name] = v       // bind scoped name to address in symbol table
			i.push(v)
		}
		return true
	})
	return nil, nil
}

// getSym searches for an existing symbol starting from the deepest scope.
func (i *Interp) getSym(name, scope string) (index int, sc string, ok bool) {
	for {
		if index, ok = i.sym[scope+name]; ok {
			return index, scope, ok
		}
		scope = strings.TrimSuffix(scope, "/")
		j := strings.LastIndex(scope, "/")
		if j == -1 {
			scope = ""
			break
		}
		if scope = scope[:j]; scope == "" {
			break
		}
	}
	index, ok = i.sym[name]
	return index, scope, ok
}

func (i *Interp) peek() any          { return i.stack[len(i.stack)-1] }
func (i *Interp) push(v any) (l int) { l = len(i.stack); i.stack = append(i.stack, v); return }
func (i *Interp) pop() (v any)       { l := len(i.stack) - 1; v = i.stack[l]; i.stack = i.stack[:l]; return }
