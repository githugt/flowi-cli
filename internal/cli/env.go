// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/samber/do/v2"
	"github.com/spf13/cobra"
)

func newEnvCommand(i do.Injector) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Manage environments (global to the workspace)",
		Long: "Environments belong to the workspace, not to a collection: every collection sees the same set. They live\n" +
			"in the workspace's environments directory and are not committed with a collection.",
	}
	ls := &cobra.Command{
		Use:   "ls",
		Short: "List the workspace's environments",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return runEnvLs(cmd, i) },
	}
	cmd.AddCommand(ls)
	return cmd
}

func runEnvLs(cmd *cobra.Command, i do.Injector) error {
	s, err := session(cmd, i)
	if err != nil {
		return err
	}
	envs := s.Workspace.Environments()
	type row struct {
		Name string `json:"name"`
		Vars int    `json:"vars"`
	}
	rows := make([]row, len(envs))
	for n, e := range envs {
		rows[n] = row{Name: e.Name, Vars: len(e.Vars)}
	}
	if do.MustInvoke[*GlobalOptions](i).Output == OutputJSON {
		return writeJSONLines(cmd.OutOrStdout(), rows)
	}
	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tVARIABLES")
	for _, r := range rows {
		_, _ = fmt.Fprintf(tw, "%s\t%d\n", r.Name, r.Vars)
	}
	return tw.Flush()
}
