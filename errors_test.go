package application

import (
	"errors"
	"testing"

	"github.com/matryer/is"
)

func TestPhaseError(t *testing.T) {
	tests := []struct {
		name   string
		module string
		phase  string
	}{
		{name: "start failure", module: "worker", phase: "start"},
		{name: "teardown failure", module: "server", phase: "post-stop"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			is := is.New(t)
			cause := errors.New("hook failed")
			err := phaseError(test.module, test.phase, cause)
			is.True(errors.Is(err, cause)) // phase error should unwrap its hook cause

			var phase *PhaseError
			is.True(errors.As(err, &phase))     // phase error should be inspectable with errors.As
			is.Equal(phase.Module, test.module) // phase error should retain the registered module name
			is.Equal(phase.Phase, test.phase)   // phase error should retain the lifecycle phase
			is.Equal(phase.Err, cause)          // phase error should retain the original hook error
		})
	}
}
