package interp

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/touno-io/go-tsunami-cli/internal/value"
)

// caller invokes a callback with a reused argument buffer. Callbacks never
// retain their arguments, so one buffer per operation avoids an allocation
// per element.
type caller struct {
	fn  *value.Func
	buf []value.Value
}

func newCaller(fn *value.Func, n int) *caller {
	return &caller{fn: fn, buf: make([]value.Value, n)}
}

func (c *caller) call2(a, b value.Value) (value.Value, error) {
	c.buf[0], c.buf[1] = a, b
	return c.fn.Call(c.buf[:2])
}

func (c *caller) call3(a, b, d value.Value) (value.Value, error) {
	c.buf[0], c.buf[1], c.buf[2] = a, b, d
	return c.fn.Call(c.buf[:3])
}

// test calls the callback with (item, index) and reports whether it
// returned true.
func (c *caller) test(v value.Value, i int) (bool, error) {
	ok, err := c.call2(v, value.Int(i))
	return ok.Truthy(), err
}

type arrayImpl func(arr *value.Array, fn *value.Func) (value.Value, error)

// arrayFn adapts an (array, function) builtin that maps null to null.
func arrayFn(name string, impl arrayImpl) builtinImpl {
	return func(a []value.Value) (value.Value, error) {
		if a[0].IsNull() {
			return value.Null, nil
		}
		arr, err := iterArg(a, 0, name)
		if err != nil {
			return value.Null, err
		}
		fn, err := fnArg(a, 1, name)
		if err != nil {
			return value.Null, err
		}
		return impl(arr, fn)
	}
}

// objectOrArrayFn adapts a builtin that accepts an object or an array as its
// first argument and a function as its second.
func objectOrArrayFn(name string, onObject func(*value.Object, *value.Func) (value.Value, error), onArray arrayImpl) builtinImpl {
	return func(a []value.Value) (value.Value, error) {
		if a[0].IsNull() {
			return value.Null, nil
		}
		fn, err := fnArg(a, 1, name)
		if err != nil {
			return value.Null, err
		}
		if o := a[0].Object(); o != nil {
			return onObject(o, fn)
		}
		arr, err := iterArg(a, 0, name)
		if err != nil {
			return value.Null, err
		}
		return onArray(arr, fn)
	}
}

func registerArrays() {
	register("map", 2, 2, arrayFn("map", mapArray))
	register("filter", 2, 2, filterBuiltin)
	register("flatMap", 2, 2, arrayFn("flatMap", flatMapArray))
	register("flatten", 1, 1, flattenBuiltin)
	register("reduce", 2, 2, arrayFn("reduce", reduceArray))
	register("groupBy", 2, 2, objectOrArrayFn("groupBy", groupObject, groupArray))
	register("orderBy", 2, 2, objectOrArrayFn("orderBy", orderObject, orderArray))
	register("distinctBy", 2, 2, objectOrArrayFn("distinctBy", distinctObject, distinctArray))
	register("joinBy", 2, 2, joinBy)
	register("contains", 2, 2, containsBuiltin)
	register("indexOf", 2, 2, func(a []value.Value) (value.Value, error) { return indexOf(a, false) })
	register("lastIndexOf", 2, 2, func(a []value.Value) (value.Value, error) { return indexOf(a, true) })
	register("find", 2, 2, findIndexes)
	register("max", 1, 1, func(a []value.Value) (value.Value, error) { return extreme(a, "max", 1) })
	register("min", 1, 1, func(a []value.Value) (value.Value, error) { return extreme(a, "min", -1) })
	register("maxBy", 2, 2, arrayFn("maxBy", func(arr *value.Array, fn *value.Func) (value.Value, error) {
		return extremeBy(arr, fn, 1)
	}))
	register("minBy", 2, 2, arrayFn("minBy", func(arr *value.Array, fn *value.Func) (value.Value, error) {
		return extremeBy(arr, fn, -1)
	}))
	register("sum", 1, 1, sumBuiltin)
	register("avg", 1, 1, avgBuiltin)
	register("zip", 2, 2, zipBuiltin)
	register("unzip", 1, 1, unzipBuiltin)
	register("take", 2, 2, func(a []value.Value) (value.Value, error) { return takeDrop(a, "take", true) })
	register("drop", 2, 2, func(a []value.Value) (value.Value, error) { return takeDrop(a, "drop", false) })
	register("slice", 3, 3, sliceBuiltin)
	register("some", 2, 2, arrayFn("some", someArray))
	register("every", 2, 2, arrayFn("every", everyArray))
	register("countBy", 2, 2, arrayFn("countBy", countArray))
	register("sumBy", 2, 2, arrayFn("sumBy", sumByArray))
	register("firstWith", 2, 2, arrayFn("firstWith", firstWith))
	register("indexWhere", 2, 2, arrayFn("indexWhere", indexWhere))
	register("partition", 2, 2, arrayFn("partition", partition))
	register("splitAt", 2, 2, splitAt)
	register("divideBy", 2, 2, divideBy)
}

