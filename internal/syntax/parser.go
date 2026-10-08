package syntax

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Error is a syntax error with a source position.
type Error struct {
	Pos Pos
	Msg string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Pos, e.Msg) }

// reserved words never start an infix function call.
var reserved = map[string]bool{
	"and": true, "or": true, "not": true, "default": true, "as": true, "is": true,
	"if": true, "else": true, "do": true, "fun": true, "var": true, "type": true,
	"import": true, "output": true, "input": true, "ns": true, "case": true,
	"true": true, "false": true, "null": true, "unless": true, "otherwise": true,
	"using": true, "from": true, "with": true,
}

var dollarParams = []Param{{Name: "$"}, {Name: "$$"}, {Name: "$$$"}}

// Parser builds an AST from source text.
type Parser struct {
	lx        *Lexer
	buf       []Token
	sawDollar bool
	base      Pos
}

// Parse parses a full script or module.
func Parse(src string) (script *Script, err error) {
	p := &Parser{lx: NewLexer(src)}
	defer p.recover(&err)
	return p.parseScript(), nil
}

// ParseExpr parses a single expression.
func ParseExpr(src string) (n Node, err error) {
	p := &Parser{lx: NewLexer(src)}
	defer p.recover(&err)
	n = p.parseExpr()
	p.expectEOF()
	return n, nil
}

func (p *Parser) recover(err *error) {
	if r := recover(); r != nil {
		pe, ok := r.(*Error)
		if !ok {
			panic(r)
		}
		*err = pe
	}
}

func (p *Parser) fail(pos Pos, format string, args ...any) {
	panic(&Error{Pos: p.abs(pos), Msg: fmt.Sprintf(format, args...)})
}

// abs converts a position inside an interpolated sub-expression to a
// position in the enclosing source.
func (p *Parser) abs(pos Pos) Pos {
	if p.base.Line == 0 {
		return pos
	}
	if pos.Line == 1 {
		return Pos{Line: p.base.Line, Col: p.base.Col + pos.Col - 1}
	}
	return Pos{Line: p.base.Line + pos.Line - 1, Col: pos.Col}
}

func (p *Parser) peekN(n int) Token {
	for len(p.buf) <= n {
		p.buf = append(p.buf, p.lx.Next())
	}
	return p.buf[n]
}

func (p *Parser) peek() Token { return p.peekN(0) }

func (p *Parser) next() Token {
	t := p.peek()
	p.buf = p.buf[1:]
	return t
}

func (p *Parser) isPunct(s string) bool { return p.peek().Kind == TPunct && p.peek().Text == s }

func (p *Parser) isWord(s string) bool { return p.peek().Kind == TIdent && p.peek().Text == s }

func (p *Parser) expect(s string) Token {
	t := p.peek()
	if t.Kind != TPunct || t.Text != s {
		p.fail(t.Pos, "expected %q but found %s", s, t.describe())
	}
	return p.next()
}

func (p *Parser) expectEOF() {
	if t := p.peek(); t.Kind != TEOF {
		p.fail(t.Pos, "unexpected %s", t.describe())
	}
}

func (p *Parser) pos(t Token) Pos { return p.abs(t.Pos) }

// clearLookahead discards buffered tokens after the lexer was repositioned.
func (p *Parser) clearLookahead() { p.buf = p.buf[:0] }

func (p *Parser) parseScript() *Script {
	s := &Script{}
	header := false
	for p.headerItem(s) {
		header = true
	}
	t := p.peek()
	switch {
	case t.Kind == TSep:
		p.next()
	case t.Kind == TEOF:
		if !header {
			p.fail(t.Pos, "empty script")
		}
		return s
	case header:
		p.fail(t.Pos, "expected a header declaration or --- but found %s", t.describe())
	}
	s.Body = p.parseExpr()
	p.expectEOF()
	return s
}

// headerItem parses one header declaration and reports whether it found
// one.
func (p *Parser) headerItem(s *Script) bool {
	t := p.peek()
	switch {
	case t.Kind == TPercent:
		p.parseVersion(s, t)
	case t.Kind != TIdent:
		return false
	case t.Text == "output" || t.Text == "input":
		p.parseIODirective(s, t)
	case t.Text == "import":
		s.Imports = append(s.Imports, p.parseImport())
	case t.Text == "var" || t.Text == "fun":
		s.Decls = append(s.Decls, p.parseDecl())
	case t.Text == "type" || t.Text == "ns":
		p.next()
		p.lx.skipBalancedLine(t)
		p.clearLookahead()
	default:
		return false
	}
	return true
}

