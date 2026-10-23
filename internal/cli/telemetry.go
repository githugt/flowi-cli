// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/samber/do/v2"
	"github.com/spf13/cobra"

	flowi "github.com/githugt/flowi-lib"
	"github.com/githugt/flowi-lib/engine"
	"github.com/githugt/flowi-lib/settings"
	"github.com/githugt/flowi-lib/telemetry"

	"github.com/githugt/flowi-cli/internal/buildinfo"
)

// Telemetry is the process's opt-in usage report: it collects while the
// command runs and sends at shutdown, when the user turned telemetry on and
// set an endpoint.
type Telemetry struct {
	collector *telemetry.Collector
	sender    *telemetry.Sender
	log       *slog.Logger
	// current re-reads the settings at shutdown, so a command that turns
	// telemetry off (or down) does not send what was collected before it ran.
	current func() (settings.Resolved, error)
}

// NewTelemetry reads the personal telemetry settings. With telemetry off
// (the default) the result collects nothing.
func NewTelemetry(g *GlobalOptions, env Env, log *slog.Logger) (*Telemetry, error) {
	resolved, err := userSettings(g, env)
	if err != nil {
		return nil, err
	}
	client := telemetry.Client{App: "cli", Version: buildinfo.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH}
	c := telemetry.NewCollector(levelOf(resolved), client, time.Now)
	c.Started()
	return &Telemetry{collector: c, sender: telemetry.NewSender(stringSetting(resolved, "telemetry_endpoint"), nil), log: log,
		current: func() (settings.Resolved, error) { return userSettings(g, env) }}, nil
}

func userSettings(g *GlobalOptions, env Env) (settings.Resolved, error) {
	paths, err := g.Paths(env.Environ)
	if err != nil {
		return settings.Resolved{}, err
	}
	resolved, _, err := flowi.UserSettings(paths, env.Environ, g.FlagLayer())
	return resolved, err
}

func levelOf(r settings.Resolved) telemetry.Level {
	return telemetry.ParseLevel(stringSetting(r, "telemetry"))
}

func stringSetting(r settings.Resolved, name string) string {
	v, _ := r.Get(name)
	s, _ := v.Value.(string)
	return s
}

// Observe feeds the engine's events to the collector.
func (t *Telemetry) Observe(ev engine.Event) { t.collector.Observe(ev) }

// Record notes that a command finished. commandPath is cobra's command path
// ("flowi workspace mount"); only the first word after "flowi" is used.
func (t *Telemetry) Record(commandPath string, exitCode int) {
	fields := map[string]string{"command": commandName(commandPath), "exit": exitName(exitCode)}
	if err := t.collector.Record(telemetry.EventCommandRun, fields); err != nil {
		t.log.Debug("telemetry event refused", "Event", "TelemetryEventRefused", "Error", err)
	}
}

// Shutdown sends the report, if there is one. A report that cannot be sent
// is dropped: telemetry never delays or fails a command.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	t.applyCurrentSettings()
	p, ok := t.collector.Payload()
	if !ok {
		return nil
	}
	if err := t.sender.Send(ctx, p); err != nil && !errors.Is(err, telemetry.ErrNoEndpoint) {
		t.log.Debug("telemetry report not sent", "Event", "TelemetrySendFailed", "Error", err)
	}
	return nil
}

// applyCurrentSettings lowers the collector to the level the settings have
// now, and sends nowhere if the endpoint was removed. A settings file that
// cannot be read counts as "off": when in doubt, send nothing.
func (t *Telemetry) applyCurrentSettings() {
	resolved, err := t.current()
	if err != nil {
		t.collector.Lower(telemetry.LevelOff)
		return
	}
	t.collector.Lower(levelOf(resolved))
	t.sender = telemetry.NewSender(stringSetting(resolved, "telemetry_endpoint"), nil)
}

// commandName maps a cobra command path to a name in the catalogue.
func commandName(path string) string {
	words := strings.Fields(path)
	if len(words) < 2 {
		return "other"
	}
	for _, known := range telemetry.CommandNames() {
		if words[1] == known {
			return known
		}
	}
	return "other"
}

func exitName(code int) string {
	switch code {
	case ExitOK:
		return "ok"
	case ExitFailed:
		return "failed"
	case ExitUsage:
		return "usage"
	case ExitCancelled:
		return "cancelled"
	}
	return "error"
}

func newTelemetryCommand(i do.Injector) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "telemetry",
		Short: "See what usage reporting would send (it is off unless you turn it on)",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newTelemetryShowCommand(i), newTelemetryStatusCommand(i), newTelemetryEventsCommand())
	return cmd
}

func newTelemetryShowCommand(i do.Injector) *cobra.Command {
	var level string
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Print exactly what a report would contain, with example values",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			l, err := levelToShow(i, level)
			if err != nil {
				return err
			}
			client := telemetry.Client{App: "cli", Version: buildinfo.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(telemetry.Sample(l, client))
		},
	}
	cmd.Flags().StringVar(&level, "level", "", "preview this level instead of the one set: off, minimal or full")
	return cmd
}

func levelToShow(i do.Injector, flag string) (telemetry.Level, error) {
	if flag != "" {
		if flag != "off" && flag != "minimal" && flag != "full" {
			return telemetry.LevelOff, &UsageError{Err: fmt.Errorf("unknown level %q (want off, minimal or full)", flag)}
		}
		return telemetry.ParseLevel(flag), nil
	}
	resolved, err := userSettings(do.MustInvoke[*GlobalOptions](i), do.MustInvoke[Env](i))
	if err != nil {
		return telemetry.LevelOff, err
	}
	return levelOf(resolved), nil
}

func newTelemetryStatusCommand(i do.Injector) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Explain telemetry and show whether it is on",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolved, err := userSettings(do.MustInvoke[*GlobalOptions](i), do.MustInvoke[Env](i))
			if err != nil {
				return err
			}
			endpoint := stringSetting(resolved, "telemetry_endpoint")
			if endpoint == "" {
				endpoint = "(none)"
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%s\n\nlevel: %s\nendpoint: %s\n\n", telemetry.Explanation(), levelOf(resolved), endpoint)
			fmt.Fprintln(out, "Change it with: flowi config set telemetry off|minimal|full")
			return nil
		},
	}
}

func newTelemetryEventsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "events",
		Short: "List every event telemetry can ever report",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			for _, e := range telemetry.Catalogue() {
				fmt.Fprintf(tw, "%s\t%s\t%s\n", e.Name, e.Level, e.Doc)
				for _, f := range e.Fields {
					fmt.Fprintf(tw, "  %s\t\t%s: %s\n", f.Name, f.Doc, strings.Join(f.Allowed, ", "))
				}
			}
			return tw.Flush()
		},
	}
}
