package jsonnet

import (
	"fmt"

	"github.com/google/go-jsonnet"
	"github.com/google/go-jsonnet/ast"

	bcrypt_lib "github.com/vshn/espejote/internal/bcrypt"
)

// ConfigureNativeFunctions registers the native functions exposed in
// `espejote-native.libsonnet` in the passed Jsonnet VM.
func ConfigureNativeFunctions(jvm *jsonnet.VM) error {

	jvm.NativeFunction(&jsonnet.NativeFunction{
		Name:   "bcrypt",
		Func:   WrapJsonnetFunctionErrorToTuple(jsonnetBcrypt),
		Params: ast.Identifiers{"password", "salt"},
	})
	return nil
}

func jsonnetBcrypt(args []any) (any, error) {
	password, salt, err := twoStringArguments(args)
	if err != nil {
		return nil, err
	}
	hash, err := bcrypt_lib.GenerateFromPasswordWithSalt([]byte(password), []byte(salt), bcrypt_lib.DefaultCost)
	if err != nil {
		return nil, err
	}
	return string(hash), nil
}

func twoStringArguments(args []any) (string, string, error) {
	if len(args) != 2 {
		return "", "", fmt.Errorf("Expected two arguments")
	}
	var arg1, arg2 string
	if value, ok := args[0].(string); ok {
		arg1 = value
	} else {
		return "", "", fmt.Errorf("Expected first argument to be string")
	}
	if value, ok := args[1].(string); ok {
		arg2 = value
	} else {
		return "", "", fmt.Errorf("Expected second argument to be string")
	}
	return arg1, arg2, nil
}
