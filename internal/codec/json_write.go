package codec

import (
	"fmt"
	"io"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/touno-io/go-tsunami-cli/internal/value"
)

const writeBuffer = 64 << 10

var spaces = strings.Repeat(" ", 256)

// skipNull describes the skipNullOn writer property.
type skipNull struct {
	objects, arrays bool
}

func parseSkipNull(props map[string]string) skipNull {
	switch strings.ToLower(props["skipNullOn"]) {
	case "objects":
		return skipNull{objects: true}
	case "arrays":
		return skipNull{arrays: true}
	case "everywhere":
		return skipNull{objects: true, arrays: true}
	}
	return skipNull{}
}

type jsonWriter struct {
	w      sink
	indent bool
	step   int
	skip   skipNull
	depth  int
}

func newJSONWriter(w sink, props map[string]string) *jsonWriter {
	return &jsonWriter{
		w:      w,
		indent: boolProp(props, "indent", true),
		step:   2,
		skip:   parseSkipNull(props),
	}
}

// WriteJSON encodes v as JSON. Lazy arrays are streamed element by element.
// Properties: indent (default true) and skipNullOn (objects, arrays,
// everywhere).
func WriteJSON(w io.Writer, v value.Value, props map[string]string) error {
	bw := newSink(w)
	jw := newJSONWriter(bw, props)
	if err := jw.value(v); err != nil {
		_ = bw.flush()
		return err
	}
	return bw.flush()
}

// WriteNDJSON writes each element of an array as a compact JSON document on
// its own line.
func WriteNDJSON(w io.Writer, v value.Value, props map[string]string) error {
	bw := newSink(w)
	jw := newJSONWriter(bw, props)
	jw.indent = false
	err := eachElement(v, func(_ int, el value.Value) error {
		if el.IsNull() && jw.skip.arrays {
			return nil
		}
		if err := jw.value(el); err != nil {
			return err
		}
		bw.put('\n')
		return nil
	})
	if err != nil {
		_ = bw.flush()
		return err
	}
	return bw.flush()
}

func (j *jsonWriter) newline() {
	if !j.indent {
		return
	}
	j.w.put('\n')
	n := j.depth * j.step
	for n > len(spaces) {
		j.w.puts(spaces)
		n -= len(spaces)
	}
	j.w.puts(spaces[:n])
}

func (j *jsonWriter) value(v value.Value) error {
	switch v.K {
	case value.KindNull:
		j.w.puts("null")
	case value.KindBool:
		if v.N != 0 {
			j.w.puts("true")
		} else {
			j.w.puts("false")
		}
	case value.KindNumber:
		if math.IsNaN(v.N) || math.IsInf(v.N, 0) {
			j.w.puts("null")
		} else {
			j.w.puts(v.NumberText())
		}
	case value.KindString, value.KindRegex, value.KindType:
		writeJSONString(j.w, v.S)
	case value.KindArray, value.KindRange:
		return j.array(v)
	case value.KindObject:
		return j.object(v.Object())
	case value.KindFunction:
		return fmt.Errorf("cannot write a Function as JSON")
	default:
		s, err := value.ToString(v)
		if err != nil {
			return err
		}
		writeJSONString(j.w, s)
	}
	return nil
}

func (j *jsonWriter) array(v value.Value) error {
	j.w.put('[')
	j.depth++
	empty := true
	err := eachElement(v, func(_ int, el value.Value) error {
		if el.IsNull() && j.skip.arrays {
			return nil
		}
		if !empty {
			j.w.put(',')
		}
		empty = false
		j.newline()
		return j.value(el)
	})
	j.depth--
	if err != nil {
		return err
	}
	if !empty {
		j.newline()
	}
	j.w.put(']')
	return nil
}

func (j *jsonWriter) object(o *value.Object) error {
	j.w.put('{')
	j.depth++
	empty := true
	for i, n := 0, o.Len(); i < n; i++ {
		el := o.Val(i)
		if el.IsNull() && j.skip.objects {
			continue
		}
		if !empty {
			j.w.put(',')
		}
		empty = false
		j.newline()
		writeJSONString(j.w, o.Key(i))
		if j.indent {
			j.w.puts(": ")
		} else {
			j.w.put(':')
		}
		if err := j.value(el); err != nil {
			return err
		}
	}
	j.depth--
	if !empty {
		j.newline()
	}
	j.w.put('}')
	return nil
}

const (
	hexDigits = "0123456789abcdef"
	backslash = byte(92)
	lineSep   = 0x2028
	paraSep   = 0x2029
)

// writeJSONString writes s as a quoted JSON string, escaping only what JSON
// requires so non-ASCII text stays readable.
func writeJSONString(w sink, s string) {
	w.put('"')
	start := 0
	for i := 0; i < len(s); {
		c := s[i]
		if c >= 0x20 && c != '"' && c != backslash && c < utf8.RuneSelf {
			i++
			continue
		}
		if c >= utf8.RuneSelf {
			r, size := utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError && size == 1 || r == lineSep || r == paraSep {
				w.puts(s[start:i])
				writeUnicodeEscape(w, r)
				i += size
				start = i
				continue
			}
			i += size
			continue
		}
		w.puts(s[start:i])
		switch c {
		case '"':
			w.puts(`\"`)
		case backslash:
			w.put(backslash)
			w.put(backslash)
		case '\n':
			w.puts(`\n`)
		case '\r':
			w.puts(`\r`)
		case '\t':
			w.puts(`\t`)
		case '\b':
			w.puts(`\b`)
		case '\f':
			w.puts(`\f`)
		default:
			writeUnicodeEscape(w, rune(c))
		}
		i++
		start = i
	}
	w.puts(s[start:])
	w.put('"')
}

// writeUnicodeEscape writes r as a four-digit JSON unicode escape.
func writeUnicodeEscape(w sink, r rune) {
	w.put(backslash)
	w.put('u')
	w.put(hexDigits[r>>12&0xF])
	w.put(hexDigits[r>>8&0xF])
	w.put(hexDigits[r>>4&0xF])
	w.put(hexDigits[r&0xF])
}
