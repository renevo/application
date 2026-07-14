# Contributing

Thank you for contributing to `github.com/renevo/application`. Contributions
may include bug reports, documentation, tests, lifecycle improvements, and
configuration hardening.

## Before You Start

For behavior changes or significant API additions, open an issue first so the
contract and scope can be discussed before implementation. Small bug fixes,
tests, and documentation corrections can go directly to a pull request.

Keep changes focused. Avoid unrelated refactors in the same pull request, and
preserve deterministic lifecycle and transactional configuration behavior.

## Development Requirements

- Go 1.26 or later.
- [`goimports`](https://pkg.go.dev/golang.org/x/tools/cmd/goimports).
- [`golangci-lint` v2](https://golangci-lint.run/).
- [`ripgrep`](https://github.com/BurntSushi/ripgrep) for workspace-wide
  formatting commands.
- A C compiler for Go race-detector builds.

Install the Go tools when needed:

```sh
go install golang.org/x/tools/cmd/goimports@latest
```

Follow the official golangci-lint installation instructions for a current v2
release. Confirm the selected binary before running checks:

```sh
golangci-lint-v2 version
```

## Getting the Source

```sh
git clone https://github.com/renevo/application.git
cd application
go mod download
```

Run the baseline tests before making changes:

```sh
go test ./...
```

## Making Changes

- Follow standard Go conventions and existing package patterns.
- Keep exported APIs small and document every exported identifier.
- Prefer typed sentinel or structured errors that support `errors.Is` and
  `errors.As`.
- Do not expose mutable lifecycle or registry internals.
- Preserve module registration order for startup and reverse order for
  teardown.
- Keep configuration commits atomic; a failed parse, decode, or validation must
  not partially update live values.
- Add focused tests for behavior changes and regression tests for bug fixes.
- Update the README and examples when changing public behavior.

Breaking changes are possible before v1, but they should still be deliberate,
documented, and justified in the pull request.

## Formatting

Run `goimports` over every Go source file before submitting:

```sh
goimports -w -local github.com/renevo/application $(rg --files -g '*.go')
```

Verify that no formatting changes remain:

```sh
test -z "$(goimports -l -local github.com/renevo/application $(rg --files -g '*.go'))"
```

## Required Checks

Run the complete validation suite locally:

```sh
go test ./...
CGO_ENABLED=1 go test -race ./...
go vet ./...
golangci-lint-v2 run ./...
```

Tests must pass on supported platforms without relying on execution order,
network services, machine-specific paths, or process-wide signals unless the
test is explicitly an integration test.

## Testing Guidance

- Use table-driven tests when several cases share one contract.
- Exercise failure paths as well as successful behavior.
- For lifecycle changes, verify exact forward and reverse hook order, partial
  startup cleanup, cancellation causes, and timeout behavior.
- For configuration changes, verify transactional rollback and preservation of
  the last committed values after failure.
- Run concurrency-sensitive changes with the race detector.

## Pull Requests

A pull request should:

- Explain the problem and the chosen solution.
- Describe user-visible behavior or API changes.
- Include tests for changed behavior.
- Update relevant documentation and examples.
- Pass formatting, tests, race detection, vet, and golangci-lint v2.
- Avoid generated files, binaries, editor settings, and unrelated dependency
  updates unless they are required by the change.

Maintainers may request that large changes be split into smaller pull requests.

## Reporting Bugs

Include the Go version, operating system, package version or commit, a minimal
reproduction, expected behavior, actual behavior, and complete error output.
For concurrency problems, include race-detector output when available.

## License

By contributing, you agree that your contributions are licensed under the
project's [MIT License](LICENSE).