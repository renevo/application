// Command simple demonstrates a two-module application loaded from HCL.
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/renevo/application"
	httpmodule "github.com/renevo/application/examples/simple/modules/http"
	"github.com/renevo/application/examples/simple/modules/poller"
)

func main() {
	configFile := "application.hcl"
	if len(os.Args) > 1 {
		configFile = os.Args[1]
	}

	app, err := application.New(
		"simple-example",
		"1.0.0",
		application.WithConfigFile(configFile),
		application.WithModule("poller", poller.New()),
		application.WithModule("http", httpmodule.New()),
	)
	if err != nil {
		slog.Error("failed to create application", "error", err)
		os.Exit(1)
	}

	if err := app.Run(context.Background(), application.WithSignals()); err != nil {
		slog.Error("application failed", "error", err)
		os.Exit(1)
	}
}
