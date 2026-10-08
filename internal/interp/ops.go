package interp

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/touno-io/go-tsunami-cli/internal/value"
)

func dirOf(path string) string { return filepath.Dir(path) }

var binaryOps = map[string]func(a, b value.Value) (value.Value, error){
	"+":  opAdd,
	"-":  opSub,
	"*":  opMul,
	"/":  opDiv,
	"==": opEq,
	"!=": opNe,
	"~=": opSimilar,
	"<":  cmpOp(func(c int) bool { return c < 0 }),
	">":  cmpOp(func(c int) bool { return c > 0 }),
	"<=": cmpOp(func(c int) bool { return c <= 0 }),
	">=": cmpOp(func(c int) bool { return c >= 0 }),
}

// numeric returns v as a number, accepting numeric strings the way DataWeave
// coerces arguments.
func numeric(v value.Value) (float64, bool) {
	switch v.K {
	case value.KindNumber:
		return v.N, true
	case value.KindString:
		if n, ok := value.ParseNumber(strings.TrimSpace(v.S)); ok {
			return n.N, true
		}
	}
	return 0, false
}

// numericPair returns both operands as numbers when at least one is a
// number and the other is numeric, mirroring DataWeave's coercion.
func numericPair(a, b value.Value) (float64, float64, bool) {
	if a.K != value.KindNumber && b.K != value.KindNumber {
		return 0, 0, false
	}
	x, ok1 := numeric(a)
	y, ok2 := numeric(b)
	return x, y, ok1 && ok2
}

func opAdd(a, b value.Value) (value.Value, error) {
	if x, y, ok := numericPair(a, b); ok {
		return value.Num(x + y), nil
	}
	switch {
	case a.K == value.KindArray:
		items, err := a.Array().Items()
		if err != nil {
			return value.Null, err
		}
		out := make([]value.Value, len(items), len(items)+1)
		copy(out, items)
		return value.NewArray(append(out, b)), nil
	case a.K.IsTemporal() && b.K == value.KindPeriod:
		return value.Temporal(a.K, b.Period().AddTo(a.Time(), false)), nil
	case a.K == value.KindPeriod && b.K.IsTemporal():
		return value.Temporal(b.K, a.Period().AddTo(b.Time(), false)), nil
	}
	return value.Null, fmt.Errorf("cannot add %s and %s", a.TypeName(), b.TypeName())
}

func opSub(a, b value.Value) (value.Value, error) {
	if x, y, ok := numericPair(a, b); ok {
		return value.Num(x - y), nil
	}
	switch {
	case a.K == value.KindObject:
		return removeKeys(a.Object(), b)
	case a.K == value.KindArray:
		return removeElements(a.Array(), value.NewArray([]value.Value{b}))
	case a.K.IsTemporal() && b.K == value.KindPeriod:
		return value.Temporal(a.K, b.Period().AddTo(a.Time(), true)), nil
	case a.K.IsTemporal() && b.K.IsTemporal():
		return temporalDiff(a, b), nil
	case a.K == value.KindNull:
		return value.Null, nil
	}
	return value.Null, fmt.Errorf("cannot subtract %s from %s", b.TypeName(), a.TypeName())
}

// temporalDiff returns the period between two dates or times.
func temporalDiff(a, b value.Value) value.Value {
	d := a.Time().Sub(b.Time())
	if a.K == value.KindDate {
		return value.PeriodValue(value.Period{Days: int(d.Hours() / 24)})
	}
	return value.PeriodValue(value.Period{Dur: d})
}

