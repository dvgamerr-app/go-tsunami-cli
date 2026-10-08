// Package interp compiles DataWeave-compatible scripts into Go closures and
// evaluates them against input values.
//
// Compilation resolves every identifier to a frame slot ahead of time, so
// evaluation performs no name lookups. Array operations such as map and
// filter stay lazy when their input is a stream, which lets a transformation
// like "payload map (row) -> {...}" run in constant memory.
package interp

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/touno-io/go-tsunami-cli/internal/codec"
	"github.com/touno-io/go-tsunami-cli/internal/syntax"
	"github.com/touno-io/go-tsunami-cli/internal/value"
)

// Options configure compilation and evaluation.
type Options struct {
	// Dir is the directory of the script, used to resolve modules and
	// resources. It defaults to the working directory.
	Dir string
	// ModulePaths are extra directories searched for imported modules.
	ModulePaths []string
	// ResourcePaths are extra directories searched by readUrl("classpath://").
	ResourcePaths []string
	// Properties are returned by p(name).
	Properties map[string]string
	// Log receives the output of log(). It defaults to os.Stderr.
	Log io.Writer
}

// Directive is a media type with writer or reader properties.
type Directive struct {
	Mime  string
	Props map[string]string
}

// Program is a compiled script. It is safe for concurrent use as long as
// each evaluation uses its own inputs.
type Program struct {
	opts     Options
	output   *Directive
	inputs   map[string]Directive
	body     evalFn
	root     *frameInfo
	inits    []rootInit
	dynamic  map[string]int
	payload  *binding
	builtins map[string]*value.Func
	modules  map[string]*module
}

// rootInit initializes a root slot at the start of an evaluation.
type rootInit struct {
	slot int
	init func(root *frame) value.Value
}

const (
	slotPayload = iota
	slotVars
	slotAttributes
	firstFreeSlot
)

// Compile parses and compiles a script.
func Compile(src string, opts Options) (*Program, error) {
	script, err := syntax.Parse(src)
	if err != nil {
		return nil, err
	}
	return CompileScript(script, opts)
}

// CompileScript compiles a parsed script.
func CompileScript(script *syntax.Script, opts Options) (prog *Program, err error) {
	if script.Body == nil {
		return nil, errors.New("script has no body; add '---' followed by an expression")
	}
	if opts.Log == nil {
		opts.Log = os.Stderr
	}
	if opts.Dir == "" {
		opts.Dir = "."
	}
	p := &Program{
		opts:     opts,
		inputs:   map[string]Directive{},
		root:     &frameInfo{n: firstFreeSlot},
		dynamic:  map[string]int{},
		builtins: map[string]*value.Func{},
		modules:  map[string]*module{},
	}
	if script.Output != nil {
		p.output = &Directive{Mime: script.Output.Mime, Props: script.Output.Props}
	}
	for _, in := range script.Inputs {
		p.inputs[in.Name] = Directive{Mime: in.Mime, Props: in.Props}
	}
	c := &compiler{prog: p}
	defer c.recover(&err)
	rs := &scope{frame: p.root, names: map[string]*binding{}, root: true}
	p.payload = rs.declare("payload", slotPayload)
	rs.declare("vars", slotVars)
	rs.declare("attributes", slotAttributes)
	for _, imp := range script.Imports {
		c.importInto(rs, imp, opts.Dir)
	}
	c.declareAll(rs, script.Decls, func(slot int, init func(*frame) value.Value) {
		p.inits = append(p.inits, rootInit{slot: slot, init: init})
	})
	p.body = c.expr(script.Body, rs)
	return p, nil
}

// Output returns the output directive, or nil when the script has none.
func (p *Program) Output() *Directive { return p.output }

// Input returns the declared input directive for name, if any.
func (p *Program) Input(name string) (Directive, bool) {
	d, ok := p.inputs[name]
	return d, ok
}

// PayloadReusable reports whether the script may read the payload more than
// once. A one-shot payload stream must be buffered in that case.
func (p *Program) PayloadReusable() bool {
	return p.payload.meta.multi
}

