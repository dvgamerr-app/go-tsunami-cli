package value

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Period is a calendar-aware duration such as P1Y2M3DT4H.
type Period struct {
	Years, Months, Days int
	Dur                 time.Duration
}

// PeriodValue wraps a period.
func PeriodValue(p Period) Value {
	return Value{K: KindPeriod, R: p}
}

// Period returns the period payload of v.
func (v Value) Period() Period {
	p, _ := v.R.(Period)
	return p
}

// String renders the period in ISO-8601 form.
func (p Period) String() string {
	var b strings.Builder
	b.WriteByte('P')
	if p.Years != 0 {
		b.WriteString(strconv.Itoa(p.Years) + "Y")
	}
	if p.Months != 0 {
		b.WriteString(strconv.Itoa(p.Months) + "M")
	}
	if p.Days != 0 {
		b.WriteString(strconv.Itoa(p.Days) + "D")
	}
	if p.Dur != 0 {
		b.WriteByte('T')
		d := p.Dur
		if h := d / time.Hour; h != 0 {
			b.WriteString(strconv.FormatInt(int64(h), 10) + "H")
			d -= h * time.Hour
		}
		if m := d / time.Minute; m != 0 {
			b.WriteString(strconv.FormatInt(int64(m), 10) + "M")
			d -= m * time.Minute
		}
		if d != 0 {
			b.WriteString(FormatNumber(d.Seconds()) + "S")
		}
	}
	if b.Len() == 1 {
		return "PT0S"
	}
	return b.String()
}

// AddTo applies the period to t, optionally negated.
func (p Period) AddTo(t time.Time, negate bool) time.Time {
	sign := 1
	if negate {
		sign = -1
	}
	t = t.AddDate(sign*p.Years, sign*p.Months, sign*p.Days)
	return t.Add(time.Duration(sign) * p.Dur)
}

// ParsePeriod parses an ISO-8601 period such as P1D or PT2H30M.
func ParsePeriod(s string) (Period, error) {
	var p Period
	invalid := fmt.Errorf("invalid period %q", s)
	if len(s) < 2 || (s[0] != 'P' && s[0] != 'p') {
		return p, invalid
	}
	inTime := false
	num := ""
	for _, c := range s[1:] {
		switch {
		case c == 'T' || c == 't':
			inTime = true
		case (c >= '0' && c <= '9') || c == '.' || c == '-':
			num += string(c)
		default:
			f, err := strconv.ParseFloat(num, 64)
			if err != nil || !p.add(c, f, inTime) {
				return p, invalid
			}
			num = ""
		}
	}
	if num != "" {
		return p, invalid
	}
	return p, nil
}

// add applies amount f of the period unit c, reporting whether the unit is
// valid in the date or time part.
func (p *Period) add(c rune, f float64, inTime bool) bool {
	if inTime {
		unit, ok := map[rune]time.Duration{'H': time.Hour, 'M': time.Minute, 'S': time.Second}[c]
		p.Dur += time.Duration(f * float64(unit))
		return ok
	}
	switch c {
	case 'Y':
		p.Years = int(f)
	case 'M':
		p.Months = int(f)
	case 'W':
		p.Days += int(f) * 7
	case 'D':
		p.Days += int(f)
	default:
		return false
	}
	return true
}

// FormatTemporal renders a temporal value in its default ISO form.
func FormatTemporal(k Kind, t time.Time) string {
	var b []byte
	switch k {
	case KindDate:
		b = t.AppendFormat(b, "2006-01-02")
	case KindDateTime:
		b = t.AppendFormat(b, "2006-01-02T15:04:05")
		b = appendFraction(b, t.Nanosecond())
		b = appendZone(b, t)
	case KindLocalDateTime:
		b = t.AppendFormat(b, "2006-01-02T15:04:05")
		b = appendFraction(b, t.Nanosecond())
	case KindTime:
		b = t.AppendFormat(b, "15:04:05")
		b = appendFraction(b, t.Nanosecond())
		b = appendZone(b, t)
	case KindLocalTime:
		b = t.AppendFormat(b, "15:04:05")
		b = appendFraction(b, t.Nanosecond())
	case KindTimeZone:
		b = appendZone(b, t)
	}
	return string(b)
}

func appendFraction(b []byte, ns int) []byte {
	if ns == 0 {
		return b
	}
	digits := 9
	switch {
	case ns%1e6 == 0:
		ns /= 1e6
		digits = 3
	case ns%1e3 == 0:
		ns /= 1e3
		digits = 6
	}
	s := strconv.Itoa(ns)
	b = append(b, '.')
	for i := len(s); i < digits; i++ {
		b = append(b, '0')
	}
	return append(b, s...)
}

func appendZone(b []byte, t time.Time) []byte {
	_, off := t.Zone()
	if off == 0 {
		return append(b, 'Z')
	}
	return t.AppendFormat(b, "-07:00")
}
