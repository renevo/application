package funcs

import (
	"os"

	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
)

// MakeEnvFunc returns an HCL function that reads an environment variable by
// name and accepts a fallback string. The fallback is returned when the
// variable is unset or contains an empty value.
func MakeEnvFunc() function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{
			{
				Name: "name",
				Type: cty.String,
			},
			{
				Name: "default",
				Type: cty.String,
			},
		},
		Type: function.StaticReturnType(cty.String),
		Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
			name := args[0].AsString()
			if val := os.Getenv(name); val != "" {
				return cty.StringVal(val), nil
			}

			return cty.StringVal(args[1].AsString()), nil
		},
	})
}
