// Package codec reads and writes runtime values in data formats such as JSON,
// CSV, NDJSON, XML and YAML.
//
// Readers return lazy arrays for record-oriented inputs whenever streaming is
// requested, so large files are processed one record at a time. Writers
// consume lazy arrays element by element and never buffer a whole document.
package codec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/touno-io/go-tsunami-cli/internal/value"
)

// Canonical media types.
const (
	JSON   = "application/json"
	NDJSON = "application/x-ndjson"
	CSV    = "application/csv"
	XML    = "application/xml"
	YAML   = "application/yaml"
	Text   = "text/plain"
	DW     = "application/dw"
	Java   = "application/java"
)

// Source is an input document. Open returns a fresh reader positioned at the
// start of the document; it is called again when a stream is re-iterated, so
// it may return an error for one-shot sources such as standard input.
type Source struct {
	Open func() (io.Reader, error)
	// Restartable reports whether Open can be called more than once.
	Restartable bool
}

// BytesSource returns a restartable source over b.
func BytesSource(b []byte) Source {
	return Source{
		Open:        func() (io.Reader, error) { return strings.NewReader(string(b)), nil },
		Restartable: true,
	}
}

// StringSource returns a restartable source over s.
func StringSource(s string) Source {
	return Source{
		Open:        func() (io.Reader, error) { return strings.NewReader(s), nil },
		Restartable: true,
	}
}

// ReaderSource returns a one-shot source over r.
func ReaderSource(r io.Reader) Source {
	used := false
	return Source{Open: func() (io.Reader, error) {
		if used {
			return nil, value.ErrConsumed
		}
		used = true
		return r, nil
	}}
}

// ReadOptions configures a reader.
type ReadOptions struct {
	Props   map[string]string
	Context context.Context
	// Stream asks record-oriented readers to return a lazy array.
	Stream bool
}

func (o ReadOptions) ctx() context.Context {
	if o.Context == nil {
		return context.Background()
	}
	return o.Context
}

// ReadFunc decodes a source into a value.
type ReadFunc func(src Source, opts ReadOptions) (value.Value, error)

// WriteFunc encodes a value.
type WriteFunc func(w io.Writer, v value.Value, props map[string]string) error

// Format describes a data format.
type Format struct {
	Mime  string
	Read  ReadFunc
	Write WriteFunc
}

var formats = map[string]*Format{}

var aliases = map[string]string{
	"json":                      JSON,
	"text/json":                 JSON,
	"application/x-json":        JSON,
	"ndjson":                    NDJSON,
	"jsonl":                     NDJSON,
	"application/ndjson":        NDJSON,
	"application/jsonl":         NDJSON,
	"application/x-jsonlines":   NDJSON,
	"application/jsonlines":     NDJSON,
	"csv":                       CSV,
	"text/csv":                  CSV,
	"tsv":                       CSV,
	"text/tab-separated-values": CSV,
	"xml":                       XML,
	"text/xml":                  XML,
	"yaml":                      YAML,
	"yml":                       YAML,
	"text/yaml":                 YAML,
	"application/x-yaml":        YAML,
	"text":                      Text,
	"plain":                     Text,
	"txt":                       Text,
	"dw":                        DW,
	"dwl":                       DW,
	"application/dwl":           DW,
	"java":                      Java,
}

// Register adds or replaces a format. It is intended for package
// initialization.
func Register(f *Format) {
	formats[f.Mime] = f
}

// Normalize maps aliases and parameters of a media type to its canonical
// form, for example "csv" or "text/csv; charset=utf-8" to "application/csv".
func Normalize(mime string) string {
	m := strings.ToLower(strings.TrimSpace(mime))
	if i := strings.IndexByte(m, ';'); i >= 0 {
		m = strings.TrimSpace(m[:i])
	}
	if a, ok := aliases[m]; ok {
		return a
	}
	if strings.HasSuffix(m, "+json") {
		return JSON
	}
	if strings.HasSuffix(m, "+xml") {
		return XML
	}
	return m
}

// DefaultProps returns implied properties for a media type alias, such as a
// tab separator for "tsv".
func DefaultProps(mime string) map[string]string {
	m := strings.ToLower(strings.TrimSpace(mime))
	if m == "tsv" || m == "text/tab-separated-values" {
		return map[string]string{"separator": "\t"}
	}
	return nil
}

