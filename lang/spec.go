// Package lang provides tokens for possibly  multiple languages.
package lang

// Lexical properties of tokens to allow scanning.
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

// ASCIILen is the length of the ASCII characters set.
const ASCIILen = 1 << 7 // 128

// TokenProp represent token properties for parsing.
type TokenProp struct {
	Token
	SkipSemi   bool // automatic semicolon insertion after newline
	Precedence int  // operator precedence
}

// Spec represents the token specification for scanning.
type Spec struct {
	CharProp   [ASCIILen]uint       // special Character properties
	End        map[string]string    // end delimiters, indexed by start
	BlockProp  map[string]uint      // block properties
	TokenProps map[string]TokenProp // token properties
	DotNum     bool                 // true if a number can start with '.'
	IdentASCII bool                 // true if an identifier can be in ASCII only
	NumUnder   bool                 // true if a number can contain _ character
}

// HasInit stores if a statement may contain a simple init statement.
var HasInit = map[Token]bool{
	Case:   true,
	For:    true,
	If:     true,
	Select: true,
	Switch: true,
}
