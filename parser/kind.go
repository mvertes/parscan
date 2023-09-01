package parser

import "fmt"

// kind defines the AST node kind. Its name is the concatenation
// of a category (Block, Decl, Expr, Op, Stmt) and an instance name.
type Kind int

const (
	Undefined = Kind(iota)
	BlockParen
	BlockStmt
	Comment
	DeclFunc
	ExprCall
	Ident
	LiteralNumber
	LiteralString
	OpAdd
	OpAssign
	OpDefine
	OpDot
	OpInferior
	OpMultiply
	OpSubtract
	StmtIf
	StmtReturn
)

var kindString = [...]string{
	Undefined:     "Undefined",
	BlockParen:    "BlockParen",
	BlockStmt:     "BlockStmt",
	Comment:       "Comment",
	DeclFunc:      "DeclFunc",
	ExprCall:      "ExprCall",
	Ident:         "Ident",
	LiteralString: "LiteralString",
	LiteralNumber: "LiteralNumber",
	OpAdd:         "OpAdd",
	OpAssign:      "OpAssign",
	OpDefine:      "OpDefine",
	OpDot:         "OpDot",
	OpInferior:    "OpInferior",
	OpMultiply:    "OpMultiply",
	OpSubtract:    "OpSubtract",
	StmtIf:        "StmtIf",
	StmtReturn:    "StmtReturn",
}

func (k Kind) String() string {
	if int(k) < 0 || int(k) > len(kindString) {
		return fmt.Sprintf("unknown kind %d", k)
	}
	return kindString[k]
}
