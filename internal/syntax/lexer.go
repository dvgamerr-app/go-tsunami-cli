// Package syntax implements the lexer, abstract syntax tree and parser for
// DataWeave-compatible transformation scripts (.dwl and .tsu files).
package syntax

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// TokKind classifies a token.
type TokKind uint8

// Token kinds.
const (
	TEOF TokKind = iota
	TIllegal
	TIdent
	TNumber
	TString
	TDate
	TRegex
	TDollar
	TPercent
	TSep
	TPunct
)

// Pos is a 1-based line and column in the source.
type Pos struct {
	Line, Col int
}

func (p Pos) String() string { return fmt.Sprintf("%d:%d", p.Line, p.Col) }

// Token is a lexical token.
type Token struct {
	Kind  TokKind
	Text  string
	Quote byte
	Off   int
	End   int
	Pos   Pos
}

// Is reports whether the token is the punctuation or identifier s.
func (t Token) Is(s string) bool {
	return (t.Kind == TPunct || t.Kind == TIdent) && t.Text == s
}

func (t Token) describe() string {
	switch t.Kind {
	case TEOF:
		return "end of input"
	case TString:
		return fmt.Sprintf("string %q", t.Text)
	}
	return fmt.Sprintf("%q", t.Text)
}

// bom is the byte order mark that some editors prepend to UTF-8 files.
const bom = string(rune(0xFEFF))

// punctuation sorted longest first so the lexer can match greedily.
var puncts = []string{
	"---", "->", "::", "++", "--", "==", "!=", "~=", "<=", ">=", ".*", "..", ".@", ".^", ".#",
	"(", ")", "[", "]", "{", "}", ",", ":", ".", "?", "!", "=", "<", ">", "+", "-", "*", "/", "@", "#", "&", "^", "~", ";",
}

// Lexer splits a script into tokens on demand.
type Lexer struct {
	src  string
	off  int
	line int
	col  int
}

// NewLexer returns a lexer for src.
func NewLexer(src string) *Lexer {
	return &Lexer{src: src, line: 1, col: 1}
}

func (l *Lexer) pos() Pos { return Pos{Line: l.line, Col: l.col} }

func (l *Lexer) advance(n int) {
	for i := 0; i < n && l.off < len(l.src); i++ {
		if l.src[l.off] == '\n' {
			l.line++
			l.col = 1
		} else if l.src[l.off]&0xC0 != 0x80 {
			l.col++
		}
		l.off++
	}
}

// reset moves the lexer back to the start of tok.
func (l *Lexer) reset(tok Token) {
	l.off, l.line, l.col = tok.Off, tok.Pos.Line, tok.Pos.Col
}

// resetAfter moves the lexer to the end of tok.
func (l *Lexer) resetAfter(tok Token) {
	l.reset(tok)
	l.advance(tok.End - tok.Off)
}

func (l *Lexer) skipSpace() {
	for l.off < len(l.src) {
		c := l.src[l.off]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\f':
			l.advance(1)
		case c == 0xEF && strings.HasPrefix(l.src[l.off:], bom):
			l.advance(3)
		case c == '/' && l.off+1 < len(l.src) && l.src[l.off+1] == '/':
			for l.off < len(l.src) && l.src[l.off] != '\n' {
				l.advance(1)
			}
		case c == '/' && l.off+1 < len(l.src) && l.src[l.off+1] == '*':
			end := strings.Index(l.src[l.off+2:], "*/")
			if end < 0 {
				l.advance(len(l.src) - l.off)
				return
			}
			l.advance(end + 4)
		default:
			return
		}
	}
}

// Next returns the next token.
func (l *Lexer) Next() Token {
	l.skipSpace()
	start, pos := l.off, l.pos()
	if l.off >= len(l.src) {
		return Token{Kind: TEOF, Off: start, End: start, Pos: pos}
	}
	tok := Token{Off: start, Pos: pos}
	c := l.src[l.off]
	switch {
	case isIdentStart(l.src[l.off:]):
		l.scanIdent()
		tok.Kind = TIdent
	case isDigitByte(c):
		l.scanNumber()
		tok.Kind = TNumber
	case c == '"' || c == '\'' || c == '`':
		return l.stringToken(tok, c)
	case c == '|':
		return l.dateToken(tok)
	case c == '$':
		for n := 0; n < 3 && l.off < len(l.src) && l.src[l.off] == '$'; n++ {
			l.advance(1)
		}
		tok.Kind = TDollar
	case c == '%':
		l.advance(1)
		tok.Kind = TPercent
	case strings.HasPrefix(l.src[l.off:], "---"):
		for l.off < len(l.src) && l.src[l.off] == '-' {
			l.advance(1)
		}
		tok.Kind = TSep
	default:
		tok.Kind = l.scanPunct()
	}
	tok.End = l.off
	tok.Text = l.src[start:l.off]
	return tok
}

func isDigitByte(c byte) bool { return c >= '0' && c <= '9' }

func (l *Lexer) stringToken(tok Token, quote byte) Token {
	tok.Kind = TString
	tok.Quote = quote
	text, ok := l.scanString(quote)
	if !ok {
		tok.Kind = TIllegal
	}
	tok.Text = text
	tok.End = l.off
	return tok
}

