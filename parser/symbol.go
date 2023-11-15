package parser

import (
	"fmt"
	"go/constant"
	"reflect"
	"strings"
)

type symKind int

const (
	symValue symKind = iota // a Go value defined in the runtime
	symType                 // a Go type
	symLabel                // a label indication a position in the VM code
	symConst                // a Go constant
	symVar                  // a Go variable, located in the VM memory
	symFunc                 // a Go function, located in the VM code
)

const unsetAddr = -65535

type symbol struct {
	kind  symKind
	index int // address of symbol in frame
	value any
	cval  constant.Value
	Type  reflect.Type
	local bool // if true address is relative to local frame, otherwise global
	used  bool
}

func (p *Parser) AddSym(i int, name string, v any) { p.addSym(i, name, v, symValue, nil, false) }

func (p *Parser) addSym(i int, name string, v any, k symKind, t reflect.Type, local bool) {
	name = strings.TrimPrefix(name, "/")
	p.symbols[name] = &symbol{kind: k, index: i, local: local, value: v, Type: t, used: true}
}

// getSym searches for an existing symbol starting from the deepest scope.
func (p *Parser) getSym(name, scope string) (sym *symbol, sc string, ok bool) {
	for {
		if sym, ok = p.symbols[scope+"/"+name]; ok {
			return sym, scope, ok
		}
		i := strings.LastIndex(scope, "/")
		if i == -1 {
			i = 0
		}
		if scope = scope[:i]; scope == "" {
			break
		}
	}
	sym, ok = p.symbols[name]
	return sym, scope, ok
}

func initUniverse() map[string]*symbol {
	return map[string]*symbol{
		"any":    {kind: symType, index: unsetAddr, Type: reflect.TypeOf((*any)(nil)).Elem()},
		"bool":   {kind: symType, index: unsetAddr, Type: reflect.TypeOf((*bool)(nil)).Elem()},
		"error":  {kind: symType, index: unsetAddr, Type: reflect.TypeOf((*error)(nil)).Elem()},
		"int":    {kind: symType, index: unsetAddr, Type: reflect.TypeOf((*int)(nil)).Elem()},
		"string": {kind: symType, index: unsetAddr, Type: reflect.TypeOf((*string)(nil)).Elem()},

		"nil":   {index: unsetAddr},
		"iota":  {kind: symConst, index: unsetAddr},
		"true":  {index: unsetAddr, value: true, Type: reflect.TypeOf(true)},
		"false": {index: unsetAddr, value: false, Type: reflect.TypeOf(false)},

		"println": {index: unsetAddr, value: func(v ...any) { fmt.Println(v...) }},
	}
}
