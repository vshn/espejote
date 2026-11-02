package jsonnet

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vshn/espejote/internal/bcrypt"
)

func Test_jsonnetBcrypt(t *testing.T) {
	password := "thepassword"
	salt := "IV3UaP1bJE8kMyZV"

	hp, err := jsonnetBcrypt([]any{password, salt})
	assert.NoError(t, err)
	hpString, ok := hp.(string)
	assert.True(t, ok)

	err = bcrypt.CompareHashAndPassword([]byte(hpString), []byte(password))
	assert.NoError(t, err)
}

func Test_jsonnetBcrypt_Repeatable(t *testing.T) {
	password := "thepassword"
	salt := "IV3UaP1bJE8kMyZV"

	hp, err := jsonnetBcrypt([]any{password, salt})
	assert.NoError(t, err)
	hpString, ok := hp.(string)
	assert.True(t, ok)

	err = bcrypt.CompareHashAndPassword([]byte(hpString), []byte(password))
	assert.NoError(t, err)

	hp2, err := jsonnetBcrypt([]any{password, salt})
	assert.NoError(t, err)
	assert.Equal(t, hp, hp2)
}

func Test_jsonnetBcrypt_InvalidArgs(t *testing.T) {
	_, err := jsonnetBcrypt([]any{"password"})
	assert.ErrorContains(t, err, "Expected two arguments")
}

func Test_jsonnetBcrypt_BcryptError(t *testing.T) {
	_, err := jsonnetBcrypt([]any{"password", ""})
	assert.Equal(t, bcrypt.ErrIncorrectSaltLength, err)
}

func Test_twoStringArguments(t *testing.T) {
	cases := []struct {
		Input  []any
		Output []string
		Err    error
	}{
		{
			Input:  []any{"foo", "bar"},
			Output: []string{"foo", "bar"},
			Err:    nil,
		},
		{
			Input:  []any{1, 2},
			Output: []string{"", ""},
			Err:    fmt.Errorf("Expected first argument to be string"),
		},
		{
			Input:  []any{"foo", 2},
			Output: []string{"", ""},
			Err:    fmt.Errorf("Expected second argument to be string"),
		},
		{
			Input:  []any{},
			Output: []string{"", ""},
			Err:    fmt.Errorf("Expected two arguments"),
		},
	}
	for _, c := range cases {
		a1, a2, err := twoStringArguments(c.Input)
		assert.Equal(t, c.Err, err)
		assert.Equal(t, c.Output, []string{a1, a2})
	}
}
