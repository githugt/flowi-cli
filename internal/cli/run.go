// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/samber/do/v2"
	"github.com/spf13/cobra"

	flowi "github.com/githugt/flowi-lib"
	"github.com/githugt/flowi-lib/diag"
	"github.com/githugt/flowi-lib/engine"
	"github.com/githugt/flowi-lib/report"

	"github.com/githugt/flowi-cli/internal/tui"
)

type runFlags struct {
	collection, env, prefer string
	inputs, reports         []string
	seed                    uint64
	grace                   time.Duration
	quiet                   bool
}

func newRunCommand(i do.Injector) *cobra.Command {
	var f runFlags
	cmd := &cobra.Command{
		Use:   "run <flow>",
		Short: "Run a flow",
		Long: "Run a flow (by path, e.g. user-creation, or id) and show each node as it finishes.\n\n" +
			"Exit codes: 0 passed, 1 a node failed, 2 bad usage or input, 3 cancelled or aborted,\n" +
			"4 a transport or system failure (including a report that could not be written).\n" +
			"Every run's report is saved in the workspace; --report also writes it as junit, json,\n" +
			"markdown or text.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return runRun(cmd, i, args[0], f) },
	}
	fl := cmd.Flags()
	fl.StringVar(&f.collection, "collection", "", "collection (name or id) to look in")
	fl.StringVar(&f.env, "env", "", "environment (default: the defaults.environment setting)")
	fl.StringVar(&f.prefer, "prefer-version", "", "run this version of each request that has it")
	fl.StringArrayVar(&f.inputs, "input", nil, "value for a flow input, as name=value (a template; repeatable)")
	fl.Uint64Var(&f.seed, "seed", 0, "seed for generated values (default: random, printed)")
	fl.DurationVar(&f.grace, "grace", 0, "how long a cancelled run may take to stop (default: the run.grace setting)")
	fl.StringArrayVar(&f.reports, "report", nil, "also write the report, as format=path: junit, json, markdown or text (repeatable)")
	fl.BoolVar(&f.quiet, "quiet", false, "print only the final status line")
	return cmd
}

func runRun(cmd *cobra.Command, i do.Injector, ref string, f runFlags) error {
	opts, reports, err := f.options()
	if err != nil {
		return err
	}
	s, err := session(cmd, i)
	if err != nil {
		return err
	}
	target, err := s.Workspace.FindFlow(ref, f.collection)
	if err != nil {
		return err
	}
	run, err := s.Workspace.RunFlow(cmd.Context(), target, opts)
	if err != nil {
		return err
	}
	res := renderRun(cmd, i, run, f)
	logRun(i, target, res)
	return finishRun(cmd, i, res, writeReports(reports, res.Report))
}

// options turns flags into run options; bad values are usage errors.
func (f runFlags) options() (flowi.RunOptions, []reportSpec, error) {
	inputs, err := parsePairs("--input", f.inputs)
	if err != nil {
		return flowi.RunOptions{}, nil, err
	}
	reports, err := parseReports(f.reports)
	if err != nil {
		return flowi.RunOptions{}, nil, err
	}
	if f.grace < 0 {
		return flowi.RunOptions{}, nil, &UsageError{Err: errors.New("--grace must not be negative")}
	}
	return flowi.RunOptions{Env: f.env, PreferVersion: f.prefer, Inputs: inputs, Seed: f.seed, Grace: f.grace}, reports, nil
}

func parsePairs(flag string, pairs []string) (map[string]string, error) {
	out := map[string]string{}
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok || k == "" {
			return nil, &UsageError{Err: fmt.Errorf("%s wants name=value, got %q", flag, p)}
		}
		out[k] = v
	}
	return out, nil
}

// reportSpec is one --report format=path.
type reportSpec struct{ format, path string }

