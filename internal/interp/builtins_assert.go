package interp

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/touno-io/go-tsunami-cli/internal/codec"
	"github.com/touno-io/go-tsunami-cli/internal/value"
)

// registerAsserts provides the dw::test::Asserts matchers used by MUnit
// assertion scripts such as "payload must equalTo(expected)".
func registerAsserts() {
	register("equalTo", 1, 2, equalTo)
	register("notNullValue", 0, 0, func([]value.Value) (value.Value, error) {
		return matcher("notNullValue", func(v value.Value) (bool, string, error) {
			return !v.IsNull(), "expected a non-null value", nil
		}), nil
	})
	register("nullValue", 0, 0, func([]value.Value) (value.Value, error) {
		return matcher("nullValue", func(v value.Value) (bool, string, error) {
			return v.IsNull(), "expected null but was " + dwText(v), nil
		}), nil
	})
	register("must", 2, 2, must)
}

// matcher wraps a test as a function that fails with a message.
func matcher(name string, test func(v value.Value) (bool, string, error)) value.Value {
	return value.FuncValue(&value.Func{Name: name, Call: func(a []value.Value) (value.Value, error) {
		ok, msg, err := test(arg(a, 0))
		if err != nil {
			return value.Null, err
		}
		if !ok {
			return value.Null, errors.New(msg)
		}
		return value.True, nil
	}})
}

func equalTo(a []value.Value) (value.Value, error) {
	expected := a[0]
	return matcher("equalTo", func(v value.Value) (bool, string, error) {
		eq, err := value.Equal(v, expected)
		if err != nil || eq {
			return eq, "", err
		}
		return false, fmt.Sprintf("expected %s but was %s", dwText(expected), dwText(v)), nil
	}), nil
}

func must(a []value.Value) (value.Value, error) {
	matchers := []value.Value{a[1]}
	if a[1].K == value.KindArray {
		items, err := a[1].Array().Items()
		if err != nil {
			return value.Null, err
		}
		matchers = items
	}
	for _, m := range matchers {
		if err := applyMatcher(m, a[0]); err != nil {
			return value.Null, err
		}
	}
	return value.True, nil
}

func applyMatcher(m, v value.Value) error {
	fn := m.Func()
	if fn == nil {
		return fmt.Errorf("must expects a matcher, got %s", m.TypeName())
	}
	r, err := fn.Call([]value.Value{v})
	if err != nil {
		return err
	}
	if r.K == value.KindBool && !r.Truthy() {
		return errors.New("assertion failed")
	}
	return nil
}

// dwText renders a value compactly in DataWeave syntax for messages.
func dwText(v value.Value) string {
	var buf bytes.Buffer
	if err := codec.WriteDW(&buf, v, map[string]string{"indent": "false"}); err != nil {
		return v.TypeName()
	}
	s := buf.String()
	if len(s) > 2000 {
		s = s[:2000] + "..."
	}
	return s
}
