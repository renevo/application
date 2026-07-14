package funcs

import (
	"fmt"
	"os"

	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
)

// MakeEnvFunc returns an HCL function that reads an environment variable by
// name and accepts an optional fallback string. The fallback is returned when
// the variable is unset or contains an empty value.
func MakeEnvFunc() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{
			{
				Name: "name",
				Type: cty.String,
			},
		},
		VarParam: &function.Parameter{
			Name: "default",
			Type: cty.String,
		},
		Type: func(args []cty.Value) (cty.Type, error) {
			if len(args) > 2 {
				return cty.NilType, fmt.Errorf("env accepts at most two arguments")
			}

			return cty.String, nil
		},
		Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
			name := args[0].AsString()
			if val := os.Getenv(name); val != "" {
				return cty.StringVal(val), nil
			}

			if len(args) == 2 {
				return cty.StringVal(args[1].AsString()), nil
			}

			return cty.StringVal(""), nil
		},
	})
}
