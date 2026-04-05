package stdlib

import (
	"fmt"
	"reflect"
)

func init() {
	Values["fmt"] = map[string]reflect.Value{
		"Println": reflect.ValueOf(fmt.Println),
	}
}
