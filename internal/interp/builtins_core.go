package interp

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/touno-io/go-tsunami-cli/internal/codec"
	"github.com/touno-io/go-tsunami-cli/internal/value"
)

func registerCore() {
	register("sizeOf", 1, 1, sizeOf)
	register("isEmpty", 1, 1, func(a []value.Value) (value.Value, error) {
		empty, err := isEmptyValue(a[0])
		return value.Bool(empty), err
	})
	register("isBlank", 1, 1, func(a []value.Value) (value.Value, error) {
		v := a[0]
		return value.Bool(v.IsNull() || v.K == value.KindString && strings.TrimSpace(v.S) == ""), nil
	})
	register("typeOf", 1, 1, func(a []value.Value) (value.Value, error) {
		return value.Type(a[0].TypeName()), nil
	})
	register("++", 2, 2, func(a []value.Value) (value.Value, error) { return concat(a[0], a[1]) })
	register("--", 2, 2, func(a []value.Value) (value.Value, error) { return minus(a[0], a[1]) })
	register("then", 2, 2, then)
	register("uuid", 0, 0, uuid)
	register("now", 0, 0, func([]value.Value) (value.Value, error) {
		return value.Temporal(value.KindDateTime, time.Now().UTC()), nil
	})
	register("envVar", 1, 1, envVar)
	register("envVars", 0, 0, envVars)
	register("fail", 0, 1, fail)
	register("try", 1, 1, try)
	register("orElse", 2, 2, orElse)
	register("orElseTry", 2, 2, orElseTry)
	register("wait", 2, 2, func(a []value.Value) (value.Value, error) { return a[0], nil })
	register("read", 1, 3, read)
	register("write", 1, 3, write)
	registerProgram("readUrl", 1, 3, readURL)
	registerProgram("p", 1, 1, property)
	registerProgram("log", 1, 2, logger)
}

func sizeOf(a []value.Value) (value.Value, error) {
	v := a[0]
	switch v.K {
	case value.KindNull:
		return value.Int(0), nil
	case value.KindString:
		return value.Int(len([]rune(v.S))), nil
	case value.KindObject:
		return value.Int(v.Object().Len()), nil
	case value.KindArray, value.KindRange:
		arr, _ := asIterable(v)
		n, err := arr.Len()
		return value.Int(n), err
	}
	return value.Null, fmt.Errorf("function sizeOf does not accept %s", v.TypeName())
}

func then(a []value.Value) (value.Value, error) {
	if a[0].IsNull() {
		return value.Null, nil
	}
	fn, err := fnArg(a, 1, "then")
	if err != nil {
		return value.Null, err
	}
	return fn.Call([]value.Value{a[0]})
}

func uuid([]value.Value) (value.Value, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return value.Null, err
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return value.Str(fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])), nil
}

func envVar(a []value.Value) (value.Value, error) {
	name, err := strArg(a, 0, "envVar")
	if err != nil {
		return value.Null, err
	}
	if v, ok := os.LookupEnv(name); ok {
		return value.Str(v), nil
	}
	return value.Null, nil
}

func envVars([]value.Value) (value.Value, error) {
	env := os.Environ()
	b := value.NewBuilder(len(env))
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		b.Add(k, value.Str(v))
	}
	return b.Build(), nil
}

func fail(a []value.Value) (value.Value, error) {
	msg, _ := value.ToString(arg(a, 0))
	if msg == "" {
		msg = "failed"
	}
	return value.Null, errors.New(msg)
}

func try(a []value.Value) (value.Value, error) {
	fn, err := fnArg(a, 0, "try")
	if err != nil {
		return value.Null, err
	}
	return tryResult(fn.Call(nil)), nil
}

// trySucceeded reports whether v is a successful try() result.
func trySucceeded(v value.Value) bool {
	res := v.Object()
	if res == nil {
		return false
	}
	ok, _ := res.Get("success")
	return ok.Truthy()
}

func orElse(a []value.Value) (value.Value, error) {
	if trySucceeded(a[0]) {
		r, _ := a[0].Object().Get("result")
		return r, nil
	}
	if fn := a[1].Func(); fn != nil {
		return fn.Call(nil)
	}
	return a[1], nil
}

