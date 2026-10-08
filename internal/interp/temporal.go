package interp

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/touno-io/go-tsunami-cli/internal/value"
)

// parseTemporalLiteral parses the body of a |...| literal: dates, times,
// date-times with or without offsets, time zones and periods.
func parseTemporalLiteral(raw string) (value.Value, error) {
	s := strings.TrimSpace(raw)
	switch {
	case s == "":
		return value.Null, fmt.Errorf("empty temporal literal")
	case isPeriodLiteral(s):
		return parsePeriodLiteral(s)
	case s[0] == '+' || s[0] == '-' || s == "Z":
		loc, err := parseOffset(s)
		if err != nil {
			return value.Null, err
		}
		return value.Temporal(value.KindTimeZone, time.Date(1970, 1, 1, 0, 0, 0, 0, loc)), nil
	}
	datePart, timePart, hasT := strings.Cut(s, "T")
	if !hasT && strings.Contains(s, ":") {
		timePart, datePart = s, ""
	}
	y, mo, d := 1970, 1, 1
	if datePart != "" {
		var err error
		if y, mo, d, err = parseDate(datePart); err != nil {
			return value.Null, fmt.Errorf("invalid date %q", raw)
		}
		if timePart == "" {
			return value.Temporal(value.KindDate, time.Date(y, time.Month(mo), d, 0, 0, 0, 0, time.UTC)), nil
		}
	}
	return parseTimeLiteral(raw, timePart, datePart != "", y, mo, d)
}

func isPeriodLiteral(s string) bool {
	return s[0] == 'P' || s[0] == 'p' || (s[0] == '-' && len(s) > 1 && s[1] == 'P')
}

func parsePeriodLiteral(s string) (value.Value, error) {
	neg := s[0] == '-'
	p, err := value.ParsePeriod(strings.TrimPrefix(s, "-"))
	if err != nil {
		return value.Null, err
	}
	if neg {
		p = value.Period{Years: -p.Years, Months: -p.Months, Days: -p.Days, Dur: -p.Dur}
	}
	return value.PeriodValue(p), nil
}

// parseDate parses yyyy-MM-dd.
func parseDate(s string) (y, mo, d int, err error) {
	parts := strings.Split(s, "-")
	if len(parts) != 3 {
		return 0, 0, 0, fmt.Errorf("invalid date")
	}
	nums := make([]int, 3)
	for i, p := range parts {
		if nums[i], err = strconv.Atoi(p); err != nil {
			return 0, 0, 0, err
		}
	}
	y, mo, d = nums[0], nums[1], nums[2]
	if mo < 1 || mo > 12 || d < 1 || d > 31 {
		return 0, 0, 0, fmt.Errorf("invalid date")
	}
	return y, mo, d, nil
}

// parseTimeLiteral parses the time part of a literal and picks the kind
// from whether a date and a zone are present.
func parseTimeLiteral(raw, timePart string, hasDate bool, y, mo, d int) (value.Value, error) {
	clock, loc, zoned, err := splitZone(timePart)
	if err != nil {
		return value.Null, fmt.Errorf("invalid time in %q: %v", raw, err)
	}
	h, mi, sec, ns, err := parseClock(clock)
	if err != nil {
		return value.Null, fmt.Errorf("invalid time in %q: %v", raw, err)
	}
	t := time.Date(y, time.Month(mo), d, h, mi, sec, ns, loc)
	kinds := [2][2]value.Kind{
		{value.KindLocalTime, value.KindTime},
		{value.KindLocalDateTime, value.KindDateTime},
	}
	return value.Temporal(kinds[b2i(hasDate)][b2i(zoned)], t), nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// splitZone separates a trailing Z, offset or [Zone/Id] from a clock.
func splitZone(s string) (string, *time.Location, bool, error) {
	if i := strings.IndexByte(s, '['); i >= 0 && strings.HasSuffix(s, "]") {
		loc, err := time.LoadLocation(s[i+1 : len(s)-1])
		if err != nil {
			return "", nil, false, err
		}
		clock, _, _, err := splitZone(s[:i])
		return clock, loc, true, err
	}
	if strings.HasSuffix(s, "Z") || strings.HasSuffix(s, "z") {
		return s[:len(s)-1], time.UTC, true, nil
	}
	if i := strings.LastIndexAny(s, "+-"); i > 0 {
		loc, err := parseOffset(s[i:])
		return s[:i], loc, true, err
	}
	return s, time.UTC, false, nil
}

func parseOffset(s string) (*time.Location, error) {
	if s == "Z" || s == "z" {
		return time.UTC, nil
	}
	if len(s) < 3 || (s[0] != '+' && s[0] != '-') {
		return nil, fmt.Errorf("invalid offset %q", s)
	}
	h, m, err := offsetParts(strings.ReplaceAll(s[1:], ":", ""))
	if err != nil {
		return nil, fmt.Errorf("invalid offset %q", s)
	}
	off := h*3600 + m*60
	if s[0] == '-' {
		off = -off
	}
	if off == 0 {
		return time.UTC, nil
	}
	return time.FixedZone("", off), nil
}

// offsetParts parses HH or HHmm.
func offsetParts(body string) (int, int, error) {
	switch len(body) {
	case 2:
		h, err := strconv.Atoi(body)
		return h, 0, err
	case 4:
		h, err := strconv.Atoi(body[:2])
		if err != nil {
			return 0, 0, err
		}
		m, err := strconv.Atoi(body[2:])
		return h, m, err
	}
	return 0, 0, fmt.Errorf("invalid offset")
}

func parseClock(s string) (h, m, sec, ns int, err error) {
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, 0, 0, 0, fmt.Errorf("expected HH:mm[:ss]")
	}
	if h, err = strconv.Atoi(parts[0]); err != nil {
		return
	}
	if m, err = strconv.Atoi(parts[1]); err != nil {
		return
	}
	if len(parts) == 3 {
		if sec, ns, err = parseSeconds(parts[2]); err != nil {
			return
		}
	}
	if h > 23 || m > 59 || sec > 60 {
		err = fmt.Errorf("time out of range")
	}
	return
}