func mapArray(arr *value.Array, fn *value.Func) (value.Value, error) {
	if arr.IsStream() {
		return value.NewStream(func(yield func(value.Value) error) error {
			c := newCaller(fn, 2)
			return arr.Each(func(i int, v value.Value) error {
				r, err := c.call2(v, value.Int(i))
				if err != nil {
					return err
				}
				return yield(r)
			})
		}, arr.Restartable()), nil
	}
	items, _ := arr.Items()
	out := make([]value.Value, len(items))
	c := newCaller(fn, 2)
	for i, v := range items {
		r, err := c.call2(v, value.Int(i))
		if err != nil {
			return value.Null, err
		}
		out[i] = r
	}
	return value.NewArray(out), nil
}

func filterBuiltin(a []value.Value) (value.Value, error) {
	if a[0].K == value.KindString {
		fn, err := fnArg(a, 1, "filter")
		if err != nil {
			return value.Null, err
		}
		return filterString(a[0].S, fn)
	}
	return arrayFn("filter", filterArray)(a)
}

func filterString(s string, fn *value.Func) (value.Value, error) {
	var b strings.Builder
	c := newCaller(fn, 2)
	i := 0
	for _, r := range s {
		ok, err := c.test(value.Str(string(r)), i)
		if err != nil {
			return value.Null, err
		}
		if ok {
			b.WriteRune(r)
		}
		i++
	}
	return value.Str(b.String()), nil
}

func filterArray(arr *value.Array, fn *value.Func) (value.Value, error) {
	c := newCaller(fn, 2)
	return mapLazy(arr, func(i int, v value.Value) (value.Value, bool, error) {
		ok, err := c.test(v, i)
		return v, ok, err
	})
}

func flatMapArray(arr *value.Array, fn *value.Func) (value.Value, error) {
	mapped, err := mapLazy(arr, func(i int, v value.Value) (value.Value, bool, error) {
		r, err := fn.Call([]value.Value{v, value.Int(i)})
		return r, true, err
	})
	if err != nil {
		return value.Null, err
	}
	return flatten(mapped.Array())
}

func flattenBuiltin(a []value.Value) (value.Value, error) {
	if a[0].IsNull() {
		return value.Null, nil
	}
	arr, err := iterArg(a, 0, "flatten")
	if err != nil {
		return value.Null, err
	}
	return flatten(arr)
}

func reduceArray(arr *value.Array, fn *value.Func) (value.Value, error) {
	acc, hasInit, err := fn.ParamDefault(1)
	if err != nil {
		return value.Null, err
	}
	first := !hasInit
	c := newCaller(fn, 2)
	err = arr.Each(func(_ int, v value.Value) error {
		if first {
			acc, first = v, false
			return nil
		}
		r, err := c.call2(v, acc)
		acc = r
		return err
	})
	return acc, err
}

func distinctObject(o *value.Object, fn *value.Func) (value.Value, error) {
	seen := map[string]bool{}
	b := value.NewBuilder(o.Len())
	c := newCaller(fn, 2)
	for i, n := 0, o.Len(); i < n; i++ {
		k, err := c.call2(o.Val(i), value.Str(o.Key(i)))
		if err != nil {
			return value.Null, err
		}
		if h := hashKey(k); !seen[h] {
			seen[h] = true
			b.Add(o.Key(i), o.Val(i))
		}
	}
	return b.Build(), nil
}

func distinctArray(arr *value.Array, fn *value.Func) (value.Value, error) {
	distinct := func(yield func(value.Value) error) error {
		seen := map[string]bool{}
		c := newCaller(fn, 2)
		return arr.Each(func(i int, v value.Value) error {
			k, err := c.call2(v, value.Int(i))
			if err != nil {
				return err
			}
			if h := hashKey(k); !seen[h] {
				seen[h] = true
				return yield(v)
			}
			return nil
		})
	}
	stream := value.NewStream(distinct, arr.Restartable())
	if arr.IsStream() {
		return stream, nil
	}
	_, err := stream.Array().Items()
	return stream, err
}