// parseVersion parses %dw 2.0.
func (p *Parser) parseVersion(s *Script, t Token) {
	p.next()
	if !p.isWord("dw") {
		p.fail(t.Pos, "expected %%dw directive")
	}
	p.next()
	if p.peek().Kind == TNumber {
		s.Version = p.next().Text
	}
}

// parseIODirective parses an output or input directive, which runs to the
// end of its line.
func (p *Parser) parseIODirective(s *Script, t Token) {
	p.next()
	isInput := t.Text == "input"
	d := parseDirective(p.lx.restOfDirective(t), isInput)
	p.clearLookahead()
	switch {
	case isInput:
		s.Inputs = append(s.Inputs, d)
	case d.Mime == "":
		p.fail(t.Pos, "output directive requires a media type")
	case s.Output == nil:
		s.Output = &d
	}
}

// parseDirective parses "mime key=value, key2=value2". Input directives start
// with the input name.
func parseDirective(raw string, named bool) Directive {
	d := Directive{Props: map[string]string{}}
	words := splitDirective(raw)
	for _, w := range words {
		if k, v, ok := strings.Cut(w, "="); ok {
			d.Props[strings.TrimSpace(k)] = unquote(strings.TrimSpace(v))
			continue
		}
		switch {
		case named && d.Name == "":
			d.Name = w
		case d.Mime == "":
			d.Mime = w
		}
	}
	return d
}

