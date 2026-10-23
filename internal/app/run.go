// SPDX-License-Identifier: AGPL-3.0-or-later

// Package app is the composition root of the flowi CLI: it wires
// dependencies, runs one command and shuts everything down.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/samber/do/v2"

	"github.com/githugt/flowi-lib/diag"

	"github.com/githugt/flowi-cli/internal/cli"
)

// shutdownTimeout bounds application shutdown. It is unrelated to the
// flow-run grace period. When exceeded, Run stops waiting for services
// that have not finished, logs Event=ShutdownAbandoned, and returns.
const shutdownTimeout = 10 * time.Second

var errShutdownTimeout = errors.New("application shutdown timed out")

// Stdio holds the process streams. In may be nil (no input).
type Stdio struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

type options struct {
	providers       []func(do.Injector)
	shutdownTimeout time.Duration
	done            <-chan struct{} // the command context's Done, for prompts
}

// Option customises Run.
type Option func(*options)

// WithProvider registers extra services, typically test doubles.
func WithProvider(provide func(do.Injector)) Option {
	return func(o *options) { o.providers = append(o.providers, provide) }
}

// withShutdownTimeout shortens the shutdown bound in tests.
func withShutdownTimeout(d time.Duration) Option {
	return func(o *options) { o.shutdownTimeout = d }
}

// Run executes one flowi command and returns the process exit code.
func Run(ctx context.Context, args []string, stdio Stdio, opts ...Option) int {
	o := options{shutdownTimeout: shutdownTimeout, done: ctx.Done()}
	for _, opt := range opts {
		opt(&o)
	}
	injector := NewInjector(stdio, o)
	defer shutdown(injector, o.shutdownTimeout)

	if ctx.Err() != nil {
		return cli.ExitCancelled
	}
	command, err := executeCommand(ctx, injector, args, stdio)
	if err != nil && ctx.Err() == nil && !cli.IsReported(err) {
		reportError(stdio.Err, err)
	}
	code := cli.ExitCodeFor(ctx, err)
	logCommandFinished(injector, command, code)
	recordTelemetry(injector, command, code)
	return code
}

func executeCommand(ctx context.Context, injector do.Injector, args []string, stdio Stdio) (string, error) {
	root := cli.NewRootCommand(injector)
	root.SetArgs(args)
	if stdio.In != nil {
		root.SetIn(stdio.In)
	}
	root.SetOut(stdio.Out)
	root.SetErr(stdio.Err)
	cmd, err := root.ExecuteContextC(ctx)
	if cmd == nil {
		return root.Name(), err
	}
	return cmd.CommandPath(), err
}

// logCommandFinished records the command outcome at debug level. Logging
// problems (for example an unwritable log file) are reported on stderr by
// the fallback logger and never change the exit code.
func logCommandFinished(injector do.Injector, command string, code int) {
	logs, err := do.Invoke[*Logging](injector)
	if err != nil {
		do.MustInvoke[*slog.Logger](injector).Warn("logging unavailable", "Event", "LoggingUnavailable", "Error", err)
		return
	}
	logs.Logger.Debug("command finished", "Event", "CommandFinished", "Command", command, "ExitCode", code)
}

// shutdown releases services in reverse registration order. It uses a
// fresh context because the command context may already be cancelled, and
// it stops waiting at the timeout: samber/do waits for every service, so a
// service that ignores its context would otherwise hang the process.
//
// Goroutine ownership: the goroutine started here runs the injector's
// shutdown. If abandoned it keeps running until the services return or the
// process exits, which follows immediately.
func shutdown(injector *do.RootScope, timeout time.Duration) {
	ctx, cancel := context.WithTimeoutCause(context.Background(), timeout, errShutdownTimeout)
	defer cancel()
	logger := do.MustInvoke[*slog.Logger](injector) // resolved before services start shutting down
	done := make(chan *do.ShutdownReport, 1)
	go func() { done <- injector.ShutdownWithContext(ctx) }()
	select {
	case report := <-done:
		if report != nil && len(report.Errors) > 0 {
			logger.Error("application shutdown failed", "Event", "ShutdownFailed", "Error", report.Error())
		}
	case <-ctx.Done():
		logger.Error("application shutdown abandoned", "Event", "ShutdownAbandoned", "Timeout", timeout, "Cause", context.Cause(ctx))
	}
}

// reportError prints a command error. Diagnostics keep their codes so
// users can search for them; anything else is printed as "Error: ...".
// Writing to stderr is best effort: it is the last resort.
func reportError(stderr io.Writer, err error) {
	var diags *diag.ListError
	if errors.As(err, &diags) {
		_ = cli.PrintDiagnostics(stderr, cli.OutputText, "", diags.List)
		return
	}
	_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
}

// recordTelemetry notes the finished command for the opt-in usage report.
// A problem reading the telemetry settings is logged and ignored: usage
// reporting must never change a command's outcome.
func recordTelemetry(injector do.Injector, command string, code int) {
	t, err := do.Invoke[*cli.Telemetry](injector)
	if err != nil {
		do.MustInvoke[*slog.Logger](injector).Debug("telemetry unavailable", "Event", "TelemetryUnavailable", "Error", err)
		return
	}
	t.Record(command, code)
}
