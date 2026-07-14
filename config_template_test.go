package application

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/matryer/is"
)

type templateTestModule struct {
	enabled bool
	count   int
	ratio   float64
	name    string
	timeout time.Duration
	config  struct {
		Prefix string `config:"prefix,optional" description:"Route prefix"`
		Routes []struct {
			Name   string `config:"name,label"`
			Target string `config:"target" description:"Route destination"`
		} `config:"route,block" description:"HTTP route"`
	}
	started bool
}

func newTemplateTestModule() *templateTestModule {
	module := &templateTestModule{
		enabled: true,
		count:   10,
		ratio:   1.5,
		name:    "worker",
		timeout: 5 * time.Second,
	}
	module.config.Prefix = "/api"
	return module
}

func (module *templateTestModule) Initialize(ctx *Context) error {
	settings := ctx.Settings().Subset("worker")
	settings.Setting("enabled", &module.enabled, "Enable worker")
	settings.Setting("count", &module.count, "Worker count")
	settings.Setting("ratio", &module.ratio, "Worker ratio")
	settings.Setting("name", &module.name, "Worker name")
	settings.Setting("timeout", &module.timeout, "Worker timeout")
	return ctx.BindConfig(&module.config)
}

func (module *templateTestModule) Start(*Context) error { module.started = true; return nil }
func (*templateTestModule) Stop(*Context) error         { return nil }

func TestWriteConfigTemplate(t *testing.T) {
	t.Setenv("WORKER_COUNT", "99")
	missingConfig := filepath.Join(t.TempDir(), "missing.hcl")
	module := newTemplateTestModule()
	application, err := New(
		"test", "1.0.0",
		WithConfigFile(missingConfig),
		WithModule("worker", module),
	)
	is := is.New(t)
	is.NoErr(err)

	var output bytes.Buffer
	is.NoErr(application.WriteConfigTemplate(context.Background(), &output))
	is.Equal(application.State(), StateStopped)
	is.True(!module.started)

	source := output.String()
	is.True(strings.Contains(source, "enabled = true"))
	is.True(strings.Contains(source, "count = 10"))
	is.True(strings.Contains(source, "ratio = 1.5"))
	is.True(strings.Contains(source, "name = \"worker\""))
	is.True(strings.Contains(source, "timeout = \"5s\""))
	is.True(strings.Contains(source, "# Route prefix\nprefix = \"/api\""))
	is.True(strings.Contains(source, "# HTTP route\n# route \"example\" {"))

	_, diags := hclsyntax.ParseConfig(output.Bytes(), "generated.hcl", hcl.Pos{Line: 1, Column: 1})
	is.True(!diags.HasErrors())

	generatedFile := filepath.Join(t.TempDir(), "generated.hcl")
	is.NoErr(os.WriteFile(generatedFile, output.Bytes(), 0o600))
	is.NoErr(os.Unsetenv("WORKER_COUNT"))
	roundTripModule := newTemplateTestModule()
	roundTripModule.enabled = false
	roundTripModule.count = 0
	roundTripModule.ratio = 0
	roundTripModule.name = ""
	roundTripModule.timeout = 0
	roundTripModule.config.Prefix = ""
	roundTrip, err := New(
		"test", "1.0.0",
		WithConfigFile(generatedFile),
		WithModule("worker", roundTripModule),
	)
	is.NoErr(err)
	is.NoErr(roundTrip.Validate(context.Background()))
	is.Equal(roundTripModule.enabled, true)
	is.Equal(roundTripModule.count, 10)
	is.Equal(roundTripModule.ratio, 1.5)
	is.Equal(roundTripModule.name, "worker")
	is.Equal(roundTripModule.timeout, 5*time.Second)
	is.Equal(roundTripModule.config.Prefix, "/api")

	err = application.Run(context.Background())
	is.True(errors.Is(err, ErrInvalidState))

	repeatedApplication, err := New(
		"test", "1.0.0",
		WithConfigFile(missingConfig),
		WithModule("worker", newTemplateTestModule()),
	)
	is.NoErr(err)
	var repeatedOutput bytes.Buffer
	is.NoErr(repeatedApplication.WriteConfigTemplate(context.Background(), &repeatedOutput))
	is.Equal(repeatedOutput.Bytes(), output.Bytes())
}

