// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/samber/do/v2"
	"github.com/spf13/cobra"

	flowi "github.com/githugt/flowi-lib"
	"github.com/githugt/flowi-lib/diag"
	"github.com/githugt/flowi-lib/engine"
	"github.com/githugt/flowi-lib/otel"
	"github.com/githugt/flowi-lib/settings"
	"github.com/githugt/flowi-lib/vault"

	"github.com/githugt/flowi-cli/internal/buildinfo"
)

// traceFlushTimeout bounds how long shutdown waits for queued traces to be
// sent; a collector that is down must not delay the command's exit.
const traceFlushTimeout = 5 * time.Second

// Session is an opened workspace plus the diagnostics found while opening.
type Session struct {
	Workspace *flowi.Workspace
	OpenDiags diag.List
	traces    *otel.Exporter // nil unless otel.endpoint is set
}

// Shutdown sends the traces still queued. It satisfies the injector's
// shutdown hook, which runs after the command has finished.
func (s *Session) Shutdown(ctx context.Context) error {
	if s.traces == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, traceFlushTimeout)
	defer cancel()
	return s.traces.Close(ctx)
}

// SessionOption adjusts OpenSession.
type SessionOption func(*sessionConfig)

type sessionConfig struct {
	traceLog  *slog.Logger
	observers []func(engine.Event)
}

// WithObserver passes every engine event to fn (see engine.Deps.Observe).
func WithObserver(fn func(engine.Event)) SessionOption {
	return func(c *sessionConfig) { c.observers = append(c.observers, fn) }
}

// WithTracing exports traces to the collector named by otel.endpoint, when
// one is set; log receives export failures.
func WithTracing(log *slog.Logger) SessionOption {
	return func(c *sessionConfig) { c.traceLog = log }
}

// OpenSession opens the workspace selected by the global options. keys is
// the OS keychain for the vault (nil for none); passphrase asks for the
// vault passphrase (nil when no prompt is possible).
func OpenSession(ctx context.Context, g *GlobalOptions, env Env, keys vault.KeyStore, passphrase func() (string, error), opts ...SessionOption) (*Session, error) {
	var cfg sessionConfig
	for _, o := range opts {
		o(&cfg)
	}
	paths, err := g.Paths(env.Environ)
	if err != nil {
		return nil, err
	}
	if err := checkLogLevel(g.LogLevel); err != nil {
		return nil, err
	}
	wsOpts := flowi.WorkspaceOptions{
		Paths: paths, Environ: env.Environ, Flags: g.FlagLayer(), Home: env.Home,
		Keys: keys, Passphrase: passphrase, UserAgent: "Flowi-CLI/" + buildinfo.Version(),
	}
	traces, err := startTracing(cfg, g, paths, env, &wsOpts)
	if err != nil {
		return nil, err
	}
	wsOpts.Observe = engine.Observers(append(cfg.observers, wsOpts.Observe)...)
	ws, diags, err := flowi.OpenWorkspace(ctx, wsOpts)
	if err != nil {
		closeQuietly(traces)
		return nil, err
	}
	return &Session{Workspace: ws, OpenDiags: diags, traces: traces}, nil
}

// startTracing creates the exporter when tracing was asked for and
// otel.endpoint is set, and connects it to the workspace options. A
// malformed otel setting is an error: silently not tracing would hide the
// mistake.
func startTracing(cfg sessionConfig, g *GlobalOptions, paths settings.Paths, env Env, wsOpts *flowi.WorkspaceOptions) (*otel.Exporter, error) {
	if cfg.traceLog == nil {
		return nil, nil
	}
	resolved, _, err := flowi.UserSettings(paths, env.Environ, g.FlagLayer())
	if err != nil {
		return nil, err
	}
	ocfg, on, err := otel.ConfigFrom(resolved, buildinfo.Version())
	if err != nil {
		return nil, &UsageError{Err: err}
	}
	if !on {
		return nil, nil
	}
	exporter, err := otel.New(ocfg, cfg.traceLog)
	if err != nil {
		return nil, &UsageError{Err: err}
	}
	wsOpts.Observe, wsOpts.Traceparent = exporter.Observe, exporter.Traceparent
	return exporter, nil
}

// closeQuietly drops an exporter whose session never opened.
func closeQuietly(e *otel.Exporter) {
	if e == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = e.Close(ctx) // nothing was observed; there is nothing to report
}

func checkLogLevel(level string) error {
	switch level {
	case "", "debug", "info", "warn", "error":
		return nil
	}
	return &UsageError{Err: fmt.Errorf("unknown log level %q (want debug, info, warn or error)", level)}
}

// session returns the workspace session, printing any problems found while
// opening it to standard error.
func session(cmd *cobra.Command, i do.Injector) (*Session, error) {
	s, err := do.Invoke[*Session](i)
	if err != nil {
		return nil, err
	}
	if len(s.OpenDiags) > 0 {
		g := do.MustInvoke[*GlobalOptions](i)
		env := do.MustInvoke[Env](i)
		if err := PrintDiagnostics(cmd.ErrOrStderr(), g.Output, env.Cwd, s.OpenDiags); err != nil {
			return nil, err
		}
	}
	return s, nil
}
