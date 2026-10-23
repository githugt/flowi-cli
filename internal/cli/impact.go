// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"encoding/json"
	"fmt"

	"github.com/samber/do/v2"
	"github.com/spf13/cobra"

	"github.com/githugt/flowi-lib/analysis"
)

type impactFlags struct {
	collection, version string
}

func newImpactCommand(i do.Injector) *cobra.Command {
	var f impactFlags
	cmd := &cobra.Command{
		Use:   "impact <request>",
		Short: "Show which flows and hooks use a request, and whether they still fit it",
		Long: "List every consumer of a request (by path, e.g. auth/login, or id) across all mounted\n" +
			"collections: flow request nodes, post-hooks, and the request's own extract and assert.\n" +
			"Each is checked against the request's contract (its pinned examples, else its last\n" +
			"saved response): ✓ compatible, ⚠ unknown, ✗ broken.\n\n" +
			"Exits 1 when any consumer is broken.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return runImpact(cmd, i, args[0], f) },
	}
	fl := cmd.Flags()
	fl.StringVar(&f.collection, "collection", "", "collection (name or id) to look in")
	fl.StringVar(&f.version, "version", "", "report only this version")
	return cmd
}

func runImpact(cmd *cobra.Command, i do.Injector, ref string, f impactFlags) error {
	s, err := session(cmd, i)
	if err != nil {
		return err
	}
	target, err := s.Workspace.FindRequest(ref, f.collection)
	if err != nil {
		return err
	}
	rep, diags, err := s.Workspace.Impact(target, f.version)
	if err != nil {
		return err
	}
	g := do.MustInvoke[*GlobalOptions](i)
	if err := PrintDiagnostics(cmd.ErrOrStderr(), g.Output, do.MustInvoke[Env](i).Cwd, diags); err != nil {
		return err
	}
	if err := writeImpact(cmd, g.Output, rep); err != nil {
		return err
	}
	if rep.Status == analysis.Broken {
		return Exit(ExitFailed)
	}
	return nil
}

func writeImpact(cmd *cobra.Command, output string, rep analysis.Report) error {
	if output != OutputJSON {
		writeImpactText(cmd.OutOrStdout(), rep)
		return nil
	}
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding impact report: %w", err)
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return err
}
