package application

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/matryer/is"
	"github.com/renevo/application/confighcl"
)

func TestHCL(t *testing.T) {
	is := is.New(t)

	if err := os.Setenv("TEST", "set-from-env"); err != nil {
		t.Fatalf("set env failed: %v", err)
	}
	defer func() {
		if err := os.Unsetenv("TEST"); err != nil {
			t.Fatalf("unset env failed: %v", err)
		}
	}()

	cfg := &Configuration{}

	tests := []struct {
		Name  string
		Input string
		Value any
	}{
		{
			Name:  "basic",
			Input: `hello = "world"`,
			Value: &struct {
				Hello string `config:"hello,optional"`
			}{},
		},
		{
			Name:  "Duration",
			Input: `timeout = "5s"`,
			Value: &struct {
				Timeout time.Duration `config:"timeout,optional"`
			}{},
		},
		{
			Name:  "Stdlib",
			Input: `hello = lower("HELLO")`,
			Value: &struct {
				Hello string `config:"hello,optional"`
			}{},
		},
		{
			Name:  "env",
			Input: `hello = env("USER", "username")`,
			Value: &struct {
				Hello string `config:"hello,optional"`
			}{},
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			file, diags := hclsyntax.ParseConfig([]byte(test.Input), "test.hcl", hcl.Pos{Line: 1, Column: 1})
			is.True(!diags.HasErrors()) // parsing the fixture should succeed
			if diags.HasErrors() {
				is.Fail() // stop here so we do not continue with invalid input
			}

			diags = confighcl.DecodeBody(file.Body, cfg.EvalContext(context.Background()), test.Value)
			is.True(!diags.HasErrors()) // decoding the fixture should succeed
			if diags.HasErrors() {
				is.Fail() // stop here so we do not continue with invalid decoded state
			}

			t.Logf("%+v", test.Value)
		})
	}
}
