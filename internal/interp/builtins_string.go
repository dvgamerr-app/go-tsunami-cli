package interp

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/touno-io/go-tsunami-cli/internal/value"
)

// stringFn adapts a single-string function that maps null to null.
func stringFn(name string, impl func(s string) value.Value) builtinImpl {
	return func(a []value.Value) (value.Value, error) {
		if a[0].IsNull() {
			return value.Null, nil
		}
		s, err := strArg(a, 0, name)
		if err != nil {
			return value.Null, err
		}
		return impl(s), nil
	}
}

// string2Fn adapts a (string, string) function that maps a null subject to
// the given default.
func string2Fn(name string, onNull value.Value, impl func(s, t string) value.Value) builtinImpl {
	return func(a []value.Value) (value.Value, error) {
		if a[0].IsNull() {
			return onNull, nil
		}
		s, err := strArg(a, 0, name)
		if err != nil {
			return value.Null, err
		}
		t, err := strArg(a, 1, name)
		if err != nil {
			return value.Null, err
		}
		return impl(s, t), nil
	}
}

// stringNumFn adapts a (string, number) function that maps null to null.
func stringNumFn(name string, impl func(s string, n int) value.Value) builtinImpl {
	return func(a []value.Value) (value.Value, error) {
		if a[0].IsNull() {
			return value.Null, nil
		}
		s, err := strArg(a, 0, name)
		if err != nil {
			return value.Null, err
		}
		n, err := numArg(a, 1, name)
		if err != nil {
			return value.Null, err
		}
		return impl(s, int(n)), nil
	}
}

func boolString(test func(r rune) bool) func(s string) value.Value {
	return func(s string) value.Value {
		if s == "" {
			return value.False
		}
		for _, r := range s {
			if !test(r) {
				return value.False
			}
		}
		return value.True
	}
}

func strValue(f func(string) string) func(string) value.Value {
	return func(s string) value.Value { return value.Str(f(s)) }
}

