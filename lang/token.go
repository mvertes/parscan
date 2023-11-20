package lang

type TokenId int

const (
	Illegal = iota
	Comment
	Ident

	// Literal values
	Char
	Float
	Imag
	Int
	String

	// Binary operators (except indicated)
	// Arithmetic and bitwise binary operators
	Add    // +
	Sub    // -
	Mul    // *
	Quo    // /
	Rem    // %
	And    // &
	Or     // |
	Xor    // ^
	Shl    // <<
	Shr    // >>
	AndNot // &^
	Period // .

	// Binary operators returning a boolean
	Equal        // ==
	Greater      // >
	GreaterEqual // >=
	Land         // &&
	Less         // <
	LessEqual    // <=
	Lor          // ||
	NotEqual     // !=

	// Assigment operators (arithmetic and bitwise)
	Define       // :=
	Assign       // =
	AddAssign    // +=
	SubAssign    // -=
	MulAssign    // *=
	QuoAssign    // /=
	RemAssign    // %=
	AndAssign    // &=
	OrAssign     // |=
	XorAssign    // ^=
	ShlAssign    // <<=
	ShrAssign    // >>=
	AndNotAssign // &^=
	Inc          // ++
	Dec          // --

	// Unary operations
	Plus     // unary +
	Minus    // unary -
	Address  // unary &
	Deref    // unary *
	BitComp  // unary ^
	Arrow    // unary ->
	Ellipsis // unary ...
	Not      // unary !
	Tilde    // unary ~

	// Separators (punctuation)
	Comma     // ,
	Semicolon // ;
	Colon     // :

	// Block tokens
	ParenBlock   // (..)
	BracketBlock // [..]
	BraceBlock   // {..}

	// Reserved keywords
	Break
	Case
	Chan
	Const
	Continue
	Default
	Defer
	Else
	Fallthrough
	For
	Func
	Go
	Goto
	If
	Import
	Interface
	Map
	Package
	Range
	Return
	Select
	Struct
	Switch
	Type
	Var

	// Internal virtual machine tokens (no corresponding keyword)
	Call
	CallX
	Grow
	Index
	Label
	JumpFalse
	JumpSetFalse
	JumpSetTrue
	EqualSet
)

func (t TokenId) IsKeyword() bool   { return t >= Break && t <= Var }
func (t TokenId) IsLiteral() bool   { return t >= Char && t <= String }
func (t TokenId) IsOperator() bool  { return t >= Add && t <= Tilde }
func (t TokenId) IsBlock() bool     { return t >= ParenBlock && t <= BraceBlock }
func (t TokenId) IsBoolOp() bool    { return t >= Equal && t <= NotEqual || t == Not }
func (t TokenId) IsBinaryOp() bool  { return t >= Add && t <= NotEqual }
func (t TokenId) IsUnaryOp() bool   { return t >= Plus && t <= Tilde }
func (t TokenId) IsLogicalOp() bool { return t == Land || t == Lor }
