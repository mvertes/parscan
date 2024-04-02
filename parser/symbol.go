package parser

import (
	"fmt"
	"go/constant"
	"strings"

	"github.com/mvertes/parscan/vm"
)

type symKind int

const (
	symValue symKind = iota // a Go value defined in the runtime
	symType                 // a Go type
	symLabel                // a label indication a position in the VM code
	symConst                // a Go constant
	symVar                  // a Go variable, located in the VM memory
	symFunc                 // a Go function, located in the VM code
	symPkg                  // a Go package
)

const unsetAddr = -65535

type symbol struct {
	kind    symKind
	index   int            // address of symbol in frame
	pkgPath string         //
	Type    *vm.Type       //
	value   vm.Value       //
	cval    constant.Value //
	local   bool           // if true address is relative to local frame, otherwise global
	used    bool           //
}

func symtype(s *symbol) *vm.Type {
	if s.Type != nil {
		return s.Type
	}
	return vm.TypeOf(s.value)
}

// AddSym add a new named value at memory position i in the parser symbol table.
func (p *Parser) AddSym(i int, name string, v vm.Value) {
	p.addSym(i, name, v, symValue, nil, false)
}

func (p *Parser) addSym(i int, name string, v vm.Value, k symKind, t *vm.Type, local bool) {
	name = strings.TrimPrefix(name, "/")
	p.symbols[name] = &symbol{kind: k, index: i, local: local, value: v, Type: t}
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
		"any":    {kind: symType, index: unsetAddr, Type: vm.TypeOf((*any)(nil)).Elem()},
		"bool":   {kind: symType, index: unsetAddr, Type: vm.TypeOf((*bool)(nil)).Elem()},
		"error":  {kind: symType, index: unsetAddr, Type: vm.TypeOf((*error)(nil)).Elem()},
		"int":    {kind: symType, index: unsetAddr, Type: vm.TypeOf((*int)(nil)).Elem()},
		"string": {kind: symType, index: unsetAddr, Type: vm.TypeOf((*string)(nil)).Elem()},

		"nil":   {index: unsetAddr},
		"iota":  {kind: symConst, index: unsetAddr},
		"true":  {index: unsetAddr, value: vm.ValueOf(true), Type: vm.TypeOf(true)},
		"false": {index: unsetAddr, value: vm.ValueOf(false), Type: vm.TypeOf(false)},

		"println": {index: unsetAddr, value: vm.ValueOf(func(v ...any) { fmt.Println(v...) })},
	}
}
