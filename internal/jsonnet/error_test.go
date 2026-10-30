package jsonnet

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_WrapJsonnetFunctionErrorToTuple(t *testing.T) {
	id := func(args []any) (any, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("Expected one argument")
		}
		return args[0], nil
	}

	wrapped := WrapJsonnetFunctionErrorToTuple(id)

	cases := []struct {
		args   []any
		result any
		err    any
	}{
		{
			args:   []any{"foo"},
			result: "foo",
			err:    nil,
		},
		{
			args:   []any{17},
			result: 17,
			err:    nil,
		},
		{
			args:   []any{[]string{"foo", "bar"}},
			result: []string{"foo", "bar"},
			err:    nil,
		},
		{
			args:   []any{},
			result: nil,
			err:    "Expected one argument",
		},
	}
	for _, c := range cases {
		ret, err := wrapped(c.args)
		assert.NoError(t, err)
		retarr, ok := ret.([]any)
		require.True(t, ok)
		require.Equal(t, 2, len(retarr))
		retval := retarr[0]
		reterr := retarr[1]
		assert.Equal(t, c.result, retval)
		assert.Equal(t, c.err, reterr)
	}
}
