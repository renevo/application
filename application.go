package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"log/slog"
	"os"
	"os/signal"
	"reflect"
	"sync"
	"syscall"
	"time"

	"github.com/renevo/config"
)

// Application coordinates configuration and the ordered lifecycle of a fixed
// set of modules. An Application is single-use: Validate may prepare it for a
// later Run, while Install and Run are terminal workflows.
//
// Application methods serialize lifecycle operations. A concurrent operation
// returns ErrApplicationBusy instead of waiting for the active operation.
type Application struct {
	name     string
	version  string
	settings *config.Set
	logger   *slog.Logger

	modules moduleRegistry

	operationMu sync.Mutex
	stateMu     sync.RWMutex
	state       State
	runCancel   context.CancelCauseFunc
	shutdown    sync.Once

	configBindings  []configBinding
	configSources   []config.Source
	shutdownTimeout time.Duration
}

type configBinding struct {
	module string
	target reflect.Value
}

type shutdownRequest struct {
	cause error
}

func (request *shutdownRequest) Error() string {
	if request.cause == nil {
		return context.Canceled.Error()
	}
	return request.cause.Error()
}

func (request *shutdownRequest) Unwrap() error {
	if request.cause == nil {
		return context.Canceled
	}
	return request.cause
}

// Run constructs an Application and runs it with a background context. It is a
// convenience for callers that do not need RunOption values such as
// WithSignals or WithInstall.
func Run(name, version string, opts ...Option) error {
	application, err := New(name, version, opts...)
	if err != nil {
		return err
	}
	return application.Run(context.Background())
}

// New constructs an Application and applies opts in order. Empty names and
// versions default to "application" and "0.0.0". Nil options are ignored.
//
// The application starts with a new settings set, slog.Default, and a
// 30-second shutdown timeout. Option failures are returned with construction
// context, and the final logger is scoped with the application name.
func New(name, version string, opts ...Option) (*Application, error) {
	if name == "" {
		name = "application"
	}
	if version == "" {
		version = "0.0.0"
	}
	app := &Application{
		name:            name,
		version:         version,
		settings:        config.NewSet(),
		logger:          slog.Default(),
		state:           StateNew,
		shutdownTimeout: 30 * time.Second,
	}
	app.configSources = []config.Source{EnvironmentSource("")}

	for _, opt := range opts {
		if opt != nil {
			if err := opt(app); err != nil {
				return nil, fmt.Errorf("configure application: %w", err)
			}
		}
	}

	// add the application name to all logs on this logger
	app.logger = app.logger.With("app", name)

	return app, nil
}

// Name returns the application name established during construction.
func (a *Application) Name() string { return a.name }

// Version returns the application version established during construction.
func (a *Application) Version() string { return a.version }

// Settings returns the application's shared configuration set. Modules should
// register settings during Initialize, before configuration is loaded.
func (a *Application) Settings() *config.Set { return a.settings }

// Logger returns the application-scoped logger. Module lifecycle contexts
// derive a child logger that also includes the module name.
func (a *Application) Logger() *slog.Logger { return a.logger }

// String returns the application identity in name/version form.
func (a *Application) String() string { return fmt.Sprintf("%s/%s", a.name, a.version) }

// State returns a concurrency-safe snapshot of the current lifecycle state.
// The application may transition again immediately after State returns.
func (a *Application) State() State {
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()
	return a.state
}

func (a *Application) setState(state State) {
	a.stateMu.Lock()
	a.state = state
	a.stateMu.Unlock()
}

// Module returns the module registered under name. Names are matched exactly.
func (a *Application) Module(name string) (Module, bool) { return a.modules.get(name) }

// Modules returns an insertion-ordered snapshot of registered modules. The
// sequence is unaffected by later lifecycle state changes and may be stopped
// early by the iterator consumer.
func (a *Application) Modules() iter.Seq2[string, Module] {
	modules := a.modules.snapshot()
	return func(yield func(string, Module) bool) {
		for _, module := range modules {
			if !yield(module.name, module.implementation) {
				return
			}
		}
	}
}

