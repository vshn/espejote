package jsonnet

// wrapJsonnetFunctionErrorToTuple wraps a function that returns a single value and an error into a function that returns a tuple [ret, error message] and never returns an error.
func WrapJsonnetFunctionErrorToTuple(f func([]any) (any, error)) func([]any) (any, error) {
	return func(args []any) (any, error) {
		ret, err := f(args)
		if err != nil {
			return []any{ret, err.Error()}, nil
		}
		return []any{ret, nil}, nil
	}
}
