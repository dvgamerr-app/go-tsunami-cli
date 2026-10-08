package interp

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/touno-io/go-tsunami-cli/internal/syntax"
)

type sampleScript struct {
	name string
	src  string
}

var cdataPattern = regexp.MustCompile(`(?s)<!\[CDATA\[(.*?)\]\]>`)

// knownInvalid lists fragments of sources that are invalid in the sample
// repository itself: an XML file with escaped CDATA terminators and an array
// literal holding a key-value pair.
var knownInvalid = []string{"&#93;", `"productList": productList[0]`}

// TestCompileSamples compiles every DataWeave script found under the
// directory named by TSUNAMI_DW_SAMPLES, including scripts embedded in Mule
// XML configuration files. It is skipped when the variable is unset.
func TestCompileSamples(t *testing.T) {
	root := os.Getenv("TSUNAMI_DW_SAMPLES")
	if root == "" {
		t.Skip("set TSUNAMI_DW_SAMPLES to a directory of DataWeave scripts")
	}
	seen := map[string]bool{}
	failed := 0
	for _, s := range collectSamples(root) {
		if seen[s.src] || isKnownInvalid(s.src) {
			continue
		}
		seen[s.src] = true
		if err := compileSample(s); err != nil {
			failed++
			t.Errorf("%s: %v", s.name, err)
		}
	}
	t.Logf("compiled %d unique scripts, %d failed", len(seen), failed)
}

func isKnownInvalid(src string) bool {
	for _, bad := range knownInvalid {
		if strings.Contains(src, bad) {
			return true
		}
	}
	return false
}

func compileSample(s sampleScript) error {
	script, err := syntax.Parse(s.src)
	if err != nil || script.Body == nil {
		return err
	}
	_, err = CompileScript(script, Options{Dir: filepath.Dir(s.name)})
	return err
}

// collectSamples finds .dwl files and DataWeave blocks in Mule XML files.
func collectSamples(root string) []sampleScript {
	var out []sampleScript
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return skipDir(d.Name())
		}
		out = append(out, samplesIn(path)...)
		return nil
	})
	return out
}

func skipDir(name string) error {
	if name == "target" || name == "node_modules" || name == ".git" {
		return filepath.SkipDir
	}
	return nil
}

func samplesIn(path string) []sampleScript {
	ext := filepath.Ext(path)
	if ext != ".dwl" && ext != ".xml" {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	if ext == ".dwl" {
		return []sampleScript{{path, string(b)}}
	}
	var out []sampleScript
	for _, m := range cdataPattern.FindAllStringSubmatch(string(b), -1) {
		if src, ok := embeddedScript(m[1]); ok {
			out = append(out, sampleScript{path, src})
		}
	}
	return out
}

// embeddedScript unwraps a #[...] expression and decodes the escaped
// separator some XML editors write, returning whether it is DataWeave.
func embeddedScript(raw string) (string, bool) {
	src := strings.ReplaceAll(strings.TrimSpace(raw), "&#45;", "-")
	if strings.HasPrefix(src, "#[") && strings.HasSuffix(src, "]") {
		src = src[2 : len(src)-1]
	}
	return src, strings.Contains(src, "%dw") || strings.Contains(src, "---")
}
