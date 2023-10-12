package lang

type TokenId int

const (
	Illegal = iota
	Comment
	Ident
	Int
	Float
	Imag
	Char
	String

	// Operators
	Add
	Sub
	Mul
	Quo
	Rem
	And
	Or
	Xor
	Shl    // <<
	Shr    // >>
	AndNot //

	AddAssign
	SubAssign
	MulAssign
	QuoAssign
	RemAssign
	AndAssign
	OrAssign
	XorAssign
	ShlAssign
	ShrAssign
	AndNotAssign

	Land
	Lor
	Arrow
	Inc
	Dec
	Equal
	Less
	Greater
	Assign
	Not
	Plus    // unitary +
	Minus   // unitary -
	Address // unitary &
	Deref   // unitary *
	NotEqual
	LessEqual
	GreaterEqual
	Define
	Ellipsis
	Period
	Tilde

	// Separators
	Comma
	Semicolon
	Colon

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

	// Internal tokens (no corresponding keyword)
	Call
	CallX
	Label
	JumpFalse
	Enter // entering in function context
	Exit  // exiting from function context
)

func (t TokenId) IsKeyword() bool  { return t >= Break && t <= Var }
func (t TokenId) IsOperator() bool { return t >= Add && t <= Tilde }
func (t TokenId) IsBlock() bool    { return t >= ParenBlock && t <= BraceBlock }