func orElseTry(a []value.Value) (value.Value, error) {
	if trySucceeded(a[0]) {
		return a[0], nil
	}
	fn, err := fnArg(a, 1, "orElseTry")
	if err != nil {
		return value.Null, err
	}
	return tryResult(fn.Call(nil)), nil
}

// mimeArg returns the optional media type argument at i, defaulting to
// DataWeave literal syntax.
func mimeArg(a []value.Value, i int, name string) (string, error) {
	if len(a) <= i || a[i].IsNull() {
		return codec.DW, nil
	}
	return strArg(a, i, name)
}

func read(a []value.Value) (value.Value, error) {
	if a[0].IsNull() {
		return value.Null, nil
	}
	s, err := strArg(a, 0, "read")
	if err != nil {
		return value.Null, err
	}
	mime, err := mimeArg(a, 1, "read")
	if err != nil {
		return value.Null, err
	}
	return ReadValue(codec.StringSource(s), mime, codec.ReadOptions{Props: propsOf(arg(a, 2), mime)})
}

func write(a []value.Value) (value.Value, error) {
	mime, err := mimeArg(a, 1, "write")
	if err != nil {
		return value.Null, err
	}
	f, err := codec.Lookup(mime)
	if err != nil {
		return value.Null, err
	}
	var buf bytes.Buffer
	if err := f.Write(&buf, a[0], propsOf(arg(a, 2), mime)); err != nil {
		return value.Null, err
	}
	return value.Str(buf.String()), nil
}

func readURL(p *Program) builtinImpl {
	return func(a []value.Value) (value.Value, error) {
		url, err := strArg(a, 0, "readUrl")
		if err != nil {
			return value.Null, err
		}
		path, data, err := p.readResource(url)
		if err != nil {
			return value.Null, err
		}
		mime := codec.MimeForPath(path)
		if len(a) > 1 && !a[1].IsNull() {
			if mime, err = strArg(a, 1, "readUrl"); err != nil {
				return value.Null, err
			}
		}
		return ReadValue(codec.BytesSource(data), mime, codec.ReadOptions{Props: propsOf(arg(a, 2), mime)})
	}
}

func property(p *Program) builtinImpl {
	return func(a []value.Value) (value.Value, error) {
		name, err := strArg(a, 0, "p")
		if err != nil {
			return value.Null, err
		}
		if v, ok := p.opts.Properties[name]; ok {
			return value.Str(v), nil
		}
		if v, ok := os.LookupEnv(name); ok {
			return value.Str(v), nil
		}
		return value.Null, nil
	}
}

func logger(p *Program) builtinImpl {
	return func(a []value.Value) (value.Value, error) {
		v, prefix := a[0], ""
		if len(a) == 2 {
			prefix, _ = value.ToString(a[0])
			v = a[1]
		}
		var buf bytes.Buffer
		if prefix != "" {
			buf.WriteString(prefix + " - ")
		}
		if err := codec.WriteDW(&buf, v, map[string]string{"indent": "false"}); err != nil {
			return value.Null, err
		}
		buf.WriteByte('\n')
		_, _ = p.opts.Log.Write(buf.Bytes())
		return v, nil
	}
}

func propsOf(v value.Value, mime string) map[string]string {
	props := map[string]string{}
	for k, val := range codec.DefaultProps(mime) {
		props[k] = val
	}
	if o := v.Object(); o != nil {
		for i, n := 0, o.Len(); i < n; i++ {
			s, _ := value.ToString(o.Val(i))
			props[o.Key(i)] = s
		}
	}
	return props
}

func tryResult(v value.Value, err error) value.Value {
	b := value.NewBuilder(2)
	if err == nil {
		b.Add("success", value.True)
		b.Add("result", v)
		return b.Build()
	}
	msg, location := err.Error(), ""
	var e *Error
	if errors.As(err, &e) {
		msg, location = e.Err.Error(), e.Pos.String()
	}
	eb := value.NewBuilder(3)
	eb.Add("kind", value.Str("ExecutionError"))
	eb.Add("message", value.Str(msg))
	eb.Add("location", value.Str(location))
	b.Add("success", value.False)
	b.Add("error", eb.Build())
	return b.Build()
}

func isEmptyValue(v value.Value) (bool, error) {
	switch v.K {
	case value.KindNull:
		return true, nil
	case value.KindString:
		return v.S == "", nil
	case value.KindObject:
		return v.Object().Len() == 0, nil
	case value.KindArray:
		return v.Array().IsEmpty()
	}
	return false, nil
}

