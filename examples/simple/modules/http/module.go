// Package httpmodule demonstrates a module with settings and structured HCL.
package httpmodule

import (
	"time"

	"github.com/renevo/application"
)

type route struct {
	Name    string   `config:"name,label"`
	Target  string   `config:"target"`
	Methods []string `config:"methods,optional"`
}

type config struct {
	Prefix string  `config:"prefix,optional"`
	Routes []route `config:"route,block"`
}

// Module demonstrates a component that combines scalar application settings
// with startup-only structured HCL route blocks.
type Module struct {
	address     string
	readTimeout time.Duration
	config      config
}

// New returns an HTTP example module with local address, timeout, and path
// prefix defaults that may be replaced by application configuration.
func New() *Module {
	return &Module{
		address:     "127.0.0.1:8080",
		readTimeout: 5 * time.Second,
		config:      config{Prefix: "/api"},
	}
}

// Initialize registers the http settings and structured route configuration.
func (module *Module) Initialize(ctx *application.Context) error {
	settings := ctx.Settings().Subset("http")
	settings.Setting("address", &module.address, "HTTP listen address")
	settings.Setting("read_timeout", &module.readTimeout, "HTTP read timeout")
	return ctx.BindConfig(&module.config)
}

// Start logs the configured listener and routes. The example does not open a
// network listener.
func (module *Module) Start(ctx *application.Context) error {
	ctx.Logger().Info(
		"HTTP module started",
		"address", module.address,
		"read_timeout", module.readTimeout,
		"prefix", module.config.Prefix,
	)
	for _, route := range module.config.Routes {
		ctx.Logger().Info(
			"route configured",
			"name", route.Name,
			"target", route.Target,
			"methods", route.Methods,
		)
	}
	return nil
}

// Stop logs completion; the example module owns no external resources.
func (*Module) Stop(ctx *application.Context) error {
	ctx.Logger().Info("HTTP module stopped")
	return nil
}
