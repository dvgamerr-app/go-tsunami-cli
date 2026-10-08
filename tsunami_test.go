package tsunami

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const csvMime = "text/csv"

func BenchmarkSyntax(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _, _, _ = syntaxSplit(`
output text/csv
---
payload
		`)
	}
}

func TestSyntaxTransform(t *testing.T) {
	tests := []struct {
		name        string
		syntax      string
		wantHeader  string
		wantPayload string
		wantOutput  string
	}{
		{
			name:        "payload only",
			syntax:      "payload",
			wantPayload: "payload",
			wantOutput:  defaultOutputType,
		},
		{
			name:        "single line",
			syntax:      "output text/csv --- payload",
			wantPayload: "payload",
			wantOutput:  csvMime,
		},
		{
			name: "multiple lines",
			syntax: `output text/csv
		---
		payload
		`,
			wantPayload: "payload",
			wantOutput:  csvMime,
		},
		{
			name: "header without output directive",
			syntax: `metadata
---
payload`,
			wantHeader:  "metadata",
			wantPayload: "payload",
			wantOutput:  defaultOutputType,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			header, payload, output, err := syntaxSplit(test.syntax)
			if err != nil {
				t.Fatalf("syntaxSplit() error = %v", err)
			}
			if got := string(header); got != test.wantHeader {
				t.Errorf("header = %q, want %q", got, test.wantHeader)
			}
			if got := string(payload); got != test.wantPayload {
				t.Errorf("payload = %q, want %q", got, test.wantPayload)
			}
			if got := string(output); got != test.wantOutput {
				t.Errorf("output = %q, want %q", got, test.wantOutput)
			}
		})
	}
}

func TestSyntaxSplitFile(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "transform.tsu")
	if err := os.WriteFile(filename, []byte("output text/csv\n---\npayload"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	header, payload, output, err := syntaxSplit(filename)
	if err != nil {
		t.Fatalf("syntaxSplit() error = %v", err)
	}
	if got := string(header); got != "" {
		t.Errorf("header = %q, want empty", got)
	}
	if got := string(payload); got != "payload" {
		t.Errorf("payload = %q, want %q", got, "payload")
	}
	if got := string(output); got != csvMime {
		t.Errorf("output = %q, want %q", got, csvMime)
	}
}

func TestSyntaxSplitMissingFile(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "missing.tsu")

	_, _, _, err := syntaxSplit(filename)
	if err == nil {
		t.Fatal("syntaxSplit() error = nil, want missing-file error")
	}

	want := "can't find the file 'missing.tsu'"
	if err.Error() != want {
		t.Fatalf("syntaxSplit() error = %q, want %q", err, want)
	}
}

func TestTrimByte(t *testing.T) {
	if got := string(trimByte([]byte("\t\r\n payload \n"))); got != "payload" {
		t.Fatalf("trimByte() = %q, want %q", got, "payload")
	}
}