// parseSeconds parses ss[.fraction].
func parseSeconds(s string) (int, int, error) {
	secPart, frac, _ := strings.Cut(s, ".")
	sec, err := strconv.Atoi(secPart)
	if err != nil || frac == "" {
		return sec, 0, err
	}
	ns, err := strconv.Atoi((frac + "000000000")[:9])
	return sec, ns, err
}

// temporalField implements selectors such as date.year and now().hour.
func temporalField(v value.Value, name string) (value.Value, error) {
	if v.K == value.KindPeriod {
		return periodField(v.Period(), name)
	}
	t := v.Time()
	_, off := t.Zone()
	switch name {
	case "year":
		return value.Int(t.Year()), nil
	case "month":
		return value.Int(int(t.Month())), nil
	case "day":
		return value.Int(t.Day()), nil
	case "hour":
		return value.Int(t.Hour()), nil
	case "minutes":
		return value.Int(t.Minute()), nil
	case "seconds":
		return value.Int(t.Second()), nil
	case "milliseconds":
		return value.Int(t.Nanosecond() / 1e6), nil
	case "nanoseconds":
		return value.Int(t.Nanosecond()), nil
	case "dayOfWeek":
		return value.Int((int(t.Weekday())+6)%7 + 1), nil
	case "dayOfYear":
		return value.Int(t.YearDay()), nil
	case "quarter":
		return value.Int((int(t.Month())-1)/3 + 1), nil
	case "offsetSeconds":
		return value.Int(off), nil
	case "timezone":
		return value.Temporal(value.KindTimeZone, t), nil
	}
	return value.Null, fmt.Errorf("unknown date field %q", name)
}

func periodField(p value.Period, name string) (value.Value, error) {
	switch name {
	case "years":
		return value.Int(p.Years), nil
	case "months":
		return value.Int(p.Months), nil
	case "days":
		return value.Int(p.Days), nil
	case "hours":
		return value.Int(int(p.Dur.Hours())), nil
	case "minutes":
		return value.Int(int(p.Dur.Minutes()) % 60), nil
	case "seconds":
		return value.Int(int(p.Dur.Seconds()) % 60), nil
	}
	return value.Null, fmt.Errorf("unknown period field %q", name)
}

func pad(n, width int) string {
	s := strconv.Itoa(n)
	for len(s) < width {
		s = "0" + s
	}
	return s
}

// formatTemporal formats t with a Java DateTimeFormatter pattern.
func formatTemporal(t time.Time, pattern string) (string, error) {
	var b strings.Builder
	for _, tk := range tokenizePattern(pattern) {
		if tk.letter == 0 {
			b.WriteString(tk.lit)
			continue
		}
		s, err := formatField(t, tk.letter, tk.count)
		if err != nil {
			return "", err
		}
		b.WriteString(s)
	}
	return b.String(), nil
}