func reportWriters() map[string]func(io.Writer, report.Run) error {
	return map[string]func(io.Writer, report.Run) error{
		"junit": report.WriteJUnit, "json": report.WriteJSON, "markdown": report.WriteMarkdown, "text": report.WriteText,
	}
}

func parseReports(flags []string) ([]reportSpec, error) {
	writers := reportWriters()
	out := make([]reportSpec, 0, len(flags))
	for _, f := range flags {
		format, path, ok := strings.Cut(f, "=")
		if _, known := writers[format]; !ok || !known || path == "" {
			return nil, &UsageError{Err: fmt.Errorf("--report wants format=path with format junit, json, markdown or text, got %q", f)}
		}
		out = append(out, reportSpec{format: format, path: path})
	}
	return out, nil
}

// writeReports writes every requested report, even after one fails.
func writeReports(specs []reportSpec, r report.Run) error {
	var errs []error
	for _, s := range specs {
		errs = append(errs, writeReport(s, r))
	}
	return errors.Join(errs...)
}

func writeReport(s reportSpec, r report.Run) error {
	f, err := os.Create(s.path) //nolint:gosec // the user names the report file on the command line
	if err != nil {
		return fmt.Errorf("writing %s report: %w", s.format, err)
	}
	err = reportWriters()[s.format](f, r)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("writing %s report %s: %w", s.format, s.path, err)
	}
	return nil
}

// renderRun shows the run's progress and returns its result: NDJSON, live
// progress on a terminal (the plain record is printed after), or plain
// lines.
func renderRun(cmd *cobra.Command, i do.Injector, run *engine.Run, f runFlags) engine.RunResult {
	g := do.MustInvoke[*GlobalOptions](i)
	console := do.MustInvoke[Console](i)
	p := runText{out: cmd.OutOrStdout(), err: cmd.ErrOrStderr(), quiet: f.quiet}
	switch tui.ChooseRenderer(g.Output, f.quiet, console.OutTTY, console.ErrTTY) {
	case tui.RendererJSON:
		res, err := writeRunNDJSON(cmd.OutOrStdout(), run)
		if err != nil {
			cmd.PrintErrf("warning: %v\n", err)
		}
		return res
	case tui.RendererLive:
		return p.live(run, console)
	}
	return p.render(run)
}

// finishRun prints problems and maps the outcome to an exit code.
func finishRun(cmd *cobra.Command, i do.Injector, res engine.RunResult, reportErr error) error {
	g := do.MustInvoke[*GlobalOptions](i)
	if g.Output != OutputJSON {
		failures := slices.DeleteFunc(slices.Clone(res.Diags), func(d diag.Diagnostic) bool { return d.Severity != diag.SeverityError })
		if err := PrintDiagnostics(cmd.ErrOrStderr(), OutputText, do.MustInvoke[Env](i).Cwd, failures); err != nil {
			return err
		}
	}
	code := runExitCode(res)
	switch {
	case res.ReportFile == "":
		// Every run's report is saved (design §7.7); not saving it is a
		// system failure (exit 4), with the cause in res.Err.
		cmd.PrintErrf("error: the run report could not be saved: %v\n", res.Err)
		code = max(code, ExitSystem)
	case res.Err != nil:
		cmd.PrintErrf("warning: %v\n", res.Err)
	}
	if reportErr != nil {
		cmd.PrintErrf("error: %v\n", reportErr)
		code = max(code, ExitSystem)
	}
	if code != ExitOK {
		return Exit(code)
	}
	return nil
}

// runExitCode follows design §12: a failed node is 1, cancelled or aborted
// 3, a transport or system failure 4.
func runExitCode(res engine.RunResult) int {
	codes := map[engine.Status]int{
		engine.StatusPassed: ExitOK, engine.StatusFailed: ExitFailed, engine.StatusError: ExitSystem,
		engine.StatusCancelled: ExitCancelled, engine.StatusAborted: ExitCancelled,
	}
	if code, ok := codes[res.Status]; ok {
		return code
	}
	return ExitFailed
}