func TestScalarTemplateValue(t *testing.T) {
	tests := []struct {
		name      string
		valueType string
		value     string
		want      string
	}{
		{name: "bool", valueType: "bool", value: "true", want: "true"},
		{name: "int", valueType: "int", value: "-1", want: "-1"},
		{name: "int8", valueType: "int8", value: "-8", want: "-8"},
		{name: "int16", valueType: "int16", value: "-16", want: "-16"},
		{name: "int32", valueType: "int32", value: "-32", want: "-32"},
		{name: "int64", valueType: "int64", value: "-64", want: "-64"},
		{name: "uint", valueType: "uint", value: "1", want: "1"},
		{name: "uint8", valueType: "uint8", value: "8", want: "8"},
		{name: "uint16", valueType: "uint16", value: "16", want: "16"},
		{name: "uint32", valueType: "uint32", value: "32", want: "32"},
		{name: "uint64", valueType: "uint64", value: "18446744073709551615", want: "18446744073709551615"},
		{name: "uintptr", valueType: "uintptr", value: "64", want: "64"},
		{name: "float32", valueType: "float32", value: "1.25", want: "1.25"},
		{name: "float64", valueType: "float64", value: "2.5", want: "2.5"},
		{name: "string", valueType: "string", value: "42", want: `"42"`},
		{name: "duration", valueType: "time.Duration", value: "5s", want: `"5s"`},
		{name: "named int fallback", valueType: "application.namedInt", value: "42", want: `"42"`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value, err := scalarTemplateValue(templateSetting{path: "test", value: test.value, valueType: test.valueType})
			is := is.New(t)
			is.NoErr(err)
			body := hclwrite.NewEmptyFile().Body()
			body.SetAttributeValue("value", value)
			source := string(hclwrite.Format(body.BuildTokens(nil).Bytes()))
			is.True(strings.Contains(source, "value = "+test.want))
		})
	}
}

func TestWriteConfigTemplateFailures(t *testing.T) {
	t.Run("nil writer", func(t *testing.T) {
		application, err := New("test", "1.0.0")
		is := is.New(t)
		is.NoErr(err)
		is.True(application.WriteConfigTemplate(context.Background(), nil) != nil)
		is.Equal(application.State(), StateNew)
	})

	t.Run("writer error", func(t *testing.T) {
		application, err := New("test", "1.0.0", WithModule("worker", newTemplateTestModule()))
		is := is.New(t)
		is.NoErr(err)
		err = application.WriteConfigTemplate(context.Background(), errorWriter{})
		is.True(errors.Is(err, errTemplateWrite))
		is.Equal(application.State(), StateFailed)
	})

	t.Run("configuration collision", func(t *testing.T) {
		module := newTemplateTestModule()
		application, err := New("test", "1.0.0", WithModule("worker", module))
		is := is.New(t)
		is.NoErr(err)
		application.settings.Setting("prefix", new(string), "Conflicting prefix")

		var output bytes.Buffer
		err = application.WriteConfigTemplate(context.Background(), &output)
		is.True(err != nil)
		is.Equal(output.Len(), 0)
		is.Equal(application.State(), StateFailed)
	})
}

var errTemplateWrite = errors.New("template write failed")

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, errTemplateWrite }

var _ io.Writer = errorWriter{}

type templateBindingModule struct {
	target any
}

func (module *templateBindingModule) Initialize(ctx *Context) error {
	return ctx.BindConfig(module.target)
}
func (*templateBindingModule) Start(*Context) error { return nil }
func (*templateBindingModule) Stop(*Context) error  { return nil }

func TestWriteConfigTemplateRejectsBindingAfterRootMap(t *testing.T) {
	rootMap := map[string]string{"first": "value"}
	structured := struct {
		Second string `config:"second"`
	}{Second: "value"}
	application, err := New(
		"test", "1.0.0",
		WithModule("map", &templateBindingModule{target: &rootMap}),
		WithModule("struct", &templateBindingModule{target: &structured}),
	)
	is := is.New(t)
	is.NoErr(err)

	var output bytes.Buffer
	err = application.WriteConfigTemplate(context.Background(), &output)
	is.True(err != nil)
	is.Equal(output.Len(), 0)
}
