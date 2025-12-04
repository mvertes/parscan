// Package symbol implements symbol utilities.
package symbol

import (
	"fmt"
	"go/constant"
	"strings"

	"github.com/mvertes/parscan/vm"
)

// Kind represents the symbol kind.
type Kind int

// Symbol kinds.
const (
	Value Kind = iota // a value defined in the runtime
	Type              // a type
	Label             // a label indicating a position in the VM code
	Const             // a constant
	Var               // a variable, located in the VM memory
	Func              // a function, located in the VM code
	Pkg               // a package
)

//go:generate stringer -type=Kind

// UnsetAddr denotes an unset symbol index (vs 0).
const UnsetAddr = -65535

// Symbol structure used in parser and compiler.
type Symbol struct {
	Kind    Kind
	Index   int            // address of symbol in frame
	PkgPath string         //
	Type    *vm.Type       //
	Value   vm.Value       //
	Cval    constant.Value //
	Local   bool           // if true address is relative to local frame, otherwise global
	Used    bool           //
}

// Vtype returns the VM type of a symbol.
func Vtype(s *Symbol) *vm.Type {
	if s.Type != nil {
		return s.Type
	}
	return vm.TypeOf(s.Value)
}

// SymMap is a map of Symbols.
type SymMap map[string]*Symbol

// Add adds a new named symbol value at memory position i.
func (sm SymMap) Add(i int, name string, v vm.Value, k Kind, t *vm.Type, local bool) {
	name = strings.TrimPrefix(name, "/")
	sm[name] = &Symbol{Kind: k, Index: i, Local: local, Value: v, Type: t}
}

// Get searches for an existing symbol starting from the deepest scope.
func (sm SymMap) Get(name, scope string) (sym *Symbol, sc string, ok bool) {
	for {
		if sym, ok = sm[scope+"/"+name]; ok {
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
	sym, ok = sm[name]
	return sym, scope, ok
}

// Init fills the symbol map with default Go symbols.
func (sm SymMap) Init() {
	sm["any"] = &Symbol{Kind: Type, Index: UnsetAddr, Type: vm.TypeOf((*any)(nil)).Elem()}
	sm["bool"] = &Symbol{Kind: Type, Index: UnsetAddr, Type: vm.TypeOf((*bool)(nil)).Elem()}
	sm["error"] = &Symbol{Kind: Type, Index: UnsetAddr, Type: vm.TypeOf((*error)(nil)).Elem()}
	sm["int"] = &Symbol{Kind: Type, Index: UnsetAddr, Type: vm.TypeOf((*int)(nil)).Elem()}
	sm["string"] = &Symbol{Kind: Type, Index: UnsetAddr, Type: vm.TypeOf((*string)(nil)).Elem()}

	sm["nil"] = &Symbol{Index: UnsetAddr}
	sm["iota"] = &Symbol{Kind: Const, Index: UnsetAddr}
	sm["true"] = &Symbol{Index: UnsetAddr, Value: vm.ValueOf(true), Type: vm.TypeOf(true)}
	sm["false"] = &Symbol{Index: UnsetAddr, Value: vm.ValueOf(false), Type: vm.TypeOf(false)}

	sm["println"] = &Symbol{Index: UnsetAddr, Value: vm.ValueOf(func(v ...any) { fmt.Println(v...) })}
}