// removeKeys implements object - "key", object - ["a", "b"] and
// object -- {a: ...}.
func removeKeys(o *value.Object, keys value.Value) (value.Value, error) {
	drop := map[string]bool{}
	switch keys.K {
	case value.KindString:
		drop[keys.S] = true
	case value.KindArray:
		items, err := keys.Array().Items()
		if err != nil {
			return value.Null, err
		}
		for _, k := range items {
			s, err := value.ToString(k)
			if err != nil {
				return value.Null, err
			}
			drop[s] = true
		}
	case value.KindObject:
		ko := keys.Object()
		for i, n := 0, ko.Len(); i < n; i++ {
			drop[ko.Key(i)] = true
		}
	case value.KindNull:
	default:
		return value.Null, fmt.Errorf("cannot remove %s from an Object", keys.TypeName())
	}
	b := value.NewBuilder(o.Len())
	for i, n := 0, o.Len(); i < n; i++ {
		if k := o.Key(i); !drop[k] {
			b.Add(k, o.Val(i))
		}
	}
	return b.Build(), nil
}

func opMul(a, b value.Value) (value.Value, error) {
	x, ok1 := numeric(a)
	y, ok2 := numeric(b)
	if !ok1 || !ok2 {
		return value.Null, fmt.Errorf("cannot multiply %s and %s", a.TypeName(), b.TypeName())
	}
	return value.Num(x * y), nil
}

func opDiv(a, b value.Value) (value.Value, error) {
	x, ok1 := numeric(a)
	y, ok2 := numeric(b)
	if !ok1 || !ok2 {
		return value.Null, fmt.Errorf("cannot divide %s by %s", a.TypeName(), b.TypeName())
	}
	if y == 0 {
		return value.Null, errors.New("division by zero")
	}
	return value.Num(x / y), nil
}

func opEq(a, b value.Value) (value.Value, error) {
	eq, err := value.Equal(a, b)
	return value.Bool(eq), err
}

func opNe(a, b value.Value) (value.Value, error) {
	eq, err := value.Equal(a, b)
	return value.Bool(!eq), err
}

func opSimilar(a, b value.Value) (value.Value, error) {
	if eq, err := value.Equal(a, b); err != nil || eq {
		return value.Bool(eq), err
	}
	if x, ok := numeric(a); ok {
		if y, ok := numeric(b); ok {
			return value.Bool(x == y), nil
		}
	}
	as, err1 := value.ToString(a)
	bs, err2 := value.ToString(b)
	if err1 != nil || err2 != nil || a.IsNull() || b.IsNull() {
		return value.False, nil
	}
	return value.Bool(as == bs), nil
}

func cmpOp(test func(int) bool) func(a, b value.Value) (value.Value, error) {
	return func(a, b value.Value) (value.Value, error) {
		if a.IsNull() || b.IsNull() {
			return value.False, nil
		}
		if c, ok := value.Compare(a, b); ok {
			return value.Bool(test(c)), nil
		}
		if x, ok := numeric(a); ok {
			if y, ok := numeric(b); ok {
				return value.Bool(test(compareFloat(x, y))), nil
			}
		}
		return value.Null, fmt.Errorf("cannot compare %s with %s", a.TypeName(), b.TypeName())
	}
}

func compareFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// asIterable returns the elements of an array or range value.
func asIterable(v value.Value) (*value.Array, bool) {
	switch v.K {
	case value.KindArray:
		return v.Array(), true
	case value.KindRange:
		from, to := v.RangeBounds()
		return value.NewStream(func(yield func(value.Value) error) error {
			step := 1
			if to < from {
				step = -1
			}
			for n := from; ; n += step {
				if err := yield(value.Int(n)); err != nil {
					return err
				}
				if n == to {
					return nil
				}
			}
		}, true).Array(), true
	}
	return nil, false
}

// mapLazy returns a stream applying fn to each element, or a materialized
// array when the source is already in memory.
func mapLazy(a *value.Array, fn func(i int, v value.Value) (value.Value, bool, error)) (value.Value, error) {
	if a.IsStream() {
		return value.NewStream(func(yield func(value.Value) error) error {
			return a.Each(func(i int, v value.Value) error {
				r, keep, err := fn(i, v)
				if err != nil || !keep {
					return err
				}
				return yield(r)
			})
		}, a.Restartable()), nil
	}
	items, _ := a.Items()
	out := make([]value.Value, 0, len(items))
	for i, v := range items {
		r, keep, err := fn(i, v)
		if err != nil {
			return value.Null, err
		}
		if keep {
			out = append(out, r)
		}
	}
	return value.NewArray(out), nil
}

