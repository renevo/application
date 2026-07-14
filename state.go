package application

// State identifies the current phase or terminal outcome of an Application.
// Values are observational snapshots; callers do not drive transitions directly.
type State uint8

const (
	// StateNew indicates that preparation has not started and modules may still
	// be registered through construction options.
	StateNew State = iota
	// StateInitializing indicates that module Initializer hooks are running.
	StateInitializing
	// StateConfiguring indicates that registered settings and bindings are being loaded.
	StateConfiguring
	// StateReady indicates successful preparation and readiness for Run or Install.
	StateReady
	// StateInstalling indicates that optional Installer hooks are running.
	StateInstalling
	// StateStarting covers the PreStart, Start, and PostStart phases.
	StateStarting
	// StateRunning indicates that startup completed and the application is waiting for shutdown.
	StateRunning
	// StateStopping covers the reverse PreStop, Stop, and PostStop phases.
	StateStopping
	// StateStopped is the terminal outcome of a successful workflow.
	StateStopped
	// StateFailed is the terminal outcome of a failed workflow.
	StateFailed
)

// String returns the stable lowercase name of s, or "unknown" for an
// unrecognized value.
func (s State) String() string {
	switch s {
	case StateNew:
		return "new"
	case StateInitializing:
		return "initializing"
	case StateConfiguring:
		return "configuring"
	case StateReady:
		return "ready"
	case StateInstalling:
		return "installing"
	case StateStarting:
		return "starting"
	case StateRunning:
		return "running"
	case StateStopping:
		return "stopping"
	case StateStopped:
		return "stopped"
	case StateFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// moduleState describes the last lifecycle phase entered by a module.
type moduleState uint8

const (
	moduleStateRegistered moduleState = iota
	moduleStateInitializing
	moduleStateInitialized
	moduleStateInstalling
	moduleStateInstalled
	moduleStatePreStarting
	moduleStateStarting
	moduleStateStarted
	moduleStatePostStarting
	moduleStateRunning
	moduleStatePreStopping
	moduleStateStopping
	moduleStatePostStopping
	moduleStateStopped
)
