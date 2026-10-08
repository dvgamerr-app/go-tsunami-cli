package interp

import (
	"fmt"
	"sort"

	"github.com/touno-io/go-tsunami-cli/internal/value"
)

type builtinImpl func(args []value.Value) (value.Value, error)

// builtinDef describes a standard library function. Implementations must
// not retain the args slice: callers reuse it between calls.
type builtinDef struct {
	min, max int
	fn       builtinImpl
	// factory builds an implementation bound to a program, for functions
	// that depend on program options.
	factory func(p *Program) builtinImpl
}

var builtinDefs = map[string]*builtinDef{}

func register(name string, minArgs, maxArgs int, fn builtinImpl) {
	builtinDefs[name] = &builtinDef{min: minArgs, max: maxArgs, fn: fn}
}

func registerProgram(name string, minArgs, maxArgs int, factory func(p *Program) builtinImpl) {
	builtinDefs[name] = &builtinDef{min: minArgs, max: maxArgs, factory: factory}
}

func lookupBuiltin(name string) *builtinDef { return builtinDefs[name] }

func arityText(d *builtinDef) string {
	switch {
	case d.min == d.max && d.min == 1:
		return "1 argument"
	case d.min == d.max:
		return fmt.Sprintf("%d arguments", d.min)
	case d.max < 0:
		return fmt.Sprintf("at least %d arguments", d.min)
	}
	return fmt.Sprintf("%d to %d arguments", d.min, d.max)
}

// builtin returns the function value for a standard library function bound
// to this program, or nil when name is not a builtin.
func (p *Program) builtin(name string) *value.Func {
	if f, ok := p.builtins[name]; ok {
		return f
	}
	def := lookupBuiltin(name)
	if def == nil {
		return nil
	}
	impl := def.fn
	if def.factory != nil {
		impl = def.factory(p)
	}
	f := &value.Func{Name: name, Call: func(args []value.Value) (value.Value, error) {
		if len(args) < def.min || (def.max >= 0 && len(args) > def.max) {
			return value.Null, fmt.Errorf("function %s expects %s, got %d", name, arityText(def), len(args))
		}
		return impl(args)
	}}
	p.builtins[name] = f
	return f
}

// BuiltinNames lists the available standard library functions.
func BuiltinNames() []string {
	out := make([]string, 0, len(builtinDefs))
	for n := range builtinDefs {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func arg(args []value.Value, i int) value.Value {
	if i < len(args) {
		return args[i]
	}
	return value.Null
}

func fnArg(args []value.Value, i int, name string) (*value.Func, error) {
	v := arg(args, i)
	if f := v.Func(); f != nil {
		return f, nil
	}
	return nil, fmt.Errorf("function %s expects a function as argument %d, got %s", name, i+1, v.TypeName())
}

func strArg(args []value.Value, i int, name string) (string, error) {
	v := arg(args, i)
	switch v.K {
	case value.KindString:
		return v.S, nil
	case value.KindArray, value.KindObject, value.KindFunction:
		return "", fmt.Errorf("function %s expects a String as argument %d, got %s", name, i+1, v.TypeName())
	}
	return value.ToString(v)
}

func numArg(args []value.Value, i int, name string) (float64, error) {
	v := arg(args, i)
	if n, ok := numeric(v); ok {
		return n, nil
	}
	return 0, fmt.Errorf("function %s expects a Number as argument %d, got %s", name, i+1, v.TypeName())
}

func iterArg(args []value.Value, i int, name string) (*value.Array, error) {
	v := arg(args, i)
	if a, ok := asIterable(v); ok {
		return a, nil
	}
	return nil, fmt.Errorf("function %s expects an Array as argument %d, got %s", name, i+1, v.TypeName())
}

func objArg(args []value.Value, i int, name string) (*value.Object, error) {
	v := arg(args, i)
	if o := v.Object(); o != nil {
		return o, nil
	}
	return nil, fmt.Errorf("function %s expects an Object as argument %d, got %s", name, i+1, v.TypeName())
}

func init() {
	registerCore()
	registerArrays()
	registerObjects()
	registerStrings()
	registerNumbers()
	registerAsserts()
}