// Lookup returns the format for a media type.
func Lookup(mime string) (*Format, error) {
	if f, ok := formats[Normalize(mime)]; ok {
		return f, nil
	}
	return nil, fmt.Errorf("unsupported media type %q (supported: %s)", mime, strings.Join(Supported(), ", "))
}

// Supported lists the registered media types.
func Supported() []string {
	out := make([]string, 0, len(formats))
	for m := range formats {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// MimeForPath guesses a media type from a file extension.
func MimeForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return JSON
	case ".ndjson", ".jsonl":
		return NDJSON
	case ".csv":
		return CSV
	case ".tsv", ".tab":
		return "tsv"
	case ".xml":
		return XML
	case ".yaml", ".yml":
		return YAML
	case ".dwl", ".dw":
		return DW
	}
	return Text
}

// Extension returns a conventional file extension for a media type.
func Extension(mime string) string {
	switch Normalize(mime) {
	case JSON, Java:
		return ".json"
	case NDJSON:
		return ".ndjson"
	case CSV:
		return ".csv"
	case XML:
		return ".xml"
	case YAML:
		return ".yaml"
	case DW:
		return ".dwl"
	}
	return ".txt"
}

func boolProp(props map[string]string, key string, def bool) bool {
	v, ok := props[key]
	if !ok {
		return def
	}
	b, err := strconv.ParseBool(strings.TrimSpace(v))
	if err != nil {
		return def
	}
	return b
}

func stringProp(props map[string]string, key, def string) string {
	if v, ok := props[key]; ok {
		return v
	}
	return def
}

// unescapeProp decodes escapes such as \t used in directive properties.
func unescapeProp(s string) string {
	r := strings.NewReplacer(`\t`, "\t", `\n`, "\n", `\r`, "\r", `\\`, `\`)
	return r.Replace(s)
}

// scalarText renders a scalar for text-based formats.
func scalarText(v value.Value) (string, error) {
	switch v.K {
	case value.KindArray, value.KindObject, value.KindFunction:
		return "", fmt.Errorf("cannot write %s as a scalar value", v.TypeName())
	}
	return value.ToString(v)
}

// eachElement iterates an array value, or treats any other value as a single
// element.
func eachElement(v value.Value, fn func(i int, v value.Value) error) error {
	switch v.K {
	case value.KindArray:
		return v.Array().Each(fn)
	case value.KindRange:
		from, to := v.RangeBounds()
		step := 1
		if to < from {
			step = -1
		}
		for i, n := 0, from; ; i, n = i+1, n+step {
			if err := fn(i, value.Int(n)); err != nil {
				if err == value.ErrStop {
					return nil
				}
				return err
			}
			if n == to {
				return nil
			}
		}
	case value.KindNull:
		return nil
	}
	return fn(0, v)
}

func init() {
	Register(&Format{Mime: JSON, Read: ReadJSON, Write: WriteJSON})
	Register(&Format{Mime: Java, Read: ReadJSON, Write: WriteJSON})
	Register(&Format{Mime: NDJSON, Read: ReadNDJSON, Write: WriteNDJSON})
	Register(&Format{Mime: CSV, Read: ReadCSV, Write: WriteCSV})
	Register(&Format{Mime: XML, Read: ReadXML, Write: WriteXML})
	Register(&Format{Mime: YAML, Read: nil, Write: WriteYAML})
	Register(&Format{Mime: Text, Read: ReadText, Write: WriteText})
	Register(&Format{Mime: DW, Read: nil, Write: WriteDW})
}

// ReadError reports malformed input found while a lazy stream is consumed,
// which may happen long after the reader returned.
type ReadError struct {
	Err error
}

func (e *ReadError) Error() string { return e.Err.Error() }

// Unwrap returns the underlying error.
func (e *ReadError) Unwrap() error { return e.Err }

// tagReadErrors marks errors raised by a stream's reader as ReadErrors while
// passing errors returned by the consumer through unchanged.
func tagReadErrors(iter value.IterFunc) value.IterFunc {
	return func(yield func(value.Value) error) error {
		var consumerErr error
		err := iter(func(v value.Value) error {
			if e := yield(v); e != nil {
				consumerErr = e
				return e
			}
			return nil
		})
		if err == nil || err == consumerErr || err == value.ErrConsumed ||
			errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return &ReadError{Err: err}
	}
}

// checkContext reports cancellation every ctxCheckEvery records. A nil ctx
// is never cancelled.
func checkContext(ctx context.Context, n int) error {
	if ctx == nil || n%ctxCheckEvery != 0 {
		return nil
	}
	return ctx.Err()
}
