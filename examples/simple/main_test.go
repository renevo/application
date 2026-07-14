package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/matryer/is"

	"github.com/renevo/application"
	httpmodule "github.com/renevo/application/examples/simple/modules/http"
	"github.com/renevo/application/examples/simple/modules/poller"
)

func TestWriteConfigTemplate(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "application.hcl")
	newApplication := func() *application.Application {
		app, err := application.New(
			"simple-example", "1.0.0",
			application.WithConfigFile(filename),
			application.WithModule("poller", poller.New()),
			application.WithModule("http", httpmodule.New()),
		)
		if err != nil {
			t.Fatal(err)
		}
		return app
	}

	is := is.New(t)
	is.NoErr(writeConfigTemplate(newApplication(), filename)) // generation should create a new selected configuration file
	source, err := os.ReadFile(filename)
	is.NoErr(err)            // generated configuration file should be readable
	is.True(len(source) > 0) // successful generation should write HCL content

	err = writeConfigTemplate(newApplication(), filename)
	is.True(errors.Is(err, os.ErrExist)) // generation should refuse to overwrite an existing file
}

type failingInitializeModule struct{}

func (*failingInitializeModule) Initialize(*application.Context) error {
	return errors.New("initialize failed")
}
func (*failingInitializeModule) Start(*application.Context) error { return nil }
func (*failingInitializeModule) Stop(*application.Context) error  { return nil }

func TestWriteConfigTemplateRemovesFailedFile(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "application.hcl")
	app, err := application.New("test", "1.0.0", application.WithModule("failed", new(failingInitializeModule)))
	is := is.New(t)
	is.NoErr(err) // application construction should accept the failing initializer module

	err = writeConfigTemplate(app, filename)
	is.True(err != nil) // initialization failure should propagate from generation
	_, err = os.Stat(filename)
	is.True(errors.Is(err, os.ErrNotExist)) // failed generation should remove the exclusively created file
}