func joinBy(a []value.Value) (value.Value, error) {
	if a[0].IsNull() {
		return value.Null, nil
	}
	arr, err := iterArg(a, 0, "joinBy")
	if err != nil {
		return value.Null, err
	}
	sep, err := strArg(a, 1, "joinBy")
	if err != nil {
		return value.Null, err
	}
	var b strings.Builder
	err = arr.Each(func(i int, v value.Value) error {
		s, err := value.ToString(v)
		if err != nil {
			return fmt.Errorf("function joinBy: %w", err)
		}
		if i > 0 {
			b.WriteString(sep)
		}
		b.WriteString(s)
		return nil
	})
	return value.Str(b.String()), err
}

func containsBuiltin(a []value.Value) (value.Value, error) {
	switch a[0].K {
	case value.KindNull:
		return value.False, nil
	case value.KindString:
		if a[1].K == value.KindRegex {
			return value.Bool(a[1].RegexValue().MatchString(a[0].S)), nil
		}
		s, err := strArg(a, 1, "contains")
		return value.Bool(strings.Contains(a[0].S, s)), err
	}
	arr, err := iterArg(a, 0, "contains")
	if err != nil {
		return value.Null, err
	}
	found := false
	err = arr.Each(func(_ int, v value.Value) error {
		if eq, _ := value.Equal(v, a[1]); eq {
			found = true
			return value.ErrStop
		}
		return nil
	})
	return value.Bool(found), err
}

func sumBuiltin(a []value.Value) (value.Value, error) {
	total, _, err := sumValues(a, "sum")
	return value.Num(total), err
}

func avgBuiltin(a []value.Value) (value.Value, error) {
	total, n, err := sumValues(a, "avg")
	if err != nil || n == 0 {
		return value.Null, err
	}
	return value.Num(total / float64(n)), nil
}

func zipBuiltin(a []value.Value) (value.Value, error) {
	l, err := itemsArg(a, 0, "zip")
	if err != nil {
		return value.Null, err
	}
	r, err := itemsArg(a, 1, "zip")
	if err != nil {
		return value.Null, err
	}
	n := min(len(l), len(r))
	out := make([]value.Value, n)
	for i := 0; i < n; i++ {
		out[i] = value.NewArray([]value.Value{l[i], r[i]})
	}
	return value.NewArray(out), nil
}

func unzipBuiltin(a []value.Value) (value.Value, error) {
	rows, err := itemsArg(a, 0, "unzip")
	if err != nil {
		return value.Null, err
	}
	lists := make([][]value.Value, 0, len(rows))
	width := -1
	for _, row := range rows {
		items, err := itemsArg([]value.Value{row}, 0, "unzip")
		if err != nil {
			return value.Null, err
		}
		if width < 0 || len(items) < width {
			width = len(items)
		}
		lists = append(lists, items)
	}
	out := make([]value.Value, max(width, 0))
	for i := range out {
		col := make([]value.Value, len(lists))
		for j, l := range lists {
			col[j] = l[i]
		}
		out[i] = value.NewArray(col)
	}
	return value.NewArray(out), nil
}

func sliceBuiltin(a []value.Value) (value.Value, error) {
	items, err := itemsArg(a, 0, "slice")
	if err != nil || a[0].IsNull() {
		return value.Null, err
	}
	from, err := numArg(a, 1, "slice")
	if err != nil {
		return value.Null, err
	}
	until, err := numArg(a, 2, "slice")
	if err != nil {
		return value.Null, err
	}
	f := max(0, min(int(from), len(items)))
	u := max(f, min(int(until), len(items)))
	return value.NewArray(items[f:u]), nil
}

// scan calls the predicate for each element until visit returns false.
func scan(arr *value.Array, fn *value.Func, visit func(i int, v value.Value, ok bool) bool) error {
	c := newCaller(fn, 2)
	return arr.Each(func(i int, v value.Value) error {
		ok, err := c.test(v, i)
		if err != nil {
			return err
		}
		if !visit(i, v, ok) {
			return value.ErrStop
		}
		return nil
	})
}

