package symbol

import (
	"reflect"

	"github.com/mvertes/parscan/vm"
)

// Package is a package struct containing source or binary values.
type Package struct {
	Path   string
	Bin    bool
	Values map[string]vm.Value
}

// BinPkg returns a binary package from a map of reflect values.
func BinPkg(m map[string]reflect.Value, name string) *Package {
	p := &Package{Path: name, Bin: true, Values: map[string]vm.Value{}}
	for k, v := range m {
		p.Values[k] = vm.FromReflect(v)
	}
	return p
}
