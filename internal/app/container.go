// SPDX-License-Identifier: AGPL-3.0-or-later

package app

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/samber/do/v2"

	flowi "github.com/githugt/flowi-lib"
	"github.com/githugt/flowi-lib/settings"
	"github.com/githugt/flowi-lib/vault"

	"github.com/githugt/flowi-cli/internal/cli"
)

// Logging owns the process logger and its log file.
type Logging struct {
	Logger *slog.Logger
	close  func() error
}

// Shutdown closes the log file, if any.
func (l *Logging) Shutdown(context.Context) error { return l.close() }

// NewInjector builds the application's dependency graph. It always creates
// a fresh root scope; the package-level default injector is never used.
// Services that touch disk are lazy, so "flowi version" creates nothing.
func NewInjector(stdio Stdio, opts options) *do.RootScope {
	i := do.New()
	do.ProvideValue(i, stdio)
	do.ProvideValue(i, slog.New(slog.NewTextHandler(stdio.Err, nil))) // fallback before settings are known
	do.ProvideValue(i, &cli.GlobalOptions{})
	env := processEnv()
	do.ProvideValue(i, env)
	do.ProvideValue(i, cli.NewConsole(stdio.In, stdio.Out, stdio.Err, env.Environ, opts.done))
	do.ProvideValue(i, vault.OSKeyStore())
	do.Provide(i, provideTelemetry)
	do.Provide(i, provideSession)
	do.Provide(i, provideLogging)
	do.ProvideNamed(i, cli.LoggerName, func(i do.Injector) (*slog.Logger, error) {
		l, err := do.Invoke[*Logging](i)
		if err != nil {
			return nil, err
		}
		return l.Logger, nil
	})
	for _, provide := range opts.providers {
		provide(i)
	}
	return i
}

func processEnv() cli.Env {
	home, _ := os.UserHomeDir() // without a home dir, "~" paths simply stay unexpanded
	cwd, _ := os.Getwd()        // without a cwd, diagnostics show absolute paths
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved // collection paths are symlink-resolved; match them
	}
	return cli.Env{Environ: settings.ProcessEnviron(), Home: home, Cwd: cwd}
}

func provideSession(i do.Injector) (*cli.Session, error) {
	console := do.MustInvoke[cli.Console](i)
	logging, err := do.Invoke[*Logging](i)
	if err != nil {
		return nil, err
	}
	tele, err := do.Invoke[*cli.Telemetry](i)
	if err != nil {
		return nil, err
	}
	return cli.OpenSession(context.Background(), do.MustInvoke[*cli.GlobalOptions](i), do.MustInvoke[cli.Env](i),
		do.MustInvoke[vault.KeyStore](i), console.Passphrase(), cli.WithTracing(logging.Logger), cli.WithObserver(tele.Observe))
}

// provideTelemetry reads only personal settings, like logging. The report
// is sent when the injector shuts down.
func provideTelemetry(i do.Injector) (*cli.Telemetry, error) {
	logging, err := do.Invoke[*Logging](i)
	if err != nil {
		return nil, err
	}
	return cli.NewTelemetry(do.MustInvoke[*cli.GlobalOptions](i), do.MustInvoke[cli.Env](i), logging.Logger)
}

// provideLogging reads only personal settings (user file, env, flags), so
// logging works without opening a workspace.
func provideLogging(i do.Injector) (*Logging, error) {
	g := do.MustInvoke[*cli.GlobalOptions](i)
	env := do.MustInvoke[cli.Env](i)
	paths, err := g.Paths(env.Environ)
	if err != nil {
		return nil, err
	}
	resolved, _, err := flowi.UserSettings(paths, env.Environ, g.FlagLayer())
	if err != nil {
		return nil, err
	}
	logger, closeLog, err := NewLogger(resolved, do.MustInvoke[Stdio](i).Err)
	if err != nil {
		return nil, err
	}
	return &Logging{Logger: logger, close: closeLog}, nil
}