func selectKey(v value.Value, name string) (value.Value, error) {
	switch v.K {
	case value.KindObject:
		r, _ := v.Object().Get(name)
		return r, nil
	case value.KindNull:
		return value.Null, nil
	case value.KindArray:
		return mapLazy(v.Array(), func(_ int, el value.Value) (value.Value, bool, error) {
			if o := el.Object(); o != nil {
				r, ok := o.Get(name)
				return r, ok, nil
			}
			return value.Null, false, nil
		})
	}
	if v.K.IsTemporal() || v.K == value.KindPeriod {
		return temporalField(v, name)
	}
	return value.Null, fmt.Errorf("cannot select .%s on %s", name, v.TypeName())
}

func selectMulti(v value.Value, name string) (value.Value, error) {
	switch v.K {
	case value.KindObject:
		all := v.Object().GetAll(name)
		if all == nil {
			return value.Null, nil
		}
		return value.NewArray(all), nil
	case value.KindArray:
		var out []value.Value
		err := v.Array().Each(func(_ int, el value.Value) error {
			if o := el.Object(); o != nil {
				out = append(out, o.GetAll(name)...)
			}
			return nil
		})
		return value.NewArray(out), err
	case value.KindNull:
		return value.Null, nil
	}
	return value.Null, fmt.Errorf("cannot select .*%s on %s", name, v.TypeName())
}

func selectDescendants(v value.Value, name string) (value.Value, error) {
	var out []value.Value
	var walk func(value.Value) error
	walk = func(v value.Value) error {
		switch v.K {
		case value.KindObject:
			o := v.Object()
			for i, n := 0, o.Len(); i < n; i++ {
				if o.Key(i) == name {
					out = append(out, o.Val(i))
				}
				if err := walk(o.Val(i)); err != nil {
					return err
				}
			}
		case value.KindArray:
			return v.Array().Each(func(_ int, el value.Value) error { return walk(el) })
		}
		return nil
	}
	if err := walk(v); err != nil {
		return value.Null, err
	}
	if out == nil {
		return value.Null, nil
	}
	return value.NewArray(out), nil
}

func hasKey(v value.Value, name string) bool {
	switch v.K {
	case value.KindObject:
		return v.Object().IndexOf(name) >= 0
	case value.KindArray:
		found := false
		_ = v.Array().Each(func(_ int, el value.Value) error {
			if o := el.Object(); o != nil && o.IndexOf(name) >= 0 {
				found = true
				return value.ErrStop
			}
			return nil
		})
		return found
	}
	return false
}

func selectIndex(v value.Value, idx value.Value) (value.Value, error) {
	if v.IsNull() || idx.IsNull() {
		return value.Null, nil
	}
	switch idx.K {
	case value.KindString:
		return selectKey(v, idx.S)
	case value.KindRange:
		return sliceRange(v, idx)
	case value.KindNumber:
	default:
		return value.Null, fmt.Errorf("cannot index with %s", idx.TypeName())
	}
	i := int(idx.N)
	switch v.K {
	case value.KindArray:
		r, _, err := v.Array().At(i)
		return r, err
	case value.KindString:
		runes := []rune(v.S)
		if i < 0 {
			i += len(runes)
		}
		if i < 0 || i >= len(runes) {
			return value.Null, nil
		}
		return value.Str(string(runes[i])), nil
	case value.KindObject:
		o := v.Object()
		if i < 0 {
			i += o.Len()
		}
		if i < 0 || i >= o.Len() {
			return value.Null, nil
		}
		return o.Val(i), nil
	case value.KindRange:
		a, _ := asIterable(v)
		r, _, err := a.At(i)
		return r, err
	}
	return value.Null, fmt.Errorf("cannot index %s", v.TypeName())
}

