package interp

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/touno-io/go-tsunami-cli/internal/syntax"
	"github.com/touno-io/go-tsunami-cli/internal/value"
)

// Error is a compilation or evaluation error with a source position.
type Error struct {
	Pos syntax.Pos
	Err error
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %v", e.Pos, e.Err) }

// Unwrap returns the underlying error.
func (e *Error) Unwrap() error { return e.Err }

// at attaches a source position to err unless it already carries one.
func at(pos syntax.Pos, err error) error {
	if err == nil || err == value.ErrStop {
		return err
	}
	var e *Error
	if errors.As(err, &e) {
		return err
	}
	var se *syntax.Error
	if errors.As(err, &se) {
		return err
	}
	return &Error{Pos: pos, Err: err}
}

type evalFn func(*frame) (value.Value, error)

// frame holds the variable slots of one scope activation.
type frame struct {
	slots  []value.Value
	parent *frame
}

// frameN co-allocates a frame with its slots, the common case for lambdas
// such as (item, index) -> ..., halving allocations per call.
type (
	frame1 struct {
		f   frame
		buf [1]value.Value
	}
	frame2 struct {
		f   frame
		buf [2]value.Value
	}
	frame3 struct {
		f   frame
		buf [3]value.Value
	}
)

func newFrame(n int, parent *frame) *frame {
	switch n {
	case 0:
		return &frame{parent: parent}
	case 1:
		s := &frame1{f: frame{parent: parent}}
		s.f.slots = s.buf[:]
		return &s.f
	case 2:
		s := &frame2{f: frame{parent: parent}}
		s.f.slots = s.buf[:]
		return &s.f
	case 3:
		s := &frame3{f: frame{parent: parent}}
		s.f.slots = s.buf[:]
		return &s.f
	}
	return &frame{slots: make([]value.Value, n), parent: parent}
}

// frameInfo counts the slots of a frame during compilation.
type frameInfo struct{ n int }

// slotMeta is shared between the compiler and the runtime. multi is true
// when a binding is read more than once, so a one-shot stream bound to it
// must be buffered.
type slotMeta struct{ multi bool }

type binding struct {
	slot int
	uses int
	meta *slotMeta
}

type scope struct {
	parent   *scope
	frame    *frameInfo
	names    map[string]*binding
	loop     bool
	isolated bool
	root     bool
}

func (s *scope) declare(name string, slot int) *binding {
	b := &binding{slot: slot, meta: &slotMeta{}}
	s.names[name] = b
	return b
}

func (s *scope) alloc(name string) *binding {
	slot := s.frame.n
	s.frame.n++
	return s.declare(name, slot)
}

// lookup resolves name and counts the use. A use from inside a lambda of a
// binding declared outside it counts as multiple uses, since the lambda may
// run many times.
func (s *scope) lookup(name string) (*binding, int, bool) {
	depth := 0
	crossedLoop := false
	for cur := s; cur != nil; {
		if b, ok := cur.names[name]; ok {
			b.uses++
			if crossedLoop {
				b.uses++
			}
			b.meta.multi = b.uses > 1
			return b, depth, true
		}
		if cur.loop {
			crossedLoop = true
		}
		if cur.isolated {
			return nil, 0, false
		}
		next := cur.parent
		if next != nil && next.frame != cur.frame {
			depth++
		}
		cur = next
	}
	return nil, 0, false
}

func (s *scope) inModule() bool {
	for cur := s; cur != nil; cur = cur.parent {
		if cur.isolated {
			return true
		}
	}
	return false
}

func (s *scope) rootScope() *scope {
	cur := s
	for cur.parent != nil {
		cur = cur.parent
	}
	return cur
}

// thunk is a lazily evaluated variable.
type thunk struct {
	fn    evalFn
	frame *frame
	meta  *slotMeta
	name  string
	busy  bool
}

func force(fr *frame, idx int) (value.Value, error) {
	t, _ := fr.slots[idx].R.(*thunk)
	if t.busy {
		return value.Null, fmt.Errorf("variable %q references itself", t.name)
	}
	t.busy = true
	v, err := t.fn(t.frame)
	t.busy = false
	if err != nil {
		return value.Null, err
	}
	if t.meta != nil && t.meta.multi {
		if v, err = shareable(v); err != nil {
			return value.Null, err
		}
	}
	fr.slots[idx] = v
	return v, nil
}

// shareable buffers a one-shot stream so it can be read more than once.
func shareable(v value.Value) (value.Value, error) {
	if v.K == value.KindArray {
		if a := v.Array(); !a.Restartable() {
			if _, err := a.Items(); err != nil {
				return value.Null, err
			}
		}
	}
	return v, nil
}

type compiler struct {
	prog *Program
}

func (c *compiler) fail(pos syntax.Pos, format string, args ...any) {
	panic(&Error{Pos: pos, Err: fmt.Errorf(format, args...)})
}

func (c *compiler) recover(err *error) {
	if r := recover(); r != nil {
		e, ok := r.(*Error)
		if !ok {
			panic(r)
		}
		*err = e
	}
}

// declareAll binds declarations in sc. Names are declared before any body is
// compiled so declarations may reference each other in any order.
func (c *compiler) declareAll(sc *scope, decls []syntax.Decl, emit func(slot int, init func(*frame) value.Value)) {
	bindings := make([]*binding, len(decls))
	for i, d := range decls {
		bindings[i] = sc.alloc(d.Name)
	}
	for i, d := range decls {
		b := bindings[i]
		if d.Fun {
			emit(b.slot, c.lambdaMaker(d.Name, d.Params, d.Expr, sc))
			continue
		}
		fn := c.expr(d.Expr, sc)
		meta, name := b.meta, d.Name
		emit(b.slot, func(f *frame) value.Value {
			return value.Value{K: value.KindThunk, R: &thunk{fn: fn, frame: f, meta: meta, name: name}}
		})
	}
}

// importInto makes module members visible in the root scope.
func (c *compiler) importInto(rs *scope, imp syntax.Import, dir string) {
	if strings.HasPrefix(imp.Module, "dw::") || !strings.Contains(imp.Module, "::") && isCoreModule(imp.Module) {
		// Standard library modules map onto builtins, which are always
		// available, optionally through a namespace prefix.
		return
	}
	mod := c.loadModule(imp, dir)
	switch {
	case len(imp.Names) == 0:
		bindNamespace(rs, mod, imp)
	case imp.Names[0] == "*":
		for name, b := range mod.names {
			rs.names[name] = b
		}
	default:
		for _, name := range imp.Names {
			b, ok := mod.names[name]
			if !ok {
				c.fail(imp.P, "module %s has no member %q", imp.Module, name)
			}
			rs.names[name] = b
		}
	}
}

// bindNamespace exposes module members as Module::name and, for an import
// with an alias, as Alias::name.
func bindNamespace(rs *scope, mod *module, imp syntax.Import) {
	prefix := imp.Module
	if i := strings.LastIndex(prefix, "::"); i >= 0 {
		prefix = prefix[i+2:]
	}
	if imp.Alias != "" {
		prefix = imp.Alias
	}
	for name, b := range mod.names {
		rs.names[prefix+"::"+name] = b
		rs.names[imp.Module+"::"+name] = b
	}
}

func isCoreModule(name string) bool {
	switch name {
	case "Core", "Strings", "Arrays", "Objects", "Runtime", "System", "Values", "Dates", "Periods",
		"Numbers", "Math", "Types", "Asserts", "Binaries", "Crypto", "Tree", "URL", "Timer", "Diff", "Coercions":
		return true
	}
	return false
}

func (c *compiler) loadModule(imp syntax.Import, dir string) *module {
	path, err := c.prog.findModule(imp.Module, dir)
	if err != nil {
		c.fail(imp.P, "%v", err)
	}
	if m, ok := c.prog.modules[path]; ok {
		if m == nil {
			c.fail(imp.P, "circular import of module %s", imp.Module)
		}
		return m
	}
	c.prog.modules[path] = nil
	src, err := os.ReadFile(path)
	if err != nil {
		c.fail(imp.P, "%v", err)
	}
	script, err := syntax.Parse(string(src))
	if err != nil {
		c.fail(imp.P, "module %s: %v", imp.Module, err)
	}
	ms := &scope{frame: c.prog.root, names: map[string]*binding{}, isolated: true}
	for _, sub := range script.Imports {
		c.importInto(ms, sub, dirOf(path))
	}
	c.declareAll(ms, script.Decls, func(slot int, init func(*frame) value.Value) {
		c.prog.inits = append(c.prog.inits, rootInit{slot: slot, init: init})
	})
	m := &module{names: map[string]*binding{}}
	for _, d := range script.Decls {
		m.names[d.Name] = ms.names[d.Name]
	}
	if script.Body != nil {
		m.names["main"] = c.moduleMain(script, path, imp)
	}
	c.prog.modules[path] = m
	return m
}

// moduleMain exposes the body of an imported script as main({payload,
// vars, attributes}), the convention MUnit uses to run assertion scripts.
func (c *compiler) moduleMain(script *syntax.Script, path string, imp syntax.Import) *binding {
	opts := c.prog.opts
	opts.Dir = dirOf(path)
	sub, err := CompileScript(script, opts)
	if err != nil {
		c.fail(imp.P, "module %s: %v", imp.Module, err)
	}
	fn := value.FuncValue(&value.Func{Name: "main", Params: []string{"context"}, Call: func(args []value.Value) (value.Value, error) {
		inputs := map[string]value.Value{}
		if ctx := arg(args, 0).Object(); ctx != nil {
			for i, n := 0, ctx.Len(); i < n; i++ {
				inputs[ctx.Key(i)] = ctx.Val(i)
			}
		}
		return sub.Eval(inputs)
	}})
	b := &binding{slot: c.prog.root.n, meta: &slotMeta{}}
	c.prog.root.n++
	c.prog.inits = append(c.prog.inits, rootInit{slot: b.slot, init: func(*frame) value.Value { return fn }})
	return b
}

func constant(v value.Value) evalFn {
	return func(*frame) (value.Value, error) { return v, nil }
}

func (c *compiler) load(depth, slot int) evalFn {
	switch depth {
	case 0:
		return func(f *frame) (value.Value, error) {
			if v := f.slots[slot]; v.K != value.KindThunk {
				return v, nil
			}
			return force(f, slot)
		}
	case 1:
		return func(f *frame) (value.Value, error) {
			p := f.parent
			if v := p.slots[slot]; v.K != value.KindThunk {
				return v, nil
			}
			return force(p, slot)
		}
	}
	return func(f *frame) (value.Value, error) {
		for i := 0; i < depth; i++ {
			f = f.parent
		}
		if v := f.slots[slot]; v.K != value.KindThunk {
			return v, nil
		}
		return force(f, slot)
	}
}

func qualified(n *syntax.Ident) string {
	if n.Ns == "" {
		return n.Name
	}
	return n.Ns + "::" + n.Name
}

func (c *compiler) ident(n *syntax.Ident, sc *scope) evalFn {
	key := qualified(n)
	if b, depth, ok := sc.lookup(key); ok {
		return c.load(depth, b.slot)
	}
	if f := c.prog.builtin(n.Name); f != nil {
		return constant(value.FuncValue(f))
	}
	if n.Ns != "" || strings.HasPrefix(n.Name, "$") || sc.inModule() {
		if strings.HasPrefix(n.Name, "$") {
			c.fail(n.P, "%s can only be used inside a lambda argument", n.Name)
		}
		c.fail(n.P, "unable to resolve reference of %q", key)
	}
	rs := sc.rootScope()
	slot := rs.frame.n
	rs.frame.n++
	rs.declare(n.Name, slot)
	c.prog.dynamic[n.Name] = slot
	b, depth, _ := sc.lookup(n.Name)
	load, pos := c.load(depth, b.slot), n.P
	return func(f *frame) (value.Value, error) {
		v, err := load(f)
		return v, at(pos, err)
	}
}

// lambdaMaker compiles a function body and returns a constructor that
// creates the closure for a given enclosing frame.
func (c *compiler) lambdaMaker(name string, params []syntax.Param, body syntax.Node, sc *scope) func(*frame) value.Value {
	fi := &frameInfo{}
	ls := &scope{parent: sc, frame: fi, names: map[string]*binding{}, loop: true}
	n := len(params)
	names := make([]string, n)
	metas := make([]*slotMeta, n)
	bindings := make([]*binding, n)
	for i, prm := range params {
		names[i] = prm.Name
		bindings[i] = ls.alloc(prm.Name)
		metas[i] = bindings[i].meta
	}
	defaults := make([]evalFn, n)
	for i, prm := range params {
		if prm.Default != nil {
			defaults[i] = c.expr(prm.Default, ls)
		}
	}
	plan := &lambdaPlan{name: name, names: names, metas: metas, defaults: defaults, frame: fi, body: c.expr(body, ls)}
	// Unused trailing parameters, such as the index of (item, index) or the
	// $$ and $$$ of an implicit lambda, need no slot.
	for n > 0 && fi.n == n && bindings[n-1].uses == 0 && defaults[n-1] == nil {
		n--
		fi.n--
	}
	plan.n = n
	return plan.closure
}

// lambdaPlan is a compiled function body.
type lambdaPlan struct {
	name     string
	names    []string
	metas    []*slotMeta
	defaults []evalFn
	frame    *frameInfo
	body     evalFn
	n        int
}

// closure creates the function value for an enclosing frame.
func (l *lambdaPlan) closure(f *frame) value.Value {
	fn := &value.Func{Name: l.name, Params: l.names}
	fn.Call = func(args []value.Value) (value.Value, error) { return l.call(f, args) }
	fn.Default = func(i int) (value.Value, bool, error) { return l.defaultValue(f, i) }
	return value.FuncValue(fn)
}

func (l *lambdaPlan) call(f *frame, args []value.Value) (value.Value, error) {
	nf := newFrame(l.frame.n, f)
	for i := 0; i < l.n; i++ {
		v, err := l.argument(nf, args, i)
		if err != nil {
			return value.Null, err
		}
		nf.slots[i] = v
	}
	return l.body(nf)
}

// argument returns parameter i from args or its default value.
func (l *lambdaPlan) argument(nf *frame, args []value.Value, i int) (value.Value, error) {
	if i >= len(args) {
		if l.defaults[i] == nil {
			return value.Null, nil
		}
		return l.defaults[i](nf)
	}
	if v := args[i]; l.metas[i].multi && v.K == value.KindArray {
		return shareable(v)
	}
	return args[i], nil
}

func (l *lambdaPlan) defaultValue(f *frame, i int) (value.Value, bool, error) {
	if i >= l.n || l.defaults[i] == nil {
		return value.Null, false, nil
	}
	v, err := l.defaults[i](newFrame(l.frame.n, f))
	return v, true, err
}

func (c *compiler) expr(n syntax.Node, sc *scope) evalFn {
	switch n.(type) {
	case *syntax.ArrayLit, *syntax.ObjectLit:
		if isLiteral(n) {
			return c.fold(n, sc)
		}
	}
	return c.compileNode(n, sc)
}

// fold evaluates a literal-only constructor once at compile time. Values are
// immutable, so the result can be shared by every evaluation.
func (c *compiler) fold(n syntax.Node, sc *scope) evalFn {
	v, err := c.compileNode(n, sc)(nil)
	if err != nil {
		c.fail(n.Position(), "%v", err)
	}
	return constant(v)
}

// isLiteral reports whether n is built only from literals.
func isLiteral(n syntax.Node) bool {
	switch n := n.(type) {
	case *syntax.NullLit, *syntax.BoolLit, *syntax.NumLit, *syntax.DateLit, *syntax.RegexLit:
		return true
	case *syntax.StrLit:
		return !hasInterpolation(n)
	case *syntax.Paren:
		return isLiteral(n.X)
	case *syntax.ArrayLit:
		return literalElements(n.Elems)
	case *syntax.ObjectLit:
		return literalFields(n.Fields)
	}
	return false
}

func hasInterpolation(s *syntax.StrLit) bool {
	for _, p := range s.Parts {
		if p.Expr != nil {
			return true
		}
	}
	return false
}

func literalElements(elems []syntax.ArrayElem) bool {
	for _, el := range elems {
		if el.Cond != nil || !isLiteral(el.X) {
			return false
		}
	}
	return true
}

func literalFields(fields []syntax.ObjField) bool {
	for _, f := range fields {
		if f.Spread || f.KeyExpr != nil || f.Cond != nil || !isLiteral(f.Value) {
			return false
		}
	}
	return true
}

func (c *compiler) compileNode(n syntax.Node, sc *scope) evalFn {
	switch n := n.(type) {
	case *syntax.NullLit:
		return constant(value.Null)
	case *syntax.BoolLit:
		return constant(value.Bool(n.V))
	case *syntax.NumLit:
		v, ok := value.ParseNumber(n.Raw)
		if !ok {
			c.fail(n.P, "invalid number %q", n.Raw)
		}
		return constant(v)
	case *syntax.StrLit:
		return c.str(n, sc)
	case *syntax.DateLit:
		v, err := parseTemporalLiteral(n.Raw)
		if err != nil {
			c.fail(n.P, "%v", err)
		}
		return constant(v)
	case *syntax.RegexLit:
		re, err := compileRegex(n.Src)
		if err != nil {
			c.fail(n.P, "%v", err)
		}
		return constant(value.Regex(n.Src, re))
	case *syntax.Ident:
		return c.ident(n, sc)
	case *syntax.Paren:
		return c.expr(n.X, sc)
	case *syntax.ArrayLit:
		return c.array(n, sc)
	case *syntax.ObjectLit:
		return c.object(n.Fields, sc)
	case *syntax.KVPair:
		return c.object([]syntax.ObjField{n.Field}, sc)
	case *syntax.Select:
		return c.selector(n, sc)
	case *syntax.Exists:
		return c.exists(n, sc)
	case *syntax.Call:
		return c.call(n, sc)
	case *syntax.Unary:
		return c.unary(n, sc)
	case *syntax.Binary:
		return c.binary(n, sc)
	case *syntax.As:
		return c.as(n, sc)
	case *syntax.Is:
		x := c.expr(n.X, sc)
		typ := n.Type
		return func(f *frame) (value.Value, error) {
			v, err := x(f)
			if err != nil {
				return value.Null, err
			}
			return value.Bool(isType(v, typ)), nil
		}
	case *syntax.RangeExpr:
		return c.rangeExpr(n, sc)
	case *syntax.If:
		return c.ifExpr(n, sc)
	case *syntax.Lambda:
		mk := c.lambdaMaker("", n.Params, n.Body, sc)
		return func(f *frame) (value.Value, error) { return mk(f), nil }
	case *syntax.Do:
		return c.doExpr(n, sc)
	case *syntax.Match:
		return c.match(n, sc)
	}
	c.fail(n.Position(), "unsupported expression %T", n)
	return nil
}

// strPart is a literal or interpolated part of a string template.
type strPart struct {
	lit string
	fn  evalFn
}

func (c *compiler) str(n *syntax.StrLit, sc *scope) evalFn {
	if !hasInterpolation(n) {
		var b strings.Builder
		for _, p := range n.Parts {
			b.WriteString(p.Lit)
		}
		return constant(value.Str(b.String()))
	}
	parts := make([]strPart, len(n.Parts))
	for i, p := range n.Parts {
		parts[i].lit = p.Lit
		if p.Expr != nil {
			parts[i].fn = c.expr(p.Expr, sc)
		}
	}
	pos := n.P
	return func(f *frame) (value.Value, error) {
		s, err := interpolate(parts, f)
		return value.Str(s), at(pos, err)
	}
}

func interpolate(parts []strPart, f *frame) (string, error) {
	var b strings.Builder
	for _, p := range parts {
		if p.fn == nil {
			b.WriteString(p.lit)
			continue
		}
		v, err := p.fn(f)
		if err != nil {
			return "", err
		}
		s, err := value.ToString(v)
		if err != nil {
			return "", err
		}
		b.WriteString(s)
	}
	return b.String(), nil
}

// optional pairs an expression with an optional condition.
type optional struct {
	fn   evalFn
	cond evalFn
}

// include reports whether the condition, if any, holds.
func (o optional) include(f *frame) (bool, error) {
	if o.cond == nil {
		return true, nil
	}
	ok, err := o.cond(f)
	return ok.Truthy(), err
}

func (c *compiler) optional(x, cond syntax.Node, sc *scope) optional {
	o := optional{fn: c.expr(x, sc)}
	if cond != nil {
		o.cond = c.expr(cond, sc)
	}
	return o
}

func (c *compiler) array(n *syntax.ArrayLit, sc *scope) evalFn {
	elems := make([]optional, len(n.Elems))
	for i, el := range n.Elems {
		elems[i] = c.optional(el.X, el.Cond, sc)
	}
	return func(f *frame) (value.Value, error) {
		items := make([]value.Value, 0, len(elems))
		for _, el := range elems {
			ok, err := el.include(f)
			if err != nil || !ok {
				if err != nil {
					return value.Null, err
				}
				continue
			}
			v, err := el.fn(f)
			if err != nil {
				return value.Null, err
			}
			items = append(items, v)
		}
		return value.NewArray(items), nil
	}
}

type fieldPlan struct {
	optional
	pos    syntax.Pos
	name   string
	key    evalFn
	spread bool
}

func (c *compiler) object(fields []syntax.ObjField, sc *scope) evalFn {
	plans := make([]fieldPlan, len(fields))
	static := true
	for i, fd := range fields {
		p := fieldPlan{optional: c.optional(fd.Value, fd.Cond, sc), pos: fd.P, name: fd.KeyName, spread: fd.Spread}
		if fd.KeyExpr != nil {
			p.key = c.expr(fd.KeyExpr, sc)
		}
		static = static && !p.spread && p.key == nil && p.cond == nil
		plans[i] = p
	}
	if static {
		return staticObject(plans)
	}
	return func(f *frame) (value.Value, error) {
		b := value.NewBuilder(len(plans))
		for i := range plans {
			if err := plans[i].addTo(b, f); err != nil {
				return value.Null, err
			}
		}
		return b.Build(), nil
	}
}

// staticObject builds objects whose keys are all known at compile time, so
// every object shares one key shape.
func staticObject(plans []fieldPlan) evalFn {
	keys := make([]string, len(plans))
	vals := make([]evalFn, len(plans))
	for i, p := range plans {
		keys[i], vals[i] = p.name, p.fn
	}
	shape := value.NewShape(keys)
	return func(f *frame) (value.Value, error) {
		out := make([]value.Value, len(vals))
		for i, fn := range vals {
			v, err := fn(f)
			if err != nil {
				return value.Null, err
			}
			out[i] = v
		}
		return value.ObjectValue(value.NewShaped(shape, out)), nil
	}
}

// addTo evaluates the field and appends it to b when its condition holds.
func (p *fieldPlan) addTo(b *value.Builder, f *frame) error {
	ok, err := p.include(f)
	if err != nil || !ok {
		return err
	}
	v, err := p.fn(f)
	if err != nil {
		return err
	}
	if p.spread {
		return at(p.pos, spreadInto(b, v))
	}
	name := p.name
	if p.key != nil {
		kv, err := p.key(f)
		if err != nil {
			return err
		}
		if name, err = keyString(kv); err != nil {
			return at(p.pos, err)
		}
	}
	b.Add(name, v)
	return nil
}

func keyString(v value.Value) (string, error) {
	if v.K == value.KindNull {
		return "", errors.New("object key cannot be null")
	}
	return value.ToString(v)
}

// spreadInto merges an object, or every object of an array, into b.
func spreadInto(b *value.Builder, v value.Value) error {
	switch v.K {
	case value.KindNull:
		return nil
	case value.KindObject:
		b.AddObject(v.Object())
		return nil
	case value.KindArray:
		return v.Array().Each(func(_ int, el value.Value) error {
			return spreadInto(b, el)
		})
	}
	return fmt.Errorf("cannot merge %s into an object", v.TypeName())
}

func (c *compiler) selector(n *syntax.Select, sc *scope) evalFn {
	x := c.expr(n.X, sc)
	name, pos := n.Name, n.P
	var sel func(value.Value, *frame) (value.Value, error)
	switch n.Kind {
	case syntax.SelKey:
		sel = func(v value.Value, _ *frame) (value.Value, error) { return selectKey(v, name) }
	case syntax.SelMulti:
		sel = func(v value.Value, _ *frame) (value.Value, error) { return selectMulti(v, name) }
	case syntax.SelDescendant:
		sel = func(v value.Value, _ *frame) (value.Value, error) { return selectDescendants(v, name) }
	case syntax.SelAttr:
		sel = func(value.Value, *frame) (value.Value, error) { return value.Null, nil }
	case syntax.SelIndex:
		idx := c.expr(n.Index, sc)
		sel = func(v value.Value, f *frame) (value.Value, error) {
			i, err := idx(f)
			if err != nil {
				return value.Null, err
			}
			return selectIndex(v, i)
		}
	case syntax.SelFilter:
		cond := c.expr(n.Index, sc)
		sel = func(v value.Value, f *frame) (value.Value, error) {
			fv, err := cond(f)
			if err != nil {
				return value.Null, err
			}
			return filterSelector(v, fv)
		}
	}
	return func(f *frame) (value.Value, error) {
		v, err := x(f)
		if err != nil {
			return value.Null, err
		}
		r, err := sel(v, f)
		return r, at(pos, err)
	}
}

func (c *compiler) exists(n *syntax.Exists, sc *scope) evalFn {
	if s, ok := n.X.(*syntax.Select); ok && s.Kind == syntax.SelKey {
		target := c.expr(s.X, sc)
		name := s.Name
		return func(f *frame) (value.Value, error) {
			v, err := target(f)
			if err != nil {
				return value.Null, err
			}
			return value.Bool(hasKey(v, name)), nil
		}
	}
	x := c.expr(n.X, sc)
	return func(f *frame) (value.Value, error) {
		v, err := x(f)
		if err != nil {
			return value.Null, err
		}
		return value.Bool(!v.IsNull()), nil
	}
}

func (c *compiler) call(n *syntax.Call, sc *scope) evalFn {
	args := make([]evalFn, len(n.Args))
	for i, a := range n.Args {
		args[i] = c.expr(a, sc)
	}
	if impl := c.builtinCallee(n, sc, len(args)); impl != nil {
		return callFunc(impl, args, n.P)
	}
	callee := c.expr(n.Fn, sc)
	pos := n.P
	return func(f *frame) (value.Value, error) {
		cv, err := callee(f)
		if err != nil {
			return value.Null, err
		}
		fn := cv.Func()
		if fn == nil {
			return value.Null, at(pos, fmt.Errorf("cannot call a value of type %s", cv.TypeName()))
		}
		return callFunc(fn, args, pos)(f)
	}
}

// builtinCallee resolves a call to a standard library function that is not
// shadowed by a local name, checking its arity at compile time.
func (c *compiler) builtinCallee(n *syntax.Call, sc *scope, nargs int) *value.Func {
	id, ok := n.Fn.(*syntax.Ident)
	if !ok {
		return nil
	}
	if _, _, local := sc.lookup(qualified(id)); local {
		return nil
	}
	def := lookupBuiltin(id.Name)
	if def == nil {
		return nil
	}
	if nargs < def.min || (def.max >= 0 && nargs > def.max) {
		c.fail(n.P, "function %s expects %s, got %d", id.Name, arityText(def), nargs)
	}
	return c.prog.builtin(id.Name)
}

// callFunc evaluates the arguments and calls fn.
func callFunc(fn *value.Func, args []evalFn, pos syntax.Pos) evalFn {
	return func(f *frame) (value.Value, error) {
		vals := make([]value.Value, len(args))
		for i, a := range args {
			v, err := a(f)
			if err != nil {
				return value.Null, err
			}
			vals[i] = v
		}
		r, err := fn.Call(vals)
		return r, at(pos, err)
	}
}

func (c *compiler) unary(n *syntax.Unary, sc *scope) evalFn {
	x := c.expr(n.X, sc)
	pos := n.P
	op := negateNumber
	if n.Op == "!" {
		op = negateBool
	}
	return func(f *frame) (value.Value, error) {
		v, err := x(f)
		if err != nil {
			return value.Null, err
		}
		r, err := op(v)
		return r, at(pos, err)
	}
}

func negateBool(v value.Value) (value.Value, error) {
	if v.K != value.KindBool && v.K != value.KindNull {
		return value.Null, fmt.Errorf("cannot negate %s", v.TypeName())
	}
	return value.Bool(!v.Truthy()), nil
}

func negateNumber(v value.Value) (value.Value, error) {
	if v.K != value.KindNumber {
		return value.Null, fmt.Errorf("cannot negate %s", v.TypeName())
	}
	return value.Num(-v.N), nil
}

func (c *compiler) binary(n *syntax.Binary, sc *scope) evalFn {
	l, r := c.expr(n.L, sc), c.expr(n.R, sc)
	switch n.Op {
	case "and", "or":
		return logical(l, r, n.Op == "and")
	case "default":
		return defaultOp(l, r)
	}
	impl, pos := binaryOps[n.Op], n.P
	return func(f *frame) (value.Value, error) {
		lv, err := l(f)
		if err != nil {
			return value.Null, err
		}
		rv, err := r(f)
		if err != nil {
			return value.Null, err
		}
		v, err := impl(lv, rv)
		return v, at(pos, err)
	}
}

// logical implements short-circuit and/or.
func logical(l, r evalFn, isAnd bool) evalFn {
	return func(f *frame) (value.Value, error) {
		lv, err := l(f)
		if err != nil || lv.Truthy() != isAnd {
			return value.Bool(!isAnd), err
		}
		rv, err := r(f)
		return value.Bool(rv.Truthy()), err
	}
}

// defaultOp evaluates r only when l is null.
func defaultOp(l, r evalFn) evalFn {
	return func(f *frame) (value.Value, error) {
		lv, err := l(f)
		if err != nil || !lv.IsNull() {
			return lv, err
		}
		return r(f)
	}
}

func (c *compiler) as(n *syntax.As, sc *scope) evalFn {
	x := c.expr(n.X, sc)
	var opts evalFn
	if n.Opts != nil {
		opts = c.expr(n.Opts, sc)
	}
	typ, pos := n.Type, n.P
	return func(f *frame) (value.Value, error) {
		v, err := x(f)
		if err != nil {
			return value.Null, err
		}
		var props *value.Object
		if opts != nil {
			ov, err := opts(f)
			if err != nil {
				return value.Null, err
			}
			props = ov.Object()
		}
		r, err := coerce(v, typ, props)
		return r, at(pos, err)
	}
}

func (c *compiler) rangeExpr(n *syntax.RangeExpr, sc *scope) evalFn {
	from, to := c.expr(n.From, sc), c.expr(n.To, sc)
	pos := n.P
	return func(f *frame) (value.Value, error) {
		a, err := from(f)
		if err != nil {
			return value.Null, err
		}
		b, err := to(f)
		if err != nil {
			return value.Null, err
		}
		if a.K != value.KindNumber || b.K != value.KindNumber {
			return value.Null, at(pos, fmt.Errorf("range bounds must be numbers, got %s and %s", a.TypeName(), b.TypeName()))
		}
		return value.Range(int(a.N), int(b.N)), nil
	}
}

func (c *compiler) ifExpr(n *syntax.If, sc *scope) evalFn {
	cond, then := c.expr(n.Cond, sc), c.expr(n.Then, sc)
	els := constant(value.Null)
	if n.Else != nil {
		els = c.expr(n.Else, sc)
	}
	pos := n.P
	return func(f *frame) (value.Value, error) {
		cv, err := cond(f)
		if err != nil {
			return value.Null, err
		}
		if cv.K != value.KindBool && cv.K != value.KindNull {
			return value.Null, at(pos, fmt.Errorf("if condition must be a Boolean, got %s", cv.TypeName()))
		}
		if cv.Truthy() {
			return then(f)
		}
		return els(f)
	}
}

func (c *compiler) doExpr(n *syntax.Do, sc *scope) evalFn {
	fi := &frameInfo{}
	ds := &scope{parent: sc, frame: fi, names: map[string]*binding{}}
	type slotInit struct {
		slot int
		init func(*frame) value.Value
	}
	var inits []slotInit
	c.declareAll(ds, n.Decls, func(slot int, init func(*frame) value.Value) {
		inits = append(inits, slotInit{slot, init})
	})
	body := c.expr(n.Body, ds)
	return func(f *frame) (value.Value, error) {
		nf := newFrame(fi.n, f)
		for _, in := range inits {
			nf.slots[in.slot] = in.init(nf)
		}
		return body(nf)
	}
}

type casePlan struct {
	kind  int
	lit   evalFn
	typ   string
	re    value.Value
	bind  bool
	guard evalFn
	body  evalFn
}

func (c *compiler) match(n *syntax.Match, sc *scope) evalFn {
	subject := c.expr(n.X, sc)
	plans := make([]casePlan, len(n.Cases))
	for i, cs := range n.Cases {
		plans[i] = c.compileCase(cs, sc)
	}
	pos := n.P
	return func(f *frame) (value.Value, error) {
		v, err := subject(f)
		if err != nil {
			return value.Null, err
		}
		for i := range plans {
			nf, ok, err := plans[i].accept(v, f)
			if err != nil {
				return value.Null, err
			}
			if ok {
				return plans[i].body(nf)
			}
		}
		return value.Null, at(pos, fmt.Errorf("no case matched the value %s", describe(v)))
	}
}

func (c *compiler) compileCase(cs syntax.Case, sc *scope) casePlan {
	p := casePlan{kind: cs.Kind, typ: cs.Type}
	inner := sc
	if cs.Name != "" {
		inner = &scope{parent: sc, frame: &frameInfo{}, names: map[string]*binding{}}
		inner.alloc(cs.Name)
		p.bind = true
	}
	switch cs.Kind {
	case syntax.CaseLiteral:
		p.lit = c.expr(cs.Lit, sc)
	case syntax.CaseRegex:
		rv, err := c.expr(cs.Regex, sc)(nil)
		if err != nil || rv.K != value.KindRegex {
			c.fail(cs.P, "matches requires a regular expression literal")
		}
		p.re = rv
	}
	if cs.Guard != nil {
		p.guard = c.expr(cs.Guard, inner)
	}
	p.body = c.expr(cs.Body, inner)
	return p
}

// accept tests v against the case pattern and guard. It returns the frame
// the body runs in, holding the bound name when the case declares one.
func (p *casePlan) accept(v value.Value, f *frame) (*frame, bool, error) {
	bound, ok, err := p.pattern(v, f)
	if err != nil || !ok {
		return nil, false, err
	}
	nf := f
	if p.bind {
		nf = newFrame(1, f)
		nf.slots[0] = bound
	}
	if p.guard == nil {
		return nf, true, nil
	}
	g, err := p.guard(nf)
	return nf, g.Truthy(), err
}

// pattern tests the case pattern and returns the value to bind.
func (p *casePlan) pattern(v value.Value, f *frame) (value.Value, bool, error) {
	switch p.kind {
	case syntax.CaseLiteral:
		lv, err := p.lit(f)
		if err != nil {
			return value.Null, false, err
		}
		eq, err := value.Equal(v, lv)
		return v, eq, err
	case syntax.CaseIs:
		return v, isType(v, p.typ), nil
	case syntax.CaseRegex:
		return matchRegex(p.re, v)
	}
	return v, true, nil
}

// matchRegex fully matches a string against re and returns its groups.
func matchRegex(re value.Value, v value.Value) (value.Value, bool, error) {
	if v.K != value.KindString {
		return value.Null, false, nil
	}
	groups := re.RegexValue().FindStringSubmatch(v.S)
	if groups == nil || len(groups[0]) != len(v.S) {
		return value.Null, false, nil
	}
	return stringsToArray(groups), true, nil
}

func describe(v value.Value) string {
	if s, err := value.ToString(v); err == nil {
		return fmt.Sprintf("%q (%s)", s, v.TypeName())
	}
	return v.TypeName()
}

func stringsToArray(ss []string) value.Value {
	items := make([]value.Value, len(ss))
	for i, s := range ss {
		items[i] = value.Str(s)
	}
	return value.NewArray(items)
}
