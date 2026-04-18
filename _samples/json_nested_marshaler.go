package main

import (
	"encoding/json"
	"fmt"
)

type MyStr struct{ V string }

func (s MyStr) MarshalJSON() ([]byte, error) { return json.Marshal("!" + s.V + "!") }

type Wrap struct {
	Inner MyStr
}

func main() {
	top, _ := json.Marshal(MyStr{V: "x"})
	fmt.Println("top-level:", string(top))
	nested, _ := json.Marshal(Wrap{Inner: MyStr{V: "x"}})
	fmt.Println("nested:   ", string(nested))
}

// skip: parscan MarshalJSON on a struct field is invisible to native json.Marshal via reflect. Top-level json.Marshal(x) bridges via Iface → BridgeMarshalJSON, but when x is nested, json iterates fields by reflect and the field's StructOf-built type has no methods attached. Fundamental reflect.StructOf limitation; would require substituting bridge types for struct fields at serialization time. Expected output with fix: top-level: "!x!" / nested: {"Inner":"!x!"}.