// clampRange resolves inclusive, possibly negative bounds against a length
// n. ok is false when the range lies entirely outside.
func clampRange(from, to, n int) (int, int, bool) {
	if from < 0 {
		from += n
	}
	if to < 0 {
		to += n
	}
	if n == 0 || from < 0 && to < 0 || from >= n && to >= n {
		return 0, 0, false
	}
	return max(0, min(from, n-1)), max(0, min(to, n-1)), true
}

// sliceIndexes returns the indexes selected by an inclusive range, in
// reverse order when from > to.
func sliceIndexes(from, to int) []int {
	step := 1
	if from > to {
		step = -1
	}
	out := make([]int, 0, (to-from)*step+1)
	for i := from; ; i += step {
		out = append(out, i)
		if i == to {
			return out
		}
	}
}

// sliceRange implements v[from to to] with inclusive, possibly negative or
// reversed bounds. Out-of-range bounds are clamped.
func sliceRange(v value.Value, r value.Value) (value.Value, error) {
	from, to := r.RangeBounds()
	switch v.K {
	case value.KindString:
		runes := []rune(v.S)
		f, t, ok := clampRange(from, to, len(runes))
		if !ok {
			return value.Str(""), nil
		}
		out := make([]rune, 0, len(runes))
		for _, i := range sliceIndexes(f, t) {
			out = append(out, runes[i])
		}
		return value.Str(string(out)), nil
	case value.KindArray:
		items, err := v.Array().Items()
		if err != nil {
			return value.Null, err
		}
		f, t, ok := clampRange(from, to, len(items))
		if !ok {
			return value.EmptyArray(), nil
		}
		if f <= t {
			return value.NewArray(items[f : t+1]), nil
		}
		out := make([]value.Value, 0, f-t+1)
		for _, i := range sliceIndexes(f, t) {
			out = append(out, items[i])
		}
		return value.NewArray(out), nil
	}
	return value.Null, fmt.Errorf("cannot slice %s", v.TypeName())
}

func filterSelector(v value.Value, cond value.Value) (value.Value, error) {
	fn := cond.Func()
	if fn == nil {
		return value.Null, errors.New("filter selector requires a condition")
	}
	switch v.K {
	case value.KindArray:
		return mapLazy(v.Array(), func(i int, el value.Value) (value.Value, bool, error) {
			ok, err := fn.Call([]value.Value{el, value.Int(i)})
			return el, ok.Truthy(), err
		})
	case value.KindObject:
		return filterObject(v.Object(), newCaller(fn, 3))
	case value.KindNull:
		return value.Null, nil
	}
	return value.Null, fmt.Errorf("cannot filter %s", v.TypeName())
}

// isType implements the "is" operator.
func isType(v value.Value, typ string) bool {
	switch typ {
	case "Any":
		return true
	case "Nothing":
		return false
	case "Key":
		return v.K == value.KindString
	case "SimpleType":
		return v.K != value.KindArray && v.K != value.KindObject && v.K != value.KindFunction
	case "Comparable":
		return v.K == value.KindString || v.K == value.KindNumber || v.K == value.KindBool || v.K.IsTemporal()
	}
	return v.TypeName() == typ
}

// errNoCoercion marks a coercion that the target type does not support.
var errNoCoercion = errors.New("no coercion")

// coercions maps simple target types to their converters. Converters return
// errNoCoercion when the value cannot be converted.
var coercions = map[string]func(v value.Value) (value.Value, error){
	"Boolean": toBoolean,
	"Array":   toArray,
	"Object":  toObject,
	"Regex":   toRegex,
	"Period":  toPeriod,
	"Null":    toNull,
}

