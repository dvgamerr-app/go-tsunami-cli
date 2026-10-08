package value

// Func is a callable value: a builtin, a declared function or a lambda.
type Func struct {
	// Name is the declared name, empty for anonymous lambdas.
	Name string
	// Params lists the parameter names, when known.
	Params []string
	// Call invokes the function. Missing arguments are treated as null or as
	// the parameter default.
	Call func(args []Value) (Value, error)
	// Default evaluates the default value of parameter i, if it has one.
	Default func(i int) (Value, bool, error)
}

// Arity returns the number of declared parameters, or -1 when unknown.
func (f *Func) Arity() int {
	if f.Params == nil {
		return -1
	}
	return len(f.Params)
}

// ParamDefault returns the default value of parameter i, if declared.
func (f *Func) ParamDefault(i int) (Value, bool, error) {
	if f.Default == nil {
		return Null, false, nil
	}
	return f.Default(i)
}