// formatField renders one pattern field.
func formatField(t time.Time, letter byte, n int) (string, error) {
	switch letter {
	case 'y', 'u', 'Y':
		return formatYear(t.Year(), n), nil
	case 'M', 'L':
		return formatMonth(t.Month(), n), nil
	case 'd':
		return pad(t.Day(), n), nil
	case 'D':
		return pad(t.YearDay(), n), nil
	case 'E', 'e':
		return shortOrLong(t.Weekday().String(), n), nil
	case 'a':
		return t.Format("PM"), nil
	case 'H':
		return pad(t.Hour(), n), nil
	case 'k':
		return pad((t.Hour()+23)%24+1, n), nil
	case 'h':
		return pad((t.Hour()+11)%12+1, n), nil
	case 'K':
		return pad(t.Hour()%12, n), nil
	case 'm':
		return pad(t.Minute(), n), nil
	case 's':
		return pad(t.Second(), n), nil
	case 'S':
		return formatFraction(t.Nanosecond(), n), nil
	case 'n':
		return strconv.Itoa(t.Nanosecond()), nil
	case 'A':
		return pad(((t.Hour()*60+t.Minute())*60+t.Second())*1000+t.Nanosecond()/1e6, n), nil
	case 'X', 'x', 'Z', 'O':
		return formatOffset(t, letter, n), nil
	case 'z', 'V':
		return zoneName(t, letter), nil
	}
	return "", fmt.Errorf("unsupported date pattern letter %q", letter)
}

func formatYear(y, n int) string {
	if n == 2 {
		return pad(y%100, 2)
	}
	return pad(y, n)
}

func formatMonth(m time.Month, n int) string {
	if n >= 3 {
		return shortOrLong(m.String(), n)
	}
	return pad(int(m), n)
}

// shortOrLong returns the full name for four or more pattern letters and
// the three-letter abbreviation otherwise.
func shortOrLong(name string, n int) string {
	if n >= 4 {
		return name
	}
	return name[:3]
}

func formatFraction(ns, n int) string {
	frac := pad(ns, 9)
	if n <= 9 {
		return frac[:n]
	}
	return frac + strings.Repeat("0", n-9)
}

func zoneName(t time.Time, letter byte) string {
	name, _ := t.Zone()
	if letter == 'V' && t.Location().String() != "" {
		name = t.Location().String()
	}
	if name == "" {
		name = formatOffset(t, 'X', 5)
	}
	return name
}

func formatOffset(t time.Time, letter byte, n int) string {
	_, off := t.Zone()
	if off == 0 && letter == 'X' {
		return "Z"
	}
	sign := '+'
	if off < 0 {
		sign = '-'
		off = -off
	}
	h, m := off/3600, off%3600/60
	isX := letter == 'X' || letter == 'x'
	switch {
	case letter == 'Z' && n <= 3, isX && n == 2, isX && n == 1 && m != 0:
		return fmt.Sprintf("%c%02d%02d", sign, h, m)
	case isX && n == 1:
		return fmt.Sprintf("%c%02d", sign, h)
	case letter == 'O':
		return fmt.Sprintf("GMT%c%d", sign, h)
	}
	return fmt.Sprintf("%c%02d:%02d", sign, h, m)
}

// dateParser holds the fields read while parsing a formatted date.
type dateParser struct {
	s                       string
	pos                     int
	year, month, day        int
	hour, minute, sec, nsec int
	pm, hasAmPm             bool
	loc                     *time.Location
	pattern                 string
}

func (p *dateParser) fail() error {
	return fmt.Errorf("cannot parse %q with format %q", p.s, p.pattern)
}

