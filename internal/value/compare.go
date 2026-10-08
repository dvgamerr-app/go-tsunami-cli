package value

import (
	"fmt"
	"strings"
)

// Equal reports whether a and b are structurally equal. Objects compare
// without regard to field order.
func Equal(a, b Value) (bool, error) {
	if a.K != b.K {
		return false, nil
	}
	switch a.K {
	case KindArray:
		return equalArrays(a.Array(), b.Array())
	case KindObject:
		return equalObjects(a.Object(), b.Object())
	}
	return equalScalars(a, b), nil
}

// equalScalars compares two non-container values of the same kind.
func equalScalars(a, b Value) bool {
	switch a.K {
	case KindNull:
		return true
	case KindBool, KindNumber:
		return a.N == b.N
	case KindString, KindType, KindRegex:
		return a.S == b.S
	case KindRange, KindPeriod:
		return a.N == b.N && a.R == b.R
	case KindFunction:
		return a.Func() == b.Func()
	}
	return a.K.IsTemporal() && a.Time().Equal(b.Time())
}

func equalArrays(a, b *Array) (bool, error) {
	ai, err := a.Items()
	if err != nil {
		return false, err
	}
	bi, err := b.Items()
	if err != nil {
		return false, err
	}
	if len(ai) != len(bi) {
		return false, nil
	}
	for i := range ai {
		eq, err := Equal(ai[i], bi[i])
		if err != nil || !eq {
			return false, err
		}
	}
	return true, nil
}

func equalObjects(a, b *Object) (bool, error) {
	n := a.Len()
	if n != b.Len() {
		return false, nil
	}
	used := make([]bool, n)
	for i := 0; i < n; i++ {
		matched, err := claimField(b, used, a.Key(i), a.Val(i))
		if err != nil || !matched {
			return false, err
		}
	}
	return true, nil
}

// claimField finds an unused field of b equal to key: val and marks it as
// used.
func claimField(b *Object, used []bool, key string, val Value) (bool, error) {
	for j := range used {
		if used[j] || b.Key(j) != key {
			continue
		}
		eq, err := Equal(val, b.Val(j))
		if err != nil {
			return false, err
		}
		if eq {
			used[j] = true
			return true, nil
		}
	}
	return false, nil
}

// Compare orders two values of comparable kinds. It returns ok=false when the
// values cannot be ordered against each other.
func Compare(a, b Value) (int, bool) {
	if a.K != b.K {
		return 0, false
	}
	switch a.K {
	case KindNull:
		return 0, true
	case KindBool, KindNumber:
		return cmpFloat(a.N, b.N), true
	case KindString:
		return strings.Compare(a.S, b.S), true
	}
	if a.K.IsTemporal() {
		return a.Time().Compare(b.Time()), true
	}
	return 0, false
}

// SortCompare orders any two values, grouping by kind first, so it can be
// used for stable sorting of heterogeneous arrays.
func SortCompare(a, b Value) int {
	if c, ok := Compare(a, b); ok {
		return c
	}
	if a.K != b.K {
		return cmpFloat(float64(a.K), float64(b.K))
	}
	as, _ := ToString(a)
	bs, _ := ToString(b)
	return strings.Compare(as, bs)
}

func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// ToString converts a scalar to its string form. Null becomes the empty
// string. Structured values cannot be converted.
func ToString(v Value) (string, error) {
	switch v.K {
	case KindNull:
		return "", nil
	case KindString, KindType, KindRegex:
		return v.S, nil
	case KindNumber:
		return v.NumberText(), nil
	case KindBool:
		if v.N != 0 {
			return "true", nil
		}
		return "false", nil
	case KindPeriod:
		return v.Period().String(), nil
	case KindRange:
		from, to := v.RangeBounds()
		return fmt.Sprintf("%d to %d", from, to), nil
	}
	if v.K.IsTemporal() {
		return FormatTemporal(v.K, v.Time()), nil
	}
	return "", fmt.Errorf("cannot coerce %s to String", v.TypeName())
}
