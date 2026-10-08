package syntax

import (
	"strings"
	"testing"
)

func TestParseHeader(t *testing.T) {
	src := `%dw 2.0
import * from dw::core::Strings
import a, b from lib::Utils
import dw::core::Arrays as A
input payload application/csv separator=";", header=false
output application/json skipNullOn="everywhere" indent=false
type Customer = {
  id: Number
}
var x: Number = 1
fun f<T>(a: Array<T>, b = 2): Number = a + b
---
x`
	s, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if s.Version != "2.0" {
		t.Errorf("version = %q", s.Version)
	}
	if s.Output == nil || s.Output.Mime != "application/json" || s.Output.Props["skipNullOn"] != "everywhere" || s.Output.Props["indent"] != "false" {
		t.Errorf("output = %+v", s.Output)
	}
	if len(s.Inputs) != 1 || s.Inputs[0].Name != "payload" || s.Inputs[0].Mime != "application/csv" ||
		s.Inputs[0].Props["separator"] != ";" || s.Inputs[0].Props["header"] != "false" {
		t.Errorf("inputs = %+v", s.Inputs)
	}
	if len(s.Imports) != 3 || s.Imports[0].Names[0] != "*" || s.Imports[1].Module != "lib::Utils" ||
		strings.Join(s.Imports[1].Names, ",") != "a,b" || s.Imports[2].Alias != "A" {
		t.Errorf("imports = %+v", s.Imports)
	}
	if len(s.Decls) != 2 || s.Decls[1].Name != "f" || len(s.Decls[1].Params) != 2 || s.Decls[1].Params[1].Default == nil {
		t.Errorf("decls = %+v", s.Decls)
	}
	if id, ok := s.Body.(*Ident); !ok || id.Name != "x" {
		t.Errorf("body = %#v", s.Body)
	}
}

func TestSingleLineHeader(t *testing.T) {
	s, err := Parse("output text/csv --- payload")
	if err != nil {
		t.Fatal(err)
	}
	if s.Output.Mime != "text/csv" {
		t.Errorf("mime = %q", s.Output.Mime)
	}
}

func TestModuleWithoutBody(t *testing.T) {
	s, err := Parse("%dw 2.0\nfun twice(x) = x * 2\nvar k = 1\n")
	if err != nil {
		t.Fatal(err)
	}
	if s.Body != nil || len(s.Decls) != 2 {
		t.Fatalf("module = %+v", s)
	}
}

func TestPrecedence(t *testing.T) {
	tests := []struct{ src, want string }{
		{`a + b * c`, `(a + (b * c))`},
		{`a default b + c`, `(a default (b + c))`},
		{`x map f default y`, `(map(x, f) default y)`},
		{`a or b and c`, `(a or (b and c))`},
		{`not a == b`, `!((a == b))`},
		{`x as String ++ "z"`, `++(as(x, String), "z")`},
		{`a contains b == c`, `contains(a, (b == c))`},
		{`a joinBy "," splitBy ","`, `splitBy(joinBy(a, ","), ",")`},
		{`s replace "a" with "b"`, `replace(s, "a", "b")`},
		{`-x.y`, `-(x.y)`},
		{`!a.b?`, `!(exists(a.b))`},
		{`0 to 10`, `range(0, 10)`},
	}
	for _, tc := range tests {
		n, err := ParseExpr(tc.src)
		if err != nil {
			t.Fatalf("%s: %v", tc.src, err)
		}
		if got := dump(n); got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.src, got, tc.want)
		}
	}
}

func TestImplicitLambda(t *testing.T) {
	n, err := ParseExpr(`xs filter ($.a > 1) map $$`)
	if err != nil {
		t.Fatal(err)
	}
	outer, ok := n.(*Call)
	if !ok || len(outer.Args) != 2 {
		t.Fatalf("got %#v", n)
	}
	lam, ok := outer.Args[1].(*Lambda)
	if !ok || !lam.Implicit {
		t.Fatalf("map argument should be an implicit lambda, got %#v", outer.Args[1])
	}
	inner := outer.Args[0].(*Call)
	if _, ok := inner.Args[1].(*Lambda); !ok {
		t.Fatalf("filter argument should be an implicit lambda, got %#v", inner.Args[1])
	}
}

func TestStringInterpolation(t *testing.T) {
	n, err := ParseExpr(`"a $(b ++ "c(") d $name \$x"`)
	if err != nil {
		t.Fatal(err)
	}
	s := n.(*StrLit)
	if len(s.Parts) != 5 || s.Parts[0].Lit != "a " || s.Parts[1].Expr == nil || s.Parts[2].Lit != " d " ||
		s.Parts[3].Expr.(*Ident).Name != "name" || s.Parts[4].Lit != " $x" {
		t.Errorf("parts = %+v", s.Parts)
	}
}