// Validate initializes all modules and loads the initial configuration without
// installing or starting modules. A successful call leaves the application in
// StateReady and may be followed by another Validate or one Run or Install.
//
// A nil context is treated as context.Background. Concurrent lifecycle work
// returns ErrApplicationBusy.
func (a *Application) Validate(ctx context.Context) error {
	if !a.operationMu.TryLock() {
		return ErrApplicationBusy
	}
	defer a.operationMu.Unlock()
	_, err := a.prepare(ctx)
	return err
}

// WriteConfigTemplate initializes modules and writes a native HCL starter
// configuration containing their registered defaults. It does not read the
// configured file or environment sources. The operation is terminal for the
// Application once initialization begins, leaving it stopped on success or
// failed on initialization, rendering, or writing errors. Precondition errors
// such as a nil writer or invalid state do not change the application state.
//
// A nil context is treated as context.Background. Concurrent lifecycle work
// returns ErrApplicationBusy.
func (a *Application) WriteConfigTemplate(ctx context.Context, writer io.Writer) error {
	if !a.operationMu.TryLock() {
		return ErrApplicationBusy
	}
	defer a.operationMu.Unlock()

	if isNil(writer) {
		return errors.New("configuration template writer must not be nil")
	}
	if a.State() != StateNew {
		return fmt.Errorf("%w: cannot write configuration template from %s", ErrInvalidState, a.State())
	}
	if ctx == nil {
		ctx = context.Background()
	}

	modules := a.modules.freeze()
	if err := a.initializeModules(ctx, modules); err != nil {
		return err
	}
	a.setState(StateConfiguring)

	source, err := a.configTemplate()
	if err != nil {
		a.setState(StateFailed)
		return err
	}
	written, err := writer.Write(source)
	if err == nil && written != len(source) {
		err = io.ErrShortWrite
	}
	if err != nil {
		a.setState(StateFailed)
		return fmt.Errorf("write configuration template: %w", err)
	}

	a.setState(StateStopped)
	return nil
}

// Install prepares the application, then invokes each Installer in registration
// order. It stops at the first error and does not perform rollback or module
// teardown because Start has not been entered.
//
// Install is terminal for this Application, ending in StateStopped on success
// or StateFailed on failure.
func (a *Application) Install(ctx context.Context) error {
	if !a.operationMu.TryLock() {
		return ErrApplicationBusy
	}
	defer a.operationMu.Unlock()

	modules, err := a.prepare(ctx)
	if err != nil {
		return err
	}
	a.setState(StateInstalling)
	err = a.install(ctx, modules)
	if err != nil {
		a.setState(StateFailed)
	} else {
		a.setState(StateStopped)
	}
	return err
}

