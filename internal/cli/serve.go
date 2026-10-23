// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/samber/do/v2"
	"github.com/spf13/cobra"

	flowi "github.com/githugt/flowi-lib"
	"github.com/githugt/flowi-lib/serve/replay"
	"github.com/githugt/flowi-lib/vault"
)

type serveFlags struct {
	replayAddr, triggerAddr, record   string
	recordTo                          string
	valkeyAddr                        string
	latency, shutdown                 time.Duration
	sequence, cors, quiet             bool
	noReplay, noTriggers, noSchedules bool
}

func newServeCommand(i do.Injector) *cobra.Command {
	var f serveFlags
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Replay saved responses, run flows on HTTP calls and on schedules",
		Long: "Run a long-lived process with up to three parts:\n\n" +
			"  Replayer   answers HTTP calls with the pinned examples and saved responses of your requests\n" +
			"             (default 127.0.0.1:8525). Steer it with request headers: X-Flowi-Status, X-Flowi-Example,\n" +
			"             X-Flowi-Request, X-Flowi-Version, X-Flowi-Latency. GET /__flowi/routes lists what it knows.\n" +
			"  Triggers   POST /flows/<flow>/run starts a flow (default 127.0.0.1:8526). Needs a bearer token:\n" +
			"             create one with `flowi serve token`, or set FLOWI_SECRET_SERVE_TRIGGER_TOKEN.\n" +
			"  Schedules  runs the cron schedules of workspace.yaml (see `flowi schedule`).\n\n" +
			"All listeners bind to localhost unless you say otherwise. Ctrl-C (or SIGTERM) stops accepting calls,\n" +
			"lets running requests and flows finish for up to --shutdown-timeout, then cancels what is left.\n" +
			"Exit codes: 0 stopped cleanly, 3 work had to be cancelled at the timeout, 2 bad options, 4 a system failure.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return runServe(cmd, i, f) },
	}
	fl := cmd.Flags()
	fl.StringVar(&f.replayAddr, "replay-addr", "", "replayer address (default: the serve.replay_addr setting)")
	fl.StringVar(&f.triggerAddr, "trigger-addr", "", "trigger address (default: the serve.trigger_addr setting)")
	fl.DurationVar(&f.latency, "latency", 0, "delay added to every replayed answer (default: the serve.latency setting)")
	fl.BoolVar(&f.sequence, "sequence", false, "answer a request's responses one call at a time, in file-name order, then start over")
	fl.BoolVar(&f.cors, "cors", false, "let browsers call the replayer from any origin")
	fl.StringVar(&f.record, "record", "", "forward calls to this real API and save its responses; a call no request matches becomes a new request")
	fl.StringVar(&f.recordTo, "record-to", "", "with --record: the collection (name or id) new requests are saved in (default: the Scratch draft)")
	fl.BoolVar(&f.noReplay, "no-replay", false, "do not start the replayer")
	fl.BoolVar(&f.noTriggers, "no-triggers", false, "do not start the HTTP triggers")
	fl.BoolVar(&f.noSchedules, "no-schedules", false, "do not run schedules")
	fl.DurationVar(&f.shutdown, "shutdown-timeout", 0, "how long to let running work finish at shutdown (default: the serve.shutdown_timeout setting)")
	fl.StringVar(&f.valkeyAddr, "valkey-addr", "", "share run records and schedule ticks with other instances through this Valkey/Redis server, host:port (default: the serve.valkey_addr setting)")
	fl.BoolVar(&f.quiet, "quiet", false, "do not print each replayed call")
	cmd.AddCommand(newServeTokenCommand(i), newServeValkeyPasswordCommand(i))
	return cmd
}

// options applies the flags the user gave on top of the settings.
func (f serveFlags) options(cmd *cobra.Command, base flowi.ServeOptions) flowi.ServeOptions {
	changed := cmd.Flags().Changed
	if changed("replay-addr") {
		base.ReplayAddr = f.replayAddr
	}
	if changed("trigger-addr") {
		base.TriggerAddr = f.triggerAddr
	}
	if changed("latency") {
		base.Latency = f.latency
	}
	if changed("valkey-addr") {
		base.ValkeyAddr = f.valkeyAddr
	}
	if changed("shutdown-timeout") {
		base.ShutdownTimeout = f.shutdown
	}
	base.Sequence, base.CORS, base.Record, base.RecordTo = f.sequence, f.cors, f.record, f.recordTo
	base.NoReplay, base.NoTriggers, base.NoSchedules = f.noReplay, f.noTriggers, f.noSchedules
	return base
}

func runServe(cmd *cobra.Command, i do.Injector, f serveFlags) error {
	s, err := session(cmd, i)
	if err != nil {
		return err
	}
	opts := f.options(cmd, s.Workspace.ServeDefaults())
	if logger, err := do.InvokeNamed[*slog.Logger](i, LoggerName); err == nil {
		opts.Logger = logger
	}
	srv, err := s.Workspace.Serve(cmd.Context(), opts)
	if err != nil {
		return asServeError(err)
	}
	out := cmd.OutOrStdout()
	printServing(out, srv, s.Workspace)
	if !f.quiet && srv.ReplayAddr() != "" {
		go printTraffic(out, srv.Traffic().Subscribe(cmd.Context()))
	}
	return finishServe(cmd, srv.Wait())
}

