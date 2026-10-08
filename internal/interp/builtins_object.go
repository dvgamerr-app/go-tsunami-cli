package interp

import (
	"fmt"

	"github.com/touno-io/go-tsunami-cli/internal/value"
)

type objectImpl func(o *value.Object, c *caller) (value.Value, error)

// objectFn adapts an (object, function) builtin that maps null to null.
func objectFn(name string, impl objectImpl) builtinImpl {
	return func(a []value.Value) (value.Value, error) {
		if a[0].IsNull() {
			return value.Null, nil
		}
		o, err := objArg(a, 0, name)
		if err != nil {
			return value.Null, err
		}
		fn, err := fnArg(a, 1, name)
		if err != nil {
			return value.Null, err
		}
		return impl(o, newCaller(fn, 3))
	}
}

// keysFn adapts a builtin that lists one element per object field.
func keysFn(name string, pick func(o *value.Object, i int) value.Value) builtinImpl {
	return func(a []value.Value) (value.Value, error) {
		if a[0].IsNull() {
			return value.Null, nil
		}
		o, err := objArg(a, 0, name)
		if err != nil {
			return value.Null, err
		}
		out := make([]value.Value, o.Len())
		for i := range out {
			out[i] = pick(o, i)
		}
		return value.NewArray(out), nil
	}
}

func fieldKey(o *value.Object, i int) value.Value { return value.Str(o.Key(i)) }

func fieldValue(o *value.Object, i int) value.Value { return o.Val(i) }

func fieldEntry(o *value.Object, i int) value.Value {
	b := value.NewBuilder(3)
	b.Add("key", value.Str(o.Key(i)))
	b.Add("value", o.Val(i))
	b.Add("attributes", value.Null)
	return b.Build()
}

func registerObjects() {
	register("mapObject", 2, 2, objectFn("mapObject", mapObject))
	register("filterObject", 2, 2, objectFn("filterObject", filterObject))
	register("pluck", 2, 2, objectFn("pluck", pluck))
	register("everyEntry", 2, 2, objectFn("everyEntry", func(o *value.Object, c *caller) (value.Value, error) {
		return entryTest(o, c, false)
	}))
	register("someEntry", 2, 2, objectFn("someEntry", func(o *value.Object, c *caller) (value.Value, error) {
		return entryTest(o, c, true)
	}))
	register("keysOf", 1, 1, keysFn("keysOf", fieldKey))
	register("namesOf", 1, 1, keysFn("namesOf", fieldKey))
	register("keySet", 1, 1, keysFn("keySet", fieldKey))
	register("valuesOf", 1, 1, keysFn("valuesOf", fieldValue))
	register("valueSet", 1, 1, keysFn("valueSet", fieldValue))
	register("entriesOf", 1, 1, keysFn("entriesOf", fieldEntry))
	register("mergeWith", 2, 2, mergeWith)
}

func mapObject(o *value.Object, c *caller) (value.Value, error) {
	b := value.NewBuilder(o.Len())
	for i, n := 0, o.Len(); i < n; i++ {
		r, err := c.call3(o.Val(i), value.Str(o.Key(i)), value.Int(i))
		if err != nil {
			return value.Null, err
		}
		if err := spreadInto(b, r); err != nil {
			return value.Null, fmt.Errorf("function mapObject: %w", err)
		}
	}
	return b.Build(), nil
}

func filterObject(o *value.Object, c *caller) (value.Value, error) {
	b := value.NewBuilder(o.Len())
	for i, n := 0, o.Len(); i < n; i++ {
		ok, err := c.call3(o.Val(i), value.Str(o.Key(i)), value.Int(i))
		if err != nil {
			return value.Null, err
		}
		if ok.Truthy() {
			b.Add(o.Key(i), o.Val(i))
		}
	}
	return b.Build(), nil
}

func pluck(o *value.Object, c *caller) (value.Value, error) {
	out := make([]value.Value, o.Len())
	for i := range out {
		r, err := c.call3(o.Val(i), value.Str(o.Key(i)), value.Int(i))
		if err != nil {
			return value.Null, err
		}
		out[i] = r
	}
	return value.NewArray(out), nil
}

// entryTest implements everyEntry (want=false stops at the first false
// entry) and someEntry (want=true stops at the first true entry).
func entryTest(o *value.Object, c *caller, want bool) (value.Value, error) {
	for i, n := 0, o.Len(); i < n; i++ {
		ok, err := c.call2(o.Val(i), value.Str(o.Key(i)))
		if err != nil {
			return value.False, err
		}
		if ok.Truthy() == want {
			return value.Bool(want), nil
		}
	}
	return value.Bool(!want), nil
}

func mergeWith(a []value.Value) (value.Value, error) {
	if a[0].IsNull() {
		return a[1], nil
	}
	if a[1].IsNull() {
		return a[0], nil
	}
	l, err := objArg(a, 0, "mergeWith")
	if err != nil {
		return value.Null, err
	}
	r, err := objArg(a, 1, "mergeWith")
	if err != nil {
		return value.Null, err
	}
	b := value.NewBuilder(l.Len() + r.Len())
	for i, n := 0, l.Len(); i < n; i++ {
		if r.IndexOf(l.Key(i)) < 0 {
			b.Add(l.Key(i), l.Val(i))
		}
	}
	b.AddObject(r)
	return b.Build(), nil
}