func someArray(arr *value.Array, fn *value.Func) (value.Value, error) {
	found := false
	err := scan(arr, fn, func(_ int, _ value.Value, ok bool) bool {
		found = ok
		return !ok
	})
	return value.Bool(found), err
}

func everyArray(arr *value.Array, fn *value.Func) (value.Value, error) {
	all := true
	err := scan(arr, fn, func(_ int, _ value.Value, ok bool) bool {
		all = ok
		return ok
	})
	return value.Bool(all), err
}

func countArray(arr *value.Array, fn *value.Func) (value.Value, error) {
	n := 0
	err := scan(arr, fn, func(_ int, _ value.Value, ok bool) bool {
		if ok {
			n++
		}
		return true
	})
	return value.Int(n), err
}

func firstWith(arr *value.Array, fn *value.Func) (value.Value, error) {
	var out value.Value
	err := scan(arr, fn, func(_ int, v value.Value, ok bool) bool {
		if ok {
			out = v
		}
		return !ok
	})
	return out, err
}

func indexWhere(arr *value.Array, fn *value.Func) (value.Value, error) {
	idx := -1
	err := scan(arr, fn, func(i int, _ value.Value, ok bool) bool {
		if ok {
			idx = i
		}
		return !ok
	})
	return value.Int(idx), err
}

func partition(arr *value.Array, fn *value.Func) (value.Value, error) {
	var yes, no []value.Value
	err := scan(arr, fn, func(_ int, v value.Value, ok bool) bool {
		if ok {
			yes = append(yes, v)
		} else {
			no = append(no, v)
		}
		return true
	})
	b := value.NewBuilder(2)
	b.Add("success", value.NewArray(yes))
	b.Add("failure", value.NewArray(no))
	return b.Build(), err
}

func sumByArray(arr *value.Array, fn *value.Func) (value.Value, error) {
	total := 0.0
	c := newCaller(fn, 2)
	err := arr.Each(func(i int, v value.Value) error {
		r, err := c.call2(v, value.Int(i))
		if err != nil {
			return err
		}
		n, ok := numeric(r)
		if !ok {
			return fmt.Errorf("function sumBy expects numbers, got %s", r.TypeName())
		}
		total += n
		return nil
	})
	return value.Num(total), err
}

func splitAt(a []value.Value) (value.Value, error) {
	items, err := itemsArg(a, 0, "splitAt")
	if err != nil {
		return value.Null, err
	}
	n, err := numArg(a, 1, "splitAt")
	if err != nil {
		return value.Null, err
	}
	i := max(0, min(int(n), len(items)))
	b := value.NewBuilder(2)
	b.Add("l", value.NewArray(items[:i]))
	b.Add("r", value.NewArray(items[i:]))
	return b.Build(), nil
}

func divideBy(a []value.Value) (value.Value, error) {
	n, err := numArg(a, 1, "divideBy")
	if err != nil || n < 1 {
		return value.Null, fmt.Errorf("function divideBy expects a positive size")
	}
	size := int(n)
	if o := a[0].Object(); o != nil {
		return divideObject(o, size), nil
	}
	items, err := itemsArg(a, 0, "divideBy")
	if err != nil {
		return value.Null, err
	}
	var out []value.Value
	for i := 0; i < len(items); i += size {
		out = append(out, value.NewArray(items[i:min(i+size, len(items))]))
	}
	return value.NewArray(out), nil
}

func divideObject(o *value.Object, size int) value.Value {
	var out []value.Value
	for i := 0; i < o.Len(); i += size {
		b := value.NewBuilder(size)
		for j := i; j < min(i+size, o.Len()); j++ {
			b.Add(o.Key(j), o.Val(j))
		}
		out = append(out, b.Build())
	}
	return value.NewArray(out)
}

func itemsArg(a []value.Value, i int, name string) ([]value.Value, error) {
	if arg(a, i).IsNull() {
		return nil, nil
	}
	arr, err := iterArg(a, i, name)
	if err != nil {
		return nil, err
	}
	return arr.Items()
}

// expand yields the elements of an array value, or the value itself.
func expand(v value.Value, yield func(value.Value) error) error {
	if inner, ok := asIterable(v); ok {
		return inner.Each(func(_ int, el value.Value) error { return yield(el) })
	}
	if v.IsNull() {
		return nil
	}
	return yield(v)
}

