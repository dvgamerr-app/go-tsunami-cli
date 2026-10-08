package interp

import (
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	"github.com/touno-io/go-tsunami-cli/internal/value"
)

// mathFn adapts a unary numeric function that maps null to null.
func mathFn(name string, f func(float64) float64) builtinImpl {
	return func(a []value.Value) (value.Value, error) {
		if a[0].IsNull() {
			return value.Null, nil
		}
		n, err := numArg(a, 0, name)
		if err != nil {
			return value.Null, err
		}
		return value.Num(f(n)), nil
	}
}

// numTest adapts a numeric predicate.
func numTest(name string, test func(float64) bool) builtinImpl {
	return func(a []value.Value) (value.Value, error) {
		n, err := numArg(a, 0, name)
		if err != nil {
			return value.Null, err
		}
		return value.Bool(test(n)), nil
	}
}

// dayFn returns a builtin for the date offset days from today.
func dayFn(offset int) builtinImpl {
	return func([]value.Value) (value.Value, error) {
		t := time.Now().AddDate(0, 0, offset)
		return value.Temporal(value.KindDate, time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)), nil
	}
}

func registerNumbers() {
	register("abs", 1, 1, mathFn("abs", math.Abs))
	register("ceil", 1, 1, mathFn("ceil", math.Ceil))
	register("floor", 1, 1, mathFn("floor", math.Floor))
	register("round", 1, 1, mathFn("round", func(f float64) float64 { return math.Floor(f + 0.5) }))
	register("sqrt", 1, 1, mathFn("sqrt", math.Sqrt))
	register("pow", 2, 2, pow)
	register("mod", 2, 2, mod)
	register("isEven", 1, 1, numTest("isEven", func(n float64) bool { return math.Mod(n, 2) == 0 }))
	register("isOdd", 1, 1, numTest("isOdd", func(n float64) bool { return math.Abs(math.Mod(n, 2)) == 1 }))
	register("isInteger", 1, 1, numTest("isInteger", func(n float64) bool { return n == math.Trunc(n) }))
	register("isDecimal", 1, 1, numTest("isDecimal", func(n float64) bool { return n != math.Trunc(n) }))
	register("random", 0, 0, func([]value.Value) (value.Value, error) { return value.Num(rand.Float64()), nil })
	register("randomInt", 1, 1, randomInt)
	register("today", 0, 0, dayFn(0))
	register("tomorrow", 0, 0, dayFn(1))
	register("yesterday", 0, 0, dayFn(-1))
	register("daysBetween", 2, 2, daysBetween)
	register("isLeapYear", 1, 1, isLeapYear)
}

func pow(a []value.Value) (value.Value, error) {
	x, err := numArg(a, 0, "pow")
	if err != nil {
		return value.Null, err
	}
	y, err := numArg(a, 1, "pow")
	return value.Num(math.Pow(x, y)), err
}

func mod(a []value.Value) (value.Value, error) {
	x, err := numArg(a, 0, "mod")
	if err != nil {
		return value.Null, err
	}
	y, err := numArg(a, 1, "mod")
	if err != nil {
		return value.Null, err
	}
	if y == 0 {
		return value.Null, fmt.Errorf("division by zero")
	}
	return value.Num(math.Mod(x, y)), nil
}

func randomInt(a []value.Value) (value.Value, error) {
	n, err := numArg(a, 0, "randomInt")
	if err != nil || n < 1 {
		return value.Null, fmt.Errorf("function randomInt expects a positive number")
	}
	return value.Int(rand.IntN(int(n))), nil
}

func daysBetween(a []value.Value) (value.Value, error) {
	if !a[0].K.IsTemporal() || !a[1].K.IsTemporal() {
		return value.Null, fmt.Errorf("function daysBetween expects two dates")
	}
	f, t := a[0].Time(), a[1].Time()
	fd := time.Date(f.Year(), f.Month(), f.Day(), 0, 0, 0, 0, time.UTC)
	td := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return value.Int(int(td.Sub(fd).Hours() / 24)), nil
}

func isLeapYear(a []value.Value) (value.Value, error) {
	var y int
	switch {
	case a[0].K.IsTemporal():
		y = a[0].Time().Year()
	case a[0].K == value.KindNumber:
		y = int(a[0].N)
	default:
		return value.Null, fmt.Errorf("function isLeapYear expects a date or year")
	}
	return value.Bool(y%4 == 0 && (y%100 != 0 || y%400 == 0)), nil
}
