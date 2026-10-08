package codec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/touno-io/go-tsunami-cli/internal/value"
)

const (
	readChunk      = 64 << 10
	maxInternedKey = 4096
	ctxCheckEvery  = 1024
	maxShapeDepth  = 64
)

// jsonDecoder is a streaming JSON parser over a refillable buffer. When the
// whole document is already in memory, r is nil and no refills happen.
type jsonDecoder struct {
	r   io.Reader
	buf []byte
	pos int
	eof bool
	err error
	off int64

	keys    map[string]string
	shapes  []*value.Shape
	scratch []byte
}

func newJSONDecoder(r io.Reader) *jsonDecoder {
	return &jsonDecoder{r: r, buf: make([]byte, 0, readChunk), keys: map[string]string{}}
}

func newJSONBytesDecoder(b []byte) *jsonDecoder {
	return &jsonDecoder{buf: b, eof: true, keys: map[string]string{}}
}

// fill reads more input, keeping bytes from mark onward. It returns the new
// position of mark and whether more data is available.
func (d *jsonDecoder) fill(mark int) (int, bool) {
	if d.eof {
		return mark, false
	}
	if mark > 0 {
		n := copy(d.buf, d.buf[mark:])
		d.off += int64(mark)
		d.pos -= mark
		d.buf = d.buf[:n]
		mark = 0
	}
	if len(d.buf) == cap(d.buf) {
		nb := make([]byte, len(d.buf), 2*cap(d.buf)+readChunk)
		copy(nb, d.buf)
		d.buf = nb
	}
	for {
		n, err := d.r.Read(d.buf[len(d.buf):cap(d.buf)])
		d.buf = d.buf[:len(d.buf)+n]
		if err != nil {
			d.eof = true
			if err != io.EOF {
				d.err = err
			}
			return mark, n > 0
		}
		if n > 0 {
			return mark, true
		}
	}
}

func (d *jsonDecoder) errorf(format string, args ...any) error {
	if d.err != nil {
		return d.err
	}
	return fmt.Errorf("invalid JSON at offset %d: %s", d.off+int64(d.pos), fmt.Sprintf(format, args...))
}

// skipSpace advances past whitespace and returns the next byte.
func (d *jsonDecoder) skipSpace() (byte, bool) {
	for {
		for d.pos < len(d.buf) {
			c := d.buf[d.pos]
			if c != ' ' && c != '\n' && c != '\r' && c != '\t' {
				return c, true
			}
			d.pos++
		}
		if _, ok := d.fill(d.pos); !ok {
			return 0, false
		}
	}
}

func (d *jsonDecoder) skipBOM() {
	if d.pos+3 > len(d.buf) {
		d.fill(d.pos)
	}
	if len(d.buf)-d.pos >= 3 && d.buf[d.pos] == 0xEF && d.buf[d.pos+1] == 0xBB && d.buf[d.pos+2] == 0xBF {
		d.pos += 3
	}
}

func (d *jsonDecoder) value(depth int) (value.Value, error) {
	if depth > 10000 {
		return value.Null, d.errorf("nesting too deep")
	}
	c, ok := d.skipSpace()
	if !ok {
		return value.Null, d.errorf("unexpected end of input")
	}
	switch {
	case c == '{':
		d.pos++
		return d.object(depth)
	case c == '[':
		d.pos++
		return d.array(depth)
	case c == '"':
		d.pos++
		s, err := d.str()
		return value.Str(s), err
	case c == '-' || (c >= '0' && c <= '9'):
		return d.number()
	case c == 't':
		return value.True, d.literal("true")
	case c == 'f':
		return value.False, d.literal("false")
	case c == 'n':
		return value.Null, d.literal("null")
	}
	return value.Null, d.errorf("unexpected character %q", c)
}

func (d *jsonDecoder) literal(word string) error {
	for len(d.buf)-d.pos < len(word) {
		if _, ok := d.fill(d.pos); !ok {
			break
		}
	}
	if len(d.buf)-d.pos < len(word) || string(d.buf[d.pos:d.pos+len(word)]) != word {
		return d.errorf("invalid literal")
	}
	d.pos += len(word)
	return nil
}

