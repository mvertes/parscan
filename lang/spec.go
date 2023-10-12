package lang

const (
	CharIllegal = 1 << iota
	CharOp
	CharNum
	CharAlpha
	CharSep
	CharLineSep
	CharGroupSep
	CharStr
	CharBlock
	StrEsc
	StrNonl
	ExcludeEnd  // exclude end delimiter from content
	EosValidEnd // end of input string terminates block or string token
)

const ASCIILen = 1 << 7 // 128

type TokenProp struct {
	TokenId
	SkipSemi bool // automatic semicolon insertion after newline
}

type Spec struct {
	CharProp   [ASCIILen]uint       // special Character properties
	End        map[string]string    // end delimiters, indexed by start
	BlockProp  map[string]uint      // block properties
	TokenProps map[string]TokenProp // token properties
	DotNum     bool                 // true if a number can start with '.'
	IdAscii    bool                 // true if an identifier can be in ASCII only
	Num_       bool                 // true if a number can contain _ character
}

// HasInit stores if a statement may contain a simple init statement
var HasInit = map[TokenId]bool{
	Case:   true,
	For:    true,
	If:     true,
	Select: true,
	Switch: true,
}