func TestParseDefinition(t *testing.T) {
	def, err := ParseDefinition("%dw 2.0\noutput application/x-ndjson\n---\npayload")
	if err != nil {
		t.Fatal(err)
	}
	if def.Output != "application/x-ndjson" || def.Body != "payload" || def.Header != "" {
		t.Fatalf("definition = %+v", def)
	}
	filename := filepath.Join(t.TempDir(), "script.dwl")
	if err := os.WriteFile(filename, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	if def, err := ParseDefinition(filename); err != nil || def.Body != "payload" || def.Output != defaultOutputType {
		t.Fatalf("file definition = %+v, %v", def, err)
	}
}

func TestCompileAndTransform(t *testing.T) {
	script, err := Compile("output application/csv\n---\npayload map (r) -> { id: r.id as Number + 1, name: upper(r.name) }", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if script.OutputMime() != "application/csv" {
		t.Fatalf("output = %s", script.OutputMime())
	}
	out, err := script.Transform([]byte(`[{"id": "1", "name": "ann"}]`), "application/json")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "id,name\n2,ANN\n" {
		t.Fatalf("got %q", out)
	}
	ndjson := script.WithOutput("ndjson", nil)
	out, err = ndjson.Transform([]byte("id,name\n5,bob\n"), csvMime)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "{\"id\":6,\"name\":\"BOB\"}\n" {
		t.Fatalf("got %q", out)
	}
	if script.OutputMime() != "application/csv" {
		t.Fatal("WithOutput must not modify the original script")
	}
}

func TestDefaultOutputIsJSON(t *testing.T) {
	script, err := Compile("{a: 1}", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if script.OutputMime() != defaultOutputType {
		t.Fatalf("output = %s", script.OutputMime())
	}
	out, err := script.WithOutput("", map[string]string{"indent": "false"}).Transform(nil, "")
	if err != nil || string(out) != `{"a":1}` {
		t.Fatalf("got %q, %v", out, err)
	}
}

func TestRunInputsAndDirectives(t *testing.T) {
	dir := t.TempDir()
	csv := filepath.Join(dir, "in.txt")
	if err := os.WriteFile(csv, []byte("a;b\n1;2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	script, err := Compile("input payload application/csv separator=\";\"\noutput application/json indent=false\n---\n{rows: payload, who: vars.who, extra: lookup.k}", Options{})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = script.Run(context.Background(), &out,
		Input{Name: "payload", Path: csv},
		Input{Name: "vars", Data: []byte(`{"who": "me"}`)},
		Input{Name: "lookup", Reader: strings.NewReader(`{"k": true}`), MimeType: "application/json"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"rows":[{"a":"1","b":"2"}],"who":"me","extra":true}`; out.String() != want {
		t.Fatalf("got %s, want %s", out.String(), want)
	}
}

func TestRunErrors(t *testing.T) {
	if _, err := Compile("payload +", Options{}); err == nil || !strings.Contains(err.Error(), "1:10") {
		t.Fatalf("compile error = %v", err)
	}
	if _, err := CompileFile(filepath.Join(t.TempDir(), "nope.tsu"), Options{}); err == nil || !strings.Contains(err.Error(), "can't find the file 'nope.tsu'") {
		t.Fatalf("missing file error = %v", err)
	}
	bad := filepath.Join(t.TempDir(), "bad.dwl")
	if err := os.WriteFile(bad, []byte("---\n[1,"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CompileFile(bad, Options{}); err == nil || !strings.HasPrefix(err.Error(), "bad.dwl:2:") {
		t.Fatalf("file compile error = %v", err)
	}
	script, err := Compile(`payload map $ as Number`, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := script.Transform([]byte(`["1", "x"]`), "json"); err == nil || !strings.Contains(err.Error(), "cannot coerce String (x) to Number") {
		t.Fatalf("runtime error = %v", err)
	}
	if _, err := script.Transform([]byte(`[1`), "json"); err == nil || !strings.Contains(err.Error(), "reading payload") {
		t.Fatalf("read error = %v", err)
	}
	if _, err := script.WithOutput("application/x-nope", nil).Transform([]byte(`[]`), "json"); err == nil {
		t.Fatal("unsupported output should fail")
	}
}

func TestRunCancellation(t *testing.T) {
	var b strings.Builder
	b.WriteString("n\n")
	for i := 0; i < 5000; i++ {
		b.WriteString("1\n")
	}
	script, err := Compile("payload", Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = script.Run(ctx, io.Discard, Input{Name: "payload", Data: []byte(b.String()), MimeType: "csv"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func ExampleScript() {
	script, err := Compile(`%dw 2.0
output application/json indent=false
---
payload map (row) -> {
  id: row.id as Number,
  name: upper(row.name),
  joined: row.joined as Date {format: "dd/MM/yyyy"}
}`, Options{})
	if err != nil {
		panic(err)
	}
	out, err := script.Transform([]byte("id,name,joined\n1,ann,02/01/2023\n"), csvMime)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(out))
	// Output: [{"id":1,"name":"ANN","joined":"2023-01-02"}]
}

func benchmarkCSV(rows int) []byte {
	var b bytes.Buffer
	b.WriteString("id,name,email,amount,created\n")
	for i := 0; i < rows; i++ {
		fmt.Fprintf(&b, "%d,Name %d,user%d@example.com,%d.25,2023-01-%02d 10:00:00\n", i, i, i, i%500, i%28+1)
	}
	return b.Bytes()
}

func BenchmarkTransformCSVToJSON(b *testing.B) {
	data := benchmarkCSV(20000)
	script, err := Compile(`output application/json
---
payload map (r) -> {
  id: r.id as Number,
  name: upper(r.name),
  amount: r.amount as Number,
  created: r.created as LocalDateTime {format: "yyyy-MM-dd HH:mm:ss"}
}`, Options{})
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := script.Run(context.Background(), io.Discard, Input{Name: "payload", Data: data, MimeType: "csv"})
		if err != nil {
			b.Fatal(err)
		}
	}
}