func (d *jsonDecoder) number() (value.Value, error) {
	start := d.pos
	for {
		for d.pos < len(d.buf) {
			c := d.buf[d.pos]
			if (c >= '0' && c <= '9') || c == '-' || c == '+' || c == '.' || c == 'e' || c == 'E' {
				d.pos++
				continue
			}
			return d.makeNumber(d.buf[start:d.pos])
		}
		var ok bool
		start, ok = d.fill(start)
		if !ok {
			return d.makeNumber(d.buf[start:d.pos])
		}
	}
}

func (d *jsonDecoder) makeNumber(b []byte) (value.Value, error) {
	s := string(b)
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		var ne *strconv.NumError
		if !errors.As(err, &ne) || ne.Err != strconv.ErrRange {
			return value.Null, d.errorf("invalid number %q", s)
		}
	}
	return value.NumRaw(f, s), nil
}

// strBytes reads a string body after the opening quote. The returned slice is
// only valid until the next read.
func (d *jsonDecoder) strBytes() ([]byte, error) {
	start := d.pos
	for {
		for d.pos < len(d.buf) {
			c := d.buf[d.pos]
			if c == '"' {
				b := d.buf[start:d.pos]
				d.pos++
				return b, nil
			}
			if c == '\\' {
				return d.strEscaped(start)
			}
			d.pos++
		}
		var ok bool
		start, ok = d.fill(start)
		if !ok {
			return nil, d.errorf("unterminated string")
		}
	}
}

func (d *jsonDecoder) strEscaped(start int) ([]byte, error) {
	out := append(d.scratch[:0], d.buf[start:d.pos]...)
	for {
		if d.pos >= len(d.buf) {
			if _, ok := d.fill(d.pos); !ok {
				return nil, d.errorf("unterminated string")
			}
		}
		c := d.buf[d.pos]
		switch c {
		case '"':
			d.pos++
			d.scratch = out
			return out, nil
		case '\\':
			var err error
			if out, err = d.escape(out); err != nil {
				return nil, err
			}
		default:
			out = append(out, c)
			d.pos++
		}
	}
}

// simpleEscapes maps single-character JSON escapes to their bytes.
var simpleEscapes = map[byte]byte{'"': '"', '\\': '\\', '/': '/', 'n': '\n', 't': '\t', 'r': '\r', 'b': '\b', 'f': '\f'}

// escape decodes the escape sequence at the current position into out.
func (d *jsonDecoder) escape(out []byte) ([]byte, error) {
	for len(d.buf)-d.pos < 2 {
		if _, ok := d.fill(d.pos); !ok {
			return nil, d.errorf("unterminated escape")
		}
	}
	e := d.buf[d.pos+1]
	d.pos += 2
	if b, ok := simpleEscapes[e]; ok {
		return append(out, b), nil
	}
	if e != 'u' {
		return nil, d.errorf("invalid escape \\%c", e)
	}
	r, err := d.unicodeEscape()
	if err != nil {
		return nil, err
	}
	return utf8.AppendRune(out, r), nil
}

// unicodeEscape reads the hex digits of a unicode escape, combining a
// following low surrogate when present.
func (d *jsonDecoder) unicodeEscape() (rune, error) {
	r, err := d.hex4()
	if err != nil || !utf16.IsSurrogate(r) {
		return r, err
	}
	if len(d.buf)-d.pos < 6 || d.buf[d.pos] != '\\' || d.buf[d.pos+1] != 'u' {
		return r, nil
	}
	d.pos += 2
	r2, err := d.hex4()
	if err != nil {
		return 0, err
	}
	return utf16.DecodeRune(r, r2), nil
}

