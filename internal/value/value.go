// Package value defines the runtime value model shared by the interpreter and
// the format codecs.
//
// A Value is a small tagged struct instead of an interface so scalars (numbers,
// strings, booleans and null) never allocate when they are created or copied.
package value

import (
	"math"
	"regexp"
	"strconv"
	"time"
)

// Kind identifies the dynamic type of a Value.
type Kind uint8

// Value kinds. The order is significant for sorting values of mixed types.
const (
	KindNull Kind = iota
	KindBool
	KindNumber
	KindString
	KindDate
	KindDateTime
	KindLocalDateTime
	KindTime
	KindLocalTime
	KindTimeZone
	KindPeriod
	KindRange
	KindRegex
	KindArray
	KindObject
	KindFunction
	KindType
	// KindThunk marks a lazily evaluated binding. It never escapes the
	// interpreter's variable slots.
	KindThunk
)

var kindNames = [...]string{
	KindNull:          "Null",
	KindBool:          "Boolean",
	KindNumber:        "Number",
	KindString:        "String",
	KindDate:          "Date",
	KindDateTime:      "DateTime",
	KindLocalDateTime: "LocalDateTime",
	KindTime:          "Time",
	KindLocalTime:     "LocalTime",
	KindTimeZone:      "TimeZone",
	KindPeriod:        "Period",
	KindRange:         "Range",
	KindRegex:         "Regex",
	KindArray:         "Array",
	KindObject:        "Object",
	KindFunction:      "Function",
	KindType:          "Type",
	KindThunk:         "Thunk",
}

// String returns the DataWeave type name of the kind.
func (k Kind) String() string {
	if int(k) < len(kindNames) {
		return kindNames[k]
	}
	return "Unknown"
}

// IsTemporal reports whether the kind is one of the date and time kinds.
func (k Kind) IsTemporal() bool {
	return k >= KindDate && k <= KindTimeZone
}

// Value is a DataWeave runtime value.
//
// Field usage depends on K:
//   - KindBool: N is 1 for true and 0 for false.
//   - KindNumber: N holds the number; S optionally holds its exact source text.
//   - KindString, KindType: S holds the text.
//   - KindRegex: S holds the pattern source and R the compiled *regexp.Regexp.
//   - KindRange: N holds the start and R the inclusive end (int).
//   - KindArray, KindObject, KindFunction: R holds *Array, *Object or *Func.
//   - Temporal kinds: R holds a time.Time; KindPeriod holds a Period in R.
type Value struct {
	K Kind
	N float64
	S string
	R any
}

// Null is the null value.
var Null = Value{}

// True and False are the boolean values.
var (
	True  = Value{K: KindBool, N: 1}
	False = Value{K: KindBool}
)

// Bool returns a boolean value.
func Bool(b bool) Value {
	if b {
		return True
	}
	return False
}

// Num returns a number value.
func Num(f float64) Value {
	return Value{K: KindNumber, N: f}
}

// Int returns a number value for an integer.
func Int(i int) Value {
	return Value{K: KindNumber, N: float64(i)}
}

// NumRaw returns a number value that remembers its exact source text so it can
// be written back without precision or scale changes.
func NumRaw(f float64, raw string) Value {
	return Value{K: KindNumber, N: f, S: raw}
}

// Str returns a string value.
func Str(s string) Value {
	return Value{K: KindString, S: s}
}

// Type returns a type value such as String or Number.
func Type(name string) Value {
	return Value{K: KindType, S: name}
}

// Regex returns a regular expression value.
func Regex(src string, re *regexp.Regexp) Value {
	return Value{K: KindRegex, S: src, R: re}
}

// Range returns an inclusive integer range value.
func Range(from, to int) Value {
	return Value{K: KindRange, N: float64(from), R: to}
}

// Temporal returns a date or time value of the given kind.
func Temporal(k Kind, t time.Time) Value {
	return Value{K: k, R: t}
}

// FuncValue wraps a function.
func FuncValue(f *Func) Value {
	return Value{K: KindFunction, R: f}
}

// IsNull reports whether v is null.
func (v Value) IsNull() bool { return v.K == KindNull }

// Truthy reports whether a boolean value is true.
func (v Value) Truthy() bool { return v.K == KindBool && v.N != 0 }

// Array returns the array payload of v, or nil when v is not an array.
func (v Value) Array() *Array {
	a, _ := v.R.(*Array)
	return a
}

// Object returns the object payload of v, or nil when v is not an object.
func (v Value) Object() *Object {
	o, _ := v.R.(*Object)
	return o
}

// Func returns the function payload of v, or nil when v is not a function.
func (v Value) Func() *Func {
	f, _ := v.R.(*Func)
	return f
}

// Time returns the time payload of a temporal value.
func (v Value) Time() time.Time {
	t, _ := v.R.(time.Time)
	return t
}

// RegexValue returns the compiled expression of a regex value.
func (v Value) RegexValue() *regexp.Regexp {
	re, _ := v.R.(*regexp.Regexp)
	return re
}

// RangeBounds returns the inclusive bounds of a range value.
func (v Value) RangeBounds() (int, int) {
	to, _ := v.R.(int)
	return int(v.N), to
}

// TypeName returns the DataWeave type name of v.
func (v Value) TypeName() string { return v.K.String() }

// IsInteger reports whether a number value has no fractional part.
func (v Value) IsInteger() bool {
	return v.K == KindNumber && v.N == math.Trunc(v.N) && !math.IsInf(v.N, 0)
}

// NumberText returns the canonical text of a number value, preferring the
// exact source text when it is known.
func (v Value) NumberText() string {
	if v.S != "" {
		return v.S
	}
	return FormatNumber(v.N)
}

// FormatNumber renders a float without exponent notation.
func FormatNumber(f float64) string {
	switch {
	case f == 0:
		return "0"
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	}
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// ParseNumber parses DataWeave number text, returning ok=false when the text
// is not a valid number. The exact text is preserved on the returned value.
func ParseNumber(s string) (Value, bool) {
	if !looksNumeric(s) {
		return Null, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return Null, false
	}
	return NumRaw(f, s), true
}

func looksNumeric(s string) bool {
	i := skipSign(s, 0)
	i, intDigits := skipDigits(s, i)
	fracDigits := 0
	if i < len(s) && s[i] == '.' {
		i, fracDigits = skipDigits(s, i+1)
	}
	if intDigits+fracDigits == 0 {
		return false
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		var expDigits int
		i, expDigits = skipDigits(s, skipSign(s, i+1))
		if expDigits == 0 {
			return false
		}
	}
	return i == len(s)
}

// skipSign skips an optional sign at i.
func skipSign(s string, i int) int {
	if i < len(s) && (s[i] == '-' || s[i] == '+') {
		return i + 1
	}
	return i
}

// skipDigits skips decimal digits from i and returns the new index and the
// number of digits.
func skipDigits(s string, i int) (int, int) {
	start := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return i, i - start
}
