package value

import (
	"errors"
	"testing"
	"time"
)

func TestNumberText(t *testing.T) {
	tenth := 0.1
	tests := []struct {
		v    Value
		want string
	}{
		{Num(1), "1"},
		{Num(-2.5), "-2.5"},
		{Num(tenth + 2*tenth), "0.30000000000000004"},
		{Num(1e20), "100000000000000000000"},
		{NumRaw(7490, "7490.0000"), "7490.0000"},
		{Num(0), "0"},
	}
	for _, tc := range tests {
		if got := tc.v.NumberText(); got != tc.want {
			t.Errorf("NumberText(%v) = %q, want %q", tc.v.N, got, tc.want)
		}
	}
	for _, s := range []string{"1", "-1.5", "2e3", "+4", "0.5E-2"} {
		if _, ok := ParseNumber(s); !ok {
			t.Errorf("ParseNumber(%q) failed", s)
		}
	}
	for _, s := range []string{"", "-", ".", "1e", "0x10", "NaN", "1_000", "abc"} {
		if _, ok := ParseNumber(s); ok {
			t.Errorf("ParseNumber(%q) should fail", s)
		}
	}
}

func TestEqualAndCompare(t *testing.T) {
	a := ObjectValue(NewObject([]string{"x", "y"}, []Value{Int(1), Str("a")}))
	b := ObjectValue(NewObject([]string{"y", "x"}, []Value{Str("a"), Int(1)}))
	if eq, _ := Equal(a, b); !eq {
		t.Error("objects with the same fields in a different order should be equal")
	}
	if eq, _ := Equal(Str("1"), Int(1)); eq {
		t.Error("string and number should not be equal")
	}
	if eq, _ := Equal(NewArray([]Value{Int(1)}), NewArray([]Value{Int(1), Int(2)})); eq {
		t.Error("arrays of different length should not be equal")
	}
	if c, ok := Compare(Str("a"), Str("b")); !ok || c >= 0 {
		t.Error("string comparison failed")
	}
	if _, ok := Compare(Str("a"), Int(1)); ok {
		t.Error("string and number should not be comparable")
	}
	if SortCompare(Null, Int(1)) >= 0 || SortCompare(Int(2), Str("a")) >= 0 {
		t.Error("SortCompare should order by kind")
	}
}

// counter returns a stream of the numbers 0..n-1 and a count of how many
// times it was iterated.
func counter(n int, restartable bool) (*Array, *int) {
	runs := 0
	return NewStream(func(yield func(Value) error) error {
		runs++
		for i := 0; i < n; i++ {
			if err := yield(Int(i)); err != nil {
				return err
			}
		}
		return nil
	}, restartable).Array(), &runs
}

func TestOneShotStream(t *testing.T) {
	s, runs := counter(5, false)
	empty, err := s.IsEmpty()
	if err != nil || empty {
		t.Fatalf("IsEmpty = %v, %v", empty, err)
	}
	if _, err := s.Len(); !errors.Is(err, ErrConsumed) {
		t.Fatalf("second pass of a one-shot stream should fail, got %v", err)
	}
	if *runs != 1 {
		t.Fatalf("one-shot stream ran %d times", *runs)
	}
}

func TestRestartableStream(t *testing.T) {
	r, runs := counter(3, true)
	if n, err := r.Len(); err != nil || n != 3 {
		t.Fatalf("Len = %d, %v", n, err)
	}
	if v, ok, err := r.At(-1); err != nil || !ok || v.N != 2 {
		t.Fatalf("At(-1) = %v, %v, %v", v, ok, err)
	}
	if r.IsStream() || *runs != 2 {
		t.Fatalf("negative index should materialize the stream (runs=%d)", *runs)
	}
}

func TestObjects(t *testing.T) {
	keys := []string{"a", "b", "a"}
	o := NewObject(keys, []Value{Int(1), Int(2), Int(3)})
	if v, _ := o.Get("a"); v.N != 1 {
		t.Error("Get should return the first match")
	}
	if all := o.GetAll("a"); len(all) != 2 {
		t.Error("GetAll should return every match")
	}
	wide := make([]string, 40)
	vals := make([]Value, 40)
	for i := range wide {
		wide[i] = string(rune('A' + i))
		vals[i] = Int(i)
	}
	w := NewShaped(NewShape(wide), vals)
	if v, ok := w.Get(string(rune('A' + 39))); !ok || v.N != 39 {
		t.Error("indexed lookup failed")
	}
	row := NewPackedRow(NewShape([]string{"x", "y", "z"}), "onetwo", []uint32{3, 6})
	if row.Len() != 2 || row.Val(1).S != "two" {
		t.Errorf("packed row = %d %q", row.Len(), row.Val(1).S)
	}
	if _, ok := row.Get("z"); ok {
		t.Error("missing trailing field should not be found")
	}
	b := NewBuilder(1)
	b.Add("k", True)
	b.AddObject(o)
	if b.Len() != 4 || len(b.Build().Object().Fields()) != 4 {
		t.Error("builder lost fields")
	}
}

func TestTemporalFormatting(t *testing.T) {
	ts := time.Date(2023, 5, 1, 10, 20, 30, 123000000, time.FixedZone("", 7*3600))
	tests := map[Kind]string{
		KindDate:          "2023-05-01",
		KindDateTime:      "2023-05-01T10:20:30.123+07:00",
		KindLocalDateTime: "2023-05-01T10:20:30.123",
		KindTime:          "10:20:30.123+07:00",
		KindLocalTime:     "10:20:30.123",
		KindTimeZone:      "+07:00",
	}
	for k, want := range tests {
		if got := FormatTemporal(k, ts); got != want {
			t.Errorf("%s: got %q, want %q", k, got, want)
		}
	}
	p, err := ParsePeriod("P1Y2M3DT4H5M6S")
	if err != nil {
		t.Fatal(err)
	}
	if p.String() != "P1Y2M3DT4H5M6S" {
		t.Errorf("period = %s", p)
	}
	if got := p.AddTo(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), false); got.Format(time.RFC3339) != "2021-03-04T04:05:06Z" {
		t.Errorf("AddTo = %s", got)
	}
	if _, err := ParsePeriod("P1X"); err == nil {
		t.Error("invalid period should fail")
	}
}

func TestToString(t *testing.T) {
	if s, _ := ToString(True); s != "true" {
		t.Error(s)
	}
	if s, _ := ToString(Range(1, 3)); s != "1 to 3" {
		t.Error(s)
	}
	if _, err := ToString(EmptyObject()); err == nil {
		t.Error("objects cannot become strings")
	}
	if Kind(200).String() != "Unknown" || KindArray.String() != "Array" {
		t.Error("kind names")
	}
}