// concat implements the ++ function.
func concat(a, b value.Value) (value.Value, error) {
	switch {
	case a.K == value.KindArray || b.K == value.KindArray && a.IsNull():
		return concatArrays(a, b)
	case a.K == value.KindObject || b.K == value.KindObject:
		return concatObjects(a, b)
	case a.K.IsTemporal() && b.K.IsTemporal():
		return concatTemporal(a, b)
	case a.K == value.KindFunction || b.K == value.KindFunction:
		return value.Null, concatError(a, b)
	}
	as, err := value.ToString(a)
	if err != nil {
		return value.Null, err
	}
	bs, err := value.ToString(b)
	if err != nil {
		return value.Null, err
	}
	return value.Str(as + bs), nil
}

func concatObjects(a, b value.Value) (value.Value, error) {
	if (a.K != value.KindObject && !a.IsNull()) || (b.K != value.KindObject && !b.IsNull()) {
		return value.Null, concatError(a, b)
	}
	nb := value.NewBuilder(4)
	for _, v := range []value.Value{a, b} {
		if o := v.Object(); o != nil {
			nb.AddObject(o)
		}
	}
	return nb.Build(), nil
}

// concatTemporal combines a date with a time, or a local date-time with a
// time zone.
func concatTemporal(a, b value.Value) (value.Value, error) {
	d, t := a.Time(), b.Time()
	switch {
	case a.K == value.KindDate && b.K == value.KindTime:
		return value.Temporal(value.KindDateTime, joinDateTime(d, t, t.Location())), nil
	case a.K == value.KindDate && b.K == value.KindLocalTime:
		return value.Temporal(value.KindLocalDateTime, joinDateTime(d, t, time.UTC)), nil
	case a.K == value.KindLocalDateTime && b.K == value.KindTimeZone:
		return value.Temporal(value.KindDateTime, joinDateTime(d, d, t.Location())), nil
	}
	return value.Null, concatError(a, b)
}

func joinDateTime(d, t time.Time, loc *time.Location) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), loc)
}

// iterableOrEmpty treats null as an empty array.
func iterableOrEmpty(v value.Value) (*value.Array, bool) {
	if v.IsNull() {
		return value.EmptyArray().Array(), true
	}
	return asIterable(v)
}

func concatArrays(a, b value.Value) (value.Value, error) {
	left, ok1 := iterableOrEmpty(a)
	right, ok2 := iterableOrEmpty(b)
	if !ok1 || !ok2 {
		return value.Null, concatError(a, b)
	}
	if left.IsStream() || right.IsStream() {
		return value.NewStream(func(yield func(value.Value) error) error {
			fwd := func(_ int, v value.Value) error { return yield(v) }
			if err := left.Each(fwd); err != nil {
				return err
			}
			return right.Each(fwd)
		}, left.Restartable() && right.Restartable()), nil
	}
	li, _ := left.Items()
	ri, _ := right.Items()
	out := make([]value.Value, 0, len(li)+len(ri))
	return value.NewArray(append(append(out, li...), ri...)), nil
}

// minus implements the -- function.
func minus(a, b value.Value) (value.Value, error) {
	switch a.K {
	case value.KindNull:
		return value.Null, nil
	case value.KindObject:
		return removeKeys(a.Object(), b)
	case value.KindArray:
		return removeElements(a.Array(), b)
	}
	return value.Null, fmt.Errorf("cannot remove from %s", a.TypeName())
}

func removeElements(arr *value.Array, b value.Value) (value.Value, error) {
	items, err := arr.Items()
	if err != nil {
		return value.Null, err
	}
	var drop []value.Value
	if bv, ok := asIterable(b); ok {
		if drop, err = bv.Items(); err != nil {
			return value.Null, err
		}
	}
	out := make([]value.Value, 0, len(items))
	for _, it := range items {
		if !containsValue(drop, it) {
			out = append(out, it)
		}
	}
	return value.NewArray(out), nil
}

func containsValue(list []value.Value, v value.Value) bool {
	for _, d := range list {
		if eq, _ := value.Equal(v, d); eq {
			return true
		}
	}
	return false
}

func concatError(a, b value.Value) error {
	return fmt.Errorf("cannot concatenate %s and %s", a.TypeName(), b.TypeName())
}
