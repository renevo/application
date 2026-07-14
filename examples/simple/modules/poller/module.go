// Package poller demonstrates a module configured only through application settings.
package poller

import (
	"time"

	"github.com/renevo/application"
)

// Module demonstrates a settings-only background polling component.
type Module struct {
	enabled   bool
	interval  time.Duration
	batchSize int
}

// New returns a poller with defaults that may be replaced by application settings.
func New() *Module {
	return &Module{
		enabled:   true,
		interval:  30 * time.Second,
		batchSize: 10,
	}
}

// Initialize registers the poller settings used by Start.
func (module *Module) Initialize(ctx *application.Context) error {
	settings := ctx.Settings().Subset("poller")
	settings.Setting("enabled", &module.enabled, "Enable background polling")
	settings.Setting("interval", &module.interval, "Delay between polling attempts")
	settings.Setting("batch_size", &module.batchSize, "Maximum records per poll")
	return nil
}

// Start logs the effective poller settings. The example starts no goroutines.
func (module *Module) Start(ctx *application.Context) error {
	ctx.Logger().Info(
		"poller started",
		"enabled", module.enabled,
		"interval", module.interval,
		"batch_size", module.batchSize,
	)
	return nil
}

// Stop logs completion; the example module owns no external resources.
func (*Module) Stop(ctx *application.Context) error {
	ctx.Logger().Info("poller stopped")
	return nil
}