func TestObjectAndArrayForms(t *testing.T) {
	n, err := ParseExpr(`{a: 1, "b c": 2, (k): 3, (spread), ("d": 4) if x, e: 5 f: 6}`)
	if err != nil {
		t.Fatal(err)
	}
	fields := n.(*ObjectLit).Fields
	if len(fields) != 7 {
		t.Fatalf("got %d fields", len(fields))
	}
	if fields[1].KeyName != "b c" || fields[2].KeyExpr == nil || !fields[3].Spread || fields[4].Cond == nil || fields[6].KeyName != "f" {
		t.Errorf("fields = %+v", fields)
	}
	arr, err := ParseExpr(`[1, (2) if c, 3,]`)
	if err != nil {
		t.Fatal(err)
	}
	if elems := arr.(*ArrayLit).Elems; len(elems) != 3 || elems[1].Cond == nil {
		t.Errorf("elems = %+v", elems)
	}
}

func TestMatchCases(t *testing.T) {
	n, err := ParseExpr(`x match { case "a" -> 1 case is Number -> 2 case s matches /z+/ -> 3 case v if v > 1 -> 4 else -> 5 }`)
	if err != nil {
		t.Fatal(err)
	}
	cases := n.(*Match).Cases
	kinds := []int{CaseLiteral, CaseIs, CaseRegex, CaseBind, CaseElse}
	if len(cases) != len(kinds) {
		t.Fatalf("got %d cases", len(cases))
	}
	for i, k := range kinds {
		if cases[i].Kind != k {
			t.Errorf("case %d kind = %d, want %d", i, cases[i].Kind, k)
		}
	}
}

func TestRegexVersusDivision(t *testing.T) {
	n, err := ParseExpr(`a / b / c`)
	if err != nil {
		t.Fatal(err)
	}
	if dump(n) != "((a / b) / c)" {
		t.Errorf("division parsed as %s", dump(n))
	}
	n, err = ParseExpr(`s contains /a\/b[/]c/`)
	if err != nil {
		t.Fatal(err)
	}
	if re := n.(*Call).Args[1].(*RegexLit); re.Src != `a\/b[/]c` {
		t.Errorf("regex = %q", re.Src)
	}
}

func TestSyntaxErrors(t *testing.T) {
	tests := []struct{ src, want string }{
		{"payload +", "1:10: unexpected end of input"},
		{"{a: 1", `expected "}"`},
		{"[1, 2", `expected "]"`},
		{"\"abc", "unterminated string"},
		{"|2020-01-01", "unterminated date literal"},
		{"%dw 2.0\nvar = 1\n---\n1", "expected an identifier"},
		{"var x = 1\n2", "expected a header declaration"},
		{"x match { 1 -> 2 }", "expected case or else"},
		{"\"a $(b\"", "unterminated"},
	}
	for _, tc := range tests {
		_, err := Parse(tc.src)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Parse(%q) error = %v, want %q", tc.src, err, tc.want)
		}
	}
}

func TestInterpolationErrorPosition(t *testing.T) {
	_, err := Parse("---\n\"ok $(a +)\"")
	if err == nil || !strings.HasPrefix(err.Error(), "2:") {
		t.Errorf("error = %v, want a position on line 2", err)
	}
}

// dump renders an expression tree compactly for precedence tests.
func dump(n Node) string {
	switch n := n.(type) {
	case *Ident:
		return n.Name
	case *NumLit:
		return n.Raw
	case *StrLit:
		var b strings.Builder
		for _, p := range n.Parts {
			b.WriteString(p.Lit)
		}
		return `"` + b.String() + `"`
	case *Binary:
		return "(" + dump(n.L) + " " + n.Op + " " + dump(n.R) + ")"
	case *Unary:
		return n.Op + "(" + dump(n.X) + ")"
	case *Call:
		args := make([]string, len(n.Args))
		for i, a := range n.Args {
			args[i] = dump(a)
		}
		return dump(n.Fn) + "(" + strings.Join(args, ", ") + ")"
	case *Select:
		return dump(n.X) + "." + n.Name
	case *Exists:
		return "exists(" + dump(n.X) + ")"
	case *As:
		return "as(" + dump(n.X) + ", " + n.Type + ")"
	case *RangeExpr:
		return "range(" + dump(n.From) + ", " + dump(n.To) + ")"
	case *Paren:
		return dump(n.X)
	}
	return "?"
}
