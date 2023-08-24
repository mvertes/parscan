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
		".":      {parser.DotOp, parser.Call, 3},
		"*":      {parser.MulOp, 0, 4},
		"+":      {parser.AddOp, 0, 5},
		"-":      {parser.SubOp, 0, 5},
		"<":      {parser.InfOp, 0, 6},
		":=":     {parser.DefOp, 0, 7},
		"=":      {parser.AssignOp, 0, 7},
		"if":     {parser.IfStmt, parser.Stmt | parser.ExprSep, 0},
		"func":   {parser.FuncDecl, parser.Decl | parser.Call, 0},
		"return": {parser.ReturnStmt, parser.Stmt, 0},
		"{..}":   {parser.StmtBloc, parser.ExprSep, 0},
		"(..)":   {parser.ParBloc, parser.Call, 0},
		`".."`:   {parser.StringLit, 0, 0},
	},
}

func init() { GoParser.Init() }
