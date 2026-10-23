// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"context"
	"errors"

	"github.com/githugt/flowi-lib/diag"
	"github.com/githugt/flowi-lib/store"
	"github.com/githugt/flowi-lib/workspace"
)

// Process exit codes. Scripts and CI rely on these values; do not renumber.
const (
	ExitOK        = 0 // command succeeded (warnings allowed)
	ExitFailed    = 1 // flow or assertion failed, or a check found differences
	ExitUsage     = 2 // invalid usage, input files or configuration
	ExitCancelled = 3 // interrupted or aborted
	ExitSystem    = 4 // I/O, network or other system failure
)

// StoppedError ends a long-running command that was asked to stop (a
// signal) and did so cleanly: the exit code is 0 although the command's
// context was cancelled.
type StoppedError struct{}

func (*StoppedError) Error() string { return "stopped" }

// UsageError marks an error caused by how the command was invoked.
type UsageError struct{ Err error }

func (e *UsageError) Error() string { return e.Err.Error() }
func (e *UsageError) Unwrap() error { return e.Err }

// ExitError ends a command with a code after it has already reported the
// problem itself; nothing more is printed.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return "exit status" }

// Exit returns an ExitError for code.
func Exit(code int) error { return &ExitError{Code: code} }

// userErrors are library errors caused by what the user asked for.
func userErrors() []error {
	return []error{
		workspace.ErrAlreadyMounted, workspace.ErrNotMounted,
		store.ErrAlreadyCollection, store.ErrNotCollection,
	}
}

// ExitCodeFor maps a command result to a process exit code. Cancellation
// wins over any error it caused.
func ExitCodeFor(ctx context.Context, err error) int {
	var stopped *StoppedError
	if errors.As(err, &stopped) {
		return ExitOK
	}
	if ctx.Err() != nil {
		return ExitCancelled
	}
	if err == nil {
		return ExitOK
	}
	var exit *ExitError
	if errors.As(err, &exit) {
		return exit.Code
	}
	if isUserError(err) {
		return ExitUsage
	}
	return ExitSystem
}

func isUserError(err error) bool {
	var usage *UsageError
	var diags *diag.ListError
	if errors.As(err, &usage) || errors.As(err, &diags) {
		return true
	}
	for _, target := range userErrors() {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

// IsReported reports whether the command already printed its own error.
func IsReported(err error) bool {
	var exit *ExitError
	var stopped *StoppedError
	return errors.As(err, &exit) || errors.As(err, &stopped)
}
