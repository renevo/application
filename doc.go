// Package application coordinates configuration and deterministic lifecycle
// execution for an ordered set of modules.
//
// Modules start serially in registration order and stop serially in reverse
// order. During initialization, modules may register scalar settings or bind
// typed HCL structures; the complete initial configuration is validated before
// the application starts. Applications are single-use, and process signal
// handling is opt-in.
package application