// asServeError makes unusable options a usage error (exit 2).
func asServeError(err error) error {
	if errors.Is(err, flowi.ErrInvalidServeOptions) {
		return &UsageError{Err: err}
	}
	return err
}

// finishServe reports how the server ended. A signal is the normal way to
// stop it, so a clean drain is success.
func finishServe(cmd *cobra.Command, err error) error {
	switch {
	case err == nil:
		fmt.Fprintln(cmd.OutOrStdout(), "Stopped.")
		return &StoppedError{}
	case errors.Is(err, flowi.ErrShutdownTimeout):
		fmt.Fprintf(cmd.ErrOrStderr(), "Stopped, but work was still going at the shutdown timeout and was cancelled: %v\n", err)
		return Exit(ExitCancelled)
	}
	return err
}

func printServing(w io.Writer, srv *flowi.Server, ws *flowi.Workspace) {
	if a := srv.ReplayAddr(); a != "" {
		fmt.Fprintf(w, "Replayer   http://%s\n", a)
	}
	if a := srv.TriggerAddr(); a != "" {
		fmt.Fprintf(w, "Triggers   http://%s  (bearer token required)\n", a)
	}
	for _, u := range srv.Upcoming() {
		fmt.Fprintf(w, "Schedule   %s  next %s\n", u.Name, u.At.Format("2006-01-02 15:04 MST"))
	}
	for _, n := range srv.Notes() {
		fmt.Fprintf(w, "note: %s\n", n)
	}
	fmt.Fprintf(w, "Serving %d collection(s). Press Ctrl-C to stop.\n", len(ws.Collections()))
}

// printTraffic prints each replayed call until the channel closes.
func printTraffic(w io.Writer, entries <-chan replay.Entry) {
	for e := range entries {
		what := e.Request
		if what == "" {
			what = "-"
		}
		extra := e.Match
		if e.Example != "" {
			extra += " " + e.Example
		}
		fmt.Fprintf(w, "%s  %s %s -> %d  %s  [%s]\n", e.At.Format("15:04:05"), e.Method, e.Path, e.Status, what, extra)
	}
}

func newServeTokenCommand(i do.Injector) *cobra.Command {
	var rotate bool
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Create the bearer token that authorises HTTP triggers",
		Long: "Create the token callers send as `Authorization: Bearer <token>` to POST /flows/<flow>/run. It is stored\n" +
			"in the workspace vault and printed once, on standard output. Run it again with --rotate to replace it.\n" +
			"FLOWI_SECRET_SERVE_TRIGGER_TOKEN overrides the stored token (for containers).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return runServeToken(cmd, i, rotate) },
	}
	cmd.Flags().BoolVar(&rotate, "rotate", false, "replace the existing token (the old one stops working)")
	return cmd
}

func runServeToken(cmd *cobra.Command, i do.Injector, rotate bool) error {
	s, err := session(cmd, i)
	if err != nil {
		return err
	}
	_, exists, err := s.Workspace.TriggerToken()
	if err != nil {
		return err
	}
	if exists && !rotate {
		fmt.Fprintln(cmd.ErrOrStderr(), "A trigger token is already set; its value is not shown. Use --rotate to replace it.")
		return nil
	}
	token, err := s.Workspace.NewTriggerToken()
	if err != nil {
		return err
	}
	env := do.MustInvoke[Env](i)
	if _, overridden := vault.Overridden(env.Environ, "serve", "trigger_token"); overridden {
		fmt.Fprintln(cmd.ErrOrStderr(), "Note: FLOWI_SECRET_SERVE_TRIGGER_TOKEN is set and overrides this token.")
	}
	fmt.Fprintln(cmd.OutOrStdout(), token)
	fmt.Fprintln(cmd.ErrOrStderr(), "Keep this token: it is stored encrypted and not shown again.")
	return nil
}

func newServeValkeyPasswordCommand(i do.Injector) *cobra.Command {
	return &cobra.Command{
		Use:   "valkey-password",
		Short: "Store the password of the shared Valkey/Redis server (read from standard input)",
		Long: "Reads one line from standard input and stores it encrypted in the workspace vault, for serve.valkey_addr.\n" +
			"For example: printf '%s' \"$PASSWORD\" | flowi serve valkey-password.\n" +
			"FLOWI_SECRET_SERVE_VALKEY_PASSWORD overrides the stored value (for containers).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			line, err := bufio.NewReader(io.LimitReader(cmd.InOrStdin(), maxPasswordBytes)).ReadString('\n')
			if err != nil && !errors.Is(err, io.EOF) {
				return fmt.Errorf("reading the password: %w", err)
			}
			password := strings.TrimRight(line, "\r\n")
			if password == "" {
				return &UsageError{Err: errors.New("no password on standard input")}
			}
			s, err := session(cmd, i)
			if err != nil {
				return err
			}
			if err := s.Workspace.SetValkeyPassword(password); err != nil {
				return err
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "The Valkey password is stored in the workspace vault.")
			return nil
		},
	}
}

// maxPasswordBytes bounds what is read from standard input.
const maxPasswordBytes = 4096