// flatten concatenates nested arrays one level deep, lazily for streams.
func flatten(arr *value.Array) (value.Value, error) {
	if arr.IsStream() {
		return value.NewStream(func(yield func(value.Value) error) error {
			return arr.Each(func(_ int, v value.Value) error { return expand(v, yield) })
		}, arr.Restartable()), nil
	}
	items, _ := arr.Items()
	var out []value.Value
	collect := func(el value.Value) error {
		out = append(out, el)
		return nil
	}
	for _, v := range items {
		if err := expand(v, collect); err != nil {
			return value.Null, err
		}
	}
	return value.NewArray(out), nil
}

// hashKey returns a string that is equal for structurally equal values.
func hashKey(v value.Value) string {
	switch v.K {
	case value.KindArray, value.KindObject:
		return "s:" + dwText(v)
	case value.KindNumber:
		return "n:" + value.FormatNumber(v.N)
	}
	s, _ := value.ToString(v)
	return strconv.Itoa(int(v.K)) + ":" + s
}

func groupArray(arr *value.Array, fn *value.Func) (value.Value, error) {
	var keys []string
	groups := map[string][]value.Value{}
	c := newCaller(fn, 2)
	err := arr.Each(func(i int, v value.Value) error {
		kv, err := c.call2(v, value.Int(i))
		if err != nil {
			return err
		}
		k, err := keyString(kv)
		if err != nil {
			return err
		}
		if _, ok := groups[k]; !ok {
			keys = append(keys, k)
		}
		groups[k] = append(groups[k], v)
		return nil
	})
	if err != nil {
		return value.Null, err
	}
	b := value.NewBuilder(len(keys))
	for _, k := range keys {
		b.Add(k, value.NewArray(groups[k]))
	}
	return b.Build(), nil
}

func groupObject(o *value.Object, fn *value.Func) (value.Value, error) {
	var keys []string
	groups := map[string]*value.Builder{}
	c := newCaller(fn, 2)
	for i, n := 0, o.Len(); i < n; i++ {
		kv, err := c.call2(o.Val(i), value.Str(o.Key(i)))
		if err != nil {
			return value.Null, err
		}
		k, err := keyString(kv)
		if err != nil {
			return value.Null, err
		}
		g, ok := groups[k]
		if !ok {
			g = value.NewBuilder(1)
			groups[k] = g
			keys = append(keys, k)
		}
		g.Add(o.Key(i), o.Val(i))
	}
	b := value.NewBuilder(len(keys))
	for _, k := range keys {
		b.Add(k, groups[k].Build())
	}
	return b.Build(), nil
}

// sortedOrder returns the stable order of keys.
func sortedOrder(keys []value.Value) []int {
	idx := make([]int, len(keys))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(x, y int) bool { return value.SortCompare(keys[idx[x]], keys[idx[y]]) < 0 })
	return idx
}

func orderArray(arr *value.Array, fn *value.Func) (value.Value, error) {
	items, err := arr.Items()
	if err != nil {
		return value.Null, err
	}
	keys := make([]value.Value, len(items))
	c := newCaller(fn, 2)
	for i, v := range items {
		if keys[i], err = c.call2(v, value.Int(i)); err != nil {
			return value.Null, err
		}
	}
	out := make([]value.Value, len(items))
	for i, j := range sortedOrder(keys) {
		out[i] = items[j]
	}
	return value.NewArray(out), nil
}

func orderObject(o *value.Object, fn *value.Func) (value.Value, error) {
	n := o.Len()
	keys := make([]value.Value, n)
	c := newCaller(fn, 2)
	for i := 0; i < n; i++ {
		var err error
		if keys[i], err = c.call2(o.Val(i), value.Str(o.Key(i))); err != nil {
			return value.Null, err
		}
	}
	b := value.NewBuilder(n)
	for _, j := range sortedOrder(keys) {
		b.Add(o.Key(j), o.Val(j))
	}
	return b.Build(), nil
}

func indexOf(a []value.Value, last bool) (value.Value, error) {
	switch a[0].K {
	case value.KindNull:
		return value.Int(-1), nil
	case value.KindString:
		return stringIndexOf(a, last)
	}
	arr, err := iterArg(a, 0, "indexOf")
	if err != nil {
		return value.Null, err
	}
	idx := -1
	err = arr.Each(func(i int, v value.Value) error {
		if eq, _ := value.Equal(v, a[1]); eq {
			idx = i
			if !last {
				return value.ErrStop
			}
		}
		return nil
	})
	return value.Int(idx), err
}