// Eval evaluates the script. Missing payload, vars and attributes default to
// null; other names resolve against named inputs.
func (p *Program) Eval(inputs map[string]value.Value) (value.Value, error) {
	root := &frame{slots: make([]value.Value, p.root.n)}
	root.slots[slotPayload] = inputs["payload"]
	root.slots[slotVars] = orEmptyObject(inputs["vars"])
	root.slots[slotAttributes] = orEmptyObject(inputs["attributes"])
	for name, slot := range p.dynamic {
		if v, ok := inputs[name]; ok {
			root.slots[slot] = v
		} else {
			root.slots[slot] = unresolved(name)
		}
	}
	for _, in := range p.inits {
		root.slots[in.slot] = in.init(root)
	}
	if v := root.slots[slotPayload]; v.K == value.KindArray && p.payload.meta.multi && !v.Array().Restartable() {
		if _, err := v.Array().Items(); err != nil {
			return value.Null, err
		}
	}
	return p.body(root)
}

func orEmptyObject(v value.Value) value.Value {
	if v.IsNull() {
		return value.EmptyObject()
	}
	return v
}

// unresolved returns a thunk that fails when an unknown name is used.
func unresolved(name string) value.Value {
	return value.Value{K: value.KindThunk, R: &thunk{fn: func(*frame) (value.Value, error) {
		return value.Null, fmt.Errorf("unable to resolve reference of %q", name)
	}}}
}

// module is a compiled library file.
type module struct {
	names map[string]*binding
}

// findModule locates the file for a module path such as a::b::Lib.
func (p *Program) findModule(path, dir string) (string, error) {
	rel := filepath.FromSlash(strings.ReplaceAll(path, "::", "/")) + ".dwl"
	var dirs []string
	dirs = append(dirs, p.opts.ModulePaths...)
	for d := dir; ; {
		dirs = append(dirs, d, filepath.Join(d, "dw"), filepath.Join(d, "src", "main", "dw"), filepath.Join(d, "src", "main", "resources"),
			filepath.Join(d, "src", "test", "resources"), filepath.Join(d, "src", "test", "dwl"))
		parent := filepath.Dir(d)
		if parent == d || len(dirs) > 64 {
			break
		}
		d = parent
	}
	for _, d := range dirs {
		f := filepath.Join(d, rel)
		if st, err := os.Stat(f); err == nil && !st.IsDir() {
			return f, nil
		}
	}
	return "", fmt.Errorf("unable to find module %q (looked for %s)", path, rel)
}

// readResource resolves classpath:// and file paths for readUrl.
func (p *Program) readResource(url string) (string, []byte, error) {
	switch {
	case strings.HasPrefix(url, "classpath://"):
		rel := filepath.FromSlash(strings.TrimPrefix(url, "classpath://"))
		dirs := append([]string{}, p.opts.ResourcePaths...)
		for d := p.opts.Dir; ; {
			dirs = append(dirs, d, filepath.Join(d, "src", "main", "resources"), filepath.Join(d, "src", "test", "resources"))
			parent := filepath.Dir(d)
			if parent == d || len(dirs) > 64 {
				break
			}
			d = parent
		}
		for _, d := range dirs {
			f := filepath.Join(d, rel)
			if b, err := os.ReadFile(f); err == nil {
				return f, b, nil
			}
		}
		return "", nil, fmt.Errorf("resource %q not found", url)
	case strings.HasPrefix(url, "file://"):
		f := strings.TrimPrefix(url, "file://")
		b, err := os.ReadFile(f)
		return f, b, err
	case strings.Contains(url, "://"):
		return "", nil, fmt.Errorf("readUrl only supports classpath:// and file:// URLs, got %q", url)
	}
	f := url
	if !filepath.IsAbs(f) {
		f = filepath.Join(p.opts.Dir, f)
	}
	b, err := os.ReadFile(f)
	return f, b, err
}

// ReadValue decodes data in the given media type. DataWeave literal input
// (application/dw) is evaluated as an expression.
func ReadValue(src codec.Source, mime string, opts codec.ReadOptions) (value.Value, error) {
	if codec.Normalize(mime) == codec.DW {
		r, err := src.Open()
		if err != nil {
			return value.Null, err
		}
		b, err := io.ReadAll(r)
		if err != nil {
			return value.Null, err
		}
		return EvalLiteral(string(b))
	}
	f, err := codec.Lookup(mime)
	if err != nil {
		return value.Null, err
	}
	if f.Read == nil {
		return value.Null, fmt.Errorf("reading %s is not supported", f.Mime)
	}
	return f.Read(src, opts)
}

// EvalLiteral evaluates a self-contained DataWeave expression or script that
// uses no inputs, such as a test fixture.
func EvalLiteral(src string) (value.Value, error) {
	prog, err := Compile(src, Options{})
	if err != nil {
		return value.Null, err
	}
	return prog.Eval(nil)
}