func splitDirective(raw string) []string {
	var out []string
	var cur strings.Builder
	var quote byte
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		switch {
		case quote != 0:
			cur.WriteByte(c)
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
			cur.WriteByte(c)
		case c == ' ' || c == '\t' || c == '\r' || c == ',':
			flush()
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return out
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

func (p *Parser) parseImport() Import {
	t := p.next()
	imp := Import{P: p.pos(t)}
	if p.isPunct("*") {
		p.next()
		imp.Names = []string{"*"}
		p.expectWord("from")
		imp.Module = p.parseQualified()
		return imp
	}
	first := p.parseQualified()
	if !p.isPunct(",") && !p.isWord("from") {
		imp.Module = first
		if p.isWord("as") {
			p.next()
			imp.Alias = p.expectIdent()
		}
		return imp
	}
	imp.Names = append(imp.Names, first)
	for p.isPunct(",") {
		p.next()
		imp.Names = append(imp.Names, p.expectIdent())
	}
	p.expectWord("from")
	imp.Module = p.parseQualified()
	return imp
}

func (p *Parser) expectWord(w string) {
	t := p.peek()
	if t.Kind != TIdent || t.Text != w {
		p.fail(t.Pos, "expected %q but found %s", w, t.describe())
	}
	p.next()
}

func (p *Parser) expectIdent() string {
	t := p.peek()
	if t.Kind != TIdent {
		p.fail(t.Pos, "expected an identifier but found %s", t.describe())
	}
	return p.next().Text
}

func (p *Parser) parseQualified() string {
	name := p.expectIdent()
	for p.isPunct("::") {
		p.next()
		name += "::" + p.expectIdent()
	}
	return name
}

func (p *Parser) parseDecl() Decl {
	kw := p.next()
	d := Decl{P: p.pos(kw), Fun: kw.Text == "fun"}
	d.Name = p.expectIdent()
	if d.Fun {
		if p.isPunct("<") {
			p.skipTypeArgs()
		}
		d.Params = p.parseParams()
	}
	if p.isPunct(":") {
		p.next()
		p.skipType()
	}
	eq := p.peek()
	if eq.Kind != TPunct || eq.Text != "=" {
		p.fail(eq.Pos, "expected '=' in declaration of %s", d.Name)
	}
	p.next()
	d.Expr = p.parseExpr()
	if !d.Fun {
		if lam, ok := d.Expr.(*Lambda); ok && !lam.Implicit {
			d.Params = lam.Params
		}
	}
	return d
}

// parseParams parses "(a, b: Type, c = default)".
func (p *Parser) parseParams() []Param {
	p.expect("(")
	params := []Param{}
	for !p.isPunct(")") {
		t := p.peek()
		var name string
		switch t.Kind {
		case TIdent:
			name = p.next().Text
		case TDollar:
			name = p.next().Text
		default:
			p.fail(t.Pos, "expected a parameter name but found %s", t.describe())
		}
		param := Param{Name: name}
		if p.isPunct(":") {
			p.next()
			p.skipType()
		}
		if p.isPunct("=") {
			p.next()
			param.Default = p.parseExpr()
		}
		params = append(params, param)
		if !p.isPunct(",") {
			break
		}
		p.next()
	}
	p.expect(")")
	return params
}

// skipType skips a type expression up to a delimiter at nesting depth zero.
func (p *Parser) skipType() {
	depth := 0
	for {
		t := p.peek()
		if t.Kind == TEOF || t.Kind == TSep || depth == 0 && endsType(t) {
			return
		}
		depth += typeNesting(t)
		p.next()
	}
}

// endsType reports whether t ends a type at nesting depth zero.
func endsType(t Token) bool {
	return t.Kind == TPunct && (t.Text == ")" || t.Text == "," || t.Text == "=" || t.Text == "->")
}

// typeNesting returns the change in bracket depth caused by t.
func typeNesting(t Token) int {
	if t.Kind != TPunct {
		return 0
	}
	switch t.Text {
	case "<", "(", "{", "[":
		return 1
	case ">", ")", "}", "]":
		return -1
	}
	return 0
}

func (p *Parser) skipTypeArgs() {
	depth := 0
	for {
		t := p.next()
		switch {
		case t.Kind == TEOF:
			return
		case t.Is("<"):
			depth++
		case t.Is(">"):
			depth--
			if depth == 0 {
				return
			}
		}
	}
}

// parseTypeName parses a type reference after as/is and returns its name.
func (p *Parser) parseTypeName() string {
	name := p.parseQualified()
	if i := strings.LastIndex(name, "::"); i >= 0 {
		name = name[i+2:]
	}
	if p.isPunct("<") {
		p.skipTypeArgs()
	}
	return name
}

// Expression grammar, lowest precedence first.

func (p *Parser) parseExpr() Node {
	l := p.parseInfix()
	for p.isWord("default") {
		t := p.next()
		r := p.parseInfix()
		l = &Binary{node: node{p.pos(t)}, Op: "default", L: l, R: r}
	}
	return l
}

func (p *Parser) parseInfix() Node {
	l := p.parseOr()
	for {
		t := p.peek()
		switch {
		case t.Kind == TPunct && (t.Text == "++" || t.Text == "--"):
			p.next()
			r := p.parseInfixArg()
			l = &Call{node: node{p.pos(t)}, Fn: &Ident{node: node{p.pos(t)}, Name: t.Text}, Args: []Node{l, r}, Infix: true}
		case t.Kind == TIdent && t.Text == "to":
			p.next()
			l = &RangeExpr{node: node{p.pos(t)}, From: l, To: p.parseOr()}
		case t.Kind == TIdent && t.Text == "match" && p.peekN(1).Is("{"):
			p.next()
			l = p.parseMatch(l, t)
		case p.startsInfixCall(t):
			l = p.parseInfixCall(l, t)
		default:
			return l
		}
	}
}

// startsInfixCall reports whether t names a function called infix, as in
// "list map fn". Keywords and object keys (name followed by ":") do not.
func (p *Parser) startsInfixCall(t Token) bool {
	if t.Kind != TIdent || reserved[t.Text] {
		return false
	}
	n := p.peekN(1)
	return n.Kind != TPunct || (n.Text != ":" && n.Text != "=")
}

// parseInfixCall parses "l name r", including "s replace a with b".
func (p *Parser) parseInfixCall(l Node, t Token) Node {
	p.next()
	fn := &Ident{node: node{p.pos(t)}, Name: t.Text}
	for p.isPunct("::") {
		p.next()
		fn.Ns = joinNs(fn.Ns, fn.Name)
		fn.Name = p.expectIdent()
	}
	args := []Node{l, p.parseInfixArg()}
	if fn.Name == "replace" && p.isWord("with") {
		p.next()
		args = append(args, p.parseInfixArg())
	}
	return &Call{node: node{p.pos(t)}, Fn: fn, Args: args, Infix: true}
}

func joinNs(ns, name string) string {
	if ns == "" {
		return name
	}
	return ns + "::" + name
}

// parseInfixArg parses the right operand of an infix call, wrapping it in an
// implicit lambda when it references $, $$ or $$$.
func (p *Parser) parseInfixArg() Node {
	return p.withDollar(p.parseOr)
}

func (p *Parser) withDollar(parse func() Node) Node {
	save := p.sawDollar
	p.sawDollar = false
	n := parse()
	if p.sawDollar {
		n = &Lambda{node: node{n.Position()}, Params: dollarParams, Body: n, Implicit: true}
	}
	p.sawDollar = save
	return n
}

func (p *Parser) parseOr() Node {
	l := p.parseAnd()
	for p.isWord("or") {
		t := p.next()
		l = &Binary{node: node{p.pos(t)}, Op: "or", L: l, R: p.parseAnd()}
	}
	return l
}

func (p *Parser) parseAnd() Node {
	l := p.parseNot()
	for p.isWord("and") {
		t := p.next()
		l = &Binary{node: node{p.pos(t)}, Op: "and", L: l, R: p.parseNot()}
	}
	return l
}

func (p *Parser) parseNot() Node {
	if p.isWord("not") {
		t := p.next()
		return &Unary{node: node{p.pos(t)}, Op: "!", X: p.parseNot()}
	}
	return p.parseEquality()
}

func (p *Parser) parseEquality() Node {
	l := p.parseRelational()
	for {
		t := p.peek()
		if t.Kind != TPunct || (t.Text != "==" && t.Text != "!=" && t.Text != "~=") {
			return l
		}
		p.next()
		l = &Binary{node: node{p.pos(t)}, Op: t.Text, L: l, R: p.parseRelational()}
	}
}

func (p *Parser) parseRelational() Node {
	l := p.parseAdditive()
	for {
		t := p.peek()
		switch {
		case t.Kind == TPunct && (t.Text == "<" || t.Text == ">" || t.Text == "<=" || t.Text == ">="):
			p.next()
			l = &Binary{node: node{p.pos(t)}, Op: t.Text, L: l, R: p.parseAdditive()}
		case t.Kind == TIdent && t.Text == "is":
			p.next()
			l = &Is{node: node{p.pos(t)}, X: l, Type: p.parseTypeName()}
		default:
			return l
		}
	}
}

func (p *Parser) parseAdditive() Node {
	l := p.parseMultiplicative()
	for {
		t := p.peek()
		if t.Kind != TPunct || (t.Text != "+" && t.Text != "-") {
			return l
		}
		p.next()
		l = &Binary{node: node{p.pos(t)}, Op: t.Text, L: l, R: p.parseMultiplicative()}
	}
}

func (p *Parser) parseMultiplicative() Node {
	l := p.parseAs()
	for {
		t := p.peek()
		if t.Kind != TPunct || (t.Text != "*" && t.Text != "/") {
			return l
		}
		p.next()
		l = &Binary{node: node{p.pos(t)}, Op: t.Text, L: l, R: p.parseAs()}
	}
}

func (p *Parser) parseAs() Node {
	x := p.parseUnary()
	for p.isWord("as") {
		t := p.next()
		as := &As{node: node{p.pos(t)}, X: x, Type: p.parseTypeName()}
		if p.isPunct("{") {
			as.Opts = p.parseObject()
		}
		x = as
	}
	return x
}

func (p *Parser) parseUnary() Node {
	t := p.peek()
	if t.Kind == TPunct && (t.Text == "-" || t.Text == "!") {
		p.next()
		x := p.parseUnary()
		if num, ok := x.(*NumLit); ok && t.Text == "-" {
			return &NumLit{node: node{p.pos(t)}, Raw: "-" + num.Raw}
		}
		return &Unary{node: node{p.pos(t)}, Op: t.Text, X: x}
	}
	return p.parsePostfix(p.parsePrimary())
}

func (p *Parser) parsePostfix(x Node) Node {
	for {
		t := p.peek()
		if t.Kind != TPunct {
			return x
		}
		switch t.Text {
		case ".", ".*", "..":
			p.next()
			kind := map[string]int{".": SelKey, ".*": SelMulti, "..": SelDescendant}[t.Text]
			x = &Select{node: node{p.pos(t)}, X: x, Kind: kind, Name: p.selectorName()}
		case ".@", ".^", ".#":
			p.next()
			sel := &Select{node: node{p.pos(t)}, X: x, Kind: SelAttr}
			if p.peek().Kind == TIdent || p.peek().Kind == TString {
				sel.Name = p.selectorName()
			}
			x = sel
		case "[":
			p.next()
			if p.isPunct("?") && p.peekN(1).Is("(") {
				p.next()
				p.next()
				cond := p.withDollar(p.parseExpr)
				p.expect(")")
				p.expect("]")
				x = &Select{node: node{p.pos(t)}, X: x, Kind: SelFilter, Index: cond}
				continue
			}
			idx := p.parseExpr()
			p.expect("]")
			x = &Select{node: node{p.pos(t)}, X: x, Kind: SelIndex, Index: idx}
		case "(":
			x = &Call{node: node{p.pos(t)}, Fn: x, Args: p.parseArgs()}
		case "?":
			p.next()
			x = &Exists{node: node{p.pos(t)}, X: x}
		default:
			return x
		}
	}
}

func (p *Parser) selectorName() string {
	t := p.peek()
	switch t.Kind {
	case TIdent:
		return p.next().Text
	case TString:
		p.next()
		return decodeEscapes(t.Text)
	}
	p.fail(t.Pos, "expected a selector name but found %s", t.describe())
	return ""
}

func (p *Parser) parseArgs() []Node {
	p.expect("(")
	var args []Node
	for !p.isPunct(")") {
		args = append(args, p.parseExpr())
		if !p.isPunct(",") {
			break
		}
		p.next()
	}
	p.expect(")")
	return args
}

func (p *Parser) parsePrimary() Node {
	t := p.peek()
	pos := p.pos(t)
	switch t.Kind {
	case TNumber:
		p.next()
		return &NumLit{node: node{pos}, Raw: t.Text}
	case TString:
		p.next()
		return p.parseString(t)
	case TDate:
		p.next()
		return &DateLit{node: node{pos}, Raw: t.Text}
	case TDollar:
		p.next()
		p.sawDollar = true
		return &Ident{node: node{pos}, Name: t.Text}
	case TIdent:
		return p.parseWord(t)
	case TPunct:
		switch t.Text {
		case "(":
			if p.isLambdaStart() {
				return p.parseLambda()
			}
			return p.parseParen()
		case "[":
			return p.parseArray()
		case "{":
			return p.parseObject()
		case "/":
			p.clearLookahead()
			re, err := p.lx.scanRegex(t)
			if err != nil {
				p.fail(t.Pos, "%s", strings.TrimPrefix(err.Error(), t.Pos.String()+": "))
			}
			return &RegexLit{node: node{pos}, Src: re.Text}
		}
	case TIllegal:
		if t.Quote != 0 {
			p.fail(t.Pos, "unterminated string")
		}
		if t.Text == "|" {
			p.fail(t.Pos, "unterminated date literal")
		}
	}
	p.fail(t.Pos, "unexpected %s", t.describe())
	return nil
}

func (p *Parser) parseWord(t Token) Node {
	pos := p.pos(t)
	switch t.Text {
	case "true", "false":
		p.next()
		return &BoolLit{node: node{pos}, V: t.Text == "true"}
	case "null":
		p.next()
		return &NullLit{node: node{pos}}
	case "if":
		return p.parseIf()
	case "do":
		if p.peekN(1).Is("{") {
			return p.parseDo()
		}
	}
	if reserved[t.Text] && t.Text != "from" && t.Text != "with" {
		p.fail(t.Pos, "unexpected keyword %q", t.Text)
	}
	p.next()
	id := &Ident{node: node{pos}, Name: t.Text}
	for p.isPunct("::") {
		p.next()
		id.Ns = joinNs(id.Ns, id.Name)
		id.Name = p.expectIdent()
	}
	return id
}

// isLambdaStart reports whether the "(" at the head of the lookahead starts a
// lambda parameter list.
func (p *Parser) isLambdaStart() bool {
	first := p.peekN(1)
	if first.Is(")") {
		return p.peekN(2).Is("->")
	}
	if first.Kind != TIdent && first.Kind != TDollar {
		return false
	}
	depth := 0
	for i := 0; ; i++ {
		t := p.peekN(i)
		switch {
		case t.Kind == TEOF:
			return false
		case t.Is("("):
			depth++
		case t.Is(")"):
			depth--
			if depth == 0 {
				return p.peekN(i + 1).Is("->")
			}
		}
	}
}

func (p *Parser) parseLambda() Node {
	t := p.peek()
	params := p.parseParams()
	p.expect("->")
	save := p.sawDollar
	p.sawDollar = false
	body := p.parseExpr()
	p.sawDollar = save
	return &Lambda{node: node{p.pos(t)}, Params: params, Body: body}
}

func (p *Parser) parseParen() Node {
	open := p.next()
	pos := p.pos(open)
	inner := p.parseExpr()
	if p.isPunct(":") {
		p.next()
		f := p.fieldFromKey(inner, pos)
		f.Value = p.parseExpr()
		p.expect(")")
		kv := &KVPair{node: node{pos}, Field: f}
		if p.isWord("if") {
			p.next()
			kv.Field.Cond = p.parseOr()
		}
		return kv
	}
	p.expect(")")
	return &Paren{node: node{pos}, X: inner}
}

// fieldFromKey turns the key of a parenthesized pair into an object field.
func (p *Parser) fieldFromKey(key Node, pos Pos) ObjField {
	f := ObjField{P: pos}
	switch k := key.(type) {
	case *Ident:
		if k.Ns == "" {
			f.KeyName = k.Name
			return f
		}
	case *StrLit:
		if s, ok := staticString(k); ok {
			f.KeyName = s
			return f
		}
	}
	f.KeyExpr = key
	return f
}

func staticString(s *StrLit) (string, bool) {
	var b strings.Builder
	for _, part := range s.Parts {
		if part.Expr != nil {
			return "", false
		}
		b.WriteString(part.Lit)
	}
	return b.String(), true
}

func (p *Parser) parseIf() Node {
	t := p.next()
	p.expect("(")
	cond := p.parseExpr()
	p.expect(")")
	n := &If{node: node{p.pos(t)}, Cond: cond, Then: p.parseExpr()}
	if p.isWord("else") {
		p.next()
		n.Else = p.parseExpr()
	}
	return n
}

func (p *Parser) parseDo() Node {
	t := p.next()
	p.expect("{")
	n := &Do{node: node{p.pos(t)}}
	for {
		tok := p.peek()
		switch {
		case tok.Kind == TSep:
			p.next()
			n.Body = p.parseExpr()
			p.expect("}")
			return n
		case tok.Kind == TIdent && (tok.Text == "var" || tok.Text == "fun"):
			n.Decls = append(n.Decls, p.parseDecl())
		case tok.Kind == TIdent && tok.Text == "type":
			p.next()
			p.lx.skipBalancedLine(tok)
			p.clearLookahead()
		default:
			if len(n.Decls) == 0 {
				n.Body = p.parseExpr()
				p.expect("}")
				return n
			}
			p.fail(tok.Pos, "expected a declaration or --- in do block but found %s", tok.describe())
		}
	}
}

func (p *Parser) parseArray() Node {
	open := p.next()
	n := &ArrayLit{node: node{p.pos(open)}}
	for !p.isPunct("]") {
		el := ArrayElem{X: p.parseExpr()}
		if p.isWord("if") {
			p.next()
			el.Cond = p.parseOr()
		}
		n.Elems = append(n.Elems, el)
		if !p.isPunct(",") {
			break
		}
		p.next()
	}
	p.expect("]")
	return n
}

func (p *Parser) parseObject() Node {
	open := p.next()
	n := &ObjectLit{node: node{p.pos(open)}}
	for !p.isPunct("}") {
		f := p.parseField()
		if p.isWord("if") {
			p.next()
			f.Cond = p.parseOr()
		}
		n.Fields = append(n.Fields, f)
		if p.isPunct(",") {
			p.next()
			continue
		}
		if p.isPunct("}") || !p.startsField() {
			break
		}
	}
	p.expect("}")
	return n
}

// startsField reports whether the lookahead begins a field without a
// separating comma (key: value on a new line).
func (p *Parser) startsField() bool {
	t := p.peek()
	return (t.Kind == TIdent || t.Kind == TString) && p.peekN(1).Is(":")
}

func (p *Parser) parseField() ObjField {
	t := p.peek()
	pos := p.pos(t)
	var f ObjField
	switch {
	case t.Is("("):
		return p.parenField(pos)
	case t.Kind == TString:
		p.next()
		f = p.fieldFromKey(p.parseString(t), pos)
	case t.Kind == TIdent:
		p.next()
		f = ObjField{P: pos, KeyName: t.Text}
		if p.isPunct("#") {
			p.next()
			f.KeyName = p.expectIdent()
		}
	default:
		p.fail(t.Pos, "expected an object key but found %s", t.describe())
	}
	p.skipKeyExtras()
	p.expect(":")
	f.Value = p.parseExpr()
	return f
}

// parenField parses the parenthesized member forms (key: value),
// (expr): value and the spread form (expr).
func (p *Parser) parenField(pos Pos) ObjField {
	p.next()
	inner := p.parseExpr()
	if p.isPunct(":") {
		p.next()
		f := p.fieldFromKey(inner, pos)
		f.Value = p.parseExpr()
		p.expect(")")
		return f
	}
	p.expect(")")
	if !p.isPunct(":") {
		return ObjField{P: pos, Spread: true, Value: inner}
	}
	p.next()
	f := ObjField{P: pos, KeyExpr: inner}
	if s, ok := inner.(*StrLit); ok {
		if name, static := staticString(s); static {
			f = ObjField{P: pos, KeyName: name}
		}
	}
	f.Value = p.parseExpr()
	return f
}

// skipKeyExtras skips XML attribute declarations such as key @(a: 1): value.
func (p *Parser) skipKeyExtras() {
	if !p.isPunct("@") {
		return
	}
	p.next()
	if p.isPunct("(") {
		depth := 0
		for {
			t := p.next()
			if t.Kind == TEOF {
				return
			}
			if t.Is("(") {
				depth++
			} else if t.Is(")") {
				depth--
				if depth == 0 {
					return
				}
			}
		}
	}
}

func (p *Parser) parseMatch(subject Node, t Token) Node {
	m := &Match{node: node{p.pos(t)}, X: subject}
	p.expect("{")
	for !p.isPunct("}") {
		tok := p.peek()
		c := Case{P: p.pos(tok)}
		switch {
		case tok.Kind == TIdent && tok.Text == "else":
			p.next()
			c.Kind = CaseElse
		case tok.Kind == TIdent && tok.Text == "case":
			p.next()
			p.parseCasePattern(&c)
		default:
			p.fail(tok.Pos, "expected case or else in match but found %s", tok.describe())
		}
		p.expect("->")
		c.Body = p.parseExpr()
		m.Cases = append(m.Cases, c)
	}
	p.expect("}")
	return m
}

func (p *Parser) parseCasePattern(c *Case) {
	t := p.peek()
	switch {
	case t.Kind == TIdent && t.Text == "is":
		p.next()
		c.Kind = CaseIs
		c.Type = p.parseTypeName()
	case t.Kind == TIdent && t.Text == "else":
		p.next()
		c.Kind = CaseElse
	case t.Is("/"):
		c.Kind = CaseRegex
		c.Regex = p.parsePrimary()
	case t.Kind == TIdent && !reserved[t.Text]:
		p.next()
		c.Name = t.Text
		c.Kind = CaseBind
		switch {
		case p.isWord("if"):
			p.next()
			c.Guard = p.parseOr()
		case p.isWord("matches"):
			p.next()
			c.Kind = CaseRegex
			c.Regex = p.parsePrimary()
		case p.isWord("is") || p.isPunct(":"):
			p.next()
			c.Kind = CaseIs
			c.Type = p.parseTypeName()
		}
	default:
		c.Kind = CaseLiteral
		c.Lit = p.parseOr()
	}
}

// parseString splits a string token into literal parts and interpolations.
func (p *Parser) parseString(t Token) Node {
	pos := p.pos(t)
	s := &StrLit{node: node{pos}}
	raw := t.Text
	if t.Quote == '`' {
		s.Parts = []StrPart{{Lit: raw}}
		return s
	}
	b := &partsBuilder{}
	for i := 0; i < len(raw); {
		c := raw[i]
		switch {
		case c == '\\' && i+1 < len(raw):
			i += decodeEscape(raw[i:], &b.lit)
		case c == '$' && i+1 < len(raw) && raw[i+1] == '(':
			end := matchInterpolation(raw, i+2)
			if end < 0 {
				p.fail(t.Pos, "unterminated string interpolation")
			}
			b.expr(p.subParse(raw[i+2:end], p.stringOffsetPos(t, raw[:i+2])))
			i = end + 1
		case c == '$' && i+1 < len(raw) && isIdentStart(raw[i+1:]):
			j := identEnd(raw, i+1)
			b.expr(&Ident{node: node{pos}, Name: raw[i+1 : j]})
			i = j
		default:
			b.lit.WriteByte(c)
			i++
		}
	}
	s.Parts = b.done()
	return s
}

// partsBuilder accumulates the literal and interpolated parts of a string.
type partsBuilder struct {
	parts []StrPart
	lit   strings.Builder
}

func (b *partsBuilder) flush() {
	if b.lit.Len() > 0 {
		b.parts = append(b.parts, StrPart{Lit: b.lit.String()})
		b.lit.Reset()
	}
}

func (b *partsBuilder) expr(n Node) {
	b.flush()
	b.parts = append(b.parts, StrPart{Expr: n})
}

func (b *partsBuilder) done() []StrPart {
	b.flush()
	return b.parts
}

// identEnd returns the index just past the identifier starting at i.
func identEnd(s string, i int) int {
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return i
		}
		i += size
	}
	return i
}

