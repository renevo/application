# application

[![Go Reference](https://pkg.go.dev/badge/github.com/renevo/application.svg)](https://pkg.go.dev/github.com/renevo/application)
[![Test](https://github.com/renevo/application/actions/workflows/test.yml/badge.svg)](https://github.com/renevo/application/actions/workflows/test.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

`application` is a Go package for composing services from ordered lifecycle
modules. It provides deterministic startup and shutdown, transactional HCL
configuration, explicit cancellation, and structured lifecycle errors without
taking ownership of behavior that belongs in individual modules.

## Features

- Ordered, single-use application lifecycle with explicit state.
- Required `Start` and `Stop` hooks plus optional initialize, install, pre/post
  start, and pre/post stop phases.
- Reverse-order teardown for every module whose `Start` hook was entered.
- One application-wide graceful shutdown deadline.
- Hierarchical settings backed by [`github.com/renevo/config`](https://pkg.go.dev/github.com/renevo/config).
- Native HCL and JSON configuration with transactional validation and commit.
- Automatic environment configuration with optional application prefixes.
- Typed structured HCL bindings for repeated, labeled, and nested blocks.
- Atomic runtime reload across registered scalar configuration sources.
- Opt-in interrupt and termination signal handling.
- Structured errors compatible with `errors.Is` and `errors.As`.

## Requirements

- Go 1.26 or later.

## Installation

```sh
go get github.com/renevo/application
```

## Quick Start

A module implements `Start` and `Stop`. It can optionally implement lifecycle
interfaces such as `Initializer` to register settings before configuration is
loaded.

```go
package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/renevo/application"
)

type worker struct {
	interval time.Duration
}

func (module *worker) Initialize(ctx *application.Context) error {
	ctx.Settings().Subset("worker").Setting(
		"interval",
		&module.interval,
		"Delay between work cycles",
	)
	return nil
}

func (module *worker) Start(ctx *application.Context) error {
	ctx.Logger().Info("worker started", "interval", module.interval)
	return nil
}

func (*worker) Stop(ctx *application.Context) error {
	ctx.Logger().Info("worker stopped")
	return nil
}

func main() {
	app, err := application.New(
		"example",
		"1.0.0",
		application.WithConfigFile("application.hcl"),
		application.WithModule("worker", &worker{interval: 30 * time.Second}),
	)
	if err != nil {
		slog.Error("create application", "error", err)
		os.Exit(1)
	}

	if err := app.Run(context.Background(), application.WithSignals()); err != nil {
		slog.Error("run application", "error", err)
		os.Exit(1)
	}
}
```

```hcl
worker {
  interval = "10s"
}
```

See [`examples/simple`](examples/simple) for a runnable two-module application
that demonstrates settings, structured HCL, signal handling, and reverse-order
shutdown.

The complete public API is available on
[pkg.go.dev](https://pkg.go.dev/github.com/renevo/application).

## Lifecycle

Module registration order defines lifecycle order. Registration is frozen
before initialization begins.

| Phase | Order | Interface |
| --- | --- | --- |
| Initialize | Forward | `Initializer` |
| Install | Forward, opt-in | `Installer` |
| Pre-start | Forward | `PreStarter` |
| Start | Forward | `Module` |
| Post-start | Forward | `PostStarter` |
| Pre-stop | Reverse | `PreStopper` |
| Stop | Reverse | `Module` |
| Post-stop | Reverse | `PostStopper` |

The application is single-use. `Validate` may prepare an application before
one call to `Run`. `Install` is a separate terminal workflow. Concurrent or
invalid operations return sentinel errors rather than waiting indefinitely.

If `Start` fails, the module whose hook was entered is included in teardown;
later modules whose `Start` hooks were not entered are excluded. Shutdown hooks
continue after errors, and failures are combined with `errors.Join`.

## Configuration

### Settings

Modules register scalar settings during `Initialize` through
`Context.Settings`. Dot-separated setting paths map to nested singleton HCL
blocks. For example, `http.server.read_timeout` maps to:

```hcl
http {
  server {
    read_timeout = "5s"
  }
}
```

Settings are decoded through their registered codecs and committed atomically.
Environment values are loaded automatically for every registered scalar
setting. Dots and other separators become underscores and names are uppercased,
so `Http.Address` maps to `HTTP_ADDRESS` and
`Http.Server.Read_timeout` maps to `HTTP_SERVER_READ_TIMEOUT`.

Use `WithEnvPrefix` to namespace generated names:

```go
app, err := application.New(
	"example",
	"1.0.0",
	application.WithEnvPrefix("MYAPP"),
)
```

The prefixed address setting is read from `MYAPP_HTTP_ADDRESS`. Prefixes are
uppercased and surrounding underscores are removed. Invalid portable identifier
characters are rejected during application construction.

Scalar precedence is registered default, then HCL or JSON, then environment.
Environment text is decoded by the setting's registered codec, so values such
as durations and booleans use the same validation as file values. A present
environment variable with an empty value is an explicit override. If two
setting paths normalize to the same environment name, configuration loading
fails rather than choosing one.

`Application.Reload` atomically reloads every scalar source. File-backed
applications reparse the file and re-read environment values. Applications
without a file re-read environment values over registered defaults. Removing
an environment variable reveals the file value or, without a file, restores
the registered default. Failed reloads retain the last committed settings.

### Structured HCL

Use `Context.BindConfig` during `Initialize` for configuration that does not fit
a scalar setting tree, including repeated blocks, labels, and composite values:

```go
type route struct {
	Name   string `config:"name,label"`
	Target string `config:"target"`
}

type routerConfig struct {
	Routes []route `config:"route,block"`
}

func (module *router) Initialize(ctx *application.Context) error {
	return ctx.BindConfig(&module.config)
}
```

Structured bindings are staged and published only after the complete initial
configuration succeeds. They are startup-only and are not changed by runtime
reload. File reload still validates structured HCL before committing scalar
settings.

Both native HCL (`.hcl`) and JSON (`.json`) files are supported.

### Starter configuration

`Application.WriteConfigTemplate` writes a complete native HCL starter file
from defaults registered during module initialization. It does not read the
configured file or environment variables, so it can create the file selected
by `WithConfigFile` before that file exists.

```go
filename := "application.hcl"
file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
if err != nil {
	return err
}

if err := app.WriteConfigTemplate(context.Background(), file); err != nil {
	file.Close()
	os.Remove(filename)
	return err
}
if err := file.Close(); err != nil {
	os.Remove(filename)
	return err
}
```

Scalar setting descriptions and structured `description` tags become HCL
comments. Built-in booleans and numbers use native HCL literals, while strings
and `time.Duration` use readable quoted strings. Nil or empty structured block
fields produce one commented example block. Template generation is terminal
for the `Application`; construct a new application before calling `Run`.

## Shutdown and Signals

`Application.Shutdown` is idempotent and nonblocking. The first shutdown cause
is preserved and returned by `Run`; `Shutdown(nil)` is normal termination.
Teardown receives a fresh context with one overall deadline configured by
`WithShutdownTimeout`.

Signal ownership is opt-in:

```go
err := app.Run(ctx, application.WithSignals())
```

With no arguments, `WithSignals` handles `SIGTERM`, `SIGABRT`, `SIGQUIT`,
`SIGINT`, `SIGHUP`, and signal 21. `SIGHUP` always triggers scalar configuration
reload; the other signals request graceful shutdown. Use explicit arguments to
select a different set, including `WithSignals(syscall.SIGHUP)` for reload-only
signal handling. Without this option, the package responds only to the parent
context and explicit `Shutdown` or `Reload` calls.

## Error Handling

Lifecycle hook failures are wrapped in `*application.PhaseError`, which exposes
the module, phase, and underlying error:

```go
var phaseErr *application.PhaseError
if errors.As(err, &phaseErr) {
	slog.Error("module lifecycle failed",
		"module", phaseErr.Module,
		"phase", phaseErr.Phase,
		"error", phaseErr.Err,
	)
}
```

Package sentinel errors support `errors.Is` for invalid state, concurrent
operations, duplicate registration, missing configuration, and related API
conditions.

## Project Status

The module follows semantic versioning. Releases before v1 may introduce
breaking API changes. Pin a specific version in shared or production systems.

## Contributing

[Issues](https://github.com/renevo/application/issues) and pull requests are
welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, required
checks, and contribution expectations.

## License

This project is available under the [MIT License](LICENSE).