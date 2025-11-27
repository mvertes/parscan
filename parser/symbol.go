package parser

import (
	"fmt"
	"go/constant"
	"strings"

	"github.com/mvertes/parscan/vm"
)

type SymKind int

const (
	SymValue SymKind = iota // a value defined in the runtime
	SymType                 // a type
	SymLabel                // a label indication a position in the VM code
	SymConst                // a constant
	SymVar                  // a variable, located in the VM memory
	SymFunc                 // a function, located in the VM code
	SymPkg                  // a package
)

//go:generate stringer -type=SymKind

const UnsetAddr = -65535

type Symbol struct {
	Kind    SymKind
	Index   int            // address of symbol in frame
	PkgPath string         //
	Type    *vm.Type       //
	Value   vm.Value       //
	Cval    constant.Value //
	Local   bool           // if true address is relative to local frame, otherwise global
	Used    bool           //
}

func SymbolType(s *Symbol) *vm.Type {
	if s.Type != nil {
		return s.Type
	}
	return vm.TypeOf(s.Value)
}

// AddSym add a new named value at memory position i in the parser symbol table.
// func (p *Parser) AddSym(i int, name string, v vm.Value) {
// 	p.addSym(i, name, v, SymValue, nil, false)
// }

func (p *Parser) AddSymbol(i int, name string, v vm.Value, k SymKind, t *vm.Type, local bool) {
	name = strings.TrimPrefix(name, "/")
	p.Symbols[name] = &Symbol{Kind: k, Index: i, Local: local, Value: v, Type: t}
}

// GetSym searches for an existing symbol starting from the deepest scope.
func (p *Parser) GetSym(name, scope string) (sym *Symbol, sc string, ok bool) {
	for {
		if sym, ok = p.Symbols[scope+"/"+name]; ok {
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
	sym, ok = p.Symbols[name]
	return sym, scope, ok
}

func initUniverse() map[string]*Symbol {
	return map[string]*Symbol{
		"any":    {Kind: SymType, Index: UnsetAddr, Type: vm.TypeOf((*any)(nil)).Elem()},
		"bool":   {Kind: SymType, Index: UnsetAddr, Type: vm.TypeOf((*bool)(nil)).Elem()},
		"error":  {Kind: SymType, Index: UnsetAddr, Type: vm.TypeOf((*error)(nil)).Elem()},
		"int":    {Kind: SymType, Index: UnsetAddr, Type: vm.TypeOf((*int)(nil)).Elem()},
		"string": {Kind: SymType, Index: UnsetAddr, Type: vm.TypeOf((*string)(nil)).Elem()},

		"nil":   {Index: UnsetAddr},
		"iota":  {Kind: SymConst, Index: UnsetAddr},
		"true":  {Index: UnsetAddr, Value: vm.ValueOf(true), Type: vm.TypeOf(true)},
		"false": {Index: UnsetAddr, Value: vm.ValueOf(false), Type: vm.TypeOf(false)},

		"println": {Index: UnsetAddr, Value: vm.ValueOf(func(v ...any) { fmt.Println(v...) })},
	}
}
