package interp

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/touno-io/go-tsunami-cli/internal/codec"
	"github.com/touno-io/go-tsunami-cli/internal/value"
)

const gotFmt = "got %s"

func compact(t *testing.T, v value.Value) string {
	t.Helper()
	var buf bytes.Buffer
	if err := codec.WriteJSON(&buf, v, map[string]string{"indent": "false"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	return buf.String()
}

func evalJSON(t *testing.T, src string, inputs map[string]value.Value) string {
	t.Helper()
	prog, err := Compile(src, Options{})
	if err != nil {
		t.Fatalf("compile %q: %v", src, err)
	}
	v, err := prog.Eval(inputs)
	if err != nil {
		t.Fatalf("eval %q: %v", src, err)
	}
	return compact(t, v)
}

func TestExpressions(t *testing.T) {
	tests := []struct{ src, want string }{
		// literals and operators
		{`1 + 2 * 3`, `7`},
		{`(1 + 2) * 3`, `9`},
		{`10 / 4`, `2.5`},
		{`-3 + 1`, `-2`},
		{`1.50`, `1.50`},
		{`"5" + 1`, `6`},
		{`"a" ++ 1 ++ true`, `"a1true"`},
		{`[1, 2] ++ [3]`, `[1,2,3]`},
		{`{a: 1} ++ {b: 2}`, `{"a":1,"b":2}`},
		{`[1, 2, 3] - 2`, `[1,3]`},
		{`[1, 2] + 3`, `[1,2,3]`},
		{`{a: 1, b: 2} - "a"`, `{"b":2}`},
		{`{a: 1, b: 2, c: 3} -- ["a", "c"]`, `{"b":2}`},
		{`[1, 2, 3, 2] -- [2]`, `[1,3]`},
		{`1 == 1.0`, `true`},
		{`"1" == 1`, `false`},
		{`"1" ~= 1`, `true`},
		{`{a: 1, b: 2} == {b: 2, a: 1}`, `true`},
		{`2 > 1 and not (1 > 2)`, `true`},
		{`null default 5`, `5`},
		{`0 default 5`, `0`},
		{`null > 1`, `false`},
		{`"20" <= 0`, `false`},
		{`if (1 > 2) "a" else if (2 > 1) "b" else "c"`, `"b"`},
		{`1 is Number`, `true`},
		{`"x" is String and [] is Array and {} is Object and null is Null`, `true`},
		// strings
		{`"Hello $(upper("world"))!"`, `"Hello WORLD!"`},
		{`do { var name = "dw" --- "Hi $name" }`, `"Hi dw"`},
		{`'"$(["a", "b"] joinBy '","')"'`, `"\"a\",\"b\""`},
		{`"a,b,,c" splitBy ","`, `["a","b","","c"]`},
		{`"a,b,," splitBy ","`, `["a","b"]`},
		{`"a1b22c" splitBy /\d+/`, `["a","b","c"]`},
		{`"it's" replace "'" with "''"`, `"it''s"`},
		{`"2023-05-01" replace /(\d+)-(\d+)-(\d+)/ with "$3/$2/$1"`, `"01/05/2023"`},
		{`"abc" contains "b"`, `true`},
		{`"[1]" contains /^\[/`, `true`},
		{`"hello world" match /(\w+) (\w+)/`, `["hello world","hello","world"]`},
		{`"abc123" matches /[a-z]+\d+/`, `true`},
		{`"a1b2" scan /[a-z](\d)/`, `[["a1","1"],["b2","2"]]`},
		{`"hello"[1 to 3]`, `"ell"`},
		{`"hello"[0 to 200]`, `"hello"`},
		{`"hello"[-1]`, `"o"`},
		{`trim("  x  ") ++ lower("AB") ++ upper("cd")`, `"xabCD"`},
		{`capitalize("customer_first_name")`, `"Customer First Name"`},
		{`camelize("customer_first_name")`, `"customerFirstName"`},
		{`dasherize("customerFirstName")`, `"customer-first-name"`},
		{`underscore("customerFirstName")`, `"customer_first_name"`},
		{`leftPad("7", 3, "0") ++ rightPad("a", 3, ".")`, `"007a.."`},
		{`substringBefore("a-b-c", "-") ++ substringAfterLast("a-b-c", "-")`, `"ac"`},
		{`sizeOf("สวัสดี")`, `6`},
		// arrays and objects
		{`[1, 2, 3] map ($ * 2)`, `[2,4,6]`},
		{`[1, 2, 3] map (n, i) -> n + i`, `[1,3,5]`},
		{`[1, 2, 3, 4] filter ($ mod 2) == 0`, `[2,4]`},
		{`[1, 2, 3, 4] filter (mod($, 2) == 0)`, `[2,4]`},
		{`flatten([[1, 2], [3], null])`, `[1,2,3]`},
		{`[1, 2, 3] reduce ($$ + $)`, `6`},
		{`[1, 2, 3] reduce (item, acc = 10) -> acc + item`, `16`},
		{`[3, 1, 2] orderBy $`, `[1,2,3]`},
		{`[{n: "b"}, {n: "a"}] orderBy $.n`, `[{"n":"a"},{"n":"b"}]`},
		{`["a", "bb", "c"] groupBy sizeOf($)`, `{"1":["a","c"],"2":["bb"]}`},
		{`[1, 2, 1, 3] distinctBy $`, `[1,2,3]`},
		{`{a: 1, b: 2} pluck (v, k) -> k ++ v`, `["a1","b2"]`},
		{`{a: 1, b: 2} mapObject (v, k) -> {(upper(k)): v * 10}`, `{"A":10,"B":20}`},
		{`{a: 1, b: null} filterObject (v) -> v != null`, `{"a":1}`},
		{`[1, 2, 3] flatMap [$, $]`, `[1,1,2,2,3,3]`},
		{`["a", "b"] joinBy "-"`, `"a-b"`},
		{`[1, 2, 3] contains 2`, `true`},
		{`sizeOf([1, 2]) + sizeOf({a: 1}) + sizeOf(null)`, `3`},
		{`isEmpty([]) and isEmpty({}) and isEmpty("") and isEmpty(null) and not isEmpty(0)`, `true`},
		{`[10, 20, 30][1]`, `20`},
		{`[10, 20, 30][-1]`, `30`},
		{`[10, 20, 30][5]`, `null`},
		{`[10, 20, 30, 40][1 to 2]`, `[20,30]`},
		{`[1, 2, 3][2 to 0]`, `[3,2,1]`},
		{`(1 to 4) map $ * $`, `[1,4,9,16]`},
		{`{a: {b: [{c: 1}, {c: 2}]}}.a.b.c`, `[1,2]`},
		{`{a: 1, a: 2}.*a`, `[1,2]`},
		{`{a: {x: 1}, b: [{x: 2}]}..x`, `[1,2]`},
		{`{"a-b": 1}."a-b"`, `1`},
		{`{a: 1}["a"]`, `1`},
		{`null.a.b`, `null`},
		{`{a: null}.a?`, `true`},
		{`{a: 1}.b?`, `false`},
		{`[1, 5, 3, 8][?($ > 2)]`, `[5,3,8]`},
		{`max([1, 5, 3]) + min([4, 2])`, `7`},
		{`sum([1, 2, 3]) / avg([1, 2, 3])`, `3`},
		{`keysOf({a: 1, b: 2}) ++ valuesOf({c: 3})`, `["a","b",3]`},
		{`entriesOf({a: 1})`, `[{"key":"a","value":1,"attributes":null}]`},
		{`[1, 2, 3, 4] take 2`, `[1,2]`},
		{`[1, 2, 3, 4] drop 2`, `[3,4]`},
		{`zip([1, 2], ["a", "b"])`, `[[1,"a"],[2,"b"]]`},
		{`{a: 1, b: 2} mergeWith {b: 3}`, `{"a":1,"b":3}`},
		{`[1, 2, 3] some ($ > 2)`, `true`},
		{`[1, 2, 3] every ($ > 2)`, `false`},
		// object constructors
		{`do { var k = "dyn" --- {(k): 1} }`, `{"dyn":1}`},
		{`{a: 1, (b: 2) if false, (c: 3) if true}`, `{"a":1,"c":3}`},
		{`{a: 1, ({b: 2, c: 3})}`, `{"a":1,"b":2,"c":3}`},
		{`{([{x: 1}, {y: 2}])}`, `{"x":1,"y":2}`},
		{`{("a" ++ "b"): 1}`, `{"ab":1}`},
		{`[1, (2) if false, 3]`, `[1,3]`},
		{`{} ++ ("k": 1) if (true)`, `{"k":1}`},
		// coercion and formatting
		{`"42" as Number`, `42`},
		{`"7490.0000" as Number`, `7490.0000`},
		{`1234.5 as String {format: "#,##0.00"}`, `"1,234.50"`},
		{`0.5 as String {format: "#.00"}`, `".50"`},
		{`2.675 as String {format: "0.00", roundMode: "HALF_UP"}`, `"2.68"`},
		{`2.665 as String {format: "0.00"}`, `"2.66"`},
		{`99.999 as String {format: "0"}`, `"100"`},
		{`1500 as String {format: "0.00"} as Number`, `1500.00`},
		{`0.256 as String {format: "#%"}`, `"26%"`},
		{`"true" as Boolean`, `true`},
		{`null as Number`, `null`},
		{`"21-JUN-07" as Date {format: "dd-MMM-yy"}`, `"2007-06-21"`},
		{`"20230102" as Date {format: "yyyyMMdd"} as String {format: "dd/MM/yyyy"}`, `"02/01/2023"`},
		{`"2023-05-01T10:20:30.123" as LocalDateTime {format: "yyyy-MM-dd'T'HH:mm:ss.SSS"} as String ++ "Z"`, `"2023-05-01T10:20:30.123Z"`},
		{`(|2023-01-31| as Date {format: "yyyy-MM-dd"} ++ |00:00:00Z|) as String`, `"2023-01-31T00:00:00Z"`},
		{`|2023-01-31| + |P1D|`, `"2023-02-01"`},
		{`|2023-03-10T08:30:00+07:00| as String {format: "yyyy-MM-dd HH:mm Z"}`, `"2023-03-10 08:30 +0700"`},
		{`|2020-02-29|.year + |2020-02-29|.month + |2020-02-29|.day`, `2051`},
		{`daysBetween(|2023-01-01|, |2023-03-01|)`, `59`},
		{`|2023-01-01T00:00:00Z| as Number`, `1672531200`},
		{`typeOf([]) ++ typeOf(1)`, `"ArrayNumber"`},
		// functions, pattern matching and errors
		{`do { fun twice(x) = x * 2 --- twice(21) }`, `42`},
		{`do { fun f(a, b = 10) = a + b --- [f(1), f(1, 2)] }`, `[11,3]`},
		{`do { fun fact(n) = if (n <= 1) 1 else n * fact(n - 1) --- fact(5) }`, `120`},
		{`do { var add = (a, b) -> a + b --- add(2, 3) }`, `5`},
		{`((x) -> x + 1)(1)`, `2`},
		{`"HOLN" match { case "HOFM" -> 1 case "HOLN" -> 2 else -> 3 }`, `2`},
		{`5 match { case n if n > 3 -> "big" else -> "small" }`, `"big"`},
		{`"x" match { case is Number -> "num" case is String -> "str" }`, `"str"`},
		{`"ab12" match { case m matches /([a-z]+)(\d+)/ -> m[2] else -> "" }`, `"12"`},
		{`try(() -> 1 / 0).success`, `false`},
		{`try(() -> 1 / 0).error.message`, `"division by zero"`},
		{`try(() -> 42).result`, `42`},
		{`(try(() -> fail("boom")) orElse "fallback")`, `"fallback"`},
		{`"x" then ($ ++ "y")`, `"xy"`},
		{`read("[1, 2]", "application/json")[1]`, `2`},
		{`read("a,b\n1,2", "application/csv")[0].b`, `"2"`},
		{`write({a: 1}, "application/json", {indent: false})`, `"{\"a\":1}"`},
		{`write([{a: 1, b: "x,y"}], "application/csv")`, `"a,b\n1,\"x,y\"\n"`},
		{`[1, 2] must equalTo([1, 2])`, `true`},
	}
	for _, tc := range tests {
		t.Run(tc.src, func(t *testing.T) {
			if got := evalJSON(t, tc.src, nil); got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestHeaderDeclarations(t *testing.T) {
	src := `%dw 2.0
output application/json skipNullOn="everywhere"
import * from dw::core::Strings
var limit = 10
fun double(n: Number): Number = n * 2
type Row = { id: Number }
---
{ limit: double(limit), when: later, ok: isEven(limit) }
`
	prog, err := Compile(src, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if out := prog.Output(); out == nil || out.Mime != "application/json" || out.Props["skipNullOn"] != "everywhere" {
		t.Fatalf("output directive = %+v", out)
	}
	_, err = prog.Eval(nil)
	if err == nil || !strings.Contains(err.Error(), `unable to resolve reference of "later"`) {
		t.Fatalf("expected unresolved reference error, got %v", err)
	}
	v, err := prog.Eval(map[string]value.Value{"later": value.Str("now")})
	if err != nil {
		t.Fatal(err)
	}
	if got := compact(t, v); got != `{"limit":20,"when":"now","ok":true}` {
		t.Fatalf(gotFmt, got)
	}
}

func TestLazyVariables(t *testing.T) {
	// The failing variable is never used, so evaluation succeeds.
	got := evalJSON(t, "var bad = 1 / 0\nvar good = 2\n---\ngood", nil)
	if got != "2" {
		t.Fatalf(gotFmt, got)
	}
	if _, err := EvalLiteral("var a = b\nvar b = a\n---\na"); err == nil || !strings.Contains(err.Error(), "references itself") {
		t.Fatalf("expected cycle error, got %v", err)
	}
}

func TestCompileErrors(t *testing.T) {
	tests := []struct{ src, want string }{
		{`1 +`, "1:4"},
		{`{a: 1`, `expected "}"`},
		{`"abc`, "unterminated"},
		{`sizeOf()`, "expects 1 argument"},
		{`$ + 1`, "inside a lambda"},
		{`|2023-13-01|`, "invalid date"},
		{`[1, 2] map Strings::nope($)`, "unable to resolve"},
		{"output\n---\n1", "media type"},
	}
	for _, tc := range tests {
		_, err := Compile(tc.src, Options{})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Compile(%q) error = %v, want %q", tc.src, err, tc.want)
		}
	}
}

func TestRuntimeErrors(t *testing.T) {
	tests := []struct{ src, want string }{
		{`"abc" as Number`, "cannot coerce String (abc) to Number"},
		{`{} + 1`, "cannot add Object and Number"},
		{`"x".a`, "cannot select .a on String"},
		{`5 match { case 1 -> 1 }`, "no case matched"},
		{`fail("custom")`, "custom"},
		{`[1] must equalTo([2])`, "expected [2] but was [1]"},
	}
	for _, tc := range tests {
		_, err := EvalLiteral(tc.src)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Eval(%q) error = %v, want %q", tc.src, err, tc.want)
		}
	}
}

// countingStream returns a one-shot stream of n numbers and a counter of how
// many elements were produced.
func countingStream(n int) (value.Value, *int) {
	produced := 0
	return value.NewStream(func(yield func(value.Value) error) error {
		for i := 0; i < n; i++ {
			produced++
			if err := yield(value.Int(i)); err != nil {
				return err
			}
		}
		return nil
	}, false), &produced
}

func TestStreamingStaysLazy(t *testing.T) {
	prog, err := Compile(`payload map ($ * 2) filter ($ > 4)`, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if prog.PayloadReusable() {
		t.Fatal("payload used once should not be marked reusable")
	}
	in, produced := countingStream(5)
	v, err := prog.Eval(map[string]value.Value{"payload": in})
	if err != nil {
		t.Fatal(err)
	}
	if *produced != 0 {
		t.Fatalf("stream was consumed during evaluation: %d elements", *produced)
	}
	if !v.Array().IsStream() {
		t.Fatal("result should remain a stream")
	}
	if got := compact(t, v); got != "[6,8]" {
		t.Fatalf(gotFmt, got)
	}
}

func TestStreamingIndexStopsEarly(t *testing.T) {
	in, produced := countingStream(1000)
	got := evalJSON(t, `payload[2]`, map[string]value.Value{"payload": in})
	if got != "2" || *produced != 3 {
		t.Fatalf("got %s after reading %d elements", got, *produced)
	}
}

func TestReusedPayloadIsBuffered(t *testing.T) {
	prog, err := Compile(`{count: sizeOf(payload), items: payload}`, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !prog.PayloadReusable() {
		t.Fatal("payload used twice should be marked reusable")
	}
	in, _ := countingStream(3)
	v, err := prog.Eval(map[string]value.Value{"payload": in})
	if err != nil {
		t.Fatal(err)
	}
	if got := compact(t, v); got != `{"count":3,"items":[0,1,2]}` {
		t.Fatalf(gotFmt, got)
	}
}

func TestReusedVariableIsBuffered(t *testing.T) {
	in, _ := countingStream(3)
	got := evalJSON(t, "var doubled = payload map $ * 2\n---\n{a: doubled, b: sizeOf(doubled)}", map[string]value.Value{"payload": in})
	if got != `{"a":[0,2,4],"b":3}` {
		t.Fatalf(gotFmt, got)
	}
}

func TestLambdaReuseInsideMap(t *testing.T) {
	in, _ := countingStream(2)
	got := evalJSON(t, `payload map (x) -> sizeOf(payload)`, map[string]value.Value{"payload": in})
	if got != "[2,2]" {
		t.Fatalf(gotFmt, got)
	}
}

func TestModuleImport(t *testing.T) {
	dir := t.TempDir()
	lib := "%dw 2.0\nvar greeting = \"hi\"\nfun greet(name) = greeting ++ \" \" ++ name\n"
	if err := writeFile(dir, "Lib.dwl", lib); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"import * from Lib\n---\ngreet(\"a\")":                        `"hi a"`,
		"import greet from Lib\n---\ngreet(\"b\")":                    `"hi b"`,
		"import Lib\n---\nLib::greet(\"c\")":                          `"hi c"`,
		"import dw::core::Strings\n---\nStrings::capitalize(\"x_y\")": `"X Y"`,
	}
	for src, want := range cases {
		prog, err := Compile(src, Options{Dir: dir})
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		v, err := prog.Eval(nil)
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		if got := compact(t, v); got != want {
			t.Errorf("%q = %s, want %s", src, got, want)
		}
	}
	if _, err := Compile("import missing from Nope\n---\n1", Options{Dir: dir}); err == nil {
		t.Fatal("expected missing module error")
	}
}

func TestLogWritesToConfiguredWriter(t *testing.T) {
	var logs bytes.Buffer
	prog, err := Compile(`log("value", {a: 1})`, Options{Log: &logs})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prog.Eval(nil); err != nil {
		t.Fatal(err)
	}
	if got := logs.String(); got != "value - {a: 1}\n" {
		t.Fatalf("log output = %q", got)
	}
}

func writeFile(dir, name, content string) error {
	return os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600)
}
