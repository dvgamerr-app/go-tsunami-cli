package codec

import (
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/touno-io/go-tsunami-cli/internal/value"
)

// WriteDW writes v using DataWeave literal syntax, which is handy for
// inspecting values and for writing test fixtures.
func WriteDW(w io.Writer, v value.Value, props map[string]string) error {
	d := &dwWriter{out: newSink(w), indent: boolProp(props, "indent", true)}
	if err := d.value(v, 0); err != nil {
		_ = d.out.flush()
		return err
	}
	return d.out.flush()
}

type dwWriter struct {
	out    sink
	indent bool
}

func (d *dwWriter) newline(depth int) {
	if d.indent {
		d.out.put('\n')
		d.out.puts(spaces[:min(2*depth, len(spaces))])
	}
}

func (d *dwWriter) value(v value.Value, depth int) error {
	switch v.K {
	case value.KindArray:
		return d.array(v.Array(), depth)
	case value.KindObject:
		return d.object(v.Object(), depth)
	}
	s, err := dwScalar(v)
	if err != nil {
		return err
	}
	if v.K == value.KindString {
		writeJSONString(d.out, s)
		return nil
	}
	d.out.puts(s)
	return nil
}

// dwScalar renders a non-container value in DataWeave literal syntax.
func dwScalar(v value.Value) (string, error) {
	switch v.K {
	case value.KindNull:
		return "null", nil
	case value.KindString, value.KindType:
		return v.S, nil
	case value.KindRegex:
		return "/" + v.S + "/", nil
	case value.KindFunction:
		return "(...) -> ???", nil
	case value.KindBool, value.KindNumber:
		return value.ToString(v)
	}
	s, err := value.ToString(v)
	return "|" + s + "|", err
}

func (d *dwWriter) array(a *value.Array, depth int) error {
	d.out.put('[')
	empty := true
	err := a.Each(func(_ int, el value.Value) error {
		if !empty {
			d.out.put(',')
		}
		empty = false
		d.newline(depth + 1)
		return d.value(el, depth+1)
	})
	if err != nil {
		return err
	}
	if !empty {
		d.newline(depth)
	}
	d.out.put(']')
	return nil
}

func (d *dwWriter) object(obj *value.Object, depth int) error {
	d.out.put('{')
	for i, n := 0, obj.Len(); i < n; i++ {
		if i > 0 {
			d.out.put(',')
		}
		d.newline(depth + 1)
		d.out.puts(dwKey(obj.Key(i)))
		d.out.puts(": ")
		if err := d.value(obj.Val(i), depth+1); err != nil {
			return err
		}
	}
	if obj.Len() > 0 {
		d.newline(depth)
	}
	d.out.put('}')
	return nil
}

// dwKey quotes keys that are not plain identifiers.
func dwKey(k string) string {
	if isIdentifier(k) {
		return k
	}
	var b strings.Builder
	b.WriteByte('"')
	b.WriteString(strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(k))
	b.WriteByte('"')
	return b.String()
}

func isIdentifier(k string) bool {
	if r, _ := utf8.DecodeRuneInString(k); k == "" || unicode.IsDigit(r) {
		return false
	}
	for _, r := range k {
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
