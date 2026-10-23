// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/samber/do/v2"
	"github.com/spf13/cobra"

	flowi "github.com/githugt/flowi-lib"
	"github.com/githugt/flowi-lib/diag"
	"github.com/githugt/flowi-lib/engine"
	"github.com/githugt/flowi-lib/model"

	"github.com/githugt/flowi-cli/internal/tui"
)

// pinDefault is the value --pin takes without an argument: name the
// example after the response status.
const pinDefault = "\x00status"

type sendFlags struct {
	collection, env, version, prefer, save, pin string
	with, skip                                  []string
	noHooks, headers, quiet                     bool
	seed                                        uint64
}

func newSendCommand(i do.Injector) *cobra.Command {
	var f sendFlags
	cmd := &cobra.Command{
		Use:   "send <request>",
		Short: "Send a saved request",
		Long: "Send a request (by path, e.g. auth/login, or id) with its hooks, and print the response.\n\n" +
			"The response body goes to standard output and everything else to standard error, so the\n" +
			"body can be piped (flowi send auth/login | jq .token).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return runSend(cmd, i, args[0], f) },
	}
	fl := cmd.Flags()
	fl.StringVar(&f.collection, "collection", "", "collection (name or id) to look in")
	fl.StringVar(&f.env, "env", "", "environment (default: the defaults.environment setting)")
	fl.StringVar(&f.version, "version", "", "send this version")
	fl.StringVar(&f.prefer, "prefer-version", "", "send this version if the request has it")
	fl.StringArrayVar(&f.with, "with", nil, "value for a name the request uses, as key=value (repeatable)")
	fl.BoolVar(&f.noHooks, "no-hooks", false, "run no API hooks")
	fl.StringSliceVar(&f.skip, "skip", nil, "skip pre or post hooks")
	fl.Uint64Var(&f.seed, "seed", 0, "seed for generated values (default: random, printed)")
	fl.StringVar(&f.save, "save", "", "save the response: none, last or per-status (default: the responses.save setting)")
	fl.StringVar(&f.pin, "pin", "", "pin the response as an example, named after its status or the given name")
	fl.Lookup("pin").NoOptDefVal = pinDefault
	fl.BoolVar(&f.headers, "headers", false, "also print the response headers")
	fl.BoolVar(&f.quiet, "quiet", false, "print only the status line")
	return cmd
}

func runSend(cmd *cobra.Command, i do.Injector, ref string, f sendFlags) error {
	opts, err := f.options()
	if err != nil {
		return err
	}
	s, err := session(cmd, i)
	if err != nil {
		return err
	}
	target, err := s.Workspace.FindRequest(ref, f.collection)
	if err != nil {
		return err
	}
	send, err := s.Workspace.SendRequest(cmd.Context(), target, opts)
	if err != nil {
		return err
	}
	res, err := renderSend(cmd, i, send, f)
	if err != nil {
		return err
	}
	logSendFinished(i, target, res)
	return finishSend(cmd, s.Workspace, res, f)
}

// options turns flags into send options; bad values are usage errors.
func (f sendFlags) options() (flowi.SendOptions, error) {
	with, err := parseWith(f.with)
	if err != nil {
		return flowi.SendOptions{}, err
	}
	hooks, err := hookControl(f.noHooks, f.skip)
	if err != nil {
		return flowi.SendOptions{}, err
	}
	if f.pin != pinDefault {
		if err := flowi.CheckExampleName(f.pin); err != nil {
			return flowi.SendOptions{}, err // before sending: the response would be lost
		}
	}
	if f.save != "" && !slices.Contains([]string{"none", "last", "per-status"}, f.save) {
		return flowi.SendOptions{}, &UsageError{Err: fmt.Errorf("--save must be none, last or per-status, got %q", f.save)}
	}
	return flowi.SendOptions{Env: f.env, Version: f.version, PreferVersion: f.prefer, Save: f.save, With: with, Hooks: hooks, Seed: f.seed}, nil
}

func parseWith(pairs []string) (map[string]string, error) { return parsePairs("--with", pairs) }

func hookControl(none bool, skip []string) (model.HookControl, error) {
	for _, s := range skip {
		if s != "pre" && s != "post" {
			return model.HookControl{}, &UsageError{Err: fmt.Errorf("--skip takes pre or post, got %q", s)}
		}
	}
	if none {
		return model.HookControl{Mode: "none"}, nil
	}
	return model.HookControl{Skip: skip}, nil
}

// renderSend shows the send's progress and returns its result: NDJSON,
// live progress on a terminal, or plain lines.
func renderSend(cmd *cobra.Command, i do.Injector, send *engine.Send, f sendFlags) (engine.SendResult, error) {
	g := do.MustInvoke[*GlobalOptions](i)
	console := do.MustInvoke[Console](i)
	r := textReport{out: cmd.OutOrStdout(), err: cmd.ErrOrStderr(), cwd: do.MustInvoke[Env](i).Cwd, headers: f.headers, quiet: f.quiet}
	switch tui.ChooseRenderer(g.Output, f.quiet, console.OutTTY, console.ErrTTY) {
	case tui.RendererJSON:
		return writeNDJSON(cmd.OutOrStdout(), send)
	case tui.RendererLive:
		seen, err := tui.RunSend(send.Events(), cmd.ErrOrStderr(), tui.Options{Color: !console.NoColor})
		if err != nil {
			drainEvents(send) // the live view failed; the send still has to finish
			cmd.PrintErrf("warning: %v\n", err)
		}
		for _, ev := range seen {
			r.event(ev) // the permanent record, as without a terminal
		}
		res := send.Wait()
		return res, r.finish(res)
	}
	return r.render(send)
}

// finishSend pins the example if asked and maps the outcome to an exit code.
func finishSend(cmd *cobra.Command, ws *flowi.Workspace, res engine.SendResult, f sendFlags) error {
	if f.pin != "" && res.Record != nil {
		name := f.pin
		if name == pinDefault {
			name = ""
		}
		file, err := ws.PinExample(*res.Record, name)
		if err != nil {
			return err
		}
		cmd.PrintErrf("pinned example %s\n", file)
	}
	if res.Err != nil {
		cmd.PrintErrf("warning: %v\n", res.Err)
	}
	if code := sendExitCode(res); code != ExitOK {
		return Exit(code)
	}
	return nil
}

// sendExitCode: failed checks are 1, cancellation 3, transport failures 4
// (HTTP errors, and an async request that timed out), and every other error
// (bad input, missing secrets) 2.
func sendExitCode(res engine.SendResult) int {
	switch res.Status {
	case engine.StatusPassed:
		return ExitOK
	case engine.StatusFailed:
		return ExitFailed
	case engine.StatusCancelled:
		return ExitCancelled
	}
	if slices.ContainsFunc(res.Diags, func(d diag.Diagnostic) bool {
		return strings.HasPrefix(string(d.Code), "FLW-HTTP-") || d.Code == diag.CodeAsyncTimeout
	}) {
		return ExitSystem
	}
	return ExitUsage
}

// logSendFinished logs the outcome without URLs or bodies: those may hold
// values that masking did not know were secret.
func logSendFinished(i do.Injector, t flowi.RequestTarget, res engine.SendResult) {
	logger, err := do.InvokeNamed[*slog.Logger](i, LoggerName)
	if err != nil {
		return // logging problems are reported once by the app and never change the outcome
	}
	logger.Debug("send finished", "Event", "SendFinished", "RunID", res.RunID, "RequestID", t.Request, "Status", res.Status)
}
