package application

// Module is the required lifecycle contract for an application component.
// Start runs in registration order. Stop runs in reverse order for every module
// whose Start method was entered, including a module whose Start returned an
// error. Implementations should honor cancellation and deadlines on ctx.
type Module interface {
	// Start begins the module's runtime work.
	Start(ctx *Context) error
	// Stop releases resources acquired after Start was entered.
	Stop(ctx *Context) error
}

// Initializer prepares a module before configuration is loaded. Initialize runs
// once in registration order and is the phase in which modules register
// settings and structured configuration bindings.
type Initializer interface {
	// Initialize prepares the module and registers its configuration surface.
	Initialize(ctx *Context) error
}

// PreStarter runs in registration order before any Module.Start method. The
// first error aborts startup.
type PreStarter interface {
	// PreStart performs work that must finish before any module starts.
	PreStart(ctx *Context) error
}

// PostStarter runs in registration order after every Module.Start method has
// succeeded. An error triggers teardown of all started modules.
type PostStarter interface {
	// PostStart performs work after all modules have started successfully.
	PostStart(ctx *Context) error
}

// PreStopper runs in reverse registration order before any Module.Stop method.
// Errors are accumulated and do not prevent remaining teardown hooks.
type PreStopper interface {
	// PreStop performs teardown work before any module stops.
	PreStop(ctx *Context) error
}

// PostStopper runs in reverse registration order after every Module.Stop
// method. Errors are accumulated with earlier teardown failures.
type PostStopper interface {
	// PostStop performs teardown work after all modules have stopped.
	PostStop(ctx *Context) error
}

// Installer performs optional setup before startup. Install methods run in
// registration order through Application.Install or Run with WithInstall. The
// first error stops installation; the application does not perform rollback.
type Installer interface {
	// Install performs optional setup that precedes module startup.
	Install(ctx *Context) error
}
