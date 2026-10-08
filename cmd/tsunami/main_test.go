package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	exitFmt   = "exit %d: %s"
	gotQuoted = "got %q"
)

func runCLI(t *testing.T, stdin string, args ...string) (string, string, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(context.Background(), args, strings.NewReader(stdin), &out, &errOut)
	return out.String(), errOut.String(), code
}

func writeTemp(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestConvertFormats(t *testing.T) {
	dir := t.TempDir()
	csv := writeTemp(t, dir, "people.csv", "id,name\n1,Ann\n2,Bob\n")
	tests := map[string]string{
		"ndjson": "{\"id\":\"1\",\"name\":\"Ann\"}\n{\"id\":\"2\",\"name\":\"Bob\"}\n",
		"tsv":    "id\tname\n1\tAnn\n2\tBob\n",
		"yaml":   "%YAML 1.2\n---\n- id: \"1\"\n  name: Ann\n- id: \"2\"\n  name: Bob\n",
	}
	for to, want := range tests {
		out, errOut, code := runCLI(t, "", "-to", to, csv)
		if code != 0 {
			t.Fatalf("-to %s: exit %d: %s", to, code, errOut)
		}
		if out != want {
			t.Errorf("-to %s: got %q, want %q", to, out, want)
		}
	}
}

func TestInlineScriptWithStdin(t *testing.T) {
	out, errOut, code := runCLI(t, `[{"n": 1}, {"n": 2}]`, "-e", "output application/json indent=false\n---\npayload map $.n * 10")
	if code != 0 {
		t.Fatalf(exitFmt, code, errOut)
	}
	if out != "[10,20]" {
		t.Errorf(gotQuoted, out)
	}
}

func TestScriptFileVarsAndOptions(t *testing.T) {
	dir := t.TempDir()
	script := writeTemp(t, dir, "t.dwl", "%dw 2.0\noutput application/csv\n---\npayload map (r) -> { name: upper(r.name), tag: vars.tag }")
	csv := writeTemp(t, dir, "in.csv", "name;x\nann;1\n")
	out, errOut, code := runCLI(t, "", "-t", script, "-var", "tag=vip", "-in-opt", "separator=;", "-out-opt", "separator=|", csv)
	if code != 0 {
		t.Fatalf(exitFmt, code, errOut)
	}
	if out != "name|tag\nANN|vip\n" {
		t.Errorf(gotQuoted, out)
	}
}

func TestSiblingScriptAndOutputDirectory(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "a.tsu", "output application/json indent=false\n---\nsizeOf(payload)")
	a := writeTemp(t, dir, "a.json", "[1,2,3]")
	writeTemp(t, dir, "b.tsu", "output application/json indent=false\n---\npayload.x")
	b := writeTemp(t, dir, "b.json", `{"x":"y"}`)
	outDir := filepath.Join(dir, "out")
	if _, errOut, code := runCLI(t, "", "-o", outDir, a, b); code != 0 {
		t.Fatalf(exitFmt, code, errOut)
	}
	for name, want := range map[string]string{"a.json": "3", "b.json": `"y"`} {
		got, err := os.ReadFile(filepath.Join(outDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestNamedInputs(t *testing.T) {
	dir := t.TempDir()
	lookup := writeTemp(t, dir, "lookup.json", `{"1": "one"}`)
	out, errOut, code := runCLI(t, `[{"id": "1"}]`, "-i", "codes="+lookup, "-e", "output application/json indent=false\n---\npayload map codes[$.id]")
	if code != 0 {
		t.Fatalf(exitFmt, code, errOut)
	}
	if out != `["one"]` {
		t.Errorf(gotQuoted, out)
	}
}

func TestErrorsAndUsage(t *testing.T) {
	dir := t.TempDir()
	in := writeTemp(t, dir, "x.json", "[]")
	tests := []struct {
		args []string
		code int
		want string
	}{
		{[]string{in}, 1, "no transformation given"},
		{[]string{"-e", "payload +", in}, 1, "1:10"},
		{[]string{"-t", filepath.Join(dir, "missing.dwl"), in}, 1, "can't find the file 'missing.dwl'"},
		{[]string{"-to", "nope", in}, 1, "unsupported media type"},
		{[]string{"-var", "novalue", "-to", "json", in}, 1, "expected key=value"},
		{[]string{"-memlimit", "lots", "-to", "json", in}, 2, "invalid size"},
		{[]string{"-unknown"}, 2, "flag provided but not defined"},
	}
	for _, tc := range tests {
		_, errOut, code := runCLI(t, "", tc.args...)
		if code != tc.code || !strings.Contains(errOut, tc.want) {
			t.Errorf("%v: exit %d, stderr %q; want exit %d containing %q", tc.args, code, errOut, tc.code, tc.want)
		}
	}
	if out, _, code := runCLI(t, "", "-version"); code != 0 || !strings.HasPrefix(out, "tsunami ") {
		t.Errorf("-version: %d %q", code, out)
	}
	if _, errOut, code := runCLI(t, "", "-h"); code != 0 || !strings.Contains(errOut, "Usage: tsunami") {
		t.Errorf("-h: %d %q", code, errOut)
	}
}

func TestStatsAndProfiles(t *testing.T) {
	dir := t.TempDir()
	in := writeTemp(t, dir, "x.json", "[1]")
	cpu, mem := filepath.Join(dir, "cpu.out"), filepath.Join(dir, "mem.out")
	_, errOut, code := runCLI(t, "", "-stats", "-memlimit", "64MiB", "-cpuprofile", cpu, "-memprofile", mem, "-to", "json", in)
	if code != 0 || !strings.Contains(errOut, "elapsed=") {
		t.Fatalf(exitFmt, code, errOut)
	}
	for _, f := range []string{cpu, mem} {
		if st, err := os.Stat(f); err != nil || st.Size() == 0 {
			t.Errorf("profile %s not written: %v", f, err)
		}
	}
}

func TestParseSize(t *testing.T) {
	tests := map[string]int64{"512MiB": 512 << 20, "2GB": 2e9, "1.5KiB": 1536, "100": 100, "0": 0}
	for in, want := range tests {
		if got, err := parseSize(in); err != nil || got != want {
			t.Errorf("parseSize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	if _, err := parseSize("-1MB"); err == nil {
		t.Error("negative sizes should be rejected")
	}
}
