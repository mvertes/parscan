package parser

import (
	"fmt"

	"github.com/mvertes/parscan/vm"
)

var packages = map[string]map[string]vm.Value{
	"fmt": fmtPkg,
}

var fmtPkg = map[string]vm.Value{
	"Println": vm.ValueOf(fmt.Println),
}