func (d *jsonDecoder) hex4() (rune, error) {
	for len(d.buf)-d.pos < 4 {
		if _, ok := d.fill(d.pos); !ok {
			return 0, d.errorf("invalid unicode escape")
		}
	}
	var r rune
	for _, c := range d.buf[d.pos : d.pos+4] {
		v := hexValue(c)
		if v < 0 {
			return 0, d.errorf("invalid unicode escape")
		}
		r = r<<4 | v
	}
	d.pos += 4
	return r, nil
}

func hexValue(c byte) rune {
	switch {
	case c >= '0' && c <= '9':
		return rune(c - '0')
	case c >= 'a' && c <= 'f':
		return rune(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return rune(c-'A') + 10
	}
	return -1
}

func (d *jsonDecoder) str() (string, error) {
	b, err := d.strBytes()
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (d *jsonDecoder) key() (string, error) {
	b, err := d.strBytes()
	if err != nil {
		return "", err
	}
	if k, ok := d.keys[string(b)]; ok {
		return k, nil
	}
	k := string(b)
	if len(d.keys) < maxInternedKey {
		d.keys[k] = k
	}
	return k, nil
}

// next consumes the separator after a member and reports whether the
// container ended with closer.
func (d *jsonDecoder) next(closer byte, what string) (bool, error) {
	c, ok := d.skipSpace()
	if !ok {
		return false, d.errorf("unterminated %s", what)
	}
	d.pos++
	switch c {
	case closer:
		return true, nil
	case ',':
		return false, nil
	}
	return false, d.errorf("expected ',' or '%c' in %s", closer, what)
}

// expect consumes byte c after optional whitespace.
func (d *jsonDecoder) expect(c byte, msg string) error {
	if got, ok := d.skipSpace(); !ok || got != c {
		return d.errorf("%s", msg)
	}
	d.pos++
	return nil
}

// keyTracker compares an object's keys with the previous object at the same
// depth so that repeated record layouts share one shape.
type keyTracker struct {
	prev     *value.Shape
	keys     []string
	matching bool
}

func (t *keyTracker) add(i int, k string) {
	if t.matching && (i >= len(t.prev.Keys) || t.prev.Keys[i] != k) {
		t.matching = false
		t.keys = append(make([]string, 0, i+4), t.prev.Keys[:i]...)
	}
	if !t.matching {
		t.keys = append(t.keys, k)
	}
}

// shape returns the shape for n fields and whether it is new.
func (t *keyTracker) shape(n int) (*value.Shape, bool) {
	if !t.matching {
		return value.NewShape(t.keys), true
	}
	if n == len(t.prev.Keys) {
		return t.prev, false
	}
	return value.NewShape(append([]string(nil), t.prev.Keys[:n]...)), true
}

func (d *jsonDecoder) object(depth int) (value.Value, error) {
	if c, ok := d.skipSpace(); ok && c == '}' {
		d.pos++
		return value.EmptyObject(), nil
	}
	prev := d.shapeAt(depth)
	track := keyTracker{prev: prev, matching: prev != nil}
	vals := make([]value.Value, 0, shapeLen(prev))
	for {
		k, v, err := d.member(depth)
		if err != nil {
			return value.Null, err
		}
		track.add(len(vals), k)
		vals = append(vals, v)
		done, err := d.next('}', "object")
		if err != nil {
			return value.Null, err
		}
		if done {
			break
		}
	}
	shape, fresh := track.shape(len(vals))
	if fresh {
		d.setShape(depth, shape)
	}
	return value.ObjectValue(value.NewShaped(shape, vals)), nil
}

// member reads one "key": value pair of an object.
func (d *jsonDecoder) member(depth int) (string, value.Value, error) {
	if err := d.expect('"', "expected object key"); err != nil {
		return "", value.Null, err
	}
	k, err := d.key()
	if err != nil {
		return "", value.Null, err
	}
	if err := d.expect(':', "expected ':' after object key"); err != nil {
		return "", value.Null, err
	}
	v, err := d.value(depth + 1)
	return k, v, err
}

// shapeAt returns the key shape of the last object read at depth.
func (d *jsonDecoder) shapeAt(depth int) *value.Shape {
	if depth < len(d.shapes) {
		return d.shapes[depth]
	}
	return nil
}

func (d *jsonDecoder) setShape(depth int, s *value.Shape) {
	if depth >= maxShapeDepth {
		return
	}
	for len(d.shapes) <= depth {
		d.shapes = append(d.shapes, nil)
	}
	d.shapes[depth] = s
}

func (d *jsonDecoder) array(depth int) (value.Value, error) {
	var items []value.Value
	err := d.elements(depth, nil, func(v value.Value) error {
		items = append(items, v)
		return nil
	})
	if err != nil {
		return value.Null, err
	}
	return value.NewArray(items), nil
}

// elements reads array elements after the opening bracket, calling yield
// for each one and checking ctx periodically when it is not nil.
func (d *jsonDecoder) elements(depth int, ctx context.Context, yield func(value.Value) error) error {
	if c, ok := d.skipSpace(); ok && c == ']' {
		d.pos++
		return nil
	}
	for n := 1; ; n++ {
		if err := checkContext(ctx, n); err != nil {
			return err
		}
		v, err := d.value(depth + 1)
		if err != nil {
			return err
		}
		if err := yield(v); err != nil {
			return err
		}
		done, err := d.next(']', "array")
		if err != nil || done {
			return err
		}
	}
}

func (d *jsonDecoder) expectEnd() error {
	if _, ok := d.skipSpace(); ok {
		return d.errorf("unexpected data after JSON value")
	}
	return d.err
}

// ReadJSON decodes a JSON document. With opts.Stream, a top-level array is
// returned as a lazy stream of its elements.
func ReadJSON(src Source, opts ReadOptions) (value.Value, error) {
	r, err := src.Open()
	if err != nil {
		return value.Null, err
	}
	if !opts.Stream {
		b, err := io.ReadAll(r)
		if err != nil {
			return value.Null, err
		}
		return parseDocument(newJSONBytesDecoder(b), true)
	}
	d := newJSONDecoder(r)
	d.skipBOM()
	c, ok := d.skipSpace()
	if !ok {
		return value.Null, d.err
	}
	if c != '[' {
		return parseDocument(d, false)
	}
	return value.NewStream(tagReadErrors(jsonArrayStream(src, d, opts.ctx())), src.Restartable), nil
}

// parseDocument reads one JSON value followed by the end of input. Empty
// input yields null when allowEmpty is set.
func parseDocument(d *jsonDecoder, allowEmpty bool) (value.Value, error) {
	d.skipBOM()
	if _, ok := d.skipSpace(); !ok && allowEmpty {
		return value.Null, d.err
	}
	v, err := d.value(0)
	if err != nil {
		return value.Null, err
	}
	return v, d.expectEnd()
}

// jsonArrayStream iterates the elements of a top-level array. The first pass
// reuses the decoder that detected the array; later passes reopen src.
func jsonArrayStream(src Source, first *jsonDecoder, ctx context.Context) value.IterFunc {
	return func(yield func(value.Value) error) error {
		dec := first
		first = nil
		if dec == nil {
			var err error
			if dec, err = openJSONArray(src); err != nil {
				return err
			}
		}
		dec.pos++
		if err := dec.elements(0, ctx, yield); err != nil {
			return err
		}
		return dec.expectEnd()
	}
}

func openJSONArray(src Source) (*jsonDecoder, error) {
	r, err := src.Open()
	if err != nil {
		return nil, err
	}
	dec := newJSONDecoder(r)
	dec.skipBOM()
	if c, ok := dec.skipSpace(); !ok || c != '[' {
		return nil, dec.errorf("expected a JSON array")
	}
	return dec, nil
}

// ParseJSON decodes an in-memory JSON document.
func ParseJSON(b []byte) (value.Value, error) {
	return parseDocument(newJSONBytesDecoder(b), false)
}

func shapeLen(s *value.Shape) int {
	if s == nil {
		return 4
	}
	return len(s.Keys)
}
