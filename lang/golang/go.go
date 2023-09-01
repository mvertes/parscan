package golang

import (
	"github.com/gnolang/parscan/parser"
	"github.com/gnolang/parscan/scanner"
)

var GoScanner = &scanner.Scanner{
	CharProp: [scanner.ASCIILen]uint{
		'\t': scanner.CharSep,
		'\n': scanner.CharLineSep,
		' ':  scanner.CharSep,
		'!':  scanner.CharOp,
		'"':  scanner.CharStr,
		'%':  scanner.CharOp,
		'&':  scanner.CharOp,
		'\'': scanner.CharStr,
		'(':  scanner.CharBlock,
		'*':  scanner.CharOp,
		'+':  scanner.CharOp,
		',':  scanner.CharGroupSep,
		'-':  scanner.CharOp,
		'.':  scanner.CharOp,
		'/':  scanner.CharOp,
		':':  scanner.CharOp,
		';':  scanner.CharGroupSep,
		'<':  scanner.CharOp,
		'=':  scanner.CharOp,
		'>':  scanner.CharOp,
		'[':  scanner.CharBlock,
		'^':  scanner.CharOp,
		'{':  scanner.CharBlock,
		'|':  scanner.CharOp,
		'~':  scanner.CharOp,
	},
	End: map[string]string{
		"(":  ")",
		"{":  "}",
		"[":  "]",
		"/*": "*/",
		`"`:  `"`,
		"'":  "'",
		"`":  "`",
		"//": "\n",
	},
	BlockProp: map[string]uint{
		"(":  scanner.CharBlock,
		"{":  scanner.CharBlock,
		"[":  scanner.CharBlock,
		`"`:  scanner.CharStr | scanner.StrEsc | scanner.StrNonl,
		"`":  scanner.CharStr,
		"'":  scanner.CharStr | scanner.StrEsc,
		"/*": scanner.CharStr,
		"//": scanner.CharStr | scanner.ExcludeEnd | scanner.EosValidEnd,
	},
}

var GoParser = &parser.Parser{
	Scanner: GoScanner,
	Spec: map[string]parser.NodeSpec{
		".":      {Kind: parser.OpDot, Flags: parser.Call, Order: 3},
		"*":      {Kind: parser.OpMultiply, Order: 4},
		"+":      {Kind: parser.OpAdd, Order: 5},
		"-":      {Kind: parser.OpSubtract, Order: 5},
		"<":      {Kind: parser.OpInferior, Order: 6},
		":=":     {Kind: parser.OpDefine, Order: 7},
		"=":      {Kind: parser.OpAssign, Order: 7},
		"if":     {Kind: parser.StmtIf, Flags: parser.Stmt | parser.ExprSep},
		"func":   {Kind: parser.DeclFunc, Flags: parser.Decl | parser.Call},
		"return": {Kind: parser.StmtReturn, Flags: parser.Stmt},
		"{..}":   {Kind: parser.BlockStmt, Flags: parser.ExprSep},
		"(..)":   {Kind: parser.BlockParen, Flags: parser.Call},
		`".."`:   {Kind: parser.LiteralString},
		"//..":   {Kind: parser.Comment},
		"/*..":   {Kind: parser.Comment},
	},
}

func init() { GoParser.Init() }