// coerce implements "as Type { options }".
func coerce(v value.Value, typ string, opts *value.Object) (value.Value, error) {
	if v.IsNull() && typ != "Null" && typ != "Any" {
		return value.Null, nil
	}
	format := optString(opts, "format")
	var r value.Value
	var err error
	switch typ {
	case "Any":
		return v, nil
	case "String", "Key":
		return coerceString(v, format, opts)
	case "Number":
		return coerceNumber(v, format, opts)
	case "Date", "DateTime", "LocalDateTime", "Time", "LocalTime", "TimeZone":
		return coerceTemporal(v, typ, format, opts)
	default:
		conv, ok := coercions[typ]
		if !ok {
			return sameType(v, typ)
		}
		r, err = conv(v)
	}
	if err == errNoCoercion {
		return value.Null, fmt.Errorf("cannot coerce %s %s to %s", v.TypeName(), shortText(v), typ)
	}
	return r, err
}

// sameType accepts v when it already has the requested type.
func sameType(v value.Value, typ string) (value.Value, error) {
	if v.TypeName() == typ {
		return v, nil
	}
	return value.Null, fmt.Errorf("unknown type %q", typ)
}

func toBoolean(v value.Value) (value.Value, error) {
	if v.K == value.KindBool {
		return v, nil
	}
	if v.K == value.KindString {
		switch strings.ToLower(strings.TrimSpace(v.S)) {
		case "true":
			return value.True, nil
		case "false":
			return value.False, nil
		}
	}
	return value.Null, errNoCoercion
}

func toArray(v value.Value) (value.Value, error) {
	if a, ok := asIterable(v); ok {
		return value.Value{K: value.KindArray, R: a}, nil
	}
	if v.K == value.KindString {
		return stringsToArray(splitRunes(v.S)), nil
	}
	return value.Null, errNoCoercion
}

func toObject(v value.Value) (value.Value, error) {
	if v.K == value.KindObject {
		return v, nil
	}
	return value.Null, errNoCoercion
}

func toRegex(v value.Value) (value.Value, error) {
	switch v.K {
	case value.KindRegex:
		return v, nil
	case value.KindString:
		re, err := compileRegex(v.S)
		if err != nil {
			return value.Null, err
		}
		return value.Regex(v.S, re), nil
	}
	return value.Null, errNoCoercion
}

func toPeriod(v value.Value) (value.Value, error) {
	switch v.K {
	case value.KindPeriod:
		return v, nil
	case value.KindString:
		p, err := value.ParsePeriod(v.S)
		if err != nil {
			return value.Null, err
		}
		return value.PeriodValue(p), nil
	}
	return value.Null, errNoCoercion
}

func toNull(v value.Value) (value.Value, error) {
	if v.IsNull() {
		return v, nil
	}
	return value.Null, errNoCoercion
}

func splitRunes(s string) []string {
	out := make([]string, 0, utf8.RuneCountInString(s))
	for _, r := range s {
		out = append(out, string(r))
	}
	return out
}

func shortText(v value.Value) string {
	s, err := value.ToString(v)
	if err != nil {
		return ""
	}
	if len(s) > 40 {
		s = s[:40] + "..."
	}
	return "(" + s + ")"
}

func optString(opts *value.Object, key string) string {
	if opts == nil {
		return ""
	}
	v, ok := opts.Get(key)
	if !ok || v.IsNull() {
		return ""
	}
	s, _ := value.ToString(v)
	return s
}

func coerceString(v value.Value, format string, opts *value.Object) (value.Value, error) {
	switch {
	case v.K == value.KindString:
		return v, nil
	case v.K == value.KindNumber && format != "":
		s, err := formatDecimal(v, format, optString(opts, "roundMode"))
		return value.Str(s), err
	case v.K.IsTemporal() && format != "":
		s, err := formatTemporal(v.Time(), format)
		return value.Str(s), err
	case v.K == value.KindArray || v.K == value.KindObject || v.K == value.KindFunction:
		return value.Null, fmt.Errorf("cannot coerce %s to String", v.TypeName())
	}
	s, err := value.ToString(v)
	return value.Str(s), err
}

