package vm0

import (
	"fmt"
	"os"
	"strings"

	"github.com/gnolang/parscan/parser"
)

const debug = true

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

func (i *Interp) Eval(src string) (res any, err error) {
	n := &parser.Node{}
	if n.Child, err = i.Parse(src, n); err != nil {
		return
	}
	if debug {
		n.Dot(os.Getenv("DOT"), "")
	}
	for _, nod := range n.Child {
		if err = i.Run(nod, ""); err != nil {
			break
		}
	}
	return
}

// Run implements a stack based virtual machine which directly walks the AST.
func (i *Interp) Run(node *parser.Node, scope string) (err error) {
	stop := false

	node.Walk2(nil, 0, func(n, a *parser.Node, k int) (ok bool) {
		// Node pre-order processing.
		switch n.Kind {
		case parser.BlockStmt:
			if a != nil && a.Kind == parser.StmtIf {
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
		case parser.DeclFunc:
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
		case parser.LiteralNumber:
			switch v := n.Value().(type) {
			case int64:
				i.push(int(v))
			case error:
				err = v
				return false
			default:
				err = fmt.Errorf("type not supported: %T\n", v)
				return false
			}
		case parser.LiteralString:
			i.push(n.Block())
		case parser.OpInferior:
			i.stack[l-2] = i.stack[l-2].(int) < i.stack[l-1].(int)
			i.stack = i.stack[:l-1]
		case parser.OpAdd:
			i.stack[l-2] = i.stack[l-2].(int) + i.stack[l-1].(int)
			i.stack = i.stack[:l-1]
		case parser.OpSubtract:
			i.stack[l-2] = i.stack[l-2].(int) - i.stack[l-1].(int)
			i.stack = i.stack[:l-1]
		case parser.OpMultiply:
			i.stack[l-2] = i.stack[l-2].(int) * i.stack[l-1].(int)
			i.stack = i.stack[:l-1]
		case parser.OpAssign, parser.OpDefine:
			i.stack[i.stack[l-2].(int)] = i.stack[l-1]
			i.stack = i.stack[:l-2]
		case parser.StmtReturn:
			stop = true
			return false
		case parser.ExprCall:
			i.push(len(n.Child[1].Child)) // number of arguments to call
			i.callFunc(n)
		case parser.Ident:
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
	return nil
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