// stringOffsetPos computes the absolute position of text inside a string
// token, given the raw prefix before it.
func (p *Parser) stringOffsetPos(t Token, prefix string) Pos {
	pos := t.Pos
	pos.Col++ // opening quote
	for _, r := range prefix {
		if r == '\n' {
			pos.Line++
			pos.Col = 1
		} else {
			pos.Col++
		}
	}
	return p.abs(pos)
}

func (p *Parser) subParse(src string, at Pos) Node {
	sub := &Parser{lx: NewLexer(src), base: at}
	n := sub.parseExpr()
	sub.expectEOF()
	if sub.sawDollar {
		p.sawDollar = true
	}
	return n
}

// matchInterpolation returns the index of the ")" closing an interpolation
// that starts at i, skipping nested parentheses and strings.
func matchInterpolation(raw string, i int) int {
	depth := 1
	for ; i < len(raw); i++ {
		switch c := raw[i]; c {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		case '"', '\'':
			if i = skipQuoted(raw, i); i < 0 {
				return -1
			}
		}
	}
	return -1
}

// skipQuoted returns the index of the quote closing the string that opens
// at i, skipping escapes and nested interpolations, or -1.
func skipQuoted(raw string, i int) int {
	quote := raw[i]
	for j := i + 1; j < len(raw); j++ {
		switch {
		case raw[j] == quote:
			return j
		case raw[j] == '\\':
			j++
		case raw[j] == '$' && j+1 < len(raw) && raw[j+1] == '(':
			if j = matchInterpolation(raw, j+2); j < 0 {
				return -1
			}
		}
	}
	return len(raw)
}

// decodeEscapes decodes backslash escapes in s.
func decodeEscapes(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '\\' && i+1 < len(s) {
			i += decodeEscape(s[i:], &b)
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// escapes maps single-character string escapes to their bytes.
var escapes = map[byte]byte{
	'n': '\n', 't': '\t', 'r': '\r', 'b': '\b', 'f': '\f',
	'"': '"', '\'': '\'', '\\': '\\', '/': '/', '$': '$', '`': '`',
}

// decodeEscape decodes the escape sequence at the start of s and returns the
// number of bytes consumed. Unknown escapes are kept verbatim.
func decodeEscape(s string, b *strings.Builder) int {
	if c, ok := escapes[s[1]]; ok {
		b.WriteByte(c)
		return 2
	}
	if s[1] == 'u' && len(s) >= 6 {
		if r, ok := parseHex4(s[2:6]); ok {
			b.WriteRune(r)
			return 6
		}
	}
	b.WriteString(s[:2])
	return 2
}

// parseHex4 parses four hexadecimal digits.
func parseHex4(s string) (rune, bool) {
	n, err := strconv.ParseUint(s, 16, 32)
	return rune(n), err == nil
}
