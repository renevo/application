# Simple example

This example runs two modules:

- `poller` registers settings only.
- `http` registers settings and binds repeated labeled HCL route blocks.

Each module lives in its own package under `modules/<name>/module.go`; `main.go`
only assembles the application.

From the repository root, run:

```sh
go run ./examples/simple -config ./examples/simple/application.hcl
```

Press Ctrl+C to stop the application and observe reverse module shutdown order.

Generate a new starter configuration at a path that does not already exist:

```sh
go run ./examples/simple -config ./generated.hcl -generate-config
```

Generation fails rather than replacing an existing file.