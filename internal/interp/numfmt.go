package interp

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/touno-io/go-tsunami-cli/internal/value"
)

// decimalPattern is a parsed java.text.DecimalFormat pattern.
type decimalPattern struct {
	prefix, suffix string
	minInt         int
	grouping       int
	minFrac        int
	maxFrac        int
	percent        bool
}

var (
	decimalCache sync.Map
	decimalCount atomic.Int64
)

// parseDecimalPattern parses and memoizes a DecimalFormat pattern.
func parseDecimalPattern(p string) (decimalPattern, error) {
	if cached, ok := decimalCache.Load(p); ok {
		return cached.(decimalPattern), nil
	}
	dp, err := scanDecimalPattern(p)
	if err == nil && decimalCount.Add(1) <= maxCachedPatterns {
		decimalCache.Store(p, dp)
	}
	return dp, err
}

func scanDecimalPattern(p string) (decimalPattern, error) {
	if i := strings.IndexByte(p, ';'); i >= 0 {
		p = p[:i]
	}
	var dp decimalPattern
	start := strings.IndexAny(p, "#0,.")
	if start < 0 {
		return dp, fmt.Errorf("invalid number format %q", p)
	}
	end := strings.LastIndexAny(p, "#0,.") + 1
	dp.prefix = strings.ReplaceAll(p[:start], "'", "")
	dp.suffix = strings.ReplaceAll(p[end:], "'", "")
	dp.percent = strings.Contains(dp.prefix+dp.suffix, "%")
	body := p[start:end]
	intPart, fracPart, _ := strings.Cut(body, ".")
	dp.minInt = strings.Count(intPart, "0")
	if i := strings.LastIndexByte(intPart, ','); i >= 0 {
		dp.grouping = len(intPart) - i - 1
	}
	dp.minFrac = strings.Count(fracPart, "0")
	dp.maxFrac = dp.minFrac + strings.Count(fracPart, "#")
	return dp, nil
}

// formatDecimal formats a number with a DecimalFormat pattern using decimal
// (not binary) rounding, so 2.675 with "0.00" yields 2.68 under HALF_UP.
func formatDecimal(v value.Value, pattern, roundMode string) (string, error) {
	dp, err := parseDecimalPattern(pattern)
	if err != nil {
		return "", err
	}
	if math.IsNaN(v.N) || math.IsInf(v.N, 0) {
		return value.FormatNumber(v.N), nil
	}
	text := decimalText(v, dp.percent)
	neg := strings.HasPrefix(text, "-")
	intDigits, fracDigits, _ := strings.Cut(strings.TrimLeft(text, "+-"), ".")
	intDigits, fracDigits = roundDecimal(intDigits, fracDigits, dp.maxFrac, neg, strings.ToUpper(roundMode))
	intDigits, fracDigits = dp.shape(intDigits, fracDigits)

	var b strings.Builder
	if neg && strings.Trim(intDigits+fracDigits, "0,") != "" {
		b.WriteByte('-')
	}
	b.WriteString(dp.prefix)
	b.WriteString(intDigits)
	if fracDigits != "" {
		b.WriteByte('.')
		b.WriteString(fracDigits)
	}
	if intDigits == "" && fracDigits == "" {
		b.WriteByte('0')
	}
	b.WriteString(dp.suffix)
	return b.String(), nil
}

// decimalText returns the plain decimal text of v, scaled for percentages.
func decimalText(v value.Value, percent bool) string {
	text := v.NumberText()
	if !percent && !strings.ContainsAny(text, "eE") {
		return text
	}
	n := v.N
	if percent {
		n *= 100
	}
	return strconv.FormatFloat(n, 'f', -1, 64)
}

// shape applies the minimum digit counts and grouping of the pattern.
func (dp decimalPattern) shape(intDigits, fracDigits string) (string, string) {
	fracDigits = strings.TrimRight(fracDigits, "0")
	if len(fracDigits) < dp.minFrac {
		fracDigits += strings.Repeat("0", dp.minFrac-len(fracDigits))
	}
	intDigits = strings.TrimLeft(intDigits, "0")
	if len(intDigits) < dp.minInt {
		intDigits = strings.Repeat("0", dp.minInt-len(intDigits)) + intDigits
	}
	if dp.grouping > 0 && len(intDigits) > dp.grouping {
		intDigits = group(intDigits, dp.grouping)
	}
	return intDigits, fracDigits
}

func group(digits string, size int) string {
	var b strings.Builder
	first := len(digits) % size
	if first > 0 {
		b.WriteString(digits[:first])
	}
	for i := first; i < len(digits); i += size {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(digits[i : i+size])
	}
	return b.String()
}

// roundDecimal rounds the decimal number intDigits.fracDigits to n fraction
// digits. The default mode is HALF_EVEN, matching java.text.DecimalFormat.
func roundDecimal(intDigits, fracDigits string, n int, neg bool, mode string) (string, string) {
	if len(fracDigits) <= n {
		return intDigits, fracDigits
	}
	kept, dropped := fracDigits[:n], fracDigits[n:]
	digits := []byte(intDigits + kept)
	last := byte('0')
	if len(digits) > 0 {
		last = digits[len(digits)-1]
	}
	if roundsUp(mode, dropped, last, neg) {
		digits = increment(digits)
	}
	split := len(digits) - n
	return string(digits[:split]), string(digits[split:])
}

// roundsUp decides whether discarding dropped (the digits after the kept
// ones) increments the last kept digit.
func roundsUp(mode, dropped string, last byte, neg bool) bool {
	first := dropped[0]
	restNonZero := strings.Trim(dropped[1:], "0") != ""
	anyNonZero := first != '0' || restNonZero
	switch mode {
	case "UP":
		return anyNonZero
	case "DOWN":
		return false
	case "CEILING":
		return anyNonZero && !neg
	case "FLOOR":
		return anyNonZero && neg
	case "HALF_UP":
		return first >= '5'
	case "HALF_DOWN":
		return first > '5' || (first == '5' && restNonZero)
	}
	return first > '5' || (first == '5' && (restNonZero || (last-'0')%2 == 1))
}

// increment adds one to a decimal digit string.
func increment(digits []byte) []byte {
	for i := len(digits) - 1; i >= 0; i-- {
		if digits[i] != '9' {
			digits[i]++
			return digits
		}
		digits[i] = '0'
	}
	return append([]byte{'1'}, digits...)
}
