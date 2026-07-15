package application

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/matryer/is"

	"github.com/renevo/application/confighcl"
)

func TestHCL(t *testing.T) {
	t.Setenv("TEST", "set-from-env")

	cfg := &Configuration{}

	tests := []struct {
		name   string
		input  string
		target any
		want   any
	}{
		{
			name:  "basic",
			input: `hello = "world"`,
			target: &struct {
				Hello string `config:"hello,optional"`
			}{},
			want: &struct {
				Hello string `config:"hello,optional"`
			}{Hello: "world"},
		},
		{
			name:  "duration",
			input: `timeout = "5s"`,
			target: &struct {
				Timeout time.Duration `config:"timeout,optional"`
			}{},
			want: &struct {
				Timeout time.Duration `config:"timeout,optional"`
			}{Timeout: 5 * time.Second},
		},
		{
			name:  "stdlib",
			input: `hello = lower("HELLO")`,
			target: &struct {
				Hello string `config:"hello,optional"`
			}{},
			want: &struct {
				Hello string `config:"hello,optional"`
			}{Hello: "hello"},
		},
		{
			name:  "env",
			input: `hello = env("TEST", "username")`,
			target: &struct {
				Hello string `config:"hello,optional"`
			}{},
			want: &struct {
				Hello string `config:"hello,optional"`
			}{Hello: "set-from-env"},
		},
		{
			name:  "env without default",
			input: `hello = env("TEST")`,
			target: &struct {
				Hello string `config:"hello,optional"`
			}{},
			want: &struct {
				Hello string `config:"hello,optional"`
			}{Hello: "set-from-env"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			is := is.New(t)
			file, diags := hclsyntax.ParseConfig([]byte(test.input), "test.hcl", hcl.Pos{Line: 1, Column: 1})
			is.True(!diags.HasErrors()) // fixture should parse as valid native HCL

			diags = confighcl.DecodeBody(file.Body, cfg.EvalContext(context.Background()), test.target)
			is.True(!diags.HasErrors())      // fixture should decode into its target type
			is.Equal(test.target, test.want) // decoded value should match the case expectation
		})
	}
}

type settingsHCLModule struct {
	readTimeout time.Duration
	started     chan struct{}
}

func (m *settingsHCLModule) Initialize(ctx *Context) error {
	http := ctx.Settings().Subset("http")
	http.Setting("read_timeout", &m.readTimeout, "HTTP read timeout")
	return nil
}

func (m *settingsHCLModule) Start(*Context) error {
	if m.started != nil {
		close(m.started)
	}
	return nil
}
func (*settingsHCLModule) Stop(*Context) error { return nil }

type routeConfig struct {
	Prefix string `config:"prefix,optional"`
	Routes []struct {
		Name    string   `config:"name,label"`
		Target  string   `config:"target"`
		Methods []string `config:"methods,optional"`
	} `config:"route,block"`
}

type customHCLModule struct {
	config routeConfig
}

func (m *customHCLModule) Initialize(ctx *Context) error { return ctx.BindConfig(&m.config) }
func (*customHCLModule) Start(*Context) error            { return nil }
func (*customHCLModule) Stop(*Context) error             { return nil }

type bindingModule struct {
	target any
}

func (m *bindingModule) Initialize(ctx *Context) error { return ctx.BindConfig(m.target) }
func (*bindingModule) Start(*Context) error            { return nil }
func (*bindingModule) Stop(*Context) error             { return nil }

type rollbackConfig struct {
	Routes []struct {
		Name   string `config:"name,label"`
		Target string `config:"target"`
	} `config:"route,block"`
	Backend *struct {
		Name     string `config:"name"`
		Endpoint string `config:"endpoint"`
	} `config:"backend,block"`
}

func writeTestConfig(is *is.I, filename, source string) {
	is.Helper()
	is.NoErr(os.WriteFile(filename, []byte(source), 0o600)) // configuration fixture should be writable
}

