package codec

import (
	"fmt"
	"io"
	"strings"

	"github.com/touno-io/go-tsunami-cli/internal/value"
)

// WriteYAML writes v as block-style YAML, streaming arrays element by
// element.
func WriteYAML(w io.Writer, v value.Value, props map[string]string) error {
	y := &yamlWriter{out: newSink(w), skip: parseSkipNull(props)}
	y.out.puts("%YAML 1.2\n---\n")
	if err := y.node(v, 0, false); err != nil {
		_ = y.out.flush()
		return err
	}
	y.out.put('\n')
	return y.out.flush()
}

type yamlWriter struct {
	out  sink
	skip skipNull
}

// line starts a new line indented by n spaces.
func (y *yamlWriter) line(n int) {
	y.out.put('\n')
	y.out.puts(spaces[:min(n, len(spaces))])
}

// node writes v with nested lines indented by indent spaces. inline is true
// when the cursor follows "key:" or "- ".
func (y *yamlWriter) node(v value.Value, indent int, inline bool) error {
	switch v.K {
	case value.KindObject:
		return y.object(v.Object(), indent, inline)
	case value.KindArray, value.KindRange:
		return y.sequence(v, indent, inline)
	case value.KindFunction:
		return fmt.Errorf("cannot write a Function as YAML")
	}
	if inline {
		y.out.put(' ')
	}
	s, err := yamlScalar(v)
	if err != nil {
		return err
	}
	y.out.puts(s)
	return nil
}

func (y *yamlWriter) object(obj *value.Object, indent int, inline bool) error {
	if obj.Len() == 0 {
		y.empty("{}", inline)
		return nil
	}
	first := true
	for i, n := 0, obj.Len(); i < n; i++ {
		el := obj.Val(i)
		if el.IsNull() && y.skip.objects {
			continue
		}
		if !first || inline {
			y.line(indent)
		}
		first = false
		y.out.puts(yamlString(obj.Key(i)))
		y.out.put(':')
		if err := y.node(el, indent+2, true); err != nil {
			return err
		}
	}
	return nil
}

func (y *yamlWriter) sequence(v value.Value, indent int, inline bool) error {
	first := true
	err := eachElement(v, func(_ int, el value.Value) error {
		if el.IsNull() && y.skip.arrays {
			return nil
		}
		if !first || inline {
			y.line(indent)
		}
		first = false
		y.out.puts("- ")
		return y.node(el, indent+2, false)
	})
	if err == nil && first {
		y.empty("[]", inline)
	}
	return err
}

func (y *yamlWriter) empty(s string, inline bool) {
	if inline {
		y.out.put(' ')
	}
	y.out.puts(s)
}

func yamlScalar(v value.Value) (string, error) {
	switch v.K {
	case value.KindNull:
		return "null", nil
	case value.KindBool, value.KindNumber:
		return value.ToString(v)
	}
	s, err := value.ToString(v)
	return yamlString(s), err
}

// yamlKeywords are plain scalars that YAML would read as another type.
var yamlKeywords = map[string]bool{"true": true, "false": true, "null": true, "yes": true, "no": true, "on": true, "off": true, "~": true}

// yamlString quotes s when a plain scalar would be ambiguous.
func yamlString(s string) string {
	if !yamlNeedsQuotes(s) {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		writeYAMLByte(&b, s[i])
	}
	b.WriteByte('"')
	return b.String()
}

func yamlNeedsQuotes(s string) bool {
	if s == "" || yamlKeywords[strings.ToLower(s)] {
		return true
	}
	if _, ok := value.ParseNumber(s); ok {
		return true
	}
	if strings.ContainsAny(s[:1], "-?:,[]{}#&*!|>'\"%@` ") || strings.HasSuffix(s, " ") ||
		strings.Contains(s, ": ") || strings.Contains(s, " #") {
		return true
	}
	return strings.IndexFunc(s, func(r rune) bool { return r < 0x20 }) >= 0
}

var yamlEscapes = map[byte]string{'"': `\"`, '\\': `\\`, '\n': `\n`, '\r': `\r`, '\t': `\t`}

func writeYAMLByte(b *strings.Builder, c byte) {
	if esc, ok := yamlEscapes[c]; ok {
		b.WriteString(esc)
		return
	}
	if c < 0x20 {
		fmt.Fprintf(b, `\x%02x`, c)
		return
	}
	b.WriteByte(c)
}
