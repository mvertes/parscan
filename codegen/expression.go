package codegen

import (
	"fmt"
	"reflect"

	"github.com/gnolang/parscan/parser"
	"github.com/gnolang/parscan/vm1"
)

func postCallExpr(x extNode) error {
	switch x.Child[0].Kind {
	case parser.Ident:
		var numOut int
		s, _, ok := x.getSym(x.Child[0].Content(), "")
		if !ok {
			return fmt.Errorf("invalid symbol %s", x.Child[0].Content())
		}
		if i, ok := x.codeIndex(s); ok {
			// Internal call is always relative to instruction pointer.
			x.Emit(x.Node, vm1.Call, int64(i-len(x.Code)))
		} else {
			// External call, using absolute addr in symtable.
			x.Emit(x.Node, vm1.CallX, int64(len(x.Child[1].Child)))
			numOut = reflect.TypeOf(x.Data[s.index]).NumOut()
		}
		if !usedRet(x.anc) {
			x.Emit(x.Node, vm1.Pop, int64(numOut))
		}
	}
	return nil
}

func usedRet(n *parser.Node) bool {
	switch n.Kind {
	case parser.Undefined, parser.StmtBloc:
		return false
	default:
		return true
	}
}