func registerStrings() {
	register("upper", 1, 1, stringFn("upper", strValue(strings.ToUpper)))
	register("lower", 1, 1, stringFn("lower", strValue(strings.ToLower)))
	register("trim", 1, 1, stringFn("trim", strValue(strings.TrimSpace)))
	register("capitalize", 1, 1, stringFn("capitalize", strValue(capitalize)))
	register("camelize", 1, 1, stringFn("camelize", strValue(camelize)))
	register("dasherize", 1, 1, stringFn("dasherize", strValue(func(s string) string {
		return strings.ToLower(strings.Join(splitWords(s), "-"))
	})))
	register("underscore", 1, 1, stringFn("underscore", strValue(func(s string) string {
		return strings.ToLower(strings.Join(splitWords(s), "_"))
	})))
	register("reverse", 1, 1, stringFn("reverse", strValue(reverseString)))
	register("isNumeric", 1, 1, stringFn("isNumeric", boolString(unicode.IsDigit)))
	register("isAlpha", 1, 1, stringFn("isAlpha", boolString(unicode.IsLetter)))
	register("isAlphanumeric", 1, 1, stringFn("isAlphanumeric", boolString(isAlphanumeric)))
	register("isLowerCase", 1, 1, stringFn("isLowerCase", boolString(unicode.IsLower)))
	register("isUpperCase", 1, 1, stringFn("isUpperCase", boolString(unicode.IsUpper)))
	register("isWhitespace", 1, 1, stringFn("isWhitespace", boolString(unicode.IsSpace)))
	register("charCode", 1, 1, stringFn("charCode", charCode))
	register("charCodeAt", 2, 2, stringNumFn("charCodeAt", charCodeAt))
	register("fromCharCode", 1, 1, fromCharCode)
	register("ordinalize", 1, 1, ordinalize)
	register("startsWith", 2, 2, string2Fn("startsWith", value.False, func(s, t string) value.Value {
		return value.Bool(strings.HasPrefix(s, t))
	}))
	register("endsWith", 2, 2, string2Fn("endsWith", value.False, func(s, t string) value.Value {
		return value.Bool(strings.HasSuffix(s, t))
	}))
	register("substringBefore", 2, 2, string2Fn("substringBefore", value.Null, substringBefore(strings.Index)))
	register("substringBeforeLast", 2, 2, string2Fn("substringBeforeLast", value.Null, substringBefore(strings.LastIndex)))
	register("substringAfter", 2, 2, string2Fn("substringAfter", value.Null, substringAfter(strings.Index)))
	register("substringAfterLast", 2, 2, string2Fn("substringAfterLast", value.Null, substringAfter(strings.LastIndex)))
	register("appendIfMissing", 2, 2, string2Fn("appendIfMissing", value.Null, appendIfMissing))
	register("prependIfMissing", 2, 2, string2Fn("prependIfMissing", value.Null, prependIfMissing))
	register("wrapWith", 2, 2, string2Fn("wrapWith", value.Null, func(s, t string) value.Value {
		return value.Str(t + s + t)
	}))
	register("wrapIfMissing", 2, 2, string2Fn("wrapIfMissing", value.Null, func(s, t string) value.Value {
		return prependIfMissing(appendIfMissing(s, t).S, t)
	}))
	register("unwrap", 2, 2, string2Fn("unwrap", value.Null, unwrap))
	register("remove", 2, 2, string2Fn("remove", value.Null, func(s, t string) value.Value {
		return value.Str(strings.ReplaceAll(s, t, ""))
	}))
	register("substring", 3, 3, substring)
	register("withMaxSize", 2, 2, stringNumFn("withMaxSize", withMaxSize))
	register("repeat", 2, 2, stringNumFn("repeat", func(s string, n int) value.Value {
		return value.Str(strings.Repeat(s, max(0, n)))
	}))
	register("leftPad", 2, 3, func(a []value.Value) (value.Value, error) { return padString(a, "leftPad", true) })
	register("rightPad", 2, 3, func(a []value.Value) (value.Value, error) { return padString(a, "rightPad", false) })
	register("splitBy", 2, 2, splitBy)
	register("replace", 2, 3, replaceBuiltin)
	register("match", 2, 2, matchBuiltin)
	register("matches", 2, 2, matchesBuiltin)
	register("scan", 2, 2, scanBuiltin)
}

func capitalize(s string) string {
	words := splitWords(s)
	for i, w := range words {
		words[i] = titleWord(w)
	}
	return strings.Join(words, " ")
}

func camelize(s string) string {
	words := strings.FieldsFunc(s, func(r rune) bool { return r == '_' || r == '-' || r == ' ' })
	for i, w := range words {
		if i > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	out := strings.Join(words, "")
	if out == "" {
		return out
	}
	return strings.ToLower(out[:1]) + out[1:]
}

func reverseString(s string) string {
	r := []rune(s)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r)
}

