package codec

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/touno-io/go-tsunami-cli/internal/value"
)

type csvConfig struct {
	sep      byte
	quote    byte
	escape   byte
	header   bool
	quoteAll bool
	quoteHdr bool
	newline  string
}

func csvConfigFrom(props map[string]string) (csvConfig, error) {
	cfg := csvConfig{sep: ',', quote: '"', header: true, newline: "\n"}
	if s, ok := props["separator"]; ok {
		s = unescapeProp(s)
		if len(s) != 1 {
			return cfg, fmt.Errorf("csv separator must be a single byte, got %q", s)
		}
		cfg.sep = s[0]
	}
	if s, ok := props["quote"]; ok {
		s = unescapeProp(s)
		if len(s) != 1 {
			return cfg, fmt.Errorf("csv quote must be a single byte, got %q", s)
		}
		cfg.quote = s[0]
	}
	cfg.escape = cfg.quote
	if s, ok := props["escape"]; ok {
		s = unescapeProp(s)
		if len(s) != 1 {
			return cfg, fmt.Errorf("csv escape must be a single byte, got %q", s)
		}
		cfg.escape = s[0]
	}
	cfg.header = boolProp(props, "header", true)
	cfg.quoteAll = boolProp(props, "quoteValues", false)
	cfg.quoteHdr = boolProp(props, "quoteHeader", false)
	cfg.newline = unescapeProp(stringProp(props, "lineSeparator", "\n"))
	return cfg, nil
}

// ReadCSV decodes CSV into an array of objects whose values are strings.
// Properties: separator, quote, escape and header (default true; without a
// header the keys are column_0, column_1, ...).
func ReadCSV(src Source, opts ReadOptions) (value.Value, error) {
	cfg, err := csvConfigFrom(opts.Props)
	if err != nil {
		return value.Null, err
	}
	stream := value.NewStream(tagReadErrors(csvStream(src, cfg, opts.ctx())), src.Restartable)
	if opts.Stream {
		return stream, nil
	}
	if _, err := stream.Array().Items(); err != nil {
		return value.Null, err
	}
	return stream, nil
}

func csvStream(src Source, cfg csvConfig, ctx context.Context) value.IterFunc {
	return func(yield func(value.Value) error) error {
		r, err := src.Open()
		if err != nil {
			return err
		}
		cr := newCSVReader(r, cfg)
		shape, err := cr.header()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		return cr.rows(shape, ctx, yield)
	}
}

// header reads the header record when the configuration has one.
func (c *csvReader) header() (*value.Shape, error) {
	if !c.cfg.header {
		return nil, nil
	}
	rec, err := c.record()
	if err != nil {
		return nil, err
	}
	hdr := rec.fields()
	if len(hdr) > 0 {
		hdr[0] = strings.TrimPrefix(hdr[0], "\xef\xbb\xbf")
	}
	return value.NewShape(hdr), nil
}

// rows yields every remaining record as an object keyed by shape.
func (c *csvReader) rows(shape *value.Shape, ctx context.Context, yield func(value.Value) error) error {
	for n := 1; ; n++ {
		if n%ctxCheckEvery == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		rec, err := c.record()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if shape == nil || len(rec.ends) > len(shape.Keys) {
			shape = widenShape(shape, len(rec.ends))
		}
		if err := yield(value.ObjectValue(value.NewPackedRow(shape, rec.row, rec.ends))); err != nil {
			return err
		}
	}
}

// widenShape extends a shape with generated column names.
func widenShape(s *value.Shape, n int) *value.Shape {
	var keys []string
	if s != nil {
		keys = append(keys, s.Keys...)
	}
	for i := len(keys); i < n; i++ {
		keys = append(keys, "column_"+strconv.Itoa(i))
	}
	return value.NewShape(keys)
}

// csvReader is a small RFC 4180 reader tuned for throughput: unquoted fields
// are sliced from the line buffer and every record's fields share a single
// string allocation.
type csvReader struct {
	r     io.Reader
	buf   []byte
	pos   int
	eof   bool
	err   error
	cfg   csvConfig
	field []byte
	ends  []uint32
	line  int
}

func newCSVReader(r io.Reader, cfg csvConfig) *csvReader {
	return &csvReader{r: r, buf: make([]byte, 0, readChunk), cfg: cfg}
}

// fill reads more data, discarding consumed bytes.
func (c *csvReader) fill() bool {
	if c.eof {
		return false
	}
	if c.pos > 0 {
		n := copy(c.buf, c.buf[c.pos:])
		c.buf = c.buf[:n]
		c.pos = 0
	}
	if len(c.buf) == cap(c.buf) {
		nb := make([]byte, len(c.buf), 2*cap(c.buf))
		copy(nb, c.buf)
		c.buf = nb
	}
	for {
		n, err := c.r.Read(c.buf[len(c.buf):cap(c.buf)])
		c.buf = c.buf[:len(c.buf)+n]
		if err != nil {
			c.eof = true
			if err != io.EOF {
				c.err = err
			}
			return n > 0
		}
		if n > 0 {
			return true
		}
	}
}

