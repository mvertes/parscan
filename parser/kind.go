package parser

import "fmt"

type Kind int

const (
	Undefined = Kind(iota)
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

var kindString = [...]string{
	Undefined:  "Undefined",
	FuncDecl:   "FuncDecl",
	CallExpr:   "CallExpr",
	IfStmt:     "IfStmt",
	StmtBloc:   "StmtBloc",
	ReturnStmt: "ReturnStmt",
	Ident:      "Ident",
	StringLit:  "StringLit",
	NumberLit:  "NumberLit",
	ParBloc:    "ParBloc",
	DotOp:      "DotOp",
	MulOp:      "MulOp",
	AddOp:      "AddOP",
	SubOp:      "SubOp",
	AssignOp:   "AssignOp",
	DefOp:      "DefOp",
	InfOp:      "InfOp",
}

func (k Kind) String() string {
	if int(k) < 0 || int(k) > len(kindString) {
		return fmt.Sprintf("unknown kind %d", k)
	}
	return kindString[k]
}
