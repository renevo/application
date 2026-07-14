package application

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"syscall"
	"testing"

	"github.com/matryer/is"
)

type lifecycleRecorder struct {
	name     string
	events   *[]string
	mu       *sync.Mutex
	started  chan<- struct{}
	startErr error
}

func (m *lifecycleRecorder) record(phase string) {
	m.mu.Lock()
	*m.events = append(*m.events, phase+":"+m.name)
	m.mu.Unlock()
}

func (m *lifecycleRecorder) Initialize(*Context) error { m.record("initialize"); return nil }
func (m *lifecycleRecorder) PreStart(*Context) error   { m.record("pre-start"); return nil }
func (m *lifecycleRecorder) PostStart(*Context) error  { m.record("post-start"); return nil }
func (m *lifecycleRecorder) PreStop(*Context) error    { m.record("pre-stop"); return nil }
func (m *lifecycleRecorder) Stop(*Context) error       { m.record("stop"); return nil }
func (m *lifecycleRecorder) PostStop(*Context) error   { m.record("post-stop"); return nil }

func (m *lifecycleRecorder) Start(*Context) error {
	m.record("start")
	if m.started != nil {
		close(m.started)
	}
	return m.startErr
}

func TestApplication(t *testing.T) {
	tests := []struct {
		name string
		run  func(*testing.T, *is.I)
	}{
		{
			name: "lifecycle order",
			run: func(t *testing.T, is *is.I) {
				var events []string
				var eventMu sync.Mutex
				started := make(chan struct{})
				first := &lifecycleRecorder{name: "first", events: &events, mu: &eventMu}
				second := &lifecycleRecorder{name: "second", events: &events, mu: &eventMu, started: started}

				application, err := New("test", "1.0.0", WithModule("first", first), WithModule("second", second))
				is.NoErr(err)                                        // application construction should succeed
				is.NoErr(application.Validate(context.Background())) // validation should prepare the application

				runDone := make(chan error, 1)
				go func() { runDone <- application.Run(context.Background()) }()
				<-started
				is.NoErr(application.Shutdown(nil)) // explicit shutdown should be accepted while running
				is.NoErr(<-runDone)                 // normal shutdown should return no run error

				want := []string{
					"initialize:first", "initialize:second",
					"pre-start:first", "pre-start:second",
					"start:first", "start:second",
					"post-start:first", "post-start:second",
					"pre-stop:second", "pre-stop:first",
					"stop:second", "stop:first",
					"post-stop:second", "post-stop:first",
				}
				is.Equal(events, want)                      // lifecycle hooks should use forward startup and reverse teardown order
				is.Equal(application.State(), StateStopped) // clean shutdown should leave the application stopped
			},
		},
		{
			name: "failed start cleanup",
			run: func(t *testing.T, is *is.I) {
				startFailure := errors.New("start failed")
				var events []string
				var eventMu sync.Mutex
				first := &lifecycleRecorder{name: "first", events: &events, mu: &eventMu}
				second := &lifecycleRecorder{name: "second", events: &events, mu: &eventMu, startErr: startFailure}
				third := &lifecycleRecorder{name: "third", events: &events, mu: &eventMu}

				application, err := New(
					"test", "1.0.0",
					WithModule("first", first),
					WithModule("second", second),
					WithModule("third", third),
				)
				is.NoErr(err) // application construction should succeed

				err = application.Run(context.Background())
				is.True(errors.Is(err, startFailure))      // run should preserve the failing Start cause
				is.Equal(application.State(), StateFailed) // startup failure should leave the application failed

				want := []string{
					"initialize:first", "initialize:second", "initialize:third",
					"pre-start:first", "pre-start:second", "pre-start:third",
					"start:first", "start:second",
					"pre-stop:second", "pre-stop:first",
					"stop:second", "stop:first",
					"post-stop:second", "post-stop:first",
				}
				is.Equal(events, want) // teardown should include the failing module and exclude unentered modules
			},
		},
		{
			name: "opt-in signal shutdown",
			run: func(t *testing.T, is *is.I) {
				if runtime.GOOS == "windows" {
					t.Skip("Windows cannot deliver termination signals with os.Process.Signal")
				}
				var events []string
				var eventMu sync.Mutex
				started := make(chan struct{})
				module := &lifecycleRecorder{name: "worker", events: &events, mu: &eventMu, started: started}
				application, err := New("test", "1.0.0", WithModule("worker", module))
				is.NoErr(err) // application construction should succeed

				runDone := make(chan error, 1)
				go func() { runDone <- application.Run(context.Background(), WithSignals(syscall.SIGTERM)) }()
				<-started
				process, err := os.FindProcess(os.Getpid())
				is.NoErr(err)                               // current process should be addressable for signal delivery
				is.NoErr(process.Signal(syscall.SIGTERM))   // registered SIGTERM should reach the application
				is.NoErr(<-runDone)                         // terminating signals should be normal shutdown
				is.Equal(application.State(), StateStopped) // signal shutdown should leave the application stopped
			},
		},
		{
			name: "default signals",
			run: func(_ *testing.T, is *is.I) {
				want := []os.Signal{
					syscall.SIGTERM,
					syscall.SIGABRT,
					syscall.SIGQUIT,
					syscall.SIGINT,
					syscall.SIGHUP,
					syscall.Signal(21),
				}
				options := new(runOptions)
				is.NoErr(WithSignals()(options)) // zero-argument signal option should apply successfully
				is.Equal(options.signals, want)  // zero-argument signal option should preserve the complete legacy termination set
			},
		},
		{
			name: "signal roles",
			run: func(_ *testing.T, is *is.I) {
				termination, reload := partitionSignals([]os.Signal{syscall.SIGHUP})
				is.Equal(termination, []os.Signal(nil)) // SIGHUP alone should not register a termination signal
				is.True(reload)                         // SIGHUP alone should enable configuration reload

				termination, reload = partitionSignals([]os.Signal{syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT})
				is.Equal(termination, []os.Signal{syscall.SIGTERM, syscall.SIGQUIT}) // mixed signals should preserve non-SIGHUP termination order
				is.True(reload)                                                      // mixed signals should reserve SIGHUP for configuration reload
			},
		},
		{
			name: "concurrent shutdown",
			run: func(t *testing.T, is *is.I) {
				var events []string
				var eventMu sync.Mutex
				started := make(chan struct{})
				module := &lifecycleRecorder{name: "worker", events: &events, mu: &eventMu, started: started}
				application, err := New("test", "1.0.0", WithModule("worker", module))
				is.NoErr(err) // application construction should succeed

				runDone := make(chan error, 1)
				go func() { runDone <- application.Run(context.Background()) }()
				<-started

				const callers = 32
				startShutdown := make(chan struct{})
				var callersDone sync.WaitGroup
				callersDone.Add(callers)
				for index := range callers {
					go func() {
						defer callersDone.Done()
						<-startShutdown
						_ = application.Shutdown(fmt.Errorf("shutdown %d", index))
					}()
				}
				close(startShutdown)
				callersDone.Wait()
				is.True((<-runDone) != nil) // one concurrent shutdown cause should be preserved

				eventMu.Lock()
				defer eventMu.Unlock()
				stopCount := 0
				for _, event := range events {
					if event == "stop:worker" {
						stopCount++
					}
				}
				is.Equal(stopCount, 1) // concurrent shutdown requests should execute teardown once
			},
		},
		{
			name: "explicit wrapped cancellation shutdown",
			run: func(t *testing.T, is *is.I) {
				var events []string
				var eventMu sync.Mutex
				started := make(chan struct{})
				module := &lifecycleRecorder{name: "worker", events: &events, mu: &eventMu, started: started}
				application, err := New("test", "1.0.0", WithModule("worker", module))
				is.NoErr(err) // application construction should succeed

				runDone := make(chan error, 1)
				go func() { runDone <- application.Run(context.Background()) }()
				<-started
				shutdownFailure := fmt.Errorf("shutdown failed: %w", context.Canceled)
				is.NoErr(application.Shutdown(shutdownFailure)) // explicit failure should be accepted while running
				err = <-runDone
				is.True(errors.Is(err, shutdownFailure))   // run should preserve an explicit cause that wraps context cancellation
				is.Equal(application.State(), StateFailed) // explicit shutdown failure should leave the application failed
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.run(t, is.New(t))
		})
	}
}