// readInt reads between minDigits and maxDigits digits.
func (p *dateParser) readInt(minDigits, maxDigits int) (int, bool) {
	start := p.pos
	if p.pos < len(p.s) && (p.s[p.pos] == '-' || p.s[p.pos] == '+') && maxDigits > 4 {
		p.pos++
	}
	for p.pos < len(p.s) && p.pos-start < maxDigits && isDigit(p.s[p.pos]) {
		p.pos++
	}
	if p.pos-start < minDigits {
		return 0, false
	}
	n, err := strconv.Atoi(p.s[start:p.pos])
	return n, err == nil
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// parseTemporal parses s with a Java DateTimeFormatter pattern into a value
// of the requested kind.
func parseTemporal(s, pattern string, kind value.Kind) (time.Time, error) {
	p := &dateParser{s: s, pattern: pattern, year: 1970, month: 1, day: 1, loc: time.UTC}
	tokens := tokenizePattern(pattern)
	for i, tk := range tokens {
		if tk.letter == 0 {
			if !strings.HasPrefix(p.s[p.pos:], tk.lit) {
				return time.Time{}, p.fail()
			}
			p.pos += len(tk.lit)
			continue
		}
		// Adjacent numeric fields such as yyyyMMdd need fixed widths.
		adjacent := i+1 < len(tokens) && tokens[i+1].letter != 0
		ok, err := p.field(tk, adjacent)
		if err != nil {
			return time.Time{}, err
		}
		if !ok {
			return time.Time{}, p.fail()
		}
	}
	return p.result(kind)
}

func (p *dateParser) result(kind value.Kind) (time.Time, error) {
	if p.pos != len(p.s) {
		return time.Time{}, p.fail()
	}
	if p.hasAmPm {
		p.hour %= 12
		if p.pm {
			p.hour += 12
		}
	}
	if p.month < 1 || p.month > 12 || p.day < 1 || p.day > 366 || p.hour > 24 || p.minute > 59 || p.sec > 60 {
		return time.Time{}, p.fail()
	}
	loc := p.loc
	if kind == value.KindLocalDateTime || kind == value.KindLocalTime || kind == value.KindDate {
		loc = time.UTC
	}
	return time.Date(p.year, time.Month(p.month), p.day, p.hour, p.minute, p.sec, p.nsec, loc), nil
}

// field parses one pattern field. Numeric fields read up to two digits, or
// exactly the pattern width when another field follows without a separator.
func (p *dateParser) field(tk patternToken, adjacent bool) (bool, error) {
	width := max(2, tk.count)
	if adjacent {
		width = tk.count
	}
	var ok bool
	switch tk.letter {
	case 'y', 'u', 'Y':
		ok = p.readYear(tk.count, adjacent)
	case 'M', 'L':
		ok = p.readMonthField(tk.count, width)
	case 'd':
		p.day, ok = p.readInt(tk.count, width)
	case 'D':
		p.month = 1
		p.day, ok = p.readInt(tk.count, 3)
	case 'H', 'k', 'h', 'K':
		p.hour, ok = p.readInt(tk.count, width)
	case 'm':
		p.minute, ok = p.readInt(tk.count, width)
	case 's':
		p.sec, ok = p.readInt(tk.count, width)
	case 'S':
		ok = p.readFraction()
	case 'a':
		ok = p.readAmPm()
	case 'E', 'e':
		ok = p.skipLetters()
	case 'X', 'x', 'Z', 'O':
		ok = p.readOffset()
	case 'z', 'V':
		ok = p.readZoneID()
	default:
		return false, fmt.Errorf("unsupported date pattern letter %q", tk.letter)
	}
	return ok, nil
}

func (p *dateParser) readYear(count int, adjacent bool) bool {
	var ok bool
	if count == 2 {
		p.year, ok = p.readInt(2, 2)
		p.year += 2000
		return ok
	}
	maxDigits := 9
	if adjacent {
		maxDigits = count
	}
	p.year, ok = p.readInt(count, maxDigits)
	return ok
}

func (p *dateParser) readMonthField(count, width int) bool {
	var ok bool
	if count >= 3 {
		p.month, ok = readMonth(p.s, &p.pos)
		return ok
	}
	p.month, ok = p.readInt(count, width)
	return ok
}

func (p *dateParser) readFraction() bool {
	start := p.pos
	for p.pos < len(p.s) && p.pos-start < 9 && isDigit(p.s[p.pos]) {
		p.pos++
	}
	if p.pos == start {
		return false
	}
	p.nsec, _ = strconv.Atoi((p.s[start:p.pos] + "000000000")[:9])
	return true
}

func (p *dateParser) readAmPm() bool {
	p.hasAmPm = true
	if len(p.s)-p.pos < 2 {
		return false
	}
	marker := strings.ToUpper(p.s[p.pos : p.pos+2])
	p.pos += 2
	p.pm = marker == "PM"
	return marker == "AM" || marker == "PM"
}

func (p *dateParser) skipLetters() bool {
	start := p.pos
	for p.pos < len(p.s) && isLetter(p.s[p.pos]) {
		p.pos++
	}
	return p.pos > start
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

func (p *dateParser) readOffset() bool {
	end := p.pos
	for end < len(p.s) && strings.IndexByte("+-:0123456789Zz", p.s[end]) >= 0 {
		end++
	}
	loc, err := parseOffset(strings.TrimPrefix(p.s[p.pos:end], "GMT"))
	p.pos = end
	if err != nil {
		return false
	}
	p.loc = loc
	return true
}

func (p *dateParser) readZoneID() bool {
	end := p.pos
	for end < len(p.s) && p.s[end] != ' ' {
		end++
	}
	loc, err := time.LoadLocation(p.s[p.pos:end])
	if err != nil {
		loc, err = parseOffset(p.s[p.pos:end])
	}
	p.pos = end
	if err != nil {
		return false
	}
	p.loc = loc
	return true
}

func readMonth(s string, pos *int) (int, bool) {
	rest := strings.ToLower(s[*pos:])
	for m := time.January; m <= time.December; m++ {
		name := strings.ToLower(m.String())
		for _, candidate := range []string{name, name[:3]} {
			if strings.HasPrefix(rest, candidate) {
				*pos += len(candidate)
				return int(m), true
			}
		}
	}
	return 0, false
}
