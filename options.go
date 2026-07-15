package application

import (
	"errors"
	"log/slog"
	"os"
	"slices"
	"syscall"
	"time"

	"github.com/renevo/config"
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

// WithConfigSources replaces the ordered scalar configuration sources. Later
// sources override earlier sources. Calling WithConfigSources with no sources
// disables external configuration and loads registered defaults only.
func WithConfigSources(sources ...config.Source) Option {
	return func(a *Application) error {
		a.configSources = slices.Clone(sources)
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