// dateToken scans a |...| temporal literal.
func (l *Lexer) dateToken(tok Token) Token {
	end := strings.IndexByte(l.src[l.off+1:], '|')
	if end < 0 {
		l.advance(1)
		tok.Kind = TIllegal
		tok.Text = "|"
	} else {
		tok.Kind = TDate
		tok.Text = l.src[l.off+1 : l.off+1+end]
		l.advance(end + 2)
	}
	tok.End = l.off
	return tok
}

// scanPunct consumes the longest matching punctuation, or one illegal rune.
func (l *Lexer) scanPunct() TokKind {
	for _, p := range puncts {
		if strings.HasPrefix(l.src[l.off:], p) {
			l.advance(len(p))
			return TPunct
		}
	}
	_, size := utf8.DecodeRuneInString(l.src[l.off:])
	l.advance(size)
	return TIllegal
}

func isIdentStart(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return r == '_' || unicode.IsLetter(r)
}

func (l *Lexer) scanIdent() {
	for l.off < len(l.src) {
		r, size := utf8.DecodeRuneInString(l.src[l.off:])
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return
		}
		l.advance(size)
	}
}

func (l *Lexer) scanNumber() {
	l.digits()
	if l.off+1 < len(l.src) && l.src[l.off] == '.' && isDigitByte(l.src[l.off+1]) {
		l.advance(1)
		l.digits()
	}
	if l.off < len(l.src) && (l.src[l.off] == 'e' || l.src[l.off] == 'E') {
		l.exponent()
	}
}

func (l *Lexer) digits() {
	for l.off < len(l.src) && isDigitByte(l.src[l.off]) {
		l.advance(1)
	}
}

// exponent consumes e[+-]digits, or nothing when no digits follow.
func (l *Lexer) exponent() {
	save := *l
	l.advance(1)
	if l.off < len(l.src) && (l.src[l.off] == '+' || l.src[l.off] == '-') {
		l.advance(1)
	}
	if l.off < len(l.src) && isDigitByte(l.src[l.off]) {
		l.digits()
		return
	}
	*l = save
}

// scanString consumes a quoted string, including nested strings inside
// $( ... ) interpolations, and returns the raw text between the quotes.
func (l *Lexer) scanString(quote byte) (string, bool) {
	l.advance(1)
	start := l.off
	for l.off < len(l.src) {
		c := l.src[l.off]
		switch {
		case c == '\\':
			l.advance(2)
		case c == quote:
			text := l.src[start:l.off]
			l.advance(1)
			return text, true
		case c == '$' && l.off+1 < len(l.src) && l.src[l.off+1] == '(' && quote != '`':
			l.advance(2)
			if !l.skipInterpolation() {
				return l.src[start:l.off], false
			}
		default:
			l.advance(1)
		}
	}
	return l.src[start:], false
}

// skipInterpolation skips an interpolated expression up to and including the
// closing parenthesis.
func (l *Lexer) skipInterpolation() bool {
	depth := 1
	for l.off < len(l.src) {
		c := l.src[l.off]
		switch c {
		case '(':
			depth++
			l.advance(1)
		case ')':
			depth--
			l.advance(1)
			if depth == 0 {
				return true
			}
		case '"', '\'', '`':
			if _, ok := l.scanString(c); !ok {
				return false
			}
		default:
			l.advance(1)
		}
	}
	return false
}

// scanRegex re-reads the source at a '/' token as a regular expression
// literal and returns the regex token.
func (l *Lexer) scanRegex(slash Token) (Token, error) {
	l.reset(slash)
	l.advance(1)
	start := l.off
	inClass := false
	for l.off < len(l.src) {
		c := l.src[l.off]
		switch {
		case c == '\\':
			l.advance(2)
			continue
		case c == '[':
			inClass = true
		case c == ']':
			inClass = false
		case c == '\n':
			return Token{}, fmt.Errorf("%s: unterminated regular expression", slash.Pos)
		case c == '/' && !inClass:
			text := l.src[start:l.off]
			l.advance(1)
			return Token{Kind: TRegex, Text: text, Off: slash.Off, End: l.off, Pos: slash.Pos}, nil
		}
		l.advance(1)
	}
	return Token{}, fmt.Errorf("%s: unterminated regular expression", slash.Pos)
}

// restOfDirective returns the raw text after tok up to the end of its line, a
// header separator or a comment.
func (l *Lexer) restOfDirective(tok Token) string {
	l.resetAfter(tok)
	start := l.off
	end := start
	for end < len(l.src) && l.src[end] != '\n' {
		if strings.HasPrefix(l.src[end:], "---") || strings.HasPrefix(l.src[end:], "//") {
			break
		}
		end++
	}
	l.advance(end - start)
	return l.src[start:end]
}

// skipBalancedLine skips a declaration that may span lines while brackets are
// open, stopping at the first newline at depth zero.
func (l *Lexer) skipBalancedLine(tok Token) {
	l.resetAfter(tok)
	depth := 0
	for l.off < len(l.src) {
		c := l.src[l.off]
		switch c {
		case '{', '(', '[', '<':
			depth++
		case '}', ')', ']', '>':
			depth--
		case '\n':
			if depth <= 0 {
				return
			}
		case '"', '\'':
			l.scanString(c)
			continue
		}
		l.advance(1)
	}
}
