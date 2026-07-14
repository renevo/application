package confighcl

import (
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/matryer/is"
)

func parseTestBody(is *is.I, source string) hcl.Body {
	is.Helper()
	file, diags := hclsyntax.ParseConfig([]byte(source), "test.hcl", hcl.InitialPos)
	is.True(!diags.HasErrors()) // test fixture should contain valid HCL
	return file.Body
}

func TestDecodeBody(t *testing.T) {
	tests := []struct {
		name string
		run  func(*is.I)
	}{
		{
			name: "rejects nil target",
			run: func(is *is.I) {
				var target *struct{}
				diags := DecodeBody(parseTestBody(is, ""), nil, target)
				is.True(diags.HasErrors()) // nil target should produce an error diagnostic
			},
		},
		{
			name: "does not infer duration by name",
			run: func(is *is.I) {
				type Duration int
				target := struct {
					Value Duration `config:"value"`
				}{}
				diags := DecodeBody(parseTestBody(is, "value = 5"), nil, &target)
				is.True(!diags.HasErrors())         // named integer should decode without duration parsing
				is.Equal(target.Value, Duration(5)) // named integer should retain its numeric value
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.run(is.New(t))
		})
	}
}
