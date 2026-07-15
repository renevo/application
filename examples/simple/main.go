// Command simple demonstrates a two-module application loaded from HCL.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"

	"github.com/renevo/application"
	httpmodule "github.com/renevo/application/examples/simple/modules/http"
	"github.com/renevo/application/examples/simple/modules/poller"
)

func main() {
	configFile := flag.String("config", "application.hcl", "configuration file path")
	generateConfig := flag.Bool("generate-config", false, "generate the configuration file and exit")
	flag.Parse()

	app, err := application.New(
		"simple-example",
		"1.0.0",
		application.WithConfigSources(
			application.ConfigFileSource(*configFile),
			application.EnvironmentSource(""),
		),
		application.WithModule("poller", poller.New()),
		application.WithModule("http", httpmodule.New()),
	)
	if err != nil {
		slog.Error("failed to create application", "error", err)
		os.Exit(1)
	}

	if *generateConfig {
		if err := writeConfigTemplate(app, *configFile); err != nil {
			slog.Error("failed to generate configuration", "error", err)
			os.Exit(1)
		}
		return
	}

	if err := app.Run(context.Background(), application.WithSignals()); err != nil {
		slog.Error("application failed", "error", err)
		os.Exit(1)
	}
}

func writeConfigTemplate(app *application.Application, filename string) (err error) {
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, file.Close())
		if err != nil {
			err = errors.Join(err, os.Remove(filename))
		}
	}()
	return app.WriteConfigTemplate(context.Background(), file)
}
