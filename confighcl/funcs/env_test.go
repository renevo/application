package funcs

import (
	"testing"

	"github.com/matryer/is"
	"github.com/zclconf/go-cty/cty"
)

func TestEnv(t *testing.T) {
	t.Setenv("APPLICATION_ENV_TEST_SET", "from-env")
	t.Setenv("APPLICATION_ENV_TEST_UNSET", "")

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
	is.True(err != nil)
}
