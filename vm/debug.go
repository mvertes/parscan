package vm

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
)

// DebugInfo holds symbolic information for annotating debug output.
// Built by the compiler from the symbol table and source.
type DebugInfo struct {
	Source  string                // original source text (for pos -> line mapping)
	Labels  map[int]string        // code address -> label/function name
	Globals map[int]string        // data index -> symbol name
	Locals  map[string][]LocalVar // function name -> local variable list
}

// LocalVar describes a local variable within a function frame.
type LocalVar struct {
	Offset int    // offset from fp (1-based, as in Get Local N)
	Name   string // variable name (short, without scope prefix)
}

// NewDebugInfo returns an empty DebugInfo ready to be populated.
func NewDebugInfo() *DebugInfo {
	return &DebugInfo{
		Labels:  map[int]string{},
		Globals: map[int]string{},
		Locals:  map[string][]LocalVar{},
	}
}

// PosToLine converts a byte offset in source to a "line:col" string.
// Returns "" if source is empty or pos is out of range.
func (d *DebugInfo) PosToLine(pos Pos) string {
	if d == nil || d.Source == "" || int(pos) < 0 || int(pos) > len(d.Source) {
		return ""
	}
	p := int(pos)
	line := 1 + strings.Count(d.Source[:p], "\n")
	col := p - strings.LastIndex(d.Source[:p], "\n")
	return strconv.Itoa(line) + ":" + strconv.Itoa(col)
}

// LocalName returns the variable name for a local slot offset within func funcName.
func (d *DebugInfo) LocalName(funcName string, offset int) string {
	if d == nil {
		return ""
	}
	for _, lv := range d.Locals[funcName] {
		if lv.Offset == offset {
			return lv.Name
		}
	}
	return ""
}

// DumpFrame decodes and pretty-prints the call frame at the given fp.
// di is optional (may be nil); when set, slots are annotated with variable names.
func DumpFrame(w io.Writer, mem []Value, code Code, fp, sp, narg, nret int, di *DebugInfo) {
	if fp < frameOverhead || fp > len(mem) {
		_, _ = fmt.Fprintf(w, "--- invalid fp=%d (mem len=%d) ---\n", fp, len(mem))
		return
	}

	retIP := int(mem[fp-2].num)  //nolint:gosec
	prevFP := int(mem[fp-1].num) //nolint:gosec

	funcAddr := max(fp-frameOverhead-narg-1, 0)

	// Resolve function name from the code address stored in the func slot.
	funcName := ""
	if di != nil {
		codeAddr := 0
		fv := mem[funcAddr]
		if isNum(fv.ref.Kind()) {
			codeAddr = int(fv.num) //nolint:gosec
		} else if fv.ref.IsValid() {
			if iv, ok := fv.ref.Interface().(int); ok {
				codeAddr = iv
			}
		}
		funcName = di.Labels[codeAddr]
	}

	// Header line.
	header := fmt.Sprintf("--- Frame fp=%d retIP=%d prevFP=%d narg=%d nret=%d", fp, retIP, prevFP, narg, nret)
	if funcName != "" {
		header += " (" + funcName + ")"
	}
	// Annotate retIP with source position.
	if di != nil && retIP >= 0 && retIP < len(code) {
		if loc := di.PosToLine(code[retIP].Pos); loc != "" {
			header += " ret@" + loc
		}
	}
	_, _ = fmt.Fprintln(w, header+" ---")

	for i := funcAddr; i < sp; i++ {
		role := slotRole(i, fp, funcAddr, sp)
		marker := ""
		if i == fp {
			marker = " <- fp"
		}
		if i == sp-1 {
			marker += " <- sp"
		}

		// Annotate with symbol name.
		symName := ""
		if di != nil {
			switch {
			case i == funcAddr && funcName != "":
				symName = funcName
			case i > funcAddr && i < fp-frameOverhead:
				// Arg slot: look up in locals (args are the first locals).
				argIdx := i - funcAddr - 1
				symName = di.LocalName(funcName, argIdx+1)
			case i >= fp:
				localOff := i - fp + 1 + narg
				symName = di.LocalName(funcName, localOff)
			}
		}
		printSlot(w, i, role, mem[i], symName, marker)
	}
	_, _ = fmt.Fprintln(w)
}