func TestApplicationConfiguration(t *testing.T) {
	tests := []struct {
		name string
		run  func(*testing.T, *is.I)
	}{
		{
			name: "settings source",
			run: func(t *testing.T, is *is.I) {
				filename := filepath.Join(t.TempDir(), "application.hcl")
				writeTestConfig(is, filename, `http { read_timeout = "15s" }`)
				module := new(settingsHCLModule)
				application, err := New("test", "1.0.0", withTestConfigFile(filename), WithModule("http", module))
				is.NoErr(err)                                        // application construction should accept the configuration file
				is.NoErr(application.Validate(context.Background())) // validation should load registered settings
				is.Equal(module.readTimeout, 15*time.Second)         // HCL value should pass through the duration codec
				is.True(application.Settings().Loaded())             // successful validation should commit the settings set
			},
		},
		{
			name: "missing file diagnostics",
			run: func(t *testing.T, is *is.I) {
				filename := filepath.Join(t.TempDir(), "missing.hcl")
				application, err := New("test", "1.0.0", withTestConfigFile(filename), WithModule("http", new(settingsHCLModule)))
				is.NoErr(err)
				err = application.Validate(context.Background())
				is.True(err != nil)
				is.True(strings.Contains(err.Error(), "Configuration file not found"))
				is.True(strings.Contains(err.Error(), filename))
			},
		},
		{
			name: "environment overrides file",
			run: func(t *testing.T, is *is.I) {
				t.Setenv("MYAPP_HTTP_READ_TIMEOUT", "25s")
				filename := filepath.Join(t.TempDir(), "application.hcl")
				writeTestConfig(is, filename, `http { read_timeout = "15s" }`)
				module := new(settingsHCLModule)
				application, err := New(
					"test",
					"1.0.0",
					WithConfigSources(ConfigFileSource(filename), EnvironmentSource("myapp_")),
					WithModule("http", module),
				)
				is.NoErr(err)                                        // application construction should normalize the environment prefix
				is.NoErr(application.Validate(context.Background())) // validation should compose file and environment sources
				is.Equal(module.readTimeout, 25*time.Second)         // environment should override the file value through the duration codec
			},
		},
		{
			name: "file overrides environment when ordered last",
			run: func(t *testing.T, is *is.I) {
				t.Setenv("HTTP_READ_TIMEOUT", "25s")
				filename := filepath.Join(t.TempDir(), "application.hcl")
				writeTestConfig(is, filename, `http { read_timeout = "15s" }`)
				module := new(settingsHCLModule)
				application, err := New(
					"test",
					"1.0.0",
					WithConfigSources(EnvironmentSource(""), ConfigFileSource(filename)),
					WithModule("http", module),
				)
				is.NoErr(err)
				is.NoErr(application.Validate(context.Background()))
				is.Equal(module.readTimeout, 15*time.Second)
			},
		},
		{
			name: "environment-only reload",
			run: func(t *testing.T, is *is.I) {
				t.Setenv("HTTP_READ_TIMEOUT", "15s")
				started := make(chan struct{})
				module := &settingsHCLModule{readTimeout: 5 * time.Second, started: started}
				application, err := New("test", "1.0.0", WithModule("http", module))
				is.NoErr(err) // application construction should accept environment-only configuration
				runDone := make(chan error, 1)
				go func() { runDone <- application.Run(context.Background()) }()
				<-started
				waitForApplicationState(t, application, StateRunning)
				is.Equal(module.readTimeout, 15*time.Second) // initial preparation should load the environment value

				is.NoErr(os.Setenv("HTTP_READ_TIMEOUT", "invalid"))
				is.True(application.Reload(context.Background()) != nil) // invalid environment text should fail reload
				is.Equal(module.readTimeout, 15*time.Second)             // failed reload should preserve the committed setting

				is.NoErr(os.Setenv("HTTP_READ_TIMEOUT", "30s"))
				is.NoErr(application.Reload(context.Background())) // environment-only reload should not require a configuration file
				is.Equal(module.readTimeout, 30*time.Second)       // valid environment text should commit through the duration codec

				is.NoErr(os.Unsetenv("HTTP_READ_TIMEOUT"))
				is.NoErr(application.Reload(context.Background())) // removing the variable should still produce a valid reload
				is.Equal(module.readTimeout, 5*time.Second)        // omitted environment values should restore the registered default
				is.NoErr(application.Shutdown(nil))
				is.NoErr(<-runDone)
			},
		},
		{
			name: "settings reload",
			run: func(t *testing.T, is *is.I) {
				filename := filepath.Join(t.TempDir(), "application.hcl")
				writeTestConfig(is, filename, `http { read_timeout = "15s" }`)
				started := make(chan struct{})
				module := &settingsHCLModule{started: started}
				application, err := New("test", "1.0.0", withTestConfigFile(filename), WithModule("http", module))
				is.NoErr(err) // application construction should accept reloadable settings
				runDone := make(chan error, 1)
				go func() { runDone <- application.Run(context.Background()) }()
				<-started
				waitForApplicationState(t, application, StateRunning)

				writeTestConfig(is, filename, `http { read_timeout = "invalid" }`)
				is.True(application.Reload(context.Background()) != nil) // invalid reload should report a decoding error
				is.Equal(module.readTimeout, 15*time.Second)             // failed reload should preserve the committed value

				writeTestConfig(is, filename, `http { read_timeout = "30s" }`)
				is.NoErr(application.Reload(context.Background())) // valid reload should commit atomically
				is.Equal(module.readTimeout, 30*time.Second)       // successful reload should replace the setting value
				is.NoErr(application.Shutdown(nil))                // running application should accept normal shutdown
				is.NoErr(<-runDone)                                // normal shutdown after reload should return no error
			},
		},
		{
			name: "structured binding remains startup only",
			run: func(t *testing.T, is *is.I) {
				filename := filepath.Join(t.TempDir(), "application.hcl")
				writeTestConfig(is, filename, `
http { read_timeout = "15s" }
route "health" { target = "/healthz" }
`)
				started := make(chan struct{})
				settingsModule := &settingsHCLModule{started: started}
				routerModule := new(customHCLModule)
				application, err := New(
					"test",
					"1.0.0",
					withTestConfigFile(filename),
					WithModule("http", settingsModule),
					WithModule("router", routerModule),
				)
				is.NoErr(err) // application construction should accept scalar and structured configuration
				runDone := make(chan error, 1)
				go func() { runDone <- application.Run(context.Background()) }()
				<-started
				waitForApplicationState(t, application, StateRunning)
				is.Equal(settingsModule.readTimeout, 15*time.Second)
				is.Equal(routerModule.config.Routes[0].Target, "/healthz")

				writeTestConfig(is, filename, `
http { read_timeout = "30s" }
route "health" { target = "/changed" }
`)
				is.NoErr(application.Reload(context.Background()))         // valid reload should re-evaluate every scalar source
				is.Equal(settingsModule.readTimeout, 30*time.Second)       // scalar settings should receive the reloaded file value
				is.Equal(routerModule.config.Routes[0].Target, "/healthz") // structured targets should retain their startup value
				is.NoErr(application.Shutdown(nil))
				is.NoErr(<-runDone)
			},
		},
		{
			name: "custom HCL binding",
			run: func(t *testing.T, is *is.I) {
				filename := filepath.Join(t.TempDir(), "application.hcl")
				source := `
route "health" {
  target = "/healthz"
  methods = ["GET", "HEAD"]
}
route "metrics" {
  target = "/metrics"
}
`
				writeTestConfig(is, filename, source)
				module := &customHCLModule{config: routeConfig{Prefix: "/api"}}
				application, err := New("test", "1.0.0", withTestConfigFile(filename), WithModule("router", module))
				is.NoErr(err)                                                      // application construction should accept a structured binding
				is.NoErr(application.Validate(context.Background()))               // validation should decode all route blocks
				is.Equal(len(module.config.Routes), 2)                             // repeated route blocks should populate the route slice
				is.Equal(module.config.Prefix, "/api")                             // omitted optional value should preserve the target default
				is.Equal(module.config.Routes[0].Name, "health")                   // block label should populate the route name
				is.Equal(module.config.Routes[0].Target, "/healthz")               // route target should decode from its attribute
				is.Equal(module.config.Routes[0].Methods, []string{"GET", "HEAD"}) // optional method list should decode completely
			},
		},
		{
			name: "map binding before struct binding",
			run: func(t *testing.T, is *is.I) {
				filename := filepath.Join(t.TempDir(), "application.hcl")
				writeTestConfig(is, filename, `message = "hello"`)
				mapTarget := map[string]string{}
				structTarget := struct {
					Optional string `config:"optional,optional"`
				}{}
				application, err := New(
					"test",
					"1.0.0",
					withTestConfigFile(filename),
					WithModule("map", &bindingModule{target: &mapTarget}),
					WithModule("struct", &bindingModule{target: &structTarget}),
				)
				is.NoErr(err)                                              // application construction should accept multiple structured bindings
				is.NoErr(application.Validate(context.Background()))       // a consumed map body should leave an empty body for later bindings
				is.Equal(mapTarget, map[string]string{"message": "hello"}) // map binding should receive all remaining attributes
				is.Equal(structTarget.Optional, "")                        // later struct binding should decode from the empty leftover body
			},
		},
		{
			name: "failed structured binding rollback",
			run: func(t *testing.T, is *is.I) {
				filename := filepath.Join(t.TempDir(), "application.hcl")
				writeTestConfig(is, filename, `
route "changed" {}
backend {
  name = "changed"
}
`)
				target := rollbackConfig{
					Routes: []struct {
						Name   string `config:"name,label"`
						Target string `config:"target"`
					}{{Name: "original", Target: "/original"}},
					Backend: &struct {
						Name     string `config:"name"`
						Endpoint string `config:"endpoint"`
					}{Name: "original", Endpoint: "/original"},
				}
				application, err := New(
					"test",
					"1.0.0",
					withTestConfigFile(filename),
					WithModule("rollback", &bindingModule{target: &target}),
				)
				is.NoErr(err)                                              // application construction should accept a pre-populated structured binding
				is.True(application.Validate(context.Background()) != nil) // missing required fields should fail structured decoding
				is.Equal(target.Routes[0].Name, "original")                // failed decoding should not mutate an existing slice element
				is.Equal(target.Routes[0].Target, "/original")             // failed decoding should preserve existing slice element fields
				is.Equal(target.Backend.Name, "original")                  // failed decoding should not mutate an existing pointer target
				is.Equal(target.Backend.Endpoint, "/original")             // failed decoding should preserve existing pointer target fields
			},
		},
		{
			name: "overlapping slice bounds",
			run: func(t *testing.T, is *is.I) {
				filename := filepath.Join(t.TempDir(), "application.hcl")
				writeTestConfig(is, filename, "")
				backing := []string{"first", "second"}
				target := struct {
					Short []string `config:"short,optional"`
					Long  []string `config:"long,optional"`
				}{
					Short: backing[:1],
					Long:  backing[:2],
				}
				application, err := New(
					"test",
					"1.0.0",
					withTestConfigFile(filename),
					WithModule("slices", &bindingModule{target: &target}),
				)
				is.NoErr(err)                                        // application construction should accept overlapping slice views
				is.NoErr(application.Validate(context.Background())) // empty configuration should preserve optional binding defaults
				is.Equal(target.Short, []string{"first"})            // shorter slice view should retain its original bounds
				is.Equal(target.Long, []string{"first", "second"})   // longer slice view should not reuse the shorter clone
				is.Equal(cap(target.Short), 2)                       // shorter slice view should retain its original capacity
				is.Equal(cap(target.Long), 2)                        // longer slice view should retain its original capacity
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.run(t, is.New(t))
		})
	}
}

func withTestConfigFile(filename string) Option {
	return WithConfigSources(ConfigFileSource(filename), EnvironmentSource(""))
}

func waitForApplicationState(t *testing.T, application *Application, want State) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for application.State() != want {
		if time.Now().After(deadline) {
			t.Fatalf("application state = %s, want %s", application.State(), want)
		}
		runtime.Gosched()
	}
}
