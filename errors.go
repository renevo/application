package application

import (
	"errors"
	"fmt"
)

var (
	// ErrApplicationBusy indicates that another lifecycle operation is active.
	ErrApplicationBusy = errors.New("application operation already in progress")
	// ErrConfigBindingPhase indicates that BindConfig was called outside Initialize.
	ErrConfigBindingPhase = errors.New("configuration bindings may only be registered during module initialization")
	// ErrDuplicateModule indicates that a module name is already registered.
	ErrDuplicateModule = errors.New("module already registered")
	// ErrInvalidConfigTarget indicates that a binding target is not a non-nil struct or map pointer.
	ErrInvalidConfigTarget = errors.New("configuration target must be a non-nil pointer to a struct or map")
	// ErrInvalidObjectPath indicates that a dotted configuration variable path is malformed.
	ErrInvalidObjectPath = errors.New("invalid object path")
	// ErrInvalidState indicates that an operation is not legal in the current lifecycle state.
	ErrInvalidState = errors.New("invalid application state")
	// ErrModulesFrozen indicates that registration was attempted after preparation began.
	ErrModulesFrozen = errors.New("module registration is frozen")
	// ErrNotRunning indicates that shutdown was requested while Run was inactive.
	ErrNotRunning = errors.New("application is not running")
)

// PhaseError identifies a module lifecycle hook failure. It may appear inside
// an errors.Join result and can be located with errors.As. Err is the original
// hook error.
type PhaseError struct {
	// Module is the registered name of the module whose hook failed.
	Module string
	// Phase identifies the lifecycle hook, such as "start" or "post-stop".
	Phase string
	// Err is the error returned by the module hook.
	Err error
}

// Error formats the module, lifecycle phase, and underlying error.
func (e *PhaseError) Error() string {
	return fmt.Sprintf("module %q %s: %v", e.Module, e.Phase, e.Err)
}

// Unwrap returns the lifecycle hook error for errors.Is and errors.As.
func (e *PhaseError) Unwrap() error { return e.Err }