func isAlphanumeric(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

func charCode(s string) value.Value {
	for _, r := range s {
		return value.Int(int(r))
	}
	return value.Null
}

func charCodeAt(s string, i int) value.Value {
	r := []rune(s)
	if i < 0 || i >= len(r) {
		return value.Null
	}
	return value.Int(int(r[i]))
}

func fromCharCode(a []value.Value) (value.Value, error) {
	n, err := numArg(a, 0, "fromCharCode")
	if err != nil {
		return value.Null, err
	}
	return value.Str(string(rune(int(n)))), nil
}

func ordinalize(a []value.Value) (value.Value, error) {
	if a[0].IsNull() {
		return value.Null, nil
	}
	n, err := numArg(a, 0, "ordinalize")
	if err != nil {
		return value.Null, err
	}
	i := int(n)
	suffix := "th"
	if i%100 < 11 || i%100 > 13 {
		suffix = [...]string{"th", "st", "nd", "rd", "th", "th", "th", "th", "th", "th"}[abs(i%10)]
	}
	return value.Str(strconv.Itoa(i) + suffix), nil
}

func abs(i int) int {
	if i < 0 {
		return -i
	}
	return i
}

func substringBefore(find func(s, t string) int) func(s, t string) value.Value {
	return func(s, t string) value.Value {
		if i := find(s, t); i >= 0 {
			return value.Str(s[:i])
		}
		return value.Str("")
	}
}

func substringAfter(find func(s, t string) int) func(s, t string) value.Value {
	return func(s, t string) value.Value {
		if i := find(s, t); i >= 0 {
			return value.Str(s[i+len(t):])
		}
		return value.Str("")
	}
}

func appendIfMissing(s, t string) value.Value {
	if strings.HasSuffix(s, t) {
		return value.Str(s)
	}
	return value.Str(s + t)
}

func prependIfMissing(s, t string) value.Value {
	if strings.HasPrefix(s, t) {
		return value.Str(s)
	}
	return value.Str(t + s)
}

func unwrap(s, t string) value.Value {
	if len(s) >= 2*len(t) && strings.HasPrefix(s, t) && strings.HasSuffix(s, t) {
		return value.Str(s[len(t) : len(s)-len(t)])
	}
	return value.Str(s)
}

func withMaxSize(s string, n int) value.Value {
	if r := []rune(s); len(r) > n {
		return value.Str(string(r[:max(0, n)]))
	}
	return value.Str(s)
}

func substring(a []value.Value) (value.Value, error) {
	if a[0].IsNull() {
		return value.Null, nil
	}
	s, err := strArg(a, 0, "substring")
	if err != nil {
		return value.Null, err
	}
	from, err := numArg(a, 1, "substring")
	if err != nil {
		return value.Null, err
	}
	to, err := numArg(a, 2, "substring")
	if err != nil {
		return value.Null, err
	}
	r := []rune(s)
	f := max(0, min(int(from), len(r)))
	t := max(f, min(int(to), len(r)))
	return value.Str(string(r[f:t])), nil
}

func splitBy(a []value.Value) (value.Value, error) {
	if a[0].IsNull() {
		return value.Null, nil
	}
	s, err := strArg(a, 0, "splitBy")
	if err != nil {
		return value.Null, err
	}
	var parts []string
	if a[1].K == value.KindRegex {
		parts = a[1].RegexValue().Split(s, -1)
	} else {
		sep, err := strArg(a, 1, "splitBy")
		if err != nil {
			return value.Null, err
		}
		parts = splitString(s, sep)
	}
	return stringsToArray(trimTrailingEmpty(parts, s)), nil
}

func splitString(s, sep string) []string {
	if sep == "" {
		return splitRunes(s)
	}
	return strings.Split(s, sep)
}

// trimTrailingEmpty drops trailing empty strings like Java's String.split,
// keeping a single empty element only when the input itself is empty.
func trimTrailingEmpty(parts []string, s string) []string {
	for len(parts) > 1 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) == 1 && parts[0] == "" && s != "" {
		return nil
	}
	return parts
}

func matchBuiltin(a []value.Value) (value.Value, error) {
	if a[0].IsNull() {
		return value.Null, nil
	}
	s, re, err := stringRegex(a, "match")
	if err != nil {
		return value.Null, err
	}
	m := re.FindStringSubmatch(s)
	if m == nil {
		return value.EmptyArray(), nil
	}
	return stringsToArray(m), nil
}

func matchesBuiltin(a []value.Value) (value.Value, error) {
	if a[0].IsNull() {
		return value.False, nil
	}
	s, re, err := stringRegex(a, "matches")
	if err != nil {
		return value.Null, err
	}
	return value.Bool(fullMatch(re, s)), nil
}

func scanBuiltin(a []value.Value) (value.Value, error) {
	if a[0].IsNull() {
		return value.Null, nil
	}
	s, re, err := stringRegex(a, "scan")
	if err != nil {
		return value.Null, err
	}
	all := re.FindAllStringSubmatch(s, -1)
	out := make([]value.Value, len(all))
	for i, m := range all {
		out[i] = stringsToArray(m)
	}
	return value.NewArray(out), nil
}

// fullMatch reports whether re matches all of s. An anchored copy is used
// because leftmost-first matching may stop at a shorter alternative.
func fullMatch(re *regexp.Regexp, s string) bool {
	if loc := re.FindStringIndex(s); loc != nil && loc[0] == 0 && loc[1] == len(s) {
		return true
	}
	anchored, err := regexp.Compile(`^(?:` + re.String() + `)$`)
	return err == nil && anchored.MatchString(s)
}

