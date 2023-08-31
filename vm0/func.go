package vm0

import (
	"reflect"
	"strings"

	"github.com/gnolang/parscan/parser"
)

var types = map[string]reflect.Type{
	"int":    reflect.TypeOf(0),
	"string": reflect.TypeOf(""),
}

func (i *Interp) callFunc(n *parser.Node) {
	fp := i.fp
	l := len(i.stack)
	nargs := i.stack[l-1].(int)
	args := make([]reflect.Value, nargs)
	for j := range args {
		args[nargs-j-1] = reflect.ValueOf(i.stack[l-2-j])
	}
	f := reflect.ValueOf(i.stack[l-2-nargs])
	out := f.Call(args)
	i.fp = fp
	i.stack = i.stack[:l-2-nargs]
	for _, v := range out {
		i.push(v.Interface())
	}
}

func (i *Interp) declareFunc(r *parser.Node, scope string) {
	fname := r.Child[0].Content()

	// Add symbols for input and output function arguments.
	inArgs := r.Child[1].Child
	fscope := strings.TrimPrefix(scope+"/"+fname+"/", "/")
	in := make([]reflect.Type, len(inArgs))
	for j, c := range inArgs {
		i.sym[fscope+c.Content()] = j
		in[j] = types[c.Child[0].Content()]
	}
	var out []reflect.Type
	if len(r.Child) > 3 { // function has return values
		if i.IsBlock(r.Child[2]) {
			outArgs := r.Child[2].Child
			out = make([]reflect.Type, len(outArgs))
			for j, c := range outArgs {
				out[j] = types[c.Content()]
			}
		} else {
			out = []reflect.Type{types[r.Child[2].Content()]}
		}
	}
	funT := reflect.FuncOf(in, out, false)

	// Generate a wrapper function which will run function body AST.
	f := reflect.MakeFunc(funT, func(args []reflect.Value) (res []reflect.Value) {
		i.fp = len(i.stack) // fp will be restored by caller (callFunc).
		for _, arg := range args {
			i.push(arg.Interface())
		}
		if err := i.Run(r.Child[len(r.Child)-1], fscope); err != nil {
			panic(err)
		}
		b := len(i.stack) - len(out)
		for j := range out {
			res = append(res, reflect.ValueOf(i.stack[b+j]))
		}
		return res
	})

	// Add a symbol for newly created func.
	i.sym[scope+fname] = i.push(f.Interface()) - i.fp
}
