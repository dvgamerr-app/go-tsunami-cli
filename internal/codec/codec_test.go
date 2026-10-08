package codec

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/touno-io/go-tsunami-cli/internal/value"
)

func mustRead(t *testing.T, read ReadFunc, src string, props map[string]string, stream bool) value.Value {
	t.Helper()
	v, err := read(StringSource(src), ReadOptions{Props: props, Stream: stream})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return v
}

func write(t *testing.T, w WriteFunc, v value.Value, props map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := w(&buf, v, props); err != nil {
		t.Fatalf("write: %v", err)
	}
	return buf.String()
}

const unsupportedMime = "application/x-unsupported"

var compactJSON = map[string]string{"indent": "false"}

func TestJSONRoundTrip(t *testing.T) {
	bs := "\\"
	src := `{"a": [1, 2.50, -3e2], "b": {"c": null, "d": true}, "e": "x\"y\\z\n` +
		bs + `u00e9` + bs + `ud83d` + bs + `ude00", "f": []}`
	for _, stream := range []bool{false, true} {
		v := mustRead(t, ReadJSON, src, nil, stream)
		got := write(t, WriteJSON, v, compactJSON)
		want := "{\"a\":[1,2.50,-3e2],\"b\":{\"c\":null,\"d\":true},\"e\":\"x\\\"y\\\\z\\né\U0001F600\",\"f\":[]}"
		if got != want {
			t.Errorf("stream=%v\ngot  %s\nwant %s", stream, got, want)
		}
	}
}