func stringIndexOf(a []value.Value, last bool) (value.Value, error) {
	s, err := strArg(a, 1, "indexOf")
	if err != nil {
		return value.Null, err
	}
	i := strings.Index(a[0].S, s)
	if last {
		i = strings.LastIndex(a[0].S, s)
	}
	if i > 0 {
		i = len([]rune(a[0].S[:i]))
	}
	return value.Int(i), nil
}

func findIndexes(a []value.Value) (value.Value, error) {
	switch a[0].K {
	case value.KindNull:
		return value.EmptyArray(), nil
	case value.KindString:
		return findInString(a)
	}
	arr, err := iterArg(a, 0, "find")
	if err != nil {
		return value.Null, err
	}
	var out []value.Value
	err = arr.Each(func(i int, v value.Value) error {
		if eq, _ := value.Equal(v, a[1]); eq {
			out = append(out, value.Int(i))
		}
		return nil
	})
	return value.NewArray(out), err
}

func findInString(a []value.Value) (value.Value, error) {
	s := a[0].S
	var out []value.Value
	if a[1].K == value.KindRegex {
		for _, loc := range a[1].RegexValue().FindAllStringIndex(s, -1) {
			out = append(out, value.Int(loc[0]))
		}
		return value.NewArray(out), nil
	}
	sub, err := strArg(a, 1, "find")
	if err != nil || sub == "" {
		return value.EmptyArray(), err
	}
	for i := 0; i+len(sub) <= len(s); {
		j := strings.Index(s[i:], sub)
		if j < 0 {
			break
		}
		out = append(out, value.Int(i+j))
		i += j + 1
	}
	return value.NewArray(out), nil
}

func extreme(a []value.Value, name string, sign int) (value.Value, error) {
	if a[0].IsNull() {
		return value.Null, nil
	}
	arr, err := iterArg(a, 0, name)
	if err != nil {
		return value.Null, err
	}
	var best value.Value
	have := false
	err = arr.Each(func(_ int, v value.Value) error {
		if !have {
			best, have = v, true
			return nil
		}
		c, ok := value.Compare(v, best)
		if !ok {
			return fmt.Errorf("function %s cannot compare %s with %s", name, v.TypeName(), best.TypeName())
		}
		if c*sign > 0 {
			best = v
		}
		return nil
	})
	return best, err
}

func extremeBy(arr *value.Array, fn *value.Func, sign int) (value.Value, error) {
	var best, bestKey value.Value
	have := false
	c := newCaller(fn, 2)
	err := arr.Each(func(i int, v value.Value) error {
		k, err := c.call2(v, value.Int(i))
		if err != nil {
			return err
		}
		if !have || value.SortCompare(k, bestKey)*sign > 0 {
			best, bestKey, have = v, k, true
		}
		return nil
	})
	return best, err
}

func sumValues(a []value.Value, name string) (float64, int, error) {
	if a[0].IsNull() {
		return 0, 0, nil
	}
	arr, err := iterArg(a, 0, name)
	if err != nil {
		return 0, 0, err
	}
	total, n := 0.0, 0
	err = arr.Each(func(_ int, v value.Value) error {
		x, ok := numeric(v)
		if !ok {
			return fmt.Errorf("function %s expects numbers, got %s", name, v.TypeName())
		}
		total += x
		n++
		return nil
	})
	return total, n, err
}

func takeDrop(a []value.Value, name string, take bool) (value.Value, error) {
	if a[0].IsNull() {
		return value.Null, nil
	}
	arr, err := iterArg(a, 0, name)
	if err != nil {
		return value.Null, err
	}
	nf, err := numArg(a, 1, name)
	if err != nil {
		return value.Null, err
	}
	n := int(nf)
	if !arr.IsStream() {
		items, _ := arr.Items()
		n = max(0, min(n, len(items)))
		if take {
			return value.NewArray(items[:n]), nil
		}
		return value.NewArray(items[n:]), nil
	}
	return value.NewStream(func(yield func(value.Value) error) error {
		return arr.Each(func(i int, v value.Value) error {
			switch {
			case take && i >= n:
				return value.ErrStop
			case !take && i < n:
				return nil
			}
			return yield(v)
		})
	}, arr.Restartable()), nil
}
