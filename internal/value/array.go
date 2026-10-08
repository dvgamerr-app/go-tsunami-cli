package value

import "errors"

// ErrStop can be returned from an iteration callback to end the iteration
// early without reporting an error.
var ErrStop = errors.New("stop iteration")

// ErrConsumed is returned when a one-shot stream is iterated a second time.
var ErrConsumed = errors.New("the input stream was already consumed; read it into memory or use a seekable input")

// IterFunc produces the elements of a lazy array by calling yield for each
// element. It must stop and return the error when yield returns one.
type IterFunc func(yield func(Value) error) error

// Array is either a materialized slice of values or a lazy stream.
//
// Streams let large inputs flow from a reader to a writer one element at a
// time. A restartable stream can be iterated more than once (for example a
// file that can be re-read); other streams are single use.
type Array struct {
	items       []Value
	iter        IterFunc
	restartable bool
	used        bool
}

// NewArray returns an array value backed by items.
func NewArray(items []Value) Value {
	return Value{K: KindArray, R: &Array{items: items}}
}

// NewStream returns a lazy array value.
func NewStream(iter IterFunc, restartable bool) Value {
	return Value{K: KindArray, R: &Array{iter: iter, restartable: restartable}}
}

// EmptyArray returns a new empty array value.
func EmptyArray() Value {
	return NewArray(nil)
}

// IsStream reports whether the array has not been materialized.
func (a *Array) IsStream() bool { return a.iter != nil }

// Restartable reports whether the array can be iterated repeatedly.
func (a *Array) Restartable() bool { return a.iter == nil || a.restartable }

// Each calls fn for each element in order. Returning ErrStop from fn ends the
// iteration early and Each returns nil.
func (a *Array) Each(fn func(i int, v Value) error) error {
	if a.iter == nil {
		for i, v := range a.items {
			if err := fn(i, v); err != nil {
				if err == ErrStop {
					return nil
				}
				return err
			}
		}
		return nil
	}
	if a.used && !a.restartable {
		return ErrConsumed
	}
	a.used = true
	i := 0
	err := a.iter(func(v Value) error {
		err := fn(i, v)
		i++
		return err
	})
	if err == ErrStop {
		return nil
	}
	return err
}

// Items materializes the array and returns its elements. The result must not
// be modified by the caller.
func (a *Array) Items() ([]Value, error) {
	if a.iter == nil {
		return a.items, nil
	}
	var out []Value
	err := a.Each(func(_ int, v Value) error {
		out = append(out, v)
		return nil
	})
	if err != nil {
		return nil, err
	}
	a.items, a.iter = out, nil
	return out, nil
}

// Len returns the number of elements, counting a stream without keeping its
// elements in memory.
func (a *Array) Len() (int, error) {
	if a.iter == nil {
		return len(a.items), nil
	}
	n := 0
	err := a.Each(func(int, Value) error {
		n++
		return nil
	})
	return n, err
}

// At returns the element at index i. Negative indexes count from the end.
func (a *Array) At(i int) (Value, bool, error) {
	if i < 0 || a.iter == nil {
		items, err := a.Items()
		if err != nil {
			return Null, false, err
		}
		if i < 0 {
			i += len(items)
		}
		if i < 0 || i >= len(items) {
			return Null, false, nil
		}
		return items[i], true, nil
	}
	var out Value
	found := false
	err := a.Each(func(j int, v Value) error {
		if j == i {
			out, found = v, true
			return ErrStop
		}
		return nil
	})
	return out, found, err
}

// IsEmpty reports whether the array has no elements, reading at most one
// element of a stream.
func (a *Array) IsEmpty() (bool, error) {
	if a.iter == nil {
		return len(a.items) == 0, nil
	}
	empty := true
	err := a.Each(func(int, Value) error {
		empty = false
		return ErrStop
	})
	return empty, err
}