func TestJSONPrettyMatchesDataWeaveLayout(t *testing.T) {
	v := mustRead(t, ReadJSON, `{"a":1,"b":[1,{"c":[]}],"d":{}}`, nil, false)
	want := "{\n  \"a\": 1,\n  \"b\": [\n    1,\n    {\n      \"c\": []\n    }\n  ],\n  \"d\": {}\n}"
	if got := write(t, WriteJSON, v, nil); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestJSONSkipNull(t *testing.T) {
	v := mustRead(t, ReadJSON, `{"a":null,"b":[null,1],"c":{"d":null}}`, nil, false)
	tests := map[string]string{
		"objects":    `{"b":[null,1],"c":{}}`,
		"arrays":     `{"a":null,"b":[1],"c":{"d":null}}`,
		"everywhere": `{"b":[1],"c":{}}`,
	}
	for mode, want := range tests {
		if got := write(t, WriteJSON, v, map[string]string{"indent": "false", "skipNullOn": mode}); got != want {
			t.Errorf("%s: got %s, want %s", mode, got, want)
		}
	}
}

func TestJSONErrors(t *testing.T) {
	for _, src := range []string{`{"a":}`, `[1,`, `{"a" 1}`, `tru`, `"abc`, `[1] x`} {
		if _, err := ReadJSON(StringSource(src), ReadOptions{}); err == nil {
			t.Errorf("ReadJSON(%q) succeeded, want error", src)
		}
	}
}

func TestJSONStreamIsLazyAndRestartable(t *testing.T) {
	v := mustRead(t, ReadJSON, `[{"id":1},{"id":2},{"id":3}]`, nil, true)
	arr := v.Array()
	if !arr.IsStream() || !arr.Restartable() {
		t.Fatal("expected a restartable stream")
	}
	for i := 0; i < 2; i++ {
		n, err := arr.Len()
		if err != nil || n != 3 {
			t.Fatalf("pass %d: len=%d err=%v", i, n, err)
		}
	}
	first, ok, err := arr.At(0)
	if err != nil || !ok {
		t.Fatal(err)
	}
	if id, _ := first.Object().Get("id"); id.N != 1 {
		t.Fatalf("first id = %v", id)
	}
}

func TestOneShotStreamFailsOnSecondPass(t *testing.T) {
	v, err := ReadNDJSON(ReaderSource(strings.NewReader("1\n2\n")), ReadOptions{Stream: true})
	if err != nil {
		t.Fatal(err)
	}
	if n, err := v.Array().Len(); err != nil || n != 2 {
		t.Fatalf("len=%d err=%v", n, err)
	}
	if _, err := v.Array().Len(); err != value.ErrConsumed {
		t.Fatalf("second pass error = %v, want ErrConsumed", err)
	}
}

func TestCSVRead(t *testing.T) {
	src := "\xef\xbb\xbfid,name,note\r\n1,\"Smith, J\",\"say \"\"hi\"\"\"\r\n\r\n2,ไทย,\"multi\nline\"\n3,short\n4,a,b,extra\n"
	v := mustRead(t, ReadCSV, src, nil, false)
	got := write(t, WriteJSON, v, compactJSON)
	want := `[{"id":"1","name":"Smith, J","note":"say \"hi\""},{"id":"2","name":"ไทย","note":"multi\nline"},{"id":"3","name":"short"},{"id":"4","name":"a","note":"b","column_3":"extra"}]`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestCSVOptions(t *testing.T) {
	v := mustRead(t, ReadCSV, "a;b\n1;2\n", map[string]string{"separator": ";", "header": "false"}, false)
	if got := write(t, WriteJSON, v, compactJSON); got != `[{"column_0":"a","column_1":"b"},{"column_0":"1","column_1":"2"}]` {
		t.Errorf("got %s", got)
	}
	tsv := mustRead(t, ReadCSV, "a\tb\n1\t2\n", map[string]string{"separator": `\t`}, false)
	if got := write(t, WriteJSON, tsv, compactJSON); got != `[{"a":"1","b":"2"}]` {
		t.Errorf("tsv got %s", got)
	}
}

func TestCSVWrite(t *testing.T) {
	v := mustRead(t, ReadJSON, `[{"a":1,"b":"x,y","c":"q\"t"},{"b":"2","a":null,"c":true}]`, nil, false)
	want := "a,b,c\n1,\"x,y\",\"q\"\"t\"\n,2,true\n"
	if got := write(t, WriteCSV, v, nil); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := write(t, WriteCSV, v, map[string]string{"header": "false", "separator": "|", "quoteValues": "true"}); got != "\"1\"|\"x,y\"|\"q\"\"t\"\n\"\"|\"2\"|\"true\"\n" {
		t.Errorf("options got %q", got)
	}
	if err := WriteCSV(io.Discard, mustRead(t, ReadJSON, `[{"a":[1]}]`, nil, false), nil); err == nil {
		t.Error("nested values should be rejected")
	}
}

func TestNDJSON(t *testing.T) {
	v := mustRead(t, ReadNDJSON, "{\"a\":1}\n\n{\"a\":2}\r\n", nil, true)
	if got := write(t, WriteNDJSON, v, nil); got != "{\"a\":1}\n{\"a\":2}\n" {
		t.Errorf("got %q", got)
	}
	if _, err := ReadNDJSON(StringSource("{\"a\":1}\n{bad}\n"), ReadOptions{}); err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Errorf("expected line 2 error, got %v", err)
	}
}

func TestXML(t *testing.T) {
	v := mustRead(t, ReadXML, `<?xml version="1.0"?><root><item id="1"><name>A &amp; B</name></item><item><name>C</name><empty/></item></root>`, nil, false)
	if got := write(t, WriteJSON, v, compactJSON); got != `{"root":{"item":{"name":"A & B"},"item":{"name":"C","empty":null}}}` {
		t.Errorf("read got %s", got)
	}
	out := mustRead(t, ReadJSON, `{"orders":{"order":[{"id":1,"note":"<x>"},{"id":2,"note":null}]}}`, nil, false)
	want := "<?xml version='1.0' encoding='UTF-8'?>\n<orders>\n  <order>\n    <id>1</id>\n    <note>&lt;x&gt;</note>\n  </order>\n  <order>\n    <id>2</id>\n    <note/>\n  </order>\n</orders>\n"
	if got := write(t, WriteXML, out, nil); got != want {
		t.Errorf("write got\n%s\nwant\n%s", got, want)
	}
	if err := WriteXML(io.Discard, mustRead(t, ReadJSON, `{"a":1,"b":2}`, nil, false), nil); err == nil {
		t.Error("multiple roots should be rejected")
	}
}

func TestYAML(t *testing.T) {
	v := mustRead(t, ReadJSON, `{"name":"x","tags":["a","true"],"nested":{"n":1,"list":[{"k":"v","w":null}],"empty":[]},"s":"a: b"}`, nil, false)
	want := "%YAML 1.2\n---\nname: x\ntags:\n  - a\n  - \"true\"\nnested:\n  n: 1\n  list:\n    - k: v\n      w: null\n  empty: []\ns: \"a: b\"\n"
	if got := write(t, WriteYAML, v, nil); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestTextAndDW(t *testing.T) {
	if got := write(t, WriteText, value.Str("hello"), nil); got != "hello" {
		t.Errorf("text got %q", got)
	}
	if err := WriteText(io.Discard, value.EmptyObject(), nil); err == nil {
		t.Error("objects should not be written as text")
	}
	v := mustRead(t, ReadJSON, `{"a b":[1,"x"],"c":{}}`, nil, false)
	if got := write(t, WriteDW, v, nil); got != "{\n  \"a b\": [\n    1,\n    \"x\"\n  ],\n  c: {}\n}" {
		t.Errorf("dw got %q", got)
	}
	tv, err := ReadText(StringSource("plain"), ReadOptions{})
	if err != nil || tv.S != "plain" {
		t.Errorf("ReadText = %v, %v", tv, err)
	}
}

func TestMediaTypes(t *testing.T) {
	tests := map[string]string{
		"csv":                      CSV,
		"text/csv; charset=utf-8":  CSV,
		"application/vnd.api+json": JSON,
		"JSON":                     JSON,
		"jsonl":                    NDJSON,
		"tsv":                      CSV,
		unsupportedMime:            unsupportedMime,
	}
	for in, want := range tests {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
	if DefaultProps("tsv")["separator"] != "\t" {
		t.Error("tsv should default to a tab separator")
	}
	if MimeForPath("x/data.JSONL") != NDJSON || MimeForPath("a.csv") != CSV || Extension("yaml") != ".yaml" {
		t.Error("extension mapping is wrong")
	}
	if _, err := Lookup(unsupportedMime); err == nil {
		t.Error("Lookup should reject unknown media types")
	}
}

func makeCSV(rows int) string {
	var b strings.Builder
	b.WriteString("id,name,email,city,amount,created\n")
	for i := 0; i < rows; i++ {
		fmt.Fprintf(&b, "%d,Name %d,user%d@example.com,\"Bangkok, TH\",%d.50,2023-01-%02d\n", i, i, i, i%1000, i%28+1)
	}
	return b.String()
}

func BenchmarkCSVToNDJSON(b *testing.B) {
	data := makeCSV(20000)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		v, err := ReadCSV(StringSource(data), ReadOptions{Stream: true})
		if err != nil {
			b.Fatal(err)
		}
		if err := WriteNDJSON(io.Discard, v, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkJSONStreamToCSV(b *testing.B) {
	var buf bytes.Buffer
	v, _ := ReadCSV(StringSource(makeCSV(20000)), ReadOptions{})
	_ = WriteJSON(&buf, v, nil)
	data := buf.String()
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		v, err := ReadJSON(StringSource(data), ReadOptions{Stream: true})
		if err != nil {
			b.Fatal(err)
		}
		if err := WriteCSV(io.Discard, v, nil); err != nil {
			b.Fatal(err)
		}
	}
}