// more reports whether at least n unread bytes are buffered, reading more
// input when needed.
func (c *csvReader) more(n int) bool {
	for len(c.buf)-c.pos < n {
		if !c.fill() {
			return false
		}
	}
	return true
}

// record reads the next non-empty record.
func (c *csvReader) record() (csvRecord, error) {
	for {
		rec, empty, err := c.readRecord()
		if err != nil {
			return csvRecord{}, err
		}
		if !empty {
			return rec, nil
		}
	}
}

func (c *csvReader) readRecord() (csvRecord, bool, error) {
	c.field = c.field[:0]
	c.ends = c.ends[:0]
	c.line++
	if !c.more(1) {
		if c.err != nil {
			return csvRecord{}, false, c.err
		}
		return csvRecord{}, false, io.EOF
	}
	for {
		if err := c.readField(); err != nil {
			return csvRecord{}, false, err
		}
		c.ends = append(c.ends, uint32(len(c.field)))
		done, err := c.endOfField()
		if err != nil {
			return csvRecord{}, false, err
		}
		if done {
			return c.finish()
		}
	}
}

// readField reads one quoted or unquoted field.
func (c *csvReader) readField() error {
	if !c.more(1) {
		return nil
	}
	if c.buf[c.pos] == c.cfg.quote {
		c.pos++
		return c.quotedField()
	}
	c.plainField()
	return nil
}

// endOfField consumes the separator or line ending after a field and
// reports whether the record ended.
func (c *csvReader) endOfField() (bool, error) {
	if !c.more(1) {
		return true, nil
	}
	switch c.buf[c.pos] {
	case c.cfg.sep:
		c.pos++
		return false, nil
	case '\r':
		c.pos++
		if c.more(1) && c.buf[c.pos] == '\n' {
			c.pos++
		}
		return true, nil
	case '\n':
		c.pos++
		return true, nil
	}
	return false, fmt.Errorf("csv line %d: unexpected character after quoted field", c.line)
}

// plainField copies an unquoted field up to the separator or line end.
func (c *csvReader) plainField() {
	sep := c.cfg.sep
	for {
		start := c.pos
		for c.pos < len(c.buf) {
			b := c.buf[c.pos]
			if b == sep || b == '\n' || b == '\r' {
				c.field = append(c.field, c.buf[start:c.pos]...)
				return
			}
			c.pos++
		}
		c.field = append(c.field, c.buf[start:c.pos]...)
		if !c.fill() {
			return
		}
	}
}

// quotedField copies a quoted field after its opening quote.
func (c *csvReader) quotedField() error {
	quote, escape := c.cfg.quote, c.cfg.escape
	for {
		if !c.more(1) {
			return fmt.Errorf("csv line %d: unterminated quoted field", c.line)
		}
		switch b := c.buf[c.pos]; {
		case b == escape && escape != quote:
			c.escapedByte()
		case b == quote:
			if !c.doubledQuote() {
				c.pos++
				return nil
			}
		default:
			c.quotedRun()
		}
	}
}

// escapedByte copies the byte after an escape character.
func (c *csvReader) escapedByte() {
	if c.more(2) {
		c.field = append(c.field, c.buf[c.pos+1])
		c.pos += 2
		return
	}
	c.pos++
}

// doubledQuote consumes "" inside a quoted field as a literal quote.
func (c *csvReader) doubledQuote() bool {
	if c.more(2) && c.buf[c.pos+1] == c.cfg.quote {
		c.field = append(c.field, c.cfg.quote)
		c.pos += 2
		return true
	}
	return false
}

// quotedRun copies bytes up to the next quote or escape character.
func (c *csvReader) quotedRun() {
	quote, escape := c.cfg.quote, c.cfg.escape
	start := c.pos
	for c.pos < len(c.buf) && c.buf[c.pos] != quote && c.buf[c.pos] != escape {
		if c.buf[c.pos] == '\n' {
			c.line++
		}
		c.pos++
	}
	c.field = append(c.field, c.buf[start:c.pos]...)
}

func (c *csvReader) finish() (csvRecord, bool, error) {
	if len(c.ends) == 1 && c.ends[0] == 0 {
		return csvRecord{}, true, nil
	}
	return csvRecord{row: string(c.field), ends: append([]uint32(nil), c.ends...)}, false, nil
}

// csvRecord is one record packed into a single string with field end offsets.
type csvRecord struct {
	row  string
	ends []uint32
}

