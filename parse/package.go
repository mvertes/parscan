package parse

import (
	"fmt"

	"github.com/mvertes/parscan/vm"
)

// Packages contains binary package references.
var Packages = map[string]map[string]vm.Value{
	"fmt": fmtPkg,
}

var fmtPkg = map[string]vm.Value{
	"Println": vm.ValueOf(fmt.Println),
}
