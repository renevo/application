package application

import (
	"errors"
	"log/slog"
	"os"
	"syscall"
	"time"
)

// Option configures an Application during New. Returning an error aborts
// construction; nil options are ignored.
type Option func(app *Application) error

// WithModule registers a module under an exact, nonempty name. Registration
// rejects nil modules, duplicate names, and changes after preparation freezes
// the registry. Sentinel failures remain inspectable with errors.Is.
func WithModule(name string, m Module) Option {
	return func(a *Application) error {
		return a.modules.add(name, m)
	}
}

// WithConfigFile selects the HCL or JSON file loaded during application
// preparation. The path must be nonempty; reading and extension validation are
// deferred until Validate, Install, or Run.
func WithConfigFile(filename string) Option {
	return func(a *Application) error {
		if filename == "" {
			return errors.New("configuration filename must not be empty")
		}
		a.configuration = &Configuration{}
		a.configFile = filename
		return nil
	}
}

// WithEnvPrefix namespaces automatic environment setting names. Prefixes are
// uppercased and surrounding underscores are removed, so "myapp" and
// "MYAPP_" both map Http.Address to MYAPP_HTTP_ADDRESS. An empty prefix keeps
// names unprefixed. Nonempty prefixes must be portable ASCII identifiers.
func WithEnvPrefix(prefix string) Option {
	return func(a *Application) error {
		normalized, err := normalizeEnvironmentPrefix(prefix)
		if err != nil {
			return err
		}
		a.envPrefix = normalized
		return nil
	}
}

// WithLogger replaces the default logger. New derives an application-scoped
// logger from it after all options have been applied. A nil logger is rejected.
func WithLogger(logger *slog.Logger) Option {
	return func(a *Application) error {
		if logger == nil {
			return errors.New("logger must not be nil")
		}
		a.logger = logger
		return nil
	}
}

// WithShutdownTimeout sets the single deadline shared by the complete
// PreStop, Stop, and PostStop sequence. The timeout must be positive.
func WithShutdownTimeout(timeout time.Duration) Option {
	return func(a *Application) error {
		if timeout <= 0 {
			return errors.New("shutdown timeout must be positive")
		}
		a.shutdownTimeout = timeout
		return nil
	}
}

// RunOption configures one call to Application.Run. Returning an error aborts
// the call before application preparation; nil options are ignored.
type RunOption func(*runOptions) error

type runOptions struct {
	install bool
	signals []os.Signal
}

// WithInstall runs Installer hooks before startup. Installation stops at the
// first error and does not trigger module teardown because Start was not entered.
func WithInstall() RunOption {
	return func(options *runOptions) error {
		options.install = true
		return nil
	}
}

// WithSignals enables process signal handling for a Run call. With no explicit
// signals, it handles SIGTERM, SIGABRT, SIGQUIT, SIGINT, SIGHUP, and signal 21.
// Nil signals are rejected. SIGHUP always reloads scalar configuration; all
// other signals request graceful shutdown.
func WithSignals(signals ...os.Signal) RunOption {
	return func(options *runOptions) error {
		if len(signals) == 0 {
			signals = []os.Signal{
				syscall.SIGTERM,
				syscall.SIGABRT,
				syscall.SIGQUIT,
				syscall.SIGINT,
				syscall.SIGHUP,
				syscall.Signal(21),
			}
		}
		for _, signal := range signals {
			if signal == nil {
				return errors.New("signal must not be nil")
			}
		}
		options.signals = append([]os.Signal(nil), signals...)
		return nil
	}
}
