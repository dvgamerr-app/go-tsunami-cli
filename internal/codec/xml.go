package codec

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"github.com/touno-io/go-tsunami-cli/internal/value"
)

// ReadXML decodes an XML document into nested objects. Elements with child
// elements become objects (repeated children become repeated keys), text-only
// elements become strings and empty elements become null. Attributes are
// ignored.
func ReadXML(src Source, _ ReadOptions) (value.Value, error) {
	r, err := src.Open()
	if err != nil {
		return value.Null, err
	}
	dec := xml.NewDecoder(r)
	dec.Strict = false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return value.Null, nil
		}
		if err != nil {
			return value.Null, err
		}
		if start, ok := tok.(xml.StartElement); ok {
			v, err := readXMLElement(dec)
			if err != nil {
				return value.Null, err
			}
			b := value.NewBuilder(1)
			b.Add(start.Name.Local, v)
			return b.Build(), nil
		}
	}
}

func readXMLElement(dec *xml.Decoder) (value.Value, error) {
	el := &xmlElement{}
	for {
		tok, err := dec.Token()
		if err != nil {
			return value.Null, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if err := el.child(dec, t.Name.Local); err != nil {
				return value.Null, err
			}
		case xml.CharData:
			el.text.Write(t)
		case xml.EndElement:
			return el.value(), nil
		}
	}
}

// xmlElement accumulates the content of an element being read.
type xmlElement struct {
	children *value.Builder
	text     strings.Builder
}

func (e *xmlElement) child(dec *xml.Decoder, name string) error {
	v, err := readXMLElement(dec)
	if err != nil {
		return err
	}
	if e.children == nil {
		e.children = value.NewBuilder(4)
	}
	e.children.Add(name, v)
	return nil
}

// value returns the children as an object, the trimmed text, or null for
// an empty element.
func (e *xmlElement) value() value.Value {
	if e.children != nil {
		return e.children.Build()
	}
	if e.text.Len() == 0 {
		return value.Null
	}
	return value.Str(strings.TrimSpace(e.text.String()))
}

// WriteXML writes an object with a single root key as XML. Array values are
// written as repeated elements. Properties: indent (default true) and
// skipNullOn.
func WriteXML(w io.Writer, v value.Value, props map[string]string) error {
	obj := v.Object()
	if obj == nil || obj.Len() != 1 {
		return fmt.Errorf("xml output requires an object with exactly one root key")
	}
	x := &xmlWriter{out: newSink(w), indent: boolProp(props, "indent", true), skip: parseSkipNull(props)}
	x.out.puts(`<?xml version='1.0' encoding='UTF-8'?>`)
	if err := x.element(obj.Key(0), obj.Val(0), 0); err != nil {
		_ = x.out.flush()
		return err
	}
	x.out.put('\n')
	return x.out.flush()
}

type xmlWriter struct {
	out    sink
	indent bool
	skip   skipNull
}

func (x *xmlWriter) newline(depth int) {
	if !x.indent {
		return
	}
	x.out.put('\n')
	x.out.puts(spaces[:min(2*depth, len(spaces))])
}

func (x *xmlWriter) element(name string, v value.Value, depth int) error {
	name = xmlName(name)
	switch v.K {
	case value.KindArray:
		return x.repeated(name, v.Array(), depth)
	case value.KindNull:
		if !x.skip.objects {
			x.newline(depth)
			x.out.puts("<" + name + "/>")
		}
		return nil
	case value.KindObject:
		return x.children(name, v.Object(), depth)
	}
	s, err := scalarText(v)
	if err != nil {
		return err
	}
	x.newline(depth)
	x.out.puts("<" + name + ">")
	x.out.puts(xmlEscape(s))
	x.out.puts("</" + name + ">")
	return nil
}

// repeated writes one element per array item.
func (x *xmlWriter) repeated(name string, a *value.Array, depth int) error {
	return a.Each(func(_ int, el value.Value) error {
		if el.IsNull() && x.skip.arrays {
			return nil
		}
		return x.element(name, el, depth)
	})
}

// children writes an element whose content is the fields of obj.
func (x *xmlWriter) children(name string, obj *value.Object, depth int) error {
	x.newline(depth)
	x.out.puts("<" + name + ">")
	for i, n := 0, obj.Len(); i < n; i++ {
		if err := x.element(obj.Key(i), obj.Val(i), depth+1); err != nil {
			return err
		}
	}
	if obj.Len() > 0 {
		x.newline(depth)
	}
	x.out.puts("</" + name + ">")
	return nil
}

var xmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")

func xmlEscape(s string) string {
	if !strings.ContainsAny(s, `&<>"'`) {
		return s
	}
	return xmlEscaper.Replace(s)
}

// xmlName replaces characters that are not valid in XML element names.
func xmlName(s string) string {
	if s == "" {
		return "_"
	}
	b := []byte(s)
	for i, c := range b {
		valid := c == '_' || c == '-' || c == '.' || c == ':' || c >= 0x80 ||
			(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9' && i > 0)
		if !valid {
			b[i] = '_'
		}
	}
	return string(b)
}
