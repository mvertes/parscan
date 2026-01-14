// Package symbol implements symbol utilities.
package symbol

import (
	"fmt"
	"go/constant"
	"reflect"
	"strings"

	"github.com/mvertes/parscan/vm"
)

// Kind represents the symbol kind.
type Kind int

// Symbol kinds.
const (
	Unset Kind = iota
	Value      // a value defined in the runtime
	Type       // a type
	Label      // a label indicating a position in the VM code
	Const      // a constant
	Var        // a variable, located in the VM memory
	Func       // a function, located in the VM code
	Pkg        // a package
)

//go:generate stringer -type=Kind

// UnsetAddr denotes an unset symbol index (vs 0).
const UnsetAddr = -65535

// Symbol structure used in parser and compiler.
type Symbol struct {
	Kind     Kind
	Name     string         //
	Index    int            // address of symbol in frame
	PkgPath  string         //
	Type     *vm.Type       //
	Value    vm.Value       //
	SliceLen int            // initial slice length (slice types only)
	Cval     constant.Value //
	Local    bool           // if true address is relative to local frame, otherwise global
	Used     bool           //
}

// func (s *Symbol) String() string {
//  	return fmt.Sprintf("{Kind: %v, Name: %v, Index: %v, Type: %v}\n", s.Kind, s.Name, s.Index, s.Type)
//}

// IsConst returns true if symbol is a constant.
func (s *Symbol) IsConst() bool { return s.Kind == Const }

// IsType returns true if symbol is a type.
func (s *Symbol) IsType() bool { return s.Kind == Type }

// IsFunc returns true if symbol is a function.
func (s *Symbol) IsFunc() bool { return s.Kind == Func }

// IsPtr returns true if symbol is a pointer.
func (s *Symbol) IsPtr() bool { return s.Type.Rtype.Kind() == reflect.Pointer }

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
	sm[name] = &Symbol{Kind: k, Name: name, Index: i, Local: local, Value: v, Type: t}
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
	sm["any"] = &Symbol{Name: "any", Kind: Type, Index: UnsetAddr, Type: vm.TypeOf((*any)(nil)).Elem()}
	sm["bool"] = &Symbol{Name: "bool", Kind: Type, Index: UnsetAddr, Type: vm.TypeOf((*bool)(nil)).Elem()}
	sm["error"] = &Symbol{Name: "error", Kind: Type, Index: UnsetAddr, Type: vm.TypeOf((*error)(nil)).Elem()}
	sm["int"] = &Symbol{Name: "int", Kind: Type, Index: UnsetAddr, Type: vm.TypeOf((*int)(nil)).Elem()}
	sm["string"] = &Symbol{Name: "string", Kind: Type, Index: UnsetAddr, Type: vm.TypeOf((*string)(nil)).Elem()}

	sm["nil"] = &Symbol{Name: "nil", Kind: Value, Index: UnsetAddr}
	sm["iota"] = &Symbol{Name: "iota", Kind: Const, Index: UnsetAddr}
	sm["true"] = &Symbol{Name: "true", Kind: Value, Index: UnsetAddr, Value: vm.ValueOf(true), Type: vm.TypeOf(true)}
	sm["false"] = &Symbol{Name: "false", Kind: Value, Index: UnsetAddr, Value: vm.ValueOf(false), Type: vm.TypeOf(false)}

	sm["println"] = &Symbol{Name: "println", Kind: Value, Index: UnsetAddr, Value: vm.ValueOf(func(v ...any) { fmt.Println(v...) })}
}