func coerceNumber(v value.Value, format string, opts *value.Object) (value.Value, error) {
	switch v.K {
	case value.KindNumber:
		return v, nil
	case value.KindString:
		s := strings.TrimSpace(v.S)
		if format != "" {
			s = strings.NewReplacer(",", "", "%", "", " ", "").Replace(s)
		}
		if n, ok := value.ParseNumber(s); ok {
			return n, nil
		}
		return value.Null, fmt.Errorf("cannot coerce String (%s) to Number", v.S)
	case value.KindBool:
		return value.Null, errors.New("cannot coerce Boolean to Number")
	}
	if v.K.IsTemporal() {
		unit := optString(opts, "unit")
		t := v.Time()
		if unit == "milliseconds" {
			return value.Num(float64(t.UnixMilli())), nil
		}
		return value.Num(float64(t.Unix())), nil
	}
	return value.Null, fmt.Errorf("cannot coerce %s to Number", v.TypeName())
}

func coerceTemporal(v value.Value, typ, format string, opts *value.Object) (value.Value, error) {
	kind := temporalKind(typ)
	switch {
	case v.K == value.KindString:
		if format != "" {
			t, err := parseTemporal(v.S, format, kind)
			if err != nil {
				return value.Null, err
			}
			return value.Temporal(kind, t), nil
		}
		lit, err := parseTemporalLiteral(strings.TrimSpace(v.S))
		if err != nil {
			return value.Null, fmt.Errorf("cannot coerce String (%s) to %s", v.S, typ)
		}
		return convertTemporal(lit, kind)
	case v.K == value.KindNumber:
		var t time.Time
		if optString(opts, "unit") == "milliseconds" {
			t = time.UnixMilli(int64(v.N)).UTC()
		} else {
			sec, frac := math.Modf(v.N)
			t = time.Unix(int64(sec), int64(frac*1e9)).UTC()
		}
		return convertTemporal(value.Temporal(value.KindDateTime, t), kind)
	case v.K.IsTemporal():
		return convertTemporal(v, kind)
	}
	return value.Null, fmt.Errorf("cannot coerce %s to %s", v.TypeName(), typ)
}

func temporalKind(typ string) value.Kind {
	switch typ {
	case "Date":
		return value.KindDate
	case "DateTime":
		return value.KindDateTime
	case "LocalDateTime":
		return value.KindLocalDateTime
	case "Time":
		return value.KindTime
	case "LocalTime":
		return value.KindLocalTime
	}
	return value.KindTimeZone
}

// convertTemporal changes the kind of a temporal value, dropping or adding
// date, time and zone parts.
func convertTemporal(v value.Value, kind value.Kind) (value.Value, error) {
	if v.K == kind {
		return v, nil
	}
	if !temporalSources[kind][v.K] {
		return value.Null, fmt.Errorf("cannot coerce %s to %s", v.TypeName(), kind)
	}
	t := v.Time()
	switch kind {
	case value.KindDate:
		t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	case value.KindLocalDateTime:
		t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
	}
	return value.Temporal(kind, t), nil
}

// temporalSources lists, for each target kind, the kinds it can be derived
// from.
var temporalSources = map[value.Kind]map[value.Kind]bool{
	value.KindDate:          {value.KindDateTime: true, value.KindLocalDateTime: true},
	value.KindLocalDateTime: {value.KindDate: true, value.KindDateTime: true},
	value.KindDateTime:      {value.KindDate: true, value.KindLocalDateTime: true},
	value.KindLocalTime:     {value.KindDateTime: true, value.KindLocalDateTime: true, value.KindTime: true},
	value.KindTime:          {value.KindDateTime: true, value.KindLocalDateTime: true, value.KindLocalTime: true},
	value.KindTimeZone:      {value.KindDateTime: true, value.KindTime: true},
}

// compileRegex compiles a DataWeave (Java) regular expression with Go's
// RE2 engine.
func compileRegex(src string) (*regexp.Regexp, error) {
	re, err := regexp.Compile(src)
	if err != nil {
		return nil, fmt.Errorf("unsupported regular expression /%s/: %v", src, err)
	}
	return re, nil
}
