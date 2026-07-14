package application

import (
	"context"
	"log/slog"
	"reflect"

	"github.com/renevo/config"
)

type contextKey string

var applicationContextKey = contextKey("application")

// Context is the framework-created context passed to module lifecycle hooks. It
// embeds the hook's cancellation and deadline context and exposes application
// capabilities scoped to the current module.
type Context struct {
	// Context carries cancellation, deadlines, and values for the current hook.
	context.Context
	application *Application
	module      string
}

func newContext(parent context.Context, application *Application, module string) *Context {
	if parent == nil {
		parent = context.Background()
	}
	parent = context.WithValue(parent, applicationContextKey, application)
	return &Context{Context: parent, application: application, module: module}
}

// Application returns the application invoking the lifecycle hook.
func (c *Context) Application() *Application { return c.application }

// Settings returns the application's shared configuration set. Settings should
// be registered during Initialize so they are included in the initial load.
func (c *Context) Settings() *config.Set { return c.application.settings }

// State returns a snapshot of the application's current lifecycle state.
func (c *Context) State() State { return c.application.State() }

// Shutdown requests application termination and preserves the first cause. It
// has the same concurrency and state semantics as Application.Shutdown.
func (c *Context) Shutdown(cause error) error { return c.application.Shutdown(cause) }

// Cause returns the cancellation cause of the embedded lifecycle context.
func (c *Context) Cause() error {
	cause := context.Cause(c.Context)
	if request, requested := cause.(*shutdownRequest); requested {
		if request.cause == nil {
			return context.Canceled
		}
		return request.cause
	}
	return cause
}

// BindConfig registers target for structured HCL decoding during initial
// configuration. It is valid only from a module's Initialize method and target
// must be a non-nil pointer to a struct or map.
//
// Targets are assigned after the initial configuration has decoded and settings
// have committed successfully. Runtime reload does not modify bound targets.
func (c *Context) BindConfig(target any) error {
	if c.module == "" || c.application.State() != StateInitializing {
		return ErrConfigBindingPhase
	}
	value := reflect.ValueOf(target)
	if value.Kind() != reflect.Pointer || value.IsNil() || (value.Elem().Kind() != reflect.Struct && value.Elem().Kind() != reflect.Map) {
		return ErrInvalidConfigTarget
	}
	c.application.configBindings = append(c.application.configBindings, configBinding{module: c.module, target: value})
	return nil
}

// Logger returns the application logger scoped with the current module name.
// Contexts not associated with a module return the application logger directly.
func (c *Context) Logger() *slog.Logger {
	if c.module == "" {
		return c.application.logger
	}
	return c.application.logger.With("module", c.module)
}

// FromContext returns the Application carried by a framework-derived context.
// It returns nil when ctx is nil or does not contain an Application.
func FromContext(ctx context.Context) *Application {
	if ctx == nil {
		return nil
	}
	app := ctx.Value(applicationContextKey)
	if app == nil {
		return nil
	}

	result, _ := app.(*Application)
	return result
}
