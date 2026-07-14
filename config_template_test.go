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
	is.NoErr(err) // application construction should accept a missing configured file

	var output bytes.Buffer
	is.NoErr(application.WriteConfigTemplate(context.Background(), &output)) // template generation should use registered defaults without loading sources
	is.Equal(application.State(), StateStopped)                              // successful generation should leave the terminal application stopped
	is.True(!module.started)                                                 // template generation should not start modules

	source := output.String()
	is.True(strings.Contains(source, "enabled = true"))                      // booleans should use native HCL literals
	is.True(strings.Contains(source, "count = 10"))                          // integers should use native HCL literals and ignore environment overrides
	is.True(strings.Contains(source, "ratio = 1.5"))                         // floating-point defaults should use native HCL literals
	is.True(strings.Contains(source, "name = \"worker\""))                   // strings should remain quoted HCL strings
	is.True(strings.Contains(source, "timeout = \"5s\""))                    // durations should use readable quoted strings
	is.True(strings.Contains(source, "# Route prefix\nprefix = \"/api\""))   // structured descriptions should precede active defaults
	is.True(strings.Contains(source, "# HTTP route\n# route \"example\" {")) // empty repeated blocks should produce one commented example

	_, diags := hclsyntax.ParseConfig(output.Bytes(), "generated.hcl", hcl.Pos{Line: 1, Column: 1})
	is.True(!diags.HasErrors()) // generated active content should parse as native HCL

	generatedFile := filepath.Join(t.TempDir(), "generated.hcl")
	is.NoErr(os.WriteFile(generatedFile, output.Bytes(), 0o600)) // round-trip fixture should be writable
	is.NoErr(os.Unsetenv("WORKER_COUNT"))                        // round-trip validation should isolate file values from environment precedence
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
	is.NoErr(err)                                      // fresh application construction should accept generated HCL
	is.NoErr(roundTrip.Validate(context.Background())) // generated active defaults should decode successfully
	is.Equal(roundTripModule.enabled, true)            // boolean default should survive the HCL round trip
	is.Equal(roundTripModule.count, 10)                // integer default should survive the HCL round trip
	is.Equal(roundTripModule.ratio, 1.5)               // float default should survive the HCL round trip
	is.Equal(roundTripModule.name, "worker")           // string default should survive the HCL round trip
	is.Equal(roundTripModule.timeout, 5*time.Second)   // duration default should survive the HCL round trip
	is.Equal(roundTripModule.config.Prefix, "/api")    // structured attribute default should survive the HCL round trip

	err = application.Run(context.Background())
	is.True(errors.Is(err, ErrInvalidState)) // successful template generation should make later Run invalid

	repeatedApplication, err := New(
		"test", "1.0.0",
		WithConfigFile(missingConfig),
		WithModule("worker", newTemplateTestModule()),
	)
	is.NoErr(err) // equivalent fresh application construction should succeed
	var repeatedOutput bytes.Buffer
	is.NoErr(repeatedApplication.WriteConfigTemplate(context.Background(), &repeatedOutput)) // repeated generation should succeed
	is.Equal(repeatedOutput.Bytes(), output.Bytes())                                         // equivalent applications should produce deterministic bytes
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
			is.NoErr(err) // supported scalar default should convert to a cty value
			body := hclwrite.NewEmptyFile().Body()
			body.SetAttributeValue("value", value)
			source := string(hclwrite.Format(body.BuildTokens(nil).Bytes()))
			is.True(strings.Contains(source, "value = "+test.want)) // generated token type and value should match the scalar contract
		})
	}
}

func TestWriteConfigTemplateFailures(t *testing.T) {
	t.Run("nil writer", func(t *testing.T) {
		application, err := New("test", "1.0.0")
		is := is.New(t)
		is.NoErr(err)                                                              // application construction should succeed
		is.True(application.WriteConfigTemplate(context.Background(), nil) != nil) // nil writers should be rejected
		is.Equal(application.State(), StateNew)                                    // precondition failure should preserve the new state
	})

	t.Run("writer error", func(t *testing.T) {
		application, err := New("test", "1.0.0", WithModule("worker", newTemplateTestModule()))
		is := is.New(t)
		is.NoErr(err) // application construction should succeed
		err = application.WriteConfigTemplate(context.Background(), errorWriter{})
		is.True(errors.Is(err, errTemplateWrite))  // writer errors should remain inspectable
		is.Equal(application.State(), StateFailed) // write failure after initialization should fail the application
	})

	t.Run("configuration collision", func(t *testing.T) {
		module := newTemplateTestModule()
		application, err := New("test", "1.0.0", WithModule("worker", module))
		is := is.New(t)
		is.NoErr(err) // application construction should succeed
		application.settings.Setting("prefix", new(string), "Conflicting prefix")

		var output bytes.Buffer
		err = application.WriteConfigTemplate(context.Background(), &output)
		is.True(err != nil)                        // scalar and structured ownership collision should fail generation
		is.Equal(output.Len(), 0)                  // pre-write rendering failure should not produce partial output
		is.Equal(application.State(), StateFailed) // rendering failure after initialization should fail the application
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
	is.NoErr(err) // application construction should accept both binding modules

	var output bytes.Buffer
	err = application.WriteConfigTemplate(context.Background(), &output)
	is.True(err != nil)       // a binding after a root map consumer should be rejected
	is.Equal(output.Len(), 0) // binding ownership failure should not produce partial output
}
