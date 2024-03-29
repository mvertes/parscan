package golang

import "github.com/mvertes/parscan/lang"

var GoSpec = &lang.Spec{
	CharProp: [lang.ASCIILen]uint{
		'\t': lang.CharSep,
		'\n': lang.CharLineSep,
		' ':  lang.CharSep,
		'!':  lang.CharOp,
		'"':  lang.CharStr,
		'%':  lang.CharOp,
		'&':  lang.CharOp,
		'\'': lang.CharStr,
		'(':  lang.CharBlock,
		'*':  lang.CharOp,
		'+':  lang.CharOp,
		',':  lang.CharGroupSep,
		'-':  lang.CharOp,
		'`':  lang.CharStr,
		'.':  lang.CharOp,
		'/':  lang.CharOp,
		':':  lang.CharOp,
		';':  lang.CharGroupSep,
		'<':  lang.CharOp,
		'=':  lang.CharOp,
		'>':  lang.CharOp,
		'[':  lang.CharBlock,
		'^':  lang.CharOp,
		'{':  lang.CharBlock,
		'|':  lang.CharOp,
		'~':  lang.CharOp,
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
		"(":  lang.CharBlock,
		"{":  lang.CharBlock,
		"[":  lang.CharBlock,
		`"`:  lang.CharStr | lang.StrEsc | lang.StrNonl,
		"`":  lang.CharStr,
		"'":  lang.CharStr | lang.StrEsc,
		"/*": lang.CharStr,
		"//": lang.CharStr | lang.ExcludeEnd | lang.EosValidEnd,
	},
	TokenProps: map[string]lang.TokenProp{
		// Block tokens (can be nested)
		"{..}": {TokenId: lang.BraceBlock},
		"[..]": {TokenId: lang.BracketBlock},
		"(..)": {TokenId: lang.ParenBlock},

		// String tokens (not nested)
		"//..": {TokenId: lang.Comment},
		"/*..": {TokenId: lang.Comment},
		`".."`: {TokenId: lang.String},
		"`..`": {TokenId: lang.String},

		// Separators
		",": {TokenId: lang.Comma},
		";": {TokenId: lang.Semicolon},
		".": {TokenId: lang.Period},
		":": {TokenId: lang.Colon},

		// Operators
		"&":  {TokenId: lang.And, Precedence: 1},
		"*":  {TokenId: lang.Mul, Precedence: 1},
		"/":  {TokenId: lang.Quo, Precedence: 1},
		"%":  {TokenId: lang.Rem, Precedence: 1},
		"<<": {TokenId: lang.Shl, Precedence: 1},
		">>": {TokenId: lang.Shr, Precedence: 1},
		"+":  {TokenId: lang.Add, Precedence: 2},
		"-":  {TokenId: lang.Sub, Precedence: 2},
		"=":  {TokenId: lang.Assign, Precedence: 6},
		"+=": {TokenId: lang.AddAssign, Precedence: 6},
		"<":  {TokenId: lang.Less, Precedence: 3},
		">":  {TokenId: lang.Greater, Precedence: 3},
		"^":  {TokenId: lang.Xor, Precedence: 2},
		"~":  {TokenId: lang.Tilde},
		"&&": {TokenId: lang.Land, Precedence: 4},
		"||": {TokenId: lang.Lor, Precedence: 5},
		":=": {TokenId: lang.Define, Precedence: 6},
		"==": {TokenId: lang.Equal, Precedence: 3},
		"<=": {TokenId: lang.LessEqual, Precedence: 3},
		">=": {TokenId: lang.GreaterEqual, Precedence: 3},
		"->": {TokenId: lang.Arrow},
		"!":  {TokenId: lang.Not},
		"++": {TokenId: lang.Inc, SkipSemi: true},
		"--": {TokenId: lang.Dec, SkipSemi: true},

		// Reserved keywords
		"break":       {TokenId: lang.Break},
		"case":        {TokenId: lang.Case, SkipSemi: true},
		"chan":        {TokenId: lang.Chan, SkipSemi: true},
		"const":       {TokenId: lang.Const, SkipSemi: true},
		"continue":    {TokenId: lang.Continue},
		"default":     {TokenId: lang.Case, SkipSemi: true},
		"defer":       {TokenId: lang.Defer, SkipSemi: true},
		"else":        {TokenId: lang.Else, SkipSemi: true},
		"fallthrough": {TokenId: lang.Fallthrough},
		"for":         {TokenId: lang.For, SkipSemi: true},
		"func":        {TokenId: lang.Func, SkipSemi: true},
		"go":          {TokenId: lang.Go, SkipSemi: true},
		"goto":        {TokenId: lang.Goto, SkipSemi: true},
		"if":          {TokenId: lang.If, SkipSemi: true},
		"import":      {TokenId: lang.Import, SkipSemi: true},
		"interface":   {TokenId: lang.Interface, SkipSemi: true},
		"map":         {TokenId: lang.Map, SkipSemi: true},
		"package":     {TokenId: lang.Package, SkipSemi: true},
		"range":       {TokenId: lang.Range, SkipSemi: true},
		"return":      {TokenId: lang.Return},
		"select":      {TokenId: lang.Select, SkipSemi: true},
		"struct":      {TokenId: lang.Struct, SkipSemi: true},
		"switch":      {TokenId: lang.Switch, SkipSemi: true},
		"type":        {TokenId: lang.Type, SkipSemi: true},
		"var":         {TokenId: lang.Var, SkipSemi: true},
	},
}
