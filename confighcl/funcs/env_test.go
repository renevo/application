package funcs

import (
	"os"
	"testing"

	"github.com/matryer/is"
	"github.com/zclconf/go-cty/cty"
)

func TestEnv(t *testing.T) {
	t.Setenv("APPLICATION_ENV_TEST_SET", "from-env")
	t.Setenv("APPLICATION_ENV_TEST_UNSET", "restore-after-test")
	if err := os.Unsetenv("APPLICATION_ENV_TEST_UNSET"); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		args []cty.Value
		want cty.Value
	}{
		{
			name: "set without default",
			args: []cty.Value{cty.StringVal("APPLICATION_ENV_TEST_SET")},
			want: cty.StringVal("from-env"),
		},
		{
			name: "unset without default",
			args: []cty.Value{cty.StringVal("APPLICATION_ENV_TEST_UNSET")},
			want: cty.StringVal(""),
		},
		{
			name: "set with default",
			args: []cty.Value{cty.StringVal("APPLICATION_ENV_TEST_SET"), cty.StringVal("fallback")},
			want: cty.StringVal("from-env"),
		},
		{
			name: "unset with default",
			args: []cty.Value{cty.StringVal("APPLICATION_ENV_TEST_UNSET"), cty.StringVal("fallback")},
			want: cty.StringVal("fallback"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			is := is.New(t)
			got, err := MakeEnvFunc().Call(test.args)
			is.NoErr(err)
			is.Equal(got, test.want)
		})
	}
}

func TestEnvRejectsTooManyArguments(t *testing.T) {
	is := is.New(t)
	_, err := MakeEnvFunc().Call([]cty.Value{
		cty.StringVal("APPLICATION_ENV_TEST"),
		cty.StringVal("fallback"),
		cty.StringVal("extra"),
	})
	if err == nil {
		t.Fatal("expected an arity error")
	}
	is.Equal(err.Error(), "env expects env(name) or env(name, default), got 3 arguments")
}