// field returns field i.
func (r csvRecord) field(i int) string {
	start := uint32(0)
	if i > 0 {
		start = r.ends[i-1]
	}
	return r.row[start:r.ends[i]]
}

// fields unpacks every field.
func (r csvRecord) fields() []string {
	out := make([]string, len(r.ends))
	for i := range out {
		out[i] = r.field(i)
	}
	return out
}

// WriteCSV writes an array of objects as CSV, streaming one row at a time.
// Properties: separator, quote, escape, header, quoteValues, quoteHeader and
// lineSeparator.
func WriteCSV(w io.Writer, v value.Value, props map[string]string) error {
	cfg, err := csvConfigFrom(props)
	if err != nil {
		return err
	}
	cw := &csvWriter{out: newSink(w), cfg: cfg}
	if err := eachElement(v, cw.row); err != nil {
		_ = cw.out.flush()
		return err
	}
	return cw.out.flush()
}

// csvWriter writes rows whose columns are fixed by the first row.
type csvWriter struct {
	out    sink
	cfg    csvConfig
	header []string
	shape  *value.Shape
}

func (cw *csvWriter) row(_ int, row value.Value) error {
	obj := row.Object()
	if obj == nil {
		if row.IsNull() {
			return nil
		}
		return fmt.Errorf("csv rows must be objects, got %s", row.TypeName())
	}
	if cw.header == nil {
		cw.start(obj)
	}
	return writeCSVRow(cw.out, obj, cw.header, cw.shape, cw.cfg)
}

// start takes the columns from the first row and writes the header line.
func (cw *csvWriter) start(obj *value.Object) {
	cw.header = make([]string, obj.Len())
	for k := range cw.header {
		cw.header[k] = obj.Key(k)
	}
	cw.shape = obj.Shape()
	if !cw.cfg.header {
		return
	}
	for k, h := range cw.header {
		if k > 0 {
			cw.out.put(cw.cfg.sep)
		}
		writeCSVField(cw.out, h, cw.cfg, cw.cfg.quoteHdr)
	}
	cw.out.puts(cw.cfg.newline)
}

func writeCSVRow(out sink, obj *value.Object, header []string, shape *value.Shape, cfg csvConfig) error {
	positional := (shape != nil && obj.Shape() == shape && obj.Len() == len(header)) || sameKeys(obj, header)
	for k, h := range header {
		if k > 0 {
			out.put(cfg.sep)
		}
		var fv value.Value
		if positional {
			fv = obj.Val(k)
		} else {
			fv, _ = obj.Get(h)
		}
		s, err := scalarText(fv)
		if err != nil {
			return fmt.Errorf("csv column %q: %w", h, err)
		}
		writeCSVField(out, s, cfg, cfg.quoteAll)
	}
	out.puts(cfg.newline)
	return nil
}

func sameKeys(obj *value.Object, header []string) bool {
	if obj.Len() != len(header) {
		return false
	}
	for i, h := range header {
		if obj.Key(i) != h {
			return false
		}
	}
	return true
}

func writeCSVField(out sink, s string, cfg csvConfig, force bool) {
	needs := force
	if !needs {
		for i := 0; i < len(s); i++ {
			b := s[i]
			if b == cfg.sep || b == cfg.quote || b == '\n' || b == '\r' || (b == cfg.escape && cfg.escape != cfg.quote) {
				needs = true
				break
			}
		}
	}
	if !needs {
		out.puts(s)
		return
	}
	out.put(cfg.quote)
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == cfg.quote || (s[i] == cfg.escape && cfg.escape != cfg.quote) {
			out.puts(s[start:i])
			out.put(cfg.escape)
			out.put(s[i])
			start = i + 1
		}
	}
	out.puts(s[start:])
	out.put(cfg.quote)
}

// ReadText returns the whole document as a string.
func ReadText(src Source, _ ReadOptions) (value.Value, error) {
	r, err := src.Open()
	if err != nil {
		return value.Null, err
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return value.Null, err
	}
	if !utf8.Valid(b) {
		return value.Null, fmt.Errorf("text input is not valid UTF-8")
	}
	return value.Str(string(b)), nil
}

// WriteText writes a scalar as plain text. Arrays of scalars are written one
// per line.
func WriteText(w io.Writer, v value.Value, _ map[string]string) error {
	out := newSink(w)
	if v.K == value.KindArray {
		err := eachElement(v, func(i int, el value.Value) error {
			s, err := scalarText(el)
			if err != nil {
				return fmt.Errorf("cannot write %s as text/plain", v.TypeName())
			}
			if i > 0 {
				out.put('\n')
			}
			out.puts(s)
			return nil
		})
		if err != nil {
			return err
		}
		return out.flush()
	}
	s, err := scalarText(v)
	if err != nil {
		return fmt.Errorf("cannot write %s as text/plain", v.TypeName())
	}
	out.puts(s)
	return out.flush()
}