// Run prepares and starts the application, waits for cancellation, and tears
// down started modules. Startup phases run serially in registration order;
// PreStop, Stop, and PostStop each run serially in reverse order.
//
// A module becomes eligible for teardown immediately before its Start method is
// called, so a module whose Start fails is cleaned up while later modules are
// not. Teardown continues after hook failures, and startup, cancellation, and
// teardown errors are combined with errors.Join.
//
// Run is single-use and terminal. A nil parent is treated as
// context.Background. Run options are applied before preparation begins.
func (a *Application) Run(parent context.Context, runOpts ...RunOption) error {
	if !a.operationMu.TryLock() {
		return ErrApplicationBusy
	}
	defer a.operationMu.Unlock()

	options := new(runOptions)
	for _, option := range runOpts {
		if option != nil {
			if err := option(options); err != nil {
				return fmt.Errorf("configure run: %w", err)
			}
		}
	}
	if parent == nil {
		parent = context.Background()
	}

	modules, err := a.prepare(parent)
	if err != nil {
		return err
	}
	if a.State() != StateReady {
		return fmt.Errorf("%w: cannot run from %s", ErrInvalidState, a.State())
	}

	runParent := parent
	stopSignals := func() {}
	var reloadSignals chan os.Signal
	terminationSignals, reloadOnSIGHUP := partitionSignals(options.signals)
	if len(options.signals) != 0 {
		if len(terminationSignals) != 0 {
			runParent, stopSignals = signal.NotifyContext(parent, terminationSignals...)
		}
		if reloadOnSIGHUP {
			reloadSignals = make(chan os.Signal, 1)
			signal.Notify(reloadSignals, syscall.SIGHUP)
		}
	}
	defer func() {
		stopSignals()
		if reloadSignals != nil {
			signal.Stop(reloadSignals)
		}
	}()

	runContext, cancel := context.WithCancelCause(runParent)
	a.stateMu.Lock()
	a.runCancel = cancel
	a.stateMu.Unlock()
	defer func() {
		a.stateMu.Lock()
		a.runCancel = nil
		a.stateMu.Unlock()
	}()

	startupTime := time.Now()
	a.logger.Info("Starting application")
	defer func() { a.logger.Info("Stopped application", "duration", time.Since(startupTime)) }()

	if options.install {
		a.setState(StateInstalling)
		if err := a.install(runContext, modules); err != nil {
			a.setState(StateFailed)
			return err
		}
	}

	a.setState(StateStarting)
	startErr := a.start(runContext, modules)
	if startErr == nil {
		a.setState(StateRunning)
		for runContext.Err() == nil {
			select {
			case <-runContext.Done():
			case <-reloadSignals:
				if err := a.Reload(runContext); err != nil {
					a.logger.Error("Configuration reload failed", "error", err)
				}
			}
		}
	}

	a.setState(StateStopping)
	stopErr := a.stop(runContext, modules)

	cause := context.Cause(runContext)
	terminatedBySignal := len(terminationSignals) != 0 && parent.Err() == nil && runParent.Err() != nil && errors.Is(cause, context.Cause(runParent))
	if request, requested := cause.(*shutdownRequest); requested {
		cause = request.cause
	} else if terminatedBySignal || (parent.Err() != nil && errors.Is(cause, context.Canceled)) {
		cause = nil
	}
	result := errors.Join(startErr, cause, stopErr)
	if result != nil {
		a.setState(StateFailed)
	} else {
		a.setState(StateStopped)
	}
	return result
}

func partitionSignals(signals []os.Signal) (termination []os.Signal, reload bool) {
	for _, processSignal := range signals {
		if processSignal == syscall.SIGHUP {
			reload = true
			continue
		}
		termination = append(termination, processSignal)
	}
	return termination, reload
}

// Shutdown requests termination of a running application without waiting for
// teardown. It is safe for concurrent use; the first request determines the
// cancellation cause and subsequent requests have no effect.
//
// A nil cause requests normal termination. Shutdown returns ErrNotRunning when
// Run has not installed its cancellation function or has already returned.
func (a *Application) Shutdown(cause error) error {
	a.stateMu.RLock()
	cancel := a.runCancel
	a.stateMu.RUnlock()
	if cancel == nil {
		return ErrNotRunning
	}
	a.shutdown.Do(func() { cancel(&shutdownRequest{cause: cause}) })
	return nil
}

// Exit is an alias for Shutdown.
func (a *Application) Exit(cause error) error { return a.Shutdown(cause) }

// Reload atomically reloads registered settings from all configured scalar
// sources in precedence order.
// Structured targets registered with Context.BindConfig are startup-only and
// are not modified. A failed reload preserves the last committed settings.
//
// Reload requires StateRunning and returns ErrInvalidState otherwise.
func (a *Application) Reload(ctx context.Context) error {
	if a.State() != StateRunning {
		return fmt.Errorf("%w: cannot reload from %s", ErrInvalidState, a.State())
	}
	return a.loadConfigSources(newContext(ctx, a, "").Context, true)
}

func (a *Application) prepare(parent context.Context) ([]*registeredModule, error) {
	state := a.State()
	if state == StateReady {
		return a.modules.snapshot(), nil
	}
	if state != StateNew {
		return nil, fmt.Errorf("%w: cannot prepare from %s", ErrInvalidState, state)
	}

	modules := a.modules.freeze()
	if err := a.initializeModules(parent, modules); err != nil {
		return nil, err
	}

	a.setState(StateConfiguring)
	if err := a.loadConfig(newContext(parent, a, "").Context); err != nil {
		a.setState(StateFailed)
		return nil, err
	}
	a.setState(StateReady)
	return modules, nil
}