// DumpCallStack walks the frame pointer chain and prints every frame.
func (m *Machine) DumpCallStack(w io.Writer, di *DebugInfo) {
	mem := m.mem
	fp := m.fp
	sp := len(mem)

	if fp == 0 {
		_, _ = fmt.Fprintln(w, "--- no call frames (fp=0) ---")
		return
	}

	_, _ = fmt.Fprintf(w, "=== Call Stack (%d frames) ===\n\n", len(m.frameInfo))

	for depth := len(m.frameInfo) - 1; depth >= 0; depth-- {
		info := m.frameInfo[depth]
		nret := info & 0xFFFF
		narg := info >> 16

		DumpFrame(w, mem, m.code, fp, sp, narg, nret, di)

		// Walk to the previous frame.
		sp = fp - frameOverhead - narg - 1
		if fp-1 < 0 || fp-1 >= len(mem) {
			break
		}
		fp = int(mem[fp-1].num) //nolint:gosec
	}

	// Print globals with names if available.
	if di != nil && len(di.Globals) > 0 {
		_, _ = fmt.Fprintln(w, "--- Globals ---")
		indices := make([]int, 0, len(di.Globals))
		for idx := range di.Globals {
			if idx >= 0 && idx < len(mem) {
				indices = append(indices, idx)
			}
		}
		sort.Ints(indices)
		for _, idx := range indices {
			printSlot(w, idx, "global", mem[idx], di.Globals[idx], "")
		}
		_, _ = fmt.Fprintln(w)
	}
}

// DumpFrameStderr is a convenience wrapper that prints to stderr.
func DumpFrameStderr(mem []Value, code Code, fp, sp, narg, nret int, di *DebugInfo) {
	DumpFrame(os.Stderr, mem, code, fp, sp, narg, nret, di)
}

// DumpCallStackStderr is a convenience wrapper that prints to stderr.
func (m *Machine) DumpCallStackStderr(di *DebugInfo) {
	m.DumpCallStack(os.Stderr, di)
}

func slotRole(i, fp, funcAddr, sp int) string {
	switch {
	case i == funcAddr:
		return "func"
	case i > funcAddr && i < fp-frameOverhead:
		return fmt.Sprintf("arg %d", i-funcAddr-1)
	case i == fp-frameOverhead:
		return "deferHead"
	case i == fp-frameOverhead+1:
		return "retIP"
	case i == fp-frameOverhead+2:
		return "prevFP"
	case i >= fp && i < sp:
		return fmt.Sprintf("local %d", i-fp+1)
	default:
		return "?"
	}
}

func printSlot(w io.Writer, addr int, role string, v Value, symName, marker string) {
	var typStr, valStr string
	switch {
	case v.ref.IsValid():
		typStr = v.ref.Type().String()
		valStr = formatValue(v)
	case v.num != 0:
		typStr = "raw"
		valStr = strconv.FormatUint(v.num, 10)
	default:
		typStr = "zero"
		valStr = "0"
	}
	sym := ""
	if symName != "" {
		sym = " // " + symName
	}
	_, _ = fmt.Fprintf(w, "  mem[%-3d] %-12s %-10s %s%s%s\n", addr, role, typStr, valStr, sym, marker)
}

func formatValue(v Value) string {
	if !v.ref.IsValid() {
		return "<nil>"
	}
	if isNum(v.ref.Kind()) {
		return fmt.Sprintf("%v", v.Interface())
	}
	s := fmt.Sprintf("%v", v.Interface())
	if len(s) > 60 {
		s = s[:57] + "..."
	}
	return s
}
