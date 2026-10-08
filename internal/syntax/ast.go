package syntax

// Node is an expression node.
type Node interface {
	Position() Pos
}

type node struct{ P Pos }

// Position returns the source position of the node.
func (n node) Position() Pos { return n.P }

// Literal and reference nodes.
type (
	// NullLit is the null literal.
	NullLit struct{ node }
	// BoolLit is true or false.
	BoolLit struct {
		node
		V bool
	}
	// NumLit is a number literal with its source text.
	NumLit struct {
		node
		Raw string
	}
	// StrLit is a string, possibly with interpolated expressions.
	StrLit struct {
		node
		Parts []StrPart
	}
	// DateLit is a |...| temporal literal.
	DateLit struct {
		node
		Raw string
	}
	// RegexLit is a /.../ regular expression literal.
	RegexLit struct {
		node
		Src string
	}
	// Ident references a binding, builtin or module member (Ns::Name).
	Ident struct {
		node
		Ns   string
		Name string
	}
	// Paren is a parenthesized expression. It is kept so that (key): value can
	// be told apart from key: value.
	Paren struct {
		node
		X Node
	}
)

// StrPart is either literal text or an interpolated expression.
type StrPart struct {
	Lit  string
	Expr Node
}

// Constructors.
type (
	// ArrayLit is [a, b, (c) if cond].
	ArrayLit struct {
		node
		Elems []ArrayElem
	}
	// ObjectLit is {key: value, (spread), (k: v) if cond}.
	ObjectLit struct {
		node
		Fields []ObjField
	}
	// KVPair is a parenthesized single key-value pair used as an expression,
	// such as ("key": value) if (cond).
	KVPair struct {
		node
		Field ObjField
	}
)

// ArrayElem is an array literal element with an optional condition.
type ArrayElem struct {
	X    Node
	Cond Node
}

// ObjField is an object literal member.
type ObjField struct {
	// Spread is true for (expr) members that merge an object or an array of
	// objects into the result.
	Spread bool
	// KeyName is a static key; KeyExpr is set for dynamic keys.
	KeyName string
	KeyExpr Node
	Value   Node
	Cond    Node
	P       Pos
}

// Selector kinds.
const (
	SelKey = iota
	SelMulti
	SelDescendant
	SelIndex
	SelFilter
	SelAttr
)

// Operators and control flow.
type (
	// Select applies a selector to X.
	Select struct {
		node
		X     Node
		Kind  int
		Name  string
		Index Node
	}
	// Exists is the postfix ? operator.
	Exists struct {
		node
		X Node
	}
	// Call is a function call, including infix calls such as a map f.
	Call struct {
		node
		Fn    Node
		Args  []Node
		Infix bool
	}
	// Unary is -x, !x or not x.
	Unary struct {
		node
		Op string
		X  Node
	}
	// Binary is an arithmetic, comparison, logical or default operation.
	Binary struct {
		node
		Op   string
		L, R Node
	}
	// As coerces X to a type with optional format options.
	As struct {
		node
		X    Node
		Type string
		Opts Node
	}
	// Is checks the type of X.
	Is struct {
		node
		X    Node
		Type string
	}
	// RangeExpr is from to to.
	RangeExpr struct {
		node
		From, To Node
	}
	// If is if (cond) then else otherwise. Else may be nil.
	If struct {
		node
		Cond, Then, Else Node
	}
	// Lambda is (params) -> body. Implicit lambdas use $, $$ and $$$.
	Lambda struct {
		node
		Params   []Param
		Body     Node
		Implicit bool
	}
	// Do is do { declarations --- body }.
	Do struct {
		node
		Decls []Decl
		Body  Node
	}
	// Match is pattern matching: x match { case ... }.
	Match struct {
		node
		X     Node
		Cases []Case
	}
)

// Param is a function or lambda parameter.
type Param struct {
	Name    string
	Default Node
}

// Case kinds.
const (
	CaseLiteral = iota
	CaseIs
	CaseBind
	CaseRegex
	CaseElse
)

// Case is a single match case.
type Case struct {
	Kind  int
	Lit   Node
	Type  string
	Name  string
	Regex Node
	Guard Node
	Body  Node
	P     Pos
}

// Decl is a var or fun declaration.
type Decl struct {
	Name   string
	Fun    bool
	Params []Param
	Expr   Node
	P      Pos
}

// Directive is an output or input media type declaration.
type Directive struct {
	Name  string
	Mime  string
	Props map[string]string
}

// Import is an import declaration.
type Import struct {
	// Module is the module path with :: separators.
	Module string
	// Names lists the imported members; "*" imports everything. When empty
	// the module itself is imported as a namespace.
	Names []string
	// Alias renames a namespace import (import dw::core::Strings as S).
	Alias string
	P     Pos
}

// Script is a parsed script or module.
type Script struct {
	Version string
	Output  *Directive
	Inputs  []Directive
	Imports []Import
	Decls   []Decl
	// Body is nil for modules without a --- separator.
	Body Node
}