func stringRegex(a []value.Value, name string) (string, *regexp.Regexp, error) {
	s, err := strArg(a, 0, name)
	if err != nil {
		return "", nil, err
	}
	if a[1].K == value.KindRegex {
		return s, a[1].RegexValue(), nil
	}
	pat, err := strArg(a, 1, name)
	if err != nil {
		return "", nil, err
	}
	re, err := regexp.Compile(regexp.QuoteMeta(pat))
	return s, re, err
}

func replaceBuiltin(a []value.Value) (value.Value, error) {
	if a[0].IsNull() {
		return value.Null, nil
	}
	if len(a) < 3 {
		return value.Null, fmt.Errorf("replace requires a 'with' replacement")
	}
	s, err := strArg(a, 0, "replace")
	if err != nil {
		return value.Null, err
	}
	if fn := a[2].Func(); fn != nil {
		return replaceWithFunc(a, s, fn)
	}
	with, err := strArg(a, 2, "replace")
	if err != nil {
		return value.Null, err
	}
	if a[1].K == value.KindRegex {
		return value.Str(a[1].RegexValue().ReplaceAllString(s, with)), nil
	}
	old, err := strArg(a, 1, "replace")
	if err != nil {
		return value.Null, err
	}
	return value.Str(strings.ReplaceAll(s, old, with)), nil
}

// replaceWithFunc replaces each match with the result of fn applied to the
// match and its groups.
func replaceWithFunc(a []value.Value, s string, fn *value.Func) (value.Value, error) {
	_, re, err := stringRegex(a, "replace")
	if err != nil {
		return value.Null, err
	}
	var ferr error
	out := re.ReplaceAllStringFunc(s, func(m string) string {
		if ferr != nil {
			return m
		}
		r, err := fn.Call([]value.Value{stringsToArray(re.FindStringSubmatch(m))})
		if err == nil {
			var t string
			if t, err = value.ToString(r); err == nil {
				return t
			}
		}
		ferr = err
		return m
	})
	return value.Str(out), ferr
}

func padString(a []value.Value, name string, left bool) (value.Value, error) {
	if a[0].IsNull() {
		return value.Null, nil
	}
	s, err := strArg(a, 0, name)
	if err != nil {
		return value.Null, err
	}
	n, err := numArg(a, 1, name)
	if err != nil {
		return value.Null, err
	}
	padChar := " "
	if len(a) > 2 {
		if padChar, err = strArg(a, 2, name); err != nil {
			return value.Null, err
		}
	}
	missing := int(n) - len([]rune(s))
	if missing <= 0 || padChar == "" {
		return value.Str(s), nil
	}
	fill := string([]rune(strings.Repeat(padChar, missing))[:missing])
	if left {
		return value.Str(fill + s), nil
	}
	return value.Str(s + fill), nil
}

// splitWords splits identifiers such as "customer_first-name" or
// "customerFirstName" into words.
func splitWords(s string) []string {
	var words []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			words = append(words, string(cur))
			cur = cur[:0]
		}
	}
	runes := []rune(s)
	for i, r := range runes {
		switch {
		case r == '_' || r == '-' || unicode.IsSpace(r):
			flush()
		case startsWord(runes, i, cur):
			flush()
			cur = append(cur, r)
		default:
			cur = append(cur, r)
		}
	}
	flush()
	return words
}

// startsWord reports whether the upper-case rune at i begins a new camel
// case word.
func startsWord(runes []rune, i int, cur []rune) bool {
	if !unicode.IsUpper(runes[i]) || len(cur) == 0 {
		return false
	}
	return unicode.IsLower(cur[len(cur)-1]) || i+1 < len(runes) && unicode.IsLower(runes[i+1])
}

func titleWord(w string) string {
	r := []rune(strings.ToLower(w))
	if len(r) > 0 {
		r[0] = unicode.ToUpper(r[0])
	}
	return string(r)
}
