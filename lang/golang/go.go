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

const (
	Undefined = parser.Kind(iota)
	FuncDecl
	CallExpr
	IfStmt
	StmtBloc
	ReturnStmt
	Ident
	StringLit
	NumberLit
	ParBloc
	DotOp
	MulOp
	AddOp
	SubOp
	AssignOp
	DefOp
	InfOp
)

var GoParser = &parser.Parser{
	Scanner: GoScanner,
	Spec: map[string]parser.NodeSpec{
		".":      {DotOp, parser.Call, 3},
		"*":      {MulOp, 0, 4},
		"+":      {AddOp, 0, 5},
		"-":      {SubOp, 0, 5},
		"<":      {InfOp, 0, 6},
		":=":     {DefOp, 0, 7},
		"=":      {AssignOp, 0, 7},
		"#call":  {CallExpr, 0, 0},
		"#id":    {Ident, 0, 0},
		"#num":   {NumberLit, 0, 0},
		"if":     {IfStmt, parser.Stmt | parser.ExprSep, 0},
		"func":   {FuncDecl, parser.Decl | parser.Call, 0},
		"return": {ReturnStmt, parser.Stmt, 0},
		"{..}":   {StmtBloc, parser.ExprSep, 0},
		"{":      {StmtBloc, parser.ExprSep, 0}, // FIXME: redundant with above
		"(..)":   {ParBloc, parser.Call, 0},
		"(":      {ParBloc, parser.Call, 0}, // FIXME: redundant with above
		`".."`:   {StringLit, 0, 0},
	},
}
