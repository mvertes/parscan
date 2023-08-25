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
}

var GoParser = &parser.Parser{
	Scanner: GoScanner,
	Spec: map[string]parser.NodeSpec{
		".":      {Kind: parser.DotOp, Flags: parser.Call, Order: 3},
		"*":      {Kind: parser.MulOp, Order: 4},
		"+":      {Kind: parser.AddOp, Order: 5},
		"-":      {Kind: parser.SubOp, Order: 5},
		"<":      {Kind: parser.InfOp, Order: 6},
		":=":     {Kind: parser.DefOp, Order: 7},
		"=":      {Kind: parser.AssignOp, Order: 7},
		"if":     {Kind: parser.IfStmt, Flags: parser.Stmt | parser.ExprSep},
		"func":   {Kind: parser.FuncDecl, Flags: parser.Decl | parser.Call},
		"return": {Kind: parser.ReturnStmt, Flags: parser.Stmt},
		"{..}":   {Kind: parser.StmtBloc, Flags: parser.ExprSep},
		"(..)":   {Kind: parser.ParBloc, Flags: parser.Call},
		`".."`:   {Kind: parser.StringLit},
	},
}

func init() { GoParser.Init() }
