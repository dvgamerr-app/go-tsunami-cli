package value

// indexThreshold is the field count above which an object builds a lookup
// index instead of scanning its keys.
const indexThreshold = 12

// Shape is an ordered key set shared by many objects, such as the header of a
// CSV file or the static keys of an object literal. Sharing the shape avoids
// allocating keys and a lookup index for every object.
type Shape struct {
	Keys  []string
	index map[string]int
}

// NewShape returns a shape for keys with a prebuilt lookup index, so it can
// be shared between goroutines. The first occurrence of a duplicated key wins
// on lookup.
func NewShape(keys []string) *Shape {
	s := &Shape{Keys: keys}
	if len(keys) > indexThreshold {
		s.buildIndex()
	}
	return s
}

func (s *Shape) buildIndex() {
	s.index = make(map[string]int, len(s.Keys))
	for i := len(s.Keys) - 1; i >= 0; i-- {
		s.index[s.Keys[i]] = i
	}
}

// indexOf returns the first position of key among the first n keys.
func (s *Shape) indexOf(key string, n int) int {
	if s.index == nil && len(s.Keys) > indexThreshold {
		s.buildIndex()
	}
	if s.index != nil {
		if i, ok := s.index[key]; ok && i < n {
			return i
		}
		return -1
	}
	for i, k := range s.Keys[:n] {
		if k == key {
			return i
		}
	}
	return -1
}

// Field is a single key-value pair of an object.
type Field struct {
	Key string
	Val Value
}

// Object is an ordered collection of fields. Keys may repeat.
//
// Values are stored either as Values or, for rows read from text formats, as
// one packed string with field end offsets, which costs a single small
// allocation per row and no conversion work for fields that are never read.
type Object struct {
	shape *Shape
	vals  []Value
	row   string
	ends  []uint32
}

// NewObject returns an object from parallel key and value slices.
func NewObject(keys []string, vals []Value) *Object {
	return &Object{shape: &Shape{Keys: keys}, vals: vals}
}

// NewShaped returns an object whose keys come from a shared shape.
func NewShaped(s *Shape, vals []Value) *Object {
	return &Object{shape: s, vals: vals}
}

// NewPackedRow returns an object of string values stored back to back in row,
// where field i ends at ends[i]. len(ends) must not exceed len(s.Keys).
func NewPackedRow(s *Shape, row string, ends []uint32) *Object {
	return &Object{shape: s, row: row, ends: ends}
}

// ObjectValue wraps an object.
func ObjectValue(o *Object) Value {
	return Value{K: KindObject, R: o}
}

// EmptyObject returns a new empty object value.
func EmptyObject() Value {
	return ObjectValue(&Object{shape: &Shape{}})
}

// Len returns the number of fields.
func (o *Object) Len() int {
	if o.ends != nil {
		return len(o.ends)
	}
	return len(o.vals)
}

// Key returns the key of field i.
func (o *Object) Key(i int) string { return o.shape.Keys[i] }

// Val returns the value of field i.
func (o *Object) Val(i int) Value {
	if o.ends == nil {
		return o.vals[i]
	}
	start := uint32(0)
	if i > 0 {
		start = o.ends[i-1]
	}
	return Value{K: KindString, S: o.row[start:o.ends[i]]}
}

// Shape returns the key shape of the object.
func (o *Object) Shape() *Shape { return o.shape }

// Get returns the value of the first field named key.
func (o *Object) Get(key string) (Value, bool) {
	i := o.IndexOf(key)
	if i < 0 {
		return Null, false
	}
	return o.Val(i), true
}

// IndexOf returns the index of the first field named key, or -1.
func (o *Object) IndexOf(key string) int {
	return o.shape.indexOf(key, o.Len())
}

// GetAll returns every value whose key is key.
func (o *Object) GetAll(key string) []Value {
	var out []Value
	for i, n := 0, o.Len(); i < n; i++ {
		if o.Key(i) == key {
			out = append(out, o.Val(i))
		}
	}
	return out
}

// Fields returns a copy of the object's fields.
func (o *Object) Fields() []Field {
	n := o.Len()
	out := make([]Field, n)
	for i := 0; i < n; i++ {
		out[i] = Field{Key: o.Key(i), Val: o.Val(i)}
	}
	return out
}

// Builder accumulates fields for a new object.
type Builder struct {
	keys []string
	vals []Value
}

// NewBuilder returns a builder with room for n fields.
func NewBuilder(n int) *Builder {
	return &Builder{keys: make([]string, 0, n), vals: make([]Value, 0, n)}
}

// Add appends a field.
func (b *Builder) Add(k string, v Value) {
	b.keys = append(b.keys, k)
	b.vals = append(b.vals, v)
}

// AddObject appends every field of o.
func (b *Builder) AddObject(o *Object) {
	for i, n := 0, o.Len(); i < n; i++ {
		b.Add(o.Key(i), o.Val(i))
	}
}

// Len returns the number of fields added so far.
func (b *Builder) Len() int { return len(b.keys) }

// Build returns the accumulated object.
func (b *Builder) Build() Value {
	return ObjectValue(NewObject(b.keys, b.vals))
}