func (a *Application) initializeModules(parent context.Context, modules []*registeredModule) error {
	a.setState(StateInitializing)
	for _, module := range modules {
		module.state = moduleStateInitializing
		if initializer, ok := module.implementation.(Initializer); ok {
			if err := initializer.Initialize(newContext(parent, a, module.name)); err != nil {
				a.setState(StateFailed)
				return phaseError(module.name, "initialize", err)
			}
		}
		module.state = moduleStateInitialized
	}
	return nil
}

func (a *Application) install(parent context.Context, modules []*registeredModule) error {
	for _, module := range modules {
		installer, ok := module.implementation.(Installer)
		if !ok {
			continue
		}
		module.state = moduleStateInstalling
		if err := installer.Install(newContext(parent, a, module.name)); err != nil {
			return phaseError(module.name, "install", err)
		}
		module.state = moduleStateInstalled
	}
	return nil
}

func (a *Application) start(parent context.Context, modules []*registeredModule) error {
	for _, module := range modules {
		if hook, ok := module.implementation.(PreStarter); ok {
			module.state = moduleStatePreStarting
			if err := hook.PreStart(newContext(parent, a, module.name)); err != nil {
				return phaseError(module.name, "pre-start", err)
			}
		}
	}
	for _, module := range modules {
		module.state = moduleStateStarting
		module.startWasEntered = true
		if err := module.implementation.Start(newContext(parent, a, module.name)); err != nil {
			return phaseError(module.name, "start", err)
		}
		module.state = moduleStateStarted
	}
	for _, module := range modules {
		if hook, ok := module.implementation.(PostStarter); ok {
			module.state = moduleStatePostStarting
			if err := hook.PostStart(newContext(parent, a, module.name)); err != nil {
				return phaseError(module.name, "post-start", err)
			}
		}
		module.state = moduleStateRunning
	}
	return nil
}

func (a *Application) stop(runContext context.Context, modules []*registeredModule) error {
	shutdownContext, cancel := context.WithTimeout(context.WithoutCancel(runContext), a.shutdownTimeout)
	defer cancel()
	var result error

	for index := len(modules) - 1; index >= 0; index-- {
		module := modules[index]
		if module.startWasEntered {
			if hook, ok := module.implementation.(PreStopper); ok {
				module.state = moduleStatePreStopping
				result = errors.Join(result, wrapPhaseError(module.name, "pre-stop", hook.PreStop(newContext(shutdownContext, a, module.name))))
			}
		}
	}
	for index := len(modules) - 1; index >= 0; index-- {
		module := modules[index]
		if module.startWasEntered {
			module.state = moduleStateStopping
			result = errors.Join(result, wrapPhaseError(module.name, "stop", module.implementation.Stop(newContext(shutdownContext, a, module.name))))
		}
	}
	for index := len(modules) - 1; index >= 0; index-- {
		module := modules[index]
		if module.startWasEntered {
			if hook, ok := module.implementation.(PostStopper); ok {
				module.state = moduleStatePostStopping
				result = errors.Join(result, wrapPhaseError(module.name, "post-stop", hook.PostStop(newContext(shutdownContext, a, module.name))))
			}
			module.state = moduleStateStopped
		}
	}
	return errors.Join(result, shutdownContext.Err())
}

func phaseError(module, phase string, err error) error {
	return &PhaseError{Module: module, Phase: phase, Err: err}
}

func wrapPhaseError(module, phase string, err error) error {
	if err == nil {
		return nil
	}
	return phaseError(module, phase, err)
}

func (a *Application) loadConfig(ctx context.Context) error {
	if a.settings.Loaded() {
		return nil
	}
	return a.loadConfigSources(ctx, false)
}

func (a *Application) loadConfigSources(ctx context.Context, reload bool) error {
	transaction := new(configLoadTransaction)
	ctx = context.WithValue(ctx, configLoadContextKey, transaction)
	var err error
	if reload {
		err = a.settings.Reload(ctx, a.configSources...)
	} else {
		err = a.settings.Load(ctx, a.configSources...)
	}
	if err != nil {
		return err
	}
	if !reload {
		for _, binding := range transaction.bindings {
			binding.target.Elem().Set(binding.value)
		}
	}
	return nil
}
